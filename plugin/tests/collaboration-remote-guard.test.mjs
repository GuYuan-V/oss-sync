import assert from "node:assert/strict";
import test from "node:test";
import { createDeferred, loadSyncModules, buildBaselineStore, createFakeVault, installWindowFake, makeCollabEntry, makeBaseline, createSync } from "./helpers/collaboration-file-sync-loader.mjs";
import { loadRemoteModules, createRemoteSync, sha256Hex } from "./helpers/collaboration-remote-loader.mjs";
test("local mutation while download is in-flight/pre-write prevents overwrite", async () => {
  globalThis.__ossNotices = [];
  const win = installWindowFake();
  const { mod, cleanup } = await loadRemoteModules();
  const { mod: vm, cleanup: vc } = await loadSyncModules();
  const { BaselineStore, cleanup: bc } = await buildBaselineStore();
  try {
    const baseText = "base"; const baseHash = await sha256Hex(baseText);
    const remoteText = "remote new"; const remoteHash = await sha256Hex(remoteText);
    const remoteBytes = new TextEncoder().encode(remoteText);
    const entry = makeCollabEntry({ baseText, serverRevision: 7, serverHash: baseHash, localHash: baseHash });
    const baseline = await makeBaseline(BaselineStore, entry);
    const { app, store, modified } = createFakeVault(new Map([["协作oss/owner/Shared.md", baseText]]));
    const vault = new vm.CollaborationFileVault({ app });
    const freshReadStarted = createDeferred();
    const releaseFreshRead = createDeferred();
    const readExact = vault.readExact.bind(vault);
    let readCount = 0;
    vault.readExact = async (path) => {
      readCount++;
      if (readCount === 2) {
        freshReadStarted.resolve();
        await releaseFreshRead.promise;
      }
      return readExact(path);
    };
    const api = { downloadCollabContent: async () => ({ content: remoteBytes.buffer.slice(0), meta: { path: "", type: "markdown", hash: remoteHash, size: remoteBytes.byteLength, mtime: 1, revision: 8, deleted: false } }), collabUpload: async () => { throw new Error("unused"); } };
    const remote = createRemoteSync(mod, { baseline, vault, api });
    const refreshPromise = remote.refresh();
    await freshReadStarted.promise;
    store.set("协作oss/owner/Shared.md", "mutated while in-flight");
    releaseFreshRead.resolve();
    await refreshPromise;
    assert.equal(modified.length, 0); assert.equal(store.get("协作oss/owner/Shared.md"), "mutated while in-flight");
    const out = baseline.getCollaboration("vault-1", 42); assert.equal(out.serverRevision, 7);
  } finally { win.restore(); await cleanup(); await vc(); await bc(); }
});
test("shared coordinator prevents remote refresh from entering while local upload is in-flight", async () => {
  globalThis.__ossNotices = [];
  const win = installWindowFake();
  const { mod: syncMod, cleanup: sc } = await loadSyncModules();
  const { mod: remoteMod, cleanup: rc } = await loadRemoteModules();
  const { BaselineStore, cleanup: bc } = await buildBaselineStore();
  try {
    const baseText = "base"; const baseHash = await sha256Hex(baseText);
    const localText = "local edit for coordinator"; const localHash = await sha256Hex(localText);
    const remoteText = "remote later"; const remoteHash = await sha256Hex(remoteText);
    const remoteBytes = new TextEncoder().encode(remoteText);
    const entry = makeCollabEntry({ baseText, serverRevision: 7, serverHash: baseHash, localHash: baseHash });
    const baseline = await makeBaseline(BaselineStore, entry);
    const { app } = createFakeVault(new Map([["协作oss/owner/Shared.md", localText]]));
    const vaultSync = new syncMod.CollaborationFileVault({ app });
    const vaultRemote = new syncMod.CollaborationFileVault({ app });
    const uploadStarted = createDeferred();
    const uploadDeferred = createDeferred();
    const downloadStarted = createDeferred();
    const downloadDeferred = createDeferred();
    let downloadCalls = 0;
    const apiForSync = {
      collabUpload: async () => { uploadStarted.resolve(); await uploadDeferred.promise; return { path: "", type: "markdown", hash: localHash, size: 1, mtime: 1, revision: 8, deleted: false }; },
      downloadCollabContent: async () => { downloadCalls++; downloadStarted.resolve(); await downloadDeferred.promise; return { content: remoteBytes.buffer.slice(0), meta: { path: "", type: "markdown", hash: remoteHash, size: remoteBytes.byteLength, mtime: 1, revision: 9, deleted: false } }; },
    };
    const coordinator = new syncMod.CollaborationSyncCoordinator();
    const fileSync = createSync(syncMod, { baseline, vault: vaultSync, api: apiForSync, coordinator });
    const remote = createRemoteSync(remoteMod, { baseline, vault: vaultRemote, api: apiForSync, coordinator });
    fileSync.handleLocalEdit("协作oss/owner/Shared.md");
    win.flush();
    await uploadStarted.promise;
    const remotePromise = remote.refresh();
    assert.equal(downloadCalls, 0);
    uploadDeferred.resolve();
    await downloadStarted.promise;
    assert.equal(downloadCalls, 1);
    downloadDeferred.resolve();
    await remotePromise;
    const out = baseline.getCollaboration("vault-1", 42); assert.equal(out.serverRevision, 9);
  } finally { win.restore(); await sc(); await rc(); await bc(); }
});

test("invalid UTF-8 local bytes remain present and are not overwritten", async () => {
  globalThis.__ossNotices = [];
  const { mod, cleanup } = await loadRemoteModules();
  const { mod: vm, cleanup: vc } = await loadSyncModules();
  const { BaselineStore, cleanup: bc } = await buildBaselineStore();
  try {
    const path = "协作oss/owner/Shared.md";
    const invalid = new Uint8Array([0xff, 0xfe, 0xfd]);
    const remoteText = "remote valid";
    const remoteBytes = new TextEncoder().encode(remoteText);
    const remoteHash = await sha256Hex(remoteText);
    const baseline = await makeBaseline(BaselineStore, null);
    const { app, store, created, modified } = createFakeVault(new Map([[path, invalid]]));
    const vault = new vm.CollaborationFileVault({ app });
    const api = {
      downloadCollabContent: async () => ({ content: remoteBytes.buffer.slice(0), meta: { path: "", type: "markdown", hash: remoteHash, size: remoteBytes.byteLength, mtime: 1, revision: 2, deleted: false } }),
      collabUpload: async () => { throw new Error("unused"); },
    };

    await createRemoteSync(mod, { baseline, vault, api }).refresh();

    assert.deepEqual([...store.get(path)], [...invalid]);
    assert.equal(created.length, 0);
    assert.equal(modified.length, 0);
    assert.equal(baseline.getCollaboration("vault-1", 42), null);
  } finally { await cleanup(); await vc(); await bc(); }
});
