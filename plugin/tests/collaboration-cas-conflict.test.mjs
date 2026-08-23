import assert from "node:assert/strict";
import test from "node:test";
import {
  FIXED_NOW,
  FIXED_ID,
  createDeferred,
  createSequenceId,
  loadSyncModules,
  buildBaselineStore,
  createFakeVault,
  installWindowFake,
  makeCollabEntry,
  makeBaseline,
  createSync,
  createTrackedCoordinator,
  settle,
} from "./helpers/collaboration-file-sync-loader.mjs";
import { loadRemoteModules, createRemoteSync, sha256Hex } from "./helpers/collaboration-remote-loader.mjs";

function oss409(msg = "conflict") {
  const e = new Error(msg);
  e.status = 409;
  e.name = "OSSApiError";
  return e;
}

async function sha(text) {
  return sha256Hex(text);
}

async function shaBytes(bytes) {
  const d = await crypto.subtle.digest("SHA-256", bytes);
  return Array.from(new Uint8Array(d)).map((b) => b.toString(16).padStart(2, "0")).join("");
}

test("file-sync 409 independent edits merge and retry once with latest remote revision", async () => {
  const win = installWindowFake();
  globalThis.__ossNotices = [];
  const { mod, cleanup } = await loadSyncModules();
  const { BaselineStore, cleanup: bc } = await buildBaselineStore();
  try {
    const baseText = "a\nb\nc\nd";
    const baseHash = await sha(baseText);
    const localText = "A\nb\nc\nd";
    const remoteText = "a\nb\nc\nD";
    const mergedText = "A\nb\nc\nD";
    const remoteBytes = new TextEncoder().encode(remoteText);
    const remoteHash = await sha(remoteText);
    const mergedHash = await sha(mergedText);
    const { app, store } = createFakeVault(new Map([["协作oss/owner/Shared.md", localText]]));
    const entry = makeCollabEntry({ baseText, serverRevision: 7, serverHash: baseHash, localHash: baseHash });
    const baseline = await makeBaseline(BaselineStore, entry);
    const vault = new mod.CollaborationFileVault({ app });
    let saveCalls = 0;
    const orig = baseline.save.bind(baseline);
    baseline.save = async () => {
      saveCalls += 1;
      await orig();
    };
    let downloadCalls = 0;
    const uploadInputs = [];
    const seq = createSequenceId("op-fixed-");
    const secondUploadDone = createDeferred();
    const coordinator = createTrackedCoordinator(mod);
    const api = {
      collabUpload: async (_v, _f, input) => {
        uploadInputs.push({ ...input });
        if (uploadInputs.length === 1) {
          assert.equal(input.baseRevision, 7);
          throw oss409();
        }
        assert.equal(input.baseRevision, 8);
        assert.equal(input.content, mergedText);
        assert.notEqual(input.operationID, "op-fixed-001");
        secondUploadDone.resolve();
        return { path: "", type: "markdown", hash: mergedHash, size: 11, mtime: 1, revision: 9, deleted: false };
      },
      downloadCollabContent: async () => {
        downloadCalls += 1;
        return { content: remoteBytes.buffer.slice(0), meta: { path: "", type: "markdown", hash: remoteHash, size: remoteBytes.length, mtime: 1, revision: 8, deleted: false } };
      },
    };
    const sync = createSync(mod, { baseline, vault, api, now: () => FIXED_NOW, createOperationID: seq, coordinator });
    sync.handleLocalEdit("协作oss/owner/Shared.md");
    assert.equal(win.timers.size, 1);
    win.flush();
    await secondUploadDone.promise;
    await coordinator.waitForIdle();
    assert.equal(downloadCalls, 1);
    assert.equal(uploadInputs.length, 2);
    assert.equal(uploadInputs[1].operationID, "op-fixed-002");
    const out = baseline.getCollaboration("vault-1", 42);
    assert.equal(out.pending, null);
    assert.equal(out.conflict, null);
    assert.equal(out.serverRevision, 9);
    assert.equal(out.baseText, mergedText);
    assert.equal(out.localHash, mergedHash);
    assert.equal(store.get("协作oss/owner/Shared.md"), mergedText);
    assert.ok(saveCalls >= 2);
  } finally {
    win.restore();
    await cleanup();
    await bc();
  }
});

