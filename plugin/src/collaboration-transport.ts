import type { Diagnostics } from "./diagnostics";

const COLLAB_POLL_WAIT_SEC = 30;
const COLLAB_INBOX_REFRESH_MS = 30_000;

export type CollaborationTransportStatus =
  | "disconnected"
  | "sse"
  | "sse_failed"
  | "sse_unavailable"
  | "long_poll";

export interface CollaborationTransportDeps {
  readonly api: {
    readonly hasToken: () => boolean;
    readonly collabAccountEventStreamURL: () => string | null;
    readonly collabAccountPoll: (after: number, waitSeconds: number) => Promise<{ readonly changed: boolean; readonly version: number }>;
  };
  readonly diagnostics?: Diagnostics;
  readonly getVaultId: () => string;
  readonly getForceSSE: () => boolean;
  readonly onRefresh: () => Promise<void>;
  readonly onChanged: () => Promise<void>;
  readonly onStatusChange: () => void;
}

export class CollaborationTransport {
  private started = false;
  private status: CollaborationTransportStatus = "disconnected";
  private after = 0;
  private pollGen = 0;
  private pollController: AbortController | null = null;
  private eventSource: EventSource | null = null;
  private streamOpenedAt: number | null = null;
  private inboxTimer: number | null = null;

  constructor(private readonly deps: CollaborationTransportDeps) {}

  isRunning(): boolean {
    return this.started;
  }

  getStatus(): CollaborationTransportStatus {
    return this.status;
  }

  start(): void {
    if (this.started) return;
    this.started = true;
    this.after = 0;
    void this.deps.onRefresh();
    this.inboxTimer = window.setInterval(() => void this.deps.onRefresh(), COLLAB_INBOX_REFRESH_MS);
    const streamURL = this.deps.api.collabAccountEventStreamURL();
    if (streamURL) {
      this.startSSE(streamURL);
      return;
    }
    if (this.deps.getForceSSE()) {
      this.status = "sse_unavailable";
      this.deps.onStatusChange();
      return;
    }
    this.startLongPoll();
  }

  stop(): void {
    this.started = false;
    this.pollGen++;
    if (this.pollController) {
      this.pollController.abort();
      this.pollController = null;
    }
    if (this.eventSource) this.deps.diagnostics?.record({ kind: "sse_state", at: Date.now(), state: "closed" });
    this.eventSource?.close();
    this.eventSource = null;
    this.streamOpenedAt = null;
    if (this.inboxTimer !== null) {
      window.clearInterval(this.inboxTimer);
      this.inboxTimer = null;
    }
    this.status = "disconnected";
  }

  private startSSE(streamURL: string): void {
    const source = new EventSource(streamURL);
    this.eventSource = source;
    this.status = "sse";
    this.streamOpenedAt = Date.now();
    this.deps.diagnostics?.record({ kind: "sse_state", at: this.streamOpenedAt, state: "connecting" });
    this.deps.onStatusChange();
    source.onopen = () => this.deps.diagnostics?.record({ kind: "sse_state", at: Date.now(), state: "open" });
    const events: readonly ("changed" | "revoked" | "invited")[] = ["changed", "revoked", "invited"];
    for (const event of events) {
      source.addEventListener(event, () => {
        this.deps.diagnostics?.record({
          kind: "sse_event",
          at: Date.now(),
          event,
          connectionAgeMs: Date.now() - (this.streamOpenedAt ?? Date.now()),
        });
        if (event === "changed") {
          void this.deps.onChanged();
        } else {
          void this.deps.onRefresh();
        }
      });
    }
    source.onerror = () => {
      if (!this.started || this.eventSource !== source) return;
      source.close();
      this.eventSource = null;
      this.deps.diagnostics?.record({
        kind: "sse_state",
        at: Date.now(),
        state: "failed",
        reason: this.deps.getForceSSE() ? "forced" : "fallback",
      });
      if (this.deps.getForceSSE()) {
        this.failSSE();
        return;
      }
      this.startLongPoll();
    };
  }

  private failSSE(): void {
    this.status = "sse_failed";
    this.deps.onStatusChange();
  }

  private startLongPoll(): void {
    this.status = "long_poll";
    this.deps.onStatusChange();
    const gen = ++this.pollGen;
    const controller = new AbortController();
    this.pollController = controller;
    void this.pollLoop(gen, controller);
  }

  private async pollLoop(gen: number, controller: AbortController): Promise<void> {
    while (this.started && !controller.signal.aborted && gen === this.pollGen) {
      const vaultId = this.deps.getVaultId();
      if (!vaultId || !this.deps.api.hasToken()) return;
      const startedAt = Date.now();
      try {
        const result = await this.deps.api.collabAccountPoll(this.after, COLLAB_POLL_WAIT_SEC);
        if (!this.started || controller.signal.aborted || gen !== this.pollGen) return;
        this.after = result.version;
        this.deps.diagnostics?.record({
          kind: "poll",
          at: Date.now(),
          scope: "collab",
          changed: result.changed,
          version: result.version,
          durationMs: Date.now() - startedAt,
        });
        if (result.changed) {
          await this.deps.onChanged();
        }
      } catch {
        if (!this.started || controller.signal.aborted || gen !== this.pollGen) return;
        this.deps.diagnostics?.record({
          kind: "poll",
          at: Date.now(),
          scope: "collab",
          durationMs: Date.now() - startedAt,
          failed: true,
        });
        await sleep(3000);
      }
    }
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}
