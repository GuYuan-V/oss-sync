import type { SyncFileMeta } from "./api.js";

export type OrdinarySyncConflict409 = {
  readonly status: 409;
  readonly current: SyncFileMeta;
};

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
