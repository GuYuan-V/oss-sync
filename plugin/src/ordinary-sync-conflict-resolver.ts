import { baselineFromAcknowledgement } from "./ordinary-sync-baseline.js";
import { isOrdinarySyncConflict409 } from "./ordinary-sync-conflict-409.js";
import { decideOrdinarySyncReconciliation } from "./ordinary-sync-reconcile.js";
import type { BaselineEntry, PendingOperation } from "./baseline.js";
import type { SyncFileMeta } from "./api.js";
import type {
  ExpectedLocalState,
  OrdinarySyncActionOutcome,
} from "./ordinary-sync-actions.js";
import type {
  LocalReadResult,
  OrdinarySyncFileAccess,
} from "./ordinary-sync-file-access.js";

export { isOrdinarySyncConflict409 } from "./ordinary-sync-conflict-409.js";
export type { OrdinarySyncConflict409 } from "./ordinary-sync-conflict-409.js";

export interface OrdinarySyncResolverBaseline {
  readonly get: (path: string) => BaselineEntry | null;
  readonly set: (path: string, entry: BaselineEntry) => void;
  readonly save: () => Promise<void>;
  readonly putPending: (op: PendingOperation) => void;
  readonly removePending: (id: string) => void;
  readonly removePendingForPath: (path: string) => void;
}

export interface OrdinarySyncResolverApi {
  readonly downloadV2: (
    vaultId: string,
    path: string,
    revision: number,
  ) => Promise<{ readonly content: ArrayBuffer; readonly meta: SyncFileMeta }>;
  readonly uploadV2: (
    vaultId: string,
    input: {
      readonly path: string;
      readonly baseRevision: number;
      readonly hash: string;
      readonly mtime: number;
      readonly operationID: string;
      readonly content: ArrayBuffer;
    },
  ) => Promise<SyncFileMeta>;
}

export interface OrdinarySyncResolverDeps {
  readonly baseline: OrdinarySyncResolverBaseline;
  readonly fileAccess: OrdinarySyncFileAccess;
  readonly api: OrdinarySyncResolverApi;
  readonly recordConflict: (
    path: string,
    local: LocalReadResult | null,
    remote: SyncFileMeta,
  ) => Promise<void>;
  readonly createOperationID: () => string;
  readonly now: () => number;
}

export interface OrdinarySyncResolveInput {
  readonly vaultId: string;
  readonly path: string;
  readonly expectedHash: string;
  readonly remote: SyncFileMeta;
}

type RemoteChange = {
  readonly path: string;
  readonly expected: ExpectedLocalState;
  readonly local: LocalReadResult;
  readonly remoteBytes: Uint8Array;
  readonly remote: SyncFileMeta;
};

type MergedChange = RemoteChange & {
  readonly vaultId: string;
  readonly mergedBytes: Uint8Array;
};

type Conflict = {
  readonly path: string;
  readonly local: LocalReadResult | null;
  readonly remote: SyncFileMeta;
};

export class OrdinarySyncConflictResolver {
  constructor(private readonly deps: OrdinarySyncResolverDeps) {}

  async resolve(input: OrdinarySyncResolveInput): Promise<OrdinarySyncActionOutcome> {
    const expected: ExpectedLocalState = { kind: "hash", hash: input.expectedHash };
    if (input.remote.deleted) {
      return this.conflict({
        path: input.path,
        local: await this.deps.fileAccess.readExact(input.path),
        remote: input.remote,
      });
    }

    const downloaded = await this.deps.api.downloadV2(
      input.vaultId,
      input.path,
      input.remote.revision,
    );
    const local = await this.deps.fileAccess.readExact(input.path);
    if (local === null || local.hash !== expected.hash) {
      return this.conflict({ path: input.path, local, remote: downloaded.meta });
    }

    const change: RemoteChange = {
      path: input.path,
      expected,
      local,
      remoteBytes: copyBytes(downloaded.content),
      remote: downloaded.meta,
    };
    const decision = decideOrdinarySyncReconciliation({
      path: change.path,
      baseText: this.deps.baseline.get(change.path)?.baseText ?? null,
      localBytes: change.local.bytes,
      remoteBytes: change.remoteBytes,
    });
    switch (decision.kind) {
      case "text_conflict":
        return this.conflict(change);
      case "preserve_both":
        return this.preserveBoth(change);
      case "merged":
        return this.uploadMerged({
          ...change,
          vaultId: input.vaultId,
          mergedBytes: decision.bytes,
        });
      default:
        return assertNever(decision);
    }
  }

