import { App, Modal } from "obsidian";
import type { CollabEntry } from "./api";
import { CollabCollaboratorsModal } from "./collab-collaborators-modal";
import type OSSPlugin from "./main";

/** 协作管理弹窗；上半区为待回应的邀请，下半区按文件聚合已发出的邀请 */
export class CollabManagerModal extends Modal {
  private readonly selectedArticleKeys = new Set<string>();

  constructor(app: App, private readonly plugin: OSSPlugin) {
    super(app);
  }

  onOpen(): void {
    this.modalEl.addClass("oss-manager-modal");
    this.titleEl.setText(this.plugin.t("sidebar.invitations"));
    void this.load();
  }

  onClose(): void {
    this.contentEl.empty();
  }

  /** 刷新协作关系并重渲染；收到的邀请与发出的邀请分组展示，发出的按 `vault_id:file_id` 聚合 */
  private async load(): Promise<void> {
    this.contentEl.empty();
    this.contentEl.createDiv({ cls: "oss-sidebar-empty", text: this.plugin.t("collab.loading") });
    await this.plugin.collabManager.refresh();
    this.contentEl.empty();
    const collaborations = this.plugin.collabManager.getCollaborations();
    if (collaborations.length === 0) {
      this.contentEl.createDiv({ cls: "oss-sidebar-empty", text: this.plugin.t("collab.empty") });
      return;
    }
    // 收到的邀请：对方是当前用户且未被撤销
    for (const entry of collaborations) {
      if (entry.collaborator_username !== this.plugin.settings.username) continue;
      if (entry.status === "revoked") continue;
      this.renderIncoming(entry);
    }
    const byArticle = new Map<string, CollabEntry[]>();
    for (const entry of collaborations) {
      if (entry.collaborator_username === this.plugin.settings.username || entry.status === "revoked") continue;
      const key = `${entry.vault_id}:${entry.file_id}`;
      const entries = byArticle.get(key) ?? [];
      entries.push(entry);
      byArticle.set(key, entries);
    }
    if (byArticle.size > 0) this.renderBulkActions(byArticle);
    for (const [key, entries] of byArticle) this.renderArticle(key, entries);
  }

  /** 渲染单条收到的邀请；pending 提供接受与拒绝，accepted 提供退出 */
  private renderIncoming(entry: CollabEntry): void {
    const row = this.contentEl.createDiv({ cls: "oss-sidebar-collab-row" });
    row.createDiv({ cls: "oss-manager-path", text: entry.file_path });
    row.createDiv({
      cls: "oss-manager-meta",
      text: this.plugin.t("collab.incomingMeta", { name: entry.owner_username, status: entry.status }),
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

  /** 渲染批量取消区；按钮随勾选数量更新可用性与文案 */
  private renderBulkActions(byArticle: ReadonlyMap<string, readonly CollabEntry[]>): void {
    const actions = this.contentEl.createDiv({ cls: "oss-sidebar-actions oss-collab-article-bulk-actions" });
    const cancelSelected = actions.createEl("button", {
      cls: "oss-collab-cancel-selected mod-warning",
      text: this.plugin.t("collab.cancelSelectedArticles", { count: this.selectedArticleKeys.size }),
    });
    cancelSelected.disabled = this.selectedArticleKeys.size === 0;
    cancelSelected.addEventListener("click", () => void this.cancelArticles(byArticle));
  }

  /** 渲染单个文件及其协作者；勾选状态跨重渲染保留在 selectedArticleKeys 中 */
  private renderArticle(key: string, entries: readonly CollabEntry[]): void {
    const article = entries[0];
    if (!article) return;
    const row = this.contentEl.createDiv({ cls: "oss-sidebar-collab-row oss-collab-article-row" });
    const label = row.createEl("label", { cls: "oss-manager-select" });
    const checkbox = label.createEl("input", { attr: { type: "checkbox" } });
    checkbox.checked = this.selectedArticleKeys.has(key);
    checkbox.addEventListener("change", () => {
      if (checkbox.checked) this.selectedArticleKeys.add(key);
      else this.selectedArticleKeys.delete(key);
      this.renderArticles();
    });
    label.createEl("span", { text: article.file_path });
    row.createDiv({ cls: "oss-manager-meta", text: this.plugin.t("collab.collaboratorCount", { count: entries.length }) });
    const actions = row.createDiv({ cls: "oss-sidebar-actions" });
    actions.createEl("button", {
      cls: "oss-collab-view",
      text: this.plugin.t("collab.viewCollaborators"),
    }).addEventListener("click", () => new CollabCollaboratorsModal(this.app, this.plugin, entries).open());
    actions.createEl("button", {
      cls: "oss-collab-cancel-article mod-warning",
      text: this.plugin.t("collab.cancelArticle"),
    }).addEventListener("click", () => void this.cancelArticle(entries));
  }

  /** 勾选变更后整体重渲染 */
  private renderArticles(): void {
    void this.load();
  }

  /** 撤销该文件的全部协作者 */
  private async cancelArticle(entries: readonly CollabEntry[]): Promise<void> {
    await Promise.all(entries.map((entry) => this.plugin.collabManager.revoke(entry)));
    await this.load();
  }

  /** 撤销勾选中的全部协作者，完成后清空勾选 */
  private async cancelArticles(byArticle: ReadonlyMap<string, readonly CollabEntry[]>): Promise<void> {
    // 勾选以文件为粒度，需展开成协作者列表后逐条撤销
    const selected = [...this.selectedArticleKeys].flatMap((key) => byArticle.get(key) ?? []);
    await Promise.all(selected.map((entry) => this.plugin.collabManager.revoke(entry)));
    this.selectedArticleKeys.clear();
    await this.load();
  }

  /** 接受或拒绝单条邀请 */
  private async respond(entry: CollabEntry, accept: boolean): Promise<void> {
    await this.plugin.collabManager.respond(entry, accept);
    await this.load();
  }

  /** 退出已接受的协作 */
  private async leave(entry: CollabEntry): Promise<void> {
    await this.plugin.collabManager.leave(entry);
    await this.load();
  }
}
