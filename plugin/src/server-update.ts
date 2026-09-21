// 服务端更新轮询控制器，逻辑纯粹且有界，不依赖 Obsidian
import type {
  ServerUpdateStatusResponse,
  ServerVersionInfo,
} from "./api";
import { OSSApiError } from "./api";

export const SERVER_UPDATE_POLL_INTERVAL_MS = 2000;
export const SERVER_UPDATE_MAX_ATTEMPTS = 60;
export const SERVER_UPDATE_MAX_DURATION_MS = 120_000;

export type ServerUpdateTerminalState = "done" | "failed" | "up_to_date";

const TERMINAL_STATES = new Set<string>(["done", "failed", "up_to_date"]);
const TERMINAL_PHASES = new Set<string>(["done", "failed", "up_to_date"]);

export function isTerminalServerState(state: string): boolean {
  return TERMINAL_STATES.has(state);
}

export function isTerminalServerPhase(phase: string): boolean {
  return TERMINAL_PHASES.has(phase);
}

export function isStaleRoleError(error: unknown): boolean {
  const status = (error as { status?: unknown })?.status;
  if (typeof status === "number") {
    return status === 401 || status === 403;
  }
  if (error instanceof OSSApiError) {
    return error.status === 401 || error.status === 403;
  }
  return false;
}

export function isTransientConnectionError(error: unknown): boolean {
  const status = (error as { status?: unknown })?.status;
  if (typeof status === "number") {
    if (status === 401 || status === 403) return false;
    return false;
  }
  if (error instanceof OSSApiError) {
    // 401 与 403 表示角色变化，不属于瞬断
    if (error.status === 401 || error.status === 403) return false;
    // 其余 OSSApiError 同样在此返回 false，由调用方继续判定
    return false;
  }
  const message = error instanceof Error ? error.message : String(error ?? "");
  return (
    message.includes("ERR_CONNECTION_REFUSED") ||
    message.includes("ERR_CONNECTION_TIMED_OUT") ||
    message.includes("ERR_NAME_NOT_RESOLVED") ||
    message.includes("ERR_INTERNET_DISCONNECTED") ||
    message.includes("ERR_NETWORK_CHANGED") ||
    message.includes("ECONNREFUSED") ||
    message.includes("ETIMEDOUT") ||
    message.includes("Failed to fetch") ||
    message.includes("NetworkError") ||
    message.includes("network") ||
    message.includes("fetch failed")
  );
}

export interface PollOutcomeSuccess {
  readonly kind: "success";
  readonly version: string;
  readonly status: ServerUpdateStatusResponse;
}

export interface PollOutcomeFailed {
  readonly kind: "failed";
  readonly state: string;
  readonly error: string;
  readonly status: ServerUpdateStatusResponse;
}

export interface PollOutcomeRolledBack {
  readonly kind: "rolled_back";
  readonly versionBefore: string;
  readonly currentVersion: string;
  readonly status: ServerUpdateStatusResponse;
}

export interface PollOutcomeTimeout {
  readonly kind: "timeout";
  readonly attempts: number;
  readonly lastStatus: ServerUpdateStatusResponse | null;
}

export interface PollOutcomeAuthError {
  readonly kind: "auth_error";
  readonly error: unknown;
}

export type PollOutcome =
  | PollOutcomeSuccess
  | PollOutcomeFailed
  | PollOutcomeRolledBack
  | PollOutcomeTimeout
  | PollOutcomeAuthError;

export interface ServerUpdatePollerOptions {
  readonly expectedVersion: string;
  readonly intervalMs?: number;
  readonly maxAttempts?: number;
  readonly maxDurationMs?: number;
}

export interface ServerUpdatePollerDeps {
  readonly getStatus: () => Promise<ServerUpdateStatusResponse>;
  readonly getVersion: () => Promise<ServerVersionInfo>;
}

export class ServerUpdatePoller {
  private timer: ReturnType<typeof setTimeout> | null = null;
  private aborted = false;
  private startedAt = 0;
  private attempts = 0;
  private readonly intervalMs: number;
  private readonly maxAttempts: number;
  private readonly maxDurationMs: number;

  constructor(
    private readonly deps: ServerUpdatePollerDeps,
    private readonly opts: ServerUpdatePollerOptions,
    private readonly sleep: (ms: number) => Promise<void> = (ms) => new Promise<void>((resolve) => setTimeout(resolve, ms)),
  ) {
    this.intervalMs = opts.intervalMs ?? SERVER_UPDATE_POLL_INTERVAL_MS;
    this.maxAttempts = opts.maxAttempts ?? SERVER_UPDATE_MAX_ATTEMPTS;
    this.maxDurationMs = opts.maxDurationMs ?? SERVER_UPDATE_MAX_DURATION_MS;
  }

  getAttempts(): number {
    return this.attempts;
  }

  isAborted(): boolean {
    return this.aborted;
  }

  stop(): void {
    this.aborted = true;
    if (this.timer !== null) {
      clearTimeout(this.timer);
      this.timer = null;
    }
  }

  dispose(): void {
    this.stop();
  }

