export type DiagnosticEvent =
  | {
      readonly kind: "sse_state";
      readonly at: number;
      readonly state: "connecting" | "open" | "closed" | "failed";
      readonly reason?: "forced" | "fallback";
    }
  | {
      readonly kind: "sse_event";
      readonly at: number;
      readonly event: "changed" | "revoked" | "invited";
      readonly connectionAgeMs: number;
    }
  | {
      readonly kind: "poll";
      readonly at: number;
      readonly scope: "sync" | "collab";
      readonly changed?: boolean;
      readonly version?: number;
      readonly durationMs: number;
      readonly failed?: true;
    }
  | {
      readonly kind: "api";
      readonly at: number;
      readonly method: "GET" | "POST" | "PATCH" | "DELETE";
      readonly status?: number;
      readonly durationMs: number;
    }
  | {
      readonly kind: "transfer";
      readonly at: number;
      readonly scope: "upload" | "download" | "collab_upload" | "collab_download";
      readonly durationMs: number;
      readonly bytes?: number;
    }
  | { readonly kind: "collab_activity"; readonly at: number; readonly entries: number; readonly newestCreatedAt?: string };

export class Diagnostics {
  private readonly events: DiagnosticEvent[] = [];

  constructor(
    private readonly sink?: (event: DiagnosticEvent) => void,
    private readonly capacity = 200
  ) {}

  record(event: DiagnosticEvent): void {
    const safeEvent = sanitizeEvent(event);
    if (!safeEvent) return;
    this.events.push(Object.freeze(safeEvent));
    if (this.events.length > this.capacity) this.events.shift();
    this.sink?.(safeEvent);
  }

  snapshot(): readonly DiagnosticEvent[] {
    return [...this.events];
  }

  clear(): void {
    this.events.length = 0;
  }
}

function sanitizeEvent(event: DiagnosticEvent): DiagnosticEvent | null {
  switch (event.kind) {
    case "sse_state":
      if (!isSSEState(event.state)) return null;
      return event.reason === "forced" || event.reason === "fallback"
        ? { kind: "sse_state", at: finiteNumber(event.at), state: event.state, reason: event.reason }
        : { kind: "sse_state", at: finiteNumber(event.at), state: event.state };
    case "sse_event":
      if (!isSSEEvent(event.event)) return null;
      return {
        kind: "sse_event",
        at: finiteNumber(event.at),
        event: event.event,
        connectionAgeMs: finiteNumber(event.connectionAgeMs),
      };
    case "poll":
      if (event.scope !== "sync" && event.scope !== "collab") return null;
      return {
        kind: "poll",
        at: finiteNumber(event.at),
        scope: event.scope,
        ...(typeof event.changed === "boolean" ? { changed: event.changed } : {}),
        ...(typeof event.version === "number" ? { version: finiteNumber(event.version) } : {}),
        durationMs: finiteNumber(event.durationMs),
        ...(event.failed === true ? { failed: true } : {}),
      };
    case "api":
      if (!isHTTPMethod(event.method)) return null;
      return {
        kind: "api",
        at: finiteNumber(event.at),
        method: event.method,
        ...(typeof event.status === "number" ? { status: finiteNumber(event.status) } : {}),
        durationMs: finiteNumber(event.durationMs),
      };
    case "transfer":
      if (!isTransferScope(event.scope)) return null;
      return {
        kind: "transfer",
        at: finiteNumber(event.at),
        scope: event.scope,
        durationMs: finiteNumber(event.durationMs),
        ...(typeof event.bytes === "number" ? { bytes: finiteNumber(event.bytes) } : {}),
      };
    case "collab_activity":
      return {
        kind: "collab_activity",
        at: finiteNumber(event.at),
        entries: finiteNumber(event.entries),
        ...(normalizedTimestamp(event.newestCreatedAt) ? { newestCreatedAt: normalizedTimestamp(event.newestCreatedAt) } : {}),
      };
  }
}

function finiteNumber(value: number): number {
  return Number.isFinite(value) ? value : 0;
}

function normalizedTimestamp(value: string | undefined): string | undefined {
  if (!value) return undefined;
  const timestamp = Date.parse(value);
  return Number.isNaN(timestamp) ? undefined : new Date(timestamp).toISOString();
}

function isSSEState(value: string): value is "connecting" | "open" | "closed" | "failed" {
  return value === "connecting" || value === "open" || value === "closed" || value === "failed";
}

function isSSEEvent(value: string): value is "changed" | "revoked" | "invited" {
  return value === "changed" || value === "revoked" || value === "invited";
}

function isHTTPMethod(value: string): value is "GET" | "POST" | "PATCH" | "DELETE" {
  return value === "GET" || value === "POST" || value === "PATCH" || value === "DELETE";
}

function isTransferScope(value: string): value is "upload" | "download" | "collab_upload" | "collab_download" {
  return value === "upload" || value === "download" || value === "collab_upload" || value === "collab_download";
}
