import { App, Modal, Notice, Setting } from "obsidian";
import type { CollabEntry } from "./api";
import { localizeError } from "./localized-error";
import type OSSPlugin from "./main";

/** 发出的邀请详情窗口；按协作者列出，支持多选撤销与逐个切换历史查看权限 */
export class CollabOutgoingModal extends Modal {
  private readonly selectedIDs = new Set<number>();
  private entries: readonly CollabEntry[];

  constructor(app: App, private readonly plugin: OSSPlugin, entries: readonly CollabEntry[]) {
    super(app);
    this.entries = entries;
  }

  onOpen(): void {
    this.modalEl.addClass("oss-manager-modal", "oss-collab-detail-modal");
    this.titleEl.setText(this.plugin.t("collab.sectionOutgoing"));
    this.render();
  }

  onClose(): void {
    this.contentEl.empty();
  }

  /** 刷新协作关系并重渲染；撤销的条目不显示 */
  private async load(): Promise<void> {
    await this.plugin.collabManager.refresh();
    const username = this.plugin.settings.username;
    const all = this.plugin.collabManager.getCollaborations()
      .filter((entry) => entry.collaborator_username !== username && entry.status !== "revoked");
    const keys = new Set(this.entries.map((entry) => `${entry.vault_id}:${entry.file_id}`));
    this.entries = all.filter((entry) => keys.has(`${entry.vault_id}:${entry.file_id}`));
    this.render();
  }

  /** 渲染协作者列表与批量撤销按钮；空列表只给出空态 */
  private render(): void {
    this.contentEl.empty();
    if (this.entries.length === 0) {
      this.contentEl.createDiv({ cls: "oss-sidebar-empty", text: this.plugin.t("collab.empty") });
      return;
    }
    const actions = this.contentEl.createDiv({ cls: "oss-sidebar-actions oss-collaborators-actions" });
    const revokeSelected = actions.createEl("button", {
      cls: "oss-collab-revoke-selected mod-warning",
      text: this.plugin.t("collab.revokeSelected", { count: this.selectedIDs.size }),
    });
    revokeSelected.disabled = this.selectedIDs.size === 0;
    revokeSelected.addEventListener("click", () => void this.revokeSelected(revokeSelected));
    for (const entry of this.entries) this.renderEntry(entry, revokeSelected);
  }

  /** 渲染单个协作者；勾选、名称、历史查看开关与撤销按钮同行 */
  private renderEntry(entry: CollabEntry, revokeSelected: HTMLButtonElement): void {
    const row = this.contentEl.createDiv({ cls: "oss-sidebar-collab-row oss-collaborator-row" });
    const label = row.createEl("label", { cls: "oss-manager-select" });
    const checkbox = label.createEl("input", { attr: { type: "checkbox" } });
    checkbox.checked = this.selectedIDs.has(entry.id);
    checkbox.addEventListener("change", () => {
      if (checkbox.checked) this.selectedIDs.add(entry.id);
      else this.selectedIDs.delete(entry.id);
      revokeSelected.disabled = this.selectedIDs.size === 0;
      revokeSelected.setText(this.plugin.t("collab.revokeSelected", { count: this.selectedIDs.size }));
    });
    label.createEl("span", { text: entry.collaborator_username });
    row.createDiv({ cls: "oss-manager-meta", text: entry.status });
    const toggleHost = row.createDiv({ cls: "oss-collaborator-history-toggle" });
    new Setting(toggleHost)
      .setName(this.plugin.t("collab.allowHistoryView"))
      .addToggle((toggle) =>
        toggle
          .setValue(entry.allow_history_view)
          .setDisabled(entry.status !== "accepted")
          .onChange((value) => void this.setHistoryView(entry, value))
      );
    row.createEl("button", { cls: "oss-collab-revoke mod-warning", text: this.plugin.t("collab.revoke") })
      .addEventListener("click", () => void this.revoke(entry));
  }

  /** 切换协作者的历史查看权限；失败时还原列表以反映服务端真实状态 */
  private async setHistoryView(entry: CollabEntry, allow: boolean): Promise<void> {
    try {
      await this.plugin.collabManager.setHistoryView(entry, allow);
    } catch (error) {
      new Notice(this.plugin.t("collab.historyViewFailed", { error: errorMessage(error, this.plugin) }));
      await this.load();
    }
  }

  /** 撤销单条协作者 */
  private async revoke(entry: CollabEntry): Promise<void> {
    await this.plugin.collabManager.revoke(entry);
    this.selectedIDs.delete(entry.id);
    await this.load();
  }

  /** 撤销勾选中的协作者，完成后清空勾选 */
  private async revokeSelected(revokeSelected: HTMLButtonElement): Promise<void> {
    const selected = this.entries.filter((entry) => this.selectedIDs.has(entry.id));
    await Promise.all(selected.map((entry) => this.plugin.collabManager.revoke(entry)));
    this.selectedIDs.clear();
    await this.load();
  }
}

/** 把错误本地化为用户可读文本 */
function errorMessage(error: unknown, plugin: OSSPlugin): string {
  return localizeError(error, plugin.t.bind(plugin), plugin.t("common.unknownError"));
}
