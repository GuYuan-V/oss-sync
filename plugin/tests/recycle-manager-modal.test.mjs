import assert from "node:assert/strict";
import test from "node:test";
import { FakeElement } from "./helpers/fake-dom.mjs";
import { loadEntry } from "./helpers/obsidian-loader.mjs";

test("recycle manager lists files and performs restore and confirmed permanent deletion", async () => {
  globalThis.FakeElement = FakeElement;
  globalThis.__ossNotices = [];
  globalThis.window = { confirm: () => true };
  const { module, cleanup } = await loadEntry("src/recycle-manager-modal.ts");
  try {
    let listCalls = 0;
    const restoreCalls = [];
    const deleteCalls = [];
    const plugin = {
      settings: { vaultId: "vault-1" },
      api: {
        recycleList: async () => {
          listCalls += 1;
          return {
            files: deleteCalls.length === 0
              ? [{ id: 42, path: "Notes/Deleted.md", size: 10, deleted_at: "2026-08-10T12:00:00Z", expires_at: "2026-09-09T12:00:00Z", remaining_seconds: 100, can_restore: true }]
              : [],
          };
        },
        recycleRestore: async (...args) => restoreCalls.push(args),
        recycleDelete: async (...args) => deleteCalls.push(args),
      },
      t: (key) => key,
    };
    const modal = new module.RecycleManagerModal({}, plugin);

    modal.onOpen();
    await new Promise((resolve) => setImmediate(resolve));

    assert.equal(modal.contentEl.querySelectorAll(".oss-sidebar-recycle-row").length, 1);
    modal.contentEl.querySelectorAll(".oss-recycle-restore")[0].click();
    await new Promise((resolve) => setImmediate(resolve));
    assert.deepEqual(restoreCalls, [["vault-1", 42]]);

    modal.contentEl.querySelectorAll(".oss-recycle-delete")[0].click();
    await new Promise((resolve) => setImmediate(resolve));
    assert.deepEqual(deleteCalls, [["vault-1", 42]]);
    assert.ok(listCalls >= 3);
  } finally {
    await cleanup();
  }
});
