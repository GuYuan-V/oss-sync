import assert from "node:assert/strict";
import test from "node:test";
import { loadModule } from "./helpers/sync-engine-loader.mjs";

test("allows only one active conflict opening per path", async () => {
  const { module, cleanup } = await loadModule("src/conflict-opening-guard.ts");
  try {
    const guard = new module.ConflictOpeningGuard();

    assert.equal(guard.begin("note.md"), true);
    assert.equal(guard.begin("note.md"), false);
    assert.equal(guard.begin("other.md"), true);
    guard.end("note.md");
    assert.equal(guard.begin("note.md"), true);
  } finally {
    await cleanup();
  }
});
