import assert from "node:assert/strict";
import test from "node:test";
import {
  FIXED_NOW,
  FIXED_ID,
  createDeferred,
  loadSyncModules,
  buildBaselineStore,
  createFakeVault,
  installWindowFake,
  makeCollabEntry,
  makeBaseline,
  createSync,
} from "./helpers/collaboration-file-sync-loader.mjs";

test("pending is saved before API call with exact {content,baseRevision,operationID}", async () => {
  // Given: synced collaboration entry with ancestor baseText "old".
  const win = installWindowFake();
  globalThis.__ossNotices = [];
  const { mod, cleanup } = await loadSyncModules();
  const { BaselineStore, cleanup: bc } = await buildBaselineStore();
  try {
    const { app } = createFakeVault(new Map([["协作oss/owner/Shared.md", "hello world"]]));
    const entry = makeCollabEntry({ baseText: "old", serverRevision: 7, serverHash: "oldhash" });
    const baseline = await makeBaseline(BaselineStore, entry);
    const vault = new mod.CollaborationFileVault({ app });
    let saveCalls = 0;
    const origSave = baseline.save.bind(baseline);
    baseline.save = async () => { saveCalls++; await origSave(); };
    let saveCallsAtUpload = null;
    const uploads = [];
    const uploadCalled = createDeferred();
    const api = {
      collabUpload: async (vaultId, fileId, input) => {
        saveCallsAtUpload = saveCalls;
        uploads.push({ vaultId, fileId, input: { ...input } });
        uploadCalled.resolve();
        return { path: "Shared.md", type: "markdown", hash: "newhash", size: 11, mtime: 1, revision: 8, deleted: false };
      },
    };
    const sync = createSync(mod, { baseline, vault, api, now: () => FIXED_NOW, createOperationID: () => FIXED_ID });

    // When: local file is edited and debounced upload flushes.
    sync.handleLocalEdit("协作oss/owner/Shared.md");
    assert.equal(win.timers.size, 1);
    win.flush();
    await uploadCalled.promise;

    // Then: upload uses exact CAS payload and pending was persisted before the call.
    assert.equal(uploads.length, 1);
    assert.equal(uploads[0].vaultId, "vault-1");
    assert.equal(uploads[0].fileId, 42);
    assert.equal(uploads[0].input.content, "hello world");
    assert.equal(uploads[0].input.baseRevision, 7);
    assert.equal(uploads[0].input.operationID, FIXED_ID);
    assert.equal(saveCallsAtUpload, 1);
    assert.ok(saveCalls >= 1);
    const persisted = baseline.getCollaboration("vault-1", 42);
    assert.equal(persisted.baseText, "old");
  } finally {
    win.restore();
    await cleanup();
    await bc();
  }
});

test("success clears pending and advances baseline to uploaded content", async () => {
  // Given: collaboration entry with ancestor baseText "old".
  const win = installWindowFake();
  globalThis.__ossNotices = [];
  const { mod, cleanup } = await loadSyncModules();
  const { BaselineStore, cleanup: bc } = await buildBaselineStore();
  try {
    const { app } = createFakeVault(new Map([["协作oss/owner/Shared.md", "new content"]]));
    const baseline = await makeBaseline(BaselineStore, makeCollabEntry({ baseText: "old", serverRevision: 7, localHash: "old" }));
    const vault = new mod.CollaborationFileVault({ app });
    const api = { collabUpload: async () => ({ path: "Shared.md", type: "markdown", hash: "hash-new", size: 11, mtime: 123, revision: 8, deleted: false }) };
    let changed = 0;
    const changedCalled = createDeferred();
    const sync = createSync(mod, {
      baseline,
      vault,
      api,
      onChange: () => {
        changed++;
        changedCalled.resolve();
      },
      now: () => FIXED_NOW,
      createOperationID: () => FIXED_ID,
    });

    // When: edit succeeds.
    sync.handleLocalEdit("协作oss/owner/Shared.md");
    win.flush();
    await changedCalled.promise;

    // Then: pending cleared, revision/hash advanced, baseText becomes uploaded content.
    const entry = baseline.getCollaboration("vault-1", 42);
    assert.equal(entry.pending, null);
    assert.equal(entry.serverRevision, 8);
    assert.equal(entry.serverHash, "hash-new");
    assert.equal(entry.baseText, "new content");
    assert.equal(entry.localHash.length > 0, true);
    assert.equal(changed, 1);
  } finally {
    win.restore();
    await cleanup();
    await bc();
  }
});

test("failure keeps pending and preserves ancestor baseText", async () => {
  // Given: entry with ancestor baseText "old".
  const win = installWindowFake();
  globalThis.__ossNotices = [];
  const { mod, cleanup } = await loadSyncModules();
  const { BaselineStore, cleanup: bc } = await buildBaselineStore();
  try {
    const { app } = createFakeVault(new Map([["协作oss/owner/Shared.md", "fail content"]]));
    const baseline = await makeBaseline(BaselineStore, makeCollabEntry({ baseText: "old", serverRevision: 5, serverHash: "h1", localHash: "h1" }));
    const vault = new mod.CollaborationFileVault({ app });
    const uploadCalled = createDeferred();
    const api = { collabUpload: async () => { uploadCalled.resolve(); throw new Error("network"); } };
    const sync = createSync(mod, { baseline, vault, api, now: () => FIXED_NOW, createOperationID: () => FIXED_ID });

    // When: upload fails.
    sync.handleLocalEdit("协作oss/owner/Shared.md");
    win.flush();
    await uploadCalled.promise;

    // Then: pending remains and baseText still ancestor "old" (not overwritten).
    const entry = baseline.getCollaboration("vault-1", 42);
    assert.notEqual(entry.pending, null);
    assert.equal(entry.pending.id, FIXED_ID);
    assert.equal(entry.pending.createdAt, FIXED_NOW);
    assert.equal(entry.baseText, "old");
    assert.equal(entry.serverRevision, 5);
  } finally {
    win.restore();
    await cleanup();
    await bc();
  }
});
