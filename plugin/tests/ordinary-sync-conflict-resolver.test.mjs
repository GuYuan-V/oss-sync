import assert from "node:assert/strict";
import test from "node:test";
import { loadModule } from "./helpers/sync-engine-loader.mjs";

const encoder = new TextEncoder();
const decoder = new TextDecoder();

function bytes(text) {
  return encoder.encode(text);
}

function copyBuffer(value) {
  const copy = value.slice();
  return copy.buffer;
}

function remote({ path = "note.md", type = "markdown", hash = "remote-hash", revision = 8, body = "a\nb\nc\nD", content = bytes(body) } = {}) {
  return {
    bytes: content,
    meta: { path, type, hash, size: content.length, mtime: 800, revision, deleted: false },
  };
}

function local(body = "A\nb\nc\nd", hash = "local-hash") {
  return { bytes: bytes(body), hash, mtime: 700, size: body.length };
}

function snapshot(body, hash = "merged-hash") {
  return { bytes: bytes(body), hash, mtime: 900, size: body.length };
}

function createBaseline(events, path, initial = null) {
  const entries = new Map(initial ? [[path, initial]] : []);
  const pending = [];
  const saves = [];
  return {
    entries,
    pending,
    saves,
    get(path) { return entries.get(path) ?? null; },
    set(path, entry) { events.push(`set:${entry.serverRevision}`); entries.set(path, entry); },
    putPending(op) { events.push(`pending:${op.path}`); pending.push(op); },
    removePending(id) {
      events.push(`remove:${id}`);
      const index = pending.findIndex((op) => op.id === id);
      if (index >= 0) pending.splice(index, 1);
    },
    removePendingForPath(path) {
      events.push(`remove-path:${path}`);
      for (let index = pending.length - 1; index >= 0; index -= 1) {
        if (pending[index].path === path) pending.splice(index, 1);
      }
    },
    async save() {
      events.push("save");
      saves.push({ pending: pending.map((op) => ({ ...op })), entry: entries.get(path) });
    },
  };
}

function createResolver(mod, {
  path = "note.md",
  type = "markdown",
  body = "A\nb\nc\nd",
  localBytes = bytes(body),
  remoteBody = "a\nb\nc\nD",
  remoteBytes,
  baseText = "a\nb\nc\nd",
  replace,
  preserve,
  upload,
  initial,
} = {}) {
  const events = [];
  const baseline = createBaseline(events, path, initial ?? {
    serverRevision: 7,
    serverHash: "base-hash",
    serverDeleted: false,
    localHash: "base-hash",
    localMTime: 600,
    localSize: localBytes.length,
    baseText,
  });
  const firstLocal = { bytes: localBytes.slice(), hash: "local-hash", mtime: 700, size: localBytes.length };
  const conflicts = [];
  const uploads = [];
  const writes = {};
  const downloaded = remote({ path, type, body: remoteBody, content: remoteBytes });
  const fileAccess = {
    async readExact() { events.push("read"); return firstLocal; },
    async replace(path, expected, value) {
      events.push("replace");
      writes.canonical = value.slice();
      return replace?.(path, expected, value) ?? { kind: "replaced", snapshot: snapshot(decoder.decode(value)) };
    },
    async preserveSibling(path, expected, value) {
      events.push("preserve");
      writes.sibling = value.slice();
      return preserve?.(path, expected, value) ?? {
        kind: "preserved",
        siblingPath: "note_conflict.md",
        snapshot: snapshot(decoder.decode(value), "sibling-hash"),
      };
    },
  };
  const api = {
    async downloadV2() { events.push("download"); return { content: copyBuffer(downloaded.bytes), meta: downloaded.meta }; },
    async uploadV2(_vaultId, input) {
      events.push("upload");
      uploads.push({ ...input, bytes: new Uint8Array(input.content).slice() });
      return upload?.(input) ?? { ...downloaded.meta, hash: "merged-server-hash", revision: 9 };
    },
  };
  const resolver = new mod.OrdinarySyncConflictResolver({
    baseline,
    fileAccess,
    api,
    async recordConflict(path, current, server) { events.push("conflict"); conflicts.push({ path, current, server }); },
    createOperationID: () => "fresh-operation",
    now: () => 1234,
  });
  return {
    baseline,
    conflicts,
    events,
    resolve: () => resolver.resolve({ vaultId: "vault-1", path, expectedHash: firstLocal.hash, remote: downloaded.meta }),
    uploads,
    writes,
  };
}

