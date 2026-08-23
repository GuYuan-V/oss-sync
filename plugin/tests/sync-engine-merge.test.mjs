import assert from "node:assert/strict";
import test from "node:test";
import { loadModule } from "./helpers/sync-engine-loader.mjs";

function enc(str) {
  return new TextEncoder().encode(str);
}

async function shaHex(bytes) {
  const d = await crypto.subtle.digest("SHA-256", bytes.slice());
  return Array.from(new Uint8Array(d))
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}

// baselineFromAcknowledgement tests

test("baselineFromAcknowledgement: mergeable .md stores text including empty string", async () => {
  const { module: mod, cleanup } = await loadModule("src/ordinary-sync-baseline.ts");
  try {
    const input = {
      path: "notes/note.md",
      bytes: enc("hello"),
      serverRevision: 5,
      serverHash: "h1",
      serverDeleted: false,
      localHash: "lh1",
      localMTime: 1000,
      localSize: 5,
    };
    const entry = mod.baselineFromAcknowledgement(input);
    assert.equal(entry.baseText, "hello");
    assert.equal(entry.serverRevision, 5);

    const empty = {
      path: "notes/empty.md",
      bytes: enc(""),
      serverRevision: 6,
      serverHash: "h2",
      serverDeleted: false,
      localHash: "lh2",
      localMTime: 1001,
      localSize: 0,
    };
    const e2 = mod.baselineFromAcknowledgement(empty);
    assert.equal(e2.baseText, "");
    assert.ok("baseText" in e2);
  } finally {
    await cleanup();
  }
});

test("baselineFromAcknowledgement: unsupported/binary, invalid UTF-8, oversize omit baseText", async () => {
  const { module: mod, cleanup } = await loadModule("src/ordinary-sync-baseline.ts");
  try {
    const unsupported = {
      path: "image.png",
      bytes: enc("hello"),
      serverRevision: 1,
      serverHash: "h",
      serverDeleted: false,
      localHash: "lh",
      localMTime: 1,
      localSize: 5,
    };
    const e1 = mod.baselineFromAcknowledgement(unsupported);
    assert.equal(e1.baseText, undefined);
    assert.equal("baseText" in e1, false);

    const invalid = {
      path: "note.md",
      bytes: new Uint8Array([0xff, 0xfe]),
      serverRevision: 1,
      serverHash: "h",
      serverDeleted: false,
      localHash: "lh",
      localMTime: 1,
      localSize: 2,
    };
    const e2 = mod.baselineFromAcknowledgement(invalid);
    assert.equal(e2.baseText, undefined);
    assert.equal("baseText" in e2, false);

    const big = new Uint8Array(2 * 1024 * 1024 + 1);
    big.fill(97);
    const oversize = {
      path: "note.md",
      bytes: big,
      serverRevision: 1,
      serverHash: "h",
      serverDeleted: false,
      localHash: "lh",
      localMTime: 1,
      localSize: big.length,
    };
    const e3 = mod.baselineFromAcknowledgement(oversize);
    assert.equal(e3.baseText, undefined);
    assert.equal("baseText" in e3, false);
  } finally {
    await cleanup();
  }
});

test("baselineFromAcknowledgement: binary acknowledgement has no baseText property", async () => {
  const { module: mod, cleanup } = await loadModule("src/ordinary-sync-baseline.ts");
  try {
    const binary = {
      path: "image.png",
      bytes: enc("newbinary"),
      serverRevision: 2,
      serverHash: "newh",
      serverDeleted: false,
      localHash: "newlh",
      localMTime: 2,
      localSize: 9,
    };
    const e = mod.baselineFromAcknowledgement(binary);
    assert.equal(e.baseText, undefined);
    assert.equal("baseText" in e, false);

    const invalid = {
      path: "note.md",
      bytes: new Uint8Array([0xff]),
      serverRevision: 2,
      serverHash: "newh",
      serverDeleted: false,
      localHash: "newlh",
      localMTime: 2,
      localSize: 1,
    };
    const e2 = mod.baselineFromAcknowledgement(invalid);
    assert.equal(e2.baseText, undefined);
    assert.equal("baseText" in e2, false);
  } finally {
    await cleanup();
  }
});

