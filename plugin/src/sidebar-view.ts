// 侧边栏视图：展示同步状态、分享管理、协作邀请与未解决冲突。
import { ItemView, Notice, setIcon, WorkspaceLeaf } from "obsidian";
import type OSSPlugin from "./main";

export const SIDEBAR_VIEW_TYPE = "oss-sync-sidebar";

export class SidebarView extends ItemView {
  constructor(leaf: WorkspaceLeaf, private plugin: OSSPlugin) {
    super(leaf);
  }

  getViewType(): string {
    return SIDEBAR_VIEW_TYPE;
  }

  getDisplayText(): string {
    return this.plugin.t("sidebar.title");
  }

  getIcon(): string {
    return "refresh-cw";
  }

  async onOpen(): Promise<void> {
    this.contentEl.addClass("oss-sidebar");
    this.refresh();
  }

  async onClose(): Promise<void> {
    this.contentEl.removeClass("oss-sidebar");
    this.contentEl.empty();
  }

  /** 重新渲染整个侧边栏。 */
  refresh(): void {
    const { contentEl } = this;
    contentEl.empty();
    this.renderHeader();
    this.renderStatus();
    this.renderActions();
    this.renderManagers();
    this.renderRecycle();
    this.renderConflicts();
    this.renderConsoleLink();
  }

  reloadShares(): void {
    this.refresh();
  }

  private renderHeader(): void {
    const { contentEl, plugin } = this;
    const header = contentEl.createDiv({ cls: "oss-sidebar-header" });
    const icon = header.createDiv({ cls: "oss-sidebar-mark", attr: { "aria-hidden": "true" } });
    setIcon(icon, "refresh-cw");

    const copy = header.createDiv({ cls: "oss-sidebar-heading" });
    copy.createEl("h3", { text: plugin.t("sidebar.title") });
    copy.createDiv({ cls: "oss-sidebar-summary", text: plugin.t("sidebar.summary") });

    const closeLabel = plugin.t("sidebar.close");
    const closeButton = header.createEl("button", {
      cls: "clickable-icon oss-sidebar-close",
      attr: { type: "button", "aria-label": closeLabel, title: closeLabel },
    });
    setIcon(closeButton, "x");
    closeButton.addEventListener("click", () => this.leaf.detach());
  }

  private renderStatus(): void {
    const { plugin, contentEl } = this;
    const status = this.createSection(plugin.t("sidebar.status"));
    status.createDiv({
      cls: "oss-sidebar-status-line",
      text: plugin.t("sidebar.user", {
        value: plugin.settings.username || plugin.t("common.notLoggedIn"),
      }),
    });
    status.createDiv({
      cls: "oss-sidebar-status-line",
      text: plugin.t("sidebar.device", {
        value: plugin.settings.deviceName || plugin.t("common.unset"),
      }),
    });
    status.createDiv({
      cls: "oss-sidebar-status-line",
      text: plugin.t("sidebar.vault", {
        value: plugin.settings.vaultName || plugin.t("sidebar.unbound"),
      }),
    });
    status.createDiv({ cls: "oss-sidebar-status-line", text: plugin.collabManager.getTransportStatus() });
    status.createDiv({
      cls: "oss-sidebar-status-line",
      text: plugin.t("sidebar.syncMode", { value: plugin.syncEngine.getEffectiveModeLabel() }),
    });
  }

  private renderActions(): void {
    const { plugin, contentEl } = this;
    const actions = this.createSection(plugin.t("sidebar.actions"));
    const cluster = actions.createDiv({ cls: "oss-sidebar-actions oss-sidebar-actions--two-column" });
    cluster.createEl("button", { text: plugin.t("sidebar.sync"), cls: "mod-cta" })
      .addEventListener("click", () => {
        if (!plugin.settings.vaultId) {
          new Notice(plugin.t("notice.bindVaultFirst"));
          return;
        }
        void plugin.syncEngine.runOnce({ forceFull: true });
      });
    cluster.createEl("button", { text: plugin.t("sidebar.settings") })
      .addEventListener("click", () => {
        plugin.openSettings();
      });
  }

  private renderManagers(): void {
    const { plugin } = this;
    const section = this.createSection(plugin.t("sidebar.management"));
    if (!plugin.settings.vaultId || !plugin.api.hasToken()) {
      section.createDiv({ cls: "oss-sidebar-empty", text: plugin.t("sidebar.sharesUnavailable") });
      return;
    }
    const actions = section.createDiv({ cls: "oss-sidebar-actions oss-sidebar-actions--stacked" });
    actions.createEl("button", {
      cls: "oss-sidebar-manager-button oss-sidebar-share-manager",
      text: plugin.t("sidebar.manageShares"),
    }).addEventListener("click", () => plugin.openShareManager());
    actions.createEl("button", {
      cls: "oss-sidebar-manager-button oss-sidebar-collab-manager",
      text: plugin.t("sidebar.manageCollaboration"),
    }).addEventListener("click", () => plugin.openCollabManager());
  }

  private renderRecycle(): void {
    const { plugin } = this;
    const section = this.createSection(plugin.t("sidebar.manageRecycle"));
    if (!plugin.settings.vaultId || !plugin.api.hasToken()) {
      section.createDiv({ cls: "oss-sidebar-empty", text: plugin.t("recycle.bindFirst") });
      return;
    }
    section.createEl("button", {
      cls: "oss-sidebar-manager-button oss-sidebar-recycle-manager",
      text: plugin.t("sidebar.manageRecycle"),
    }).addEventListener("click", () => plugin.openRecycleManager());
  }

  private renderConflicts(): void {
    const { plugin, contentEl } = this;
    const conflicts = plugin.baseline.conflicts();
    if (conflicts.length === 0) return;
    const section = this.createSection(plugin.t("sidebar.conflicts"));
    for (const entry of conflicts) {
      const row = section.createDiv({ cls: "oss-sidebar-conflict" });
      row.createDiv({ cls: "oss-sidebar-conflict-path", text: entry.path });
      const actions = row.createDiv({ cls: "oss-sidebar-actions" });
      actions.createEl("button", { text: plugin.t("sidebar.resolveConflict"), cls: "mod-cta" })
        .addEventListener("click", () => plugin.openConflictModal(entry.path));
    }
  }

  private renderConsoleLink(): void {
    const { plugin, contentEl } = this;
    const url = plugin.settings.serverUrl.replace(/\/$/, "") + "/dashboard";
    contentEl.createEl("a", {
      cls: "oss-sidebar-console-link",
      text: plugin.t("sidebar.openConsole"),
      href: url,
    });
  }

  private createSection(title: string): HTMLElement {
    const section = this.contentEl.createEl("section", { cls: "oss-sidebar-card" });
    section.createEl("h4", { text: title });
    return section.createDiv({ cls: "oss-sidebar-section" });
  }
}
