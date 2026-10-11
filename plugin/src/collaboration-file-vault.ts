import { TFile } from "obsidian";
import type { App } from "obsidian";
import { normalizePath } from "./baseline.js";

/** 协作文件的本地根目录，与黑名单中的目录前缀一致 */
export const COLLAB_DIR = "协作oss";
/** 自身写入产生的事件抑制时长，需覆盖 Obsidian 落盘到触发 modify 的延迟 */
export const SUPPRESS_MS = 5000;

/** 拼接协作者的本地路径 */
export function collabLocalPath(owner: string, filePath: string): string {
  return `${COLLAB_DIR}/${owner}/${filePath}`;
}

/** 判断路径是否位于协作目录内 */
export function isCollabPath(path: string): boolean {
  const normalized = normalizePath(path);
  return normalized === COLLAB_DIR || normalized.startsWith(COLLAB_DIR + "/");
}

/** 协作 vault 访问依赖 */
export interface CollaborationVaultDeps {
  readonly app: App;
}

/** 可解码为文本的读取结果 */
export interface VaultReadResult {
  readonly content: string;
  readonly bytes: Uint8Array;
  readonly hash: string;
}

/** 精确读取结果；content 为 null 表示非 UTF-8 文本 */
export interface ExactVaultReadResult {
  readonly content: string | null;
  readonly bytes: Uint8Array;
  readonly hash: string;
}

/** 管理协作文件的本地读写与短时事件抑制，防止自身写入重复触发同步 */
export class CollaborationFileVault {
  private readonly suppressed = new Set<string>();
  private readonly suppressionTimers = new Map<string, number>();

  constructor(private readonly deps: CollaborationVaultDeps) {}

  /** 路径当前是否处于抑制窗口内 */
  isSuppressed(path: string): boolean {
    return this.suppressed.has(normalizePath(path));
  }

  /** 抑制路径并在窗口结束后自动解除；重复调用会重置计时器 */
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

  /** 立即清除全部抑制与计时器，停止后台任务时调用 */
  clearSuppressed(): void {
    for (const timer of this.suppressionTimers.values()) window.clearTimeout(timer);
    this.suppressionTimers.clear();
    this.suppressed.clear();
  }

  /** 读取可解码文本；文件不存在或非 UTF-8 时返回 null */
  async readLocal(path: string): Promise<VaultReadResult | null> {
    const exact = await this.readExact(path);
    if (!exact || exact.content === null) return null;
    return { content: exact.content, bytes: exact.bytes, hash: exact.hash };
  }

  /** 读取原始字节与哈希，同时尝试严格 UTF-8 解码 */
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

  /** 写入远端正文并抑制自身触发的事件；目录不存在时自动创建 */
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

  /** 对外暴露 SHA-256 十六进制计算，供上传前预估哈希 */
  async hashBytes(bytes: Uint8Array): Promise<string> {
    return hashBytes(bytes);
  }

  /** 删除本地协作文件；目标不存在时返回 false */
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

/** 计算 UTF-8 字节的 SHA-256 十六进制；输入先拷贝，避免 detach 的缓冲被改动 */
export async function hashBytes(bytes: Uint8Array): Promise<string> {
  const copy = bytes.slice();
  const digest = await crypto.subtle.digest("SHA-256", copy);
  return Array.from(new Uint8Array(digest))
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}
