import assert from "node:assert/strict";
import test from "node:test";
import { loadModule } from "./helpers/sync-engine-loader.mjs";

function makeLocal(path, hash, mtime = 100, size = 10) {
  return { path, hash, mtime, size };
}
function makeRemote(path, { hash = "rh", revision = 1, deleted = false, mtime = 100, size = 10, type = "markdown" } = {}) {
  return { path, type, hash, size, mtime, revision, deleted };
}
function makeBaseline({ serverRevision = 1, serverHash = "sh", serverDeleted = false, localHash = "sh", localMTime = 100, localSize = 10, baseText } = {}) {
  const e = { serverRevision, serverHash, serverDeleted, localHash, localMTime, localSize };
  if (baseText !== undefined) e.baseText = baseText;
  return e;
}

async function loadPlanner() {
  const { module, cleanup } = await loadModule("src/ordinary-sync-action-planner.ts");
  return {
    plan: module.planOrdinarySyncActions,
    candidatePaths: module.ordinarySyncCandidatePaths,
    cleanup,
  };
}

test("candidate paths combine remote, pending, baseline, and vault sources for full scans", async () => {
  const { candidatePaths, cleanup } = await loadPlanner();
  try {
    const paths = candidatePaths({
      forceFull: true,
      remote: new Map([["remote.md", makeRemote("remote.md")]]),
      baseline: new Map([["base.md", makeBaseline()]]),
      conflicts: new Set(),
      pending: [
        { id: "upsert", kind: "upsert", path: "pending.md", createdAt: 1 },
        { id: "rename", kind: "rename", path: "new.md", oldPath: "old.md", createdAt: 2 },
      ],
      vaultPaths: ["vault.md", "remote.md"],
    });

    assert.deepEqual(new Set(paths), new Set([
      "remote.md",
      "pending.md",
      "base.md",
      "vault.md",
    ]));
  } finally {
    await cleanup();
  }
});

test("pending local upload reuses operation id and preserves baseRevision", async () => {
  const { plan, cleanup } = await loadPlanner();
  try {
    const baseline = new Map([["notes/a.md", makeBaseline({ serverRevision: 5, serverHash: "old", localHash: "old" })]]);
    const localByPath = new Map([["notes/a.md", makeLocal("notes/a.md", "new-hash", 200, 12)]]);
    const remote = new Map([["notes/a.md", makeRemote("notes/a.md", { hash: "old", revision: 5 })]]);
    // remote inferred from baseline when forceFull false and submitted missing? but we provide same as baseline to make remoteChanged false
    const pending = [{ id: "op-1", kind: "upsert", path: "notes/a.md", createdAt: 1 }];
    let factoryCalls = 0;
    const result = plan({
      forceFull: false,
      recoverySnapshot: false,
      remote,
      baseline,
      pending,
      localByPath,
      conflicts: new Set(),
      vaultPaths: [],
      createOperationId: () => { factoryCalls++; return "gen-" + factoryCalls; },
    });
    assert.equal(factoryCalls, 0);
    assert.equal(result.actions.length, 1);
    const a = result.actions[0];
    assert.deepEqual(a, {
      kind: "upload",
      path: "notes/a.md",
      local: makeLocal("notes/a.md", "new-hash", 200, 12),
      baseRevision: 5,
      operationID: "op-1",
      operation: pending[0],
    });
    assert.ok(!("bytes" in a.local));
    assert.deepEqual(result.obsoletePendingIds, []);
    assert.deepEqual(result.removedBaselinePaths, []);
    // ensure no mutation
    assert.equal(baseline.size, 1);
    assert.equal(pending.length, 1);
  } finally { await cleanup(); }
});

test("pending delete with acknowledged baseline produces delete_remote", async () => {
  const { plan, cleanup } = await loadPlanner();
  try {
    const baseline = new Map([["notes/d.md", makeBaseline({ serverRevision: 12, serverHash: "h", localHash: "h" })]]);
    const localByPath = new Map([["notes/d.md", null]]);
    const remote = new Map();
    const pending = [{ id: "del-1", kind: "delete", path: "notes/d.md", createdAt: 1 }];
    const result = plan({
      forceFull: false,
      recoverySnapshot: false,
      remote,
      baseline,
      pending,
      localByPath,
      conflicts: new Set(),
      vaultPaths: [],
      createOperationId: () => "gen-x",
    });
    assert.equal(result.actions.length, 1);
    assert.deepEqual(result.actions[0], {
      kind: "delete_remote",
      path: "notes/d.md",
      baseRevision: 12,
      operationID: "del-1",
      operation: pending[0],
    });
    assert.deepEqual(result.obsoletePendingIds, []);
  } finally { await cleanup(); }
});

