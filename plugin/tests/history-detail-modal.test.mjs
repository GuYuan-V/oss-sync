import assert from "node:assert/strict";
import test from "node:test";
import { FakeElement } from "./helpers/fake-dom.mjs";
import { loadEntry } from "./helpers/obsidian-loader.mjs";

test("history detail shows three actions, a folded previous diff, and a current comparison", async () => {
  globalThis.FakeElement = FakeElement;
  const { module, cleanup } = await loadEntry("src/history-detail-modal.ts");
  try {
    const detailCalls = [];
    const entry = {
      id: 12,
      file_path: "Notes/History.md",
      action: "modify",
      version: 3,
      revision: 9,
      username: "author",
      device_name: "laptop",
      has_snapshot: true,
      created_at: "2026-08-11T00:00:00Z",
    };
    const plugin = {
      settings: { vaultId: "vault-1" },
      api: {
        historyDetail: async (_vaultId, historyId, mode) => {
          detailCalls.push([historyId, mode]);
          return {
            ...entry,
            content: mode === "current" ? "current" : "saved",
            diff: [" first", " second", " third", " fourth", " fifth", " sixth", " seventh", " eighth", "-saved", "+current"],
            is_text: true,
          };
        },
        historyRestore: async () => ({ path: entry.file_path }),
      },
      syncEngine: { runOnce: async () => true },
      t: (key) => key,
    };
    const modal = new module.HistoryDetailModal({}, plugin, {
      entry,
      entries: [entry],
      vaultID: "vault-1",
      canRestore: true,
    });

    // Given: a text history entry without an eligible previous snapshot.
    // When: its detail modal is opened and compared with the current note.
    modal.onOpen();
    await new Promise((resolve) => setImmediate(resolve));
    const previous = modal.contentEl.querySelectorAll(".oss-history-compare-previous")[0];
    assert.equal(previous.disabled, true);
    assert.equal(modal.contentEl.querySelectorAll(".oss-history-compare-current").length, 1);
    assert.equal(modal.contentEl.querySelectorAll(".oss-history-restore").length, 1);
    assert.equal(modal.contentEl.querySelectorAll("button").length, 3);
    assert.equal(modal.contentEl.querySelectorAll(".is-omitted").length, 1);
    assert.equal(modal.contentEl.querySelectorAll(".oss-history-detail-actions").length, 1);
    assert.equal(previous.hasClass("is-active"), true);
    assert.equal(modal.contentEl.querySelectorAll(".oss-history-comparison-status").length, 1);
    modal.contentEl.querySelectorAll(".oss-history-compare-current")[0].click();
    await new Promise((resolve) => setImmediate(resolve));

    // Then: previous and current comparisons use the server-provided folded diff.
    assert.deepEqual(detailCalls, [[12, "last"], [12, "current"]]);
    assert.equal(modal.contentEl.querySelectorAll(".oss-diff-preview").length, 1);
    assert.equal(modal.contentEl.querySelectorAll(".oss-history-compare-current")[0].hasClass("is-active"), true);
  } finally {
    await cleanup();
  }
});
