import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { pathToFileURL } from "node:url";
import { build } from "esbuild";

async function loadReconcile() {
  const dir = await mkdtemp(join(tmpdir(), "oss-reconcile-"));
  const outfile = join(dir, "reconcile.mjs");
  await build({
    entryPoints: ["src/collaboration-file-reconcile.ts"],
    outfile,
    bundle: true,
    platform: "node",
    format: "esm",
  });
  const mod = await import(pathToFileURL(outfile).href);
  return { mod, cleanup: () => rm(dir, { recursive: true, force: true }) };
}

const HASH_BASE = "a94a8fe5ccb19ba61c4c0873d391e987982fbbd3aaaaaaaaaaaaaaaaaaaaaaaa";
const HASH_LOCAL = "b94a8fe5ccb19ba61c4c0873d391e987982fbbd3bbbbbbbbbbbbbbbbbbbbbbbb";
const HASH_REMOTE = "c94a8fe5ccb19ba61c4c0873d391e987982fbbd3cccccccccccccccccccccccc";
const HASH_REMOTE2 = "d94a8fe5ccb19ba61c4c0873d391e987982fbbd3dddddddddddddddddddddddd";
const enc = (s) => new TextEncoder().encode(s);
const bytes = (s) => enc(s);

function baseEntry(overrides = {}) {
  return {
    vaultId: "vault-1",
    fileId: 42,
    localPath: "notes/doc.md",
    serverRevision: 7,
    serverHash: HASH_BASE,
    localHash: HASH_BASE,
    baseText: "a\nb\nc\nd",
    pending: null,
    conflict: null,
    ...overrides,
  };
}
function content(path, text, hash) {
  return { path, bytes: bytes(text), hash };
}
function reconcileInput({ baseline = baseEntry(), ancestorText = baseline.baseText, local, remote, path = "notes/doc.md" }) {
  return { path, baseline, ancestorText, local, remote };
}
function assertExhaustive(decision) {
  switch (decision.kind) {
    case "adopt_remote": return;
    case "keep_local": return;
    case "apply_remote": return;
    case "upload_local": assert.equal(typeof decision.content, "string"); return;
    case "upload_merged": assert.equal(typeof decision.content, "string"); return;
    case "persist_text_conflict": assert.equal(typeof decision.remoteText, "string"); return;
    case "preserve_both": return;
    default: throw new Error(`unhandled ${decision.kind}`);
  }
}

test("adopts remote when no local file", async () => {
  const { mod, cleanup } = await loadReconcile();
  try {
    const input = reconcileInput({ local: null, remote: content("notes/doc.md", "hello", HASH_REMOTE) });
    const decision = mod.decideCollaborationReconciliation(input);
    assert.equal(decision.kind, "adopt_remote");
    assertExhaustive(decision);
  } finally { await cleanup(); }
});

test("adopts remote when hashes equal", async () => {
  const { mod, cleanup } = await loadReconcile();
  try {
    const input = reconcileInput({ local: content("notes/doc.md", "same", HASH_REMOTE), remote: content("notes/doc.md", "same", HASH_REMOTE) });
    const decision = mod.decideCollaborationReconciliation(input);
    assert.equal(decision.kind, "adopt_remote");
    assertExhaustive(decision);
  } finally { await cleanup(); }
});

test("keeps local when pending operation exists", async () => {
  const { mod, cleanup } = await loadReconcile();
  try {
    const baseline = baseEntry({ pending: { id: "op-1", createdAt: Date.now() } });
    const input = reconcileInput({ baseline, local: content("notes/doc.md", "local", HASH_LOCAL), remote: content("notes/doc.md", "remote", HASH_REMOTE) });
    const decision = mod.decideCollaborationReconciliation(input);
    assert.equal(decision.kind, "keep_local");
    assertExhaustive(decision);
  } finally { await cleanup(); }
});

test("keeps local when persisted conflict exists", async () => {
  const { mod, cleanup } = await loadReconcile();
  try {
    const baseline = baseEntry({ conflict: { remoteRevision: 9, remoteHash: HASH_REMOTE, remoteText: "remote", detectedAt: Date.now() } });
    const input = reconcileInput({ baseline, local: content("notes/doc.md", "local", HASH_LOCAL), remote: content("notes/doc.md", "remote2", HASH_REMOTE2) });
    const decision = mod.decideCollaborationReconciliation(input);
    assert.equal(decision.kind, "keep_local");
    assertExhaustive(decision);
  } finally { await cleanup(); }
});

test("applies remote when only remote changed", async () => {
  const { mod, cleanup } = await loadReconcile();
  try {
    const baseline = baseEntry({ serverHash: HASH_BASE, localHash: HASH_BASE });
    const input = reconcileInput({ baseline, local: content("notes/doc.md", "a", HASH_BASE), remote: content("notes/doc.md", "b", HASH_REMOTE) });
    const decision = mod.decideCollaborationReconciliation(input);
    assert.equal(decision.kind, "apply_remote");
    assertExhaustive(decision);
  } finally { await cleanup(); }
});