test("remote-only file triggers download with expectedLocal absent", async () => {
  const { plan, cleanup } = await loadPlanner();
  try {
    const baseline = new Map();
    const localByPath = new Map([["new/file.md", null]]);
    const remote = new Map([["new/file.md", makeRemote("new/file.md", { hash: "rh", revision: 3, deleted: false })]]);
    const result = plan({
      forceFull: false,
      recoverySnapshot: false,
      remote,
      baseline,
      pending: [],
      localByPath,
      conflicts: new Set(),
      vaultPaths: [],
      createOperationId: () => "gen-1",
    });
    assert.equal(result.actions.length, 1);
    const a = result.actions[0];
    assert.equal(a.kind, "download");
    assert.equal(a.path, "new/file.md");
    assert.deepEqual(a.remote, makeRemote("new/file.md", { hash: "rh", revision: 3 }));
    assert.deepEqual(a.expectedLocal, { kind: "absent" });
    assert.equal("local" in a, false);
  } finally { await cleanup(); }
});

test("unchanged local with remote update triggers download and delete_local with hash guard", async () => {
  const { plan, cleanup } = await loadPlanner();
  try {
    const baseline = new Map([["a.md", makeBaseline({ serverRevision: 1, serverHash: "h1", localHash: "h1", localMTime: 10 })]]);
    const localByPath = new Map([["a.md", makeLocal("a.md", "h1", 10)]]);
    // remote updated
    const remoteDownload = new Map([["a.md", makeRemote("a.md", { hash: "h2", revision: 2, deleted: false })]]);
    const r1 = plan({
      forceFull: false,
      recoverySnapshot: false,
      remote: remoteDownload,
      baseline,
      pending: [],
      localByPath,
      conflicts: new Set(),
      vaultPaths: [],
      createOperationId: () => "gen",
    });
    assert.equal(r1.actions.length, 1);
    assert.deepEqual(r1.actions[0], {
      kind: "download",
      path: "a.md",
      remote: makeRemote("a.md", { hash: "h2", revision: 2 }),
      expectedLocal: { kind: "hash", hash: "h1" },
    });

    const remoteDelete = new Map([["a.md", makeRemote("a.md", { hash: "h1", revision: 2, deleted: true })]]);
    const r2 = plan({
      forceFull: false,
      recoverySnapshot: false,
      remote: remoteDelete,
      baseline,
      pending: [],
      localByPath,
      conflicts: new Set(),
      vaultPaths: [],
      createOperationId: () => "gen",
    });
    assert.equal(r2.actions.length, 1);
    assert.deepEqual(r2.actions[0], {
      kind: "delete_local",
      path: "a.md",
      remote: makeRemote("a.md", { hash: "h1", revision: 2, deleted: true }),
      expectedLocal: { kind: "hash", hash: "h1" },
    });
  } finally { await cleanup(); }
});

test("new local file without baseline uploads with generated operation id", async () => {
  const { plan, cleanup } = await loadPlanner();
  try {
    const baseline = new Map();
    const localByPath = new Map([["fresh.md", makeLocal("fresh.md", "lh", 50, 5)]]);
    const remote = new Map();
    let gen = 0;
    const result = plan({
      forceFull: true,
      recoverySnapshot: false,
      remote,
      baseline,
      pending: [],
      localByPath,
      conflicts: new Set(),
      vaultPaths: ["fresh.md"],
      createOperationId: () => `gen-${++gen}`,
    });
    assert.equal(result.actions.length, 1);
    assert.equal(result.actions[0].kind, "upload");
    assert.equal(result.actions[0].operationID, "gen-1");
    assert.equal(result.actions[0].baseRevision, 0);
    assert.deepEqual(result.actions[0].local, makeLocal("fresh.md", "lh", 50, 5));
    assert.ok(!("bytes" in result.actions[0].local));
    assert.equal(gen, 1);
  } finally { await cleanup(); }
});

