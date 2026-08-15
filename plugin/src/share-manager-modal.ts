import { App, Modal, Notice } from "obsidian";
import type OSSPlugin from "./main";
import { renderShareRows } from "./share-section";

export class ShareManagerModal extends Modal {
  constructor(app: App, private readonly plugin: OSSPlugin) {
    super(app);
  }

  onOpen(): void {
    this.modalEl.addClass("oss-manager-modal");
    this.titleEl.setText(this.plugin.t("sidebar.shares"));
    void this.load();
  }

  onClose(): void {
    this.contentEl.empty();
  }

  private async load(): Promise<void> {
    this.contentEl.empty();
    this.contentEl.createDiv({
      cls: "oss-sidebar-empty",
      text: this.plugin.t("sidebar.sharesLoading"),
    });
    try {
      const result = await this.plugin.api.listShares();
      this.contentEl.empty();
      renderShareRows(this.contentEl, this.plugin, {
        shares: result.shares,
        onChanged: () => this.load(),
      });
    } catch (error) {
      const message = error instanceof Error ? error.message : this.plugin.t("common.unknownError");
      this.contentEl.empty();
      this.contentEl.createDiv({
        cls: "oss-sidebar-empty",
        text: this.plugin.t("sidebar.sharesLoadFailed", { error: message }),
      });
      new Notice(this.plugin.t("sidebar.sharesLoadFailed", { error: message }));
    }
  }
}
