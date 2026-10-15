// 客户端只读取仓库清单中的版本，安装由 Obsidian 社区插件管理器完成

export const PLUGIN_MANIFEST_URL = "https://raw.githubusercontent.com/helantianshen/oss-sync/HEAD/manifest.json";
export const PLUGIN_VERSION_CHECK_INTERVAL_MS = 30 * 60 * 1000;

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

/** 按 semver 比较两个版本字符串；a < b 返回 -1，a === b 返回 0，a > b 返回 1*/
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

/** remote 是否比 current 新*/
export function isNewerVersion(current: string, remote: string): boolean {
  return compareVersions(remote, current) > 0;
}

/** 清单只接受本插件的正式版本，预发布版本不触发市场更新提示 */
export async function checkPluginVersion(
  currentVersion: string,
  fetchManifest: (url: string) => Promise<{ status: number; json: unknown }>,
): Promise<string | null> {
  const response = await fetchManifest(PLUGIN_MANIFEST_URL);
  if (response.status !== 200) throw new Error(`Plugin manifest HTTP ${response.status}`);
  const manifest = response.json;
  if (!manifest || typeof manifest !== "object") throw new Error("Invalid plugin manifest");
  const { id, version } = manifest as { id?: unknown; version?: unknown };
  if (id !== "oss-sync" || typeof version !== "string" || !/^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/.test(version)) {
    throw new Error("Invalid plugin manifest version or id");
  }
  return isNewerVersion(currentVersion, version) ? version : null;
}