// decideOrdinarySyncReconciliation tests

test("decideOrdinarySyncReconciliation: independent text edits merge with content and bytes", async () => {
  const { module: mod, cleanup } = await loadModule("src/ordinary-sync-reconcile.ts");
  try {
    const base = "a\nb\nc\nd";
    const local = enc("A\nb\nc\nd");
    const remote = enc("a\nb\nc\nD");
    const res = mod.decideOrdinarySyncReconciliation({
      path: "note.md",
      baseText: base,
      localBytes: local,
      remoteBytes: remote,
    });
    assert.equal(res.kind, "merged");
    assert.equal(res.content, "A\nb\nc\nD");
    assert.deepEqual(res.bytes, enc("A\nb\nc\nD"));
  } finally {
    await cleanup();
  }
});

test("decideOrdinarySyncReconciliation: overlap conflicts as text_conflict", async () => {
  const { module: mod, cleanup } = await loadModule("src/ordinary-sync-reconcile.ts");
  try {
    const res = mod.decideOrdinarySyncReconciliation({
      path: "note.md",
      baseText: "a\nb\nc",
      localBytes: enc("a\nX\nc"),
      remoteBytes: enc("a\nY\nc"),
    });
    assert.equal(res.kind, "text_conflict");
  } finally {
    await cleanup();
  }
});

test("decideOrdinarySyncReconciliation: missing ancestor with different text is text_conflict, identical merges", async () => {
  const { module: mod, cleanup } = await loadModule("src/ordinary-sync-reconcile.ts");
  try {
    const conflict = mod.decideOrdinarySyncReconciliation({
      path: "note.md",
      baseText: null,
      localBytes: enc("hello local"),
      remoteBytes: enc("hello remote"),
    });
    assert.equal(conflict.kind, "text_conflict");

    const merged = mod.decideOrdinarySyncReconciliation({
      path: "note.md",
      baseText: null,
      localBytes: enc("same"),
      remoteBytes: enc("same"),
    });
    assert.equal(merged.kind, "merged");
    assert.equal(merged.content, "same");
    assert.deepEqual(merged.bytes, enc("same"));
  } finally {
    await cleanup();
  }
});

test("decideOrdinarySyncReconciliation: unsupported/invalid/oversize yields preserve_both", async () => {
  const { module: mod, cleanup } = await loadModule("src/ordinary-sync-reconcile.ts");
  try {
    const p1 = mod.decideOrdinarySyncReconciliation({
      path: "image.png",
      baseText: "old",
      localBytes: enc("a"),
      remoteBytes: enc("b"),
    });
    assert.equal(p1.kind, "preserve_both");

    const p2 = mod.decideOrdinarySyncReconciliation({
      path: "note.md",
      baseText: "old",
      localBytes: new Uint8Array([0xff]),
      remoteBytes: enc("valid"),
    });
    assert.equal(p2.kind, "preserve_both");

    const p3 = mod.decideOrdinarySyncReconciliation({
      path: "note.md",
      baseText: "old",
      localBytes: enc("valid"),
      remoteBytes: new Uint8Array([0xff, 0xfe]),
    });
    assert.equal(p3.kind, "preserve_both");

    const big = new Uint8Array(2 * 1024 * 1024 + 1);
    big.fill(97);
    const p4 = mod.decideOrdinarySyncReconciliation({
      path: "note.md",
      baseText: "old",
      localBytes: big,
      remoteBytes: enc("valid"),
    });
    assert.equal(p4.kind, "preserve_both");

    const p5 = mod.decideOrdinarySyncReconciliation({
      path: "note.md",
      baseText: "old",
      localBytes: enc("valid"),
      remoteBytes: big,
    });
    assert.equal(p5.kind, "preserve_both");
  } finally {
    await cleanup();
  }
});

