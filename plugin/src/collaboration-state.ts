export interface CollaborationPendingOperation {
  readonly id: string;
  readonly createdAt: number;
}

export interface CollaborationConflict {
  readonly remoteRevision: number;
  readonly remoteHash: string;
  readonly remoteText: string;
  readonly detectedAt: number;
}

export interface CollaborationBaselineEntry {
  readonly vaultId: string;
  readonly fileId: number;
  readonly localPath: string;
  readonly serverRevision: number;
  readonly serverHash: string;
  readonly localHash: string;
  readonly baseText: string;
  readonly pending: CollaborationPendingOperation | null;
  readonly conflict: CollaborationConflict | null;
}

export function collaborationKey(vaultId: string, fileId: number): string {
  return `${vaultId}:${fileId}`;
}

export function cloneCollaborationEntry(
  entry: CollaborationBaselineEntry
): CollaborationBaselineEntry {
  return {
    vaultId: entry.vaultId,
    fileId: entry.fileId,
    localPath: entry.localPath,
    serverRevision: entry.serverRevision,
    serverHash: entry.serverHash,
    localHash: entry.localHash,
    baseText: entry.baseText,
    pending: entry.pending ? { id: entry.pending.id, createdAt: entry.pending.createdAt } : null,
    conflict: entry.conflict
      ? {
          remoteRevision: entry.conflict.remoteRevision,
          remoteHash: entry.conflict.remoteHash,
          remoteText: entry.conflict.remoteText,
          detectedAt: entry.conflict.detectedAt,
        }
      : null,
  };
}

const OPERATION_ID_RE = /^[A-Za-z0-9._-]{1,64}$/;

function isValidOperationID(value: unknown): value is string {
  return typeof value === "string" && OPERATION_ID_RE.test(value);
}

function isValidServerRevision(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0;
}

export function isCollaborationNeedsRecovery(entry: CollaborationBaselineEntry): boolean {
  return !isValidServerRevision(entry.serverRevision);
}

export function sanitizeCollaborationEntry(raw: unknown): CollaborationBaselineEntry | null {
  if (!raw || typeof raw !== "object") return null;
  const r = raw as Record<string, unknown>;
  const vaultId = r.vaultId;
  const fileId = r.fileId;
  const localPath = r.localPath;
  let serverRevision: unknown = r.serverRevision;
  const serverHash = r.serverHash;
  const localHash = r.localHash;
  const baseText = r.baseText;
  if (typeof vaultId !== "string" || !vaultId.trim()) return null;
  if (typeof fileId !== "number" || !Number.isSafeInteger(fileId) || fileId <= 0) return null;
  if (typeof localPath !== "string" || !localPath.trim()) return null;
  // 缺失或非法 revision 不丢弃记录，而是标记为需要远端恢复（哨兵 -1）
  let normalizedRevision: number;
  if (!isValidServerRevision(serverRevision)) {
    normalizedRevision = -1;
  } else {
    normalizedRevision = serverRevision as number;
  }
  if (typeof serverHash !== "string") return null;
  if (typeof localHash !== "string") return null;
  if (typeof baseText !== "string") return null;
  let pending: CollaborationPendingOperation | null = null;
  if (r.pending !== null && r.pending !== undefined) {
    if (typeof r.pending === "object" && r.pending !== null) {
      const p = r.pending as Record<string, unknown>;
      if (isValidOperationID(p.id) && typeof p.createdAt === "number" && Number.isFinite(p.createdAt)) {
        pending = { id: p.id as string, createdAt: p.createdAt as number };
      } else {
        // 无效 pending 仅清除 pending，不丢弃整条记录
        pending = null;
      }
    } else {
      pending = null;
    }
  }
  let conflict: CollaborationConflict | null = null;
  if (r.conflict !== null && r.conflict !== undefined) {
    if (typeof r.conflict === "object" && r.conflict !== null) {
      const c = r.conflict as Record<string, unknown>;
      if (
        isValidServerRevision(c.remoteRevision) &&
        typeof c.remoteHash === "string" &&
        typeof c.remoteText === "string" &&
        typeof c.detectedAt === "number" &&
        Number.isFinite(c.detectedAt)
      ) {
        conflict = {
          remoteRevision: c.remoteRevision as number,
          remoteHash: c.remoteHash as string,
          remoteText: c.remoteText as string,
          detectedAt: c.detectedAt as number,
        };
      } else {
        conflict = null;
      }
    } else {
      conflict = null;
    }
  }
  return {
    vaultId: vaultId as string,
    fileId: fileId as number,
    localPath: localPath as string,
    serverRevision: normalizedRevision,
    serverHash: serverHash as string,
    localHash: localHash as string,
    baseText: baseText as string,
    pending,
    conflict,
  };
}
