import assert from "node:assert/strict";
import test from "node:test";
import { loadEntry } from "./helpers/obsidian-loader.mjs";

class FakeEventSource {
  static instances = [];

  constructor(url) {
    this.url = url;
    this.closed = false;
    this.listeners = new Map();
    this.onerror = null;
    FakeEventSource.instances.push(this);
  }

  addEventListener(type, listener) {
    this.listeners.set(type, listener);
  }

  close() {
    this.closed = true;
  }
}

function harness(streamUrl, forceSSE = false) {
  let pollCalls = 0;
  let accountPollCalls = 0;
  const syncCalls = [];
  const pendingPoll = new Promise(() => {});
  const api = {
    hasToken: () => true,
    collabEventStreamURL: () => streamUrl,
    collabAccountEventStreamURL: () => streamUrl?.replace("/vaults/vault-1", ""),
    collabList: async () => ({ collaborations: [] }),
    collabInbox: async () => ({ collaborations: [] }),
    collabPoll: () => {
      pollCalls += 1;
      return pendingPoll;
    },
    collabAccountPoll: () => {
      accountPollCalls += 1;
      return pendingPoll;
    },
  };
  const baseline = { getCollaboration: () => null, setCollaboration: () => {}, save: async () => {}, load: async () => {}, bindCollaborationAccount: () => false };
  const plugin = {
    settings: { vaultId: "vault-1", username: "collab-user", forceSSE },
    syncEngine: { runOnce: async (options) => syncCalls.push(options) },
    baseline,
    t: (key) => key,
  };
  const app = { vault: { getAbstractFileByPath: () => null, readBinary: async () => new ArrayBuffer(0), createBinary: async () => {}, modifyBinary: async () => {}, createFolder: async () => {} } };
  return {
    api,
    plugin,
    app,
    pollCalls: () => pollCalls,
    accountPollCalls: () => accountPollCalls,
    syncCalls,
  };
}

test("starts collaboration with SSE and falls back to long polling after an SSE error", async () => {
  globalThis.window = globalThis;
  globalThis.EventSource = FakeEventSource;
  FakeEventSource.instances = [];
  const { module, cleanup } = await loadEntry("src/collab-manager.ts");
  try {
    const fixture = harness("http://localhost:9090/api/vaults/vault-1/collaborations/stream");
    const manager = new module.CollabManager(fixture.app, fixture.api, fixture.plugin, () => {});

    manager.start();

    assert.equal(FakeEventSource.instances.length, 1);
    assert.equal(FakeEventSource.instances[0].url, "http://localhost:9090/api/collaborations/stream");
    assert.equal(fixture.pollCalls(), 0);
    assert.equal(manager.getTransportStatus(), "sidebar.collabSSE");

    const source = FakeEventSource.instances[0];
    assert.equal(source.listeners.has("invited"), true);
    source.onerror();

    assert.equal(source.closed, true);
    assert.equal(fixture.pollCalls(), 0);
    assert.equal(fixture.accountPollCalls(), 1);
    assert.equal(manager.getTransportStatus(), "sidebar.collabConnected");
    manager.stop();
  } finally {
    await cleanup();
  }
});

test("runs an incremental vault sync immediately when SSE reports changed content", async () => {
  globalThis.window = globalThis;
  globalThis.EventSource = FakeEventSource;
  FakeEventSource.instances = [];
  const { module, cleanup } = await loadEntry("src/collab-manager.ts");
  try {
    const fixture = harness("http://localhost:9090/api/vaults/vault-1/collaborations/stream");
    const manager = new module.CollabManager(fixture.app, fixture.api, fixture.plugin, () => {});

    // Given: collaboration SSE is connected.
    manager.start();
    const source = FakeEventSource.instances[0];

    // When: the server reports changed collaboration content.
    source.listeners.get("changed")();
    await new Promise((resolve) => setImmediate(resolve));

    // Then: the owner vault syncs immediately instead of waiting for its polling interval.
    assert.deepEqual(fixture.syncCalls, [{ forceFull: false }]);
    manager.stop();
  } finally {
    await cleanup();
  }
});

