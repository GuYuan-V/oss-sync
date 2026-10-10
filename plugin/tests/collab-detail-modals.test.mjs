import assert from "node:assert/strict";
import test from "node:test";
import { FakeElement } from "./helpers/fake-dom.mjs";
import { loadEntry } from "./helpers/obsidian-loader.mjs";

/** 构造插件桩；t 用中文文案表，保证数量标签可被断言 */
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
    t: (key, params = {}) => {
      const table = {
        "collab.loading": "加载中",
        "collab.empty": "暂无协作",
        "collab.sectionIncoming": "我收到的邀请",
        "collab.sectionOutgoing": "我发出的邀请",
        "collab.view": "查看",
        "collab.countIncoming": `${params.pending}待处理/${params.accepted}正协作`,
        "collab.countOutgoing": `${params.count}发出`,
        "collab.viewCollaborators": "查看",
        "collab.cancelArticle": "取消",
        "collab.cancelSelectedArticles": `取消选中文章（${params.count}）`,
        "collab.revoke": "撤销",
        "collab.revokeSelected": `撤销选中（${params.count}）`,
        "collab.historyView": "修改记录",
        "collab.leave": "退出协作",
        "sidebar.accept": "接受",
        "sidebar.reject": "拒绝",
        "collab.incomingMeta": `来自 ${params.name} · ${params.status}`,
        "collab.collaboratorCount": `${params.count} 位协作者`,
        "collab.collaboratorsTitle": `协作者：${params.path}`,
      };
      return table[key] ?? key;
    },
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

function emptyCalls() {
  return { respond: [], revoke: [], leave: [], historyView: [] };
}

// 主弹窗只呈现两个分区标题行，条目一律不直接铺开
test("collaboration overview shows only two section headers with counts", async () => {
  globalThis.FakeElement = FakeElement;
  globalThis.__ossNotices = [];
  const { module, cleanup } = await loadEntry("src/collab-manager-modal.ts");
  try {
    const entries = baseEntries();
    const modal = new module.CollabManagerModal({}, makePlugin(entries, emptyCalls()));

    modal.onOpen();
    await new Promise((resolve) => setImmediate(resolve));

    const headers = modal.contentEl.querySelectorAll(".oss-collab-section-header");
    assert.equal(headers.length, 2);
    // 收到的邀请：1 条 pending、0 条 accepted
    assert.equal(headers[0].getText(), "我收到的邀请1待处理/0正协作查看");
    // 发出的邀请：同一文件两个协作者按文章去重后计 1 篇
    assert.equal(headers[1].getText(), "我发出的邀请1发出查看");
    // 主弹窗不渲染任何条目行
    assert.equal(modal.contentEl.querySelectorAll(".oss-sidebar-collab-row").length, 0);
  } finally {
    await cleanup();
  }
});

// 收到的邀请详情窗按文章列出，pending 可接受拒绝，accepted 可退出
test("incoming detail modal lists articles with accept, reject and leave actions", async () => {
  globalThis.FakeElement = FakeElement;
  globalThis.__ossNotices = [];
  const { module, cleanup } = await loadEntry("src/collab-incoming-modal.ts");
  try {
    const calls = emptyCalls();
    const entries = [
      { ...baseEntries()[0] },
      { ...baseEntries()[0], id: 9, file_path: "Accepted.md", status: "accepted" },
    ];
    const modal = new module.CollabIncomingModal({}, makePlugin(entries, calls), entries);

    modal.onOpen();
    await new Promise((resolve) => setImmediate(resolve));

    assert.equal(modal.contentEl.querySelectorAll(".oss-sidebar-collab-row").length, 2);
    assert.equal(modal.contentEl.querySelectorAll(".oss-collab-accept").length, 1);
    assert.equal(modal.contentEl.querySelectorAll(".oss-collab-reject").length, 1);
    assert.equal(modal.contentEl.querySelectorAll(".oss-collab-leave").length, 1);
    // 文件名过长省略，完整名由 title 悬停展示且与显示文本一致
    const ellipses = modal.contentEl.querySelectorAll(".oss-ellipsis");
    assert.equal(ellipses.length, 2);
    assert.equal(ellipses[0].attrs.title, "Incoming.md");
    assert.equal(ellipses[1].attrs.title, "Accepted.md");

    modal.contentEl.querySelectorAll(".oss-collab-leave")[0].click();
    await new Promise((resolve) => setImmediate(resolve));
    assert.deepEqual(calls.leave, [[entries[1]]]);
  } finally {
    await cleanup();
  }
});

// 发出的邀请详情窗按文章列出，文章行带查看协作者与取消
test("outgoing detail modal lists articles with view and cancel actions", async () => {
  globalThis.FakeElement = FakeElement;
  globalThis.__ossNotices = [];
  const { module, cleanup } = await loadEntry("src/collab-outgoing-modal.ts");
  try {
    const calls = emptyCalls();
    const entries = baseEntries().slice(1);
    const modal = new module.CollabOutgoingModal({}, makePlugin(entries, calls), entries);

    modal.onOpen();
    await new Promise((resolve) => setImmediate(resolve));

    // 两个协作者属于同一篇文章，聚合为一行
    assert.equal(modal.contentEl.querySelectorAll(".oss-collab-article-row").length, 1);
    assert.equal(modal.contentEl.querySelectorAll(".oss-collab-view").length, 1);
    assert.equal(modal.contentEl.querySelectorAll(".oss-collab-cancel-article").length, 1);
    // 批量取消按钮保留
    assert.equal(modal.contentEl.querySelectorAll(".oss-collab-cancel-selected").length, 1);
    assert.equal(modal.contentEl.querySelectorAll(".oss-collab-cancel-selected")[0].getText(), "取消选中文章（0）");
  } finally {
    await cleanup();
  }
});

// 协作者窗口按协作者列出，撤销按钮与修改记录开关在同一行
test("collaborators modal keeps revoke beside history toggle in one row", async () => {
  globalThis.FakeElement = FakeElement;
  globalThis.__ossNotices = [];
  const { module, cleanup } = await loadEntry("src/collab-outgoing-modal.ts");
  try {
    const calls = emptyCalls();
    const entries = baseEntries().slice(1);
    const modal = new module.CollabCollaboratorsModal({}, makePlugin(entries, calls), entries);

    modal.onOpen();
    await new Promise((resolve) => setImmediate(resolve));

    const rows = modal.contentEl.querySelectorAll(".oss-collaborator-row");
    assert.equal(rows.length, 2);
    for (const row of rows) {
      // 同一行内含名称、状态、历史开关容器与撤销按钮
      assert.equal(row.querySelectorAll(".oss-collaborator-history-toggle").length, 1);
      assert.equal(row.querySelectorAll(".oss-collab-revoke").length, 1);
    }
    assert.equal(modal.contentEl.querySelectorAll(".oss-collab-revoke-selected").length, 1);
  } finally {
    await cleanup();
  }
});
