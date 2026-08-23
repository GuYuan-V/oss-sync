// collaboration-upload-helpers.ts — 统一的协作上传参数准备
// 所有协上传入口必须经过此函数，避免直接构造 {content, baseRevision, operationID}

import { isValidOperationID, isValidServerRevision, createOperationID } from "./operation-id.js";
import type { CollaborationBaselineEntry } from "./collaboration-state.js";
import type { CollaborationUploadInput } from "./api.js";

export interface PreparedCollaborationUpload {
  readonly input: CollaborationUploadInput;
  readonly pendingID: string;
}

export function prepareCollaborationUpload(
  entry: CollaborationBaselineEntry,
  content: string,
  createID: () => string,
): PreparedCollaborationUpload | { error: "missing_base_revision" | "invalid_base_revision" | "missing_operation_id" | "invalid_operation_id" } {
  const hasBase = entry.serverRevision !== undefined && entry.serverRevision !== null;
  const baseValid = isValidServerRevision(entry.serverRevision);
  if (!hasBase) return { error: "missing_base_revision" };
  if (!baseValid) return { error: "invalid_base_revision" };

  // operationID 优先复用 pending（若本地未变），否则生成新的
  // 调用方应传入已验证的 entry.pending，不在这里做 hash 比较
  let op = entry.pending?.id ?? "";
  if (!isValidOperationID(op)) {
    const created = (() => {
      try { return createID(); } catch { return createOperationID(); }
    })();
    op = isValidOperationID(created) ? created : createOperationID();
  }
  if (!isValidOperationID(op)) return { error: "invalid_operation_id" };
  if (op.trim() === "") return { error: "missing_operation_id" };

  return {
    input: { content, baseRevision: entry.serverRevision, operationID: op },
    pendingID: op,
  };
}

export function operationIDErrorCode(raw: string): "missing_operation_id" | "invalid_operation_id" {
  return raw.trim() === "" ? "missing_operation_id" : "invalid_operation_id";
}
