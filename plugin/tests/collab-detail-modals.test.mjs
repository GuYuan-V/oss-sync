import assert from "node:assert/strict";
import test from "node:test";
import { FakeElement } from "./helpers/fake-dom.mjs";
import { loadEntry } from "./helpers/obsidian-loader.mjs";

/** 构造一份含收到与发出两类邀请的插件桩 */
function makePlugin(entries, calls) {
  return {
    settings: { vaultId: "vault-1", username: "current-user" },
    collabManager: {
      refresh: async () => {},
      getCollaborations: () => entries,
      respond: async (...args) => calls.respond.push(args),
      revoke: async (...args) => calls.revoke.push(args),
      leave: async (...args) => calls.leave.push(args),
      setHistoryView: async (...args) => calls.historyView.push(args),
    },
    t: (key, params = {}) => `${key}:${params.count ?? params.path ?? params.name ?? params.status ?? ""}`,
  };
}

function baseEntries() {
  return [
    {
      id: 1,
      file_id: 41,
      vault_id: "owner-vault",
      file_path: "Incoming.md",
      owner_username: "owner",
      collaborator_username: "current-user",
      status: "pending",
      allow_history_view: false,
    },
    {
      id: 2,
      file_id: 42,
      vault_id: "vault-1",
      file_path: "Outgoing.md",
      owner_username: "current-user",
      collaborator_username: "first",
      status: "accepted",
      allow_history_view: false,
    },
    {
      id: 3,
      file_id: 42,
      vault_id: "vault-1",
      file_path: "Outgoing.md",
      owner_username: "current-user",
      collaborator_username: "second",
      status: "accepted",
      allow_history_view: true,
    },
  ];
}

test("collaboration manager splits incoming and outgoing with counts and view buttons", async () => {
  globalThis.FakeElement = FakeElement;
  globalThis.__ossNotices = [];
  const { module, cleanup } = await loadEntry("src/collab-manager-modal.ts");
  try {
    const calls = { respond: [], revoke: [], leave: [], historyView: [] };
    const entries = baseEntries();
    const modal = new module.CollabManagerModal({}, makePlugin(entries, calls));

    modal.onOpen();
    await new Promise((resolve) => setImmediate(resolve));

    // 两个分区标题各带数量
    const titles = modal.contentEl.querySelectorAll(".oss-collab-section-header");
    assert.equal(titles.length, 2);
    assert.equal(modal.contentEl.querySelectorAll(".oss-collab-section-count").length, 2);
    // 每个分区一个查看按钮，加上文件行自身的查看按钮
    assert.equal(modal.contentEl.querySelectorAll(".oss-collab-view").length, 3);
    // 收到的邀请行与文件行各一行，协作者不铺在主弹窗
    assert.equal(modal.contentEl.querySelectorAll(".oss-sidebar-collab-row").length, 2);
    // 收到的邀请行带接受与拒绝
    assert.equal(modal.contentEl.querySelectorAll(".oss-collab-accept").length, 1);
    assert.equal(modal.contentEl.querySelectorAll(".oss-collab-reject").length, 1);
    // 文件行带取消，批量撤销按钮保留
    assert.equal(modal.contentEl.querySelectorAll(".oss-collab-cancel-article").length, 1);
    assert.equal(modal.contentEl.querySelectorAll(".oss-collab-cancel-selected").length, 1);
    // 过长文件名带原生 title 供悬停查看全名
    assert.equal(modal.contentEl.querySelectorAll(".oss-ellipsis").length, 2);
    for (const el of modal.contentEl.querySelectorAll(".oss-ellipsis")) {
      assert.ok(el.attrs.title);
    }
  } finally {
    await cleanup();
  }
});

test("collaboration manager omits revoked entries from both sections", async () => {
  globalThis.FakeElement = FakeElement;
  globalThis.__ossNotices = [];
  const { module, cleanup } = await loadEntry("src/collab-manager-modal.ts");
  try {
    const calls = { respond: [], revoke: [], leave: [], historyView: [] };
    const entries = baseEntries().map((entry) => ({ ...entry, status: "revoked" }));
    const modal = new module.CollabManagerModal({}, makePlugin(entries, calls));

    modal.onOpen();
    await new Promise((resolve) => setImmediate(resolve));

    // 全部撤销时给空态，不渲染分区
    assert.equal(modal.contentEl.querySelectorAll(".oss-collab-section-header").length, 0);
    assert.equal(modal.contentEl.querySelectorAll(".oss-sidebar-empty").length, 1);
  } finally {
    await cleanup();
  }
});

test("incoming detail modal lists articles with accept, reject and leave actions", async () => {
  globalThis.FakeElement = FakeElement;
  globalThis.__ossNotices = [];
  const { module, cleanup } = await loadEntry("src/collab-incoming-modal.ts");
  try {
    const calls = { respond: [], revoke: [], leave: [], historyView: [] };
    const entries = [
      { ...baseEntries()[0] },
      { ...baseEntries()[0], id: 9, file_path: "Accepted.md", status: "accepted" },
    ];
    const modal = new module.CollabIncomingModal({}, makePlugin(entries, calls), entries);

    modal.onOpen();
    await new Promise((resolve) => setImmediate(resolve));

    assert.equal(modal.contentEl.querySelectorAll(".oss-sidebar-collab-row").length, 2);
    assert.equal(modal.contentEl.querySelectorAll(".oss-collab-accept").length, 1);
    assert.equal(modal.contentEl.querySelectorAll(".oss-collab-leave").length, 1);

    modal.contentEl.querySelectorAll(".oss-collab-leave")[0].click();
    await new Promise((resolve) => setImmediate(resolve));
    assert.deepEqual(calls.leave, [[entries[1]]]);
  } finally {
    await cleanup();
  }
});

test("outgoing detail modal toggles history permission per collaborator", async () => {
  globalThis.FakeElement = FakeElement;
  globalThis.__ossNotices = [];
  const { module, cleanup } = await loadEntry("src/collab-outgoing-modal.ts");
  try {
    const calls = { respond: [], revoke: [], leave: [], historyView: [] };
    const entries = baseEntries().slice(1);
    const modal = new module.CollabOutgoingModal({}, makePlugin(entries, calls), entries);

    modal.onOpen();
    await new Promise((resolve) => setImmediate(resolve));

    // 每位协作者一行，行内带历史查看开关容器
    assert.equal(modal.contentEl.querySelectorAll(".oss-collaborator-row").length, 2);
    assert.equal(modal.contentEl.querySelectorAll(".oss-collaborator-history-toggle").length, 2);
    assert.equal(modal.contentEl.querySelectorAll(".oss-collab-revoke").length, 2);
    assert.equal(modal.contentEl.querySelectorAll(".oss-collab-revoke-selected").length, 1);
    // 标题为「我发出的邀请」
    assert.equal(modal.titleEl.text, "collab.sectionOutgoing:");
  } finally {
    await cleanup();
  }
});
