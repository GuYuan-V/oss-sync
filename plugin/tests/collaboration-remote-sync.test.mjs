import assert from "node:assert/strict";
import test from "node:test";
import {
  FIXED_NOW,
  loadSyncModules,
  buildBaselineStore,
  createFakeVault,
  installWindowFake,
  makeCollabEntry,
  makeBaseline,
} from "./helpers/collaboration-file-sync-loader.mjs";
import { loadRemoteModules, createRemoteSync, sha256Hex } from "./helpers/collaboration-remote-loader.mjs";

async function vaultFor(app) {
  const { mod, cleanup } = await loadSyncModules();
  return { vault: new mod.CollaborationFileVault({ app }), cleanup };
}

test("baseline.load then bind username and save", async () => {
  globalThis.__ossNotices = [];
  const { mod, cleanup } = await loadRemoteModules();
  const { app } = createFakeVault(new Map());
  const { vault, cleanup: vc } = await vaultFor(app);
  let loadCalls = 0;
  let bindArg = null;
  let saveCalls = 0;
  const baseline = {
    getCollaboration: () => null,
    setCollaboration: () => {},
    save: async () => { saveCalls++; },
    load: async () => { loadCalls++; },
    bindCollaborationAccount: (a) => { bindArg = a; return true; },
  };
  const api = { downloadCollabContent: async () => null, collabUpload: async () => { throw new Error("unused"); } };
  const remote = createRemoteSync(mod, { baseline, vault, api, getUsername: () => "alice", getAccepted: () => [] });
  await remote.refresh();
  assert.equal(loadCalls, 1);
  assert.equal(bindArg, "alice");
  assert.equal(saveCalls, 1);
  await cleanup();
  await vc();
});

test("no local installs exact remote and persists server revision/hash/baseText", async () => {
  globalThis.__ossNotices = [];
  const win = installWindowFake();
  const { mod, cleanup } = await loadRemoteModules();
  const { BaselineStore, cleanup: bc } = await buildBaselineStore();
  try {
    const text = "# remote content";
    const hash = await sha256Hex(text);
    const bytes = new TextEncoder().encode(text);
    const { app, store, created } = createFakeVault(new Map());
    const baseline = await makeBaseline(BaselineStore, null);
    const { vault, cleanup: vc } = await vaultFor(app);
    let saveCalls = 0;
    const origSave = baseline.save.bind(baseline);
    baseline.save = async () => { saveCalls++; await origSave(); };
    const api = {
      downloadCollabContent: async () => ({ content: bytes.buffer.slice(0), meta: { path: "", type: "markdown", hash, size: bytes.byteLength, mtime: 1, revision: 9, deleted: false } }),
      collabUpload: async () => { throw new Error("unused"); },
    };
    const remote = createRemoteSync(mod, { baseline, vault, api });
    await remote.refresh();
    assert.equal(created.length, 1);
    assert.equal(created[0], "协作oss/owner/Shared.md");
    assert.equal(store.get("协作oss/owner/Shared.md"), text);
    const entry = baseline.getCollaboration("vault-1", 42);
    assert.equal(entry.serverRevision, 9);
    assert.equal(entry.serverHash, hash);
    assert.equal(entry.localHash, hash);
    assert.equal(entry.baseText, text);
    assert.equal(entry.pending, null);
    assert.ok(saveCalls >= 1);
    await vc();
  } finally { win.restore(); await cleanup(); await bc(); }
});

test("identical local adopts without create/modify", async () => {
  globalThis.__ossNotices = [];
  const { mod, cleanup } = await loadRemoteModules();
  const { BaselineStore, cleanup: bc } = await buildBaselineStore();
  try {
    const text = "# same";
    const hash = await sha256Hex(text);
    const bytes = new TextEncoder().encode(text);
    const { app, created, modified } = createFakeVault(new Map([["协作oss/owner/Shared.md", text]]));
    const baseline = await makeBaseline(BaselineStore, null);
    const { vault, cleanup: vc } = await vaultFor(app);
    const api = {
      downloadCollabContent: async () => ({ content: bytes.buffer.slice(0), meta: { path: "", type: "markdown", hash, size: bytes.byteLength, mtime: 1, revision: 5, deleted: false } }),
      collabUpload: async () => { throw new Error("unused"); },
    };
    const remote = createRemoteSync(mod, { baseline, vault, api });
    await remote.refresh();
    assert.equal(created.length, 0);
    assert.equal(modified.length, 0);
    const entry = baseline.getCollaboration("vault-1", 42);
    assert.equal(entry.serverRevision, 5);
    assert.equal(entry.serverHash, hash);
    assert.equal(entry.localHash, hash);
    assert.equal(entry.baseText, text);
    await vc();
  } finally { await cleanup(); await bc(); }
});

