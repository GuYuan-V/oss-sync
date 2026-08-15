import { App, Modal, Notice, Setting, TFile } from "obsidian";
<<<<<<< HEAD
import type OSSPlugin from "./main";
import type { OSSApiClient } from "./api";
import { buildConflictDiff, LineCapacityExceededError } from "./conflict-diff";
=======
import { diff_match_patch } from "diff-match-patch";
import type OSSPlugin from "./main";
import type { OSSApiClient } from "./api";
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b

export type ConflictResolution = "accept_remote" | "force_local" | "keep_both";

export class ConflictModal extends Modal {
  private remoteContent = "";
<<<<<<< HEAD
=======
  private localContent = "";
  private diffHtml = "";
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b

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
<<<<<<< HEAD
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
=======
    titleEl.setText(`冲突解决：${this.file.path}`);

    this.localContent = await this.app.vault.read(this.file);
    this.diffHtml = this.buildDiff(this.localContent, this.remoteContent);

    const preview = contentEl.createDiv({ cls: "oss-diff-preview" });
    preview.style.cssText =
      "max-height:400px;overflow:auto;border:1px solid var(--background-modifier-border);" +
      "padding:8px;font-family:var(--font-monospace);font-size:12px;white-space:pre-wrap;";
    preview.innerHTML = this.diffHtml;

    new Setting(contentEl)
      .setName("选择解决方式")
      .setHeading();

    new Setting(contentEl)
      .setName("接受云端覆盖本地")
      .setDesc("用云端最新版本替换本地文件")
      .addButton((b) =>
        b.setButtonText("Accept Remote").onClick(() => this.resolve("accept_remote"))
      );

    new Setting(contentEl)
      .setName("强制本地覆盖云端")
      .setDesc("用本地版本上传覆盖服务端")
      .addButton((b) =>
        b.setButtonText("Force Push Local").setWarning().onClick(() => this.resolve("force_local"))
      );

    new Setting(contentEl)
      .setName("保留双方并产生副本")
      .setDesc("本地修改另存为 _conflict_时间戳.md，原文件取云端版本")
      .addButton((b) =>
        b.setButtonText("Keep Both").onClick(() => this.resolve("keep_both"))
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
      );

    new Setting(contentEl)
      .addButton((b) =>
<<<<<<< HEAD
        b.setButtonText(this.plugin.t("conflict.later")).setWarning().onClick(() => this.close())
=======
        b.setButtonText("稍后处理").setWarning().onClick(() => this.close())
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
      );
  }

  onClose(): void {
    this.contentEl.empty();
<<<<<<< HEAD
    this.plugin.sidebarView?.refresh();
=======
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
  }

  private async resolve(r: ConflictResolution): Promise<void> {
    try {
      await this.onResolved(r);
      this.close();
<<<<<<< HEAD
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
=======
    } catch (e) {
      new Notice("OSS 冲突解决失败: " + (e as Error).message);
    }
  }

  private buildDiff(local: string, remote: string): string {
    const dmp = new diff_match_patch();
    const diffs = dmp.diff_main(local, remote);
    dmp.diff_cleanupSemantic(diffs);
    let html = "";
    for (const [op, text] of diffs) {
      const esc = this.escape(text);
      if (op === 0) {
        html += esc;
      } else if (op === 1) {
        html += `<ins style="background:#dfd;color:#050">${esc}</ins>`;
      } else {
        html += `<del style="background:#fdd;color:#500">${esc}</del>`;
      }
    }
    return html;
  }

  private escape(s: string): string {
    return s
      .replace(/&/g, "&amp;")
      .replace(/</g, "&lt;")
      .replace(/>/g, "&gt;");
  }
}
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
