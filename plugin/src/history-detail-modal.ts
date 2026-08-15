import { App, Modal, Notice } from "obsidian";
import type { HistoryDetail, HistoryEntry } from "./api";
import { collapseContextRows, type ConflictDiffRow } from "./conflict-diff";
import type OSSPlugin from "./main";

type HistoryDetailOptions = {
  readonly entry: HistoryEntry;
  readonly entries: readonly HistoryEntry[];
  readonly vaultID: string;
  readonly canRestore: boolean;
};

type ComparisonMode = "previous" | "current";

export class HistoryDetailModal extends Modal {
  private detail: HistoryDetail | null = null;

  constructor(
    app: App,
    private readonly plugin: OSSPlugin,
    private readonly options: HistoryDetailOptions
  ) {
    super(app);
  }

  onOpen(): void {
    this.modalEl.addClass("oss-history-detail-modal");
    this.titleEl.setText(this.plugin.t("history.detailTitle", { version: this.options.entry.version }));
    void this.loadSnapshot();
  }

  onClose(): void {
    this.contentEl.empty();
  }

  private async loadSnapshot(): Promise<void> {
    try {
      this.detail = await this.plugin.api.historyDetail(this.options.vaultID, this.options.entry.id, "last");
      this.render(this.detail, "previous");
    } catch (error) {
      this.renderFailure(error);
    }
  }

  private render(detail: HistoryDetail, mode: ComparisonMode): void {
    this.contentEl.empty();
    this.contentEl.createDiv({ cls: "oss-history-meta", text: detail.file_path });
    this.contentEl.createDiv({
      cls: "oss-history-comparison-status",
      text: this.plugin.t(mode === "previous" ? "history.viewingPrevious" : "history.viewingCurrent"),
    });
    const previousAvailable = this.options.entries.some(
      (candidate) => candidate.version === this.options.entry.version - 1 && candidate.has_snapshot
    );
    if (!detail.is_text || detail.content === undefined) {
      this.contentEl.createDiv({ cls: "oss-history-status", text: this.plugin.t("history.snapshotUnavailable") });
    } else {
      this.renderDiff(collapseContextRows(serverDiffRows(detail.diff)));
    }
    const actions = this.contentEl.createDiv({ cls: "oss-history-detail-actions" });
    const previousButton = actions.createEl("button", {
      cls: `oss-history-compare-previous${mode === "previous" ? " is-active" : ""}`,
      text: this.plugin.t("history.comparePrevious"),
    });
    previousButton.disabled = !previousAvailable;
    previousButton.addEventListener("click", () => this.render(this.detail ?? detail, "previous"));
    const currentButton = actions.createEl("button", {
      cls: `oss-history-compare-current${mode === "current" ? " is-active" : ""}`,
      text: this.plugin.t("history.compareCurrent"),
    });
    currentButton.disabled = !detail.is_text;
    currentButton.addEventListener("click", () => void this.compareCurrent());
    const restoreButton = actions.createEl("button", {
      cls: "oss-history-restore mod-warning",
      text: this.plugin.t("history.restore"),
    });
    restoreButton.disabled = !this.options.canRestore || !this.options.entry.has_snapshot;
    restoreButton.addEventListener("click", () => void this.restore());
  }

  private renderDiff(rows: readonly ConflictDiffRow[]): void {
    const preview = this.contentEl.createDiv({ cls: "oss-diff-preview" });
    if (rows.length === 0) {
      preview.createDiv({ cls: "oss-diff-empty", text: this.plugin.t("conflict.noChanges") });
      return;
    }
    for (const row of rows) {
      if (row.kind === "omitted") {
        preview.createDiv({
          cls: "oss-diff-row is-omitted",
          text: this.plugin.t("conflict.omittedLines", { count: row.count }),
        });
        continue;
      }
      const line = preview.createDiv({ cls: `oss-diff-row is-${row.kind}` });
      line.createDiv({
        cls: "oss-diff-marker",
        text: row.kind === "removed" ? "-" : row.kind === "added" ? "+" : "",
      });
      line.createDiv({ cls: "oss-diff-text", text: row.text });
    }
  }

  private async compareCurrent(): Promise<void> {
    try {
      const detail = await this.plugin.api.historyDetail(this.options.vaultID, this.options.entry.id, "current");
      this.render(detail, "current");
    } catch (error) {
      this.renderFailure(error);
    }
  }

  private async restore(): Promise<void> {
    try {
      await this.plugin.api.historyRestore(this.options.vaultID, this.options.entry.id);
      const synced = await this.plugin.syncEngine.runOnce({ forceFull: true });
      new Notice(this.plugin.t(synced ? "history.restoreSuccess" : "history.restoreSyncFailed"));
      this.close();
    } catch (error) {
      new Notice(this.plugin.t("history.restoreFailed", { error: errorMessage(error, this.plugin) }));
    }
  }

  private renderFailure(error: unknown): void {
    if (this.detail) {
      this.render(this.detail, "previous");
    } else {
      this.contentEl.empty();
    }
    this.contentEl.createDiv({
      cls: "oss-history-status",
      text: this.plugin.t("history.failed", { error: errorMessage(error, this.plugin) }),
    });
  }
}

function serverDiffRows(lines: readonly string[]): ConflictDiffRow[] {
  return lines.map((line) => {
    const marker = line.slice(0, 1);
    if (marker === "-") return { kind: "removed", text: line.slice(1) };
    if (marker === "+") return { kind: "added", text: line.slice(1) };
    return { kind: "context", text: line.slice(1) };
  });
}

function errorMessage(error: unknown, plugin: OSSPlugin): string {
  return plugin.localizedError(error);
}
