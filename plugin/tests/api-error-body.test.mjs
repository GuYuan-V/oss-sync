import assert from "node:assert/strict";
import test from "node:test";
import { loadApiClient } from "./helpers/api-loader.mjs";

function makeClient(serverUrl = "https://sync.example.com") {
  return {
    serverUrl,
    username: "tester",
    password: "secret",
    clientId: "client-1",
    deviceName: "device-1",
    vaultId: "vault-1",
  };
}

function primeResponse(response) {
  globalThis.__ossRequests = [];
  globalThis.__ossResponse = response;
}

test("an HTML error page becomes a readable message instead of a JSON parse error", async () => {
  const { OSSApiClient, cleanup } = await loadApiClient();
  try {
    primeResponse({
      status: 500,
      json: null,
      text: "<!doctype html><html><body><h1># 数据局平台</h1><p>database is locked</p></body></html>",
      arrayBuffer: new ArrayBuffer(0),
      headers: {},
    });
    const client = new OSSApiClient(makeClient(), undefined, () => {});

    await assert.rejects(
      client.listVaults(),
      (error) => {
        assert.equal(error.status, 500);
        assert.equal(error.message, "服务器内部错误（HTTP 500）");
        // 不得把 JSON 解析异常泄漏给用户
        assert.equal(error.message.includes("Unexpected token"), false);
        return true;
      }
    );
  } finally {
    delete globalThis.__ossRequests;
    delete globalThis.__ossResponse;
    await cleanup();
  }
});

test("a JSON error body still surfaces the server message and code", async () => {
  const { OSSApiClient, cleanup } = await loadApiClient();
  try {
    primeResponse({
      status: 401,
      json: null,
      text: JSON.stringify({ error: "jwt token expired", code: "token_expired" }),
      arrayBuffer: new ArrayBuffer(0),
      headers: {},
    });
    const client = new OSSApiClient(makeClient(), undefined, () => {});
    client.setToken("stale-token");

    await assert.rejects(
      client.listVaults(),
      (error) => {
        assert.equal(error.status, 401);
        assert.equal(error.message, "jwt token expired");
        assert.equal(error.code, "token_expired");
        return true;
      }
    );
  } finally {
    delete globalThis.__ossRequests;
    delete globalThis.__ossResponse;
    await cleanup();
  }
});

test("a plain-text 503 keeps the body text as the message", async () => {
  const { OSSApiClient, cleanup } = await loadApiClient();
  try {
    primeResponse({
      status: 503,
      json: null,
      text: "service unavailable",
      arrayBuffer: new ArrayBuffer(0),
      headers: {},
    });
    const client = new OSSApiClient(makeClient(), undefined, () => {});

    await assert.rejects(
      client.listVaults(),
      (error) => {
        assert.equal(error.status, 503);
        assert.equal(error.message, "service unavailable");
        return true;
      }
    );
  } finally {
    delete globalThis.__ossRequests;
    delete globalThis.__ossResponse;
    await cleanup();
  }
});

test("an empty error body falls back to a status description", async () => {
  const { OSSApiClient, cleanup } = await loadApiClient();
  try {
    primeResponse({ status: 404, json: null, text: "", arrayBuffer: new ArrayBuffer(0), headers: {} });
    const client = new OSSApiClient(makeClient(), undefined, () => {});

    await assert.rejects(
      client.listVaults(),
      (error) => {
        assert.equal(error.status, 404);
        assert.equal(error.message, "接口不存在（HTTP 404）");
        return true;
      }
    );
  } finally {
    delete globalThis.__ossRequests;
    delete globalThis.__ossResponse;
    await cleanup();
  }
});