test("uploads local when only local changed and mergeable", async () => {
  const { mod, cleanup } = await loadReconcile();
  try {
    const baseline = baseEntry({ serverHash: HASH_BASE, localHash: HASH_BASE, baseText: "x" });
    const input = reconcileInput({ baseline, local: content("notes/doc.md", "local edit", HASH_LOCAL), remote: content("notes/doc.md", "x", HASH_BASE) });
    const decision = mod.decideCollaborationReconciliation(input);
    assert.equal(decision.kind, "upload_local");
    assert.equal(decision.content, "local edit");
    assertExhaustive(decision);
  } finally { await cleanup(); }
});

test("uploads merged when concurrent independent edits", async () => {
  const { mod, cleanup } = await loadReconcile();
  try {
    const baseline = baseEntry({ serverHash: HASH_BASE, localHash: HASH_BASE, baseText: "a\nb\nc\nd" });
    const input = reconcileInput({ baseline, local: content("notes/doc.md", "A\nb\nc\nd", HASH_LOCAL), remote: content("notes/doc.md", "a\nb\nc\nD", HASH_REMOTE) });
    const decision = mod.decideCollaborationReconciliation(input);
    assert.equal(decision.kind, "upload_merged");
    assert.equal(decision.content, "A\nb\nc\nD");
    assertExhaustive(decision);
  } finally { await cleanup(); }
});

test("persists conflict on overlapping text edits", async () => {
  const { mod, cleanup } = await loadReconcile();
  try {
    const baseline = baseEntry({ serverHash: HASH_BASE, localHash: HASH_BASE, baseText: "a\nb\nc" });
    const input = reconcileInput({ baseline, local: content("notes/doc.md", "a\nX\nc", HASH_LOCAL), remote: content("notes/doc.md", "a\nY\nc", HASH_REMOTE) });
    const decision = mod.decideCollaborationReconciliation(input);
    assert.equal(decision.kind, "persist_text_conflict");
    assert.equal(decision.remoteText, "a\nY\nc");
    assertExhaustive(decision);
  } finally { await cleanup(); }
});

test("preserves both on unsupported extension concurrent change", async () => {
  const { mod, cleanup } = await loadReconcile();
  try {
    const baseline = baseEntry({ serverHash: HASH_BASE, localHash: HASH_BASE, baseText: "a\nb" });
    const input = reconcileInput({ path: "assets/image.png", baseline, local: { path: "assets/image.png", bytes: bytes("local"), hash: HASH_LOCAL }, remote: { path: "assets/image.png", bytes: bytes("remote"), hash: HASH_REMOTE } });
    const decision = mod.decideCollaborationReconciliation(input);
    assert.equal(decision.kind, "preserve_both");
    assertExhaustive(decision);
  } finally { await cleanup(); }
});

test("preserves both on invalid UTF-8 concurrent change", async () => {
  const { mod, cleanup } = await loadReconcile();
  try {
    const baseline = baseEntry({ serverHash: HASH_BASE, localHash: HASH_BASE, baseText: "hello" });
    const bad = new Uint8Array([0xff, 0xfe, 0xfd]);
    const input = reconcileInput({ baseline, local: content("notes/doc.md", "local hi", HASH_LOCAL), remote: { path: "notes/doc.md", bytes: bad, hash: HASH_REMOTE } });
    const decision = mod.decideCollaborationReconciliation(input);
    assert.equal(decision.kind, "preserve_both");
    assertExhaustive(decision);
  } finally { await cleanup(); }
});

test("preserves both on over-limit concurrent change", async () => {
  const { mod, cleanup } = await loadReconcile();
  try {
    const baseline = baseEntry({ serverHash: HASH_BASE, localHash: HASH_BASE, baseText: "base" });
    const big = new Uint8Array(2 * 1024 * 1024 + 1);
    big.fill(97);
    const input = reconcileInput({ baseline, local: { path: "notes/doc.md", bytes: big, hash: HASH_LOCAL }, remote: { path: "notes/doc.md", bytes: big, hash: HASH_REMOTE } });
    const decision = mod.decideCollaborationReconciliation(input);
    assert.equal(decision.kind, "preserve_both");
    assertExhaustive(decision);
  } finally { await cleanup(); }
});

test("persists conflict when no trustworthy ancestor", async () => {
  const { mod, cleanup } = await loadReconcile();
  try {
    const baseline = baseEntry({ serverHash: HASH_BASE, localHash: HASH_BASE, baseText: "" });
    const input = reconcileInput({ baseline, ancestorText: null, local: content("notes/doc.md", "local only", HASH_LOCAL), remote: content("notes/doc.md", "remote only", HASH_REMOTE) });
    const decision = mod.decideCollaborationReconciliation(input);
    assert.equal(decision.kind, "persist_text_conflict");
    assert.equal(decision.remoteText, "remote only");
    assertExhaustive(decision);
  } finally { await cleanup(); }
});