test("reconcile and adopt branches carry expectedLocal and full shapes", async () => {
  const { plan, cleanup } = await loadPlanner();
  try {
    const baseline = new Map([["note.md", makeBaseline({ serverRevision: 1, serverHash: "base", localHash: "base" })]]);
    // divergent: local changed to lh, remote changed to rh (different)
    const localByPath = new Map([["note.md", makeLocal("note.md", "lh", 20)]]);
    const remoteReconcile = new Map([["note.md", makeRemote("note.md", { hash: "rh", revision: 2, deleted: false })]]);
    const r1 = plan({
      forceFull: false,
      recoverySnapshot: false,
      remote: remoteReconcile,
      baseline,
      pending: [],
      localByPath,
      conflicts: new Set(),
      vaultPaths: [],
      createOperationId: () => "gen",
    });
    assert.equal(r1.actions.length, 1);
    assert.equal(r1.actions[0].kind, "reconcile");
    assert.deepEqual(r1.actions[0], {
      kind: "reconcile",
      path: "note.md",
      local: makeLocal("note.md", "lh", 20),
      remote: makeRemote("note.md", { hash: "rh", revision: 2 }),
      expectedLocal: { kind: "hash", hash: "lh" },
    });

    // same hash divergent -> adopt
    const localSame = new Map([["note.md", makeLocal("note.md", "same", 20)]]);
    const remoteSame = new Map([["note.md", makeRemote("note.md", { hash: "same", revision: 2 })]]);
    // need localChanged true (hash diff from baseline) but hash equals remote => adopt
    // baseline hash is "base", local "same" != base => localChanged true; remote "same" != base => remoteChanged true; but hashes equal => adopt
    const r2 = plan({
      forceFull: false,
      recoverySnapshot: false,
      remote: remoteSame,
      baseline,
      pending: [],
      localByPath: localSame,
      conflicts: new Set(),
      vaultPaths: [],
      createOperationId: () => "gen",
    });
    assert.equal(r2.actions[0].kind, "adopt");
    assert.deepEqual(r2.actions[0].expectedLocal, { kind: "hash", hash: "same" });
  } finally { await cleanup(); }
});

test("conflict when local and server deleted diverge and when baseline absent with both sides", async () => {
  const { plan, cleanup } = await loadPlanner();
  try {
    const baseline = new Map([["c.md", makeBaseline({ serverRevision: 1, serverHash: "base", localHash: "base" })]]);
    const localByPath = new Map([["c.md", makeLocal("c.md", "local-h", 20)]]);
    const remoteDeleted = new Map([["c.md", makeRemote("c.md", { hash: "base", revision: 2, deleted: true })]]);
    const r = plan({
      forceFull: false,
      recoverySnapshot: false,
      remote: remoteDeleted,
      baseline,
      pending: [],
      localByPath,
      conflicts: new Set(),
      vaultPaths: [],
      createOperationId: () => "gen",
    });
    assert.equal(r.actions.length, 1);
    assert.equal(r.actions[0].kind, "conflict");
    assert.equal(r.actions[0].remote.deleted, true);
    assert.deepEqual(r.actions[0].expectedLocal, { kind: "hash", hash: "local-h" });
    assert.ok(!("bytes" in r.actions[0].local));

    // baseline absent with local and deleted remote => conflict
    const baselineEmpty = new Map();
    const local2 = new Map([["x.md", makeLocal("x.md", "lh2")]]);
    const remoteDel2 = new Map([["x.md", makeRemote("x.md", { hash: "rh", revision: 1, deleted: true })]]);
    const r2 = plan({
      forceFull: false,
      recoverySnapshot: false,
      remote: remoteDel2,
      baseline: baselineEmpty,
      pending: [],
      localByPath: local2,
      conflicts: new Set(),
      vaultPaths: [],
      createOperationId: () => "gen",
    });
    assert.equal(r2.actions[0].kind, "conflict");
    assert.deepEqual(r2.actions[0].expectedLocal, { kind: "hash", hash: "lh2" });
  } finally { await cleanup(); }
});

