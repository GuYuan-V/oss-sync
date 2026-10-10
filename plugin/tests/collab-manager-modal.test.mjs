import assert from "node:assert/strict";
import test from "node:test";
import { FakeElement } from "./helpers/fake-dom.mjs";
import { loadEntry } from "./helpers/obsidian-loader.mjs";

test("collaboration manager groups managed collaborators by article", async () => {
  globalThis.FakeElement = FakeElement;
  globalThis.__ossNotices = [];
  const { module, cleanup } = await loadEntry("src/collab-manager-modal.ts");
  try {
    const respondCalls = [];
    const revokeCalls = [];
    const historyViewCalls = [];
    const entries = [
      {
        id: 1,
        file_id: 41,
        vault_id: "owner-vault",
        file_path: "Incoming.md",
        owner_username: "owner",
        collaborator_username: "current-user",
        status: "pending",
      },
      {
        id: 2,
        file_id: 42,
        vault_id: "vault-1",
        file_path: "Outgoing.md",
        owner_username: "current-user",
        collaborator_username: "other-user",
        status: "accepted",
      },
      {
        id: 3,
        file_id: 42,
        vault_id: "vault-1",
        file_path: "Outgoing.md",
        owner_username: "current-user",
        collaborator_username: "another-user",
        status: "accepted",
      },
    ];
    const plugin = {
      settings: { vaultId: "vault-1", username: "current-user" },
      collabManager: {
        refresh: async () => {},
        getCollaborations: () => entries,
        respond: async (...args) => respondCalls.push(args),
        revoke: async (...args) => revokeCalls.push(args),
        setHistoryView: async (...args) => historyViewCalls.push(args),
      },
      t: (key) => key,
    };
    const modal = new module.CollabManagerModal({}, plugin);

    modal.onOpen();
    await new Promise((resolve) => setImmediate(resolve));

    // 抽屉默认收起：收到的邀请 1 行 + 文件行 1 行，协作者不占行
    assert.equal(modal.contentEl.querySelectorAll(".oss-sidebar-collab-row").length, 2);
    assert.equal(modal.contentEl.querySelectorAll(".oss-collab-view").length, 1);
    assert.equal(modal.contentEl.querySelectorAll(".oss-collab-cancel-article").length, 1);
    // 两个大类标题
    assert.equal(modal.contentEl.querySelectorAll(".oss-collab-section-title").length, 2);
    // 折叠态不渲染抽屉
    assert.equal(modal.contentEl.querySelectorAll(".oss-collab-drawer").length, 0);
    assert.equal(modal.contentEl.querySelectorAll(".oss-collab-drawer-toggle").length, 1);

    // 点击文件名展开抽屉，协作者行出现在带边框分隔的抽屉内
    modal.contentEl.querySelectorAll(".oss-collab-article-name")[0].click();
    await new Promise((resolve) => setImmediate(resolve));
    assert.equal(modal.contentEl.querySelectorAll(".oss-collab-drawer").length, 1);
    assert.equal(modal.contentEl.querySelectorAll(".oss-collab-drawer .oss-sidebar-collab-row").length, 2);
    assert.equal(modal.contentEl.querySelectorAll(".oss-collab-drawer .oss-collab-revoke").length, 2);

    // 再次点击收起
    modal.contentEl.querySelectorAll(".oss-collab-article-name")[0].click();
    await new Promise((resolve) => setImmediate(resolve));
    assert.equal(modal.contentEl.querySelectorAll(".oss-collab-drawer").length, 0);

    modal.contentEl.querySelectorAll(".oss-collab-accept")[0].click();
    await new Promise((resolve) => setImmediate(resolve));
    assert.deepEqual(respondCalls, [[entries[0], true]]);
    modal.contentEl.querySelectorAll(".oss-collab-cancel-article")[0].click();
    await new Promise((resolve) => setImmediate(resolve));
    assert.deepEqual(revokeCalls, [[entries[1]], [entries[2]]]);
  } finally {
    await cleanup();
  }
});
