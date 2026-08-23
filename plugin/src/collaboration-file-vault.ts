import { TFile } from "obsidian";
import type { App } from "obsidian";
import { normalizePath } from "./baseline.js";

export const COLLAB_DIR = "协作oss";
export const SUPPRESS_MS = 5000;

export function collabLocalPath(owner: string, filePath: string): string {
  return `${COLLAB_DIR}/${owner}/${filePath}`;
}

export function isCollabPath(path: string): boolean {
  const normalized = normalizePath(path);
  return normalized === COLLAB_DIR || normalized.startsWith(COLLAB_DIR + "/");
}

export interface CollaborationVaultDeps {
  readonly app: App;
}

export interface VaultReadResult {
  readonly content: string;
  readonly bytes: Uint8Array;
  readonly hash: string;
}

export interface ExactVaultReadResult {
  readonly content: string | null;
  readonly bytes: Uint8Array;
  readonly hash: string;
}

export class CollaborationFileVault {
  private readonly suppressed = new Set<string>();
  private readonly suppressionTimers = new Map<string, number>();

  constructor(private readonly deps: CollaborationVaultDeps) {}

  isSuppressed(path: string): boolean {
    return this.suppressed.has(normalizePath(path));
  }

  suppress(path: string): void {
    const key = normalizePath(path);
    const existing = this.suppressionTimers.get(key);
    if (existing !== undefined) window.clearTimeout(existing);
    this.suppressed.add(key);
    const timer = window.setTimeout(() => {
      this.suppressed.delete(key);
      this.suppressionTimers.delete(key);
    }, SUPPRESS_MS);
    this.suppressionTimers.set(key, timer);
  }

  clearSuppressed(): void {
    for (const timer of this.suppressionTimers.values()) window.clearTimeout(timer);
    this.suppressionTimers.clear();
    this.suppressed.clear();
  }

  async readLocal(path: string): Promise<VaultReadResult | null> {
    const exact = await this.readExact(path);
    if (!exact || exact.content === null) return null;
    return { content: exact.content, bytes: exact.bytes, hash: exact.hash };
  }

  async readExact(path: string): Promise<ExactVaultReadResult | null> {
    const key = normalizePath(path);
    const file = this.deps.app.vault.getAbstractFileByPath(key);
    if (!(file instanceof TFile)) return null;
    const buffer = await this.deps.app.vault.readBinary(file);
    const bytes = new Uint8Array(buffer);
    const hash = await hashBytes(bytes);
    let content: string | null;
    try {
      content = new TextDecoder("utf-8", { fatal: true }).decode(bytes);
    } catch {
      content = null;
    }
    return { content, bytes, hash };
  }

  async writeCanonical(path: string, bytes: Uint8Array): Promise<void> {
    const key = normalizePath(path);
    await this.ensureParentFolders(key);
    this.suppress(key);
    const existing = this.deps.app.vault.getAbstractFileByPath(key);
    const buffer = (() => {
      const copy = new ArrayBuffer(bytes.byteLength);
      new Uint8Array(copy).set(bytes);
      return copy;
    })();
    if (existing instanceof TFile) {
      await this.deps.app.vault.modifyBinary(existing, buffer);
    } else {
      await this.deps.app.vault.createBinary(key, buffer);
    }
  }

  async hashBytes(bytes: Uint8Array): Promise<string> {
    return hashBytes(bytes);
  }

  async deleteLocal(path: string): Promise<boolean> {
    const key = normalizePath(path);
    const file = this.deps.app.vault.getAbstractFileByPath(key);
    if (!(file instanceof TFile)) return false;
    this.suppress(key);
    await this.deps.app.vault.delete(file, false);
    return true;
  }

  private async ensureParentFolders(path: string): Promise<void> {
    const parts = normalizePath(path).split("/");
    parts.pop();
    let current = "";
    for (const part of parts) {
      current = current ? `${current}/${part}` : part;
      if (!this.deps.app.vault.getAbstractFileByPath(current)) {
        try {
          await this.deps.app.vault.createFolder(current);
        } catch {
          if (!this.deps.app.vault.getAbstractFileByPath(current)) {
            throw new Error(`mkdir failed: ${current}`);
          }
        }
      }
    }
  }
}

export async function hashBytes(bytes: Uint8Array): Promise<string> {
  const copy = bytes.slice();
  const digest = await crypto.subtle.digest("SHA-256", copy);
  return Array.from(new Uint8Array(digest))
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}
