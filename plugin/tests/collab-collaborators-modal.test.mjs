import assert from "node:assert/strict";
import test from "node:test";
import { FakeElement } from "./helpers/fake-dom.mjs";
import { loadEntry } from "./helpers/obsidian-loader.mjs";

test("collaborator detail bulk revoke preserves selected collaboration identity", async () => {
  globalThis.FakeElement = FakeElement;
  const { module, cleanup } = await loadEntry("src/collab-collaborators-modal.ts");
  try {
    const revokeCalls = [];
    const entries = [
      { id: 2, file_id: 42, vault_id: "vault-1", file_path: "Outgoing.md", collaborator_username: "first", status: "accepted" },
      { id: 3, file_id: 42, vault_id: "vault-1", file_path: "Outgoing.md", collaborator_username: "second", status: "accepted" },
    ];
    const modal = new module.CollabCollaboratorsModal({}, {
      collabManager: { revoke: async (...args) => revokeCalls.push(args) },
      t: (key, params = {}) => `${key}:${params.count ?? params.path ?? ""}`,
    }, entries);

    modal.onOpen();
    const checkbox = modal.contentEl.querySelectorAll("input")[1];
    checkbox.checked = true;
    checkbox.listeners.get("change")[0]();
    modal.contentEl.querySelectorAll(".oss-collab-revoke-selected")[0].click();
    await new Promise((resolve) => setImmediate(resolve));

    assert.deepEqual(revokeCalls, [[entries[1]]]);
  } finally {
    await cleanup();
  }
});

// 协作者弹窗中每位用户同行带历史查看开关，pending 状态禁用
test("collaborator row carries history view toggle with accepted state", async () => {
  globalThis.FakeElement = FakeElement;
  const { module, cleanup } = await loadEntry("src/collab-collaborators-modal.ts");
  try {
    const historyViewCalls = [];
    const entries = [
      {
        id: 7,
        file_id: 42,
        vault_id: "vault-1",
        file_path: "Outgoing.md",
        collaborator_username: "first",
        status: "accepted",
        allow_history_view: false,
      },
      {
        id: 8,
        file_id: 42,
        vault_id: "vault-1",
        file_path: "Outgoing.md",
        collaborator_username: "second",
        status: "pending",
        allow_history_view: false,
      },
    ];
    const modal = new module.CollabCollaboratorsModal({}, {
      collabManager: {
        revoke: async () => {},
        setHistoryView: async (...args) => historyViewCalls.push(args),
      },
      t: (key, params = {}) => `${key}:${params.count ?? params.path ?? ""}`,
    }, entries);

    modal.onOpen();

    // 每位协作者一行，行内均带历史开关容器
    const rows = modal.contentEl.querySelectorAll(".oss-collaborator-row");
    assert.equal(rows.length, 2);
    assert.equal(modal.contentEl.querySelectorAll(".oss-collaborator-history-toggle").length, 2);
  } finally {
    await cleanup();
  }
});
