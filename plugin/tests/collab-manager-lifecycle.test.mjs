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

function createHarness() {
  let refreshes = 0;
  const syncCalls = [];
  const api = {
    hasToken: () => true,
    collabAccountEventStreamURL: () => "http://localhost:9090/api/collaborations/stream",
    collabList: async () => {
      refreshes += 1;
      return { collaborations: [] };
    },
    collabInbox: async () => ({ collaborations: [] }),
  };
  const baseline = { getCollaboration: () => null, setCollaboration: () => {}, save: async () => {} };
  const plugin = {
    settings: { vaultId: "vault-1", username: "collab-user", forceSSE: false },
    syncEngine: { runOnce: async (options) => syncCalls.push(options) },
    baseline,
    t: (key) => key,
  };
  return {
    api,
    plugin,
    refreshes: () => refreshes,
    syncCalls,
  };
}

async function settle() {
  await new Promise((resolve) => setImmediate(resolve));
}

test("starts once and performs one immediate refresh", async () => {
  // Given
  globalThis.window = globalThis;
  globalThis.EventSource = FakeEventSource;
  FakeEventSource.instances = [];
  const { module, cleanup } = await loadEntry("src/collab-manager.ts");
  try {
    const fixture = createHarness();
    const manager = new module.CollabManager({ vault: {} }, fixture.api, fixture.plugin, () => {});

    // When
    manager.start();
    manager.start();
    await settle();

    // Then
    assert.equal(fixture.refreshes(), 1);
    assert.equal(FakeEventSource.instances.length, 1);
    manager.stop();
  } finally {
    await cleanup();
  }
});

for (const event of ["invited", "revoked"]) {
  test(`${event} refreshes collaboration data without regular sync`, async () => {
    // Given
    globalThis.window = globalThis;
    globalThis.EventSource = FakeEventSource;
    FakeEventSource.instances = [];
    const { module, cleanup } = await loadEntry("src/collab-manager.ts");
    try {
      const fixture = createHarness();
      const manager = new module.CollabManager({ vault: {} }, fixture.api, fixture.plugin, () => {});
      manager.start();
      await settle();

      // When
      FakeEventSource.instances[0].listeners.get(event)();
      await settle();

      // Then
      assert.equal(fixture.refreshes(), 2);
      assert.deepEqual(fixture.syncCalls, []);
      manager.stop();
    } finally {
      await cleanup();
    }
  });
}

test("stop closes the stream and reports disconnected", async () => {
  // Given
  globalThis.window = globalThis;
  globalThis.EventSource = FakeEventSource;
  FakeEventSource.instances = [];
  const { module, cleanup } = await loadEntry("src/collab-manager.ts");
  try {
    const fixture = createHarness();
    const manager = new module.CollabManager({ vault: {} }, fixture.api, fixture.plugin, () => {});
    manager.start();
    const source = FakeEventSource.instances[0];

    // When
    manager.stop();

    // Then
    assert.equal(source.closed, true);
    assert.equal(manager.isRunning(), false);
    assert.equal(manager.getTransportStatus(), "sidebar.collabDisconnected");
  } finally {
    await cleanup();
  }
});
