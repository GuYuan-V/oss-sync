export type TransferNotice =
  | { readonly kind: "uploaded"; readonly path?: string; readonly count?: number }
  | { readonly kind: "downloaded"; readonly path?: string; readonly count?: number }
  | { readonly kind: "failed"; readonly path: string };

export function buildTransferNoticePlan(
  uploaded: readonly string[],
  downloaded: readonly string[],
  failed: readonly string[],
  aggregateThreshold = 5,
): readonly TransferNotice[] {
  const notices: TransferNotice[] = [];
  const aggregate = uploaded.length + downloaded.length + failed.length > aggregateThreshold;
  appendTransferNotices(notices, "uploaded", uploaded, aggregate);
  appendTransferNotices(notices, "downloaded", downloaded, aggregate);
  for (const path of failed) notices.push({ kind: "failed", path });
  return notices;
}

function appendTransferNotices(
  notices: TransferNotice[],
  kind: "uploaded" | "downloaded",
  paths: readonly string[],
  aggregate: boolean,
): void {
  if (paths.length === 0) return;
  if (aggregate) {
    notices.push({ kind, count: paths.length });
    return;
  }
  for (const path of paths) notices.push({ kind, path });
}
