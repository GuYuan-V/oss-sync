import assert from "node:assert/strict";
import test from "node:test";
import {
  FIXED_NOW,
  createDeferred,
  createSequenceId,
  loadSyncModules,
  buildBaselineStore,
  createFakeVault,
  installWindowFake,
  makeCollabEntry,
  makeBaseline,
  createSync,
} from "./helpers/collaboration-file-sync-loader.mjs";

test("stable operation ID is reused when unchanged", async () => {
  // Given: pending already stored with deterministic ID for same hash.
  const win = installWindowFake();
  globalThis.__ossNotices = [];
  const { mod, cleanup } = await loadSyncModules();
  const { BaselineStore, cleanup: bc } = await buildBaselineStore();
  try {
    const { app } = createFakeVault(new Map([["协作oss/owner/Shared.md", "same bytes"]]));
    const baseline = await makeBaseline(BaselineStore, makeCollabEntry({ baseText: "old", serverRevision: 7 }));
    const vault = new mod.CollaborationFileVault({ app });
    const calls = [];
    const seq = createSequenceId("op-fixed-");
    const firstUpload = createDeferred();
    const secondUpload = createDeferred();
    const api = { collabUpload: async (_v, _f, input) => {
      calls.push(input.operationID);
      (calls.length === 1 ? firstUpload : secondUpload).resolve();
      throw new Error("fail");
    } };
    const sync = createSync(mod, { baseline, vault, api, now: () => FIXED_NOW, createOperationID: seq });

    // When: same content uploaded twice with same revision.
    sync.handleLocalEdit("协作oss/owner/Shared.md");
    win.flush();
    await firstUpload.promise;
    const firstId = calls[0];
    assert.ok(firstId);
    sync.handleLocalEdit("协作oss/owner/Shared.md");
    win.flush();
    await secondUpload.promise;

    // Then: operation ID is stable and reused.
    assert.equal(calls.length, 2);
    assert.equal(calls[1], firstId);
    assert.equal(firstId, "op-fixed-001");
  } finally {
    win.restore();
    await cleanup();
    await bc();
  }
});

test("suppression prevents upload", async () => {
  // Given: file is suppressed (just written via collaboration download).
  const win = installWindowFake();
  globalThis.__ossNotices = [];
  const { mod, cleanup } = await loadSyncModules();
  const { BaselineStore, cleanup: bc } = await buildBaselineStore();
  try {
    const { app } = createFakeVault(new Map([["协作oss/owner/Shared.md", "content"]]));
    const baseline = await makeBaseline(BaselineStore, makeCollabEntry({ serverRevision: 3 }));
    const vault = new mod.CollaborationFileVault({ app });
    vault.suppress("协作oss/owner/Shared.md");
    let called = false;
    const api = { collabUpload: async () => { called = true; return { path: "", type: "markdown", hash: "", size: 0, mtime: 0, revision: 4, deleted: false }; } };
    const sync = createSync(mod, { baseline, vault, api, now: () => FIXED_NOW, createOperationID: () => "op-fixed-001" });

    // When: local edit fires while suppressed.
    sync.handleLocalEdit("协作oss/owner/Shared.md");
    assert.equal(win.timers.size, 1);
    win.flush();

    // Then: no upload occurs while suppressed.
    assert.equal(called, false);
  } finally {
    win.restore();
    await cleanup();
    await bc();
  }
});

test("same-path debounce coalesces", async () => {
  // Given: multiple rapid edits to same path.
  const win = installWindowFake();
  globalThis.__ossNotices = [];
  const { mod, cleanup } = await loadSyncModules();
  const { BaselineStore, cleanup: bc } = await buildBaselineStore();
  try {
    const { app } = createFakeVault(new Map([["协作oss/owner/Shared.md", "coalesce"]]));
    const baseline = await makeBaseline(BaselineStore, makeCollabEntry({ serverRevision: 1 }));
    const vault = new mod.CollaborationFileVault({ app });
    let calls = 0;
    const uploadCalled = createDeferred();
    const api = { collabUpload: async () => {
      calls++;
      uploadCalled.resolve();
      return { path: "", type: "markdown", hash: "h2", size: 1, mtime: 1, revision: 2, deleted: false };
    } };
    const sync = createSync(mod, { baseline, vault, api, now: () => FIXED_NOW, createOperationID: createSequenceId() });

    // When: edit fires three times before debounce expires.
    sync.handleLocalEdit("协作oss/owner/Shared.md");
    sync.handleLocalEdit("协作oss/owner/Shared.md");
    sync.handleLocalEdit("协作oss/owner/Shared.md");
    assert.equal(win.timers.size, 1);
    win.flush();
    await uploadCalled.promise;

    // Then: only one upload.
    assert.equal(calls, 1);
  } finally {
    win.restore();
    await cleanup();
    await bc();
  }
});

test("missing baseline does not upload", async () => {
  // Given: no collaboration baseline entry exists.
  const win = installWindowFake();
  globalThis.__ossNotices = [];
  const { mod, cleanup } = await loadSyncModules();
  const { BaselineStore, cleanup: bc } = await buildBaselineStore();
  try {
    const { app } = createFakeVault(new Map([["协作oss/owner/Shared.md", "content"]]));
    const baseline = await makeBaseline(BaselineStore, null);
    const vault = new mod.CollaborationFileVault({ app });
    let called = false;
    const api = { collabUpload: async () => { called = true; return { path: "", type: "markdown", hash: "", size: 0, mtime: 0, revision: 1, deleted: false }; } };
    const sync = createSync(mod, { baseline, vault, api, now: () => FIXED_NOW, createOperationID: () => "op-fixed-001" });

    // When: edit fires for path without baseline.
    sync.handleLocalEdit("协作oss/owner/Shared.md");
    win.flush();

    // Then: upload not called.
    assert.equal(called, false);
  } finally {
    win.restore();
    await cleanup();
    await bc();
  }
});