test("differing local with no baseline is untouched and baseline absent", async () => {
  globalThis.__ossNotices = [];
  const { mod, cleanup } = await loadRemoteModules();
  const { BaselineStore, cleanup: bc } = await buildBaselineStore();
  try {
    const remoteText = "# remote";
    const remoteHash = await sha256Hex(remoteText);
    const remoteBytes = new TextEncoder().encode(remoteText);
    const localText = "# local different";
    const { app, store, created, modified } = createFakeVault(new Map([["协作oss/owner/Shared.md", localText]]));
    const baseline = await makeBaseline(BaselineStore, null);
    const { vault, cleanup: vc } = await vaultFor(app);
    const api = {
      downloadCollabContent: async () => ({ content: remoteBytes.buffer.slice(0), meta: { path: "", type: "markdown", hash: remoteHash, size: remoteBytes.byteLength, mtime: 1, revision: 9, deleted: false } }),
      collabUpload: async () => { throw new Error("unused"); },
    };
    const remote = createRemoteSync(mod, { baseline, vault, api });
    await remote.refresh();
    assert.equal(created.length, 0);
    assert.equal(modified.length, 0);
    assert.equal(store.get("协作oss/owner/Shared.md"), localText);
    assert.equal(baseline.getCollaboration("vault-1", 42), null);
    await vc();
  } finally { await cleanup(); await bc(); }
});

test("remote-only change writes remote and advances state", async () => {
  globalThis.__ossNotices = [];
  const win = installWindowFake();
  const { mod, cleanup } = await loadRemoteModules();
  const { BaselineStore, cleanup: bc } = await buildBaselineStore();
  try {
    const baseText = "old content";
    const baseHash = await sha256Hex(baseText);
    const remoteText = "new remote";
    const remoteHash = await sha256Hex(remoteText);
    const remoteBytes = new TextEncoder().encode(remoteText);
    const { app, store, modified } = createFakeVault(new Map([["协作oss/owner/Shared.md", baseText]]));
    const entry = makeCollabEntry({ baseText, serverRevision: 7, serverHash: baseHash, localHash: baseHash });
    const baseline = await makeBaseline(BaselineStore, entry);
    const { vault, cleanup: vc } = await vaultFor(app);
    const api = {
      downloadCollabContent: async () => ({ content: remoteBytes.buffer.slice(0), meta: { path: "", type: "markdown", hash: remoteHash, size: remoteBytes.byteLength, mtime: 1, revision: 8, deleted: false } }),
      collabUpload: async () => { throw new Error("unused"); },
    };
    const remote = createRemoteSync(mod, { baseline, vault, api });
    await remote.refresh();
    assert.equal(modified.length, 1);
    assert.equal(store.get("协作oss/owner/Shared.md"), remoteText);
    const out = baseline.getCollaboration("vault-1", 42);
    assert.equal(out.serverRevision, 8);
    assert.equal(out.serverHash, remoteHash);
    assert.equal(out.localHash, remoteHash);
    assert.equal(out.baseText, remoteText);
    await vc();
  } finally { win.restore(); await cleanup(); await bc(); }
});

test("pending and conflict block canonical overwrite", async () => {
  globalThis.__ossNotices = [];
  const win = installWindowFake();
  const { mod, cleanup } = await loadRemoteModules();
  const { BaselineStore, cleanup: bc } = await buildBaselineStore();
  try {
    const baseText = "base";
    const baseHash = await sha256Hex(baseText);
    const remoteText = "remote new";
    const remoteHash = await sha256Hex(remoteText);
    const remoteBytes = new TextEncoder().encode(remoteText);
    const { app, store, modified } = createFakeVault(new Map([["协作oss/owner/Shared.md", baseText]]));
    const pendingEntry = makeCollabEntry({ baseText, serverRevision: 7, serverHash: baseHash, localHash: baseHash, pending: { id: "op-1", createdAt: FIXED_NOW } });
    const baseline = await makeBaseline(BaselineStore, pendingEntry);
    const { vault, cleanup: vc } = await vaultFor(app);
    let uploadCalled = false;
    const api = {
      downloadCollabContent: async () => ({ content: remoteBytes.buffer.slice(0), meta: { path: "", type: "markdown", hash: remoteHash, size: remoteBytes.byteLength, mtime: 1, revision: 8, deleted: false } }),
      collabUpload: async () => { uploadCalled = true; return { path: "", type: "markdown", hash: remoteHash, size: 1, mtime: 1, revision: 8, deleted: false }; },
    };
    const remote = createRemoteSync(mod, { baseline, vault, api });
    await remote.refresh();
    assert.equal(modified.length, 0);
    assert.equal(store.get("协作oss/owner/Shared.md"), baseText);
    uploadCalled = false;
    const conflictEntry = makeCollabEntry({ baseText, serverRevision: 7, serverHash: baseHash, localHash: baseHash, conflict: { remoteRevision: 9, remoteHash, remoteText, detectedAt: FIXED_NOW } });
    baseline.setCollaboration(conflictEntry.vaultId, conflictEntry.fileId, conflictEntry);
    modified.length = 0;
    await remote.refresh();
    assert.equal(modified.length, 0);
    assert.equal(uploadCalled, false);
    await vc();
  } finally { win.restore(); await cleanup(); await bc(); }
});
