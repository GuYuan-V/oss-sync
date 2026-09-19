import assert from "node:assert/strict";
import test from "node:test";
import { loadSyncEngine } from "./helpers/sync-engine-loader.mjs";

function upload(path) {
  return {
    kind: "upload",
    path,
    local: { path, hash: "hash", mtime: 1, size: 1 },
    baseRevision: 0,
    operationID: `operation-${path}`,
  };
}

function result(kind = "resolved") {
  return { ok: true, result: { kind } };
}

test("sync engine emits one upload summary above the batch threshold", async () => {
  const { SyncEngine, cleanup } = await loadSyncEngine();
  globalThis.__ossNotices = [];
  try {
    const plugin = {
      settings: { syncIntervalSec: 3, remotePollIntervalSec: 30 },
      t: (key, params = {}) => `${key}:${params.count ?? params.path ?? ""}`,
    };
    const engine = new SyncEngine({ vault: {} }, {}, {}, plugin);
    const actions = ["a.md", "b.md", "c.md", "d.md", "e.md", "f.md"].map(upload);

    engine.reportTransferNotifications(actions, actions.map(() => result()));

    assert.deepEqual(globalThis.__ossNotices, ["sync.uploadedMany:6"]);
  } finally {
    delete globalThis.__ossNotices;
    await cleanup();
  }
});

test("sync engine emits failed paths individually without a failure summary notice", async () => {
  const { SyncEngine, cleanup } = await loadSyncEngine();
  globalThis.__ossNotices = [];
  try {
    const plugin = {
      settings: { syncIntervalSec: 3, remotePollIntervalSec: 30 },
      t: (key, params = {}) => `${key}:${params.count ?? params.path ?? ""}`,
    };
    const engine = new SyncEngine({ vault: {} }, {}, {}, plugin);
    const actions = ["a.md", "b.md", "c.md", "d.md", "broken.md", "conflict.md"].map(upload);
    const results = actions.map((_, index) => result(index >= 4 ? "deferred_retry" : "resolved"));

    engine.reportTransferNotifications(actions, results);

    assert.deepEqual(globalThis.__ossNotices, [
      "sync.uploadedMany:4",
      "notice.syncFileFailed:broken.md",
      "notice.syncFileFailed:conflict.md",
    ]);
    assert.equal(globalThis.__ossNotices.some((notice) => notice.startsWith("notice.syncFailures")), false);
  } finally {
    delete globalThis.__ossNotices;
    await cleanup();
  }
});
