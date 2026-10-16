import assert from "node:assert/strict";
import test from "node:test";
import { loadModule, loadSyncEngine } from "./helpers/sync-engine-loader.mjs";

const entry = { serverRevision: 1, serverHash: "h", serverDeleted: false, localHash: "h", localMTime: 1, localSize: 1 };
const meta = (path, revision = 1, deleted = false) => ({ path, hash: "h", revision, deleted, mtime: 1, size: 1, type: "markdown" });
const op = (id, oldPath, path) => ({ id, kind: "rename", oldPath, path, createdAt: 1 });
const plugin = { settings: { vaultId: "vault", incrementalCheck: true, maxConcurrency: 1, syncPoisonObsidianFiles: false, syncIntervalSec: 300, remotePollIntervalSec: 30 }, setSyncState() {}, t: (key) => key };

test("planner requires discovery hints for incremental local moves", async () => {
  const { module, cleanup } = await loadModule("src/ordinary-sync-action-planner.ts");
  try {
    const input = { forceFull: false, recoverySnapshot: false, remote: new Map(), baseline: new Map([["A/a.md", entry]]), pending: [], conflicts: new Set(), vaultPaths: ["B/A/a.md"], localByPath: new Map([["A/a.md", null], ["B/A/a.md", { path: "B/A/a.md", hash: "h", mtime: 1, size: 1 }]]), createOperationId: () => "generated" };
    assert.deepEqual(module.ordinarySyncCandidatePaths(input), []);
    assert.deepEqual(module.planOrdinarySyncActions(input).actions, []);
    const full = module.planOrdinarySyncActions({ ...input, forceFull: true, remote: new Map([["A/a.md", meta("A/a.md")]]) });
    assert.deepEqual(full.actions.map(({ kind, path }) => ({ kind, path })), [{ kind: "delete_remote", path: "A/a.md" }, { kind: "upload", path: "B/A/a.md" }]);
  } finally { await cleanup(); }
});

async function withEngine(fn) {
  const [{ SyncEngine, cleanup }, loaded] = await Promise.all([loadSyncEngine(), loadModule("src/baseline.ts")]);
  try {
    const store = new loaded.module.BaselineStore({});
    store.save = async () => {};
    store.load = async () => {};
    store.bindVault = () => false;
    const bytes = new TextEncoder().encode("edited").buffer;
    const file = { __tfile: true, path: "B/A/a.md", stat: { mtime: 2, size: 6 }, content: bytes };
    const vault = { adapter: { exists: async (path) => path === file.path, list: async () => ({ files: [file.path], folders: [] }) }, getAbstractFileByPath: (path) => path === file.path ? file : null, readBinary: async (target) => target.content, getFiles: () => [file] };
    const rename = op("move", "A/a.md", file.path);
    const api = { hasToken: () => true, isClockDriftLarge: () => false, changes: async () => ({ files: [], next_cursor: 0, has_more: false, recovery_snapshot: false }), acknowledge: async () => {}, renameV2: async () => ({ old: meta(rename.oldPath, 2, true), new: meta(rename.path, 3) }) };
    const engine = new SyncEngine({ vault }, api, store, plugin);
    engine.applyStrategy = async () => {};
    await fn({ store, rename, engine, api, file });
  } finally { await cleanup(); await loaded.cleanup(); }
}

test("rename fallback produces an upload from the refreshed journal", async () => {
  await withEngine(async ({ store, rename, engine }) => {
    store.putPending(rename);
    const originalPending = store.pending();
    await engine.processRenames("vault", originalPending, new Map());
    assert.equal(store.pending()[0].kind, "upsert");
    assert.equal((await engine.planActions(false, new Map(), store.pending()))[0].kind, "upload");
  });
});

test("executeRun uploads a downgraded rename in the same run", async () => {
  await withEngine(async ({ store, rename, engine }) => {
    store.putPending(rename);
    const applied = [];
    engine.applyAction = async (vault, action) => { applied.push(action); return { kind: "resolved" }; };
    assert.equal(await engine.executeRun(false), true);
    assert.equal(applied.length, 1);
    assert.equal(applied[0].kind, "upload");
    assert.equal(applied[0].path, rename.path);
  });
});

test("executeRun renames unchanged content without uploading body", async () => {
  await withEngine(async ({ store, rename, engine, api, file }) => {
    const hash = Array.from(new Uint8Array(await crypto.subtle.digest("SHA-256", file.content))).map((v) => v.toString(16).padStart(2, "0")).join("");
    store.set(rename.oldPath, { ...entry, serverHash: hash, localHash: hash });
    store.putPending(rename);
    const renamed = [];
    api.renameV2 = async (vault, request) => { renamed.push(request); return { old: { ...meta(rename.oldPath, 2, true), hash }, new: { ...meta(rename.path, 3), hash } }; };
    const applied = [];
    engine.applyAction = async (vault, action) => { applied.push(action); return { kind: "resolved" }; };
    assert.equal(await engine.executeRun(false), true);
    assert.equal(renamed.length, 1);
    assert.deepEqual(applied, []);
    assert.deepEqual(store.pending(), []);
    assert.equal(store.get(rename.path).serverHash, hash);
  });
});

