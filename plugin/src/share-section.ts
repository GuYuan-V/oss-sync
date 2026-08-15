// 分享区：渲染当前仓库的公开文章、浏览量与管理操作。
import { Notice } from "obsidian";
import type { ShareOut } from "./api";
import type OSSPlugin from "./main";

export function renderShareRows(
  section: HTMLElement,
  plugin: OSSPlugin,
  options: {
    readonly shares: readonly ShareOut[];
    readonly onChanged?: () => void | Promise<void>;
  }
): void {
  const { shares, onChanged } = options;
  if (shares.length === 0) {
    section.createDiv({ cls: "oss-sidebar-empty", text: plugin.t("sidebar.noShares") });
    return;
  }
  for (const share of shares) {
    const row = section.createDiv({ cls: "oss-sidebar-share" });
    row.createDiv({ cls: "oss-sidebar-share-path", text: share.target_path });
    row.createDiv({
      cls: "oss-sidebar-share-meta",
      text: plugin.t("sidebar.shareMeta", { views: share.views }),
    });
    const actions = row.createDiv({ cls: "oss-sidebar-actions oss-sidebar-share-actions" });
    const url = absoluteShareURL(plugin.settings.serverUrl, share.url);
    actions.createEl("button", {
      cls: "oss-sidebar-share-copy",
      text: plugin.t("sidebar.copyLink"),
    }).addEventListener("click", () => void copyShareLink(plugin, url));
    actions.createEl("a", {
      cls: "oss-sidebar-share-open",
      text: plugin.t("sidebar.openShare"),
      href: url,
      attr: { target: "_blank", rel: "noopener" },
    });
    actions.createEl("button", {
      cls: "oss-sidebar-share-toggle",
      text: plugin.t(share.allow_copy ? "sidebar.blockCopy" : "sidebar.allowCopy"),
    }).addEventListener("click", () => void updateAllowCopy(plugin, share, onChanged));
    actions.createEl("button", {
      cls: "oss-sidebar-share-delete mod-warning",
      text: plugin.t("sidebar.deleteShare"),
    }).addEventListener("click", () => void deleteShare(plugin, share.share_id, onChanged));
  }
}

function absoluteShareURL(serverURL: string, shareURL: string): string {
  if (/^https?:\/\//i.test(shareURL)) return shareURL;
  return serverURL.replace(/\/$/, "") + (shareURL.startsWith("/") ? shareURL : `/${shareURL}`);
}

async function copyShareLink(plugin: OSSPlugin, url: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(url);
    new Notice(plugin.t("sidebar.shareLinkCopied"));
  } catch (error) {
    const message = plugin.localizedError(error);
    new Notice(plugin.t("sidebar.shareActionFailed", { error: message }));
  }
}

async function updateAllowCopy(
  plugin: OSSPlugin,
  share: ShareOut,
  onChanged?: () => void | Promise<void>
): Promise<void> {
  try {
    await plugin.api.updateShareAllowCopy(share.share_id, !share.allow_copy);
    share.allow_copy = !share.allow_copy;
    if (onChanged) {
      await onChanged();
    } else {
      plugin.sidebarView?.refresh();
    }
  } catch (error) {
    const message = plugin.localizedError(error);
    new Notice(plugin.t("sidebar.shareActionFailed", { error: message }));
  }
}

async function deleteShare(
  plugin: OSSPlugin,
  shareID: string,
  onChanged?: () => void | Promise<void>
): Promise<void> {
  try {
    await plugin.api.deleteShare(shareID);
    if (onChanged) {
      await onChanged();
    } else {
      plugin.sidebarView?.reloadShares();
    }
  } catch (error) {
    const message = plugin.localizedError(error);
    new Notice(plugin.t("sidebar.shareActionFailed", { error: message }));
  }
}
