import assert from "node:assert/strict";
import test from "node:test";
import { loadModule, loadSyncEngine } from "./helpers/sync-engine-loader.mjs";

const bytes = new TextEncoder().encode("same content");
async function fixture(run) {
  const previousWindow = globalThis.window;
  globalThis.window = { setTimeout, clearTimeout, setInterval, clearInterval };
  const engineModule = await loadSyncEngine();
  const baselineModule = await loadModule("src/baseline.ts");
  const data = new Map([["New/a.md", bytes]]);
  const state = new Map();
  const indexed = new Set(data.keys());
  const calls = [];
  const file = (path) => indexed.has(path) && data.has(path)
    ? { __tfile: true, path, stat: { mtime: 1, size: data.get(path).length } } : null;
  const adapter = {
    async exists(path) { return data.has(path) || state.has(path); },
    async read(path) { return state.get(path); },
    async write(path, value) { state.set(path, value); calls.push("save"); },
    async list(path) {
      const prefix = path ? `${path}/` : "";
      const files = [], folders = new Set();
      for (const key of data.keys()) {
        if (!key.startsWith(prefix)) continue;
        const rest = key.slice(prefix.length);
        if (rest.includes("/")) folders.add(prefix + rest.split("/")[0]);
        else files.push(key);
      }
      return { files, folders: [...folders] };
    },
  };
  const vault = {
    adapter,
    getFiles: () => [...indexed].map(file).filter(Boolean),
    getAbstractFileByPath: file,
    async readBinary(target) { return data.get(target.path).slice().buffer; },
  };
  const hash = Array.from(new Uint8Array(await crypto.subtle.digest("SHA-256", bytes))).map((v) => v.toString(16).padStart(2, "0")).join("");
  const base = { serverRevision: 1, serverHash: hash, localHash: hash, serverDeleted: false, localMTime: 1, localSize: bytes.length };
  const store = new baselineModule.module.BaselineStore(vault);
  await store.load();
  store.bindVault("v");
  store.set("Old/a.md", base);
  const api = {
    hasToken: () => true,
    isClockDriftLarge: () => false,
    changes: async () => ({ files: [], next_cursor: 1, has_more: false, recovery_snapshot: false }),
    acknowledge: async () => { calls.push("ack"); },
    uploadV2: async (id, request) => {
      calls.push(`upload:${request.path}`);
      return { path: request.path, hash: request.hash, revision: 2, type: "markdown", size: bytes.length, mtime: 1, deleted: false };
    },
    deleteV2: async (id, request) => {
      calls.push(`delete:${request.path}`);
      return { path: request.path, hash, revision: 3, type: "markdown", size: bytes.length, mtime: 1, deleted: true };
    },
  };
  const plugin = { settings: { vaultId: "v", incrementalCheck: true, maxConcurrency: 2, syncPoisonObsidianFiles: false, syncIntervalSec: 3 }, setSyncState() {}, t: (key) => key };
  const engine = new engineModule.SyncEngine({ vault }, api, store, plugin);
  engine.applyStrategy = async () => {};
  engine.enableLocalDiscovery();
  try { await run({ engine, store, data, indexed, adapter, calls, api, base, hash, vault }); }
  finally { engine.stop(); globalThis.window = previousWindow; await engineModule.cleanup(); await baselineModule.cleanup(); }
}

test("missed move uploads and persists the target before deleting the old path using incremental remote changes", async () => {
  await fixture(async ({ engine, calls, store }) => {
    assert.equal(await engine.runOnce({ forceFull: false }), true);
    const upload = calls.indexOf("upload:New/a.md");
    const deletion = calls.indexOf("delete:Old/a.md");
    assert.ok(upload >= 0 && deletion > upload);
    assert.ok(calls.slice(upload + 1, deletion).includes("save"));
    assert.equal(store.get("Old/a.md").serverDeleted, true);
    assert.deepEqual(store.pending(), []);
  });
});

test("upload failure retains scanned source delete with durable dependencies for retry", async () => {
  await fixture(async ({ engine, calls, store, api }) => {
    api.uploadV2 = async () => { throw new Error("offline"); };
    assert.equal(await engine.runOnce({ forceFull: false }), false);
    assert.equal(calls.some((call) => call.startsWith("delete:")), false);
    assert.deepEqual(store.pending().find((operation) => operation.kind === "delete").waitForPaths, ["New/a.md"]);
  });
});

test("incomplete index and adapter failure abort discovery without deleting sources", async () => {
  await fixture(async ({ engine, indexed, calls, store, adapter }) => {
    indexed.clear();
    assert.equal(await engine.runOnce({ forceFull: false }), false);
    assert.deepEqual(store.pending(), []);
    indexed.add("New/a.md");
    adapter.list = async () => { throw new Error("unavailable"); };
    assert.equal(await engine.runOnce({ forceFull: false }), false);
    assert.equal(calls.some((call) => call.startsWith("delete:")), false);
  });
});

test("scanned deletion rechecks source absence and confirmed target content after a restart", async () => {
  await fixture(async ({ engine, store, data, base, vault }) => {
    await engine.discoverLocalChanges(false);
    const deletion = store.pending().find((operation) => operation.kind === "delete");
    assert.equal(await engine.canApplyScannedDelete(deletion), false);
    const upload = store.pending().find((operation) => operation.kind === "upsert");
    store.removePending(upload.id);
    store.set("New/a.md", base);
    await store.save();
    const restarted = new store.constructor(vault);
    await restarted.load();
    engine.baseline = restarted;
    assert.equal(await engine.canApplyScannedDelete(deletion), true);
    data.set("Old/a.md", bytes);
    assert.equal(await engine.canApplyScannedDelete(deletion), false);
  });
});