test("decideOrdinarySyncReconciliation: no markers on text_conflict and merged", async () => {
  const { module: mod, cleanup } = await loadModule("src/ordinary-sync-reconcile.ts");
  try {
    const conflict = mod.decideOrdinarySyncReconciliation({
      path: "note.md",
      baseText: "a\nb\nc",
      localBytes: enc("a\nX\nc"),
      remoteBytes: enc("a\nY\nc"),
    });
    assert.equal(conflict.kind, "text_conflict");

    const merged = mod.decideOrdinarySyncReconciliation({
      path: "note.md",
      baseText: "a\nb\nc\nd",
      localBytes: enc("A\nb\nc\nd"),
      remoteBytes: enc("a\nb\nc\nD"),
    });
    assert.equal(merged.kind, "merged");
    assert.equal(merged.content.includes("<<<<<<<"), false);
    assert.equal(merged.content.includes("======="), false);
    assert.equal(merged.content.includes(">>>>>>>"), false);
    assert.deepEqual(merged.bytes, enc(merged.content));
  } finally {
    await cleanup();
  }
});

// OrdinarySyncFileAccess tests

function makeVault(initial = new Map()) {
  const files = new Map(initial);
  const folders = new Set();
  const vault = {
    getAbstractFileByPath(p) {
      if (files.has(p)) {
        const entry = files.get(p);
        return {
          __tfile: true,
          path: p,
          stat: { mtime: entry.mtime, size: entry.bytes.length, ctime: entry.mtime },
        };
      }
      if (folders.has(p)) return { path: p };
      return null;
    },
    async readBinary(file) {
      const entry = files.get(file.path);
      if (!entry) throw new Error("missing " + file.path);
      const copy = new Uint8Array(entry.bytes).buffer.slice(
        entry.bytes.byteOffset,
        entry.bytes.byteOffset + entry.bytes.byteLength,
      );
      const dup = new ArrayBuffer(copy.byteLength);
      new Uint8Array(dup).set(new Uint8Array(copy));
      return dup;
    },
    async createBinary(p, data) {
      const bytes = new Uint8Array(data.slice(0));
      files.set(p, { bytes, mtime: Date.now() });
      return {
        __tfile: true,
        path: p,
        stat: { mtime: Date.now(), size: bytes.length },
      };
    },
    async modifyBinary(file, data) {
      const bytes = new Uint8Array(data.slice(0));
      if (!files.has(file.path)) throw new Error("missing modify " + file.path);
      files.set(file.path, { bytes, mtime: Date.now() });
    },
    async createFolder(p) {
      folders.add(p);
    },
    _files: files,
    _folders: folders,
  };
  return vault;
}

test("OrdinarySyncFileAccess: exact bytes/hash and deterministic sibling naming via injected now", async () => {
  const { module: mod, cleanup } = await loadModule("src/ordinary-sync-file-access.ts");
  try {
    const vault = makeVault();
    const suppressed = [];
    const fixedNow = new Date("2024-01-02T03:04:05.006Z").getTime();
    const fa = new mod.OrdinarySyncFileAccess(vault, (p) => suppressed.push(p), () => fixedNow);
    const bytes = enc("hello world");
    const hash = await shaHex(bytes);
    await vault.createBinary(
      "notes/a.md",
      bytes.buffer.slice(bytes.byteOffset, bytes.byteOffset + bytes.byteLength),
    );
    vault._files.get("notes/a.md").mtime = 1000;
    const read = await fa.readExact("notes/a.md");
    assert.ok(read);
    assert.equal(read.hash, hash);
    assert.deepEqual(read.bytes, bytes);
    assert.equal(read.mtime, 1000);
    assert.equal(read.size, bytes.length);
    read.bytes[0] = 99;
    const read2 = await fa.readExact("notes/a.md");
    assert.notEqual(read2.bytes[0], 99);
  } finally {
    await cleanup();
  }
});

