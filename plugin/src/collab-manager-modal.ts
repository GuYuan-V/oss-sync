import { App, Modal } from "obsidian";
import type { CollabEntry } from "./api";
import { CollabCollaboratorsModal } from "./collab-collaborators-modal";
import type OSSPlugin from "./main";

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

  private renderBulkActions(byArticle: ReadonlyMap<string, readonly CollabEntry[]>): void {
    const actions = this.contentEl.createDiv({ cls: "oss-sidebar-actions oss-collab-article-bulk-actions" });
    const cancelSelected = actions.createEl("button", {
      cls: "oss-collab-cancel-selected mod-warning",
      text: this.plugin.t("collab.cancelSelectedArticles", { count: this.selectedArticleKeys.size }),
    });
    cancelSelected.disabled = this.selectedArticleKeys.size === 0;
    cancelSelected.addEventListener("click", () => void this.cancelArticles(byArticle));
  }

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

  private renderArticles(): void {
    void this.load();
  }

  private async cancelArticle(entries: readonly CollabEntry[]): Promise<void> {
    await Promise.all(entries.map((entry) => this.plugin.collabManager.revoke(entry)));
    await this.load();
  }

  private async cancelArticles(byArticle: ReadonlyMap<string, readonly CollabEntry[]>): Promise<void> {
    const selected = [...this.selectedArticleKeys].flatMap((key) => byArticle.get(key) ?? []);
    await Promise.all(selected.map((entry) => this.plugin.collabManager.revoke(entry)));
    this.selectedArticleKeys.clear();
    await this.load();
  }

  private async respond(entry: CollabEntry, accept: boolean): Promise<void> {
    await this.plugin.collabManager.respond(entry, accept);
    await this.load();
  }

  private async leave(entry: CollabEntry): Promise<void> {
    await this.plugin.collabManager.leave(entry);
    await this.load();
  }
}
