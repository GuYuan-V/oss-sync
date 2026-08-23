import { diff3Merge } from "node-diff3";

export const MAX_MERGE_BYTES = 2 * 1024 * 1024;

export type TextMergeResult =
  | { kind: "merged"; content: string }
  | { kind: "conflict" };

export type MergeRegion =
  | { readonly kind: "resolved"; readonly lines: readonly string[] }
  | {
      readonly kind: "conflict";
      readonly baseLines: readonly string[];
      readonly localLines: readonly string[];
      readonly remoteLines: readonly string[];
    };

export type BlockOrder = "local_first" | "remote_first";

const ALLOWED_EXTENSIONS: readonly string[] = [
  ".md",
  ".markdown",
  ".txt",
  ".json",
  ".yaml",
  ".yml",
  ".toml",
  ".css",
  ".js",
  ".ts",
  ".canvas",
];

function normalizeToLf(text: string): string {
  return text.replace(/\r\n?/g, "\n");
}

function toLines(text: string): string[] {
  const normalized = normalizeToLf(text);
  if (normalized === "") {
    return [];
  }
  return normalized.split("\n");
}

function isAllowedExtension(path: string): boolean {
  const lower = path.toLowerCase();
  for (const ext of ALLOWED_EXTENSIONS) {
    if (lower.endsWith(ext)) {
      return true;
    }
  }
  return false;
}

function mergeIndependentReplacements(
  local: string[],
  base: string[],
  remote: string[],
): string[] | null {
  if (local.length !== base.length || remote.length !== base.length) {
    return null;
  }
  const merged: string[] = [];
  for (let index = 0; index < base.length; index += 1) {
    if (local[index] === remote[index]) {
      merged.push(local[index]);
    } else if (local[index] === base[index]) {
      merged.push(remote[index]);
    } else if (remote[index] === base[index]) {
      merged.push(local[index]);
    } else {
      return null;
    }
  }
  return merged;
}

export function buildMergeRegions(
  baseText: string,
  localText: string,
  remoteText: string,
): MergeRegion[] {
  const baseLines = toLines(baseText);
  const localLines = toLines(localText);
  const remoteLines = toLines(remoteText);
  const raw = diff3Merge(localLines, baseLines, remoteLines, {
    excludeFalseConflicts: true,
  });
  const out: MergeRegion[] = [];
  for (const region of raw) {
    if (region.conflict !== undefined) {
      const resolved = mergeIndependentReplacements(
        region.conflict.a,
        region.conflict.o,
        region.conflict.b,
      );
      if (resolved) {
        out.push({ kind: "resolved", lines: resolved });
      } else {
        out.push({
          kind: "conflict",
          baseLines: region.conflict.o.slice(),
          localLines: region.conflict.a.slice(),
          remoteLines: region.conflict.b.slice(),
        });
      }
    } else if (region.ok !== undefined) {
      out.push({ kind: "resolved", lines: region.ok.slice() });
    }
  }
  return out;
}

export function hasConflictRegion(regions: readonly MergeRegion[]): boolean {
  return regions.some((r) => r.kind === "conflict");
}

export function resolveOrderedMerge(
  regions: readonly MergeRegion[],
  orderForConflictIndex?: (conflictIndex: number) => BlockOrder,
): string {
  const merged: string[] = [];
  let conflictIdx = 0;
  for (const region of regions) {
    if (region.kind === "resolved") {
      merged.push(...region.lines);
    } else {
      const order = orderForConflictIndex?.(conflictIdx) ?? "local_first";
      conflictIdx += 1;
      if (order === "local_first") {
        merged.push(...region.localLines, ...region.remoteLines);
      } else {
        merged.push(...region.remoteLines, ...region.localLines);
      }
    }
  }
  return merged.join("\n");
}

export function mergeText(
  baseText: string,
  localText: string,
  remoteText: string,
): TextMergeResult {
  const regions = buildMergeRegions(baseText, localText, remoteText);
  if (hasConflictRegion(regions)) {
    return { kind: "conflict" };
  }
  const content = resolveOrderedMerge(regions);
  return { kind: "merged", content };
}

export function decodeMergeableText(
  path: string,
  bytes: Uint8Array,
): string | null {
  if (!isAllowedExtension(path)) {
    return null;
  }
  if (bytes.length > MAX_MERGE_BYTES) {
    return null;
  }
  try {
    const decoder = new TextDecoder("utf-8", { fatal: true });
    return decoder.decode(bytes);
  } catch (error) {
    if (error instanceof TypeError) {
      return null;
    }
    throw error;
  }
}
