import assert from "node:assert/strict";
import test from "node:test";
import { loadBaselineStore } from "./helpers/baseline-loader.mjs";

function memoryAdapter(files) {
  return {
    async exists(p) { return files.has(p); },
    async read(p) { return files.get(p); },
    async write(p, r) { files.set(p, r); },
  };
}
function makeCollabEntry(vaultId, fileId, overrides = {}) {
  return { vaultId, fileId, localPath: `collab/note-${fileId}.md`, serverRevision: 41, serverHash: "srv-collab-AAA-111", localHash: "local-collab-BBB-222", baseText: "collab base text ascii-991", pending: { id: "pending-collab-555", createdAt: 456 }, conflict: { remoteRevision: 77, remoteHash: "rh-444", remoteText: "remote text ascii 444", detectedAt: 1710000000300 }, ...overrides };
}

test("Given v2 ordinary state When migrated to v3 Then ordinary files pending conflicts and cursor survive", async () => {
  const { BaselineStore, cleanup } = await loadBaselineStore();
  try {
    // Given: v2 state with distinct ordinary values
    const path = "Notes/Keep-991.md";
    const entry = {
      serverRevision: 1199,
      serverHash: "srv-aaa-991",
      serverDeleted: false,
      localHash: "local-bbb-992",
      localMTime: 1710000000001,
      localSize: 4242,
    };
    const pending = {
      id: "pending-xyz-771",
      kind: "upsert",
      path: "Notes/Pending-771.md",
      createdAt: 2,
    };
    const conflict = {
      path: "Notes/Conflict-882.md",
      localHash: "lh-111",
      remoteRevision: 55,
      remoteHash: "rh-222",
      remoteDeleted: false,
      remoteMTime: 1710000000099,
      remoteSize: 999,
      remoteType: "markdown",
      detectedAt: 1710000000100,
    };
    const files = new Map([
      [
        ".oss-sync-state.json",
        JSON.stringify({
          version: 2,
          vaultId: "vault-migrate-991",
          cursor: 77,
          files: { [path]: entry },
          pending: [pending],
          conflicts: [conflict],
        }),
      ],
    ]);
    const store = new BaselineStore({ adapter: memoryAdapter(files) });
    // When: load migrates to v3
    await store.load();
    // Then: ordinary state preserved
    assert.equal(store.getVaultID(), "vault-migrate-991");
    assert.equal(store.getCursor(), 77);
    assert.deepEqual(store.get(path), entry);
    assert.deepEqual(store.pending(), [pending]);
    assert.deepEqual(store.conflicts(), [conflict]);
    // And: save upgrades persisted version to 3
    await store.save();
    const persisted = JSON.parse(files.get(".oss-sync-state.json"));
    assert.equal(persisted.version, 3);
    assert.deepEqual(persisted.files[path], entry);
  } finally {
    await cleanup();
  }
});

test("Given v2 entry without baseText When loaded as v3 Then baseText remains absent", async () => {
  const { BaselineStore, cleanup } = await loadBaselineStore();
  try {
    // Given: v2 entry has no baseText
    const path = "Notes/NoBase-481.md";
    const entry = {
      serverRevision: 10,
      serverHash: "srv-no-base-481",
      serverDeleted: false,
      localHash: "local-no-base-482",
      localMTime: 1710000000400,
      localSize: 100,
    };
    const files = new Map([
      [
        ".oss-sync-state.json",
        JSON.stringify({
          version: 2,
          vaultId: "vault-base-481",
          cursor: 1,
          files: { [path]: entry },
          pending: [],
          conflicts: [],
        }),
      ],
    ]);
    const store = new BaselineStore({ adapter: memoryAdapter(files) });
    // When: load as v3
    await store.load();
    // Then: baseText absent
    const loaded = store.get(path);
    assert.ok(loaded);
    assert.equal(loaded.baseText, undefined);
    assert.equal("baseText" in loaded, false);
  } finally {
    await cleanup();
  }
});

test("Given v3 collaboration pending and conflict When reloaded Then collaboration survives", async () => {
  const { BaselineStore, cleanup } = await loadBaselineStore();
  try {
    // Given: collaboration entry keyed by vaultId:fileId
    const vaultId = "vault-collab-ABC-123";
    const fileId = 987;
    const entry = makeCollabEntry(vaultId, fileId);
    const files = new Map();
    const store = new BaselineStore({ adapter: memoryAdapter(files) });
    await store.load();
    store.bindCollaborationAccount("alice-111");
    store.setCollaboration(vaultId, fileId, entry);
    await store.save();
    // When: fresh store over same adapter
    const fresh = new BaselineStore({ adapter: memoryAdapter(files) });
    await fresh.load();
    // Then: collaboration survives
    assert.deepEqual(fresh.getCollaboration(vaultId, fileId), entry);
    assert.deepEqual(fresh.collaborationEntries(), [entry]);
  } finally {
    await cleanup();
  }
});

