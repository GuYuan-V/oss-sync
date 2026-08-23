import { TFile } from "obsidian";
import type { Vault } from "obsidian";
import { normalizePath } from "./baseline.js";
import type { ExpectedLocalState } from "./ordinary-sync-actions.js";

export interface LocalReadResult {
  readonly bytes: Uint8Array;
  readonly hash: string;
  readonly mtime: number;
  readonly size: number;
}

export type CreateResult =
  | { readonly kind: "created"; readonly snapshot: LocalReadResult }
  | { readonly kind: "stale"; readonly actual: LocalReadResult | null };

export type ReplaceResult =
  | { readonly kind: "replaced"; readonly snapshot: LocalReadResult }
  | { readonly kind: "stale"; readonly actual: LocalReadResult | null };

export type DeleteResult =
  | { readonly kind: "deleted" }
  | { readonly kind: "stale"; readonly actual: LocalReadResult | null };

export type WriteCanonicalResult =
  | { readonly kind: "written"; readonly snapshot: LocalReadResult }
  | { readonly kind: "stale"; readonly reason: "missing" | "mutated"; readonly actual?: LocalReadResult | null };

export type PreserveSiblingResult =
  | { readonly kind: "preserved"; readonly siblingPath: string; readonly snapshot: LocalReadResult }
  | { readonly kind: "stale"; readonly reason: "missing" | "mutated"; readonly actual?: LocalReadResult | null }
  | { readonly kind: "collision"; readonly siblingPath: string };

export function conflictCopyPath(path: string, now: number = Date.now()): string {
  const slash = path.lastIndexOf("/");
  const directory = slash >= 0 ? path.slice(0, slash + 1) : "";
  const filename = slash >= 0 ? path.slice(slash + 1) : path;
  const dot = filename.lastIndexOf(".");
  const base = dot > 0 ? filename.slice(0, dot) : filename;
  const extension = dot > 0 ? filename.slice(dot) : "";
  const timestamp = new Date(now).toISOString().replace(/[:.]/g, "-");
  return `${directory}${base}_conflict_${timestamp}${extension}`;
}

export class OrdinarySyncFileAccess {
  private readonly now: () => number;

  constructor(
    private readonly vault: Vault,
    private readonly suppress: (path: string) => void = () => undefined,
    now: () => number = () => Date.now(),
  ) {
    this.now = now;
  }

  async readExact(path: string): Promise<LocalReadResult | null> {
    const key = normalizePath(path);
    const file = this.vault.getAbstractFileByPath(key);
    if (!(file instanceof TFile)) {
      return null;
    }
    const buffer = await this.vault.readBinary(file);
    const bytes = new Uint8Array(buffer);
    const copy = bytes.slice();
    const hash = await hashBytes(copy);
    return {
      bytes: copy,
      hash,
      mtime: file.stat.mtime,
      size: file.stat.size,
    };
  }

  async create(path: string, expected: ExpectedLocalState, bytes: Uint8Array): Promise<CreateResult> {
    const key = normalizePath(path);
    await this.ensureParentFolders(key);
    const actual = await this.readExact(key);
    const matches = isExpected(actual, expected);
    if (!matches) {
      return { kind: "stale", actual };
    }
    if (actual !== null) {
      return { kind: "stale", actual };
    }
    const fresh = freshBuffer(bytes);
    this.suppress(key);
    await this.vault.createBinary(key, fresh);
    const snapshot = await this.snapshotFromBytes(bytes, key);
    return { kind: "created", snapshot };
  }

  async replace(path: string, expected: ExpectedLocalState, bytes: Uint8Array): Promise<ReplaceResult> {
    const key = normalizePath(path);
    await this.ensureParentFolders(key);
    const actual = await this.readExact(key);
    if (!isExpected(actual, expected)) {
      return { kind: "stale", actual };
    }
    if (actual === null) {
      return { kind: "stale", actual };
    }
    const file = this.vault.getAbstractFileByPath(key);
    if (!(file instanceof TFile)) {
      return { kind: "stale", actual };
    }
    const fresh = freshBuffer(bytes);
    this.suppress(key);
    await this.vault.modifyBinary(file, fresh);
    const snapshot = await this.snapshotFromBytes(bytes, key);
    return { kind: "replaced", snapshot };
  }