test("does not fall back to long polling when SSE is forced", async () => {
  globalThis.window = globalThis;
  globalThis.EventSource = FakeEventSource;
  FakeEventSource.instances = [];
  const { module, cleanup } = await loadEntry("src/collab-manager.ts");
  try {
    const fixture = harness("http://localhost:9090/api/vaults/vault-1/collaborations/stream", true);
    const manager = new module.CollabManager(fixture.app, fixture.api, fixture.plugin, () => {});

    manager.start();
    const source = FakeEventSource.instances[0];
    source.onerror();

    assert.equal(source.closed, true);
    assert.equal(fixture.pollCalls(), 0);
    assert.equal(fixture.accountPollCalls(), 0);
    assert.equal(manager.getTransportStatus(), "sidebar.collabSSEFailed");
    manager.stop();
  } finally {
    await cleanup();
  }
});

test("does not start long polling when forced SSE has no safe stream URL", async () => {
  globalThis.window = globalThis;
  globalThis.EventSource = FakeEventSource;
  FakeEventSource.instances = [];
  const { module, cleanup } = await loadEntry("src/collab-manager.ts");
  try {
    const fixture = harness(null, true);
    const manager = new module.CollabManager(fixture.app, fixture.api, fixture.plugin, () => {});

    manager.start();

    assert.equal(FakeEventSource.instances.length, 0);
    assert.equal(fixture.pollCalls(), 0);
    assert.equal(fixture.accountPollCalls(), 0);
    assert.equal(manager.getTransportStatus(), "sidebar.collabSSEUnavailable");
    manager.stop();
  } finally {
    await cleanup();
  }
});

test("syncs accepted collaboration content only for the current collaborator", async () => {
  globalThis.window = globalThis;
  const { module, cleanup } = await loadEntry("src/collab-manager.ts");
  try {
    const downloads = [];
    const created = [];
    const api = {
      hasToken: () => true,
      collabList: async () => ({
        collaborations: [
          {
            id: 1,
            file_id: 42,
            vault_id: "owner-vault",
            file_path: "Notes/Incoming.md",
            owner_username: "owner",
            collaborator_username: "collab-user",
            status: "accepted",
          },
          {
            id: 2,
            file_id: 43,
            vault_id: "vault-1",
            file_path: "Notes/Outgoing.md",
            owner_username: "collab-user",
            collaborator_username: "other-user",
            status: "accepted",
          },
        ],
      }),
      collabInbox: async () => ({ collaborations: [] }),
      downloadCollabContent: async (vaultID, fileID) => {
        downloads.push([vaultID, fileID]);
        const bytes = new TextEncoder().encode("# shared");
        const hash = await crypto.subtle.digest("SHA-256", bytes).then((d) => Array.from(new Uint8Array(d)).map((b) => b.toString(16).padStart(2, "0")).join(""));
        return { content: bytes.buffer, meta: { path: "", type: "markdown", hash, size: bytes.byteLength, mtime: 1, revision: 7, deleted: false } };
      },
    };
    const vault = {
      getAbstractFileByPath: () => null,
      readBinary: async () => { throw new Error("missing file"); },
      createFolder: async () => {},
      createBinary: async (path, content) => created.push([path, content]),
      modifyBinary: async () => {},
    };
    const plugin = {
      settings: { vaultId: "vault-1", username: "collab-user" },
      baseline: { getCollaboration: () => null, setCollaboration: () => {}, save: async () => {}, load: async () => {}, bindCollaborationAccount: () => false },
      t: (key) => key,
    };
    const manager = new module.CollabManager({ vault }, api, plugin, () => {});

    await manager.refresh();

    assert.deepEqual(downloads, [["owner-vault", 42]]);
    assert.equal(created.length, 1);
    assert.equal(created[0][0], "协作oss/owner/Notes/Incoming.md");
  } finally {
    await cleanup();
  }
});

