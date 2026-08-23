import { decodeMergeableText, mergeText } from "./text-merge.js";

export interface OrdinarySyncReconcileInput {
  readonly path: string;
  readonly baseText: string | null;
  readonly localBytes: Uint8Array;
  readonly remoteBytes: Uint8Array;
}

export type OrdinarySyncReconcileDecision =
  | { readonly kind: "merged"; readonly content: string; readonly bytes: Uint8Array }
  | { readonly kind: "text_conflict" }
  | { readonly kind: "preserve_both" };

export function decideOrdinarySyncReconciliation(
  input: OrdinarySyncReconcileInput,
): OrdinarySyncReconcileDecision {
  const localText = decodeMergeableText(input.path, input.localBytes);
  const remoteText = decodeMergeableText(input.path, input.remoteBytes);
  if (localText === null || remoteText === null) {
    return { kind: "preserve_both" };
  }
  if (input.baseText === null) {
    if (localText === remoteText) {
      return {
        kind: "merged",
        content: localText,
        bytes: new TextEncoder().encode(localText),
      };
    }
    return { kind: "text_conflict" };
  }
  const result = mergeText(input.baseText, localText, remoteText);
  if (result.kind === "merged") {
    return {
      kind: "merged",
      content: result.content,
      bytes: new TextEncoder().encode(result.content),
    };
  }
  return { kind: "text_conflict" };
}
