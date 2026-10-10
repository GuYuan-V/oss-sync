import { App, Modal, Notice, Setting } from "obsidian";
import type { CollabEntry } from "./api";
import { localizeError } from "./localized-error";
import type OSSPlugin from "./main";

/** 单文件协作者弹窗；逐条撤销、按勾选批量撤销，并在同一行切换协作者的历史查看权限 */
export class CollabCollaboratorsModal extends Modal {
  private readonly selectedIDs = new Set<number>();

  constructor(app: App, private readonly plugin: OSSPlugin, private readonly entries: readonly CollabEntry[]) {
    super(app);
  }

  onOpen(): void {
    this.modalEl.addClass("oss-manager-modal", "oss-collaborators-modal");
    this.titleEl.setText(this.plugin.t("collab.collaboratorsTitle", { path: this.entries[0]?.file_path ?? "" }));
    this.render();
  }

  onClose(): void {
    this.contentEl.empty();
  }

  /** 渲染协作者列表与批量撤销按钮；空列表时只渲染按钮 */
  private render(): void {
    this.contentEl.empty();
    const actions = this.contentEl.createDiv({ cls: "oss-sidebar-actions oss-collaborators-actions" });
    const revokeSelected = actions.createEl("button", {
      cls: "oss-collab-revoke-selected mod-warning",
      text: this.plugin.t("collab.revokeSelected", { count: this.selectedIDs.size }),
    });
    revokeSelected.disabled = this.selectedIDs.size === 0;
    revokeSelected.addEventListener("click", () => void this.revokeSelected());
    for (const entry of this.entries) this.renderEntry(entry, revokeSelected);
  }

  /** 渲染单条协作者；勾选变更只就地刷新批量按钮，历史查看开关与该用户同行 */
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
    const toggleLabel = row.createDiv({ cls: "oss-collaborator-history-toggle" });
    new Setting(toggleLabel)
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
      this.render();
    }
  }

  /** 撤销单条协作者 */
  private async revoke(entry: CollabEntry): Promise<void> {
    await this.plugin.collabManager.revoke(entry);
    this.close();
  }

  /** 撤销勾选中的协作者 */
  private async revokeSelected(): Promise<void> {
    const selected = this.entries.filter((entry) => this.selectedIDs.has(entry.id));
    await Promise.all(selected.map((entry) => this.plugin.collabManager.revoke(entry)));
    this.close();
  }
}

/** 把错误本地化为用户可读文本 */
function errorMessage(error: unknown, plugin: OSSPlugin): string {
  return localizeError(error, plugin.t.bind(plugin), plugin.t("common.unknownError"));
}