test("discovers accepted collaborations from another vault through the account inbox", async () => {
  globalThis.window = globalThis;
  const { module, cleanup } = await loadEntry("src/collab-manager.ts");
  try {
    const downloads = [];
    const api = {
      hasToken: () => true,
      collabList: async () => ({ collaborations: [] }),
      collabInbox: async () => ({
        collaborations: [
          {
            id: 7,
            file_id: 84,
            vault_id: "owner-vault",
            file_path: "Shared.md",
            owner_username: "owner",
            collaborator_username: "collab-user",
            status: "accepted",
          },
        ],
      }),
      downloadCollabContent: async (vaultID, fileID) => {
        downloads.push([vaultID, fileID]);
        const bytes = new TextEncoder().encode("# shared");
        const hash = await crypto.subtle.digest("SHA-256", bytes).then((d) => Array.from(new Uint8Array(d)).map((b) => b.toString(16).padStart(2, "0")).join(""));
        return { content: bytes.buffer, meta: { path: "", type: "markdown", hash, size: bytes.byteLength, mtime: 1, revision: 7, deleted: false } };
      },
    };
    const vault = {
      getAbstractFileByPath: () => null,
      readBinary: async () => { throw new Error("missing file"); },
      createFolder: async () => {},
      createBinary: async () => {},
      modifyBinary: async () => {},
    };
    const plugin = {
      settings: { vaultId: "vault-1", username: "collab-user" },
      baseline: { getCollaboration: () => null, setCollaboration: () => {}, save: async () => {}, load: async () => {}, bindCollaborationAccount: () => false },
      t: (key) => key,
    };
    const manager = new module.CollabManager({ vault }, api, plugin, () => {});

    await manager.refresh();

    assert.deepEqual(downloads, [["owner-vault", 84]]);
    assert.deepEqual(manager.getCollaborations().map((entry) => entry.id), [7]);
  } finally {
    await cleanup();
  }
});

test("responds to an incoming collaboration through its owner vault", async () => {
  globalThis.window = globalThis;
  const { module, cleanup } = await loadEntry("src/collab-manager.ts");
  try {
    const responses = [];
    const entry = {
      id: 9,
      file_id: 90,
      vault_id: "owner-vault",
      file_path: "Shared.md",
      owner_username: "owner",
      collaborator_username: "collab-user",
      status: "pending",
    };
    const api = {
      hasToken: () => true,
      collabList: async () => ({ collaborations: [entry] }),
      collabInbox: async () => ({ collaborations: [] }),
      downloadCollabContent: async () => null,
      collabRespond: async (vaultID, collabID, accept) => {
        responses.push([vaultID, collabID, accept]);
      },
    };
    const plugin = {
      settings: { vaultId: "vault-1", username: "collab-user" },
      baseline: { getCollaboration: () => null, setCollaboration: () => {}, save: async () => {}, load: async () => {}, bindCollaborationAccount: () => false },
      t: (key) => key,
    };
    const manager = new module.CollabManager({ vault: { getAbstractFileByPath: () => null, readBinary: async () => new ArrayBuffer(0), createBinary: async () => {}, modifyBinary: async () => {}, createFolder: async () => {} } }, api, plugin, () => {});
    await manager.refresh();

    await manager.respond(entry, true);

    assert.deepEqual(responses, [["owner-vault", 9, true]]);
  } finally {
    await cleanup();
  }
});

test("uses long polling when the server URL cannot safely carry an SSE query token", async () => {
  globalThis.window = globalThis;
  globalThis.EventSource = FakeEventSource;
  FakeEventSource.instances = [];
  const { module, cleanup } = await loadEntry("src/collab-manager.ts");
  try {
    const fixture = harness(null);
    const manager = new module.CollabManager(fixture.app, fixture.api, fixture.plugin, () => {});

    manager.start();

    assert.equal(FakeEventSource.instances.length, 0);
    assert.equal(fixture.pollCalls(), 0);
    assert.equal(fixture.accountPollCalls(), 1);
    assert.equal(manager.getTransportStatus(), "sidebar.collabConnected");
    manager.stop();
  } finally {
    await cleanup();
  }
});
