import assert from "node:assert/strict";
import test from "node:test";
import { FakeElement } from "./helpers/fake-dom.mjs";
import { loadMain } from "./helpers/plugin-main-loader.mjs";

function deferred() {
  let resolve; let reject;
  const promise = new Promise((res, rej) => { resolve = res; reject = rej; });
  return { promise, resolve, reject };
}
function makeApp(captured) {
  const vault = {
    adapter: { async exists() { return false; }, async read() { return null; }, async write() {} },
    getName() { return "vault"; }, getAbstractFileByPath() { return null; }, getFiles() { return []; },
    on() { return { unload() {} }; }, createFolder() { return Promise.resolve(); },
  };
  const workspace = {
    onLayoutReady(cb) { captured.cb = cb; },
    on() { return { unload() {} }; }, getLeavesOfType() { return []; }, getRightLeaf() { return null; },
    getActiveFile() { return null; }, revealLeaf() { return Promise.resolve(); }, detachLeavesOfType() {},
  };
  return { vault, workspace, setting: { open() {}, openTabById() {} } };
}
function seedPluginData(overrides = {}) {
  return {
    // 合法 JWT：loadSettings 会校验 exp，过期或畸形令牌会被丢弃
    token: makeToken(3600), serverUrl: "http://localhost:9090", username: "tester", password: "",
    syncIntervalSec: 3, maxConcurrency: 6, syncPoisonObsidianFiles: false, incrementalCheck: true,
    keepDirectoryTree: true, vaultId: "vault-1", vaultName: "Vault", clientId: "client-1",
    deviceName: "device-1", remotePollIntervalSec: 30, forceSSE: false, diagnosticsEnabled: false,
    vaultSyncMode: "short_poll", language: "en", role: "", updateRepo: "owner/repo", ...overrides,
  };
}

function makeToken(expOffsetSec) {
  const header = Buffer.from(JSON.stringify({ alg: "HS256", typ: "JWT" })).toString("base64url");
  const payload = Buffer.from(JSON.stringify({ uid: 1, exp: Math.floor(Date.now() / 1000) + expOffsetSec })).toString("base64url");
  return `${header}.${payload}.signature`;
}
function installWindow() {
  const origWindow = globalThis.window; const origFake = globalThis.FakeElement; const origNotices = globalThis.__ossNotices;
  const origModals = globalThis.__ossOpenedModals;
  globalThis.window = { setInterval: () => 1, clearInterval: () => {}, setTimeout, clearTimeout };
  globalThis.FakeElement = FakeElement; globalThis.__ossNotices = []; globalThis.__ossOpenedModals = [];
  return () => {
    if (origWindow === undefined) delete globalThis.window; else globalThis.window = origWindow;
    globalThis.FakeElement = origFake; globalThis.__ossNotices = origNotices;
    globalThis.__ossOpenedModals = origModals;
  };
}
async function createPlugin(captured) {
  const { module, cleanup } = await loadMain();
  const app = makeApp(captured);
  const manifest = { id: "oss-sync", version: "0.1.0", dir: ".obsidian/plugins/oss-sync" };
  const plugin = new module.default(app, manifest);
  plugin.loadData = async () => seedPluginData();
  plugin.saveData = async () => {};
  await plugin.onload();
  return { plugin, cleanup };
}
async function flushTicks(n = 2) {
  for (let i = 0; i < n; i++) { await Promise.resolve(); await new Promise((r) => setImmediate(r)); }
}

test("starts collaboration before vault validation settles and defers forced sync until success", async () => {
  const restore = installWindow();
  const captured = { cb: null };
  const { plugin, cleanup } = await createPlugin(captured);
  try {
    const validation = deferred();
    plugin.ensureVaultBinding = () => validation.promise;
    let collabStarts = 0; plugin.collabManager.start = () => { collabStarts += 1; };
    const runOnceCalls = []; plugin.syncEngine.runOnce = async (opts) => { runOnceCalls.push(opts); return true; };
    assert.ok(captured.cb); captured.cb();
    await Promise.resolve(); await new Promise((r) => setImmediate(r));
    assert.equal(collabStarts, 1); assert.equal(runOnceCalls.length, 0);
    validation.resolve(); await flushTicks(3);
    assert.equal(collabStarts, 1); assert.deepEqual(runOnceCalls, [{ forceFull: true }]);
  } finally { await plugin.onunload(); await cleanup(); restore(); }
});

