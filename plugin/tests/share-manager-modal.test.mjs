import assert from "node:assert/strict";
import test from "node:test";
import { FakeElement } from "./helpers/fake-dom.mjs";
import { loadEntry } from "./helpers/obsidian-loader.mjs";

test("share manager lists articles and reloads after management actions", async () => {
  globalThis.FakeElement = FakeElement;
  globalThis.__ossNotices = [];
  const { module, cleanup } = await loadEntry("src/share-manager-modal.ts");
  try {
    let listCalls = 0;
    const updateCalls = [];
    const deleteCalls = [];
    const share = {
      share_id: "share-1",
      vault_id: "vault-1",
      target_path: "Notes/Shared.md",
      is_folder: false,
      allow_copy: true,
      views: 27,
      url: "/p/share-1",
      created_at: "2026-08-10T12:00:00Z",
    };
    const plugin = {
      settings: { serverUrl: "https://example.com", vaultId: "vault-1" },
      api: {
        listShares: async () => {
          listCalls += 1;
          return { shares: deleteCalls.length === 0 ? [share] : [] };
        },
        updateShareAllowCopy: async (...args) => updateCalls.push(args),
        deleteShare: async (...args) => deleteCalls.push(args),
      },
      sidebarView: { refresh() {}, reloadShares() {} },
      t: (key) => key,
    };
    const modal = new module.ShareManagerModal({}, plugin);

    modal.onOpen();
    await new Promise((resolve) => setImmediate(resolve));

    assert.equal(modal.contentEl.querySelectorAll(".oss-sidebar-share").length, 1);
    const toggle = modal.contentEl.querySelectorAll(".oss-sidebar-share-toggle")[0];
    toggle.click();
    await new Promise((resolve) => setImmediate(resolve));
    assert.deepEqual(updateCalls, [["share-1", false]]);
    assert.equal(listCalls, 2);

    const remove = modal.contentEl.querySelectorAll(".oss-sidebar-share-delete")[0];
    remove.click();
    await new Promise((resolve) => setImmediate(resolve));
    assert.deepEqual(deleteCalls, [["share-1"]]);
    assert.equal(modal.contentEl.querySelectorAll(".oss-sidebar-empty").length, 1);
  } finally {
    await cleanup();
  }
});
