import { Notice } from "obsidian";
import type { CollaborationBaselineEntry } from "./collaboration-state.js";
import { CollaborationFileVault } from "./collaboration-file-vault.js";
import type { CollaborationUploadInput, SyncFileMeta } from "./api.js";
import { decodeMergeableText } from "./text-merge.js";
import { decideCollaborationReconciliation, type CollaborationContent } from "./collaboration-file-reconcile.js";
import { collabConflictCopyPath, isCollaborationConflictError, resolveCollaboration409 } from "./collaboration-cas-resolver.js";
import { localizeError } from "./localized-error.js";
import type { TranslationKey, TranslationParams } from "./i18n.js";
import type { AcceptedCollaborationFile } from "./collaboration-file-sync.js";
import { CollaborationSyncCoordinator } from "./collaboration-sync-coordinator.js";
import { isValidOperationID, isValidServerRevision } from "./operation-id.js";
import { prepareCollaborationUpload } from "./collaboration-upload-helpers.js";

export { CollaborationSyncCoordinator };
export type { AcceptedCollaborationFile };

export interface CollaborationRemoteSyncApi {
  readonly downloadCollabContent: (v: string, f: number) => Promise<{ content: ArrayBuffer; meta: SyncFileMeta } | null>;
  readonly collabUpload: (v: string, f: number, i: CollaborationUploadInput) => Promise<SyncFileMeta>;
}
export interface CollaborationRemoteSyncPlugin {
  readonly t: (k: TranslationKey, p?: TranslationParams) => string;
}
export interface CollaborationRemoteSyncBaseline {
  readonly getCollaboration: (v: string, f: number) => CollaborationBaselineEntry | null;
  readonly setCollaboration: (v: string, f: number, e: CollaborationBaselineEntry) => void;
  readonly removeCollaboration?: (v: string, f: number) => boolean;
  readonly save: () => Promise<void>;
  readonly load: () => Promise<void>;
  readonly bindCollaborationAccount: (a: string) => boolean;
}
export interface CollaborationRemoteSyncDeps {
  readonly baseline: CollaborationRemoteSyncBaseline;
  readonly vault: CollaborationFileVault;
  readonly api: CollaborationRemoteSyncApi;
  readonly plugin: CollaborationRemoteSyncPlugin;
  readonly getAccepted: () => readonly AcceptedCollaborationFile[];
  readonly getUsername: () => string;
  readonly now: () => number;
  readonly createOperationID: () => string;
  readonly onChange: () => void;
  readonly coordinator: CollaborationSyncCoordinator;
}
interface RemoteVersion {
  readonly bytes: Uint8Array;
  readonly hash: string;
  readonly revision: number;
}
interface ReconcileContext {
  readonly remote: RemoteVersion;
  readonly local: CollaborationContent | null;
}
function assertNever(value: never): never {
  throw new Error(`unhandled ${String(value)}`);
}
export class CollaborationRemoteSync {
  constructor(private readonly deps: CollaborationRemoteSyncDeps) {}
  refresh(): Promise<void> {
    return this.deps.coordinator.run(() => this.doRefresh());
  }
  private async doRefresh(): Promise<void> {
    await this.deps.baseline.load();
    const didBind = this.deps.baseline.bindCollaborationAccount(this.deps.getUsername());
    if (didBind) await this.deps.baseline.save();
    for (const ac of this.deps.getAccepted()) {
      const b = this.deps.baseline.getCollaboration(ac.vaultId, ac.fileId);
      if (b?.pending) {
        await this.retryPending(ac, b);
        continue;
      }
      if (b?.conflict) continue;
      await this.syncOne(ac, b);
    }
  }
  private async retryPending(ac: AcceptedCollaborationFile, entry: CollaborationBaselineEntry): Promise<void> {
    if (!isValidServerRevision(entry.serverRevision)) {
      // 哨兵 -1：清掉错误 pending，以无基线方式重新拉取，避免 keep_local 阻塞
      if (this.deps.baseline.removeCollaboration) {
        this.deps.baseline.removeCollaboration(ac.vaultId, ac.fileId);
        await this.deps.baseline.save();
      } else {
        this.deps.baseline.setCollaboration(ac.vaultId, ac.fileId, { ...entry, serverRevision: -1, pending: null, conflict: null });
        await this.deps.baseline.save();
      }
      await this.syncOne(ac, null);
      return;
    }
    const local = await this.deps.vault.readExact(ac.localPath);
    if (!local) return;
    const same = local.hash === entry.localHash;
    let op = same && entry.pending && isValidOperationID(entry.pending.id) ? entry.pending.id : this.deps.createOperationID();
    if (!isValidOperationID(op)) op = this.deps.createOperationID();
    const pendingEntry: CollaborationBaselineEntry = same ? { ...entry, pending: { id: op, createdAt: this.deps.now() } } : { ...entry, localHash: local.hash, pending: { id: op, createdAt: this.deps.now() } };
    if (!same) {
      this.deps.baseline.setCollaboration(ac.vaultId, ac.fileId, pendingEntry);
      await this.deps.baseline.save();
    } else if (!entry.pending || !isValidOperationID(entry.pending.id)) {
      this.deps.baseline.setCollaboration(ac.vaultId, ac.fileId, { ...entry, pending: { id: op, createdAt: this.deps.now() } });
      await this.deps.baseline.save();
    }
    const decoded = decodeMergeableText(ac.localPath, local.bytes);
    if (decoded === null) return;
    const prepared = prepareCollaborationUpload(pendingEntry, decoded, () => op);
    if ("error" in prepared) return;
    try {
      const r = await this.deps.api.collabUpload(ac.vaultId, ac.fileId, prepared.input);
      this.deps.baseline.setCollaboration(ac.vaultId, ac.fileId, {
        vaultId: ac.vaultId, fileId: ac.fileId, localPath: ac.localPath,
        serverRevision: r.revision, serverHash: r.hash, localHash: local.hash, baseText: decoded, pending: null, conflict: null,
      });
      await this.deps.baseline.save();
      new Notice(this.deps.plugin.t("collab.uploaded", { path: ac.localPath }));
      this.deps.onChange();
    } catch (err) {
      if (isCollaborationConflictError(err)) {
        const toResolve: CollaborationBaselineEntry = this.deps.baseline.getCollaboration(ac.vaultId, ac.fileId) ?? { ...pendingEntry, pending: { id: op, createdAt: this.deps.now() } };
        await resolveCollaboration409(
          { baseline: this.deps.baseline, vault: this.deps.vault, api: this.deps.api, plugin: this.deps.plugin, now: this.deps.now, createOperationID: this.deps.createOperationID, onChange: this.deps.onChange },
          { accepted: ac, entry: toResolve },
        );
        return;
      }
      new Notice(this.deps.plugin.t("collab.uploadFailed", { error: localizeError(err, this.deps.plugin.t.bind(this.deps.plugin), this.deps.plugin.t("common.unknownError")) }));
    }
  }
  private async syncOne(ac: AcceptedCollaborationFile, entry: CollaborationBaselineEntry | null): Promise<void> {
    let fetched: { content: ArrayBuffer; meta: SyncFileMeta } | null;
    try {
      fetched = await this.deps.api.downloadCollabContent(ac.vaultId, ac.fileId);
    } catch (err) {
      new Notice(this.deps.plugin.t("collab.refreshFailed", { error: localizeError(err, this.deps.plugin.t.bind(this.deps.plugin), this.deps.plugin.t("common.unknownError")) }));
      return;
    }
    if (!fetched) return;
    const remoteBytes = new Uint8Array(fetched.content);
    const remoteHash = fetched.meta.hash;
    const remoteRev = fetched.meta.revision;
    const remote: CollaborationContent = { path: ac.localPath, bytes: remoteBytes, hash: remoteHash };
    const remoteVersion = { bytes: remoteBytes, hash: remoteHash, revision: remoteRev };
    if (!entry || !isValidServerRevision(entry.serverRevision)) {
      if (entry && !isValidServerRevision(entry.serverRevision) && this.deps.baseline.removeCollaboration) {
        this.deps.baseline.removeCollaboration(ac.vaultId, ac.fileId);
        await this.deps.baseline.save();
      }
      await this.handleFirst(ac, remoteVersion);
      return;
    }
    const local = await this.deps.vault.readExact(ac.localPath);
    const localContent: CollaborationContent | null = local ? { path: ac.localPath, bytes: local.bytes, hash: local.hash } : null;
    const decision = decideCollaborationReconciliation({ path: ac.localPath, baseline: entry, ancestorText: entry.baseText, local: localContent, remote });
    switch (decision.kind) {
      case "adopt_remote":
        await this.handleAdopt(ac, entry, { remote: remoteVersion, local: localContent });
        break;
      case "apply_remote":
        await this.handleApply(ac, { remote: remoteVersion, local: localContent });
        break;
      case "upload_local":
        if (localContent) await this.handleUploadLocal(ac, entry, { local: localContent, content: decision.content });
        break;
      case "upload_merged":
        if (localContent) await this.handleUploadLocal(ac, entry, { local: localContent, content: decision.content });
        break;
      case "persist_text_conflict": {
        const latestHash = localContent ? localContent.hash : entry.localHash;
        this.deps.baseline.setCollaboration(ac.vaultId, ac.fileId, {
          vaultId: entry.vaultId, fileId: entry.fileId, localPath: entry.localPath,
          serverRevision: entry.serverRevision, serverHash: entry.serverHash, localHash: latestHash, baseText: entry.baseText,
          pending: null, conflict: { remoteRevision: remoteVersion.revision, remoteHash: remoteVersion.hash, remoteText: decision.remoteText, detectedAt: this.deps.now() },
        });
        await this.deps.baseline.save();
        this.deps.onChange();
        new Notice(`协作冲突：${ac.localPath} 存在重叠编辑，已在右侧边栏等待解决`, 8000);
        this.showConflictPopup(ac.localPath);
        break;
      }
      case "preserve_both": {
        if (!localContent) {
          await this.persistDownload(ac, remoteVersion, remoteVersion.hash);
          break;
        }
        const stashFresh = await this.deps.vault.readExact(ac.localPath);
        if (!stashFresh || stashFresh.hash !== localContent.hash) break;
        const sibling = collabConflictCopyPath(ac.localPath, this.deps.now());
        await this.deps.vault.writeCanonical(sibling, localContent.bytes);
        await this.persistDownload(ac, remoteVersion, remoteVersion.hash);
        break;
      }
      case "keep_local":
        break;
      default:
        assertNever(decision);
    }
  }
  private async handleFirst(ac: AcceptedCollaborationFile, remote: RemoteVersion): Promise<void> {
    const local = await this.deps.vault.readExact(ac.localPath);
    if (!local) {
      await this.persistDownload(ac, remote, remote.hash);
      return;
    }
    if (local.hash === remote.hash) {
      const baseText = decodeMergeableText(ac.localPath, remote.bytes) ?? "";
      this.deps.baseline.setCollaboration(ac.vaultId, ac.fileId, {
        vaultId: ac.vaultId, fileId: ac.fileId, localPath: ac.localPath,
        serverRevision: remote.revision, serverHash: remote.hash, localHash: local.hash, baseText, pending: null, conflict: null,
      });
      await this.deps.baseline.save();
    }
  }
  private async handleAdopt(ac: AcceptedCollaborationFile, entry: CollaborationBaselineEntry, ctx: ReconcileContext): Promise<void> {
    const { remote, local } = ctx;
    if (!local) {
      await this.persistDownload(ac, remote, remote.hash);
      return;
    }
    if (local.hash === remote.hash) {
      const baseText = decodeMergeableText(ac.localPath, remote.bytes) ?? entry.baseText;
      this.deps.baseline.setCollaboration(ac.vaultId, ac.fileId, {
        vaultId: entry.vaultId, fileId: entry.fileId, localPath: entry.localPath,
        serverRevision: remote.revision, serverHash: remote.hash, localHash: local.hash, baseText, pending: null, conflict: null,
      });
      await this.deps.baseline.save();
      return;
    }
    const fresh = await this.deps.vault.readExact(ac.localPath);
    if (!fresh || fresh.hash !== local.hash) return;
    await this.persistDownload(ac, remote, remote.hash);
  }
  private async handleApply(ac: AcceptedCollaborationFile, ctx: ReconcileContext): Promise<void> {
    const { remote, local } = ctx;
    if (!local) return;
    const fresh = await this.deps.vault.readExact(ac.localPath);
    if (!fresh || fresh.hash !== local.hash) return;
    await this.persistDownload(ac, remote, remote.hash);
  }
  private async handleUploadLocal(ac: AcceptedCollaborationFile, entry: CollaborationBaselineEntry, ctx: { local: CollaborationContent; content: string }): Promise<void> {
    if (!isValidServerRevision(entry.serverRevision)) {
      if (this.deps.baseline.removeCollaboration) {
        this.deps.baseline.removeCollaboration(ac.vaultId, ac.fileId);
        await this.deps.baseline.save();
      }
      await this.syncOne(ac, null);
      return;
    }
    const { local, content } = ctx;
    let op = entry.pending && entry.localHash === local.hash && isValidOperationID(entry.pending.id) ? entry.pending.id : this.deps.createOperationID();
    if (!isValidOperationID(op)) op = this.deps.createOperationID();
    const pendingEntry: CollaborationBaselineEntry = { ...entry, localHash: local.hash, pending: { id: op, createdAt: this.deps.now() } };
    this.deps.baseline.setCollaboration(ac.vaultId, ac.fileId, pendingEntry);
    await this.deps.baseline.save();
    const prepared = prepareCollaborationUpload(pendingEntry, content, () => op);
    if ("error" in prepared) return;
    try {
      const r = await this.deps.api.collabUpload(ac.vaultId, ac.fileId, prepared.input);
      this.deps.baseline.setCollaboration(ac.vaultId, ac.fileId, {
        vaultId: ac.vaultId, fileId: ac.fileId, localPath: ac.localPath,
        serverRevision: r.revision, serverHash: r.hash, localHash: local.hash, baseText: content, pending: null, conflict: null,
      });
      await this.deps.baseline.save();
      new Notice(this.deps.plugin.t("collab.uploaded", { path: ac.localPath }));
      this.deps.onChange();
    } catch (err) {
      if (isCollaborationConflictError(err)) {
        await resolveCollaboration409(
          { baseline: this.deps.baseline, vault: this.deps.vault, api: this.deps.api, plugin: this.deps.plugin, now: this.deps.now, createOperationID: this.deps.createOperationID, onChange: this.deps.onChange },
          { accepted: ac, entry: pendingEntry },
        );
        return;
      }
      new Notice(this.deps.plugin.t("collab.uploadFailed", { error: localizeError(err, this.deps.plugin.t.bind(this.deps.plugin), this.deps.plugin.t("common.unknownError")) }));
    }
  }
  private async persistDownload(ac: AcceptedCollaborationFile, remote: RemoteVersion, localHash: string): Promise<void> {
    await this.deps.vault.writeCanonical(ac.localPath, remote.bytes);
    const baseText = decodeMergeableText(ac.localPath, remote.bytes) ?? "";
    this.deps.baseline.setCollaboration(ac.vaultId, ac.fileId, {
      vaultId: ac.vaultId, fileId: ac.fileId, localPath: ac.localPath,
      serverRevision: remote.revision, serverHash: remote.hash, localHash, baseText, pending: null, conflict: null,
    });
    await this.deps.baseline.save();
    new Notice(this.deps.plugin.t("collab.downloaded", { path: ac.localPath }));
    this.deps.onChange();
  }

  private showConflictPopup(localPath: string): void {
    try {
      // 尝试自动揭示侧边栏，让用户第一时间看到冲突
      const app: any = (this.deps as any).app ?? (globalThis as any)?.app;
      if (app?.workspace) {
        const leaves = app.workspace.getLeavesOfType?.("oss-sync-sidebar");
        if (leaves?.length === 0) {
          const leaf = app.workspace.getRightLeaf?.(false);
          leaf?.setViewState?.({ type: "oss-sync-sidebar", active: true });
          app.workspace.revealLeaf?.(leaf);
        }
      }
    } catch {}
  }
}
