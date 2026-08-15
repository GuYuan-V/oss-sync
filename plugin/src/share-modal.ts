import { App, Modal, Notice, Setting, TFile, TFolder } from "obsidian";
import type OSSPlugin from "./main";

export class ShareModal extends Modal {
  private isFolder: boolean;
  private targetPath: string;
  private allowCopy = true;
  private recursive = false;

  constructor(app: App, private plugin: OSSPlugin, private file: TFile | TFolder) {
    super(app);
    this.isFolder = file instanceof TFolder;
    this.targetPath = file.path;
  }

  onOpen(): void {
    const { contentEl, titleEl } = this;
<<<<<<< HEAD
    titleEl.setText(this.plugin.t("share.title"));

    new Setting(contentEl)
      .setName(this.plugin.t("share.target"))
      .setDesc(this.isFolder ? this.plugin.t("share.folder") : this.plugin.t("share.file", { path: this.targetPath }))
      .addText(() => {});

    new Setting(contentEl)
      .setName(this.plugin.t("share.allowCopy"))
=======
    titleEl.setText("分享至轻博客");

    new Setting(contentEl)
      .setName("目标")
      .setDesc(this.isFolder ? "整个文件夹" : `单篇：${this.targetPath}`)
      .addText(() => {});

    new Setting(contentEl)
      .setName("允许访问者复制源码")
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
      .addToggle((t) => t.setValue(this.allowCopy).onChange((v) => (this.allowCopy = v)));

    if (!this.isFolder) {
      new Setting(contentEl)
<<<<<<< HEAD
        .setName(this.plugin.t("share.recursive"))
        .setDesc(this.plugin.t("share.recursiveDesc"))
=======
        .setName("递归分享双链文章")
        .setDesc("自动解析本文内的 [[双链]]，为已存在的文章生成分享链接")
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
        .addToggle((t) => t.setValue(this.recursive).onChange((v) => (this.recursive = v)));
    }

    new Setting(contentEl)
<<<<<<< HEAD
      .setName(this.plugin.t("share.hint"))
      .setDesc(this.plugin.t("share.hintDesc"));
=======
      .setName("提示")
      .setDesc("重命名/移动已分享文章会导致原链接失效，需重新分享。");
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b

    new Setting(contentEl)
      .addButton((btn) =>
        btn
<<<<<<< HEAD
          .setButtonText(this.plugin.t("common.cancel"))
=======
          .setButtonText("取消")
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
          .setWarning()
          .onClick(() => this.close())
      )
      .addButton((btn) =>
<<<<<<< HEAD
        btn.setButtonText(this.plugin.t("share.generate")).onClick(async () => {
=======
        btn.setButtonText("生成链接").onClick(async () => {
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
          await this.create();
        })
      );
  }

  onClose(): void {
    this.contentEl.empty();
  }

  private async create(): Promise<void> {
    try {
      const res = await this.plugin.api.createShare({
        targetPath: this.targetPath,
        isFolder: this.isFolder,
        allowCopy: this.allowCopy,
        recursiveBacklinks: this.recursive,
      });
      const fullUrl = this.plugin.settings.serverUrl.replace(/\/$/, "") + res.url;
      await navigator.clipboard.writeText(fullUrl);
<<<<<<< HEAD
      new Notice(this.plugin.t("share.copied", { url: fullUrl }), 8000);
      if (res.extra && res.extra.length > 0) {
        new Notice(this.plugin.t("share.extra", { count: res.extra.length }), 6000);
      }
      this.plugin.sidebarView?.reloadShares();
      this.close();
    } catch (error) {
      const message = error instanceof Error ? error.message : this.plugin.t("common.unknownError");
      new Notice(this.plugin.t("share.failed", { error: message }));
=======
      new Notice(`OSS: 链接已复制 ${fullUrl}`, 8000);
      if (res.extra && res.extra.length > 0) {
        new Notice(`OSS: 同时分享了 ${res.extra.length} 篇双链文章`, 6000);
      }
      this.close();
    } catch (e) {
      new Notice("OSS 分享失败: " + (e as Error).message);
>>>>>>> 3b7aaacb143eff9df5a728b914a633fc58e70a6b
    }
  }
}
