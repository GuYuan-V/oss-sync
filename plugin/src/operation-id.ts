// operation-id.ts — 统一的 collaboration operationID 生成与校验
// 规则与服务端 deviceauth.NormalizeClientID 保持一致：
// 1-64 字符，仅允许 A-Z a-z 0-9 - _ .
// 优先使用 crypto.randomUUID()，其输出完全符合该字符集。

const OPERATION_ID_RE = /^[A-Za-z0-9._-]{1,64}$/;

export function isValidOperationID(value: unknown): value is string {
  return typeof value === "string" && OPERATION_ID_RE.test(value);
}

export function normalizeOperationID(value: string): string | null {
  const trimmed = value.trim();
  if (!isValidOperationID(trimmed)) return null;
  return trimmed;
}

export function createOperationID(): string {
  // crypto.randomUUID() 在 Obsidian/Electron 环境始终可用
  try {
    const raw = crypto.randomUUID();
    const normalized = normalizeOperationID(raw);
    if (normalized) return normalized;
  } catch {
    // fallback
  }
  // 备用：时间戳 + 随机串，同样符合字符集
  const fallback = `${Date.now()}-${Math.random().toString(36).slice(2, 10)}-${Math.random().toString(36).slice(2, 10)}`;
  // 确保仅含合法字符且 <=64
  const cleaned = fallback.replace(/[^A-Za-z0-9._-]/g, "-").slice(0, 64);
  if (isValidOperationID(cleaned)) return cleaned;
  // 极端兜底：截断 UUID 风格
  return "op-" + Math.random().toString(36).slice(2, 10);
}

export function ensureOperationID(value: unknown, fallback: () => string = createOperationID): string {
  if (isValidOperationID(value)) return value;
  if (typeof value === "string") {
    const n = normalizeOperationID(value);
    if (n) return n;
  }
  return fallback();
}

export function isValidServerRevision(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0;
}
