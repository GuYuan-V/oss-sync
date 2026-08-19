import { App, Modal } from "obsidian";
import type { CollabEntry } from "./api";
import type OSSPlugin from "./main";

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

  private renderEntry(entry: CollabEntry, revokeSelected: HTMLButtonElement): void {
    const row = this.contentEl.createDiv({ cls: "oss-sidebar-collab-row" });
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
    row.createEl("button", { cls: "oss-collab-revoke mod-warning", text: this.plugin.t("collab.revoke") })
      .addEventListener("click", () => void this.revoke(entry));
  }

  private async revoke(entry: CollabEntry): Promise<void> {
    await this.plugin.collabManager.revoke(entry);
    this.close();
  }

  private async revokeSelected(): Promise<void> {
    const selected = this.entries.filter((entry) => this.selectedIDs.has(entry.id));
    await Promise.all(selected.map((entry) => this.plugin.collabManager.revoke(entry)));
    this.close();
  }
}
