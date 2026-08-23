import { normalizePath } from "./baseline.js";
import type { CollaborationBaselineEntry } from "./collaboration-state.js";
import type { CollaborationFileVault } from "./collaboration-file-vault.js";
import type { SyncFileMeta, CollaborationUploadInput } from "./api.js";
import type { AcceptedCollaborationFile } from "./collaboration-file-sync.js";
import { decideCollaborationReconciliation } from "./collaboration-file-reconcile.js";
import { decodeMergeableText } from "./text-merge.js";
import { localizeError } from "./localized-error.js";
import type { TranslationKey, TranslationParams } from "./i18n.js";
import { Notice } from "obsidian";
import { isValidOperationID, createOperationID as createValidOperationID } from "./operation-id.js";
import type { ConflictResolution } from "./conflict-modal.js";

function hasStatus(error: unknown): error is { status: unknown } {
  return typeof error === "object" && error !== null && "status" in error;
}
export function isCollaborationConflictError(error: unknown): boolean {
  if (!hasStatus(error)) return false;
  return error.status === 409;
}
export function collabConflictCopyPath(path: string, now: number): string {
  const key = normalizePath(path);
  const slash = key.lastIndexOf("/");
  const dir = slash >= 0 ? key.slice(0, slash + 1) : "";
  const file = slash >= 0 ? key.slice(slash + 1) : key;
  const dot = file.lastIndexOf(".");
  const stem = dot > 0 ? file.slice(0, dot) : file;
  const ext = dot > 0 ? file.slice(dot) : "";
  const ts = new Date(now).toISOString().replace(/[:.]/g, "-");
  return `${dir}${stem}_conflict_${ts}${ext}`;
}
export interface CollaborationCasBaseline {
  readonly getCollaboration: (vaultId: string, fileId: number) => CollaborationBaselineEntry | null;
  readonly setCollaboration: (vaultId: string, fileId: number, entry: CollaborationBaselineEntry) => void;
  readonly save: () => Promise<void>;
}
export interface CollaborationCasApi {
  readonly downloadCollabContent: (vaultId: string, fileId: number) => Promise<{ content: ArrayBuffer; meta: SyncFileMeta } | null>;
  readonly collabUpload: (vaultId: string, fileId: number, input: CollaborationUploadInput) => Promise<SyncFileMeta>;
}
export interface CollaborationCasPlugin {
  readonly t: (key: TranslationKey, params?: TranslationParams) => string;
}
export interface CollaborationCasDeps {
  readonly baseline: CollaborationCasBaseline;
  readonly vault: CollaborationFileVault;
  readonly api: CollaborationCasApi;
  readonly plugin: CollaborationCasPlugin;
  readonly now: () => number;
  readonly createOperationID: () => string;
  readonly onChange: () => void;
}
export interface Resolve409Input {
  readonly accepted: AcceptedCollaborationFile;
  readonly entry: CollaborationBaselineEntry;
}
function assertNever(value: never): never {
  throw new Error(`unhandled ${String(value)}`);
}
async function stashPending(deps: CollaborationCasDeps, entry: CollaborationBaselineEntry, hash: string): Promise<void> {
  const id = deps.createOperationID();
  deps.baseline.setCollaboration(entry.vaultId, entry.fileId, { ...entry, localHash: hash, pending: { id, createdAt: deps.now() } });
  await deps.baseline.save();
}
async function advanceToRemote(deps: CollaborationCasDeps, accepted: AcceptedCollaborationFile, bytes: Uint8Array, hash: string, rev: number): Promise<void> {
  await deps.vault.writeCanonical(accepted.localPath, bytes);
  const baseText = decodeMergeableText(accepted.localPath, bytes) ?? "";
  deps.baseline.setCollaboration(accepted.vaultId, accepted.fileId, {
    vaultId: accepted.vaultId, fileId: accepted.fileId, localPath: accepted.localPath,
    serverRevision: rev, serverHash: hash, localHash: hash, baseText, pending: null, conflict: null,
  });
  await deps.baseline.save();
  new Notice(deps.plugin.t("collab.downloaded", { path: accepted.localPath }));
  deps.onChange();
}
async function tryUpload(deps: CollaborationCasDeps, accepted: AcceptedCollaborationFile, content: string, rev: number, op: string): Promise<SyncFileMeta> {
  return deps.api.collabUpload(accepted.vaultId, accepted.fileId, { content, baseRevision: rev, operationID: op });
}
export async function resolveCollaboration409(deps: CollaborationCasDeps, input: Resolve409Input): Promise<void> {
  const { accepted, entry } = input;
  let fetched: { content: ArrayBuffer; meta: SyncFileMeta } | null = null;
  try {
    fetched = await deps.api.downloadCollabContent(accepted.vaultId, accepted.fileId);
  } catch (error) {
    if (error instanceof Error) {
      const message = localizeError(error, deps.plugin.t.bind(deps.plugin), deps.plugin.t("common.unknownError"));
      new Notice(deps.plugin.t("collab.uploadFailed", { error: message }));
      return;
    }
    throw error;
  }
  if (!fetched) {
    const message = localizeError(new Error("download failed"), deps.plugin.t.bind(deps.plugin), deps.plugin.t("common.unknownError"));
    new Notice(deps.plugin.t("collab.uploadFailed", { error: message }));
    return;
  }
  const remoteBytes = new Uint8Array(fetched.content);
  const remoteHash = fetched.meta.hash;
  const remoteRev = fetched.meta.revision;
  const fresh = await deps.vault.readExact(accepted.localPath);
  const freshLocal = fresh ? { path: accepted.localPath, bytes: fresh.bytes, hash: fresh.hash } : null;
  const remoteContent = { path: accepted.localPath, bytes: remoteBytes, hash: remoteHash };
  const synthetic: CollaborationBaselineEntry = {
    vaultId: entry.vaultId, fileId: entry.fileId, localPath: entry.localPath,
    serverRevision: entry.serverRevision, serverHash: entry.serverHash, localHash: entry.serverHash,
    baseText: entry.baseText, pending: null, conflict: null,
  };
  const decision = decideCollaborationReconciliation({
    path: accepted.localPath, baseline: synthetic, ancestorText: entry.baseText, local: freshLocal, remote: remoteContent,
  });
  switch (decision.kind) {
    case "adopt_remote":
    case "apply_remote": {
      if (!freshLocal) {
        await advanceToRemote(deps, accepted, remoteBytes, remoteHash, remoteRev);
        return;
      }
      const latest = await deps.vault.readExact(accepted.localPath);
      if (!latest || latest.hash !== freshLocal.hash) {
        const latestHash = latest ? latest.hash : freshLocal.hash;
        await stashPending(deps, entry, latestHash);
        return;
      }
      await advanceToRemote(deps, accepted, remoteBytes, remoteHash, remoteRev);
      return;
    }
    case "keep_local": return;
    case "upload_local": {
      const op = deps.createOperationID();
      const localHash = freshLocal ? freshLocal.hash : entry.localHash;
      deps.baseline.setCollaboration(accepted.vaultId, accepted.fileId, { ...entry, localHash, pending: { id: op, createdAt: deps.now() } });
      await deps.baseline.save();
      try {
        const result = await tryUpload(deps, accepted, decision.content, remoteRev, op);
        const encoded = new TextEncoder().encode(decision.content);
        const h = await deps.vault.hashBytes(encoded);
        deps.baseline.setCollaboration(accepted.vaultId, accepted.fileId, {
          vaultId: accepted.vaultId, fileId: accepted.fileId, localPath: accepted.localPath,
          serverRevision: result.revision, serverHash: result.hash, localHash: h, baseText: decision.content, pending: null, conflict: null,
        });
        await deps.baseline.save();
        new Notice(deps.plugin.t("collab.uploaded", { path: accepted.localPath }));
        deps.onChange();
      } catch (error) {
        if (error instanceof Error) {
          const message = localizeError(error, deps.plugin.t.bind(deps.plugin), deps.plugin.t("common.unknownError"));
          new Notice(deps.plugin.t("collab.uploadFailed", { error: message }));
          return;
        }
        throw error;
      }
      return;
    }
    case "upload_merged": {
      if (!freshLocal) {
        await stashPending(deps, entry, entry.localHash);
        return;
      }
      const latest = await deps.vault.readExact(accepted.localPath);
      if (!latest || latest.hash !== freshLocal.hash) {
        const latestHash = latest ? latest.hash : freshLocal.hash;
        await stashPending(deps, entry, latestHash);
        return;
      }
      const mergedContent = decision.content;
      const mergedBytes = new TextEncoder().encode(mergedContent);
      const mergedHash = await deps.vault.hashBytes(mergedBytes);
      const newId = deps.createOperationID();
      deps.baseline.setCollaboration(accepted.vaultId, accepted.fileId, { ...entry, localHash: mergedHash, pending: { id: newId, createdAt: deps.now() } });
      await deps.baseline.save();
      try {
        await deps.vault.writeCanonical(accepted.localPath, mergedBytes);
      } catch (error) {
        if (error instanceof Error) {
          const message = localizeError(error, deps.plugin.t.bind(deps.plugin), deps.plugin.t("common.unknownError"));
          new Notice(deps.plugin.t("collab.uploadFailed", { error: message }));
          return;
        }
        throw error;
      }
      try {
        const result = await tryUpload(deps, accepted, mergedContent, remoteRev, newId);
        const finalHash = await deps.vault.hashBytes(mergedBytes);
        deps.baseline.setCollaboration(accepted.vaultId, accepted.fileId, {
          vaultId: accepted.vaultId, fileId: accepted.fileId, localPath: accepted.localPath,
          serverRevision: result.revision, serverHash: result.hash, localHash: finalHash, baseText: mergedContent, pending: null, conflict: null,
        });
        await deps.baseline.save();
        new Notice(deps.plugin.t("collab.uploaded", { path: accepted.localPath }));
        deps.onChange();
      } catch (error) {
        if (error instanceof Error) {
          const message = localizeError(error, deps.plugin.t.bind(deps.plugin), deps.plugin.t("common.unknownError"));
          new Notice(deps.plugin.t("collab.uploadFailed", { error: message }));
          return;
        }
        throw error;
      }
      return;
    }
    case "persist_text_conflict": {
      const latest = await deps.vault.readExact(accepted.localPath);
      if (latest && freshLocal && latest.hash !== freshLocal.hash) {
        await stashPending(deps, entry, latest.hash);
        return;
      }
      const latestHash = (await deps.vault.readExact(accepted.localPath))?.hash ?? freshLocal?.hash ?? entry.localHash;
      const lh = freshLocal?.hash && latest?.hash === freshLocal.hash ? freshLocal.hash : latestHash;
      deps.baseline.setCollaboration(accepted.vaultId, accepted.fileId, {
        vaultId: entry.vaultId, fileId: entry.fileId, localPath: entry.localPath,
        serverRevision: entry.serverRevision, serverHash: entry.serverHash, localHash: lh, baseText: entry.baseText,
        pending: null, conflict: { remoteRevision: remoteRev, remoteHash, remoteText: decision.remoteText, detectedAt: deps.now() },
      });
      await deps.baseline.save();
      deps.onChange();
      new Notice(`协作冲突：${accepted.localPath} 存在重叠编辑，已在右侧边栏等待解决`, 8000);
      return;
    }
    case "preserve_both": {
      if (!freshLocal) {
        await advanceToRemote(deps, accepted, remoteBytes, remoteHash, remoteRev);
        return;
      }
      const latest = await deps.vault.readExact(accepted.localPath);
      if (!latest || latest.hash !== freshLocal.hash) {
        const latestHash = latest ? latest.hash : freshLocal.hash;
        await stashPending(deps, entry, latestHash);
        return;
      }
      const sibling = collabConflictCopyPath(accepted.localPath, deps.now());
      await deps.vault.writeCanonical(sibling, freshLocal.bytes);
      await advanceToRemote(deps, accepted, remoteBytes, remoteHash, remoteRev);
      return;
    }
    default: assertNever(decision);
  }
}