test("OrdinarySyncFileAccess: writeCanonicalIfUnchanged succeeds with snapshot, stale otherwise", async () => {
  const { module: mod, cleanup } = await loadModule("src/ordinary-sync-file-access.ts");
  try {
    const vault = makeVault();
    const suppressed = [];
    const fa = new mod.OrdinarySyncFileAccess(vault, (p) => suppressed.push(p));
    const localBytes = enc("local content");
    await vault.createBinary(
      "notes/canonical.md",
      localBytes.buffer.slice(localBytes.byteOffset, localBytes.byteOffset + localBytes.byteLength),
    );
    const localHash = await shaHex(localBytes);

    const newBytes = enc("new canonical");
    const ok = await fa.writeCanonicalIfUnchanged("notes/canonical.md", localHash, newBytes);
    assert.equal(ok.kind, "written");
    assert.ok(suppressed.includes("notes/canonical.md"));
    assert.deepEqual(ok.snapshot.bytes, newBytes);
    assert.equal(ok.snapshot.hash, await shaHex(newBytes));
    assert.equal(ok.snapshot.size, newBytes.length);
    const after = await fa.readExact("notes/canonical.md");
    assert.deepEqual(after.bytes, newBytes);
    assert.equal(after.hash, ok.snapshot.hash);

    const mutatedBytes = enc("mutated after");
    await vault.createBinary(
      "notes/other.md",
      mutatedBytes.buffer.slice(mutatedBytes.byteOffset, mutatedBytes.byteOffset + mutatedBytes.byteLength),
    );
    const wrongHash = await shaHex(enc("different"));
    const stale = await fa.writeCanonicalIfUnchanged("notes/other.md", wrongHash, enc("new"));
    assert.equal(stale.kind, "stale");
    assert.equal(stale.reason, "mutated");
    const still = await fa.readExact("notes/other.md");
    assert.deepEqual(still.bytes, mutatedBytes);

    const missing = await fa.writeCanonicalIfUnchanged("notes/missing.md", localHash, enc("new"));
    assert.equal(missing.kind, "stale");
    assert.equal(missing.reason, "missing");

    const vault2 = makeVault();
    const fa2 = new mod.OrdinarySyncFileAccess(vault2, () => {});
    await vault2.createBinary("a.md", enc("orig").buffer.slice(enc("orig").byteOffset, enc("orig").byteOffset + enc("orig").byteLength));
    const h2 = await shaHex(enc("orig"));
    const toWrite = enc("payload");
    const copyBefore = toWrite.slice();
    await fa2.writeCanonicalIfUnchanged("a.md", h2, toWrite);
    toWrite[0] = 99;
    const stored = await fa2.readExact("a.md");
    assert.deepEqual(stored.bytes, copyBefore);
  } finally {
    await cleanup();
  }
});

