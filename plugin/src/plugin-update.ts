// GitHub Release 在线更新支持。
//
// 模块不依赖 Obsidian，便于在 Node 测试中注入伪造的 fetch 与文件系统。
// 更新流程：查询最新 Release → 读取 manifest.json 得到远端版本 → 下载三件套 →
// 由 plugin-update-apply.ts 原子替换并重载插件。

export interface HttpResult {
  status: number;
  json: unknown;
  text: string;
  arrayBuffer: ArrayBuffer;
  headers: Record<string, string>;
}

export type HttpFetch = (options: {
  url: string;
  method: string;
  headers?: Record<string, string>;
}) => Promise<HttpResult>;

export interface GitHubReleaseAsset {
  name: string;
  browser_download_url: string;
}

export interface GitHubRelease {
  tag_name: string;
  html_url: string;
  assets: GitHubReleaseAsset[];
}

export interface UpdateFile {
  name: string;
  content: ArrayBuffer;
}

export const UPDATE_FILE_NAMES = ["main.js", "manifest.json", "styles.css"] as const;

// 版本比较

interface ParsedVersion {
  core: readonly number[];
  pre: string | null;
}

function parseVersion(value: string): ParsedVersion {
  const raw = value.trim();
  const dash = raw.indexOf("-");
  const coreText = dash >= 0 ? raw.slice(0, dash) : raw;
  const pre = dash >= 0 ? raw.slice(dash + 1) : null;
  const core = coreText.split(".").map((part) => {
    const n = parseInt(part, 10);
    return Number.isNaN(n) ? 0 : n;
  });
  while (core.length < 3) core.push(0);
  return { core: core.slice(0, 3), pre };
}

/** 按 semver 比较两个版本字符串；a < b 返回 -1，a === b 返回 0，a > b 返回 1。 */
export function compareVersions(a: string, b: string): number {
  const pa = parseVersion(a);
  const pb = parseVersion(b);
  for (let i = 0; i < 3; i++) {
    if (pa.core[i] !== pb.core[i]) return pa.core[i] < pb.core[i] ? -1 : 1;
  }
  return comparePreRelease(pa.pre, pb.pre);
}

function comparePreRelease(a: string | null, b: string | null): number {
  if (a === b) return 0;
  if (a === null) return 1; // 正式版高于预发布版
  if (b === null) return -1;
  const as = a.split(".");
  const bs = b.split(".");
  const length = Math.max(as.length, bs.length);
  for (let i = 0; i < length; i++) {
    const x = as[i];
    const y = bs[i];
    if (x === undefined) return -1;
    if (y === undefined) return 1;
    const xNumeric = /^\d+$/.test(x);
    const yNumeric = /^\d+$/.test(y);
    if (xNumeric && yNumeric) {
      const xn = parseInt(x, 10);
      const yn = parseInt(y, 10);
      if (xn !== yn) return xn < yn ? -1 : 1;
    } else if (xNumeric !== yNumeric) {
      return xNumeric ? -1 : 1; // 数字标识符小于字母数字标识符
    } else if (x !== y) {
      return x < y ? -1 : 1;
    }
  }
  return 0;
}

/** remote 是否比 current 新。 */
export function isNewerVersion(current: string, remote: string): boolean {
  return compareVersions(remote, current) > 0;
}

/** 去掉 Release 标签前缀的 v/V。 */
export function parseVersionTag(tag: string): string {
  return tag.trim().replace(/^[vV]/, "");
}

/** 从 Release 内的 manifest.json 文本解析版本；解析失败返回 null。 */
export function manifestVersionFromText(text: string | null): string | null {
  if (!text) return null;
  try {
    const parsed = JSON.parse(text) as { version?: unknown };
    return typeof parsed.version === "string" && parsed.version ? parsed.version : null;
  } catch {
    return null;
  }
}

// GitHub Release 查询与下载

export class GitHubReleaseSource {
  constructor(private readonly fetchImpl: HttpFetch) {}

  async latestRelease(repo: string): Promise<GitHubRelease> {
    if (!/^[\w.-]+\/[\w.-]+$/.test(repo)) {
      throw new Error(`invalid GitHub repository "${repo}"`);
    }
    const result = await this.fetchImpl({
      url: `https://api.github.com/repos/${repo}/releases/latest`,
      method: "GET",
      headers: {
        Accept: "application/vnd.github+json",
        "User-Agent": "oss-sync-obsidian-plugin",
      },
    });
    if (result.status === 404) {
      throw new Error(`no GitHub releases found for ${repo}`);
    }
    if (result.status >= 400) {
      throw new Error(`GitHub API error: HTTP ${result.status}`);
    }
    const body = result.json as Partial<GitHubRelease> & { assets?: unknown };
    if (typeof body.tag_name !== "string" || !body.tag_name) {
      throw new Error("GitHub release response is missing tag_name");
    }
    const assets = Array.isArray(body.assets) ? body.assets.filter(isReleaseAsset) : [];
    return {
      tag_name: body.tag_name,
      html_url: typeof body.html_url === "string" ? body.html_url : "",
      assets,
    };
  }

  /** 读取 Release 中指定资产的文本；资产缺失或下载失败返回 null。 */
  async downloadAssetText(release: GitHubRelease, name: string): Promise<string | null> {
    const asset = release.assets.find((item) => item.name === name);
    if (!asset) return null;
    const result = await this.fetchImpl({ url: asset.browser_download_url, method: "GET" });
    if (result.status >= 400) return null;
    return result.text || new TextDecoder().decode(result.arrayBuffer);
  }

  /** 下载指定资产；资产缺失或 HTTP 错误时抛错。 */
  async downloadAsset(release: GitHubRelease, name: string): Promise<ArrayBuffer> {
    const asset = release.assets.find((item) => item.name === name);
    if (!asset) {
      throw new Error(`release asset "${name}" not found`);
    }
    const result = await this.fetchImpl({ url: asset.browser_download_url, method: "GET" });
    if (result.status >= 400) {
      throw new Error(`failed to download "${name}": HTTP ${result.status}`);
    }
    return result.arrayBuffer;
  }
}

function isReleaseAsset(value: unknown): value is GitHubReleaseAsset {
  if (!value || typeof value !== "object") return false;
  const record = value as Record<string, unknown>;
  return (
    typeof record.name === "string" &&
    typeof record.browser_download_url === "string"
  );
}

// 更新检查与资源下载

export interface UpdateCheckResult {
  readonly currentVersion: string;
  readonly remoteVersion: string;
  readonly release: GitHubRelease;
}

export function isUpdateAvailable(result: UpdateCheckResult): boolean {
  return isNewerVersion(result.currentVersion, result.remoteVersion);
}

export async function checkForUpdates(
  repo: string,
  currentVersion: string,
  source: GitHubReleaseSource
): Promise<UpdateCheckResult> {
  const release = await source.latestRelease(repo);
  const manifestText = await source.downloadAssetText(release, "manifest.json");
  const remoteVersion = manifestVersionFromText(manifestText) ?? parseVersionTag(release.tag_name);
  return { currentVersion, remoteVersion, release };
}

export async function downloadUpdateAssets(
  source: GitHubReleaseSource,
  release: GitHubRelease
): Promise<UpdateFile[]> {
  const files: UpdateFile[] = [];
  for (const name of UPDATE_FILE_NAMES) {
    files.push({ name, content: await source.downloadAsset(release, name) });
  }
  return files;
}