  async poll(signal?: AbortSignal): Promise<PollOutcome> {
    this.aborted = false;
    this.attempts = 0;
    this.startedAt = Date.now();
    let lastStatus: ServerUpdateStatusResponse | null = null;
    let consecutiveTransientErrors = 0;

    while (!this.aborted) {
      if (signal?.aborted) {
        this.aborted = true;
        break;
      }
      if (this.attempts >= this.maxAttempts) {
        return { kind: "timeout", attempts: this.attempts, lastStatus };
      }
      if (Date.now() - this.startedAt >= this.maxDurationMs) {
        return { kind: "timeout", attempts: this.attempts, lastStatus };
      }
      this.attempts += 1;

      try {
        const status = await this.deps.getStatus();
        lastStatus = status;
        consecutiveTransientErrors = 0;

        const outcome = this.evaluateTerminal(status);
        if (outcome !== null) return outcome;

        // 未进入终态时休眠后继续下一轮，轮询次数与总时长有上限
      } catch (error: unknown) {
        if (isStaleRoleError(error)) {
          return { kind: "auth_error", error };
        }
        if (isTransientConnectionError(error)) {
          consecutiveTransientErrors += 1;
          // 重启期间连接中断属于预期情况，继续有界轮询
          // 连续瞬断仍受尝试次数与总时长上限约束
        } else {
          // 非瞬断的 API 错误在轮询中同样计为可容忍错误，继续等待
          // 重启期间的 5xx 错误同样继续轮询
          const rawStatus = (error as { status?: unknown })?.status;
          const statusNum = typeof rawStatus === "number" ? rawStatus : error instanceof OSSApiError ? error.status : undefined;
          const is5xx = typeof statusNum === "number" && statusNum >= 500;
          const is4xx = typeof statusNum === "number" && statusNum >= 400 && statusNum < 500;
          if (is5xx) {
            consecutiveTransientErrors += 1;
          } else if (is4xx) {
            // 除 401/403 外的 4xx 计为失败尝试，仍在有界次数内继续轮询
            consecutiveTransientErrors += 1;
          } else {
            consecutiveTransientErrors += 1;
          }
        }
        // 瞬断次数无单独上限，由尝试次数与总时长兜底
      }

      if (this.aborted || signal?.aborted) break;
      await this.sleep(this.intervalMs);
    }

    return { kind: "timeout", attempts: this.attempts, lastStatus };
  }

  private evaluateTerminal(status: ServerUpdateStatusResponse): PollOutcome | null {
    const expected = normalizeVersion(this.opts.expectedVersion);
    const versionMatches = normalizeVersion(status.version) === expected;
    const lastUpdate = status.last_update;
    const state = status.state;
    const phase = lastUpdate?.phase ?? state;

    // 终态判定依据 state 或 phase 是否为 done、failed 或 up_to_date
    const isTerminal =
      isTerminalServerState(state) ||
      isTerminalServerState(phase) ||
      (lastUpdate != null &&
        (isTerminalServerPhase(lastUpdate.state) ||
          isTerminalServerPhase(lastUpdate.phase) ||
          isTerminalServerState(lastUpdate.state)));

    if (!isTerminal) {
      // 版本一致且 state 为 done 时直接视为成功
      if (versionMatches && state === "done") return { kind: "success", version: status.version, status };
      return null;
    }

    // 终态下按版本号与 ok 标记区分成功、回滚与失败
    if (versionMatches && (lastUpdate?.ok === true || state === "done" || phase === "done")) {
      return { kind: "success", version: status.version, status };
    }

    if (!versionMatches && (state === "failed" || lastUpdate?.state === "failed" || phase === "failed")) {
      const error = lastUpdate?.error ?? `server update failed (state=${state})`;
      // 回滚的启发式判定：已失败且版本号仍为旧版本
      if (lastUpdate?.code === "failed" || state === "failed") {
        if (normalizeVersion(status.version) !== expected) {
          return { kind: "rolled_back", versionBefore: this.opts.expectedVersion, currentVersion: status.version, status };
        }
      }
      return { kind: "failed", state: state, error, status };
    }

    if (state === "failed" || lastUpdate?.state === "failed" || phase === "failed") {
      const error = lastUpdate?.error ?? `server update failed (state=${state})`;
      if (!versionMatches) {
        return { kind: "rolled_back", versionBefore: this.opts.expectedVersion, currentVersion: status.version, status };
      }
      return { kind: "failed", state, error, status };
    }

    // state 为 up_to_date 时视为未发生更新，按失败返回
    if (state === "up_to_date") {
      return { kind: "failed", state, error: lastUpdate?.error ?? "already up to date", status };
    }

    if (isTerminal && versionMatches) {
      return { kind: "success", version: status.version, status };
    }

    return { kind: "failed", state, error: lastUpdate?.error ?? `terminal state=${state} phase=${phase}`, status };
  }
}

function normalizeVersion(value: string): string {
  return value.trim().replace(/^[vV]/, "");
}

export function createBoundedPoller(
  deps: ServerUpdatePollerDeps,
  opts: ServerUpdatePollerOptions,
  sleep?: (ms: number) => Promise<void>,
): ServerUpdatePoller {
  return new ServerUpdatePoller(deps, opts, sleep);
}
