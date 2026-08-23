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
  | { readonly kind: "collab_activity"; readonly at: number; readonly entries: number; readonly newestCreatedAt?: string }
  | {
      readonly kind: "api_error";
      readonly at: number;
      readonly scope: "collab_upload";
      readonly status: number;
      readonly code?: string;
      readonly reason?: string;
    }
  | {
      readonly kind: "runtime_info";
      readonly at: number;
      readonly pluginVersion: string;
      readonly buildCommit?: string;
      readonly buildTime?: string;
      readonly serverVersion?: string;
      readonly diagnosticsEnabled: boolean;
    }
  | {
      readonly kind: "diagnostics_enabled";
      readonly at: number;
      readonly enabled: boolean;
    }
  | {
      readonly kind: "collab_upload_attempt";
      readonly at: number;
      readonly hasBaseRevision: boolean;
      readonly baseRevisionValid: boolean;
      readonly hasOperationID: boolean;
      readonly operationIDValid: boolean;
      readonly operationIDLength: number;
    };

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
    case "api_error": {
      if (event.scope !== "collab_upload") return null;
      const status = finiteNumber(event.status);
      if (!Number.isFinite(status)) return null;
      return {
        kind: "api_error",
        at: finiteNumber(event.at),
        scope: event.scope,
        status,
        ...(typeof event.code === "string" && event.code ? { code: event.code } : {}),
        ...(typeof event.reason === "string" && event.reason ? { reason: event.reason } : {}),
      };
    }
    case "runtime_info": {
      if (typeof event.pluginVersion !== "string" || !event.pluginVersion) return null;
      return {
        kind: "runtime_info",
        at: finiteNumber(event.at),
        pluginVersion: event.pluginVersion.slice(0, 64),
        ...(typeof event.buildCommit === "string" && event.buildCommit ? { buildCommit: event.buildCommit.slice(0, 64) } : {}),
        ...(typeof event.buildTime === "string" && event.buildTime ? { buildTime: event.buildTime.slice(0, 64) } : {}),
        ...(typeof event.serverVersion === "string" && event.serverVersion ? { serverVersion: event.serverVersion.slice(0, 64) } : {}),
        diagnosticsEnabled: !!event.diagnosticsEnabled,
      };
    }
    case "diagnostics_enabled": {
      return {
        kind: "diagnostics_enabled",
        at: finiteNumber(event.at),
        enabled: !!event.enabled,
      };
    }
    case "collab_upload_attempt": {
      return {
        kind: "collab_upload_attempt",
        at: finiteNumber(event.at),
        hasBaseRevision: !!event.hasBaseRevision,
        baseRevisionValid: !!event.baseRevisionValid,
        hasOperationID: !!event.hasOperationID,
        operationIDValid: !!event.operationIDValid,
        operationIDLength: finiteNumber(event.operationIDLength),
      };
    }
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
