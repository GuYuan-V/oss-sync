import assert from "node:assert/strict";
import test from "node:test";
import { loadApiClient } from "./helpers/api-loader.mjs";

function settings() {
  return {
    serverUrl: "http://localhost:9090",
    username: "user",
    password: "",
    syncIntervalSec: 3,
    maxConcurrency: 6,
    syncPoisonObsidianFiles: false,
    incrementalCheck: true,
    keepDirectoryTree: true,
    vaultId: "vault 1",
    vaultName: "Vault",
    clientId: "client 1",
    deviceName: "Device",
    remotePollIntervalSec: 30,
    vaultSyncMode: "short_poll",
    language: "en",
  };
}

test("uses typed recycle list, restore, and permanent-delete endpoints", async () => {
  globalThis.__ossRequests = [];
  globalThis.__ossResponse = { status: 200, json: { files: [] }, text: "", headers: {} };
  const { OSSApiClient, cleanup } = await loadApiClient();
  try {
    const client = new OSSApiClient(settings());
    client.setToken("token-1");

    await client.recycleList("vault 1");
    await client.recycleRestore("vault 1", 42);
    await client.recycleDelete("vault 1", 42);

    assert.deepEqual(
      globalThis.__ossRequests.map((request) => [request.method, new URL(request.url).pathname]),
      [
        ["GET", "/api/vaults/vault%201/recycle-bin"],
        ["POST", "/api/vaults/vault%201/recycle-bin/42/restore"],
        ["POST", "/api/vaults/vault%201/recycle-bin/42/delete"],
      ]
    );
    for (const request of globalThis.__ossRequests) {
      assert.equal(new URL(request.url).searchParams.get("client_id"), "client 1");
    }
  } finally {
    await cleanup();
  }
});
