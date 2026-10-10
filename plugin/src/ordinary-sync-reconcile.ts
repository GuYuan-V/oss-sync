import { decodeMergeableText, mergeText } from "./text-merge.js";

/** 三方合并输入；baseText 为 null 表示无可用祖先 */
export interface OrdinarySyncReconcileInput {
  readonly path: string;
  readonly baseText: string | null;
  readonly localBytes: Uint8Array;
  readonly remoteBytes: Uint8Array;
}

/** 合并决策；preserve_both 表示无法三方合并且需要保留两份文件 */
export type OrdinarySyncReconcileDecision =
  | { readonly kind: "merged"; readonly content: string; readonly bytes: Uint8Array }
  | { readonly kind: "text_conflict" }
  | { readonly kind: "preserve_both" };

/** 决定普通同步的合并策略；任一侧非 UTF-8 文本时保留双方 */
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
