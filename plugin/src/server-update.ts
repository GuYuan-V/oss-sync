// Server update polling controller — pure bounded logic with no Obsidian dependency.
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
    // 401/403 are not transient — they mean role changed.
    if (error.status === 401 || error.status === 403) return false;
    // Other 5xx / Bad Gateway during restart are treated as transient by caller if needed,
    // but explicit network errors are also transient.
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

        // Not terminal yet — wait and loop.
      } catch (error: unknown) {
        if (isStaleRoleError(error)) {
          return { kind: "auth_error", error };
        }
        if (isTransientConnectionError(error)) {
          consecutiveTransientErrors += 1;
          // Tolerate expected restart connection loss — continue bounded polling.
          // If we see many transient errors in a row, still bounded by attempts/duration.
        } else {
          // Non-transient API error: treat as transient for polling unless it's terminal condition.
          // But 5xx BadGateway etc during restart should also be tolerated as transient.
          const rawStatus = (error as { status?: unknown })?.status;
          const statusNum = typeof rawStatus === "number" ? rawStatus : error instanceof OSSApiError ? error.status : undefined;
          const is5xx = typeof statusNum === "number" && statusNum >= 500;
          const is4xx = typeof statusNum === "number" && statusNum >= 400 && statusNum < 500;
          if (is5xx) {
            consecutiveTransientErrors += 1;
          } else if (is4xx) {
            // 4xx other than 401/403 — surface as failed attempt but continue polling for bounded retries
            consecutiveTransientErrors += 1;
          } else {
            consecutiveTransientErrors += 1;
          }
        }
        // If max transient errors not bounded, attempts/duration will bound.
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

    // Validate terminal operation status/version: terminal when state/phase is done/failed/up_to_date.
    const isTerminal =
      isTerminalServerState(state) ||
      isTerminalServerState(phase) ||
      (lastUpdate != null &&
        (isTerminalServerPhase(lastUpdate.state) ||
          isTerminalServerPhase(lastUpdate.phase) ||
          isTerminalServerState(lastUpdate.state)));

    if (!isTerminal) {
      // Also consider version change as terminal success even if helper hasn't reported done yet but version flipped.
      if (versionMatches && state === "done") return { kind: "success", version: status.version, status };
      return null;
    }

    // Terminal — distinguish success/rollback/failure by version + ok flag.
    if (versionMatches && (lastUpdate?.ok === true || state === "done" || phase === "done")) {
      return { kind: "success", version: status.version, status };
    }

    if (!versionMatches && (state === "failed" || lastUpdate?.state === "failed" || phase === "failed")) {
      const error = lastUpdate?.error ?? `server update failed (state=${state})`;
      // Heuristic for rollback: failed but version remains old.
      if (lastUpdate?.code === "failed" || state === "failed") {
        // If version did not change, it's rolled_back or failed.
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

    // up_to_date or idle with version mismatch implies no update happened — treat as failed if expected newer.
    if (state === "up_to_date") {
      return { kind: "failed", state, error: lastUpdate?.error ?? "already up to date", status };
    }

    if (isTerminal && versionMatches) {
      return { kind: "success", version: status.version, status };
    }

    // Fallback: terminal but unmatched.
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
