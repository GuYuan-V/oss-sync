import assert from "node:assert/strict";
import test from "node:test";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { build } from "esbuild";

async function loadLoginState() {
  const dir = await mkdtemp(join(tmpdir(), "oss-login-state-"));
  const outfile = join(dir, "login-state.mjs");
  await build({
    entryPoints: ["src/login-state.ts"],
    outfile,
    bundle: true,
    platform: "node",
    format: "esm",
  });
  return {
    module: await import(pathToFileURL(outfile).href),
    cleanup: () => rm(dir, { recursive: true, force: true }),
  };
}

test("login accepts existing credentials shorter than registration minimums", async () => {
  const { module, cleanup } = await loadLoginState();
  try {
    assert.equal(module.validateLoginCredentials("ab", "short"), null);
  } finally {
    await cleanup();
  }
});

test("login rejects only missing credential fields", async () => {
  const { module, cleanup } = await loadLoginState();
  try {
    assert.equal(module.validateLoginCredentials("", "password"), "username_required");
    assert.equal(module.validateLoginCredentials("user", ""), "password_required");
  } finally {
    await cleanup();
  }
});

test("pending device login defers authorized session initialization", async () => {
  const { module, cleanup } = await loadLoginState();
  try {
    assert.equal(module.shouldInitializeAuthorizedSession("pending"), false);
    assert.equal(module.shouldInitializeAuthorizedSession("approved"), true);
    assert.equal(module.shouldInitializeAuthorizedSession(undefined), true);
  } finally {
    await cleanup();
  }
});
