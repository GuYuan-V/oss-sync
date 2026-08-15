import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { pathToFileURL } from "node:url";
import { build } from "esbuild";

async function loadDiagnostics() {
  const dir = await mkdtemp(join(tmpdir(), "oss-diagnostics-"));
  const outfile = join(dir, "diagnostics.mjs");
  await build({ entryPoints: ["src/diagnostics.ts"], outfile, bundle: true, platform: "node", format: "esm" });
  const module = await import(pathToFileURL(outfile).href);
  return { ...module, cleanup: () => rm(dir, { recursive: true, force: true }) };
}

test("keeps a bounded copy of safe diagnostic events and forwards them to its sink", async () => {
  const { Diagnostics, cleanup } = await loadDiagnostics();
  try {
    const forwarded = [];
    const diagnostics = new Diagnostics((event) => forwarded.push(event), 2);

    // Given: a collector with a two-event capacity.
    // When: three safe transport events are recorded and a snapshot is changed locally.
    diagnostics.record({ kind: "api", at: 1, method: "GET", status: 200, durationMs: 2 });
    diagnostics.record({ kind: "poll", at: 2, scope: "sync", changed: false, durationMs: 3 });
    diagnostics.record({ kind: "transfer", at: 3, scope: "upload", durationMs: 4, bytes: 5 });
    const snapshot = diagnostics.snapshot();
    snapshot.pop();

    // Then: the oldest event is evicted, consumers receive each event, and snapshots cannot mutate storage.
    assert.equal(diagnostics.snapshot().length, 2);
    assert.equal(diagnostics.snapshot()[0].kind, "poll");
    assert.equal(forwarded.length, 3);
  } finally {
    await cleanup();
  }
});

test("uses a closed event schema that cannot retain credentials or content", async () => {
  const { Diagnostics, cleanup } = await loadDiagnostics();
  try {
    const diagnostics = new Diagnostics();

    // Given: transport events from every supported category.
    // When: their serializable snapshot is inspected.
    diagnostics.record({ kind: "sse_state", at: 1, state: "connecting" });
    const poisoned = {
      kind: "sse_event",
      at: 2,
      event: "changed",
      connectionAgeMs: 3,
      token: "s3cr3t-token",
      content: "private note",
      path: "Notes/Private.md",
    };
    diagnostics.record(poisoned);
    poisoned.connectionAgeMs = 999;
    diagnostics.record({ kind: "poll", at: 3, scope: "collab", durationMs: 4, failed: true });
    diagnostics.record({ kind: "api", at: 4, method: "POST", durationMs: 5 });
    diagnostics.record({ kind: "transfer", at: 5, scope: "collab_upload", durationMs: 6, bytes: 7 });
    diagnostics.record({ kind: "collab_activity", at: 6, entries: 1, newestCreatedAt: "2026-08-12T00:00:00Z" });
    const serialized = JSON.stringify(diagnostics.snapshot());

    // Then: the collector drops unapproved fields and retained events cannot be changed by callers.
    assert.equal(diagnostics.snapshot()[1].connectionAgeMs, 3);
    assert.equal(Object.isFrozen(diagnostics.snapshot()[1]), true);
    for (const forbidden of ["token", "password", "authorization", "content", "body", "text", "path"]) {
      assert.equal(serialized.includes(forbidden), false, forbidden);
    }
    diagnostics.clear();
    assert.deepEqual(diagnostics.snapshot(), []);
  } finally {
    await cleanup();
  }
});
