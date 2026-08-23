import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { pathToFileURL } from "node:url";
import { build } from "esbuild";

async function loadTextMerge() {
  const dir = await mkdtemp(join(tmpdir(), "oss-text-merge-"));
  const outfile = join(dir, "text-merge.mjs");
  await build({
    entryPoints: ["src/text-merge.ts"],
    outfile,
    bundle: true,
    platform: "node",
    format: "esm",
  });
  const mod = await import(pathToFileURL(outfile).href);
  return { mod, cleanup: () => rm(dir, { recursive: true, force: true }) };
}

const enc = (s) => new TextEncoder().encode(s);

test("MAX_MERGE_BYTES is 2 MiB", async () => {
  const { mod, cleanup } = await loadTextMerge();
  try {
    assert.equal(mod.MAX_MERGE_BYTES, 2 * 1024 * 1024);
  } finally {
    await cleanup();
  }
});

test("mergeText merges edits on different lines", async () => {
  const { mod, cleanup } = await loadTextMerge();
  try {
    const base = "a\nb\nc\nd";
    const local = "A\nb\nc\nd";
    const remote = "a\nb\nc\nD";
    const res = mod.mergeText(base, local, remote);
    assert.equal(res.kind, "merged");
    assert.equal(res.content, "A\nb\nc\nD");
  } finally {
    await cleanup();
  }
});

test("mergeText merges inserts at different locations", async () => {
  const { mod, cleanup } = await loadTextMerge();
  try {
    const base = "a\nb";
    const local = "a\nx\nb";
    const remote = "a\nb\ny";
    const res = mod.mergeText(base, local, remote);
    assert.equal(res.kind, "merged");
    assert.equal(res.content, "a\nx\nb\ny");
  } finally {
    await cleanup();
  }
});

test("mergeText merges identical edits", async () => {
  const { mod, cleanup } = await loadTextMerge();
  try {
    const base = "a\nb\nc";
    const local = "a\nX\nc";
    const remote = "a\nX\nc";
    const res = mod.mergeText(base, local, remote);
    assert.equal(res.kind, "merged");
    assert.equal(res.content, "a\nX\nc");
  } finally {
    await cleanup();
  }
});

test("mergeText conflicts on same-line overlap", async () => {
  const { mod, cleanup } = await loadTextMerge();
  try {
    const base = "a\nb\nc";
    const local = "a\nX\nc";
    const remote = "a\nY\nc";
    const res = mod.mergeText(base, local, remote);
    assert.equal(res.kind, "conflict");
  } finally {
    await cleanup();
  }
});

test("mergeText conflicts on same-position inserts with different content", async () => {
  const { mod, cleanup } = await loadTextMerge();
  try {
    const base = "a\nb";
    const local = "a\nX\nb";
    const remote = "a\nY\nb";
    const res = mod.mergeText(base, local, remote);
    assert.equal(res.kind, "conflict");
  } finally {
    await cleanup();
  }
});

test("mergeText normalizes CRLF and LF", async () => {
  const { mod, cleanup } = await loadTextMerge();
  try {
    const base = "a\r\nb\r\nc";
    const local = "a\r\nX\r\nc";
    const remote = "a\r\nb\r\nY";
    const res = mod.mergeText(base, local, remote);
    assert.equal(res.kind, "merged");
    assert.equal(res.content, "a\nX\nY");
  } finally {
    await cleanup();
  }
});

test("mergeText keeps empty text empty", async () => {
  const { mod, cleanup } = await loadTextMerge();
  try {
    const res = mod.mergeText("", "", "");
    assert.equal(res.kind, "merged");
    assert.equal(res.content, "");
  } finally {
    await cleanup();
  }
});

test("mergeText preserves terminal newline when both sides add it", async () => {
  const { mod, cleanup } = await loadTextMerge();
  try {
    const res = mod.mergeText("a\nb", "a\nb\n", "a\nb\n");
    assert.equal(res.kind, "merged");
    assert.equal(res.content, "a\nb\n");
  } finally {
    await cleanup();
  }
});

test("mergeText removes terminal newline when both sides remove it", async () => {
  const { mod, cleanup } = await loadTextMerge();
  try {
    const res = mod.mergeText("a\nb\n", "a\nb", "a\nb");
    assert.equal(res.kind, "merged");
    assert.equal(res.content, "a\nb");
  } finally {
    await cleanup();
  }
});

test("mergeText handles constructor and __proto__ lines without pollution", async () => {
  const { mod, cleanup } = await loadTextMerge();
  try {
    const base = "a\nb\nc\nd";
    const local = "constructor\nb\nc\nd";
    const remote = "a\nb\n__proto__\nd";
    const res = mod.mergeText(base, local, remote);
    assert.equal(res.kind, "merged");
    assert.equal(res.content, "constructor\nb\n__proto__\nd");
    assert.equal(Object.prototype.hasOwnProperty.call({}, "polluted"), false);
  } finally {
    await cleanup();
  }
});

test("decodeMergeableText allowlists UTF-8 under limit", async () => {
  const { mod, cleanup } = await loadTextMerge();
  try {
    const bytes = enc("hello");
    for (const ext of [".md", ".markdown", ".txt", ".json", ".yaml", ".yml", ".toml", ".css", ".js", ".ts", ".canvas"]) {
      const got = mod.decodeMergeableText(`notes${ext}`, bytes);
      assert.equal(got, "hello");
    }
  } finally {
    await cleanup();
  }
});

test("decodeMergeableText rejects unknown extension", async () => {
  const { mod, cleanup } = await loadTextMerge();
  try {
    const got = mod.decodeMergeableText("image.png", enc("hello"));
    assert.equal(got, null);
  } finally {
    await cleanup();
  }
});

test("decodeMergeableText rejects invalid UTF-8", async () => {
  const { mod, cleanup } = await loadTextMerge();
  try {
    const bad = new Uint8Array([0xff, 0xfe, 0xfd]);
    const got = mod.decodeMergeableText("notes.md", bad);
    assert.equal(got, null);
  } finally {
    await cleanup();
  }
});

test("decodeMergeableText rejects over-limit input", async () => {
  const { mod, cleanup } = await loadTextMerge();
  try {
    const big = new Uint8Array(mod.MAX_MERGE_BYTES + 1);
    big.fill(97);
    const got = mod.decodeMergeableText("notes.md", big);
    assert.equal(got, null);
  } finally {
    await cleanup();
  }
});
