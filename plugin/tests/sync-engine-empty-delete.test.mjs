import assert from "node:assert/strict";
import test from "node:test";
import { loadSyncEngine } from "./helpers/sync-engine-loader.mjs";

const EMPTY_SHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855";

function plugin() {
  return {
    settings: {
      syncPoisonObsidianFiles: false,
      syncIntervalSec: 3,
      remotePollIntervalSec: 30,
    },
  };
}

function emptyFileVault(path) {
  const file = {
    __tfile: true,
    path,
    content: new ArrayBuffer(0),
    stat: { mtime: 20, size: 0 },
  };
  return {
    file,
    vault: {
      getAbstractFileByPath(candidate) {
        return candidate === path ? file : null;
      },
      async readBinary(target) {
        return target.content;
      },
    },
  };
}

test("uploads a zero-byte local file instead of treating it as deletion", async () => {
  const { SyncEngine, cleanup } = await loadSyncEngine();
  try {
    const path = "Notes/Empty.md";
    const { vault } = emptyFileVault(path);
    const acknowledged = {
      serverRevision: 4,
      serverHash: "server-content-hash",
      serverDeleted: false,
      localHash: "server-content-hash",
      localMTime: 10,
      localSize: 12,
    };
    const baseline = {
      get: (candidate) => (candidate === path ? acknowledged : null),
      getConflict: () => null,
      paths: () => [path],
    };
    const pending = {
      id: "empty-file-modified",
      kind: "upsert",
      path,
      createdAt: 1,
    };
    const engine = new SyncEngine({ vault }, {}, baseline, plugin());

    const actions = await engine.planActions(false, new Map(), [pending]);

    assert.equal(actions.length, 1);
    assert.equal(actions[0].kind, "upload");
    assert.equal(actions[0].local.size, 0);
    assert.equal(actions[0].local.hash, EMPTY_SHA256);
    assert.equal(actions[0].baseRevision, 4);
  } finally {
    await cleanup();
  }
});

test("conflicts on first sync when an empty local file meets non-empty server content", async () => {
  const { SyncEngine, cleanup } = await loadSyncEngine();
  try {
    const path = "Notes/Empty.md";
    const { vault } = emptyFileVault(path);
    const baseline = {
      get: () => null,
      getConflict: () => null,
    };
    const remote = new Map([
      [
        path,
        {
          path,
          type: "markdown",
          hash: "server-content-hash",
          size: 12,
          mtime: 10,
          revision: 4,
          deleted: false,
        },
      ],
    ]);
    const engine = new SyncEngine({ vault }, {}, baseline, plugin());

    const actions = await engine.planActions(false, remote, []);

    assert.equal(actions.length, 1);
    assert.equal(actions[0].kind, "conflict");
    assert.equal(actions[0].local.size, 0);
    assert.equal(actions[0].local.hash, EMPTY_SHA256);
  } finally {
    await cleanup();
  }
});

test("adopts a remote tombstone without downloading a deleted file", async () => {
  const { SyncEngine, cleanup } = await loadSyncEngine();
  try {
    const path = "Notes/Deleted.md";
    const baseline = {
      get: () => null,
      getConflict: () => null,
    };
    const vault = { getAbstractFileByPath: () => null };
    const remote = new Map([
      [
        path,
        {
          path,
          type: "markdown",
          hash: "",
          size: 0,
          mtime: 20,
          revision: 5,
          deleted: true,
        },
      ],
    ]);
    const engine = new SyncEngine({ vault }, {}, baseline, plugin());

    const actions = await engine.planActions(false, remote, []);

    assert.equal(actions.length, 1);
    assert.equal(actions[0].kind, "adopt");
    assert.equal(actions[0].local, null);
  } finally {
    await cleanup();
  }
});
