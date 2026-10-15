const assert = require('node:assert/strict');
const { test } = require('node:test');
const fs = require('node:fs');
const path = require('node:path');
const vm = require('node:vm');
const themePath = path.join(__dirname, '../blog/shirone/theme.js');
const { loadHomeIndex, readHomeIndex, matchesPost, paginate } = require(themePath);

const context = { homeURL: '/b/vault-1', pageURL: 'https://blog.example/p/post-1', vaultId: 'vault-1', themeName: 'shirone-custom' };
const post = (href = '/p/post-1', overrides = {}) => ({
  dataset: { title: '中文文章', summary: '公开摘要', category: '技术', date: '2026-10-02', words: '120', ...overrides },
  getAttribute(name) { return name === 'href' ? href : null; },
  querySelectorAll() { return [{ dataset: { tag: 'Go' } }, { dataset: { tag: 'Go' } }]; },
});
const page = (options = {}) => ({
  body: { dataset: { page: 'home', vaultId: 'vault-1', themeName: 'shirone-custom', ...options.identity } },
  querySelector(selector) {
    if (selector === '#post-index') return options.missingIndex ? null : { querySelectorAll: () => options.posts || [post()] };
    if (selector === '[data-blog-name]') return { textContent: options.name || '仓库名称' };
    if (selector === '[data-blog-description]') return { textContent: '博客介绍' };
    throw new Error(`unexpected query: ${selector}`);
  },
});
const response = (overrides = {}) => ({
  ok: true, url: 'https://blog.example/b/vault-1',
  headers: { get: () => 'text/html; charset=utf-8' },
  text: async () => '<html>public homepage</html>', ...overrides,
});

test('public share fetches only its supplied same-origin homepage and extracts plain data', async () => {
  let calls = 0;
  const result = await loadHomeIndex(context, async (url, options) => {
    calls += 1;
    assert.equal(url, 'https://blog.example/b/vault-1');
    assert.equal(options.mode, 'same-origin');
    assert.equal(options.redirect, 'error');
    assert.equal(options.cache, 'no-store');
    assert.equal(options.credentials, 'same-origin');
    return response();
  }, (html) => { assert.equal(html, '<html>public homepage</html>'); return page(); });
  assert.equal(calls, 1);
  assert.equal(result.status, 'ready');
  assert.equal(result.name, '仓库名称');
  assert.deepEqual(result.posts, [{ title: '中文文章', summary: '公开摘要', category: '技术', tags: ['Go'], date: '2026-10-02', words: 120, url: 'https://blog.example/p/post-1' }]);
  assert.equal(Object.hasOwn(result.posts[0], 'element'), false);
});

test('private share and unsafe home addresses never fetch or parse a document', async () => {
  let calls = 0;
  const unexpected = () => { calls += 1; throw new Error('must not be called'); };
  assert.deepEqual(await loadHomeIndex({ ...context, homeURL: '' }, unexpected, unexpected), { status: 'disabled', posts: [] });
  for (const homeURL of ['https://other.example/b/vault-1', 'javascript:alert(1)', '//other.example/b/vault-1', 'https://user:secret@blog.example/b/vault-1']) {
    assert.deepEqual(await loadHomeIndex({ ...context, homeURL }, unexpected, unexpected), { status: 'error', posts: [] });
  }
  assert.equal(calls, 0);
});

test('transport, redirect, content type and parser failures cannot become an empty success', async () => {
  for (const incoming of [
    response({ ok: false }),
    response({ url: 'https://other.example/b/vault-1' }),
    response({ url: 'https://blog.example/login' }),
    response({ headers: { get: () => 'application/json' } }),
  ]) {
    assert.deepEqual(await loadHomeIndex(context, async () => incoming, () => page()), { status: 'error', posts: [] });
  }
  assert.deepEqual(await loadHomeIndex(context, async () => { throw new Error('offline'); }, () => page()), { status: 'error', posts: [] });
  assert.deepEqual(await loadHomeIndex(context, async () => response(), () => { throw new Error('invalid HTML'); }), { status: 'error', posts: [] });
});

test('wrong vault, theme, page type and missing index are rejected', async () => {
  for (const options of [{ identity: { vaultId: 'private-vault' } }, { identity: { themeName: 'default' } }, { identity: { page: 'post' } }, { missingIndex: true }]) {
    assert.deepEqual(await loadHomeIndex(context, async () => response(), () => page(options)), { status: 'error', posts: [] });
  }
  assert.deepEqual(readHomeIndex(page({ posts: [] }), context, response().url).posts, []);
});

test('malicious index URLs are rejected and HTML-like metadata stays literal', async () => {
  for (const href of ['javascript:alert(1)', 'data:text/html,<script>alert(1)</script>', '//evil.example/p/post-1', '/ui/logout', '/p/folder/file.md', '/p/post-1?private=1', '']) {
    const result = await loadHomeIndex(context, async () => response(), () => page({ posts: [post(href)] }));
    assert.deepEqual(result, { status: 'error', posts: [] });
  }
  const hostile = '<img src=x onerror=alert(1)><script>alert(1)</script>';
  const result = readHomeIndex(page({ name: hostile, posts: [post('post-1', { title: hostile, words: 'Infinity' })] }), context, 'https://blog.example/p/index');
  assert.equal(result.name, hostile);
  assert.equal(result.posts[0].title, hostile);
  assert.equal(result.posts[0].url, 'https://blog.example/p/post-1');
  assert.equal(result.posts[0].words, 0);
  assert.equal(JSON.stringify(result).includes('innerHTML'), false);
});

test('loaded index works with existing exact filters and filtered pagination', () => {
  const result = readHomeIndex(page(), context, response().url);
  assert.equal(matchesPost(result.posts[0], { query: '中文 go', category: '技术', tag: 'Go' }), true);
  assert.equal(matchesPost(result.posts[0], { query: '<script>' }), false);
  assert.equal(matchesPost(result.posts[0], { tag: 'go' }), false);
  assert.deepEqual(paginate(result.posts, 9).items, result.posts);
});

test('repeated successful copy restores the original code button label', async () => {
  const source = fs.readFileSync(themePath, 'utf8');
  const fragment = source.slice(source.indexOf('  const copyLabels = '), source.indexOf('  const allowsCopy = '));
  const timers = [];
  const button = { textContent: '复制代码', isConnected: true, classList: { add() {}, remove() {} } };
  const sandbox = { navigator: { clipboard: { writeText: async () => {} } }, announce() {}, $: () => null, window: { setTimeout: (callback) => timers.push(callback) } };
  vm.createContext(sandbox);
  vm.runInContext(`${fragment}\nthis.copyText = copyText;`, sandbox);
  await sandbox.copyText('code', button, '成功');
  await sandbox.copyText('code', button, '成功');
  timers.forEach((callback) => callback());
  assert.equal(button.textContent, '复制代码');
});