test("keeps collaboration started and does not force sync when validation rejects offline", async () => {
  const restore = installWindow();
  const captured = { cb: null };
  const { plugin, cleanup } = await createPlugin(captured);
  try {
    const validation = deferred();
    plugin.ensureVaultBinding = () => validation.promise;
    let collabStarts = 0; plugin.collabManager.start = () => { collabStarts += 1; };
    const runOnceCalls = []; plugin.syncEngine.runOnce = async (opts) => { runOnceCalls.push(opts); return true; };
    assert.ok(captured.cb); captured.cb();
    validation.reject(new Error("offline"));
    await Promise.resolve(); await new Promise((r) => setImmediate(r)); await new Promise((r) => setImmediate(r));
    assert.equal(collabStarts, 1); assert.equal(runOnceCalls.length, 0);
  } finally { await plugin.onunload(); await cleanup(); restore(); }
});

test("unload before first microtask prevents eager collaboration and forced sync", async () => {
  const restore = installWindow();
  const captured = { cb: null };
  const { plugin, cleanup } = await createPlugin(captured);
  try {
    const validation = deferred();
    plugin.ensureVaultBinding = () => validation.promise;
    let collabStarts = 0; plugin.collabManager.start = () => { collabStarts += 1; };
    const runOnceCalls = []; plugin.syncEngine.runOnce = async (opts) => { runOnceCalls.push(opts); return true; };
    assert.ok(captured.cb); captured.cb();
    await plugin.onunload();
    await flushTicks(3);
    validation.resolve(); await flushTicks(2);
    assert.equal(collabStarts, 0); assert.equal(runOnceCalls.length, 0);
  } finally { await cleanup(); restore(); }
});

test("unload while validation pending prevents forced sync and does not restart collaboration", async () => {
  const restore = installWindow();
  const captured = { cb: null };
  const { plugin, cleanup } = await createPlugin(captured);
  try {
    const validation = deferred();
    plugin.ensureVaultBinding = () => validation.promise;
    let collabStarts = 0; plugin.collabManager.start = () => { collabStarts += 1; };
    const runOnceCalls = []; plugin.syncEngine.runOnce = async (opts) => { runOnceCalls.push(opts); return true; };
    assert.ok(captured.cb); captured.cb();
    await Promise.resolve(); await new Promise((r) => setImmediate(r));
    assert.equal(collabStarts, 1);
    await plugin.onunload();
    validation.resolve(); await flushTicks(3);
    assert.equal(collabStarts, 1); assert.equal(runOnceCalls.length, 0);
  } finally { await cleanup(); restore(); }
});

test("synchronous collab start throw is caught by Notice path with no validation or forced sync", async () => {
  const restore = installWindow();
  const captured = { cb: null };
  const { plugin, cleanup } = await createPlugin(captured);
  try {
    let validationCalls = 0;
    plugin.ensureVaultBinding = async () => { validationCalls += 1; };
    const runOnceCalls = []; plugin.syncEngine.runOnce = async (opts) => { runOnceCalls.push(opts); return true; };
    plugin.collabManager.start = () => { throw new Error("boom"); };
    assert.ok(captured.cb); captured.cb();
    await flushTicks(3);
    assert.equal(validationCalls, 0); assert.equal(runOnceCalls.length, 0);
    assert.ok(globalThis.__ossNotices.length >= 1);
  } finally { await plugin.onunload(); await cleanup(); restore(); }
});