test("executeRun discovers an unqueued move without fetching the remote manifest", async () => {
  await withEngine(async ({ store, rename, engine, api }) => {
    store.set(rename.oldPath, entry);
    engine.enableLocalDiscovery();
    api.manifest = async () => { assert.fail("local reconciliation must not require a full remote snapshot"); };
    const applied = [];
    engine.applyAction = async (vault, action) => { applied.push(action); return { kind: "resolved" }; };
    assert.equal(await engine.executeRun(false), true);
    assert.deepEqual(applied.map(({ kind, path }) => ({ kind, path })), [{ kind: "upload", path: rename.path }, { kind: "delete_remote", path: rename.oldPath }]);
    assert.equal(applied[1].operation.source, "scan");
    assert.deepEqual(applied[1].operation.waitForPaths, [rename.path]);
  });
});

test("duplicate second rename preserves the earliest source path", async () => {
  const { module, cleanup } = await loadModule("src/baseline.ts");
  try {
    const store = new module.BaselineStore({});
    store.putPending(op("first", "A/a.md", "B/a.md"));
    store.putPending(op("second", "B/a.md", "C/a.md"));
    assert.equal(store.pending()[0].oldPath, "A/a.md");
    store.putPending(op("duplicate-second", "B/a.md", "C/a.md"));
    assert.equal(store.pending()[0].oldPath, "A/a.md");
  } finally { await cleanup(); }
});

test("rename plus pre-run edit retains an upload against the renamed revision", async () => {
  await withEngine(async ({ store, rename, engine }) => {
    store.set(rename.oldPath, entry);
    store.putPending(rename);
    store.putPending({ id: "edit", kind: "upsert", path: rename.path, createdAt: 2 });
    assert.equal(store.pending().length, 1);
    const originalPending = store.pending();
    const remote = new Map();
    await engine.processRenames("vault", originalPending, remote);
    assert.equal(store.pending().length, 1);
    assert.equal(store.pending()[0].kind, "upsert");
    assert.notEqual(store.get(rename.path).localHash, store.get(rename.path).serverHash);
    const actions = await engine.planActions(false, new Map(), store.pending());
    assert.equal(actions.length, 1);
    assert.equal(actions[0].kind, "upload");
    assert.equal(actions[0].baseRevision, 3);
  });
});

test("executeRun uploads pre-run edits immediately after the rename", async () => {
  await withEngine(async ({ store, rename, engine }) => {
    store.set(rename.oldPath, entry);
    store.putPending(rename);
    store.putPending({ id: "edit", kind: "upsert", path: rename.path, createdAt: 2 });
    const applied = [];
    engine.applyAction = async (vault, action) => { applied.push(action); return { kind: "resolved" }; };
    assert.equal(await engine.executeRun(false), true);
    assert.equal(applied.length, 1);
    assert.equal(applied[0].kind, "upload");
    assert.equal(applied[0].path, rename.path);
    assert.equal(applied[0].baseRevision, 3);
  });
});

test("a failed rename preserves its journal and cursor before retrying the complete transfer", async () => {
  await withEngine(async ({ store, rename, engine, api }) => {
    store.set(rename.oldPath, entry);
    store.setCursor(7);
    store.putPending(rename);
    api.changes = async () => ({ files: [], next_cursor: 8, has_more: false, recovery_snapshot: false });
    let acknowledged = 0;
    api.acknowledge = async () => { acknowledged++; };
    const successfulRename = api.renameV2;
    api.renameV2 = async () => { throw new Error("transient transport failure"); };
    const applied = [];
    engine.applyAction = async (vault, action) => { applied.push(action); return { kind: "resolved" }; };
    assert.equal(await engine.executeRun(false), false);
    assert.deepEqual(store.pending(), [rename]);
    assert.equal(store.getCursor(), 7);
    assert.equal(acknowledged, 0);
    assert.deepEqual(applied, []);
    api.renameV2 = successfulRename;
    assert.equal(await engine.executeRun(false), true);
    assert.equal(applied.length, 1);
    assert.equal(applied[0].kind, "upload");
    assert.equal(acknowledged, 1);
    assert.equal(store.getCursor(), 8);
  });
});

test("round-trip A to B to A becomes an upsert at the original path", async () => {
  const { module, cleanup } = await loadModule("src/baseline.ts");
  try {
    const store = new module.BaselineStore({});
    store.putPending(op("move-out", "A/a.md", "B/a.md"));
    store.putPending(op("move-back", "B/a.md", "A/a.md"));
    assert.deepEqual(store.pending(), [{ id: "move-back", kind: "upsert", path: "A/a.md", oldPath: undefined, createdAt: 1 }]);
  } finally { await cleanup(); }
});