test("file-sync 409 overlapping text persists conflict retaining local", async () => {
  const win = installWindowFake();
  globalThis.__ossNotices = [];
  const { mod, cleanup } = await loadSyncModules();
  const { BaselineStore, cleanup: bc } = await buildBaselineStore();
  try {
    const baseText = "a\nb\nc";
    const baseHash = await sha(baseText);
    const localText = "a\nX\nc";
    const remoteText = "a\nY\nc";
    const remoteBytes = new TextEncoder().encode(remoteText);
    const remoteHash = await sha(remoteText);
    const { app, store } = createFakeVault(new Map([["协作oss/owner/Shared.md", localText]]));
    const entry = makeCollabEntry({ baseText, serverRevision: 7, serverHash: baseHash, localHash: baseHash });
    const baseline = await makeBaseline(BaselineStore, entry);
    const vault = new mod.CollaborationFileVault({ app });
    const done = createDeferred();
    const coordinator = createTrackedCoordinator(mod);
    const api = {
      collabUpload: async () => { throw oss409(); },
      downloadCollabContent: async () => {
        const r = { content: remoteBytes.buffer.slice(0), meta: { path: "", type: "markdown", hash: remoteHash, size: remoteBytes.length, mtime: 1, revision: 8, deleted: false } };
        done.resolve();
        return r;
      },
    };
    const sync = createSync(mod, { baseline, vault, api, now: () => FIXED_NOW, createOperationID: () => FIXED_ID, coordinator });
    sync.handleLocalEdit("协作oss/owner/Shared.md");
    win.flush();
    await done.promise;
    await coordinator.waitForIdle();
    const out = baseline.getCollaboration("vault-1", 42);
    assert.equal(out.pending, null);
    assert.ok(out.conflict);
    assert.equal(out.conflict.remoteText, remoteText);
    assert.equal(out.conflict.remoteRevision, 8);
    assert.equal(out.conflict.remoteHash, remoteHash);
    assert.equal(out.baseText, baseText);
    assert.equal(store.get("协作oss/owner/Shared.md"), localText);
    assert.equal(out.serverRevision, 7);
  } finally {
    win.restore();
    await cleanup();
    await bc();
  }
});

test("file-sync 409 binary/unsupported creates conflict sibling with exact bytes", async () => {
  const win = installWindowFake();
  globalThis.__ossNotices = [];
  const { mod, cleanup } = await loadSyncModules();
  const { BaselineStore, cleanup: bc } = await buildBaselineStore();
  try {
    const baseText = "old";
    const baseHash = await sha(baseText);
    const localText = "local png content";
    const localBytes = new TextEncoder().encode(localText);
    const remoteBytes = new Uint8Array([0xaa, 0xbb, 0xcc, 4, 5, 6]);
    const remoteHash = await shaBytes(remoteBytes);
    const localPath = "协作oss/owner/image.png";
    const { app, store } = createFakeVault(new Map([[localPath, localText]]));
    const entry = makeCollabEntry({ localPath, baseText, serverRevision: 5, serverHash: baseHash, localHash: baseHash });
    const baseline = await makeBaseline(BaselineStore, entry);
    const vault = new mod.CollaborationFileVault({ app });
    const done = createDeferred();
    const coordinator = createTrackedCoordinator(mod);
    const api = {
      collabUpload: async () => { throw oss409(); },
      downloadCollabContent: async () => {
        done.resolve();
        return { content: remoteBytes.buffer.slice(0), meta: { path: "", type: "markdown", hash: remoteHash, size: remoteBytes.length, mtime: 1, revision: 6, deleted: false } };
      },
    };
    const sync = createSync(mod, { baseline, vault, api, getAccepted: () => [{ vaultId: "vault-1", fileId: 42, localPath }], now: () => FIXED_NOW, createOperationID: () => FIXED_ID, coordinator });
    sync.handleLocalEdit(localPath);
    win.flush();
    await done.promise;
    await coordinator.waitForIdle();
    const sibling = [...store.keys()].find((k) => k.includes("_conflict_"));
    assert.ok(sibling, "sibling created");
    const sibVal = store.get(sibling);
    const sibBytes = sibVal instanceof Uint8Array ? sibVal : new TextEncoder().encode(sibVal);
    assert.deepEqual(sibBytes, localBytes);
    const canon = store.get(localPath);
    const canonBytes = canon instanceof Uint8Array ? canon : new TextEncoder().encode(canon);
    assert.deepEqual(canonBytes, remoteBytes);
    const out = baseline.getCollaboration("vault-1", 42);
    assert.equal(out.pending, null);
    assert.equal(out.conflict, null);
    assert.equal(out.serverRevision, 6);
    assert.equal(out.serverHash, remoteHash);
    assert.equal(out.localHash, remoteHash);
  } finally {
    win.restore();
    await cleanup();
    await bc();
  }
});