test("plugin update failure restarts paused sync and collaboration", async () => {
  const restore = installWindow();
  const captured = { cb: null };
  const { plugin, cleanup } = await createPlugin(captured);
  try {
    plugin.app.plugins = {
      plugins: { "oss-sync": plugin },
      async disablePlugin() {},
      async enablePlugin() {},
    };
    plugin.app.vault.adapter = {
      async read() { return "old"; },
      async write() { throw new Error("write failed"); },
      async rename() {},
      async remove() {},
    };
    const calls = [];
    plugin.syncEngine.stop = () => calls.push("sync-stop");
    plugin.syncEngine.start = () => calls.push("sync-start");
    plugin.collabManager.isRunning = () => true;
    plugin.collabManager.stop = () => calls.push("collab-stop");
    plugin.collabManager.start = () => calls.push("collab-start");

    await assert.rejects(
      plugin.applyPluginUpdateFiles([{ name: "main.js", content: new ArrayBuffer(0) }]),
      /write failed/,
    );
    assert.deepEqual(calls, ["sync-stop", "collab-stop", "sync-start", "collab-start"]);
  } finally { await plugin.onunload(); await cleanup(); restore(); }
});

test("expired device token is removed and prompts for login without losing vault binding", async () => {
  const restore = installWindow();
  const captured = { cb: null };
  const { plugin, cleanup } = await createPlugin(captured);
  try {
    let saved;
    const stops = [];
    plugin.saveData = async (data) => { saved = data; };
    plugin.syncEngine.stop = () => stops.push("sync");
    plugin.collabManager.stop = () => stops.push("collaboration");

    await plugin.handleTokenExpired();

    assert.equal(plugin.isLoggedIn(), false);
    assert.deepEqual(stops, ["sync", "collaboration"]);
    assert.equal(Object.hasOwn(saved, "token"), false);
    assert.equal(saved.clientId, "client-1");
    assert.equal(saved.vaultId, "vault-1");
    assert.equal(saved.vaultName, "Vault");
    assert.equal(globalThis.__ossNotices.length, 1);
    assert.match(globalThis.__ossNotices[0], /expired|过期/i);
  } finally { await plugin.onunload(); await cleanup(); restore(); }
});

test("attachment conflict opens a binary resolver without downloading text", async () => {
  const restore = installWindow();
  const { plugin, cleanup } = await createPlugin({ cb: null });
  try {
    plugin.app.vault.getAbstractFileByPath = () => ({ __tfile: true, path: "附件/image.png" });
    plugin.syncEngine.getConflict = () => ({ remoteDeleted: false, remoteRevision: 75, remoteType: "attachment" });
    plugin.syncEngine.getBaseline = () => null;
    plugin.api.downloadV2 = async () => { throw new Error("must not preload binary as text"); };
    plugin.openConflictModal("附件/image.png");
    await flushTicks(2);
    assert.equal(globalThis.__ossOpenedModals.length, 1);
    assert.equal(globalThis.__ossOpenedModals[0].options.binary, true);
    assert.equal(globalThis.__ossNotices.length, 0);
  } finally { await plugin.onunload(); await cleanup(); restore(); }
});

test("opens one resolver for a remote deletion conflict and releases the path when closed", async () => {
  const restore = installWindow();
  const captured = { cb: null };
  const { plugin, cleanup } = await createPlugin(captured);
  try {
    plugin.app.vault.getAbstractFileByPath = () => ({ __tfile: true, path: "note.md" });
    plugin.syncEngine.getConflict = () => ({ remoteDeleted: true, remoteRevision: 7 });
    plugin.syncEngine.getBaseline = () => ({ baseText: "before" });
    let downloads = 0;
    plugin.api.downloadV2 = async () => { downloads += 1; throw new Error("must not download a tombstone"); };

    plugin.openConflictModal("note.md");
    plugin.openConflictModal("note.md");
    await flushTicks(2);

    assert.equal(downloads, 0);
    assert.equal(globalThis.__ossOpenedModals.length, 1);
    assert.equal(globalThis.__ossNotices.length, 0);

    globalThis.__ossOpenedModals[0].onClose();
    plugin.openConflictModal("note.md");
    await flushTicks(2);
    assert.equal(globalThis.__ossOpenedModals.length, 2);
  } finally { await plugin.onunload(); await cleanup(); restore(); }
});

