import assert from "node:assert/strict";
import test from "node:test";
import { loadModule } from "./helpers/sync-engine-loader.mjs";

function enc(s) { return new TextEncoder().encode(s); }
async function shaHex(bytes) {
  const d = await crypto.subtle.digest("SHA-256", bytes.slice());
  return Array.from(new Uint8Array(d)).map(b=>b.toString(16).padStart(2,"0")).join("");
}
function makeVault(initial = new Map()) {
  const files = new Map(initial);
  const folders = new Set();
  const vault = {
    getAbstractFileByPath(p){
      if(files.has(p)) {
        const e=files.get(p);
        return { __tfile:true, path:p, stat:{mtime:e.mtime,size:e.bytes.length,ctime:e.mtime} };
      }
      if(folders.has(p)) return { path:p };
      return null;
    },
    async readBinary(file){
      const e=files.get(file.path);
      if(!e) throw new Error("missing "+file.path);
      const buf = new Uint8Array(e.bytes).buffer.slice(e.bytes.byteOffset,e.bytes.byteOffset+e.bytes.byteLength);
      const dup = new ArrayBuffer(buf.byteLength);
      new Uint8Array(dup).set(new Uint8Array(buf));
      return dup;
    },
    async createBinary(p,data){
      const bytes=new Uint8Array(data.slice(0));
      const mtime=Date.now();
      files.set(p,{bytes,mtime});
      return { __tfile:true, path:p, stat:{mtime,size:bytes.length} };
    },
    async modifyBinary(file,data){
      const bytes=new Uint8Array(data.slice(0));
      const mtime=Date.now();
      if(!files.has(file.path)) throw new Error("missing modify");
      files.set(file.path,{bytes,mtime});
    },
    async createFolder(p){ folders.add(p); },
    async delete(file){ files.delete(file.path); },
    adapter:{ exists:async()=>false, read:async()=>"", write:async()=>{} },
    _files:files, _folders:folders,
  };
  return vault;
}

// ---------- file-access guards ----------

test("create guards: absent succeeds, existing fails stale with actual", async () => {
  const { module: mod, cleanup } = await loadModule("src/ordinary-sync-file-access.ts");
  try {
    const vault = makeVault();
    const fa = new mod.OrdinarySyncFileAccess(vault, ()=>{}, ()=>1234);
    const absent = { kind:"absent" };
    const created = await fa.create("a/b.md", absent, enc("hello"));
    assert.equal(created.kind, "created");
    assert.equal(new TextDecoder().decode(created.snapshot.bytes), "hello");
    // second create with same absent should be stale
    const stale = await fa.create("a/b.md", absent, enc("world"));
    assert.equal(stale.kind, "stale");
    assert.ok(stale.actual);
    assert.equal(new TextDecoder().decode(stale.actual.bytes), "hello");
    // guard mutation preservation: create with hash expected when file exists with different hash -> stale
    const hash = await shaHex(enc("other"));
    const stale2 = await fa.create("a/b.md", {kind:"hash", hash}, enc("x"));
    assert.equal(stale2.kind, "stale");
  } finally { await cleanup(); }
});

test("replace guards: hash match succeeds, mutation/appearance causes stale", async () => {
  const { module: mod, cleanup } = await loadModule("src/ordinary-sync-file-access.ts");
  try {
    const vault = makeVault(new Map([["note.md", {bytes:enc("orig"), mtime:1000}]]));
    const fa = new mod.OrdinarySyncFileAccess(vault);
    const r = await fa.readExact("note.md");
    assert.ok(r);
    const expected = { kind:"hash", hash: r.hash };
    const replaced = await fa.replace("note.md", expected, enc("new"));
    assert.equal(replaced.kind, "replaced");
    assert.equal(new TextDecoder().decode(replaced.snapshot.bytes), "new");
    // file appearance: try to replace absent file with absent expected -> stale
    const staleMissing = await fa.replace("missing.md", {kind:"hash", hash:"abc"}, enc("x"));
    assert.equal(staleMissing.kind, "stale");
    assert.equal(staleMissing.actual, null);
    // mutation: old hash should now be stale
    const staleMutated = await fa.replace("note.md", expected, enc("again"));
    assert.equal(staleMutated.kind, "stale");
    assert.ok(staleMutated.actual);
    assert.equal(new TextDecoder().decode(staleMutated.actual.bytes), "new");
    // absent expected on existing file -> stale
    const staleAbsent = await fa.replace("note.md", {kind:"absent"}, enc("x"));
    assert.equal(staleAbsent.kind, "stale");
  } finally { await cleanup(); }
});

