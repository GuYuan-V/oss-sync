import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { pathToFileURL } from "node:url";
import { build } from "esbuild";

async function loadRecovery() {
  const dir = await mkdtemp(join(tmpdir(), "oss-device-login-"));
  const outfile = join(dir, "device-login-recovery.mjs");
  await build({
    entryPoints: ["src/device-login-recovery.ts"],
    outfile,
    bundle: true,
    platform: "node",
    format: "esm",
  });
  const module = await import(pathToFileURL(outfile).href);
  return {
    loginWithRevokedDeviceRecovery: module.loginWithRevokedDeviceRecovery,
    cleanup: () => rm(dir, { recursive: true, force: true }),
  };
}

test("retries login once after replacing a revoked device identity", async () => {
  const { loginWithRevokedDeviceRecovery, cleanup } = await loadRecovery();
  try {
    let loginCalls = 0;
    let replacementCalls = 0;
    const result = await loginWithRevokedDeviceRecovery(
      async () => {
        loginCalls += 1;
        if (loginCalls === 1) throw Object.assign(new Error("revoked"), { code: "device_revoked" });
        return { device_status: "pending" };
      },
      async () => {
        replacementCalls += 1;
      }
    );

    assert.deepEqual(result, {
      response: { device_status: "pending" },
      replacedRevokedIdentity: true,
    });
    assert.equal(loginCalls, 2);
    assert.equal(replacementCalls, 1);
  } finally {
    await cleanup();
  }
});

test("keeps the current identity when login succeeds", async () => {
  const { loginWithRevokedDeviceRecovery, cleanup } = await loadRecovery();
  try {
    let replacementCalls = 0;
    const result = await loginWithRevokedDeviceRecovery(
      async () => ({ device_status: "approved" }),
      async () => {
        replacementCalls += 1;
      }
    );

    assert.deepEqual(result, {
      response: { device_status: "approved" },
      replacedRevokedIdentity: false,
    });
    assert.equal(replacementCalls, 0);
  } finally {
    await cleanup();
  }
});

test("does not retry unrelated login failures", async () => {
  const { loginWithRevokedDeviceRecovery, cleanup } = await loadRecovery();
  try {
    const failure = Object.assign(new Error("bad credentials"), { code: "invalid_credentials" });
    let replacementCalls = 0;

    await assert.rejects(
      loginWithRevokedDeviceRecovery(
        async () => {
          throw failure;
        },
        async () => {
          replacementCalls += 1;
        }
      ),
      (error) => error === failure
    );
    assert.equal(replacementCalls, 0);
  } finally {
    await cleanup();
  }
});
