import { diff_match_patch } from "diff-match-patch";

export type ConflictDiffRow =
  | { kind: "context"; text: string }
  | { kind: "removed"; text: string }
  | { kind: "added"; text: string }
  | { kind: "omitted"; count: number };

export class LineCapacityExceededError extends Error {
  constructor() {
    super("too many unique lines for single-code-unit line encoding");
    this.name = "LineCapacityExceededError";
  }
}

const CODE_MIN = 0x0001;
const CODE_MAX = 0xffff;
const LF = 0x000a;
const CR = 0x000d;
const SURROGATE_MIN = 0xd800;
const SURROGATE_MAX = 0xdfff;

// 将 CRLF 与 CR 统一为 LF，单个末尾换行不产生空行，中间空行保留为有效行。
function normalizeLines(text: string): string[] {
  const lines = text.replace(/\r\n?/g, "\n").split("\n");
  if (lines[lines.length - 1] === "") {
    lines.pop();
  }
  return lines;
}

// 每行映射为一个 UTF-16 码元（排除 NUL、LF、CR 与代理区），并以 LF 分隔，使行级差异可按字符级差异计算。
class LineCodec {
  private readonly lineToCode = new Map<string, string>();
  private readonly codeToLine: Record<string, string> = {};
  private nextCodeUnit = CODE_MIN;

  encode(lines: string[]): string {
    let chars = "";
    for (const line of lines) {
      chars += this.codeOf(line);
      chars += "\n";
    }
    return chars;
  }

  decode(chars: string): string[] {
    const lines: string[] = [];
    for (const part of chars.split("\n")) {
      if (part !== "") {
        lines.push(this.codeToLine[part]);
      }
    }
    return lines;
  }

  private codeOf(line: string): string {
    const existing = this.lineToCode.get(line);
    if (existing !== undefined) {
      return existing;
    }
    while (this.nextCodeUnit <= CODE_MAX) {
      const codeUnit = this.nextCodeUnit;
      this.nextCodeUnit += 1;
      if (codeUnit === LF || codeUnit === CR) {
        continue;
      }
      if (codeUnit >= SURROGATE_MIN && codeUnit <= SURROGATE_MAX) {
        continue;
      }
      const code = String.fromCharCode(codeUnit);
      this.lineToCode.set(line, code);
      this.codeToLine[code] = line;
      return code;
    }
    throw new LineCapacityExceededError();
  }
}

// 压缩连续 context 行：首段超过 2 行仅保留末尾 2 行，尾段超过 2 行仅保留开头 2 行，中段超过 4 行仅保留首尾各 2 行，其余完整保留。
export function collapseContextRows(rows: ConflictDiffRow[]): ConflictDiffRow[] {
  const out: ConflictDiffRow[] = [];
  let i = 0;
  while (i < rows.length) {
    const row = rows[i];
    if (row.kind !== "context") {
      out.push(row);
      i += 1;
      continue;
    }
    let end = i;
    while (end < rows.length && rows[end].kind === "context") {
      end += 1;
    }
    const count = end - i;
    if (i === 0 && count > 2) {
      out.push({ kind: "omitted", count: count - 2 }, ...rows.slice(end - 2, end));
    } else if (end === rows.length && count > 2) {
      out.push(...rows.slice(i, i + 2), { kind: "omitted", count: count - 2 });
    } else if (count > 4) {
      out.push(
        ...rows.slice(i, i + 2),
        { kind: "omitted", count: count - 4 },
        ...rows.slice(end - 2, end)
      );
    } else {
      out.push(...rows.slice(i, end));
    }
    i = end;
  }
  return out;
}

export function buildConflictDiff(local: string, remote: string): ConflictDiffRow[] {
  const codec = new LineCodec();
  const encodedLocal = codec.encode(normalizeLines(local));
  const encodedRemote = codec.encode(normalizeLines(remote));

  const dmp = new diff_match_patch();
  const diffs = dmp.diff_main(encodedLocal, encodedRemote, false);
  dmp.diff_cleanupMerge(diffs);

  // 按替换块缓存变更行，使同一块内 removed 始终排在 added 之前，避免 diff_main 交错输出打乱顺序。
  const rows: ConflictDiffRow[] = [];
  const removed: string[] = [];
  const added: string[] = [];
  const flush = (): void => {
    for (const line of removed) {
      rows.push({ kind: "removed", text: line });
    }
    for (const line of added) {
      rows.push({ kind: "added", text: line });
    }
    removed.length = 0;
    added.length = 0;
  };
  for (const [op, text] of diffs) {
    const lines = codec.decode(text);
    if (op === 0) {
      if (lines.length > 0) {
        flush();
        for (const line of lines) {
          rows.push({ kind: "context", text: line });
        }
      }
    } else if (op === -1) {
      removed.push(...lines);
    } else {
      added.push(...lines);
    }
  }
  flush();

  if (!rows.some((r) => r.kind === "removed" || r.kind === "added")) {
    return [];
  }
  return collapseContextRows(rows);
}