test("recoverySnapshot compacted handling: delete_local_absent, conflict, upload, and obsolete", async () => {
  const { plan, cleanup } = await loadPlanner();
  try {
    const base = makeBaseline({ serverRevision: 4, serverHash: "same", localHash: "same", serverDeleted: false });
    const baseline = new Map([["a.md", base]]);
    // unchanged local -> delete_local_absent
    const localSame = new Map([["a.md", makeLocal("a.md", "same")]]);
    const r1 = plan({
      forceFull: true,
      recoverySnapshot: true,
      remote: new Map(),
      baseline,
      pending: [],
      localByPath: localSame,
      conflicts: new Set(),
      vaultPaths: ["a.md"],
      createOperationId: () => "gen",
    });
    assert.equal(r1.actions.length, 1);
    assert.deepEqual(r1.actions[0], { kind: "delete_local_absent", path: "a.md", expectedLocal: { kind: "hash", hash: "same" } });
    assert.deepEqual(r1.removedBaselinePaths, []);

    // changed local -> conflict with compacted delete
    const localChanged = new Map([["a.md", makeLocal("a.md", "changed")]]);
    const r2 = plan({
      forceFull: true,
      recoverySnapshot: true,
      remote: new Map(),
      baseline,
      pending: [],
      localByPath: localChanged,
      conflicts: new Set(),
      vaultPaths: ["a.md"],
      createOperationId: () => "gen",
    });
    assert.equal(r2.actions[0].kind, "conflict");
    assert.equal(r2.actions[0].remote.deleted, true);
    assert.equal(r2.actions[0].remote.revision, 0);
    assert.deepEqual(r2.actions[0].expectedLocal, { kind: "hash", hash: "changed" });

    // tombstone with local -> upload
    const tomb = makeBaseline({ serverRevision: 2, serverHash: "h", serverDeleted: true, localHash: "", localMTime: 0, localSize: 0 });
    const baselineTomb = new Map([["b.md", tomb]]);
    const localExists = new Map([["b.md", makeLocal("b.md", "new")]]);
    const r3 = plan({
      forceFull: true,
      recoverySnapshot: true,
      remote: new Map(),
      baseline: baselineTomb,
      pending: [],
      localByPath: localExists,
      conflicts: new Set(),
      vaultPaths: ["b.md"],
      createOperationId: () => "gen-up",
    });
    assert.equal(r3.actions[0].kind, "upload");
    assert.equal(r3.actions[0].baseRevision, 0);
    assert.deepEqual(r3.removedBaselinePaths, ["b.md"]);

    // tombstone without local and with pending delete -> obsolete
    const localAbsent = new Map([["b.md", null]]);
    const pendingDel = [{ id: "pend-del", kind: "delete", path: "b.md", createdAt: 1 }];
    const r4 = plan({
      forceFull: true,
      recoverySnapshot: true,
      remote: new Map(),
      baseline: baselineTomb,
      pending: pendingDel,
      localByPath: localAbsent,
      conflicts: new Set(),
      vaultPaths: [],
      createOperationId: () => "gen",
    });
    assert.equal(r4.actions.length, 0);
    assert.deepEqual(r4.obsoletePendingIds, ["pend-del"]);
    assert.deepEqual(r4.removedBaselinePaths, ["b.md"]);

    // live baseline absent locally -> removed, no action
    const liveBase = makeBaseline({ serverRevision: 1, serverHash: "h", serverDeleted: false });
    const baselineLive = new Map([["c.md", liveBase]]);
    const r5 = plan({
      forceFull: true,
      recoverySnapshot: true,
      remote: new Map(),
      baseline: baselineLive,
      pending: [],
      localByPath: new Map([["c.md", null]]),
      conflicts: new Set(),
      vaultPaths: ["c.md"],
      createOperationId: () => "gen",
    });
    assert.equal(r5.actions.length, 0);
    assert.deepEqual(r5.removedBaselinePaths, ["c.md"]);
  } finally { await cleanup(); }
});

