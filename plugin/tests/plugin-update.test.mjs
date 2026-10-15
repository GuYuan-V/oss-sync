import assert from "node:assert/strict";
import test from "node:test";
import { loadUpdateModule } from "./helpers/plugin-update-loader.mjs";
import { loadMain } from "./helpers/plugin-main-loader.mjs";

test("compareVersions orders semver core versions", async () => {
  const { compareVersions, cleanup } = await loadUpdateModule("src/plugin-update.ts");
  try {
    assert.equal(compareVersions("0.1.0", "0.1.0"), 0);
    assert.equal(compareVersions("0.1", "0.1.0"), 0);
    assert.equal(compareVersions("0.1.0", "0.1.1"), -1);
    assert.equal(compareVersions("0.1.9", "0.2.0"), -1);
    assert.equal(compareVersions("1.0.0", "0.9.9"), 1);
    assert.equal(compareVersions("0.10.0", "0.9.9"), 1);
  } finally {
    await cleanup();
  }
});

test("compareVersions orders pre-release identifiers below releases", async () => {
  const { compareVersions, cleanup } = await loadUpdateModule("src/plugin-update.ts");
  try {
    assert.equal(compareVersions("0.2.0-beta.1", "0.2.0"), -1);
    assert.equal(compareVersions("0.2.0", "0.2.0-beta.1"), 1);
    assert.equal(compareVersions("0.2.0-alpha", "0.2.0-beta"), -1);
    assert.equal(compareVersions("0.2.0-beta.10", "0.2.0-beta.2"), 1);
    assert.equal(compareVersions("0.2.0-beta.1", "0.2.0-beta.1"), 0);
  } finally {
    await cleanup();
  }
});

test("checks only the repository root manifest and reports newer stable versions", async () => {
  const { checkPluginVersion, cleanup } = await loadUpdateModule("src/plugin-update.ts");
  try {
    const requests = [];
    const fetchManifest = async (url) => {
      requests.push(url);
      return { status: 200, json: { id: "oss-sync", version: "0.3.0" } };
    };
    assert.equal(await checkPluginVersion("0.2.10", fetchManifest), "0.3.0");
    assert.deepEqual(requests, ["https://raw.githubusercontent.com/helantianshen/oss-sync/HEAD/manifest.json"]);
    assert.equal(await checkPluginVersion("0.3.0", fetchManifest), null);
    assert.equal(await checkPluginVersion("0.4.0", fetchManifest), null);
  } finally { await cleanup(); }
});

test("rejects failed, foreign, malformed and prerelease manifests", async () => {
  const { checkPluginVersion, cleanup } = await loadUpdateModule("src/plugin-update.ts");
  try {
    for (const response of [
      { status: 404, json: {} }, { status: 500, json: {} },
      ...[null, [], {}, { id: "other", version: "9.0.0" },
        ...["1.0.0-rc.1", "v1.0.0", "1.0", "1.0.0foo", "01.0.0", 12]
          .map((version) => ({ id: "oss-sync", version })),
      ].map((json) => ({ status: 200, json })),
    ]) {
      await assert.rejects(checkPluginVersion("0.2.10", async () => response));
    }
    await assert.rejects(checkPluginVersion("0.2.10", async () => { throw new Error("offline"); }));
  } finally { await cleanup(); }
});

test("version check interval is thirty minutes", async () => {
  const { PLUGIN_VERSION_CHECK_INTERVAL_MS, cleanup } = await loadUpdateModule("src/plugin-update.ts");
  try { assert.equal(PLUGIN_VERSION_CHECK_INTERVAL_MS, 1_800_000); }
  finally { await cleanup(); }
});

test("each completed check can retry, concurrent checks are skipped and unload stops checks", async () => {
  const { module, cleanup } = await loadMain();
  const previousRequests = globalThis.__ossRequests;
  const previousNotices = globalThis.__ossNotices;
  globalThis.__ossRequests = [];
  globalThis.__ossNotices = [];
  try {
    const plugin = new module.default({}, { id: "oss-sync", version: "0.2.10" });
    plugin.settings = { ...plugin.settings, pluginVersionLastCheckedAt: Date.now() };
    plugin.loaded = true;
    plugin.saveData = async () => { assert.fail("version checks must not write settings"); };
    await Promise.all([plugin.autoCheckPluginVersion(), plugin.autoCheckPluginVersion()]);
    assert.equal(globalThis.__ossRequests.length, 1);
    assert.deepEqual(globalThis.__ossNotices, []);
    assert.equal(plugin.getDiagnostics().at(-1).result, "failed");
    await plugin.autoCheckPluginVersion();
    assert.equal(globalThis.__ossRequests.length, 2);
    plugin.loaded = false;
    await plugin.autoCheckPluginVersion();
    assert.equal(globalThis.__ossRequests.length, 2);
  } finally {
    globalThis.__ossRequests = previousRequests;
    globalThis.__ossNotices = previousNotices;
    await cleanup();
  }
});

test("only newer versions display a notice while every result is logged", async () => {
  const { module, cleanup } = await loadMain();
  const previousResponse = globalThis.__ossRequestUrlResponse;
  const previousNotices = globalThis.__ossNotices;
  globalThis.__ossNotices = [];
  try {
    const plugin = new module.default({}, { id: "oss-sync", version: "0.2.10" });
    plugin.settings = { ...plugin.settings, language: "en" };
    plugin.loaded = true;
    for (const version of ["0.2.10", "0.2.9", "0.3.0", "0.3.0"]) {
      globalThis.__ossRequestUrlResponse = { status: 200, json: { id: "oss-sync", version },
        text: JSON.stringify({ id: "oss-sync", version }), arrayBuffer: new ArrayBuffer(0), headers: {} };
      await plugin.autoCheckPluginVersion();
    }
    assert.equal(globalThis.__ossNotices.length, 2);
    assert.match(globalThis.__ossNotices[0], /Community plugins/);
    assert.deepEqual(plugin.getDiagnostics().map((event) => event.result),
      ["up_to_date", "up_to_date", "update_available", "update_available"]);
    assert.equal(plugin.getDiagnostics().at(-1).remoteVersion, "0.3.0");
  } finally {
    globalThis.__ossRequestUrlResponse = previousResponse;
    globalThis.__ossNotices = previousNotices;
    await cleanup();
  }
});
