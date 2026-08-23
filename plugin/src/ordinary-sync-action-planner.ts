import type { BaselineEntry, PendingOperation } from "./baseline.js";
import type { SyncFileMeta } from "./api.js";
import type { ExpectedLocalState, OrdinarySyncAction, OrdinarySyncLocalMeta, OrdinarySyncPlannerInput, OrdinarySyncPlanResult } from "./ordinary-sync-actions.js";

export type OrdinarySyncCandidatePathsInput = Pick<
  OrdinarySyncPlannerInput,
  "forceFull" | "remote" | "baseline" | "pending" | "conflicts" | "vaultPaths"
>;

function classify(path: string): "markdown" | "attachment" | "config" {
  const lower = path.toLowerCase();
  if (lower.endsWith(".md")) return "markdown";
  if (lower.startsWith(".obsidian/")) return "config";
  return "attachment";
}
function remoteFromBaseline(path: string, baseline: BaselineEntry): SyncFileMeta {
  return { path, type: classify(path), hash: baseline.serverHash, size: baseline.localSize, mtime: baseline.localMTime, revision: baseline.serverRevision, deleted: baseline.serverDeleted };
}
function compactedDelete(path: string): SyncFileMeta {
  return { path, type: classify(path), hash: "", size: 0, mtime: 0, revision: 0, deleted: true };
}
function expected(local: OrdinarySyncLocalMeta | null): ExpectedLocalState {
  if (local === null) return { kind: "absent" };
  return { kind: "hash", hash: local.hash };
}

export function ordinarySyncCandidatePaths(
  input: OrdinarySyncCandidatePathsInput,
): readonly string[] {
  const renamePaths = new Set<string>();
  const paths = new Set<string>();

  for (const operation of input.pending) {
    if (operation.kind === "rename") {
      renamePaths.add(operation.path);
      if (operation.oldPath) renamePaths.add(operation.oldPath);
    } else {
      paths.add(operation.path);
    }
  }
  for (const path of input.remote.keys()) paths.add(path);
  if (input.forceFull) {
    for (const path of input.baseline.keys()) paths.add(path);
    for (const path of input.vaultPaths) paths.add(path);
  }

  return [...paths].filter(
    (path) => !renamePaths.has(path) && !input.conflicts.has(path),
  );
}

export function planOrdinarySyncActions(input: OrdinarySyncPlannerInput): OrdinarySyncPlanResult {
  const { forceFull, recoverySnapshot, remote, baseline, pending, localByPath, conflicts, vaultPaths, createOperationId } = input;
  const pendingByPath = new Map<string, PendingOperation>();
  for (const op of pending) {
    if (op.kind !== "rename") pendingByPath.set(op.path, op);
  }
  const actions: OrdinarySyncAction[] = [];
  const obsoleteSet = new Set<string>();
  const removedSet = new Set<string>();
  for (const path of ordinarySyncCandidatePaths({
    forceFull,
    remote,
    baseline,
    pending,
    conflicts,
    vaultPaths,
  })) {
    const base = baseline.get(path) ?? null;
    const local = localByPath.get(path) ?? null;
    const submitted = remote.get(path);
    const server = submitted ?? (!forceFull && base !== null ? remoteFromBaseline(path, base) : undefined);
    const operation = pendingByPath.get(path);
    if (base === null) {
      if (operation?.kind === "delete") {
        if (server !== undefined && !server.deleted) {
          actions.push({ kind: "delete_remote", path, baseRevision: server.revision, operationID: operation.id, operation });
        } else if (server?.deleted === true && local === null) {
          actions.push({ kind: "adopt", path, local: null, remote: server, expectedLocal: expected(local) });
        } else if (server?.deleted === true) {
          actions.push({ kind: "conflict", path, local, remote: server, expectedLocal: expected(local) });
        } else obsoleteSet.add(operation.id);
      } else if (local !== null && server === undefined) {
        actions.push({ kind: "upload", path, local, baseRevision: 0, operationID: operation?.id ?? createOperationId(), operation });
      } else if (local === null && server !== undefined && !server.deleted) {
        actions.push({ kind: "download", path, remote: server, expectedLocal: expected(local) });
      } else if (local !== null && server !== undefined && !server.deleted && local.hash === server.hash) {
        actions.push({ kind: "adopt", path, local, remote: server, expectedLocal: expected(local) });
      } else if (local !== null && server !== undefined && !server.deleted) {
        actions.push({ kind: "reconcile", path, local, remote: server, expectedLocal: expected(local) });
      } else if (local !== null && server !== undefined) {
        actions.push({ kind: "conflict", path, local, remote: server, expectedLocal: expected(local) });
      } else if (local === null && server?.deleted === true) {
        actions.push({ kind: "adopt", path, local: null, remote: server, expectedLocal: expected(local) });
      } else if (operation !== undefined) obsoleteSet.add(operation.id);
      continue;
    }
    if (recoverySnapshot && submitted === undefined) {
      if (base.serverDeleted) {
        removedSet.add(path);
        if (local !== null) actions.push({ kind: "upload", path, local, baseRevision: 0, operationID: operation?.id ?? createOperationId(), operation });
        else if (operation !== undefined) obsoleteSet.add(operation.id);
      } else if (local === null) {
        removedSet.add(path);
        if (operation !== undefined) obsoleteSet.add(operation.id);
      } else if (local.hash === base.serverHash) {
        actions.push({ kind: "delete_local_absent", path, expectedLocal: expected(local) });
      } else {
        actions.push({ kind: "conflict", path, local, remote: compactedDelete(path), expectedLocal: expected(local) });
      }
      continue;
    }
    const localChanged = base.serverDeleted ? local !== null : local === null || local.hash !== base.serverHash;
    const remoteChanged = server !== undefined ? server.revision !== base.serverRevision || server.hash !== base.serverHash || server.deleted !== base.serverDeleted : forceFull;
    if (!localChanged && !remoteChanged) {
      if (operation !== undefined) obsoleteSet.add(operation.id);
      continue;
    }
    if (localChanged && !remoteChanged) {
      if (local !== null) actions.push({ kind: "upload", path, local, baseRevision: base.serverRevision, operationID: operation?.id ?? createOperationId(), operation });
      else actions.push({ kind: "delete_remote", path, baseRevision: base.serverRevision, operationID: operation?.id ?? createOperationId(), operation });
      continue;
    }
    if (!localChanged && remoteChanged && server !== undefined) {
      if (server.deleted) actions.push({ kind: "delete_local", path, remote: server, expectedLocal: expected(local) });
      else actions.push({ kind: "download", path, remote: server, expectedLocal: expected(local) });
      continue;
    }
    if (local !== null && server !== undefined && !server.deleted && local.hash === server.hash) {
      actions.push({ kind: "adopt", path, local, remote: server, expectedLocal: expected(local) });
    } else if (local !== null && server !== undefined && !server.deleted) {
      actions.push({ kind: "reconcile", path, local, remote: server, expectedLocal: expected(local) });
    } else if (server !== undefined) {
      actions.push({ kind: "conflict", path, local, remote: server, expectedLocal: expected(local) });
    } else if (operation !== undefined) obsoleteSet.add(operation.id);
  }
  return { actions, obsoletePendingIds: [...obsoleteSet], removedBaselinePaths: [...removedSet] };
}