test("OrdinarySyncFileAccess: preserveSibling creates deterministic sibling with snapshot, collision handling", async () => {
  const { module: mod, cleanup } = await loadModule("src/ordinary-sync-file-access.ts");
  try {
    const fixedNow = new Date("2024-01-02T03:04:05.006Z").getTime();
    const vault = makeVault();
    const suppressed = [];
    const fa = new mod.OrdinarySyncFileAccess(vault, (p) => suppressed.push(p), () => fixedNow);
    const localBytes = enc("local sibling bytes");
    await vault.createBinary(
      "notes/note.md",
      localBytes.buffer.slice(localBytes.byteOffset, localBytes.byteOffset + localBytes.byteLength),
    );
    const localHash = await shaHex(localBytes);

    const res = await fa.preserveSiblingIfUnchanged("notes/note.md", localHash, localBytes);
    assert.equal(res.kind, "preserved");
    const expectedName = "notes/note_conflict_2024-01-02T03-04-05-006Z.md";
    assert.equal(res.siblingPath, expectedName);
    assert.deepEqual(res.snapshot.bytes, localBytes);
    assert.equal(res.snapshot.hash, await shaHex(localBytes));
    assert.ok(suppressed.includes(expectedName));
    const sibling = await fa.readExact(expectedName);
    assert.deepEqual(sibling.bytes, localBytes);

    const vault2 = makeVault();
    const fa2 = new mod.OrdinarySyncFileAccess(vault2, () => {}, () => fixedNow);
    const hiddenBytes = enc("hidden");
    await vault2.createBinary(".hidden", hiddenBytes.buffer.slice(hiddenBytes.byteOffset, hiddenBytes.byteOffset + hiddenBytes.byteLength));
    const hiddenHash = await shaHex(hiddenBytes);
    const res2 = await fa2.preserveSiblingIfUnchanged(".hidden", hiddenHash, hiddenBytes);
    assert.equal(res2.kind, "preserved");
    assert.equal(res2.siblingPath, ".hidden_conflict_2024-01-02T03-04-05-006Z");
    assert.deepEqual(res2.snapshot.bytes, hiddenBytes);

    const vault3 = makeVault();
    const fa3 = new mod.OrdinarySyncFileAccess(vault3, () => {}, () => fixedNow);
    await vault3.createBinary(
      "a/.obsidian",
      hiddenBytes.buffer.slice(hiddenBytes.byteOffset, hiddenBytes.byteOffset + hiddenBytes.byteLength),
    );
    const h3 = await shaHex(hiddenBytes);
    const res3 = await fa3.preserveSiblingIfUnchanged("a/.obsidian", h3, hiddenBytes);
    assert.equal(res3.kind, "preserved");
    assert.equal(res3.siblingPath, "a/.obsidian_conflict_2024-01-02T03-04-05-006Z");

    const vault4 = makeVault();
    const fa4 = new mod.OrdinarySyncFileAccess(vault4, () => {}, () => fixedNow);
    await vault4.createBinary(
      "notes/noext",
      localBytes.buffer.slice(localBytes.byteOffset, localBytes.byteOffset + localBytes.byteLength),
    );
    const h4 = await shaHex(localBytes);
    const res4 = await fa4.preserveSiblingIfUnchanged("notes/noext", h4, localBytes);
    assert.equal(res4.kind, "preserved");
    assert.equal(res4.siblingPath, "notes/noext_conflict_2024-01-02T03-04-05-006Z");

    const vault5 = makeVault();
    const fa5 = new mod.OrdinarySyncFileAccess(vault5, () => {}, () => fixedNow);
    await vault5.createBinary(
      "notes/c.md",
      localBytes.buffer.slice(localBytes.byteOffset, localBytes.byteOffset + localBytes.byteLength),
    );
    const h5 = await shaHex(localBytes);
    const first = await fa5.preserveSiblingIfUnchanged("notes/c.md", h5, localBytes);
    assert.equal(first.kind, "preserved");
    const second = await fa5.preserveSiblingIfUnchanged("notes/c.md", h5, localBytes);
    assert.equal(second.kind, "collision");
    assert.equal(second.siblingPath, first.siblingPath);
    const siblingAfter = await fa5.readExact(first.siblingPath);
    assert.deepEqual(siblingAfter.bytes, localBytes);

    const vault6 = makeVault();
    const fa6 = new mod.OrdinarySyncFileAccess(vault6, () => {});
    await vault6.createBinary(
      "notes/d.md",
      enc("original").buffer.slice(enc("original").byteOffset, enc("original").byteOffset + enc("original").byteLength),
    );
    const wrongHash = await shaHex(enc("different"));
    const stale = await fa6.preserveSiblingIfUnchanged("notes/d.md", wrongHash, enc("original"));
    assert.equal(stale.kind, "stale");
    const canon = await fa6.readExact("notes/d.md");
    assert.deepEqual(canon.bytes, enc("original"));

    const vault7 = makeVault();
    const fa7 = new mod.OrdinarySyncFileAccess(vault7, () => {});
    const miss = await fa7.preserveSiblingIfUnchanged("notes/miss.md", "anyhash", enc("x"));
    assert.equal(miss.kind, "stale");

    const canon5 = await fa5.readExact("notes/c.md");
    assert.deepEqual(canon5.bytes, localBytes);

    const vault8 = makeVault();
    const fa8 = new mod.OrdinarySyncFileAccess(vault8, () => {}, () => fixedNow);
    const binary = new Uint8Array([0xff, 0x00, 0xfe, 0xfd]);
    await vault8.createBinary("assets/b.bin", binary.slice().buffer.slice(binary.byteOffset, binary.byteOffset + binary.byteLength));
    const binHash = await shaHex(binary);
    const res8 = await fa8.preserveSiblingIfUnchanged("assets/b.bin", binHash, binary);
    assert.equal(res8.kind, "preserved");
    assert.deepEqual(res8.snapshot.bytes, binary);
    const binSibling = await fa8.readExact(res8.siblingPath);
    assert.deepEqual(binSibling.bytes, binary);
  } finally {
    await cleanup();
  }
});