test("file-sync 409 second 409 remains pending bounded retry", async () => {
  const win = installWindowFake();
  globalThis.__ossNotices = [];
  const { mod, cleanup } = await loadSyncModules();
  const { BaselineStore, cleanup: bc } = await buildBaselineStore();
  try {
    const baseText = "a\nb\nc\nd";
    const baseHash = await sha(baseText);
    const localText = "A\nb\nc\nd";
    const remoteText = "a\nb\nc\nD";
    const remoteBytes = new TextEncoder().encode(remoteText);
    const remoteHash = await sha(remoteText);
    const { app } = createFakeVault(new Map([["协作oss/owner/Shared.md", localText]]));
    const entry = makeCollabEntry({ baseText, serverRevision: 7, serverHash: baseHash, localHash: baseHash });
    const baseline = await makeBaseline(BaselineStore, entry);
    const vault = new mod.CollaborationFileVault({ app });
    const done = createDeferred();
    let uploadCount = 0;
    const coordinator = createTrackedCoordinator(mod);
    const api = {
      collabUpload: async () => {
        uploadCount += 1;
        if (uploadCount === 2) done.resolve();
        throw oss409();
      },
      downloadCollabContent: async () => ({ content: remoteBytes.buffer.slice(0), meta: { path: "", type: "markdown", hash: remoteHash, size: remoteBytes.length, mtime: 1, revision: 8, deleted: false } }),
    };
    const sync = createSync(mod, { baseline, vault, api, now: () => FIXED_NOW, createOperationID: createSequenceId(), coordinator });
    sync.handleLocalEdit("协作oss/owner/Shared.md");
    win.flush();
    await done.promise;
    await coordinator.waitForIdle();
    const out = baseline.getCollaboration("vault-1", 42);
    assert.notEqual(out.pending, null);
    assert.ok(globalThis.__ossNotices.some((m) => String(m).includes("uploadFailed") || String(m).includes("collab.uploadFailed")));
  } finally {
    win.restore();
    await cleanup();
    await bc();
  }
});

test("file-sync 409 respects local mutation in flight does not overwrite newer bytes", async () => {
  const win = installWindowFake();
  globalThis.__ossNotices = [];
  const { mod, cleanup } = await loadSyncModules();
  const { BaselineStore, cleanup: bc } = await buildBaselineStore();
  try {
    const baseText = "base";
    const baseHash = await sha(baseText);
    const localText = "local edit";
    const remoteText = "remote edit";
    const remoteBytes = new TextEncoder().encode(remoteText);
    const remoteHash = await sha(remoteText);
    const { app, store } = createFakeVault(new Map([["协作oss/owner/Shared.md", localText]]));
    const entry = makeCollabEntry({ baseText, serverRevision: 5, serverHash: baseHash, localHash: baseHash });
    const baseline = await makeBaseline(BaselineStore, entry);
    const vault = new mod.CollaborationFileVault({ app });
    const done = createDeferred();
    const coordinator = createTrackedCoordinator(mod);
    const api = {
      collabUpload: async () => { throw oss409(); },
      downloadCollabContent: async () => {
        store.set("协作oss/owner/Shared.md", "newer local");
        done.resolve();
        return { content: remoteBytes.buffer.slice(0), meta: { path: "", type: "markdown", hash: remoteHash, size: remoteBytes.length, mtime: 1, revision: 6, deleted: false } };
      },
    };
    const sync = createSync(mod, { baseline, vault, api, now: () => FIXED_NOW, createOperationID: () => FIXED_ID, coordinator });
    sync.handleLocalEdit("协作oss/owner/Shared.md");
    win.flush();
    await done.promise;
    await coordinator.waitForIdle();
    assert.equal(store.get("协作oss/owner/Shared.md"), "newer local");
    const out = baseline.getCollaboration("vault-1", 42);
    assert.ok(out.pending || out.conflict || out.localHash);
  } finally {
    win.restore();
    await cleanup();
    await bc();
  }
});

