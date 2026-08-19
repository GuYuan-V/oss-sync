// 文件修改记录弹窗：展示指定路径在服务端的历史版本列表。
import { App, Modal, Notice } from "obsidian";
import type OSSPlugin from "./main";
import type { HistoryEntry } from "./api";
import { HistoryDetailModal } from "./history-detail-modal";

export class HistoryModal extends Modal {
  constructor(app: App, private plugin: OSSPlugin, private filePath: string) {
    super(app);
  }

  onOpen(): void {
    const { contentEl, titleEl } = this;
    titleEl.setText(this.plugin.t("history.title", { path: this.filePath }));
    contentEl.empty();
    contentEl.createDiv({ cls: "oss-history-status", text: this.plugin.t("history.loading") });
    void this.load();
  }

  onClose(): void {
    this.contentEl.empty();
  }

  private async load(): Promise<void> {
    const { contentEl } = this;
    const target = this.plugin.collabManager.getHistoryTarget(this.filePath) ?? {
      vaultID: this.plugin.settings.vaultId,
      path: this.filePath,
      canRestore: true,
    };
    const vaultID = target.vaultID;
    if (!vaultID) {
      contentEl.empty();
      new Notice(this.plugin.t("history.bindFirst"));
      return;
    }
    try {
      const result = await this.plugin.api.history(vaultID, target.path);
      contentEl.empty();
      if (result.history.length === 0) {
        contentEl.createDiv({ cls: "oss-history-status", text: this.plugin.t("history.empty") });
        return;
      }
      this.entries = result.history;
      for (const entry of result.history) {
        this.renderEntry(entry, target);
      }
    } catch (error) {
      contentEl.empty();
      contentEl.createDiv({
        cls: "oss-history-status",
        text: this.plugin.t("history.failed", { error: errorMessage(error, this.plugin) }),
      });
    }
  }

  private renderEntry(
    entry: HistoryEntry,
    target: { readonly vaultID: string; readonly canRestore: boolean }
  ): void {
    const { contentEl } = this;
    const container = contentEl.createDiv({ cls: "oss-history-entry" });
    container.createEl("time", {
      cls: "oss-history-time",
      text: new Date(entry.created_at).toLocaleString(
        this.plugin.getLanguage() === "zh" ? "zh-CN" : "en-US"
      ),
    });
    container.createEl("span", { cls: "oss-history-user", text: entry.username });
    container.createEl("span", { cls: "oss-history-device", text: entry.device_name || this.plugin.t("common.unknown") });
    container.createEl("button", {
      cls: "oss-history-view",
      text: this.plugin.t("history.view"),
    }).addEventListener("click", () => {
      new HistoryDetailModal(this.app, this.plugin, {
        entry,
        entries: this.entries,
        vaultID: target.vaultID,
        canRestore: target.canRestore,
      }).open();
    });
  }

  private entries: readonly HistoryEntry[] = [];
}

function errorMessage(error: unknown, plugin: OSSPlugin): string {
  return plugin.localizedError(error);
}
