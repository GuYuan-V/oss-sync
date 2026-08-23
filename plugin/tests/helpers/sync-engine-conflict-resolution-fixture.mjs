import { loadSyncEngine } from "./sync-engine-loader.mjs";

const encoder = new TextEncoder();

export function enc(value) {
  return encoder.encode(value);
}

export function deferred() {
  let resolve;
  const promise = new Promise((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}

function pendingSummary(pending) {
  return pending.map((operation) => ({ kind: operation.kind, path: operation.path }));
}

function createVault(initialFiles, events) {
  const files = new Map(initialFiles);
  let onDelete = () => {};

  const vault = {
    getAbstractFileByPath(path) {
      const entry = files.get(path);
      if (!entry) return null;
      return {
        __tfile: true,
        path,
        stat: { mtime: entry.mtime, size: entry.bytes.byteLength },
      };
    },
    async readBinary(file) {
      const entry = files.get(file.path);
      if (!entry) throw new Error(`missing ${file.path}`);
      return entry.bytes.slice().buffer;
    },
    async createBinary(path, content) {
      const bytes = new Uint8Array(content).slice();
      events.push(`create:${path}`);
      files.set(path, { bytes, mtime: 20 });
    },
    async modifyBinary(file, content) {
      const bytes = new Uint8Array(content).slice();
      events.push(`replace:${file.path}`);
      files.set(file.path, { bytes, mtime: 30 });
    },
    async createFolder() {},
    async delete(file) {
      files.delete(file.path);
      events.push(`delete:${file.path}`);
      onDelete(file.path);
    },
  };

  return {
    vault,
    bytes(path) {
      const entry = files.get(path);
      return entry?.bytes.slice() ?? null;
    },
    setFile(path, bytes, mtime = 40) {
      files.set(path, { bytes: bytes.slice(), mtime });
      events.push(`local-write:${path}`);
    },
    removeFile(path) {
      files.delete(path);
      events.push(`local-delete:${path}`);
    },
    afterDelete(handler) {
      onDelete = handler;
    },
    paths() {
      return [...files.keys()];
    },
  };
}

async function shaHex(bytes) {
  const digest = await crypto.subtle.digest("SHA-256", bytes.slice());
  return Array.from(new Uint8Array(digest))
    .map((value) => value.toString(16).padStart(2, "0"))
    .join("");
}

export async function createConflictFixture({ localBytes, remoteBytes, remoteDeleted = false }) {
  const { SyncEngine, cleanup } = await loadSyncEngine();
  const installedWindow = globalThis.window === undefined;
  if (installedWindow) {
    globalThis.window = {
      setTimeout: () => 1,
      clearTimeout: () => {},
    };
  }
  const path = "Notes/Conflict.md";
  const events = [];
  const localHash = await shaHex(localBytes);
  const remote = {
    path,
    type: "markdown",
    hash: remoteDeleted ? "" : await shaHex(remoteBytes),
    size: remoteDeleted ? 0 : remoteBytes.byteLength,
    mtime: 50,
    revision: 9,
    deleted: remoteDeleted,
  };
  const vaultState = createVault(new Map([[path, { bytes: localBytes.slice(), mtime: 10 }]]), events);
  const conflicts = new Map([
    [path, {
      path,
      localHash,
      remoteRevision: remote.revision,
      remoteHash: remote.hash,
      remoteDeleted: remote.deleted,
      remoteMTime: remote.mtime,
      remoteSize: remote.size,
      remoteType: remote.type,
      detectedAt: 1,
    }],
  ]);
  let pending = [{ id: "obsolete", kind: "upsert", path, createdAt: 1 }];
  const saves = [];
  const baseline = {
    get: () => null,
    set() {},
    removePendingForPath(candidate) {
      events.push(`remove-pending:${candidate}`);
      pending = pending.filter((operation) => operation.path !== candidate && operation.oldPath !== candidate);
    },
    putPending(operation) {
      pending = pending.filter((existing) => existing.path !== operation.path && existing.oldPath !== operation.path);
      pending.push(operation);
      events.push(`queue:${operation.kind}:${operation.path}`);
    },
    getConflict(candidate) {
      return conflicts.get(candidate) ?? null;
    },
    putConflict(conflict) {
      conflicts.set(conflict.path, conflict);
      events.push(`record-conflict:${conflict.path}`);
    },
    removeConflict(candidate) {
      conflicts.delete(candidate);
      events.push(`remove-conflict:${candidate}`);
    },
    async save() {
      saves.push(pendingSummary(pending));
      events.push(`save:${pending.map((operation) => `${operation.kind}:${operation.path}`).join(",")}`);
    },
  };
  const api = {};
  const plugin = {
    settings: {
      vaultId: "vault-1",
      syncPoisonObsidianFiles: false,
      syncIntervalSec: 3,
      remotePollIntervalSec: 30,
    },
    t: (key) => key,
    openConflictModal() {},
  };
  const engine = new SyncEngine({ vault: vaultState.vault }, api, baseline, plugin);

  return {
    api,
    async cleanupFixture() {
      if (installedWindow) delete globalThis.window;
      await cleanup();
    },
    engine,
    events,
    path,
    remote,
    saves,
    state: {
      conflict: () => conflicts.get(path) ?? null,
      pending: () => pendingSummary(pending),
      vault: vaultState,
    },
  };
}
