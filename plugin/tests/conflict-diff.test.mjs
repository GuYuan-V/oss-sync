import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { pathToFileURL } from "node:url";
import { build } from "esbuild";

// Bundles the pure diff module so the test drives the real exported function.
async function loadConflictDiff() {
  const dir = await mkdtemp(join(tmpdir(), "oss-conflict-diff-"));
  const outfile = join(dir, "conflict-diff.mjs");
  await build({
    entryPoints: ["src/conflict-diff.ts"],
    outfile,
    bundle: true,
    platform: "node",
    format: "esm",
  });
  const module = await import(pathToFileURL(outfile).href);
  return {
    ...module,
    cleanup: () => rm(dir, { recursive: true, force: true }),
  };
}

// Row helpers — change rows carry raw `text` (no "-"/"+" markers; the renderer
// adds those later), omitted rows carry a `count`.
const ctx = (text) => ({ kind: "context", text });
const rem = (text) => ({ kind: "removed", text });
const add = (text) => ({ kind: "added", text });
const omit = (count) => ({ kind: "omitted", count });
const lines = (...ls) => ls.join("\n");

// Shared runner: each fixture is Given (local/remote content), When
// (buildConflictDiff is called), Then (the exact row sequence is asserted).
async function runCases(cases) {
  const { buildConflictDiff, cleanup } = await loadConflictDiff();
  try {
    for (const c of cases) {
      const actual = buildConflictDiff(c.local, c.remote);
      assert.deepEqual(actual, c.expected, c.name);
    }
  } finally {
    await cleanup();
  }
}

test("identical and empty content produce no rows", async () => {
  await runCases([
    { name: "both empty", local: "", remote: "", expected: [] },
    { name: "identical content", local: "a\nb", remote: "a\nb", expected: [] },
    { name: "CRLF identical to LF", local: "a\r\nb", remote: "a\nb", expected: [] },
    { name: "trailing newline ignored", local: "a\nb\n", remote: "a\nb", expected: [] },
    { name: "CRLF plus trailing newline ignored", local: "a\r\nb\r\n", remote: "a\nb", expected: [] },
  ]);
});

test("replacement emits removed rows before added rows with raw text", async () => {
  await runCases([
    {
      name: "single-line replacement",
      local: lines("a", "old", "c"),
      remote: lines("a", "new", "c"),
      expected: [ctx("a"), rem("old"), add("new"), ctx("c")],
    },
    {
      name: "pure deletion keeps a removed row",
      local: lines("a", "b", "c"),
      remote: lines("a", "c"),
      expected: [ctx("a"), rem("b"), ctx("c")],
    },
    {
      name: "pure insertion keeps an added row",
      local: lines("a", "c"),
      remote: lines("a", "b", "c"),
      expected: [ctx("a"), add("b"), ctx("c")],
    },
  ]);
});

test("retains exactly 2 context lines around a change", async () => {
  await runCases([
    {
      name: "two context lines each side are kept verbatim",
      local: lines("1", "2", "X", "5", "6"),
      remote: lines("1", "2", "Y", "5", "6"),
      expected: [ctx("1"), ctx("2"), rem("X"), add("Y"), ctx("5"), ctx("6")],
    },
    {
      name: "extra context beyond 2 is compressed on both sides",
      local: lines("1", "2", "3", "X", "5", "6", "7"),
      remote: lines("1", "2", "3", "Y", "5", "6", "7"),
      expected: [
        omit(1),
        ctx("2"),
        ctx("3"),
        rem("X"),
        add("Y"),
        ctx("5"),
        ctx("6"),
        omit(1),
      ],
    },
  ]);
});