test("recognizes a structural 409 only with complete current metadata", async () => {
  const { module: mod, cleanup } = await loadModule("src/ordinary-sync-conflict-resolver.ts");
  try {
    const valid = Object.assign(new Error("conflict"), { status: 409, current: remote().meta });
    const missingRevision = Object.assign(new Error("conflict"), {
      status: 409,
      current: { ...remote().meta, revision: "eight" },
    });

    assert.equal(mod.isOrdinarySyncConflict409(valid), true);
    assert.equal(mod.isOrdinarySyncConflict409(missingRevision), false);
    assert.equal(mod.isOrdinarySyncConflict409({ status: 409, current: remote().meta }), true);
  } finally {
    await cleanup();
  }
});

test("merges safely, persists each recovery boundary, and uploads exactly once", async () => {
  const { module: mod, cleanup } = await loadModule("src/ordinary-sync-conflict-resolver.ts");
  try {
    const fixture = createResolver(mod);

    const outcome = await fixture.resolve();

    assert.equal(outcome.kind, "resolved");
    assert.equal("handled" in outcome, false);
    assert.deepEqual(fixture.events, [
      "download", "read", "pending:note.md", "save", "replace", "set:8", "save",
      "upload", "set:9", "remove:fresh-operation", "save",
    ]);
    assert.equal(fixture.uploads.length, 1);
    assert.equal(fixture.uploads[0].baseRevision, 8);
    assert.deepEqual(fixture.uploads[0].bytes, bytes("A\nb\nc\nD"));
    assert.deepEqual(fixture.baseline.pending, []);
    assert.equal(fixture.baseline.saves[0].pending[0].id, "fresh-operation");
    assert.equal(fixture.baseline.saves[1].entry.serverRevision, 8);
    assert.equal(fixture.baseline.entries.get("note.md").serverRevision, 9);
    assert.equal(fixture.baseline.entries.get("note.md").baseText, "A\nb\nc\nD");
  } finally {
    await cleanup();
  }
});

test("defers after a structural second 409 without a third upload", async () => {
  const { module: mod, cleanup } = await loadModule("src/ordinary-sync-conflict-resolver.ts");
  try {
    const secondConflict = Object.assign(new Error("second conflict"), { status: 409, current: remote().meta });
    const fixture = createResolver(mod, { upload: async () => { throw secondConflict; } });

    const outcome = await fixture.resolve();

    assert.equal(outcome.kind, "deferred_retry");
    assert.equal("handled" in outcome, false);
    assert.equal(outcome.reason, "second_conflict");
    assert.equal(outcome.cause, secondConflict);
    assert.equal(fixture.uploads.length, 1);
    assert.equal(fixture.baseline.pending[0].id, "fresh-operation");
    assert.equal(fixture.baseline.entries.get("note.md").serverRevision, 8);
  } finally {
    await cleanup();
  }
});

test("defers transport errors but propagates unknown retry failures", async () => {
  const { module: mod, cleanup } = await loadModule("src/ordinary-sync-conflict-resolver.ts");
  try {
    const transport = new Error("offline");
    const deferred = createResolver(mod, { upload: async () => { throw transport; } });
    const deferredOutcome = await deferred.resolve();
    assert.equal(deferredOutcome.kind, "deferred_retry");
    assert.equal(deferredOutcome.reason, "transport");
    assert.equal(deferredOutcome.cause, transport);
    assert.equal(deferred.baseline.pending.length, 1);

    const unknown = createResolver(mod, { upload: async () => { throw "unknown failure"; } });
    await assert.rejects(unknown.resolve(), (error) => error === "unknown failure");
    assert.equal(unknown.uploads.length, 1);
    assert.equal(unknown.baseline.pending.length, 1);
  } finally {
    await cleanup();
  }
});

test("records the actual stale local state after removing an uninstalled merged pending operation", async () => {
  const { module: mod, cleanup } = await loadModule("src/ordinary-sync-conflict-resolver.ts");
  try {
    const actual = local("newer local", "newer-hash");
    const fixture = createResolver(mod, { replace: async () => ({ kind: "stale", actual }) });

    const outcome = await fixture.resolve();

    assert.equal(outcome.kind, "conflicted");
    assert.equal("handled" in outcome, false);
    assert.deepEqual(fixture.events, ["download", "read", "pending:note.md", "save", "replace", "remove:fresh-operation", "save", "conflict"]);
    assert.deepEqual(fixture.baseline.pending, []);
    assert.equal(fixture.conflicts[0].current, actual);
    assert.equal(fixture.conflicts[0].server.revision, 8);
    assert.equal(fixture.uploads.length, 0);
  } finally {
    await cleanup();
  }
});

