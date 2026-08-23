import type { CollaborationBaselineEntry } from "./collaboration-state.js";
import { decodeMergeableText, mergeText } from "./text-merge.js";

export interface CollaborationContent {
  readonly path: string;
  readonly bytes: Uint8Array;
  readonly hash: string;
}

export interface CollaborationReconcileInput {
  readonly path: string;
  readonly baseline: CollaborationBaselineEntry;
  readonly ancestorText: string | null;
  readonly local: CollaborationContent | null;
  readonly remote: CollaborationContent;
}

export type CollaborationReconcileDecision =
  | { readonly kind: "adopt_remote" }
  | { readonly kind: "keep_local" }
  | { readonly kind: "apply_remote" }
  | { readonly kind: "upload_local"; readonly content: string }
  | { readonly kind: "upload_merged"; readonly content: string }
  | { readonly kind: "persist_text_conflict"; readonly remoteText: string }
  | { readonly kind: "preserve_both" };

export function decideCollaborationReconciliation(
  input: CollaborationReconcileInput,
): CollaborationReconcileDecision {
  if (input.baseline.pending !== null) {
    return { kind: "keep_local" };
  }
  if (input.baseline.conflict !== null) {
    return { kind: "keep_local" };
  }
  if (input.local === null) {
    return { kind: "adopt_remote" };
  }
  if (input.local.hash === input.remote.hash) {
    return { kind: "adopt_remote" };
  }
  const localUnchanged = input.local.hash === input.baseline.localHash;
  const remoteUnchanged = input.remote.hash === input.baseline.serverHash;
  if (localUnchanged && remoteUnchanged) {
    return { kind: "adopt_remote" };
  }
  if (localUnchanged && !remoteUnchanged) {
    return { kind: "apply_remote" };
  }
  if (!localUnchanged && remoteUnchanged) {
    const decodedLocal = decodeMergeableText(input.path, input.local.bytes);
    if (decodedLocal === null) {
      return { kind: "preserve_both" };
    }
    return { kind: "upload_local", content: decodedLocal };
  }
  const decodedLocal = decodeMergeableText(input.path, input.local.bytes);
  const decodedRemote = decodeMergeableText(input.path, input.remote.bytes);
  if (decodedLocal === null || decodedRemote === null) {
    return { kind: "preserve_both" };
  }
  if (decodedLocal !== decodedRemote && input.ancestorText === null) {
    return { kind: "persist_text_conflict", remoteText: decodedRemote };
  }
  const merged = mergeText(input.ancestorText ?? "", decodedLocal, decodedRemote);
  if (merged.kind === "merged") {
    return { kind: "upload_merged", content: merged.content };
  }
  return { kind: "persist_text_conflict", remoteText: decodedRemote };
}