test("deleteExact guards: hash match deletes, stale on mutation, absent idempotent", async () => {
  const { module: mod, cleanup } = await loadModule("src/ordinary-sync-file-access.ts");
  try {
    const vault = makeVault(new Map([["del.md", {bytes:enc("toDelete"), mtime:1000}]]));
    const fa = new mod.OrdinarySyncFileAccess(vault);
    const r = await fa.readExact("del.md");
    assert.ok(r);
    // wrong hash -> stale with actual
    const wrong = await fa.deleteExact("del.md", {kind:"hash", hash:"deadbeef"});
    assert.equal(wrong.kind, "stale");
    assert.ok(wrong.actual);
    // correct hash -> deleted
    const ok = await fa.deleteExact("del.md", {kind:"hash", hash: r.hash});
    assert.equal(ok.kind, "deleted");
    assert.equal(vault._files.has("del.md"), false);
    // delete absent with absent expected -> deleted (idempotent)
    const again = await fa.deleteExact("del.md", {kind:"absent"});
    assert.equal(again.kind, "deleted");
    // appearance guard: recreate file, then absent expected should be stale
    vault._files.set("del.md", {bytes:enc("reappeared"), mtime:2000});
    const staleAppear = await fa.deleteExact("del.md", {kind:"absent"});
    assert.equal(staleAppear.kind, "stale");
    assert.ok(staleAppear.actual);
    // mutation guard: hash stale after change
    const r2 = await fa.readExact("del.md");
    assert.ok(r2);
    vault._files.set("del.md", {bytes:enc("mutated"), mtime:3000});
    const staleMut = await fa.deleteExact("del.md", {kind:"hash", hash: r2.hash});
    assert.equal(staleMut.kind, "stale");
  } finally { await cleanup(); }
});

test("writeCanonicalIfUnchanged and preserveSibling preserve exact bytes and collision", async () => {
  const { module: mod, cleanup } = await loadModule("src/ordinary-sync-file-access.ts");
  try {
    const vault = makeVault(new Map([["canon.md", {bytes:enc("local"), mtime:1000}]]));
    const fa = new mod.OrdinarySyncFileAccess(vault, ()=>{}, ()=> 1700000000000);
    const r = await fa.readExact("canon.md");
    assert.ok(r);
    // preserve sibling exact bytes
    const sibling = await fa.preserveSiblingIfUnchanged("canon.md", r.hash, r.bytes);
    assert.equal(sibling.kind, "preserved");
    assert.ok(sibling.siblingPath.includes("_conflict_"));
    const sibRead = await fa.readExact(sibling.siblingPath);
    assert.ok(sibRead);
    assert.equal(sibRead.hash, r.hash);
    assert.deepEqual(sibRead.bytes, r.bytes);
    // collision: second preserve with same timestamp should collide
    const fa2 = new mod.OrdinarySyncFileAccess(vault, ()=>{}, ()=>1700000000000);
    const collision = await fa2.preserveSiblingIfUnchanged("canon.md", r.hash, r.bytes);
    // need current hash still same, but sibling exists -> collision
    // r hash still valid because canon unchanged
    assert.equal(collision.kind, "collision");
    assert.equal(collision.siblingPath, sibling.siblingPath);
    // writeCanonical exact bytes
    const remote = enc("remote");
    const written = await fa.writeCanonicalIfUnchanged("canon.md", r.hash, remote);
    assert.equal(written.kind, "written");
    assert.deepEqual(written.snapshot.bytes, remote);
    const after = await fa.readExact("canon.md");
    assert.ok(after);
    assert.deepEqual(after.bytes, remote);
    // stale due to mutation
    const stale = await fa.writeCanonicalIfUnchanged("canon.md", r.hash, enc("x"));
    assert.equal(stale.kind, "stale");
    assert.equal(stale.reason, "mutated");
    assert.ok(stale.actual);
  } finally { await cleanup(); }
});