test("file-sync 409 merged canonical write failure does not upload and keeps pending", async () => {
  const win = installWindowFake();
  globalThis.__ossNotices = [];
  const { mod, cleanup } = await loadSyncModules();
  const { BaselineStore, cleanup: bc } = await buildBaselineStore();
  try {
    const baseText = "a\nb\nc\nd";
    const baseHash = await sha(baseText);
    const localText = "A\nb\nc\nd";
    const remoteText = "a\nb\nc\nD";
    const remoteBytes = new TextEncoder().encode(remoteText);
    const remoteHash = await sha(remoteText);
    const { app, store } = createFakeVault(new Map([["协作oss/owner/Shared.md", localText]]));
    const entry = makeCollabEntry({ baseText, serverRevision: 7, serverHash: baseHash, localHash: baseHash });
    const baseline = await makeBaseline(BaselineStore, entry);
    const vault = new mod.CollaborationFileVault({ app });
    const origWrite = vault.writeCanonical.bind(vault);
    vault.writeCanonical = async (p, bytes) => {
      const text = new TextDecoder().decode(bytes);
      if (text === "A\nb\nc\nD") {
        throw new Error("canonical write failed");
      }
      return origWrite(p, bytes);
    };
    let uploadCalls = 0;
    const coordinator = createTrackedCoordinator(mod);
    const uploadDone = createDeferred();
    const api = {
      collabUpload: async () => {
        uploadCalls += 1;
        if (uploadCalls === 1) throw oss409();
        uploadDone.resolve();
        return { path: "", type: "markdown", hash: await sha("A\nb\nc\nD"), size: 7, mtime: 1, revision: 9, deleted: false };
      },
      downloadCollabContent: async () => ({ content: remoteBytes.buffer.slice(0), meta: { path: "", type: "markdown", hash: remoteHash, size: remoteBytes.length, mtime: 1, revision: 8, deleted: false } }),
    };
    const sync = createSync(mod, { baseline, vault, api, now: () => FIXED_NOW, createOperationID: createSequenceId("op-fixed-"), coordinator });
    sync.handleLocalEdit("协作oss/owner/Shared.md");
    win.flush();
    await coordinator.waitForIdle();
    assert.equal(uploadCalls, 1);
    const out = baseline.getCollaboration("vault-1", 42);
    assert.notEqual(out.pending, null);
    assert.equal(store.get("协作oss/owner/Shared.md"), localText);
    assert.ok(globalThis.__ossNotices.some((m) => String(m).includes("uploadFailed")));
  } finally {
    win.restore();
    await cleanup();
    await bc();
  }
});

