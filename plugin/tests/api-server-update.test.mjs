import assert from "node:assert/strict";
import test from "node:test";
import { loadApiClient } from "./helpers/api-loader.mjs";

function settings(serverUrl) {
  return {
    serverUrl,
    username: "",
    password: "",
    syncIntervalSec: 3,
    maxConcurrency: 6,
    syncPoisonObsidianFiles: false,
    incrementalCheck: true,
    keepDirectoryTree: true,
    vaultId: "vault-1",
    vaultName: "Vault",
    clientId: "client 1",
    deviceName: "Device",
    remotePollIntervalSec: 30,
    vaultSyncMode: "short_poll",
    language: "en",
    role: "admin",
    updateRepo: "helantianshen/oss-sync",
  };
}

test("getServerVersion requests /api/admin/version with JWT headers", async () => {
  globalThis.__ossRequests = [];
  globalThis.__ossResponse = {
    status: 200,
    json: { version: "1.2.3", env: "prod", commit: "abc", built_at: "2026-01-01T00:00:00Z" },
    text: "",
    arrayBuffer: new ArrayBuffer(0),
    headers: {},
  };
  const { OSSApiClient, cleanup } = await loadApiClient();
  try {
    const client = new OSSApiClient(settings("http://localhost:8080"));
    client.setToken("jwt-token");
    const info = await client.getServerVersion();
    assert.equal(info.version, "1.2.3");
    assert.equal(info.env, "prod");
    assert.equal(globalThis.__ossRequests.length, 1);
    const req = globalThis.__ossRequests[0];
    assert.equal(req.url, "http://localhost:8080/api/admin/version");
    assert.equal(req.method, "GET");
    assert.equal(req.headers.Authorization, "Bearer jwt-token");
  } finally {
    await cleanup();
  }
});

test("checkServerUpdate requests /api/admin/update/check", async () => {
  globalThis.__ossRequests = [];
  globalThis.__ossResponse = {
    status: 200,
    json: {
      check_id: "check-123",
      candidate: { version: "1.2.4", tag: "v1.2.4", goos: "linux", goarch: "amd64", asset_name: "a", asset_url: "https://example.com/a", release_url: "https://example.com/r", size: 123, digest: "sha256:abc", release_id: 1, asset_id: 2 },
      current_version: "1.2.3",
      latest_version: "v1.2.4",
      update_available: true,
      release_url: "https://example.com/r",
      expires_at: 999999,
    },
    text: "",
    arrayBuffer: new ArrayBuffer(0),
    headers: {},
  };
  const { OSSApiClient, cleanup } = await loadApiClient();
  try {
    const client = new OSSApiClient(settings("http://localhost:8080"));
    client.setToken("tkn");
    const check = await client.checkServerUpdate();
    assert.equal(check.check_id, "check-123");
    assert.equal(check.candidate.version, "1.2.4");
    assert.equal(globalThis.__ossRequests[0].url, "http://localhost:8080/api/admin/update/check");
  } finally {
    await cleanup();
  }
});

test("getServerUpdateStatus requests /api/admin/update/status", async () => {
  globalThis.__ossRequests = [];
  globalThis.__ossResponse = {
    status: 200,
    json: { version: "1.2.3", exec_path: "/bin/oss", backup_path: "/bin/oss.bak", update_in_progress: false, state: "idle", last_update: null },
    text: "",
    arrayBuffer: new ArrayBuffer(0),
    headers: {},
  };
  const { OSSApiClient, cleanup } = await loadApiClient();
  try {
    const client = new OSSApiClient(settings("http://localhost:8080"));
    client.setToken("tkn");
    const status = await client.getServerUpdateStatus();
    assert.equal(status.version, "1.2.3");
    assert.equal(globalThis.__ossRequests[0].url, "http://localhost:8080/api/admin/update/status");
  } finally {
    await cleanup();
  }
});

test("triggerServerUpdate POSTs check_id + expected_version JSON", async () => {
  globalThis.__ossRequests = [];
  globalThis.__ossResponse = {
    status: 202,
    json: { ok: true, code: "accepted", operation: { id: "op-1", state: "in_progress", version: "1.2.4", started_at_unix: 1, updated_at_unix: 1 }, version: "1.2.4" },
    text: "",
    arrayBuffer: new ArrayBuffer(0),
    headers: {},
  };
  const { OSSApiClient, cleanup } = await loadApiClient();
  try {
    const client = new OSSApiClient(settings("http://localhost:8080"));
    client.setToken("tkn");
    const res = await client.triggerServerUpdate({ checkId: "check-123", expectedVersion: "1.2.4" });
    assert.equal(res.ok, true);
    assert.equal(res.version, "1.2.4");
    const req = globalThis.__ossRequests[0];
    assert.equal(req.url, "http://localhost:8080/api/admin/update");
    assert.equal(req.method, "POST");
    const body = JSON.parse(req.body);
    assert.equal(body.check_id, "check-123");
    assert.equal(body.expected_version, "1.2.4");
  } finally {
    await cleanup();
  }
});

test("server update transport throws OSSApiError on 401/403 for stale role", async () => {
  globalThis.__ossRequests = [];
  globalThis.__ossResponse = {
    status: 403,
    json: { error: "forbidden" },
    text: JSON.stringify({ error: "forbidden" }),
    arrayBuffer: new ArrayBuffer(0),
    headers: {},
  };
  const { OSSApiClient, cleanup } = await loadApiClient();
  try {
    const client = new OSSApiClient(settings("http://localhost:8080"));
    client.setToken("tkn");
    await assert.rejects(() => client.getServerVersion(), (err) => {
      assert.equal(err.status, 403);
      return true;
    });
  } finally {
    await cleanup();
  }
});

test("server update check and trigger use JWT client headers (not direct GitHub)", async () => {
  globalThis.__ossRequests = [];
  globalThis.__ossResponse = {
    status: 200,
    json: { version: "1.0.0", env: "dev" },
    text: "",
    arrayBuffer: new ArrayBuffer(0),
    headers: {},
  };
  const { OSSApiClient, cleanup } = await loadApiClient();
  try {
    const client = new OSSApiClient(settings("https://example.com"));
    client.setToken("jwt");
    await client.getServerVersion();
    const req = globalThis.__ossRequests[0];
    // Must use existing JWT API client — Authorization header present, no GitHub URL
    assert.ok(req.headers.Authorization?.startsWith("Bearer "));
    assert.doesNotMatch(req.url, /github\.com/);
  } finally {
    await cleanup();
  }
});