test("rename and conflict exclusions produce no actions and no side effects", async () => {
  const { plan, cleanup } = await loadPlanner();
  try {
    const baseline = new Map([["a.md", makeBaseline()]]);
    const localByPath = new Map([["a.md", makeLocal("a.md", "new")]]);
    const remote = new Map([["a.md", makeRemote("a.md", { hash: "rh", revision: 2 })]]);
    const pendingRename = [{ id: "ren-1", kind: "rename", path: "b.md", oldPath: "a.md", createdAt: 1 }];
    const r1 = plan({
      forceFull: false,
      recoverySnapshot: false,
      remote,
      baseline,
      pending: pendingRename,
      localByPath,
      conflicts: new Set(),
      vaultPaths: [],
      createOperationId: () => "gen",
    });
    assert.equal(r1.actions.length, 0);
    assert.deepEqual(r1.obsoletePendingIds, []);
    // target also excluded
    const localByPath2 = new Map([["b.md", makeLocal("b.md", "x")]]);
    const r1b = plan({
      forceFull: false,
      recoverySnapshot: false,
      remote: new Map([["b.md", makeRemote("b.md")]]),
      baseline: new Map(),
      pending: pendingRename,
      localByPath: localByPath2,
      conflicts: new Set(),
      vaultPaths: [],
      createOperationId: () => "gen",
    });
    assert.equal(r1b.actions.length, 0);

    const r2 = plan({
      forceFull: false,
      recoverySnapshot: false,
      remote,
      baseline,
      pending: [],
      localByPath,
      conflicts: new Set(["a.md"]),
      vaultPaths: [],
      createOperationId: () => "gen",
    });
    assert.equal(r2.actions.length, 0);
  } finally { await cleanup(); }
});

test("obsolete pending ids and removed paths are explicit and inputs not mutated", async () => {
  const { plan, cleanup } = await loadPlanner();
  try {
    const baseline = new Map([["keep.md", makeBaseline({ serverRevision: 1, serverHash: "h", localHash: "h" })]]);
    const localByPath = new Map([["keep.md", makeLocal("keep.md", "h")]]);
    const remote = new Map([["keep.md", makeRemote("keep.md", { hash: "h", revision: 1 })]]);
    const pending = [{ id: "up-keep", kind: "upsert", path: "keep.md", createdAt: 1 }];
    const pendingCopy = JSON.parse(JSON.stringify(pending));
    const baselineCopy = new Map(baseline);
    const result = plan({
      forceFull: false,
      recoverySnapshot: false,
      remote,
      baseline,
      pending,
      localByPath,
      conflicts: new Set(),
      vaultPaths: [],
      createOperationId: () => "gen",
    });
    // both unchanged -> obsolete pending
    assert.deepEqual(result.actions, []);
    assert.deepEqual(result.obsoletePendingIds, ["up-keep"]);
    // inputs unchanged
    assert.deepEqual(pending, pendingCopy);
    assert.equal(baseline.size, baselineCopy.size);
    assert.deepEqual([...baseline.entries()], [...baselineCopy.entries()]);

    // orphan pending delete without server tombstone -> obsolete
    const baselineEmpty = new Map();
    const localEmpty = new Map([["orphan.md", null]]);
    const pendingOrphan = [{ id: "orphan-del", kind: "delete", path: "orphan.md", createdAt: 1 }];
    const r2 = plan({
      forceFull: false,
      recoverySnapshot: false,
      remote: new Map(),
      baseline: baselineEmpty,
      pending: pendingOrphan,
      localByPath: localEmpty,
      conflicts: new Set(),
      vaultPaths: [],
      createOperationId: () => "gen",
    });
    assert.equal(r2.actions.length, 0);
    assert.deepEqual(r2.obsoletePendingIds, ["orphan-del"]);
  } finally { await cleanup(); }
});

test("LocalMeta never carries bytes and factory not called when operation present", async () => {
  const { plan, cleanup } = await loadPlanner();
  try {
    const baseline = new Map();
    const localByPath = new Map([["x.md", makeLocal("x.md", "h")]]);
    const remote = new Map();
    let called = false;
    const result = plan({
      forceFull: true,
      recoverySnapshot: false,
      remote,
      baseline,
      pending: [{ id: "op-x", kind: "upsert", path: "x.md", createdAt: 1 }],
      localByPath,
      conflicts: new Set(),
      vaultPaths: ["x.md"],
      createOperationId: () => { called = true; return "gen"; },
    });
    assert.equal(called, false);
    assert.equal(result.actions[0].operationID, "op-x");
    for (const a of result.actions) {
      if ("local" in a && a.local !== null) assert.equal("bytes" in a.local, false);
    }
  } finally { await cleanup(); }
});
