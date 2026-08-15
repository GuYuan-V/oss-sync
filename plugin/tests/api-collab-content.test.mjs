import assert from "node:assert/strict";
import test from "node:test";
import { loadApiClient } from "./helpers/api-loader.mjs";

function settings() {
  return {
    serverUrl: "http://localhost:9090",
    username: "collab-user",
    password: "",
    syncIntervalSec: 3,
    maxConcurrency: 6,
    syncPoisonObsidianFiles: false,
    incrementalCheck: true,
    keepDirectoryTree: true,
    vaultId: "vault 1",
    vaultName: "Vault",
    clientId: "client-1",
    deviceName: "Device",
    remotePollIntervalSec: 30,
    vaultSyncMode: "short_poll",
    language: "en",
  };
}

test("downloads collaboration content by file ID through the collaboration endpoint", async () => {
  globalThis.__ossRequests = [];
  globalThis.__ossResponse = {
    status: 200,
    json: {},
    text: "",
    arrayBuffer: new TextEncoder().encode("# shared").buffer,
    headers: {
      "X-OSS-Hash": "hash-1",
      "X-OSS-MTime": "123",
      "X-OSS-Revision": "7",
    },
  };
  const { OSSApiClient, cleanup } = await loadApiClient();
  try {
    const client = new OSSApiClient(settings());
    client.setToken("token-1");

    const result = await client.downloadCollabContent("vault 1", 42);

    assert.equal(globalThis.__ossRequests.length, 1);
    assert.equal(
      new URL(globalThis.__ossRequests[0].url).pathname,
      "/api/vaults/vault%201/collaborations/files/42/content"
    );
    assert.equal(globalThis.__ossRequests[0].headers.Authorization, "Bearer token-1");
    assert.equal(result.meta.hash, "hash-1");
    assert.equal(result.meta.revision, 7);
    assert.equal(new TextDecoder().decode(result.content), "# shared");
  } finally {
    await cleanup();
  }
});
