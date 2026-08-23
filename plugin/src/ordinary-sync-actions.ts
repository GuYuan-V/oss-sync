import type { BaselineEntry, PendingOperation } from "./baseline.js";
import type { SyncFileMeta } from "./api.js";
import type { OrdinarySyncConflict409 } from "./ordinary-sync-conflict-409.js";

export type ExpectedLocalState =
  | { readonly kind: "absent" }
  | { readonly kind: "hash"; readonly hash: string };

export type OrdinarySyncActionOutcome =
  | { readonly kind: "resolved" }
  | { readonly kind: "conflicted" }
  | {
      readonly kind: "deferred_retry";
      readonly reason: "stale_local" | "second_conflict" | "transport";
      readonly cause?: Error | OrdinarySyncConflict409;
    };

export type OrdinarySyncLocalMeta = {
  readonly path: string;
  readonly hash: string;
  readonly mtime: number;
  readonly size: number;
};

export type OrdinarySyncAction =
  | {
      readonly kind: "upload";
      readonly path: string;
      readonly local: OrdinarySyncLocalMeta;
      readonly baseRevision: number;
      readonly operationID: string;
      readonly operation?: PendingOperation;
    }
  | {
      readonly kind: "delete_remote";
      readonly path: string;
      readonly baseRevision: number;
      readonly operationID: string;
      readonly operation?: PendingOperation;
    }
  | {
      readonly kind: "download";
      readonly path: string;
      readonly remote: SyncFileMeta;
      readonly expectedLocal: ExpectedLocalState;
    }
  | {
      readonly kind: "delete_local";
      readonly path: string;
      readonly remote: SyncFileMeta;
      readonly expectedLocal: ExpectedLocalState;
    }
  | {
      readonly kind: "delete_local_absent";
      readonly path: string;
      readonly expectedLocal: ExpectedLocalState;
    }
  | {
      readonly kind: "adopt";
      readonly path: string;
      readonly local: OrdinarySyncLocalMeta | null;
      readonly remote: SyncFileMeta;
      readonly expectedLocal: ExpectedLocalState;
    }
  | {
      readonly kind: "conflict";
      readonly path: string;
      readonly local: OrdinarySyncLocalMeta | null;
      readonly remote: SyncFileMeta;
      readonly expectedLocal: ExpectedLocalState;
    }
  | {
      readonly kind: "reconcile";
      readonly path: string;
      readonly local: OrdinarySyncLocalMeta;
      readonly remote: SyncFileMeta;
      readonly expectedLocal: ExpectedLocalState;
    };

export type OrdinarySyncPlanEffects = {
  readonly obsoletePendingIds: readonly string[];
  readonly removedBaselinePaths: readonly string[];
};

export type OrdinarySyncPlanResult = {
  readonly actions: readonly OrdinarySyncAction[];
  readonly obsoletePendingIds: readonly string[];
  readonly removedBaselinePaths: readonly string[];
};

export type OrdinarySyncPlannerInput = {
  readonly forceFull: boolean;
  readonly recoverySnapshot: boolean;
  readonly remote: ReadonlyMap<string, SyncFileMeta>;
  readonly baseline: ReadonlyMap<string, BaselineEntry>;
  readonly pending: readonly PendingOperation[];
  readonly localByPath: ReadonlyMap<string, OrdinarySyncLocalMeta | null>;
  readonly conflicts: ReadonlySet<string>;
  readonly vaultPaths: readonly string[];
  readonly createOperationId: () => string;
};

export function assertNeverOrdinarySyncAction(
  value: never,
): never {
  throw new Error(`unhandled OrdinarySyncAction ${String(value)}`);
}