test("ensureVaultBinding falls back to the default vault when unbound", async () => {
  const restore = installWindow();
  const captured = { cb: null };
  const { plugin, cleanup } = await createPlugin(captured);
  try {
    plugin.settings.vaultId = "";
    plugin.settings.vaultName = "";
    const saved = [];
    plugin.saveSettings = async () => { saved.push({ vaultId: plugin.settings.vaultId, vaultName: plugin.settings.vaultName }); };
    plugin.refreshVaults = async () => [
      { id: "vault-other", name: "Other", is_default: false },
      { id: "vault-default", name: "Default", is_default: true },
    ];
    await plugin.ensureVaultBinding();
    assert.equal(plugin.settings.vaultId, "vault-default");
    assert.equal(plugin.settings.vaultName, "Default");
    assert.deepEqual(saved, [{ vaultId: "vault-default", vaultName: "Default" }]);
  } finally { await plugin.onunload(); await cleanup(); restore(); }
});

test("ensureVaultBinding falls back to the first vault when none is default", async () => {
  const restore = installWindow();
  const captured = { cb: null };
  const { plugin, cleanup } = await createPlugin(captured);
  try {
    plugin.settings.vaultId = "";
    plugin.settings.vaultName = "";
    plugin.saveSettings = async () => {};
    plugin.refreshVaults = async () => [
      { id: "vault-a", name: "A", is_default: false },
      { id: "vault-b", name: "B", is_default: false },
    ];
    await plugin.ensureVaultBinding();
    assert.equal(plugin.settings.vaultId, "vault-a");
    assert.equal(plugin.settings.vaultName, "A");
  } finally { await plugin.onunload(); await cleanup(); restore(); }
});

test("ensureVaultBinding keeps a stale vault unbound when the list is empty", async () => {
  const restore = installWindow();
  const captured = { cb: null };
  const { plugin, cleanup } = await createPlugin(captured);
  try {
    plugin.settings.vaultId = "vault-gone";
    plugin.settings.vaultName = "Gone";
    let collabStopped = 0;
    plugin.collabManager.stop = () => { collabStopped += 1; };
    plugin.refreshVaults = async () => [];
    await plugin.ensureVaultBinding();
    assert.equal(plugin.settings.vaultId, "");
    assert.equal(plugin.settings.vaultName, "");
    assert.equal(collabStopped, 1);
  } finally { await plugin.onunload(); await cleanup(); restore(); }
});

test("loadSettings drops an expired persisted token", async () => {
  const restore = installWindow();
  const captured = { cb: null };
  const { module, cleanup } = await loadMain();
  const app = makeApp(captured);
  const manifest = { id: "oss-sync", version: "0.1.0", dir: ".obsidian/plugins/oss-sync" };
  const plugin = new module.default(app, manifest);
  plugin.loadData = async () => seedPluginData({ token: makeToken(-60) });
  const writes = [];
  plugin.saveData = async (data) => { writes.push(data); };
  try {
    await plugin.loadSettings();
    assert.equal(plugin.isLoggedIn(), false);
  } finally { await cleanup(); restore(); }
});

test("loadSettings keeps a valid persisted token and wires it to the api client", async () => {
  const restore = installWindow();
  const captured = { cb: null };
  const { module, cleanup } = await loadMain();
  const app = makeApp(captured);
  const manifest = { id: "oss-sync", version: "0.1.0", dir: ".obsidian/plugins/oss-sync" };
  const plugin = new module.default(app, manifest);
  plugin.loadData = async () => seedPluginData({ token: makeToken(3600) });
  plugin.saveData = async () => {};
  try {
    await plugin.onload();
    assert.equal(plugin.isLoggedIn(), true);
  } finally { await plugin.onunload(); await cleanup(); restore(); }
});