test("OrdinarySyncFileAccess: failure leaves canonical byte-identical and sibling not reported", async () => {
  const { module: mod, cleanup } = await loadModule("src/ordinary-sync-file-access.ts");
  try {
    const vault = makeVault();
    const suppressed = [];
    const fixedNow = new Date("2024-01-02T03:04:05.006Z").getTime();
    const fa = new mod.OrdinarySyncFileAccess(vault, (p) => suppressed.push(p), () => fixedNow);
    const orig = enc("orig canonical");
    await vault.createBinary("notes/e.md", orig.buffer.slice(orig.byteOffset, orig.byteOffset + orig.byteLength));
    const wrong = await shaHex(enc("other"));
    const r = await fa.preserveSiblingIfUnchanged("notes/e.md", wrong, orig);
    assert.equal(r.kind, "stale");
    const after = await fa.readExact("notes/e.md");
    assert.deepEqual(after.bytes, orig);

    const beforeWrite = await fa.readExact("notes/e.md");
    const staleWrite = await fa.writeCanonicalIfUnchanged("notes/e.md", wrong, enc("new"));
    assert.equal(staleWrite.kind, "stale");
    const afterWrite = await fa.readExact("notes/e.md");
    assert.deepEqual(afterWrite.bytes, beforeWrite.bytes);

    suppressed.length = 0;
    const h = await shaHex(orig);
    const ok = await fa.writeCanonicalIfUnchanged("notes/e.md", h, enc("newer"));
    assert.equal(ok.kind, "written");
    assert.ok(suppressed.length > 0);

    // createBinary failure fixture: sibling creation throws, canonical must remain byte-identical
    const vaultFail = makeVault();
    await vaultFail.createBinary(
      "notes/f.md",
      enc("canonical").buffer.slice(enc("canonical").byteOffset, enc("canonical").byteOffset + enc("canonical").byteLength),
    );
    const canonBefore = await (async () => {
      const f = new mod.OrdinarySyncFileAccess(vaultFail, () => {}, () => fixedNow);
      const r = await f.readExact("notes/f.md");
      return r.bytes.slice();
    })();
    const hashFail = await shaHex(enc("canonical"));
    const originalCreateBinary = vaultFail.createBinary;
    vaultFail.createBinary = async () => {
      throw new Error("createBinary injected failure");
    };
    const faFail = new mod.OrdinarySyncFileAccess(vaultFail, () => {}, () => fixedNow);
    await assert.rejects(() => faFail.preserveSiblingIfUnchanged("notes/f.md", hashFail, enc("canonical")), /createBinary injected failure/);
    // canonical must remain byte-identical, no sibling created
    const faCheck = new mod.OrdinarySyncFileAccess(vaultFail, () => {}, () => fixedNow);
    // restore for read
    vaultFail.createBinary = originalCreateBinary;
    const canonAfter = await faCheck.readExact("notes/f.md");
    assert.deepEqual(canonAfter.bytes, canonBefore);
    assert.equal(canonAfter.hash, await shaHex(canonBefore));
    const siblingExists = vaultFail.getAbstractFileByPath("notes/f_conflict_2024-01-02T03-04-05-006Z.md");
    assert.equal(siblingExists, null);
  } finally {
    await cleanup();
  }
});
