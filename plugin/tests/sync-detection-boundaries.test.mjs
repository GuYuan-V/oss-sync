import assert from "node:assert/strict";
import test from "node:test";
import { loadSyncEngine } from "./helpers/sync-engine-loader.mjs";

async function withQueue(run) {
  const { SyncEngine, cleanup } = await loadSyncEngine();
  const pending = [];
  const engine = Object.create(SyncEngine.prototype);
  Object.assign(engine, {
    plugin: { settings: { syncPoisonObsidianFiles: false } },
    suppressed: new Set(),
    enqueueChain: Promise.resolve(),
    baseline: {
      async load() {},
      putPending(operation) { pending.push(operation); },
      async save() {},
    },
    debounceFn() {},
  });
  try {
    await run(engine, pending);
  } finally {
    await cleanup();
  }
}

test("queue records an ordinary directory child's path move without inspecting content", async () => {
  await withQueue(async (engine, pending) => {
    engine.enqueueRename("Notes/Sub/a.md", "Archive/Notes/Sub/a.md");
    await engine.flushPendingQueue();
    assert.equal(pending.length, 1);
    assert.equal(pending[0].kind, "rename");
    assert.equal(pending[0].oldPath, "Notes/Sub/a.md");
    assert.equal(pending[0].path, "Archive/Notes/Sub/a.md");
  });
});

test("queue translates moves across the blacklist boundary to delete and upsert", async () => {
  await withQueue(async (engine, pending) => {
    engine.enqueueRename("Notes/a.md", ".trash/Notes/a.md");
    engine.enqueueRename(".trash/Notes/b.md", "Notes/b.md");
    await engine.flushPendingQueue();
    assert.deepEqual(pending.map(({ kind, path }) => ({ kind, path })), [
      { kind: "delete", path: "Notes/a.md" },
      { kind: "upsert", path: "Notes/b.md" },
    ]);
  });
});

test("queue preserves a real move when only the old path is suppressed", async () => {
  await withQueue(async (engine, pending) => {
    engine.suppressed.add("Notes/a.md");
    engine.enqueueRename("Notes/a.md", "Archive/Notes/a.md");
    await engine.flushPendingQueue();
    assert.equal(pending.length, 1);
    assert.equal(pending[0].kind, "rename");
  });
});

test("directory moves persist as one batch and collaboration moves stay outside ordinary sync", async () => {
  await withQueue(async (engine, pending) => {
    let saves = 0;
    engine.baseline.save = async () => { saves++; };
    engine.enqueueRenames([
      { oldPath: "Notes/a.md", path: "Archive/Notes/a.md" },
      { oldPath: "Notes/b.md", path: "Archive/Notes/b.md" },
      { oldPath: "协作oss/owner/c.md", path: "Notes/c.md" },
    ]);
    await engine.flushPendingQueue();
    assert.equal(saves, 1);
    assert.equal(pending.length, 2);
  });
});