test("Given durable collaboration When ordinary bindVault changes Then collaboration preserved", async () => {
  const { BaselineStore, cleanup } = await loadBaselineStore();
  try {
    // Given: collaboration under ordinary vault ORIG
    const vaultId = "vault-collab-PRESERVE-222";
    const fileId = 333;
    const entry = makeCollabEntry(vaultId, fileId, {
      serverHash: "srv-preserve-AAA",
      localHash: "local-preserve-BBB",
      baseText: "preserve ascii 8a9d",
      serverRevision: 42,
      pending: { id: "pending-preserve-333", createdAt: 789 },
      conflict: {
        remoteRevision: 78,
        remoteHash: "rh-preserve-444",
        remoteText: "remote preserve 444",
        detectedAt: 1710000000400,
      },
    });
    const files = new Map();
    const store = new BaselineStore({ adapter: memoryAdapter(files) });
    await store.load();
    store.bindCollaborationAccount("alice-preserve-111");
    store.setCollaboration(vaultId, fileId, entry);
    store.bindVault("vault-orig-111");
    await store.save();
    // When: ordinary vault changes
    const changed = store.bindVault("vault-new-999");
    assert.equal(changed, true);
    await store.save();
    // Then: collaboration still present
    assert.deepEqual(store.getCollaboration(vaultId, fileId), entry);
    assert.deepEqual(store.collaborationEntries(), [entry]);
    const fresh = new BaselineStore({ adapter: memoryAdapter(files) });
    await fresh.load();
    assert.deepEqual(fresh.getCollaboration(vaultId, fileId), entry);
  } finally {
    await cleanup();
  }
});

test("Given collaboration When collaboration account changes Then collaboration cleared", async () => {
  const { BaselineStore, cleanup } = await loadBaselineStore();
  try {
    // Given: collaboration for alice
    const vaultId = "vault-collab-CLEAR-444";
    const fileId = 555;
    const entry = makeCollabEntry(vaultId, fileId, {
      serverHash: "srv-clear-AAA",
      localHash: "local-clear-BBB",
      baseText: "clear ascii c0de",
      serverRevision: 43,
      pending: { id: "pending-clear-555", createdAt: 101 },
      conflict: {
        remoteRevision: 79,
        remoteHash: "rh-clear-555",
        remoteText: "remote clear 555",
        detectedAt: 1710000000500,
      },
    });
    const files = new Map();
    const store = new BaselineStore({ adapter: memoryAdapter(files) });
    await store.load();
    store.bindCollaborationAccount("alice-111");
    store.setCollaboration(vaultId, fileId, entry);
    await store.save();
    // When: account changes to bob
    const changed = store.bindCollaborationAccount("bob-222");
    assert.equal(changed, true);
    await store.save();
    // Then: cleared
    assert.equal(store.getCollaboration(vaultId, fileId), null);
    assert.deepEqual(store.collaborationEntries(), []);
    const fresh = new BaselineStore({ adapter: memoryAdapter(files) });
    await fresh.load();
    assert.equal(fresh.getCollaboration(vaultId, fileId), null);
  } finally {
    await cleanup();
  }
});

test("Given collaboration entry When mutating input or output Then stored state stays immutable", async () => {
  const { BaselineStore, cleanup } = await loadBaselineStore();
  try {
    const vaultId = "vault-immut-777";
    const fileId = 777;
    const entry = makeCollabEntry(vaultId, fileId, {
      baseText: "immut base ascii",
      pending: { id: "pending-immut-777", createdAt: 202 },
      conflict: { remoteRevision: 80, remoteHash: "rh-immut-777", remoteText: "remote immut 777", detectedAt: 1710000000600 },
    });
    const files = new Map();
    const store = new BaselineStore({ adapter: memoryAdapter(files) });
    await store.load();
    store.bindCollaborationAccount("alice-immut");
    store.setCollaboration(vaultId, fileId, entry);
    entry.baseText = "mutated input";
    entry.pending.id = "mutated";
    entry.conflict.remoteText = "mutated";
    const a = store.getCollaboration(vaultId, fileId);
    assert.equal(a.baseText, "immut base ascii");
    assert.equal(a.pending.id, "pending-immut-777");
    a.baseText = "mutated get";
    a.pending.createdAt = 9999;
    assert.equal(store.getCollaboration(vaultId, fileId).baseText, "immut base ascii");
    const list = store.collaborationEntries();
    list[0].baseText = "mutated list";
    list.push(makeCollabEntry("vault-x", 999));
    assert.equal(store.collaborationEntries().length, 1);
    assert.equal(store.getCollaboration(vaultId, fileId).baseText, "immut base ascii");
  } finally {
    await cleanup();
  }
});
