import type { SyncFileMeta } from "./api.js";

/** 上传被服务端拒绝时返回的 409；current 为服务端当前版本 */
export type OrdinarySyncConflict409 = {
  readonly status: 409;
  readonly current: SyncFileMeta;
};

/** 判断错误是否为普通同步 409，且携带可用的服务端当前版本 */
export function isOrdinarySyncConflict409(
  error: unknown,
): error is OrdinarySyncConflict409 {
  if (!isRecord(error)) {
    return false;
  }
  return error["status"] === 409 && isSyncFileMeta(error["current"]);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

function isSyncFileMeta(value: unknown): value is SyncFileMeta {
  if (!isRecord(value)) {
    return false;
  }
  return (
    typeof value["path"] === "string" &&
    (value["type"] === "markdown" ||
      value["type"] === "attachment" ||
      value["type"] === "config") &&
    typeof value["hash"] === "string" &&
    typeof value["size"] === "number" &&
    typeof value["mtime"] === "number" &&
    typeof value["revision"] === "number" &&
    typeof value["deleted"] === "boolean"
  );
}
