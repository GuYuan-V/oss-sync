import assert from "node:assert/strict";
import test from "node:test";
import { FakeElement } from "./helpers/fake-dom.mjs";
import { loadEntry } from "./helpers/obsidian-loader.mjs";

test("history list maps a collaboration file to its owner Vault and renders one compact row", async () => {
  globalThis.FakeElement = FakeElement;
  const { module, cleanup } = await loadEntry("src/history-modal.ts");
  try {
    const historyCalls = [];
    const entry = {
      id: 18,
      file_path: "# OpenCode Cli.md",
      action: "modify",
      version: 18,
      revision: 20,
      username: "123456789",
      device_name: "Obsidian Vault - Obsidian",
      has_snapshot: true,
      created_at: "2026-08-12T20:31:17+08:00",
    };
    const plugin = {
      settings: { vaultId: "collaborator-vault" },
      collabManager: {
        getHistoryTarget: () => ({ vaultID: "owner-vault", path: "# OpenCode Cli.md", canRestore: false }),
      },
      api: {
        history: async (vaultID, path) => {
          historyCalls.push([vaultID, path]);
          return { history: [entry] };
        },
      },
      getLanguage: () => "zh",
      t: (key) => key,
    };
    const modal = new module.HistoryModal({}, plugin, "协作oss/owner/# OpenCode Cli.md");

    modal.onOpen();
    await new Promise((resolve) => setImmediate(resolve));

    assert.deepEqual(historyCalls, [["owner-vault", "# OpenCode Cli.md"]]);
    const rows = modal.contentEl.querySelectorAll(".oss-history-entry");
    assert.equal(rows.length, 1);
    assert.equal(rows[0].querySelectorAll(".oss-history-time").length, 1);
    assert.equal(rows[0].querySelectorAll(".oss-history-user")[0].text, "123456789");
    assert.equal(rows[0].querySelectorAll(".oss-history-device")[0].text, "Obsidian Vault - Obsidian");
    assert.equal(rows[0].querySelectorAll(".oss-history-view").length, 1);
  } finally {
    await cleanup();
  }
});
