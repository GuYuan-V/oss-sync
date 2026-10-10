import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import test from "node:test";
import { build } from "esbuild";

async function loadTransport(obsidianStub) {
  const dir = await mkdtemp(join(tmpdir(), "oss-http-transport-"));
  const outfile = join(dir, "http-transport.mjs");
  await build({
    entryPoints: ["src/http-transport.ts"],
    outfile,
    bundle: true,
    platform: "node",
    format: "esm",
    plugins: [
      {
        name: "obsidian-stub",
        setup(builder) {
          builder.onResolve({ filter: /^obsidian$/ }, () => ({
            path: "obsidian",
            namespace: "obsidian-stub",
          }));
          builder.onLoad({ filter: /.*/, namespace: "obsidian-stub" }, () => ({
            contents: obsidianStub,
            loader: "js",
          }));
        },
      },
    ],
  });
  return {
    module: await import(pathToFileURL(outfile).href),
    cleanup: () => rm(dir, { recursive: true, force: true }),
  };
}

test("httpRequest falls back to node https when requestUrl reports a redirect loop", async () => {
  const stub = `
    export async function requestUrl() {
      const err = new Error("net::ERR_TOO_MANY_REDIRECTS");
      throw err;
    }
  `;
  const { module, cleanup } = await loadTransport(stub);
  try {
    // 指向本机一个不存在服务的端口，原生 https 会以连接失败结束
    await assert.rejects(
      module.httpRequest({ url: "https://127.0.0.1:1/api", method: "GET", headers: {} }),
      /ECONNREFUSED|connect|socket/i
    );
  } finally {
    await cleanup();
  }
});

test("httpRequest rethrows non-redirect errors from requestUrl", async () => {
  const stub = `
    export async function requestUrl() {
      throw new Error("net::ERR_CONNECTION_REFUSED");
    }
  `;
  const { module, cleanup } = await loadTransport(stub);
  try {
    await assert.rejects(
      module.httpRequest({ url: "https://example.invalid/api", method: "GET", headers: {} }),
      /ERR_CONNECTION_REFUSED/
    );
  } finally {
    await cleanup();
  }
});

test("httpRequest returns the requestUrl response unchanged on success", async () => {
  const stub = `
    export async function requestUrl(options) {
      globalThis.__lastRequest = options;
      return {
        status: 200,
        json: { ok: true },
        text: '{"ok":true}',
        arrayBuffer: new Uint8Array([1, 2, 3]).buffer,
        headers: { "x-oss-hash": "abc" },
      };
    }
  `;
  const { module, cleanup } = await loadTransport(stub);
  try {
    const res = await module.httpRequest({
      url: "https://sync.example.com/api/vaults",
      method: "GET",
      headers: { "X-OSS-Client-ID": "client-1" },
    });
    assert.equal(res.status, 200);
    assert.deepEqual(res.json, { ok: true });
    assert.equal(res.headers["x-oss-hash"], "abc");
    assert.equal(res.arrayBuffer.byteLength, 3);
    // throw:false 必须保留，避免 requestUrl 自己抛 HTTP 错误
    assert.equal(globalThis.__lastRequest.throw, false);
    assert.equal(globalThis.__lastRequest.method, "GET");
  } finally {
    delete globalThis.__lastRequest;
    await cleanup();
  }
});
