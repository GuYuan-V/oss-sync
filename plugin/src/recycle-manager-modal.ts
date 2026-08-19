import { App, Modal, Notice } from "obsidian";
import type { RecycleBinFile } from "./api";
import type OSSPlugin from "./main";

export class RecycleManagerModal extends Modal {
  constructor(app: App, private readonly plugin: OSSPlugin) {
    super(app);
  }

  onOpen(): void {
    this.modalEl.addClass("oss-manager-modal");
    this.titleEl.setText(this.plugin.t("recycle.title"));
    void this.load();
  }

  onClose(): void {
    this.contentEl.empty();
  }

  private async load(): Promise<void> {
    const vaultID = this.plugin.settings.vaultId;
    this.contentEl.empty();
    if (!vaultID) {
      this.contentEl.createDiv({ cls: "oss-sidebar-empty", text: this.plugin.t("recycle.bindFirst") });
      return;
    }
    this.contentEl.createDiv({ cls: "oss-sidebar-empty", text: this.plugin.t("recycle.loading") });
    try {
      const result = await this.plugin.api.recycleList(vaultID);
      this.contentEl.empty();
      if (result.files.length === 0) {
        this.contentEl.createDiv({ cls: "oss-sidebar-empty", text: this.plugin.t("recycle.empty") });
        return;
      }
      for (const file of result.files) {
        this.renderFile(vaultID, file);
      }
    } catch (error) {
      const message = error instanceof Error ? error.message : this.plugin.t("common.unknownError");
      this.contentEl.empty();
      this.contentEl.createDiv({
        cls: "oss-sidebar-empty",
        text: this.plugin.t("recycle.loadFailed", { error: message }),
      });
    }
  }

  private renderFile(vaultID: string, file: RecycleBinFile): void {
    const row = this.contentEl.createDiv({ cls: "oss-sidebar-recycle-row" });
    row.createDiv({ cls: "oss-manager-path", text: file.path });
    row.createDiv({
      cls: "oss-manager-meta",
      text: this.plugin.t("recycle.meta", {
        size: file.size,
        expires: new Date(file.expires_at).toLocaleString(),
      }),
    });
    const actions = row.createDiv({ cls: "oss-sidebar-actions" });
    const restore = actions.createEl("button", {
      cls: "oss-recycle-restore",
      text: this.plugin.t("recycle.restore"),
    });
    if (!file.can_restore) {
      restore.setAttribute("disabled", "true");
    }
    restore.addEventListener("click", () => void this.restore(vaultID, file.id));
    actions.createEl("button", {
      cls: "oss-recycle-delete mod-warning",
      text: this.plugin.t("recycle.delete"),
    }).addEventListener("click", () => void this.delete(vaultID, file.id, file.path));
  }

  private async restore(vaultID: string, fileID: number): Promise<void> {
    try {
      await this.plugin.api.recycleRestore(vaultID, fileID);
      await this.load();
    } catch (error) {
      this.showActionError(error);
    }
  }

  private async delete(vaultID: string, fileID: number, path: string): Promise<void> {
    if (!window.confirm(this.plugin.t("recycle.deleteConfirm", { path }))) return;
    try {
      await this.plugin.api.recycleDelete(vaultID, fileID);
      await this.load();
    } catch (error) {
      this.showActionError(error);
    }
  }

  private showActionError(error: unknown): void {
    const message = error instanceof Error ? error.message : this.plugin.t("common.unknownError");
    new Notice(this.plugin.t("recycle.actionFailed", { error: message }));
  }
}