test("records a missing local file as null after a guarded merged write", async () => {
  const { module: mod, cleanup } = await loadModule("src/ordinary-sync-conflict-resolver.ts");
  try {
    const fixture = createResolver(mod, { replace: async () => ({ kind: "stale", actual: null }) });

    const outcome = await fixture.resolve();

    assert.equal(outcome.kind, "conflicted");
    assert.equal(fixture.conflicts[0].current, null);
    assert.deepEqual(fixture.baseline.pending, []);
  } finally {
    await cleanup();
  }
});

test("queues and saves the preserve-both sibling before canonical replacement", async () => {
  const { module: mod, cleanup } = await loadModule("src/ordinary-sync-conflict-resolver.ts");
  try {
    const localBytes = new Uint8Array([1, 2, 3]);
    const remoteBytes = new Uint8Array([4, 5, 6]);
    const fixture = createResolver(mod, {
      path: "note.bin",
      type: "attachment",
      localBytes,
      remoteBytes,
      baseText: "base",
      preserve: async (_path, _expected, value) => ({
        kind: "preserved", siblingPath: "note_conflict.bin", snapshot: { bytes: value.slice(), hash: "sibling-hash", mtime: 901, size: value.length },
      }),
      replace: async (_path, _expected, value) => ({ kind: "replaced", snapshot: { bytes: value.slice(), hash: "remote-hash", mtime: 902, size: value.length } }),
    });

    const outcome = await fixture.resolve();

    assert.equal(outcome.kind, "resolved");
    assert.deepEqual(fixture.events, ["download", "read", "preserve", "pending:note_conflict.bin", "save", "replace", "set:8", "remove-path:note.bin", "save"]);
    assert.deepEqual(fixture.baseline.pending, [{ id: "fresh-operation", kind: "upsert", path: "note_conflict.bin", createdAt: 1234 }]);
    assert.deepEqual(fixture.baseline.saves[0].pending[0].path, "note_conflict.bin");
    assert.deepEqual(fixture.writes.sibling, localBytes);
    assert.deepEqual(fixture.writes.canonical, remoteBytes);
  } finally {
    await cleanup();
  }
});

test("preserve-both canonical staleness retains the sibling and collision or overlap conflicts", async () => {
  const { module: mod, cleanup } = await loadModule("src/ordinary-sync-conflict-resolver.ts");
  try {
    const actual = local("newer local", "newer-hash");
    const stale = createResolver(mod, {
      path: "note.bin",
      type: "attachment",
      localBytes: new Uint8Array([1, 2, 3]),
      remoteBytes: new Uint8Array([4, 5, 6]),
      baseText: "base",
      preserve: async (_path, _expected, value) => ({ kind: "preserved", siblingPath: "note_conflict.bin", snapshot: { bytes: value.slice(), hash: "sibling-hash", mtime: 901, size: value.length } }),
      replace: async () => ({ kind: "stale", actual }),
    });
    const staleOutcome = await stale.resolve();
    assert.equal(staleOutcome.kind, "conflicted");
    assert.deepEqual(stale.baseline.pending, [{ id: "fresh-operation", kind: "upsert", path: "note_conflict.bin", createdAt: 1234 }]);
    assert.equal(stale.events.indexOf("save") < stale.events.indexOf("replace"), true);
    assert.equal(stale.conflicts[0].current, actual);

    const collision = createResolver(mod, {
      path: "note.bin",
      type: "attachment",
      localBytes: new Uint8Array([1, 2, 3]),
      remoteBytes: new Uint8Array([4, 5, 6]),
      baseText: "base",
      preserve: async () => ({ kind: "collision", siblingPath: "note_conflict.bin" }),
    });
    const collisionOutcome = await collision.resolve();
    assert.equal(collisionOutcome.kind, "conflicted");
    assert.deepEqual(collision.baseline.pending, []);
    assert.equal(collision.events.includes("replace"), false);

    const overlap = createResolver(mod, {
      body: "a\nLOCAL\nc",
      remoteBody: "a\nREMOTE\nc",
      baseText: "a\nb\nc",
    });
    const overlapOutcome = await overlap.resolve();
    assert.equal(overlapOutcome.kind, "conflicted");
    assert.equal(overlap.events.includes("preserve"), false);
  } finally {
    await cleanup();
  }
});
