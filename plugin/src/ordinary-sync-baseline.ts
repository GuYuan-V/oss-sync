import type { BaselineEntry } from "./baseline.js";
import { decodeMergeableText } from "./text-merge.js";

export type LiveAcknowledgement = {
  readonly kind: "live";
  readonly path: string;
  readonly bytes: Uint8Array;
  readonly serverRevision: number;
  readonly serverHash: string;
  readonly localHash: string;
  readonly localMTime: number;
  readonly localSize: number;
};

export type DeletedAcknowledgement = {
  readonly kind: "deleted";
  readonly path: string;
  readonly serverRevision: number;
  readonly serverHash: string;
  readonly localHash: string;
  readonly localMTime: number;
  readonly localSize: number;
};

export type AcknowledgementInput = LiveAcknowledgement | DeletedAcknowledgement;

export function baselineFromAcknowledgement(input: AcknowledgementInput): BaselineEntry {
  if (input.kind === "deleted") {
    const entry: BaselineEntry = {
      serverRevision: input.serverRevision,
      serverHash: input.serverHash,
      serverDeleted: true,
      localHash: input.localHash,
      localMTime: input.localMTime,
      localSize: input.localSize,
    };
    return entry;
  }
  const decoded = decodeMergeableText(input.path, input.bytes);
  const entry: BaselineEntry = {
    serverRevision: input.serverRevision,
    serverHash: input.serverHash,
    serverDeleted: false,
    localHash: input.localHash,
    localMTime: input.localMTime,
    localSize: input.localSize,
  };
  if (decoded !== null) {
    entry.baseText = decoded;
  }
  return entry;
}
