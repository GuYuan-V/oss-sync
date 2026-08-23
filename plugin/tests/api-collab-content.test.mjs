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

test("sends collaboration upload with content, base_revision, operation_id and stable client_id", async () => {
  // Given: a collaboration file with known content and revision base.
  globalThis.__ossRequests = [];
  globalThis.__ossResponse = {
    status: 200,
    json: {
      path: "Shared.md",
      type: "markdown",
      hash: "abc123",
      size: 14,
      mtime: 1700000000000,
      revision: 8,
      deleted: false,
      server_time: 1700000000000,
    },
    text: "",
    headers: {},
  };
  const { OSSApiClient, cleanup } = await loadApiClient();
  try {
    const client = new OSSApiClient(settings());
    client.setToken("token-1");

    // When: uploading collaboration content with CAS fields.
    const result = await client.collabUpload("vault 1", 42, {
      content: "# shared edit",
      baseRevision: 7,
      operationID: "op-1",
    });

    // Then: request carries content, base_revision, operation_id and stable client_id via header.
    assert.equal(globalThis.__ossRequests.length, 1);
    const req = globalThis.__ossRequests[0];
    assert.equal(req.method, "POST");
    assert.equal(new URL(req.url).pathname, "/api/vaults/vault%201/collaborations/files/42/upload");
    assert.equal(req.headers.Authorization, "Bearer token-1");
    assert.equal(req.headers["X-OSS-Client-ID"], "client-1");
    const body = JSON.parse(req.body);
    assert.equal(body.content, "# shared edit");
    assert.equal(body.base_revision, 7);
    assert.equal(body.operation_id, "op-1");
    assert.equal(result.path, "Shared.md");
  } finally {
    await cleanup();
  }
});

test("returns SyncFileMeta-shaped metadata on successful collaboration upload", async () => {
  // Given: server returns SyncFileMeta on success.
  const serverMeta = {
    path: "Shared.md",
    type: "markdown",
    hash: "hash-collab-1",
    size: 20,
    mtime: 1700000001000,
    revision: 9,
    deleted: false,
    server_time: 1700000001000,
  };
  globalThis.__ossRequests = [];
  globalThis.__ossResponse = {
    status: 200,
    json: serverMeta,
    text: JSON.stringify(serverMeta),
    headers: {},
  };
  const { OSSApiClient, cleanup } = await loadApiClient();
  try {
    const client = new OSSApiClient(settings());
    client.setToken("token-1");

    // When: uploading with CAS base.
    const result = await client.collabUpload("vault 1", 42, {
      content: "# new content",
      baseRevision: 8,
      operationID: "op-2",
    });

    // Then: request body matches CAS contract and returned value is SyncFileMeta-shaped, not {status}.
    const req = globalThis.__ossRequests[0];
    const body = JSON.parse(req.body);
    assert.equal(body.content, "# new content");
    assert.equal(body.base_revision, 8);
    assert.equal(body.operation_id, "op-2");
    assert.equal(req.headers["X-OSS-Client-ID"], "client-1");
    assert.equal(result.path, "Shared.md");
    assert.equal(result.type, "markdown");
    assert.equal(result.hash, "hash-collab-1");
    assert.equal(result.size, 20);
    assert.equal(result.mtime, 1700000001000);
    assert.equal(result.revision, 9);
    assert.equal(result.deleted, false);
    assert.equal(result.server_time, 1700000001000);
    assert.equal(typeof result.status, "undefined");
  } finally {
    await cleanup();
  }
});

test("propagates stable client_id via existing client header on collaboration upload", async () => {
  // Given: settings with stable clientId.
  globalThis.__ossRequests = [];
  globalThis.__ossResponse = {
    status: 200,
    json: {
      path: "Shared.md",
      type: "markdown",
      hash: "h",
      size: 1,
      mtime: 1,
      revision: 1,
      deleted: false,
    },
    text: "",
    headers: {},
  };
  const { OSSApiClient, cleanup } = await loadApiClient();
  try {
    const customSettings = { ...settings(), clientId: "stable-client-xyz" };
    const client = new OSSApiClient(customSettings);
    client.setToken("token-1");

    // When: uploading.
    await client.collabUpload("vault 1", 42, {
      content: "hello",
      baseRevision: 3,
      operationID: "op-stable-1",
    });

    // Then: stable client_id is sent via existing header (X-OSS-Client-ID) and body carries CAS fields.
    const req = globalThis.__ossRequests[0];
    assert.equal(req.headers["X-OSS-Client-ID"], "stable-client-xyz");
    const body = JSON.parse(req.body);
    assert.equal(body.content, "hello");
    assert.equal(body.base_revision, 3);
    assert.equal(body.operation_id, "op-stable-1");
    assert.equal(body.client_id, undefined);
  } finally {
    await cleanup();
  }
});

test("converts HTTP 409 with current metadata into OSSApiError.current", async () => {
  // Given: server reports revision conflict with current SyncFileMeta.
  const currentMeta = {
    path: "Shared.md",
    type: "markdown",
    hash: "hash-current",
    size: 18,
    mtime: 1700000002000,
    revision: 10,
    deleted: false,
    server_time: 1700000002000,
  };
  globalThis.__ossRequests = [];
  globalThis.__ossResponse = {
    status: 409,
    json: {
      error: "revision conflict",
      current: currentMeta,
    },
    text: JSON.stringify({ error: "revision conflict", current: currentMeta }),
    headers: {},
  };
  const { OSSApiClient, cleanup } = await loadApiClient();
  try {
    const client = new OSSApiClient(settings());
    client.setToken("token-1");

    // When: uploading with stale base_revision.
    let caught = null;
    try {
      await client.collabUpload("vault 1", 42, {
        content: "# stale edit",
        baseRevision: 7,
        operationID: "op-conflict-1",
      });
    } catch (e) {
      caught = e;
    }

    // Then: it throws OSSApiError with status 409 and current SyncFileMeta, and request carried CAS fields.
    assert.notEqual(caught, null);
    assert.equal(caught.name, "OSSApiError");
    assert.equal(caught.status, 409);
    assert.ok(caught.current, "expected current metadata");
    assert.equal(caught.current.path, "Shared.md");
    assert.equal(caught.current.hash, "hash-current");
    assert.equal(caught.current.revision, 10);
    assert.equal(caught.current.deleted, false);
    const req = globalThis.__ossRequests[0];
    const body = JSON.parse(req.body);
    assert.equal(body.base_revision, 7);
    assert.equal(body.operation_id, "op-conflict-1");
    assert.equal(body.content, "# stale edit");
  } finally {
    await cleanup();
  }
});
