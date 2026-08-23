import assert from "node:assert/strict";
import test from "node:test";
import { loadSyncEngine } from "./helpers/sync-engine-loader.mjs";

const encoder = new TextEncoder();
const decoder = new TextDecoder();
const PATH = "Notes/Guard.md";
const NEXT_CURSOR = 7;

function enc(value) {
  return encoder.encode(value);
}

async function shaHex(bytes) {
  const digest = await crypto.subtle.digest("SHA-256", bytes.slice());
  return Array.from(new Uint8Array(digest))
    .map((value) => value.toString(16).padStart(2, "0"))
    .join("");
}

function deferred() {
  let resolve;
  const promise = new Promise((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}

async function live(path, bytes, revision = 5) {
  return {
    path,
    type: "markdown",
    hash: await shaHex(bytes),
    size: bytes.byteLength,
    mtime: 50,
    revision,
    deleted: false,
  };
}

function tombstone(path, revision = 5) {
  return {
    path,
    type: "markdown",
    hash: "",
    size: 0,
    mtime: 50,
    revision,
    deleted: true,
  };
}

async function liveBaseline(bytes, revision = 1) {
  const hash = await shaHex(bytes);
  return {
    serverRevision: revision,
    serverHash: hash,
    serverDeleted: false,
    localHash: hash,
    localMTime: 10,
    localSize: bytes.byteLength,
    baseText: decoder.decode(bytes),
  };
}

function createVault(initialFiles) {
  const files = new Map(
    initialFiles.map(([path, bytes]) => [path, { bytes: bytes.slice(), mtime: 10 }]),
  );
  const folders = new Set();
  const calls = { create: [], replace: [], delete: [] };

  const fileFor = (path) => {
    const entry = files.get(path);
    return entry
      ? { __tfile: true, path, stat: { mtime: entry.mtime, size: entry.bytes.byteLength } }
      : null;
  };

  return {
    calls,
    vault: {
      getAbstractFileByPath(path) {
        return fileFor(path) ?? (folders.has(path) ? { path } : null);
      },
      getFiles() {
        return [...files.keys()].map(fileFor);
      },
      async readBinary(file) {
        const entry = files.get(file.path);
        if (!entry) throw new Error(`missing ${file.path}`);
        return entry.bytes.slice().buffer;
      },
      async createBinary(path, content) {
        calls.create.push(path);
        files.set(path, { bytes: new Uint8Array(content).slice(), mtime: 20 });
      },
      async modifyBinary(file, content) {
        calls.replace.push(file.path);
        files.set(file.path, { bytes: new Uint8Array(content).slice(), mtime: 20 });
      },
      async createFolder(path) {
        folders.add(path);
      },
      async delete(file) {
        calls.delete.push(file.path);
        files.delete(file.path);
      },
    },
    bytes(path) {
      return files.get(path)?.bytes.slice() ?? null;
    },
    remove(path) {
      files.delete(path);
    },
    set(path, bytes) {
      files.set(path, { bytes: bytes.slice(), mtime: 40 });
    },
  };
}

async function createRunOnceFixture({
  initialFiles = [],
  initialEntries = [],
  initialPending = [],
  remoteFiles = [],
  recoverySnapshot = false,
  afterPlan,
  download,
  upload,
  deleteRemote,
  rename,
}) {
  const { SyncEngine, cleanup } = await loadSyncEngine();
  const installedWindow = globalThis.window === undefined;
  if (installedWindow) {
    globalThis.window = {
      setTimeout: () => 1,
      clearTimeout: () => {},
      setInterval: () => 1,
      clearInterval: () => {},
    };
  }

  const entries = new Map(initialEntries);
  const conflicts = new Map();
  const vault = createVault(initialFiles);
  const calls = { acknowledge: [], changes: [], delete: [], download: [], manifest: [], rename: [], upload: [] };
  let pending = [...initialPending];
  let cursor = 3;
  let saves = 0;
  const savesBeforeActions = recoverySnapshot ? 3 : 2;
  const baseline = {
    get: (path) => entries.get(path) ?? null,
    set: (path, entry) => entries.set(path, entry),
    remove: (path) => entries.delete(path),
    paths: () => [...entries.keys()],
    pending: () => [...pending],
    putPending: (operation) => {
      pending = pending.filter((item) => item.path !== operation.path && item.oldPath !== operation.path);
      pending.push(operation);
    },
    removePending: (id) => {
      pending = pending.filter((operation) => operation.id !== id);
    },
    removePendingForPath: () => {},
    conflicts: () => [...conflicts.values()],
    getConflict: (path) => conflicts.get(path) ?? null,
    putConflict: (conflict) => conflicts.set(conflict.path, conflict),
    removeConflict: (path) => conflicts.delete(path),
    load: async () => {},
    save: async () => {
      saves += 1;
      if (saves === savesBeforeActions) await afterPlan?.(vault);
    },
    bindVault: () => false,
    getCursor: () => cursor,
    setCursor: (next) => {
      cursor = Math.max(cursor, next);
    },
  };
  const api = {
    hasToken: () => true,
    syncStrategy: async () => ({
      policy: "user_choice",
      effective_mode: "short_poll",
      min_debounce_sec: 3,
      long_poll_wait_sec: 30,
    }),
    changes: async (vaultID, after) => {
      calls.changes.push([vaultID, after]);
      return {
        files: remoteFiles,
        next_cursor: NEXT_CURSOR,
        has_more: false,
        recovery_snapshot: recoverySnapshot,
      };
    },
    manifest: async (...args) => {
      calls.manifest.push(args);
      throw new Error("unexpected manifest request");
    },
    downloadV2: async (...args) => {
      calls.download.push(args);
      if (!download) throw new Error("unexpected download request");
      return download();
    },
    uploadV2: async (...args) => {
      calls.upload.push(args);
      if (!upload) throw new Error("unexpected upload request");
      return upload(...args);
    },
    deleteV2: async (...args) => {
      calls.delete.push(args);
      if (!deleteRemote) throw new Error("unexpected delete request");
      return deleteRemote(...args);
    },
    renameV2: async (...args) => {
      calls.rename.push(args);
      if (!rename) throw new Error("unexpected rename request");
      return rename(...args);
    },
    acknowledge: async (vaultID, nextCursor) => {
      calls.acknowledge.push([vaultID, nextCursor]);
    },
    isClockDriftLarge: () => false,
    getTimeOffset: () => 0,
  };
  const plugin = {
    settings: {
      vaultId: "v1",
      incrementalCheck: true,
      maxConcurrency: 1,
      syncPoisonObsidianFiles: false,
      syncIntervalSec: 3,
      remotePollIntervalSec: 30,
    },
    t: (key) => key,
    setSyncState: () => {},
    openConflictModal: () => {},
  };
  const engine = new SyncEngine({ vault: vault.vault }, api, baseline, plugin);

  return {
    cleanup: async () => {
      if (installedWindow) delete globalThis.window;
      await cleanup();
    },
    runOnce: () => engine.runOnce({ forceFull: false }),
    state: { calls, conflicts, cursor: () => cursor, entries, pending: () => pending, vault },
  };
}

function assertConflict(conflict, localHash, remote) {
  assert.ok(conflict, "conflict is persisted");
  assert.deepEqual(
    {
      path: conflict.path,
      localHash: conflict.localHash,
      remoteRevision: conflict.remoteRevision,
      remoteHash: conflict.remoteHash,
      remoteDeleted: conflict.remoteDeleted,
      remoteMTime: conflict.remoteMTime,
      remoteSize: conflict.remoteSize,
      remoteType: conflict.remoteType,
    },
    {
      path: remote.path,
      localHash,
      remoteRevision: remote.revision,
      remoteHash: remote.hash,
      remoteDeleted: remote.deleted,
      remoteMTime: remote.mtime,
      remoteSize: remote.size,
      remoteType: remote.type,
    },
  );
  assert.equal(typeof conflict.detectedAt, "number");
}

function assertConflictAcknowledged(state) {
  assert.deepEqual(state.calls.acknowledge, [["v1", NEXT_CURSOR]]);
  assert.equal(state.cursor(), NEXT_CURSOR);
}

function pendingKindsAndPaths(pending) {
  return pending.map((operation) => ({
    kind: operation.kind,
    oldPath: operation.oldPath ?? null,
    path: operation.path,
  }));
}

test("runOnce preserves a local file created while a no-baseline download is pending", async () => {
  const remoteBytes = enc("remote bytes");
  const localBytes = enc("local bytes after plan");
  const remote = await live(PATH, remoteBytes);
  const started = deferred();
  const response = deferred();
  const fixture = await createRunOnceFixture({
    remoteFiles: [remote],
    download: async () => {
      started.resolve();
      return response.promise;
    },
  });
  try {
    const run = fixture.runOnce();
    await started.promise;
    fixture.state.vault.set(PATH, localBytes);
    response.resolve({ content: remoteBytes.slice().buffer, meta: remote });

    assert.equal(await run, true);
    assert.deepEqual(fixture.state.vault.bytes(PATH), localBytes);
    assert.equal(fixture.state.entries.has(PATH), false);
    assertConflict(fixture.state.conflicts.get(PATH), await shaHex(localBytes), remote);
    assert.deepEqual(fixture.state.calls.changes, [["v1", 3]]);
    assert.deepEqual(fixture.state.calls.download, [["v1", PATH, remote.revision]]);
    assert.deepEqual(fixture.state.vault.calls, { create: [], replace: [], delete: [] });
    assertConflictAcknowledged(fixture.state);
  } finally {
    await fixture.cleanup();
  }
});

test("runOnce preserves changed bytes while an existing local download is pending", async () => {
  const baseBytes = enc("base bytes");
  const remoteBytes = enc("remote bytes");
  const localBytes = enc("new local bytes");
  const remote = await live(PATH, remoteBytes);
  const baseline = await liveBaseline(baseBytes);
  const started = deferred();
  const response = deferred();
  const fixture = await createRunOnceFixture({
    initialFiles: [[PATH, baseBytes]],
    initialEntries: [[PATH, baseline]],
    remoteFiles: [remote],
    download: async () => {
      started.resolve();
      return response.promise;
    },
  });
  try {
    const run = fixture.runOnce();
    await started.promise;
    fixture.state.vault.set(PATH, localBytes);
    response.resolve({ content: remoteBytes.slice().buffer, meta: remote });

    assert.equal(await run, true);
    assert.deepEqual(fixture.state.vault.bytes(PATH), localBytes);
    assert.deepEqual(fixture.state.entries.get(PATH), baseline);
    assertConflict(fixture.state.conflicts.get(PATH), await shaHex(localBytes), remote);
    assert.deepEqual(fixture.state.calls.download, [["v1", PATH, remote.revision]]);
    assert.deepEqual(fixture.state.vault.calls, { create: [], replace: [], delete: [] });
    assertConflictAcknowledged(fixture.state);
  } finally {
    await fixture.cleanup();
  }
});

test("runOnce preserves a local mutation before a remote tombstone deletion", async () => {
  const baseBytes = enc("base bytes");
  const localBytes = enc("local bytes after plan");
  const remote = tombstone(PATH);
  const baseline = await liveBaseline(baseBytes);
  const fixture = await createRunOnceFixture({
    initialFiles: [[PATH, baseBytes]],
    initialEntries: [[PATH, baseline]],
    remoteFiles: [remote],
    afterPlan: (vault) => vault.set(PATH, localBytes),
  });
  try {
    assert.equal(await fixture.runOnce(), true);
    assert.deepEqual(fixture.state.vault.bytes(PATH), localBytes);
    assert.deepEqual(fixture.state.entries.get(PATH), baseline);
    assertConflict(fixture.state.conflicts.get(PATH), await shaHex(localBytes), remote);
    assert.deepEqual(fixture.state.calls.download, []);
    assert.deepEqual(fixture.state.vault.calls, { create: [], replace: [], delete: [] });
    assertConflictAcknowledged(fixture.state);
  } finally {
    await fixture.cleanup();
  }
});

test("runOnce preserves a local mutation during compacted tombstone cleanup", async () => {
  const baseBytes = enc("base bytes");
  const localBytes = enc("local bytes after recovery plan");
  const baseline = await liveBaseline(baseBytes);
  const compacted = { ...tombstone(PATH, 0), mtime: 0 };
  const fixture = await createRunOnceFixture({
    initialFiles: [[PATH, baseBytes]],
    initialEntries: [[PATH, baseline]],
    recoverySnapshot: true,
    afterPlan: (vault) => vault.set(PATH, localBytes),
  });
  try {
    assert.equal(await fixture.runOnce(), true);
    assert.deepEqual(fixture.state.vault.bytes(PATH), localBytes);
    assert.deepEqual(fixture.state.entries.get(PATH), baseline);
    assertConflict(fixture.state.conflicts.get(PATH), await shaHex(localBytes), compacted);
    assert.deepEqual(fixture.state.calls.manifest, []);
    assert.deepEqual(fixture.state.vault.calls, { create: [], replace: [], delete: [] });
    assertConflictAcknowledged(fixture.state);
  } finally {
    await fixture.cleanup();
  }
});

test("runOnce adopts only a current local snapshot matching the remote hash", async () => {
  const baseBytes = enc("base bytes");
  const remoteBytes = enc("matching remote bytes");
  const remote = await live(PATH, remoteBytes);
  const fixture = await createRunOnceFixture({
    initialFiles: [[PATH, remoteBytes]],
    initialEntries: [[PATH, await liveBaseline(baseBytes)]],
    remoteFiles: [remote],
  });
  try {
    assert.equal(await fixture.runOnce(), true);
    assert.deepEqual(fixture.state.vault.bytes(PATH), remoteBytes);
    assert.deepEqual(fixture.state.entries.get(PATH), {
      serverRevision: remote.revision,
      serverHash: remote.hash,
      serverDeleted: false,
      localHash: remote.hash,
      localMTime: 10,
      localSize: remoteBytes.byteLength,
      baseText: decoder.decode(remoteBytes),
    });
    assert.equal(fixture.state.conflicts.has(PATH), false);
    assert.deepEqual(fixture.state.calls.download, []);
    assertConflictAcknowledged(fixture.state);
  } finally {
    await fixture.cleanup();
  }
});

for (const [description, mutate, localHash] of [
  ["mutates", (vault, bytes) => vault.set(PATH, bytes), (bytes) => shaHex(bytes)],
  ["disappears", (vault) => vault.remove(PATH), async () => ""],
]) {
  test(`runOnce conflicts instead of adopting when the local file ${description} after planning`, async () => {
    const baseBytes = enc("base bytes");
    const remoteBytes = enc("matching remote bytes");
    const laterBytes = enc("later local bytes");
    const remote = await live(PATH, remoteBytes);
    const baseline = await liveBaseline(baseBytes);
    const fixture = await createRunOnceFixture({
      initialFiles: [[PATH, remoteBytes]],
      initialEntries: [[PATH, baseline]],
      remoteFiles: [remote],
      afterPlan: (vault) => mutate(vault, laterBytes),
    });
    try {
      assert.equal(await fixture.runOnce(), true);
      assert.deepEqual(fixture.state.vault.bytes(PATH), description === "mutates" ? laterBytes : null);
      assert.deepEqual(fixture.state.entries.get(PATH), baseline);
      assert.equal(fixture.state.entries.get(PATH).baseText, decoder.decode(baseBytes));
      assertConflict(fixture.state.conflicts.get(PATH), await localHash(laterBytes), remote);
      assert.deepEqual(fixture.state.calls.download, []);
      assertConflictAcknowledged(fixture.state);
    } finally {
      await fixture.cleanup();
    }
  });
}

test("runOnce adopts a deleted remote entry only while the local path remains absent", async () => {
  const remote = tombstone(PATH);
  const fixture = await createRunOnceFixture({ remoteFiles: [remote] });
  try {
    assert.equal(await fixture.runOnce(), true);
    assert.equal(fixture.state.vault.bytes(PATH), null);
    assert.deepEqual(fixture.state.entries.get(PATH), {
      serverRevision: remote.revision,
      serverHash: remote.hash,
      serverDeleted: true,
      localHash: "",
      localMTime: 0,
      localSize: 0,
    });
    assert.equal("baseText" in fixture.state.entries.get(PATH), false);
    assert.equal(fixture.state.conflicts.has(PATH), false);
    assert.deepEqual(fixture.state.vault.calls, { create: [], replace: [], delete: [] });
    assertConflictAcknowledged(fixture.state);
  } finally {
    await fixture.cleanup();
  }
});

test("runOnce conflicts when a local file appears before deleted remote adoption", async () => {
  const localBytes = enc("local bytes after plan");
  const remote = tombstone(PATH);
  const fixture = await createRunOnceFixture({
    remoteFiles: [remote],
    afterPlan: (vault) => vault.set(PATH, localBytes),
  });
  try {
    assert.equal(await fixture.runOnce(), true);
    assert.deepEqual(fixture.state.vault.bytes(PATH), localBytes);
    assert.equal(fixture.state.entries.has(PATH), false);
    assertConflict(fixture.state.conflicts.get(PATH), await shaHex(localBytes), remote);
    assert.deepEqual(fixture.state.vault.calls, { create: [], replace: [], delete: [] });
    assertConflictAcknowledged(fixture.state);
  } finally {
    await fixture.cleanup();
  }
});

test("runOnce does not ACK a deferred local mutation", async () => {
  const baseBytes = enc("base bytes");
  const plannedBytes = enc("planned local bytes");
  const laterBytes = enc("later local bytes");
  const fixture = await createRunOnceFixture({
    initialFiles: [[PATH, plannedBytes]],
    initialEntries: [[PATH, await liveBaseline(baseBytes)]],
    remoteFiles: [await live(PATH, baseBytes, 1)],
    afterPlan: (vault) => vault.set(PATH, laterBytes),
  });
  try {
    assert.equal(await fixture.runOnce(), false);
    assert.deepEqual(fixture.state.vault.bytes(PATH), laterBytes);
    assert.deepEqual(fixture.state.calls.upload, []);
    assert.deepEqual(fixture.state.calls.acknowledge, []);
    assert.equal(fixture.state.cursor(), 3);
    assert.equal(fixture.state.conflicts.has(PATH), false);
  } finally {
    await fixture.cleanup();
  }
});

for (const [description, change, pendingKind] of [
  ["changes", (vault, bytes) => vault.set(PATH, bytes), "upsert"],
  ["deletes", (vault) => vault.remove(PATH), "delete"],
]) {
  test(`runOnce retains the local ${description} after acknowledging an upload snapshot`, async () => {
    const baseBytes = enc("server base");
    const sentBytes = enc("bytes sent to the server");
    const laterBytes = enc("bytes written while upload is pending");
    const sentHash = await shaHex(sentBytes);
    const server = await live(PATH, sentBytes, 12);
    const started = deferred();
    const response = deferred();
    const fixture = await createRunOnceFixture({
      initialFiles: [[PATH, sentBytes]],
      initialEntries: [[PATH, await liveBaseline(baseBytes, 4)]],
      initialPending: [{ id: "upload-snapshot", kind: "upsert", path: PATH, createdAt: 1 }],
      upload: async (_vaultID, input) => {
        started.resolve(input);
        return response.promise;
      },
    });
    try {
      const run = fixture.runOnce();
      const input = await started.promise;
      assert.equal(input.hash, sentHash);
      assert.deepEqual(new Uint8Array(input.content), sentBytes);
      change(fixture.state.vault, laterBytes);
      response.resolve(server);

      assert.equal(await run, true);
      assert.deepEqual(fixture.state.vault.bytes(PATH), pendingKind === "upsert" ? laterBytes : null);
      assert.deepEqual(fixture.state.entries.get(PATH), {
        serverRevision: server.revision,
        serverHash: server.hash,
        serverDeleted: false,
        localHash: pendingKind === "upsert" ? await shaHex(laterBytes) : "",
        localMTime: pendingKind === "upsert" ? 40 : 0,
        localSize: pendingKind === "upsert" ? laterBytes.byteLength : 0,
        baseText: decoder.decode(sentBytes),
      });
      assert.deepEqual(pendingKindsAndPaths(fixture.state.pending()), [{ kind: pendingKind, oldPath: null, path: PATH }]);
      assert.deepEqual(fixture.state.calls.upload.map(([vaultID]) => vaultID), ["v1"]);
      assertConflictAcknowledged(fixture.state);
    } finally {
      await fixture.cleanup();
    }
  });
}

test("runOnce retains a local file created while a remote delete request is pending", async () => {
  const baseBytes = enc("server base");
  const laterBytes = enc("file created while delete is pending");
  const server = tombstone(PATH, 13);
  const started = deferred();
  const response = deferred();
  const fixture = await createRunOnceFixture({
    initialEntries: [[PATH, await liveBaseline(baseBytes, 4)]],
    initialPending: [{ id: "delete-server-copy", kind: "delete", path: PATH, createdAt: 1 }],
    deleteRemote: async (_vaultID, input) => {
      started.resolve(input);
      return response.promise;
    },
  });
  try {
    const run = fixture.runOnce();
    const input = await started.promise;
    assert.deepEqual({ baseRevision: input.baseRevision, operationID: input.operationID, path: input.path }, {
      baseRevision: 4,
      operationID: "delete-server-copy",
      path: PATH,
    });
    fixture.state.vault.set(PATH, laterBytes);
    response.resolve(server);

    assert.equal(await run, true);
    assert.deepEqual(fixture.state.vault.bytes(PATH), laterBytes);
    assert.deepEqual(fixture.state.entries.get(PATH), {
      serverRevision: server.revision,
      serverHash: server.hash,
      serverDeleted: true,
      localHash: await shaHex(laterBytes),
      localMTime: 40,
      localSize: laterBytes.byteLength,
    });
    assert.equal("baseText" in fixture.state.entries.get(PATH), false);
    assert.deepEqual(pendingKindsAndPaths(fixture.state.pending()), [{ kind: "upsert", oldPath: null, path: PATH }]);
    assert.deepEqual(fixture.state.calls.delete.map(([vaultID]) => vaultID), ["v1"]);
    assertConflictAcknowledged(fixture.state);
  } finally {
    await fixture.cleanup();
  }
});

test("runOnce re-reads a renamed target and queues its post-request edit", async () => {
  const oldPath = "Notes/Before.md";
  const targetPath = "Notes/After.md";
  const sourceBytes = enc("source before rename");
  const targetBytes = enc("target bytes at rename request");
  const laterBytes = enc("target bytes after rename request");
  const targetHash = await shaHex(targetBytes);
  const renameResult = {
    old: tombstone(oldPath, 10),
    new: await live(targetPath, targetBytes, 11),
  };
  const started = deferred();
  const response = deferred();
  const fixture = await createRunOnceFixture({
    initialFiles: [[targetPath, targetBytes]],
    initialEntries: [[oldPath, await liveBaseline(sourceBytes, 4)]],
    initialPending: [{ id: "rename-before-after", kind: "rename", oldPath, path: targetPath, createdAt: 1 }],
    rename: async (_vaultID, input) => {
      started.resolve(input);
      return response.promise;
    },
  });
  try {
    const run = fixture.runOnce();
    const input = await started.promise;
    assert.deepEqual({ baseRevision: input.baseRevision, newPath: input.newPath, oldPath: input.oldPath, targetRevision: input.targetRevision }, {
      baseRevision: 4,
      newPath: targetPath,
      oldPath,
      targetRevision: 0,
    });
    fixture.state.vault.set(targetPath, laterBytes);
    response.resolve(renameResult);

    assert.equal(await run, true);
    assert.deepEqual(fixture.state.vault.bytes(targetPath), laterBytes);
    assert.deepEqual(fixture.state.entries.get(oldPath), {
      serverRevision: renameResult.old.revision,
      serverHash: renameResult.old.hash,
      serverDeleted: true,
      localHash: "",
      localMTime: 0,
      localSize: 0,
    });
    assert.equal("baseText" in fixture.state.entries.get(oldPath), false);
    assert.deepEqual(fixture.state.entries.get(targetPath), {
      serverRevision: renameResult.new.revision,
      serverHash: targetHash,
      serverDeleted: false,
      localHash: await shaHex(laterBytes),
      localMTime: 40,
      localSize: laterBytes.byteLength,
      baseText: decoder.decode(targetBytes),
    });
    assert.deepEqual(pendingKindsAndPaths(fixture.state.pending()), [{ kind: "upsert", oldPath: null, path: targetPath }]);
    assert.deepEqual(fixture.state.calls.rename.map(([vaultID]) => vaultID), ["v1"]);
    assertConflictAcknowledged(fixture.state);
  } finally {
    await fixture.cleanup();
  }
});

test("runOnce never derives renamed target baseText from post-request bytes", async () => {
  const oldPath = "Notes/Before.md";
  const targetPath = "Notes/After.md";
  const sourceBytes = enc("source before rename");
  const targetBytes = enc("target bytes at rename request");
  const laterBytes = enc("target bytes after rename request");
  const renameResult = {
    old: tombstone(oldPath, 10),
    new: await live(targetPath, laterBytes, 11),
  };
  const started = deferred();
  const response = deferred();
  const fixture = await createRunOnceFixture({
    initialFiles: [[targetPath, targetBytes]],
    initialEntries: [[oldPath, await liveBaseline(sourceBytes, 4)]],
    initialPending: [{ id: "rename-hash-mismatch", kind: "rename", oldPath, path: targetPath, createdAt: 1 }],
    rename: async (_vaultID, input) => {
      started.resolve(input);
      return response.promise;
    },
  });
  try {
    const run = fixture.runOnce();
    await started.promise;
    fixture.state.vault.set(targetPath, laterBytes);
    response.resolve(renameResult);

    assert.equal(await run, true);
    assert.deepEqual(fixture.state.vault.bytes(targetPath), laterBytes);
    assert.deepEqual(fixture.state.entries.get(targetPath), {
      serverRevision: renameResult.new.revision,
      serverHash: renameResult.new.hash,
      serverDeleted: false,
      localHash: await shaHex(laterBytes),
      localMTime: 40,
      localSize: laterBytes.byteLength,
    });
    assert.equal("baseText" in fixture.state.entries.get(targetPath), false);
    assert.deepEqual(pendingKindsAndPaths(fixture.state.pending()), [{ kind: "upsert", oldPath: null, path: targetPath }]);
    assert.deepEqual(fixture.state.entries.get(oldPath), {
      serverRevision: renameResult.old.revision,
      serverHash: renameResult.old.hash,
      serverDeleted: true,
      localHash: "",
      localMTime: 0,
      localSize: 0,
    });
    assert.equal("baseText" in fixture.state.entries.get(oldPath), false);
    assert.deepEqual(fixture.state.calls.rename.map(([vaultID]) => vaultID), ["v1"]);
    assertConflictAcknowledged(fixture.state);
  } finally {
    await fixture.cleanup();
  }
});
