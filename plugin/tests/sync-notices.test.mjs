import assert from "node:assert/strict";
import test from "node:test";
import { loadModule } from "./helpers/sync-engine-loader.mjs";

test("aggregates successful uploads and downloads above five files", async () => {
  const { module, cleanup } = await loadModule("src/sync-notices.ts");
  try {
    const plan = module.buildTransferNoticePlan(
      ["a.md", "b.md", "c.md", "d.md", "e.md", "f.md"],
      ["1.md", "2.md", "3.md", "4.md", "5.md", "6.md"],
      [],
    );

    assert.deepEqual(plan, [
      { kind: "uploaded", count: 6 },
      { kind: "downloaded", count: 6 },
    ]);
  } finally {
    await cleanup();
  }
});

test("keeps failed file paths individual while aggregating successful transfers", async () => {
  const { module, cleanup } = await loadModule("src/sync-notices.ts");
  try {
    const plan = module.buildTransferNoticePlan(
      ["a.md", "b.md", "c.md", "d.md", "e.md", "f.md"],
      [],
      ["broken.md", "conflict.md"],
    );

    assert.deepEqual(plan, [
      { kind: "uploaded", count: 6 },
      { kind: "failed", path: "broken.md" },
      { kind: "failed", path: "conflict.md" },
    ]);
  } finally {
    await cleanup();
  }
});

test("aggregates by direction when the combined successful batch exceeds five files", async () => {
  const { module, cleanup } = await loadModule("src/sync-notices.ts");
  try {
    const plan = module.buildTransferNoticePlan(
      ["a.md", "b.md", "c.md"],
      ["1.md", "2.md", "3.md"],
      [],
    );

    assert.deepEqual(plan, [
      { kind: "uploaded", count: 3 },
      { kind: "downloaded", count: 3 },
    ]);
  } finally {
    await cleanup();
  }
});

test("counts failures toward the batch threshold but still lists them individually", async () => {
  const { module, cleanup } = await loadModule("src/sync-notices.ts");
  try {
    const plan = module.buildTransferNoticePlan(
      ["a.md", "b.md", "c.md", "d.md"],
      [],
      ["broken.md", "conflict.md"],
    );

    assert.deepEqual(plan, [
      { kind: "uploaded", count: 4 },
      { kind: "failed", path: "broken.md" },
      { kind: "failed", path: "conflict.md" },
    ]);
  } finally {
    await cleanup();
  }
});
