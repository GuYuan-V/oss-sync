import type { Vault } from "obsidian";
import {
  cloneCollaborationEntry,
  collaborationKey,
  sanitizeCollaborationEntry,
} from "./collaboration-state.js";
import type { CollaborationBaselineEntry } from "./collaboration-state.js";

export type {
  CollaborationBaselineEntry,
  CollaborationConflict,
  CollaborationPendingOperation,
} from "./collaboration-state.js";

export const BASELINE_FILENAME = ".oss-sync-state.json";

export interface BaselineEntry {
  serverRevision: number;
  serverHash: string;
  serverDeleted: boolean;
  localHash: string;
  localMTime: number;
  localSize: number;
  baseText?: string;
}

export interface PendingOperation {
  id: string;
  kind: "upsert" | "delete" | "rename";
  path: string;
  oldPath?: string;
  createdAt: number;
}

export interface ConflictEntry {
  path: string;
  localHash: string;
  remoteRevision: number;
  remoteHash: string;
  remoteDeleted: boolean;
  remoteMTime: number;
  remoteSize: number;
  remoteType: "markdown" | "attachment" | "config";
  detectedAt: number;
}

interface SyncStateFile {
  version: 3;
  vaultId: string;
  cursor: number;
  files: Record<string, BaselineEntry>;
  pending: PendingOperation[];
  conflicts: ConflictEntry[];
  collaborationAccount: string;
  collaborationFiles: Record<string, CollaborationBaselineEntry>;
}

function emptyState(): SyncStateFile {
  return {
    version: 3,
    vaultId: "",
    cursor: 0,
    files: {},
    pending: [],
    conflicts: [],
    collaborationAccount: "",
    collaborationFiles: {},
  };
}

export class BaselineStore {
  private data: SyncStateFile = emptyState();
  private loaded = false;
  private loadPromise: Promise<void> | null = null;
  private saveChain: Promise<void> = Promise.resolve();

  constructor(private vault: Vault) {}

  async load(): Promise<void> {
    if (this.loaded) return;
    if (!this.loadPromise) {
      this.loadPromise = this.loadFromAdapter();
    }
    await this.loadPromise;
  }

  private async loadFromAdapter(): Promise<void> {
    try {
      if (!(await this.vault.adapter.exists(BASELINE_FILENAME))) {
        this.data = emptyState();
        return;
      }
      const parsed = JSON.parse(await this.vault.adapter.read(BASELINE_FILENAME));
      if (parsed?.version === 3 && typeof parsed.files === "object") {
        const sanitizedCollab = sanitizeCollaborationFiles(parsed.collaborationFiles);
        this.data = {
          ...emptyState(),
          ...parsed,
          files: parsed.files ?? {},
          pending: Array.isArray(parsed.pending) ? parsed.pending : [],
          conflicts: Array.isArray(parsed.conflicts) ? parsed.conflicts : [],
          collaborationAccount:
            typeof parsed.collaborationAccount === "string" ? parsed.collaborationAccount : "",
          collaborationFiles: sanitizedCollab,
        };
      } else if (parsed?.version === 2 && typeof parsed.files === "object") {
        this.data = {
          ...emptyState(),
          vaultId: typeof parsed.vaultId === "string" ? parsed.vaultId : "",
          cursor: typeof parsed.cursor === "number" ? parsed.cursor : 0,
          files: parsed.files ?? {},
          pending: Array.isArray(parsed.pending) ? parsed.pending : [],
          conflicts: Array.isArray(parsed.conflicts) ? parsed.conflicts : [],
        };
      } else {
        this.data = emptyState();
      }
    } catch {
      this.data = emptyState();
    } finally {
      this.loaded = true;
    }
  }

  async save(): Promise<void> {
    await this.load();
    const raw = JSON.stringify(this.data);
    const pending = this.saveChain.then(() =>
      this.vault.adapter.write(BASELINE_FILENAME, raw)
    );
    this.saveChain = pending.catch(() => undefined);
    await pending;
  }

  bindVault(vaultID: string): boolean {
    if (this.data.vaultId === vaultID) return false;
    const { collaborationAccount: account, collaborationFiles: files } = this.data;
    this.data = emptyState();
    this.data.vaultId = vaultID;
    this.data.collaborationAccount = account;
    this.data.collaborationFiles = files;
    return true;
  }

  bindCollaborationAccount(account: string): boolean {
    if (this.data.collaborationAccount === account) return false;
    this.data.collaborationAccount = account;
    this.data.collaborationFiles = {};
    return true;
  }

  getVaultID(): string {
    return this.data.vaultId;
  }

  getCursor(): number {
    return this.data.cursor;
  }

  setCursor(cursor: number): void {
    this.data.cursor = Math.max(this.data.cursor, cursor);
  }

  get(path: string): BaselineEntry | null {
    return this.data.files[normalizePath(path)] ?? null;
  }