  async deleteExact(path: string, expected: ExpectedLocalState): Promise<DeleteResult> {
    const key = normalizePath(path);
    const actual = await this.readExact(key);
    if (!isExpected(actual, expected)) {
      return { kind: "stale", actual };
    }
    if (actual === null) {
      return { kind: "deleted" };
    }
    const file = this.vault.getAbstractFileByPath(key);
    if (!(file instanceof TFile)) {
      return { kind: "stale", actual };
    }
    this.suppress(key);
    await this.vault.delete(file);
    return { kind: "deleted" };
  }

  async writeCanonicalIfUnchanged(
    path: string,
    expectedHash: string,
    newBytes: Uint8Array,
  ): Promise<WriteCanonicalResult> {
    const expected: ExpectedLocalState = { kind: "hash", hash: expectedHash };
    const result = await this.replace(path, expected, newBytes);
    if (result.kind === "replaced") {
      return { kind: "written", snapshot: result.snapshot };
    }
    const actual = result.actual;
    if (actual === null) {
      return { kind: "stale", reason: "missing", actual };
    }
    return { kind: "stale", reason: "mutated", actual };
  }

  async preserveSibling(
    canonicalPath: string,
    expected: ExpectedLocalState,
    localBytes: Uint8Array,
  ): Promise<PreserveSiblingResult> {
    const key = normalizePath(canonicalPath);
    const actual = await this.readExact(key);
    if (!isExpected(actual, expected)) {
      if (actual === null) {
        return { kind: "stale", reason: "missing", actual };
      }
      return { kind: "stale", reason: "mutated", actual };
    }
    if (actual === null) {
      return { kind: "stale", reason: "missing", actual };
    }
    const siblingPath = conflictCopyPath(key, this.now());
    const existing = this.vault.getAbstractFileByPath(siblingPath);
    if (existing !== null) {
      return { kind: "collision", siblingPath };
    }
    await this.ensureParentFolders(siblingPath);
    const fresh = freshBuffer(localBytes);
    this.suppress(siblingPath);
    await this.vault.createBinary(siblingPath, fresh);
    const snapshot = await this.snapshotFromBytes(localBytes, siblingPath);
    return { kind: "preserved", siblingPath, snapshot };
  }

  async preserveSiblingIfUnchanged(
    canonicalPath: string,
    expectedHash: string,
    localBytes: Uint8Array,
  ): Promise<PreserveSiblingResult> {
    const expected: ExpectedLocalState = { kind: "hash", hash: expectedHash };
    return this.preserveSibling(canonicalPath, expected, localBytes);
  }

  private async snapshotFromBytes(bytes: Uint8Array, path: string): Promise<LocalReadResult> {
    const snapshotBytes = bytes.slice();
    const snapshotHash = await hashBytes(snapshotBytes.slice());
    const created = this.vault.getAbstractFileByPath(path);
    let mtime = 0;
    let size = snapshotBytes.length;
    if (created instanceof TFile) {
      mtime = created.stat.mtime;
      size = created.stat.size;
    }
    return { bytes: snapshotBytes, hash: snapshotHash, mtime, size };
  }

  private async ensureParentFolders(path: string): Promise<void> {
    const parts = normalizePath(path).split("/");
    parts.pop();
    let current = "";
    for (const part of parts) {
      current = current ? `${current}/${part}` : part;
      if (this.vault.getAbstractFileByPath(current) === null) {
        try {
          await this.vault.createFolder(current);
        } catch (error) {
          if (this.vault.getAbstractFileByPath(current) === null) {
            throw error;
          }
        }
      }
    }
  }
}

function isExpected(actual: LocalReadResult | null, expected: ExpectedLocalState): boolean {
  if (expected.kind === "absent") {
    return actual === null;
  }
  return actual !== null && actual.hash === expected.hash;
}

function freshBuffer(bytes: Uint8Array): ArrayBuffer {
  const copy = new Uint8Array(bytes.length);
  copy.set(bytes);
  return copy.buffer;
}

async function hashBytes(bytes: Uint8Array): Promise<string> {
  const copy = bytes.slice();
  const digest = await crypto.subtle.digest("SHA-256", copy);
  return Array.from(new Uint8Array(digest))
    .map((value) => value.toString(16).padStart(2, "0"))
    .join("");
}
