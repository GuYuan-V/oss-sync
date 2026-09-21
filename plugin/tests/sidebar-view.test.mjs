// SidebarView.refresh() 呈现未解决冲突入口与管理按钮，不呈现最近活动列表
// 只断言路径、类名、标签与事件调用等数据契约，不断言翻译文案
import assert from "node:assert/strict";
import test from "node:test";
import { FakeElement } from "./helpers/fake-dom.mjs";
import { loadEntry } from "./helpers/obsidian-loader.mjs";

const CONFLICT_PATH = "Notes/Conflict.md";

/** 通过真实 BaselineStore 从持久化文件加载给定冲突*/
async function makeStore(conflicts) {
  const { module, cleanup } = await loadEntry("src/baseline.ts");
  try {
    const files = new Map([
      [
        ".oss-sync-state.json",
        JSON.stringify({
          version: 2,
          vaultId: "vault-a",
          cursor: 1,
          files: {},
          pending: [],
          conflicts,
        }),
      ],
    ]);
    const store = new module.BaselineStore({
      adapter: {
        async exists(path) {
          return files.has(path);
        },
        async read(path) {
          return files.get(path);
        },
        async write(path, raw) {
          files.set(path, raw);
        },
      },
    });
    await store.load();
    return { store, cleanup };
  } catch (error) {
    await cleanup();
    throw error;
  }
}

function makePlugin(store) {
  const openConflictModalCalls = [];
  const openShareManagerCalls = [];
  const openCollabManagerCalls = [];
  const openRecycleManagerCalls = [];
  const updateShareCalls = [];
  const deleteShareCalls = [];
  return {
    settings: {
      username: "tester",
      deviceName: "laptop",
      vaultName: "vault-a",
      vaultId: "vault-a",
      serverUrl: "https://example.com",
    },
    api: {
      hasToken: () => true,
      listShares: async () => ({
        shares: [
          {
            share_id: "share-1",
            vault_id: "vault-a",
            target_path: "Notes/Shared.md",
            is_folder: false,
            allow_copy: true,
            views: 27,
            url: "/p/share-1",
            created_at: "2026-08-10T12:00:00Z",
          },
        ],
      }),
      updateShareAllowCopy: async (shareID, allowCopy) => updateShareCalls.push([shareID, allowCopy]),
      deleteShare: async (shareID) => deleteShareCalls.push(shareID),
    },
    t: (key, params = {}) => `${key}:${JSON.stringify(params)}`,
    baseline: store,
    collabManager: {
      getTransportStatus: () => "disconnected",
      getPendingCollabs: () => [
        {
          id: 1,
          owner_username: "owner",
          file_path: "Notes/Collab.md",
          status: "pending",
        },
      ],
      getRecentActivity: () => ["touched Notes/A.md", "touched Notes/B.md"],
    },
    syncEngine: {
      getEffectiveModeLabel: () => "user_choice",
      runOnce: () => {},
      dismissConflict: () => {},
    },
    openSettings: () => {},
    openShareManager: () => openShareManagerCalls.push(true),
    openCollabManager: () => openCollabManagerCalls.push(true),
    openRecycleManager: () => openRecycleManagerCalls.push(true),
    openConflictModal: (path) => openConflictModalCalls.push(path),
    openShareManagerCalls,
    openCollabManagerCalls,
    openRecycleManagerCalls,
    openConflictModalCalls,
    updateShareCalls,
    deleteShareCalls,
  };
}

function renderSidebar(View, plugin) {
  const root = new FakeElement("div");
  const view = new View({ contentEl: root, containerEl: root, detach() {} }, plugin);
  view.refresh();
  return root;
}

test("keeps a persisted conflict visible with a native reopen action after refresh", async () => {
  const { module, cleanup } = await loadEntry("src/sidebar-view.ts");
  const { SidebarView } = module;
  const { store, cleanup: cleanupStore } = await makeStore([
    {
      path: CONFLICT_PATH,
      localHash: "local",
      remoteRevision: 2,
      remoteHash: "remote",
      remoteDeleted: false,
      remoteMTime: 1,
      remoteSize: 10,
      remoteType: "markdown",
      detectedAt: 1,
    },
  ]);
  try {
    assert.equal(store.conflicts().length, 1);
    assert.equal(store.conflicts()[0].path, CONFLICT_PATH);

    const plugin = makePlugin(store);
    const root = renderSidebar(SidebarView, plugin);

    const rows = root.querySelectorAll(".oss-sidebar-conflict");
    assert.ok(rows.length > 0, "expected a rendered conflict row for the persisted conflict");
    assert.ok(rows[0].getText().includes(CONFLICT_PATH), "conflict row must surface the path");

    const buttons = rows[0].querySelectorAll("button");
    assert.ok(buttons.length > 0, "expected a native button in the conflict row");
    buttons[0].click();
    assert.deepEqual(plugin.openConflictModalCalls, [CONFLICT_PATH]);
  } finally {
    await cleanup();
    await cleanupStore();
  }
});

test("no longer renders recent activity after refresh", async () => {
  const { module, cleanup } = await loadEntry("src/sidebar-view.ts");
  const { SidebarView } = module;
  const { store, cleanup: cleanupStore } = await makeStore([]);
  try {
    const plugin = makePlugin(store);
    assert.ok(plugin.collabManager.getRecentActivity().length > 0);

    const root = renderSidebar(SidebarView, plugin);

    const lists = root.querySelectorAll(".oss-activity-list");
    assert.equal(lists.length, 0, "recent activity list must not be rendered");
  } finally {
    await cleanup();
    await cleanupStore();
  }
});

test("renders compact share, collaboration, and recycle management buttons instead of inline rows", async () => {
  const { module, cleanup } = await loadEntry("src/sidebar-view.ts");
  const { SidebarView } = module;
  const { store, cleanup: cleanupStore } = await makeStore([]);
  try {
    // 绑定仓库带有一篇分享文章与一条待处理协作
    const plugin = makePlugin(store);

    const root = renderSidebar(SidebarView, plugin);

    const shareButtons = root.querySelectorAll(".oss-sidebar-share-manager");
    const collabButtons = root.querySelectorAll(".oss-sidebar-collab-manager");
    const recycleButtons = root.querySelectorAll(".oss-sidebar-recycle-manager");
    assert.equal(shareButtons.length, 1);
    assert.equal(collabButtons.length, 1);
    assert.equal(recycleButtons.length, 1);
    assert.equal(root.querySelectorAll(".oss-sidebar-share").length, 0);
    assert.equal(root.querySelectorAll(".oss-sidebar-invite").length, 0);

    shareButtons[0].click();
    collabButtons[0].click();
    recycleButtons[0].click();
    assert.equal(plugin.openShareManagerCalls.length, 1);
    assert.equal(plugin.openCollabManagerCalls.length, 1);
    assert.equal(plugin.openRecycleManagerCalls.length, 1);
  } finally {
    await cleanup();
    await cleanupStore();
  }
});
