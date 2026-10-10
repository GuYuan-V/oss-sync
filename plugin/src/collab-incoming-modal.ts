import { App, Modal } from "obsidian";
import type { CollabEntry } from "./api";
import type OSSPlugin from "./main";

/** 收到的邀请详情窗口；按文章列出，pending 可接受或拒绝，accepted 可退出 */
export class CollabIncomingModal extends Modal {
  private entries: readonly CollabEntry[];

  constructor(app: App, private readonly plugin: OSSPlugin, entries: readonly CollabEntry[]) {
    super(app);
    this.entries = entries;
  }

  onOpen(): void {
    this.modalEl.addClass("oss-manager-modal", "oss-collab-detail-modal");
    this.titleEl.setText(this.plugin.t("collab.sectionIncoming"));
    this.render();
  }

  onClose(): void {
    this.contentEl.empty();
  }

  /** 刷新协作关系并重渲染；撤销的条目不显示 */
  private async load(): Promise<void> {
    this.contentEl.empty();
    this.contentEl.createDiv({ cls: "oss-sidebar-empty", text: this.plugin.t("collab.loading") });
    await this.plugin.collabManager.refresh();
    const username = this.plugin.settings.username;
    this.entries = this.plugin.collabManager.getCollaborations()
      .filter((entry) => entry.collaborator_username === username && entry.status !== "revoked");
    this.render();
  }

  /** 渲染文章列表；空列表给出空态 */
  private render(): void {
    this.contentEl.empty();
    if (this.entries.length === 0) {
      this.contentEl.createDiv({ cls: "oss-sidebar-empty", text: this.plugin.t("collab.empty") });
      return;
    }
    for (const entry of this.entries) this.renderEntry(entry);
  }

  /** 渲染单篇文章；名称过长省略，悬停显示全名 */
  private renderEntry(entry: CollabEntry): void {
    const row = this.contentEl.createDiv({ cls: "oss-sidebar-collab-row" });
    const name = row.createDiv({ cls: "oss-manager-path oss-ellipsis" });
    name.setText(entry.file_path);
    name.setAttribute("title", entry.file_path);
    row.createDiv({
      cls: "oss-manager-meta",
      text: this.plugin.t("collab.incomingFrom", { name: entry.owner_username }),
    });
    const actions = row.createDiv({ cls: "oss-sidebar-actions" });
    if (entry.status === "pending") {
      actions.createEl("button", { cls: "oss-collab-accept", text: this.plugin.t("sidebar.accept") })
        .addEventListener("click", () => void this.respond(entry, true));
      actions.createEl("button", { cls: "oss-collab-reject mod-warning", text: this.plugin.t("sidebar.reject") })
        .addEventListener("click", () => void this.respond(entry, false));
      return;
    }
    if (entry.status === "accepted") {
      actions.createEl("button", { cls: "oss-collab-leave mod-warning", text: this.plugin.t("collab.leave") })
        .addEventListener("click", () => void this.leave(entry));
    }
  }

  /** 接受或拒绝邀请，完成后重新拉取列表 */
  private async respond(entry: CollabEntry, accept: boolean): Promise<void> {
    await this.plugin.collabManager.respond(entry, accept);
    await this.load();
  }

  /** 退出已接受的协作，完成后重新拉取列表 */
  private async leave(entry: CollabEntry): Promise<void> {
    await this.plugin.collabManager.leave(entry);
    await this.load();
  }
}
