import { mkdtemp, rm } from "node:fs/promises";
import { existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { build } from "esbuild";
function resolveEntry(e) { const alt = join("plugin", e); return existsSync(e) ? e : existsSync(alt) ? alt : e; }
export const FIXED_NOW = 1700000000000;
export const FIXED_ID = "op-fixed-001";
export const FIXED_ID_2 = "op-fixed-002";
export function createSequenceId(p = "op-fixed-") { let n = 1; return () => `${p}${String(n++).padStart(3, "0")}`; }
export function createDeferred() { let res, rej; const promise = new Promise((a, b) => { res = a; rej = b; }); return { promise, resolve: res, reject: rej }; }
export async function settle(c = 10) { for (let i = 0; i < c; i++) await new Promise((r) => setImmediate(r)); }
const OBSIDIAN_STUB = `export class TFile{constructor(p,c){this.path=p;this._content=c;this.stat={mtime:1,size:c.length}}static [Symbol.hasInstance](i){return !!i&&typeof i.path==="string"&&"_content"in i&&"stat"in i}}export class Notice{constructor(m){globalThis.__ossNotices=globalThis.__ossNotices||[];globalThis.__ossNotices.push(m)}}export const App=class{}`;
export async function loadSyncModules() {
  const dir = await mkdtemp(join(tmpdir(), "oss-sync-"));
  const out = join(dir, "bundle.mjs");
  await build({ entryPoints: [resolveEntry("src/collaboration-file-sync.ts")], outfile: out, bundle: true, platform: "node", format: "esm", plugins: [{ name: "obsidian-stub", setup(b) { b.onResolve({ filter: /^obsidian$/ }, () => ({ path: "obsidian", namespace: "obsidian-stub" })); b.onLoad({ filter: /.*/, namespace: "obsidian-stub" }, () => ({ contents: OBSIDIAN_STUB, loader: "js" })); } }] });
  const mod = await import(pathToFileURL(out).href);
  return { mod, cleanup: () => rm(dir, { recursive: true, force: true }) };
}
export async function buildBaselineStore() {
  const dir = await mkdtemp(join(tmpdir(), "oss-baseline-"));
  const out = join(dir, "baseline.mjs");
  await build({ entryPoints: [resolveEntry("src/baseline.ts")], outfile: out, bundle: true, platform: "node", format: "esm", plugins: [{ name: "obsidian-stub", setup(b) { b.onResolve({ filter: /^obsidian$/ }, () => ({ path: "obsidian", namespace: "obsidian-stub" })); b.onLoad({ filter: /.*/, namespace: "obsidian-stub" }, () => ({ contents: "export class TFile{static [Symbol.hasInstance](i){return !!i&&typeof i.path==='string'&&'_content' in i}}", loader: "js" })); } }] });
  const m = await import(pathToFileURL(out).href);
  return { BaselineStore: m.BaselineStore, cleanup: () => rm(dir, { recursive: true, force: true }) };
}
export function createFakeVault(files) {
  const raw = files instanceof Map ? files : new Map(Object.entries(files ?? {}));
  const store = new Map(raw);
  const folders = new Set(); const created = []; const modified = []; const folderCreated = []; const fileObjects = new Map();
  const sizeOf = (c) => c instanceof Uint8Array ? c.byteLength : new TextEncoder().encode(c).length;
  for (const [p, c] of store.entries()) fileObjects.set(p, { path: p, _content: c, stat: { mtime: 1, size: sizeOf(c) } });
  const app = { vault: {
    getAbstractFileByPath(p) { if (fileObjects.has(p)) return fileObjects.get(p); if (folders.has(p)) return { path: p, isFolder: true }; if (store.has(p) && !fileObjects.has(p)) { const c = store.get(p); const o = { path: p, _content: c, stat: { mtime: 1, size: sizeOf(c) } }; fileObjects.set(p, o); return o; } return null; },
    async readBinary(file) { const c = store.get(file.path); if (c === undefined) throw new Error(`file not found: ${file.path}`); if (c instanceof Uint8Array) { const b = new ArrayBuffer(c.byteLength); new Uint8Array(b).set(c); return b; } const bytes = new TextEncoder().encode(c); const b = new ArrayBuffer(bytes.byteLength); new Uint8Array(b).set(bytes); return b; },
    async createBinary(p, buf) { const b = new Uint8Array(buf); let stored; try { stored = new TextDecoder("utf-8", { fatal: true }).decode(b); } catch { stored = new Uint8Array(b); } store.set(p, stored); fileObjects.set(p, { path: p, _content: stored, stat: { mtime: 1, size: b.length } }); created.push(p); },
    async modifyBinary(file, buf) { const b = new Uint8Array(buf); let stored; try { stored = new TextDecoder("utf-8", { fatal: true }).decode(b); } catch { stored = new Uint8Array(b); } const p = file.path; store.set(p, stored); fileObjects.set(p, { path: p, _content: stored, stat: { mtime: 1, size: b.length } }); modified.push(p); },
    async createFolder(p) { folders.add(p); folderCreated.push(p); },
  } };
  return { app, store, fileObjects, folders, created, modified, folderCreated };
}
export function installWindowFake() {
  let nextId = 1; const timers = new Map();
  const fakeWindow = { setTimeout(fn) { const id = nextId++; timers.set(id, fn); return id; }, clearTimeout(id) { timers.delete(id); } };
  globalThis.window = fakeWindow;
  return { timers, flush() { const fns = [...timers.values()]; timers.clear(); for (const fn of fns) fn(); }, restore() { delete globalThis.window; } };
}
export function makeCollabEntry(o = {}) { return { vaultId: "vault-1", fileId: 42, localPath: "协作oss/owner/Shared.md", serverRevision: 7, serverHash: "oldhash", localHash: "oldhash", baseText: "old", pending: null, conflict: null, ...o }; }
export async function makeBaseline(BaselineStore, entry, username = "collab-user") {
  const baseline = new BaselineStore({ adapter: { async exists() { return false; }, async read() { return ""; }, async write() {} } });
  await baseline.load(); baseline.bindCollaborationAccount(username); if (entry) baseline.setCollaboration(entry.vaultId, entry.fileId, entry); return baseline;
}
function makeCoordinator(mod, explicit) {
  if (explicit) return explicit;
  return new mod.CollaborationSyncCoordinator();
}
export function createTrackedCoordinator(mod) {
  const inner = new mod.CollaborationSyncCoordinator();
  let last = Promise.resolve();
  const coordinator = {
    run(task) {
      const p = inner.run(task);
      last = p;
      return p;
    },
    waitForIdle() {
      return last.catch(() => undefined);
    },
  };
  return coordinator;
}
export function createSync(mod, { baseline, vault, api, onChange = () => {}, now = () => FIXED_NOW, createOperationID = () => FIXED_ID, getAccepted, coordinator } = {}) {
  const plugin = { t: (k) => k };
  const coord = makeCoordinator(mod, coordinator);
  return new mod.CollaborationFileSync({ baseline, vault, api, plugin, getAccepted: getAccepted ?? (() => [{ vaultId: "vault-1", fileId: 42, localPath: "协作oss/owner/Shared.md" }]), onChange, now, createOperationID, coordinator: coord });
}