export async function resolvePersistedCollaborationConflict(
  deps: CollaborationCasDeps,
  vaultId: string,
  fileId: number,
  resolution: ConflictResolution,
): Promise<void> {
  const entry = deps.baseline.getCollaboration(vaultId, fileId);
  if (!entry?.conflict) throw new Error(deps.plugin.t("sync.conflictNotFound"));
  const accepted: AcceptedCollaborationFile = {
    vaultId,
    fileId,
    localPath: entry.localPath,
  };
  const conflictRev = entry.conflict.remoteRevision;
  const conflictText = entry.conflict.remoteText;

  if (typeof resolution === "object" && resolution.kind === "ordered_merge") {
    const content = resolution.content;
    const encoded = new TextEncoder().encode(content);
    const expectedHash = await deps.vault.hashBytes(encoded);
    // hash guard: ensure local hasn't changed before we overwrite
    // we intentionally read exact before writeCanonical to avoid clobbering concurrent edit
    const opRaw = deps.createOperationID?.() ?? createValidOperationID();
    const op = isValidOperationID(opRaw) ? opRaw : createValidOperationID();
    // Persist pending before local write to survive crash between write and upload
    deps.baseline.setCollaboration(vaultId, fileId, {
      ...entry,
      localHash: expectedHash,
      pending: { id: op, createdAt: deps.now() },
    });
    await deps.baseline.save();
    try {
      await deps.vault.writeCanonical(entry.localPath, encoded);
    } catch (error) {
      const message = localizeError(error, deps.plugin.t.bind(deps.plugin), deps.plugin.t("common.unknownError"));
      new Notice(deps.plugin.t("collab.uploadFailed", { error: message }));
      return;
    }
    try {
      const result = await deps.api.collabUpload(vaultId, fileId, { content, baseRevision: conflictRev, operationID: op });
      const finalHash = await deps.vault.hashBytes(encoded);
      deps.baseline.setCollaboration(vaultId, fileId, {
        vaultId,
        fileId,
        localPath: entry.localPath,
        serverRevision: result.revision,
        serverHash: result.hash,
        localHash: finalHash,
        baseText: content,
        pending: null,
        conflict: null,
      });
      await deps.baseline.save();
      new Notice(deps.plugin.t("collab.uploaded", { path: entry.localPath }));
      deps.onChange();
    } catch (error) {
      if (isCollaborationConflictError(error)) {
        // 409: fetch new remote and persist as new conflict
        try {
          const fetched = await deps.api.downloadCollabContent(vaultId, fileId);
          if (fetched) {
            const txt = new TextDecoder().decode(new Uint8Array(fetched.content));
            deps.baseline.setCollaboration(vaultId, fileId, {
              ...entry,
              localHash: expectedHash,
              pending: null,
              conflict: {
                remoteRevision: fetched.meta.revision,
                remoteHash: fetched.meta.hash,
                remoteText: txt,
                detectedAt: deps.now(),
              },
            });
            await deps.baseline.save();
            deps.onChange();
          }
        } catch {
          // keep pending for retry
        }
        const message = localizeError(error, deps.plugin.t.bind(deps.plugin), deps.plugin.t("common.unknownError"));
        new Notice(deps.plugin.t("collab.uploadFailed", { error: message }));
        return;
      }
      // transport failure: keep pending so retry can succeed, do not clear conflict
      const message = localizeError(error as Error, deps.plugin.t.bind(deps.plugin), deps.plugin.t("common.unknownError"));
      new Notice(deps.plugin.t("collab.uploadFailed", { error: message }));
    }
    return;
  }

  if (resolution === "accept_remote") {
    let fetched: { content: ArrayBuffer; meta: SyncFileMeta } | null = null;
    try {
      fetched = await deps.api.downloadCollabContent(vaultId, fileId);
    } catch (error) {
      const message = localizeError(error as Error, deps.plugin.t.bind(deps.plugin), deps.plugin.t("common.unknownError"));
      new Notice(deps.plugin.t("collab.uploadFailed", { error: message }));
      return;
    }
    if (!fetched) {
      new Notice(deps.plugin.t("collab.uploadFailed", { error: deps.plugin.t("common.unknownError") }));
      return;
    }
    const bytes = new Uint8Array(fetched.content);
    // hash guard: if local changed since conflict was created, stash as pending instead of overwriting
    const fresh = await deps.vault.readExact(entry.localPath);
    if (fresh && fresh.hash !== entry.localHash) {
      await stashPending(deps, entry, fresh.hash);
      return;
    }
    await deps.vault.writeCanonical(entry.localPath, bytes);
    const baseText = decodeMergeableText(entry.localPath, bytes) ?? conflictText ?? "";
    deps.baseline.setCollaboration(vaultId, fileId, {
      vaultId,
      fileId,
      localPath: entry.localPath,
      serverRevision: fetched.meta.revision,
      serverHash: fetched.meta.hash,
      localHash: fetched.meta.hash,
      baseText,
      pending: null,
      conflict: null,
    });
    await deps.baseline.save();
    new Notice(deps.plugin.t("collab.downloaded", { path: entry.localPath }));
    deps.onChange();
    return;
  }

  if (resolution === "force_local") {
    const fresh = await deps.vault.readExact(entry.localPath);
    if (!fresh || fresh.content === null) throw new Error(deps.plugin.t("sync.conflictNotFound"));
    if (fresh.hash !== entry.localHash) {
      await stashPending(deps, entry, fresh.hash);
      throw new Error(deps.plugin.t("conflict.orderedStale"));
    }
    const content = fresh.content;
    const opRaw = deps.createOperationID?.() ?? createValidOperationID();
    const op = isValidOperationID(opRaw) ? opRaw : createValidOperationID();
    deps.baseline.setCollaboration(vaultId, fileId, {
      ...entry,
      localHash: fresh.hash,
      pending: { id: op, createdAt: deps.now() },
    });
    await deps.baseline.save();
    try {
      const result = await deps.api.collabUpload(vaultId, fileId, { content, baseRevision: conflictRev, operationID: op });
      const hash = await deps.vault.hashBytes(new TextEncoder().encode(content));
      deps.baseline.setCollaboration(vaultId, fileId, {
        vaultId,
        fileId,
        localPath: entry.localPath,
        serverRevision: result.revision,
        serverHash: result.hash,
        localHash: hash,
        baseText: content,
        pending: null,
        conflict: null,
      });
      await deps.baseline.save();
      new Notice(deps.plugin.t("collab.uploaded", { path: entry.localPath }));
      deps.onChange();
    } catch (error) {
      if (isCollaborationConflictError(error)) {
        try {
          const fetched = await deps.api.downloadCollabContent(vaultId, fileId);
          if (fetched) {
            const txt = new TextDecoder().decode(new Uint8Array(fetched.content));
            deps.baseline.setCollaboration(vaultId, fileId, {
              ...entry,
              localHash: fresh.hash,
              pending: null,
              conflict: {
                remoteRevision: fetched.meta.revision,
                remoteHash: fetched.meta.hash,
                remoteText: txt,
                detectedAt: deps.now(),
              },
            });
            await deps.baseline.save();
            deps.onChange();
          }
        } catch {}
        const message = localizeError(error as Error, deps.plugin.t.bind(deps.plugin), deps.plugin.t("common.unknownError"));
        new Notice(deps.plugin.t("collab.uploadFailed", { error: message }));
        return;
      }
      const message = localizeError(error as Error, deps.plugin.t.bind(deps.plugin), deps.plugin.t("common.unknownError"));
      new Notice(deps.plugin.t("collab.uploadFailed", { error: message }));
    }
    return;
  }

  if (resolution === "keep_both") {
    const fresh = await deps.vault.readExact(entry.localPath);
    if (!fresh || fresh.content === null) throw new Error(deps.plugin.t("sync.conflictNotFound"));
    if (fresh.hash !== entry.localHash) {
      await stashPending(deps, entry, fresh.hash);
      throw new Error(deps.plugin.t("conflict.orderedStale"));
    }
    const copyPath = collabConflictCopyPath(entry.localPath, deps.now());
    await deps.vault.writeCanonical(copyPath, fresh.bytes);
    // then adopt remote
    let fetched: { content: ArrayBuffer; meta: SyncFileMeta } | null = null;
    try {
      fetched = await deps.api.downloadCollabContent(vaultId, fileId);
    } catch (error) {
      const message = localizeError(error as Error, deps.plugin.t.bind(deps.plugin), deps.plugin.t("common.unknownError"));
      new Notice(deps.plugin.t("collab.uploadFailed", { error: message }));
      return;
    }
    if (!fetched) {
      new Notice(deps.plugin.t("collab.uploadFailed", { error: deps.plugin.t("common.unknownError") }));
      return;
    }
    const bytes = new Uint8Array(fetched.content);
    await deps.vault.writeCanonical(entry.localPath, bytes);
    const baseText = decodeMergeableText(entry.localPath, bytes) ?? "";
    deps.baseline.setCollaboration(vaultId, fileId, {
      vaultId,
      fileId,
      localPath: entry.localPath,
      serverRevision: fetched.meta.revision,
      serverHash: fetched.meta.hash,
      localHash: fetched.meta.hash,
      baseText,
      pending: null,
      conflict: null,
    });
    await deps.baseline.save();
    new Notice(deps.plugin.t("collab.downloaded", { path: entry.localPath }));
    deps.onChange();
    return;
  }

  throw new Error(`unhandled resolution ${String(resolution)}`);
}