test("preserveSibling stale on missing/mutated returns actual", async () => {
  const { module: mod, cleanup } = await loadModule("src/ordinary-sync-file-access.ts");
  try {
    const vault = makeVault();
    const fa = new mod.OrdinarySyncFileAccess(vault);
    const missing = await fa.preserveSiblingIfUnchanged("no.md", "abc", enc("x"));
    assert.equal(missing.kind, "stale");
    assert.equal(missing.reason, "missing");
    assert.equal(missing.actual, null);
    vault._files.set("a.md", {bytes:enc("v1"), mtime:1000});
    const r = await fa.readExact("a.md");
    assert.ok(r);
    vault._files.set("a.md", {bytes:enc("v2"), mtime:2000});
    const mutated = await fa.preserveSiblingIfUnchanged("a.md", r.hash, r.bytes);
    assert.equal(mutated.kind, "stale");
    assert.equal(mutated.reason, "mutated");
    assert.ok(mutated.actual);
  } finally { await cleanup(); }
});

// ---------- baseline ----------

test("baseline live empty markdown carries baseText ''", async () => {
  const { module: mod, cleanup } = await loadModule("src/ordinary-sync-baseline.ts");
  try {
    const empty = { kind:"live", path:"note.md", bytes: enc(""), serverRevision:1, serverHash:"h", localHash:"lh", localMTime:1, localSize:0 };
    const e = mod.baselineFromAcknowledgement(empty);
    assert.equal(e.baseText, "");
    assert.ok("baseText" in e);
    assert.equal(e.serverDeleted, false);
  } finally { await cleanup(); }
});

test("baseline tombstone never carries baseText", async () => {
  const { module: mod, cleanup } = await loadModule("src/ordinary-sync-baseline.ts");
  try {
    const del = { kind:"deleted", path:"note.md", serverRevision:2, serverHash:"h", localHash:"lh", localMTime:2, localSize:0 };
    const e = mod.baselineFromAcknowledgement(del);
    assert.equal(e.serverDeleted, true);
    assert.equal("baseText" in e, false);
    assert.equal(e.baseText, undefined);
  } finally { await cleanup(); }
});

test("baseline live binary/invalid UTF-8/oversized omit baseText", async () => {
  const { module: mod, cleanup } = await loadModule("src/ordinary-sync-baseline.ts");
  try {
    const bin = { kind:"live", path:"image.png", bytes: enc("hello"), serverRevision:1, serverHash:"h", localHash:"lh", localMTime:1, localSize:5 };
    assert.equal(mod.baselineFromAcknowledgement(bin).baseText, undefined);
    assert.equal("baseText" in mod.baselineFromAcknowledgement(bin), false);
    const invalid = { kind:"live", path:"note.md", bytes: new Uint8Array([0xff,0xfe]), serverRevision:1, serverHash:"h", localHash:"lh", localMTime:1, localSize:2 };
    assert.equal("baseText" in mod.baselineFromAcknowledgement(invalid), false);
    const big = new Uint8Array(2*1024*1024+1); big.fill(97);
    const oversize = { kind:"live", path:"note.md", bytes: big, serverRevision:1, serverHash:"h", localHash:"lh", localMTime:1, localSize:big.length };
    assert.equal("baseText" in mod.baselineFromAcknowledgement(oversize), false);
    const liveText = { kind:"live", path:"note.md", bytes: enc("hello"), serverRevision:1, serverHash:"h", localHash:"lh", localMTime:1, localSize:5 };
    assert.equal(mod.baselineFromAcknowledgement(liveText).baseText, "hello");
  } finally { await cleanup(); }
});
