import assert from "node:assert/strict";
import test from "node:test";
import { loadModule, loadSyncEngine } from "./helpers/sync-engine-loader.mjs";

test("device-local files are always excluded even with an obsolete true argument", async () => {
  const { module, cleanup } = await loadModule("src/blacklist.ts");
  try {
    for (const path of [".obsidian/workspace.json", ".obsidian/workspace-mobile.json", ".obsidian/cache/index", ".obsidian/plugins/test/data.json", "./.obsidian/plugins/oss-sync/data.json"]) {
      assert.equal(module.shouldSync(path), false, path);
      assert.equal(module.shouldSync(path, true), false, path);
    }
    for (const path of ["notes/a.md", ".obsidian/app.json", ".obsidian/appearance.json", ".obsidian/plugins/test/main.js", ".obsidian/plugins/test/dataXjson", "xobsidian/plugins/test/data.json"]) assert.equal(module.shouldSync(path), true, path);
  } finally { await cleanup(); }
});

test("legacy remote state and baseline are ignored while the page cursor advances", async () => {
  const { SyncEngine, cleanup } = await loadSyncEngine();
  const engine = Object.create(SyncEngine.prototype);
  const paths = [".obsidian/workspace.json", ".obsidian/cache/index", ".obsidian/plugins/test/data.json", "notes/a.md"];
  Object.assign(engine, {
    baseline: { getCursor: () => 0, paths: () => paths, get: () => ({ serverRevision: 1 }) },
    api: { changes: async () => ({ files: paths.map(path => ({ path })), next_cursor: 9, has_more: false }) },
  });
  try {
    const remote = await engine.fetchRemote("v", false);
    assert.deepEqual([...remote.files.keys()], ["notes/a.md"]);
    assert.equal(remote.nextCursor, 9);
    assert.deepEqual([...engine.baselineSnapshot().keys()], ["notes/a.md"]);
  } finally { await cleanup(); }
});