  set(path: string, entry: BaselineEntry): void {
    this.data.files[normalizePath(path)] = entry;
  }

  remove(path: string): void {
    delete this.data.files[normalizePath(path)];
  }

  paths(): string[] {
    return Object.keys(this.data.files);
  }

  pending(): PendingOperation[] {
    return [...this.data.pending];
  }

  conflicts(): ConflictEntry[] {
    return [...this.data.conflicts];
  }

  putPending(operation: PendingOperation): void {
    const path = normalizePath(operation.path);
    const oldPath = operation.oldPath ? normalizePath(operation.oldPath) : undefined;
    if (operation.kind === "upsert") {
      const rename = this.data.pending.find((item) => item.kind === "rename" && item.path === path);
      if (rename) return;
    }
    if (operation.kind === "delete") {
      const rename = this.data.pending.find((item) => item.kind === "rename" && item.path === path);
      if (rename?.oldPath) {
        this.data.pending = this.data.pending.filter((item) => item.id !== rename.id);
        this.data.pending.push({ ...operation, path: rename.oldPath });
        return;
      }
    }
    if (operation.kind === "rename" && oldPath) {
      const previousRename = this.data.pending.find(
        (item) => item.kind === "rename" && item.path === oldPath
      );
      if (previousRename?.oldPath) {
        this.data.pending = this.data.pending.filter((item) => item.id !== previousRename.id);
        operation = { ...operation, oldPath: previousRename.oldPath };
      }
    }
    this.data.pending = this.data.pending.filter((item) => {
      if (operation.kind === "rename") {
        return item.path !== path && item.path !== operation.oldPath;
      }
      return item.path !== path;
    });
    this.data.pending.push({
      ...operation,
      path,
      oldPath: operation.oldPath ? normalizePath(operation.oldPath) : undefined,
    });
  }

  removePending(operationID: string): void {
    this.data.pending = this.data.pending.filter((item) => item.id !== operationID);
  }

  removePendingForPath(path: string): void {
    const normalized = normalizePath(path);
    this.data.pending = this.data.pending.filter(
      (item) => item.path !== normalized && item.oldPath !== normalized
    );
  }

  putConflict(conflict: ConflictEntry): void {
    this.data.conflicts = this.data.conflicts.filter((item) => item.path !== conflict.path);
    this.data.conflicts.push({ ...conflict, path: normalizePath(conflict.path) });
  }
  removeConflict(path: string): void {
    this.data.conflicts = this.data.conflicts.filter(
      (item) => item.path !== normalizePath(path)
    );
  }

  getConflict(path: string): ConflictEntry | null {
    return this.data.conflicts.find((item) => item.path === normalizePath(path)) ?? null;
  }

  getCollaboration(vaultId: string, fileId: number): CollaborationBaselineEntry | null {
    const entry = this.data.collaborationFiles[collaborationKey(vaultId, fileId)];
    return entry ? cloneCollaborationEntry(entry) : null;
  }
  setCollaboration(vaultId: string, fileId: number, entry: CollaborationBaselineEntry): void {
    this.data.collaborationFiles[collaborationKey(vaultId, fileId)] = cloneCollaborationEntry(entry);
  }
  removeCollaboration(vaultId: string, fileId: number): boolean {
    const key = collaborationKey(vaultId, fileId);
    if (!(key in this.data.collaborationFiles)) return false;
    delete this.data.collaborationFiles[key];
    return true;
  }
  removeCollaborationByLocalPath(localPath: string): number {
    const normalized = normalizePath(localPath);
    let removed = 0;
    for (const [key, entry] of Object.entries(this.data.collaborationFiles)) {
      if (normalizePath(entry.localPath) === normalized) {
        delete this.data.collaborationFiles[key];
        removed++;
      }
    }
    return removed;
  }
  collaborationEntries(): CollaborationBaselineEntry[] {
    return Object.values(this.data.collaborationFiles).map(cloneCollaborationEntry);
  }
}

export function normalizePath(path: string): string {
  return path.replace(/\\/g, "/").replace(/^\.\/+/, "");
}

function sanitizeCollaborationFiles(raw: unknown): Record<string, CollaborationBaselineEntry> {
  if (!raw || typeof raw !== "object") return {};
  const out: Record<string, CollaborationBaselineEntry> = {};
  for (const [key, value] of Object.entries(raw as Record<string, unknown>)) {
    const sanitized = sanitizeCollaborationEntry(value);
    if (sanitized) {
      // 确保 key 与内容一致，防止持久化篡改
      const expected = collaborationKey(sanitized.vaultId, sanitized.fileId);
      out[expected] = sanitized;
      if (expected !== key) {
        // 忽略原始 key，使用规范 key
      }
    } else if (value && typeof value === "object") {
      const r = value as Record<string, unknown>;
      // 尝试保留有效但 pending 非法的记录（已在 sanitize 中处理）
      // 若整体无效则丢弃该条，防止无效 revision 导致 400
    }
  }
  return out;
}