  private async preserveBoth(change: RemoteChange): Promise<OrdinarySyncActionOutcome> {
    const sibling = await this.deps.fileAccess.preserveSibling(
      change.path,
      change.expected,
      change.local.bytes.slice(),
    );
    if (sibling.kind !== "preserved") {
      return this.conflict({
        ...change,
        local: sibling.kind === "stale" ? actualLocal(sibling.actual, change.local) : change.local,
      });
    }

    this.deps.baseline.putPending({
      id: this.deps.createOperationID(),
      kind: "upsert",
      path: sibling.siblingPath,
      createdAt: this.deps.now(),
    });
    await this.deps.baseline.save();

    const installed = await this.deps.fileAccess.replace(
      change.path,
      change.expected,
      change.remoteBytes.slice(),
    );
    if (installed.kind !== "replaced") {
      return this.conflict({ ...change, local: installed.actual ?? change.local });
    }

    this.stageRemote(change, installed.snapshot);
    this.deps.baseline.removePendingForPath(change.path);
    await this.deps.baseline.save();
    return { kind: "resolved" };
  }

  private async uploadMerged(change: MergedChange): Promise<OrdinarySyncActionOutcome> {
    const operation: PendingOperation = {
      id: this.deps.createOperationID(),
      kind: "upsert",
      path: change.path,
      createdAt: this.deps.now(),
    };
    this.deps.baseline.putPending(operation);
    await this.deps.baseline.save();

    const merged = change.mergedBytes.slice();
    const installed = await this.deps.fileAccess.replace(
      change.path,
      change.expected,
      merged.slice(),
    );
    if (installed.kind !== "replaced") {
      this.deps.baseline.removePending(operation.id);
      await this.deps.baseline.save();
      return this.conflict({ ...change, local: actualLocal(installed.actual, change.local) });
    }

    this.stageRemote(change, installed.snapshot);
    await this.deps.baseline.save();
    let acknowledged: SyncFileMeta;
    try {
      acknowledged = await this.deps.api.uploadV2(change.vaultId, {
        path: change.path,
        baseRevision: change.remote.revision,
        hash: installed.snapshot.hash,
        mtime: installed.snapshot.mtime,
        operationID: operation.id,
        content: toArrayBuffer(merged),
      });
    } catch (error) {
      return deferred(error);
    }

    this.deps.baseline.set(
      change.path,
      baselineFromAcknowledgement({
        kind: "live",
        path: change.path,
        bytes: merged.slice(),
        serverRevision: acknowledged.revision,
        serverHash: acknowledged.hash,
        localHash: installed.snapshot.hash,
        localMTime: installed.snapshot.mtime,
        localSize: installed.snapshot.size,
      }),
    );
    this.deps.baseline.removePending(operation.id);
    await this.deps.baseline.save();
    return { kind: "resolved" };
  }

  private stageRemote(change: RemoteChange, local: LocalReadResult): void {
    this.deps.baseline.set(
      change.path,
      baselineFromAcknowledgement({
        kind: "live",
        path: change.path,
        bytes: change.remoteBytes.slice(),
        serverRevision: change.remote.revision,
        serverHash: change.remote.hash,
        localHash: local.hash,
        localMTime: local.mtime,
        localSize: local.size,
      }),
    );
  }

  private async conflict(conflict: Conflict): Promise<OrdinarySyncActionOutcome> {
    await this.deps.recordConflict(conflict.path, conflict.local, conflict.remote);
    return { kind: "conflicted" };
  }
}

function copyBytes(content: ArrayBuffer): Uint8Array {
  return new Uint8Array(content).slice();
}

function toArrayBuffer(bytes: Uint8Array): ArrayBuffer {
  const copy = new ArrayBuffer(bytes.byteLength);
  new Uint8Array(copy).set(bytes);
  return copy;
}

function actualLocal(actual: LocalReadResult | null | undefined, previous: LocalReadResult) {
  return actual === undefined ? previous : actual;
}

function deferred(error: unknown): OrdinarySyncActionOutcome {
  if (isOrdinarySyncConflict409(error)) {
    return { kind: "deferred_retry", reason: "second_conflict", cause: error };
  }
  if (error instanceof Error) {
    return { kind: "deferred_retry", reason: "transport", cause: error };
  }
  throw error;
}

function assertNever(value: never): never {
  throw new Error(`unhandled ordinary sync reconciliation ${String(value)}`);
}