test("compresses leading context into omitted(8) plus the 2 lines before the change", async () => {
  await runCases([
    {
      name: "10 unchanged leading lines",
      local: lines("0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "old"),
      remote: lines("0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "new"),
      expected: [omit(8), ctx("8"), ctx("9"), rem("old"), add("new")],
    },
  ]);
});

test("compresses trailing context into the 2 lines after the change plus omitted(8)", async () => {
  await runCases([
    {
      name: "10 unchanged trailing lines",
      local: lines("old", "0", "1", "2", "3", "4", "5", "6", "7", "8", "9"),
      remote: lines("new", "0", "1", "2", "3", "4", "5", "6", "7", "8", "9"),
      expected: [rem("old"), add("new"), ctx("0"), ctx("1"), omit(8)],
    },
  ]);
});

test("compresses a 10-line unchanged middle run to 2 + omitted(6) + 2", async () => {
  await runCases([
    {
      name: "10 unchanged lines between two changes",
      local: lines("a1", "0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "b1"),
      remote: lines("a2", "0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "b2"),
      expected: [
        rem("a1"),
        add("a2"),
        ctx("0"),
        ctx("1"),
        omit(6),
        ctx("8"),
        ctx("9"),
        rem("b1"),
        add("b2"),
      ],
    },
  ]);
});

test("middle runs of 4 stay whole while runs of 5 omit exactly 1 line", async () => {
  await runCases([
    {
      name: "middle run of 4 is fully retained",
      local: lines("a1", "0", "1", "2", "3", "b1"),
      remote: lines("a2", "0", "1", "2", "3", "b2"),
      expected: [
        rem("a1"),
        add("a2"),
        ctx("0"),
        ctx("1"),
        ctx("2"),
        ctx("3"),
        rem("b1"),
        add("b2"),
      ],
    },
    {
      name: "middle run of 5 omits exactly 1 line",
      local: lines("a1", "0", "1", "2", "3", "4", "b1"),
      remote: lines("a2", "0", "1", "2", "3", "4", "b2"),
      expected: [
        rem("a1"),
        add("a2"),
        ctx("0"),
        ctx("1"),
        omit(1),
        ctx("3"),
        ctx("4"),
        rem("b1"),
        add("b2"),
      ],
    },
  ]);
});

test("covers file boundaries, adjacent blocks, blank lines, HTML-like text, and normalization", async () => {
  await runCases([
    {
      name: "change at file start",
      local: lines("X", "a", "b"),
      remote: lines("Y", "a", "b"),
      expected: [rem("X"), add("Y"), ctx("a"), ctx("b")],
    },
    {
      name: "change at file end",
      local: lines("a", "b", "X"),
      remote: lines("a", "b", "Y"),
      expected: [ctx("a"), ctx("b"), rem("X"), add("Y")],
    },
    {
      name: "full-file replacement keeps removed rows before added rows",
      local: lines("a", "b"),
      remote: lines("x", "y"),
      expected: [rem("a"), rem("b"), add("x"), add("y")],
    },
    {
      name: "insertion abutting a deletion forms one block",
      local: lines("a", "b", "c"),
      remote: lines("a", "x", "y", "c"),
      expected: [ctx("a"), rem("b"), add("x"), add("y"), ctx("c")],
    },
    {
      name: "blank line as unchanged context",
      local: lines("a", "", "b"),
      remote: lines("a", "", "c"),
      expected: [ctx("a"), ctx(""), rem("b"), add("c")],
    },
    {
      name: "blank line as changed content",
      local: lines("a", "", "b"),
      remote: lines("a", "x", "b"),
      expected: [ctx("a"), rem(""), add("x"), ctx("b")],
    },
    {
      name: "HTML-like content stays as raw text",
      local: lines("<div>", '<span class="x">hi</span>', "</div>"),
      remote: lines("<div>", "<p>hi</p>", "</div>"),
      expected: [ctx("<div>"), rem('<span class="x">hi</span>'), add("<p>hi</p>"), ctx("</div>")],
    },
    {
      name: "CRLF input is normalized before diffing",
      local: "a\r\nb\r\nc",
      remote: "a\r\nX\r\nc",
      expected: [ctx("a"), rem("b"), add("X"), ctx("c")],
    },
    {
      name: "trailing newline on both sides does not change the diff",
      local: "a\nb\n",
      remote: "a\nc\n",
      expected: [ctx("a"), rem("b"), add("c")],
    },
  ]);
});