test("remote-sync 409 independent merge via retryPending uses downloaded revision and fresh ID", async () => {
  const win = installWindowFake();
  globalThis.__ossNotices = [];
  const { mod: remoteMod, cleanup: rc } = await loadRemoteModules();
  const { mod: syncMod, cleanup: sc } = await loadSyncModules();
  const { BaselineStore, cleanup: bc } = await buildBaselineStore();
  try {
    const baseText = "a\nb\nc\nd";
    const baseHash = await sha(baseText);
    const localText = "A\nb\nc\nd";
    const remoteText = "a\nb\nc\nD";
    const mergedText = "A\nb\nc\nD";
    const remoteBytes = new TextEncoder().encode(remoteText);
    const remoteHash = await sha(remoteText);
    const mergedHash = await sha(mergedText);
    const entry = makeCollabEntry({ baseText, serverRevision: 7, serverHash: baseHash, localHash: baseHash });
    const baseline = await makeBaseline(BaselineStore, entry);
    const { app } = createFakeVault(new Map([["协作oss/owner/Shared.md", localText]]));
    const vault = new syncMod.CollaborationFileVault({ app });
    const uploadInputs = [];
    const seq = createSequenceId("op-");
    const done = createDeferred();
    const api = {
      downloadCollabContent: async () => ({ content: remoteBytes.buffer.slice(0), meta: { path: "", type: "markdown", hash: remoteHash, size: remoteBytes.length, mtime: 1, revision: 8, deleted: false } }),
      collabUpload: async (_v, _f, input) => {
        uploadInputs.push({ ...input });
        if (uploadInputs.length === 1) throw oss409();
        done.resolve();
        return { path: "", type: "markdown", hash: mergedHash, size: 11, mtime: 1, revision: 9, deleted: false };
      },
    };
    const pendingEntry = makeCollabEntry({ baseText, serverRevision: 7, serverHash: baseHash, localHash: await sha(localText), pending: { id: "op-old", createdAt: FIXED_NOW } });
    baseline.setCollaboration(pendingEntry.vaultId, pendingEntry.fileId, pendingEntry);
    const remote = createRemoteSync(remoteMod, { baseline, vault, api, now: () => FIXED_NOW, createOperationID: seq, getUsername: () => "collab-user" });
    await remote.refresh();
    await done.promise;
    await settle(5);
    assert.equal(uploadInputs.length, 2);
    assert.equal(uploadInputs[0].baseRevision, 7);
    assert.equal(uploadInputs[1].baseRevision, 8);
    assert.equal(uploadInputs[1].content, mergedText);
    assert.notEqual(uploadInputs[1].operationID, "op-old");
    const out = baseline.getCollaboration("vault-1", 42);
    assert.equal(out.pending, null);
    assert.equal(out.baseText, mergedText);
  } finally {
    win.restore();
    await rc();
    await sc();
    await bc();
  }
});

test("remote-sync 409 preserve_both binary sibling exact bytes via refresh", async () => {
  const win = installWindowFake();
  globalThis.__ossNotices = [];
  const { mod: remoteMod, cleanup: rc } = await loadRemoteModules();
  const { mod: syncMod, cleanup: sc } = await loadSyncModules();
  const { BaselineStore, cleanup: bc } = await buildBaselineStore();
  try {
    const baseText = "old";
    const baseHash = await sha(baseText);
    const localText = "local bin text";
    const localBytes = new TextEncoder().encode(localText);
    const remoteBytes = new Uint8Array([0xaa, 0xbb, 3, 4]);
    const remoteHash = await shaBytes(remoteBytes);
    const localPath = "协作oss/owner/data.bin";
    const { app, store } = createFakeVault(new Map([[localPath, localText]]));
    const entry = makeCollabEntry({ localPath, baseText, serverRevision: 3, serverHash: baseHash, localHash: baseHash });
    const baseline = await makeBaseline(BaselineStore, entry);
    const vault = new syncMod.CollaborationFileVault({ app });
    const api = {
      downloadCollabContent: async () => ({ content: remoteBytes.buffer.slice(0), meta: { path: "", type: "markdown", hash: remoteHash, size: remoteBytes.length, mtime: 1, revision: 4, deleted: false } }),
      collabUpload: async () => { throw new Error("should not upload"); },
    };
    const remote = createRemoteSync(remoteMod, { baseline, vault, api, getAccepted: () => [{ vaultId: "vault-1", fileId: 42, localPath }], now: () => FIXED_NOW });
    await remote.refresh();
    await settle(10);
    const sibling = [...store.keys()].find((k) => k.includes("_conflict_"));
    assert.ok(sibling);
    const sv = store.get(sibling);
    const sibBytes = sv instanceof Uint8Array ? sv : new TextEncoder().encode(sv);
    assert.deepEqual(sibBytes, localBytes);
    const cv = store.get(localPath);
    const canonBytes = cv instanceof Uint8Array ? cv : new TextEncoder().encode(cv);
    assert.deepEqual(canonBytes, remoteBytes);
    const out = baseline.getCollaboration("vault-1", 42);
    assert.equal(out.serverRevision, 4);
    assert.equal(out.pending, null);
  } finally {
    win.restore();
    await rc();
    await sc();
    await bc();
  }
});
