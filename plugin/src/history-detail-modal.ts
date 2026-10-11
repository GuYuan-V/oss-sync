import { App, Modal, Notice } from "obsidian";
import type { HistoryDetail, HistoryEntry } from "./api";
import { collapseContextRows, type ConflictDiffRow } from "./conflict-diff";
import type OSSPlugin from "./main";

/** 详情弹窗入参；entries 用于判断上一版快照是否可用 */
type HistoryDetailOptions = {
  readonly entry: HistoryEntry;
  readonly entries: readonly HistoryEntry[];
  readonly vaultID: string;
  readonly canRestore: boolean;
};

/** 对比基准；previous 与该版本上一版比较，current 与当前版本比较 */
type ComparisonMode = "previous" | "current";

/** 历史版本详情弹窗；支持切换对比基准与恢复该版本 */
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

  /** 打开时默认加载当前文件对比 */
  private async loadSnapshot(): Promise<void> {
    try {
      this.detail = await this.plugin.api.historyDetail(this.options.vaultID, this.options.entry.id, "current");
      this.render(this.detail, "current");
    } catch (error) {
      this.renderFailure(error);
    }
  }

  /** 渲染详情与操作按钮；非文本或缺少快照时禁用差异与恢复 */
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
    previousButton.addEventListener("click", () => void this.comparePrevious());
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

  /** 渲染折叠后的服务端差异行 */
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
      // 上下文行无标记符号，靠缩进与颜色区分增删
      line.createDiv({
        cls: "oss-diff-marker",
        text: row.kind === "removed" ? "-" : row.kind === "added" ? "+" : "",
      });
      line.createDiv({ cls: "oss-diff-text", text: row.text });
    }
  }

  /** 切换为与上一版对比；diff 已由服务端按 mode 计算，需重新请求 */
  private async comparePrevious(): Promise<void> {
    if (this.detail !== null && this.detail.mode === "last") {
      this.render(this.detail, "previous");
      return;
    }
    try {
      const detail = await this.plugin.api.historyDetail(this.options.vaultID, this.options.entry.id, "last");
      this.detail = detail;
      this.render(detail, "previous");
    } catch (error) {
      this.renderFailure(error);
    }
  }

  /** 切换为与当前版本对比；diff 已由服务端按 mode 计算，需重新请求 */
  private async compareCurrent(): Promise<void> {
    if (this.detail !== null && this.detail.mode === "current") {
      this.render(this.detail, "current");
      return;
    }
    try {
      const detail = await this.plugin.api.historyDetail(this.options.vaultID, this.options.entry.id, "current");
      this.detail = detail;
      this.render(detail, "current");
    } catch (error) {
      this.renderFailure(error);
    }
  }

  /** 恢复该版本并触发一次全量同步；同步未完成时单独提示 */
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

  /** 加载失败时保留已渲染内容，仅在详情末尾追加错误提示 */
  private renderFailure(error: unknown): void {
    if (this.detail) {
      this.render(this.detail, "current");
    } else {
      this.contentEl.empty();
    }
    this.contentEl.createDiv({
      cls: "oss-history-status",
      text: this.plugin.t("history.failed", { error: errorMessage(error, this.plugin) }),
    });
  }
}

/** 把服务端 diff 文本按首字符前缀转为差异行；无前缀的行按上下文处理 */
function serverDiffRows(lines: readonly string[]): ConflictDiffRow[] {
  return lines.map((line) => {
    // 前缀已从文本中剥离，行内容不含首字符
    const marker = line.slice(0, 1);
    if (marker === "-") return { kind: "removed", text: line.slice(1) };
    if (marker === "+") return { kind: "added", text: line.slice(1) };
    return { kind: "context", text: line.slice(1) };
  });
}

/** 统一走插件本地化入口，保证错误文案与 Obsidian 界面语言一致 */
function errorMessage(error: unknown, plugin: OSSPlugin): string {
  return plugin.localizedError(error);
}
