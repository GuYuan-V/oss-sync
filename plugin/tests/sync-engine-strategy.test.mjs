import assert from "node:assert/strict";
import test from "node:test";
import { loadSyncEngine } from "./helpers/sync-engine-loader.mjs";

function plugin() {
  return {
    settings: {
      vaultId: "vault-policy",
      vaultSyncMode: "short_poll",
      syncIntervalSec: 3,
      remotePollIntervalSec: 30,
    },
  };
}

function strategy(effectiveMode, minDebounceSec, longPollWaitSec) {
  return {
    policy: "user_choice",
    effective_mode: effectiveMode,
    min_debounce_sec: minDebounceSec,
    long_poll_wait_sec: longPollWaitSec,
  };
}

test("rebuilds debounce when the server minimum changes", async () => {
  const { SyncEngine, cleanup } = await loadSyncEngine();
  try {
    const api = {
      hasToken: () => true,
      syncStrategy: async () => strategy("short_poll", 12, 30),
    };
    const engine = new SyncEngine({ vault: {} }, api, {}, plugin());
    engine.strategy.strategy = strategy("short_poll", 3, 30);
    let cancellations = 0;
    const currentDebounce = () => {};
    currentDebounce.cancel = () => cancellations++;
    engine.debounceFn = currentDebounce;

    await engine.applyStrategy();

    assert.equal(cancellations, 1);
    assert.notEqual(engine.debounceFn, currentDebounce);
  } finally {
    await cleanup();
  }
});

test("restarts long polling when the server wait changes", async () => {
  const { SyncEngine, cleanup } = await loadSyncEngine();
  try {
    const api = {
      hasToken: () => true,
      syncStrategy: async () => strategy("long_poll", 3, 18),
    };
    const engine = new SyncEngine({ vault: {} }, api, {}, plugin());
    engine.strategy.strategy = strategy("long_poll", 3, 30);
    engine.effectiveMode = "long_poll";
    let resets = 0;
    engine.resetPolling = () => resets++;

    await engine.applyStrategy();

    assert.equal(resets, 1);
  } finally {
    await cleanup();
  }
});