test("periodic content check detects a missed modification with unchanged size and mtime", async () => {
  await fixture(async ({ engine, store, data, base }) => {
    store.remove("Old/a.md");
    store.set("New/a.md", base);
    data.set("New/a.md", new TextEncoder().encode("diff content"));
    await engine.discoverLocalChanges(false);
    assert.deepEqual(store.pending(), []);
    await engine.discoverLocalChanges(true);
    assert.equal(store.pending()[0].kind, "upsert");
  });
});

test("missing source checks an overwritten live target and blocks deletion if its upload fails", async () => {
  await fixture(async ({ engine, store, api, calls, base }) => {
    store.set("New/a.md", { ...base, serverHash: "previous-body", localHash: "previous-body" });
    engine.lastContentCheck = Date.now();
    api.uploadV2 = async () => { throw new Error("offline"); };
    assert.equal(await engine.runOnce({ forceFull: false }), false);
    assert.equal(calls.some((call) => call.startsWith("delete:")), false);
    assert.deepEqual(store.pending().find((operation) => operation.kind === "delete").waitForPaths, ["New/a.md"]);
  });
});

test("a second missed move rebuilds upload dependencies instead of waiting on the vanished target", async () => {
  await fixture(async ({ engine, store, data, indexed }) => {
    await engine.discoverLocalChanges(false);
    data.delete("New/a.md");
    indexed.delete("New/a.md");
    data.set("Final/a.md", bytes);
    indexed.add("Final/a.md");
    await engine.discoverLocalChanges(false);
    assert.deepEqual(store.pending().find((operation) => operation.kind === "delete").waitForPaths, ["Final/a.md"]);
  });
});

test("an explicit target delete releases its upload dependency without losing source protection", async () => {
  await fixture(async ({ engine, store }) => {
    await engine.discoverLocalChanges(false);
    store.putPending({ id: "explicit-delete", kind: "delete", path: "New/a.md", createdAt: 1 });
    const source = store.pending().find((operation) => operation.path === "Old/a.md");
    assert.equal(source.source, "scan");
    assert.deepEqual(source.waitForPaths, []);
  });
});

test("local timer runs independently of long polling and is removed on stop", async () => {
  await fixture(async ({ engine }) => {
    const intervals = new Map();
    let id = 0;
    globalThis.window.setInterval = (callback, milliseconds) => {
      intervals.set(++id, { callback, milliseconds });
      return id;
    };
    globalThis.window.clearInterval = (timer) => intervals.delete(timer);
    engine.effectiveMode = "long_poll";
    let longPolls = 0;
    engine.startLongPoll = () => { longPolls++; };
    const runs = [];
    engine.runOnce = async (options) => { runs.push(options); return true; };
    engine.lastContentCheck = 123;
    engine.start();
    assert.equal(engine.lastContentCheck, 0);
    assert.equal(longPolls, 1);
    assert.equal(intervals.size, 1);
    const timer = [...intervals.values()][0];
    assert.equal(timer.milliseconds, 60_000);
    await timer.callback();
    assert.deepEqual(runs, [{ forceFull: false }]);
    engine.stop();
    assert.equal(intervals.size, 0);
    await timer.callback();
    assert.equal(runs.length, 1);
  });
});

test("empty first binding never treats a remote-only path as a local deletion", async () => {
  await fixture(async ({ engine, store }) => {
    store.bindVault("new-vault");
    await engine.discoverLocalChanges(false);
    assert.equal(store.pending().some((operation) => operation.kind === "delete"), false);
  });
});

test("a conflicted existing target prevents scanned source deletion", async () => {
  await fixture(async ({ engine, store, base }) => {
    store.set("New/a.md", base);
    store.putConflict({ path: "New/a.md", localHash: base.localHash, remoteRevision: 2, remoteHash: "changed", remoteDeleted: false, remoteMTime: 1, remoteSize: 1, remoteType: "markdown", detectedAt: 1 });
    await engine.discoverLocalChanges(false);
    const deletion = store.pending().find((operation) => operation.kind === "delete");
    assert.deepEqual(deletion.waitForPaths, ["New/a.md"]);
    assert.equal(await engine.canApplyScannedDelete(deletion), false);
  });
});

test("unchanged paths are not read every run but content is checked after ten minutes", async () => {
  await fixture(async ({ engine, store, vault, base }) => {
    store.remove("Old/a.md");
    store.set("New/a.md", base);
    const read = vault.readBinary;
    let reads = 0;
    vault.readBinary = async (file) => { reads++; return read(file); };
    engine.lastContentCheck = Date.now();
    assert.equal(await engine.runOnce({ forceFull: false }), true);
    assert.equal(reads, 0);
    engine.lastContentCheck = Date.now() - 600_001;
    assert.equal(await engine.runOnce({ forceFull: false }), true);
    assert.equal(reads, 1);
  });
});

test("unindexed hidden configuration baselines do not become scanned deletes", async () => {
  await fixture(async ({ engine, store, base }) => {
    store.remove("Old/a.md");
    store.set(".obsidian/config.json", base);
    await engine.discoverLocalChanges(false);
    assert.equal(store.pending().some((operation) => operation.kind === "delete"), false);
  });
});

test("discovery diagnostics retain operation correlation and duration without private fields", async () => {
  const { module, cleanup } = await loadModule("src/diagnostics.ts");
  try {
    const diagnostics = new module.Diagnostics();
    diagnostics.record({ kind: "local_discovery", at: 1, reason: "rename_applied", count: 1, operationID: "move-1", durationMs: 2, path: "Private/note.md", content: "secret", token: "private-token" });
    assert.deepEqual(diagnostics.snapshot(), [{ kind: "local_discovery", at: 1, reason: "rename_applied", count: 1, operationID: "move-1", durationMs: 2 }]);
  } finally { await cleanup(); }
});
