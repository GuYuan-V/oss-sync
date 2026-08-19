import { App, Modal, Notice, Setting, TFile } from "obsidian";
import type OSSPlugin from "./main";
import type { OSSApiClient } from "./api";
import { buildConflictDiff, LineCapacityExceededError } from "./conflict-diff";

export type ConflictResolution = "accept_remote" | "force_local" | "keep_both";

export class ConflictModal extends Modal {
  private remoteContent = "";

  constructor(
    app: App,
    private plugin: OSSPlugin,
    private api: OSSApiClient,
    private file: TFile,
    remoteContent: string,
    private onResolved: (r: ConflictResolution) => Promise<void>
  ) {
    super(app);
    this.remoteContent = remoteContent;
  }

  async onOpen(): Promise<void> {
    const { contentEl, titleEl } = this;
    this.modalEl.addClass("oss-conflict-modal");
    titleEl.empty();
    appendTextWithPathBreaks(titleEl, this.plugin.t("conflict.title", { path: this.file.path }));

    const preview = contentEl.createDiv({ cls: "oss-diff-preview" });
    const localContent = await this.app.vault.read(this.file);
    try {
      const rows = buildConflictDiff(localContent, this.remoteContent);
      if (rows.length === 0) {
        preview.createDiv({ cls: "oss-diff-empty", text: this.plugin.t("conflict.noChanges") });
      }
      for (const row of rows) {
        switch (row.kind) {
          case "omitted":
            preview.createDiv({
              cls: "oss-diff-row is-omitted",
              text: this.plugin.t("conflict.omittedLines", { count: row.count }),
            });
            break;
          case "context":
          case "removed":
          case "added": {
            const diffRow = preview.createDiv({ cls: `oss-diff-row is-${row.kind}` });
            const marker = row.kind === "removed" ? "-" : row.kind === "added" ? "+" : "";
            diffRow.createSpan({ cls: "oss-diff-marker", text: marker });
            const textSpan = diffRow.createSpan({ cls: "oss-diff-text" });
            appendTextWithPathBreaks(textSpan, row.text);
            break;
          }
          default:
            assertNever(row);
        }
      }
    } catch (error: unknown) {
      const message = error instanceof LineCapacityExceededError
        ? error.message
        : error instanceof Error
          ? error.message
          : this.plugin.t("common.unknownError");
      const failureText = this.plugin.t("conflict.diffFailed", { error: message });
      new Notice(failureText);
      preview.createDiv({ cls: "oss-diff-empty", text: failureText });
    }

    new Setting(contentEl)
      .setName(this.plugin.t("conflict.choose"))
      .setHeading();

    new Setting(contentEl)
      .setName(this.plugin.t("conflict.acceptRemote"))
      .setDesc(this.plugin.t("conflict.acceptRemoteDesc"))
      .addButton((b) =>
        b.setButtonText(this.plugin.t("conflict.acceptRemoteButton")).onClick(() => this.resolve("accept_remote"))
      );

    new Setting(contentEl)
      .setName(this.plugin.t("conflict.forceLocal"))
      .setDesc(this.plugin.t("conflict.forceLocalDesc"))
      .addButton((b) =>
        b.setButtonText(this.plugin.t("conflict.forceLocalButton")).setWarning().onClick(() => this.resolve("force_local"))
      );

    new Setting(contentEl)
      .setName(this.plugin.t("conflict.keepBoth"))
      .setDesc(this.plugin.t("conflict.keepBothDesc"))
      .addButton((b) =>
        b.setButtonText(this.plugin.t("conflict.keepBothButton")).onClick(() => this.resolve("keep_both"))
      );

    new Setting(contentEl)
      .addButton((b) =>
        b.setButtonText(this.plugin.t("conflict.later")).setWarning().onClick(() => this.close())
      );
  }

  onClose(): void {
    this.contentEl.empty();
    this.plugin.sidebarView?.refresh();
  }

  private async resolve(r: ConflictResolution): Promise<void> {
    try {
      await this.onResolved(r);
      this.close();
    } catch (error) {
      const message = error instanceof Error ? error.message : this.plugin.t("common.unknownError");
      new Notice(this.plugin.t("conflict.failed", { error: message }));
    }
  }
}

function appendTextWithPathBreaks(element: HTMLElement, text: string): void {
  for (const part of text.split(/(?=[\/\\])/)) {
    if (part.length === 0) continue;
    if (part[0] === "/" || part[0] === "\\") {
      element.createEl("wbr");
    }
    element.appendChild(document.createTextNode(part));
  }
}

function assertNever(value: never): never {
  throw new Error(`Unexpected conflict diff row: ${String(value)}`);
}
