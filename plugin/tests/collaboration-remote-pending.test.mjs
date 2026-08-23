import assert from "node:assert/strict";
import test from "node:test";
import { FIXED_NOW, createDeferred, loadSyncModules, buildBaselineStore, createFakeVault, installWindowFake, makeCollabEntry, makeBaseline, createSync } from "./helpers/collaboration-file-sync-loader.mjs";
import { loadRemoteModules, createRemoteSync, sha256Hex } from "./helpers/collaboration-remote-loader.mjs";
test("local-only refresh persists pending before upload", async () => {
  globalThis.__ossNotices = [];
  const win = installWindowFake();
  const { mod: remoteMod, cleanup: rc } = await loadRemoteModules();
  const { mod: syncMod, cleanup: sc } = await loadSyncModules();
  const { BaselineStore, cleanup: bc } = await buildBaselineStore();
  try {
    const baseText = "base"; const baseHash = await sha256Hex(baseText);
    const localText = "local edit"; const localHash = await sha256Hex(localText);
    const entry = makeCollabEntry({ baseText, serverRevision: 5, serverHash: baseHash, localHash: baseHash });
    const baseline = await makeBaseline(BaselineStore, entry);
    let saveAtUpload = null; let saveCalls = 0; const origSave = baseline.save.bind(baseline);
    baseline.save = async () => { saveCalls++; await origSave(); };
    const { app } = createFakeVault(new Map([["协作oss/owner/Shared.md", localText]]));
    const vault = new syncMod.CollaborationFileVault({ app });
    const d = createDeferred();
    const api = {
      downloadCollabContent: async () => ({ content: new TextEncoder().encode(localText).buffer, meta: { path: "", type: "markdown", hash: baseHash, size: 9, mtime: 1, revision: 5, deleted: false } }),
      collabUpload: async (_v, _f, input) => { saveAtUpload = saveCalls; d.resolve(input); return { path: "", type: "markdown", hash: localHash, size: 9, mtime: 1, revision: 6, deleted: false }; },
    };
    const remote = createRemoteSync(remoteMod, { baseline, vault, api });
    const p = remote.refresh(); await d.promise; await p;
    assert.equal(saveAtUpload, 1);
    const out = baseline.getCollaboration("vault-1", 42);
    assert.equal(out.serverRevision, 6); assert.equal(out.pending, null);
  } finally { win.restore(); await rc(); await sc(); await bc(); }
});
test("startup pending retry reuses operation ID/base revision", async () => {
  globalThis.__ossNotices = [];
  const win = installWindowFake();
  const { mod, cleanup } = await loadRemoteModules();
  const { mod: vm, cleanup: vc } = await loadSyncModules();
  const { BaselineStore, cleanup: bc } = await buildBaselineStore();
  try {
    const baseText = "base"; const baseHash = await sha256Hex(baseText);
    const localText = "pending same"; const localHash = await sha256Hex(localText);
    const pendingId = "op-pending-1";
    const entry = makeCollabEntry({ baseText, serverRevision: 7, serverHash: baseHash, localHash, pending: { id: pendingId, createdAt: FIXED_NOW } });
    const baseline = await makeBaseline(BaselineStore, entry);
    const { app } = createFakeVault(new Map([["协作oss/owner/Shared.md", localText]]));
    const vault = new vm.CollaborationFileVault({ app });
    let captured = null; const d = createDeferred();
    const api = { downloadCollabContent: async () => null, collabUpload: async (_v, _f, input) => { captured = { ...input }; d.resolve(); return { path: "", type: "markdown", hash: localHash, size: 1, mtime: 1, revision: 8, deleted: false }; } };
    const remote = createRemoteSync(mod, { baseline, vault, api });
    const p = remote.refresh(); await d.promise; await p;
    assert.equal(captured.operationID, pendingId); assert.equal(captured.baseRevision, 7); assert.equal(captured.content, localText);
    const out = baseline.getCollaboration("vault-1", 42); assert.equal(out.pending, null); assert.equal(out.serverRevision, 8);
  } finally { win.restore(); await cleanup(); await vc(); await bc(); }
});
test("changed pending bytes persist a new ID before upload", async () => {
  globalThis.__ossNotices = [];
  const win = installWindowFake();
  const { mod, cleanup } = await loadRemoteModules();
  const { mod: vm, cleanup: vc } = await loadSyncModules();
  const { BaselineStore, cleanup: bc } = await buildBaselineStore();
  try {
    const baseText = "base"; const baseHash = await sha256Hex(baseText);
    const oldLocal = "old pending"; const oldHash = await sha256Hex(oldLocal);
    const newLocal = "new pending edit"; const newHash = await sha256Hex(newLocal);
    const pendingId = "op-old";
    const entry = makeCollabEntry({ baseText, serverRevision: 7, serverHash: baseHash, localHash: oldHash, pending: { id: pendingId, createdAt: FIXED_NOW } });
    const baseline = await makeBaseline(BaselineStore, entry);
    const { app } = createFakeVault(new Map([["协作oss/owner/Shared.md", newLocal]]));
    const vault = new vm.CollaborationFileVault({ app });
    let saveCalls = 0; let saveAtUpload = null; const origSave = baseline.save.bind(baseline);
    baseline.save = async () => { saveCalls++; await origSave(); };
    let capturedId = null; const d = createDeferred();
    const api = { downloadCollabContent: async () => null, collabUpload: async (_v, _f, input) => { capturedId = input.operationID; saveAtUpload = saveCalls; d.resolve(); return { path: "", type: "markdown", hash: newHash, size: 1, mtime: 1, revision: 8, deleted: false }; } };
    const remote = createRemoteSync(mod, { baseline, vault, api, createOperationID: () => "op-new-123" });
    const p = remote.refresh(); await d.promise; await p;
    assert.notEqual(capturedId, pendingId); assert.equal(capturedId, "op-new-123"); assert.equal(saveAtUpload, 1);
    const mid = baseline.getCollaboration("vault-1", 42); assert.equal(mid.serverRevision, 8);
  } finally { win.restore(); await cleanup(); await vc(); await bc(); }
});
test("failed retry retains pending", async () => {
  globalThis.__ossNotices = [];
  const win = installWindowFake();
  const { mod, cleanup } = await loadRemoteModules();
  const { mod: vm, cleanup: vc } = await loadSyncModules();
  const { BaselineStore, cleanup: bc } = await buildBaselineStore();
  try {
    const baseText = "base"; const baseHash = await sha256Hex(baseText);
    const localText = "retry fail"; const localHash = await sha256Hex(localText);
    const pendingId = "op-pending-1";
    const entry = makeCollabEntry({ baseText, serverRevision: 7, serverHash: baseHash, localHash, pending: { id: pendingId, createdAt: FIXED_NOW } });
    const baseline = await makeBaseline(BaselineStore, entry);
    const { app } = createFakeVault(new Map([["协作oss/owner/Shared.md", localText]]));
    const vault = new vm.CollaborationFileVault({ app });
    const d = createDeferred();
    const api = { downloadCollabContent: async () => null, collabUpload: async () => { d.resolve(); throw new Error("network"); } };
    const remote = createRemoteSync(mod, { baseline, vault, api });
    const p = remote.refresh(); await d.promise; await p;
    const out = baseline.getCollaboration("vault-1", 42); assert.notEqual(out.pending, null); assert.equal(out.pending.id, pendingId); assert.equal(out.baseText, baseText);
  } finally { win.restore(); await cleanup(); await vc(); await bc(); }
});
