import assert from "node:assert/strict";
import test from "node:test";
import { loadModule, loadSyncEngine } from "./helpers/sync-engine-loader.mjs";

function enc(str) { return new TextEncoder().encode(str); }
async function shaHex(bytes) {
  const d = await crypto.subtle.digest("SHA-256", bytes.slice());
  return Array.from(new Uint8Array(d)).map(b=>b.toString(16).padStart(2,"0")).join("");
}
function tStub(key){ return key; }

// Helpers for vault mocking
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
      const copy=new Uint8Array(e.bytes).buffer.slice(e.bytes.byteOffset,e.bytes.byteOffset+e.bytes.byteLength);
      const dup=new ArrayBuffer(copy.byteLength);
      new Uint8Array(dup).set(new Uint8Array(copy));
      return dup;
    },
    async createBinary(p,data,opts){
      const bytes=new Uint8Array(data.slice(0));
      const mtime=opts?.mtime ?? Date.now();
      files.set(p,{bytes,mtime});
      return { __tfile:true, path:p, stat:{mtime,size:bytes.length} };
    },
    async modifyBinary(file,data,opts){
      const bytes=new Uint8Array(data.slice(0));
      const mtime=opts?.mtime ?? Date.now();
      if(!files.has(file.path)) throw new Error("missing modify");
      files.set(file.path,{bytes,mtime});
    },
    async createFolder(p){ folders.add(p); },
    async delete(file){ files.delete(file.path); },
    getFiles(){ return [...files.entries()].filter(([k,v])=>true).map(([p,e])=>({__tfile:true,path:p,stat:{mtime:e.mtime,size:e.bytes.length}})); },
    adapter: { exists:async()=>false, read:async()=>"", write:async()=>{} },
    _files:files, _folders:folders
  };
  return vault;
}

// upload/download/adopt baseText capture via baselineFromAcknowledgement is tested already via primitive file,
// but integration: engine upload success stores baseText including empty
test("sync-engine upload success captures acknowledged baseText via baselineFromAcknowledgement", async () => {
  const vault = makeVault(new Map([["note.md", {bytes:enc("hello"), mtime:1000}]]));
  const api = {
    async uploadV2(vaultId,input){
      const h = await shaHex(new Uint8Array(input.content));
      return { path:input.path, type:"markdown", hash:h, size:new Uint8Array(input.content).byteLength, mtime: 2000, revision:2, deleted:false };
    },
    async downloadV2(){ throw new Error("unused"); }
  };
  const stored = new Map();
  const baseline = {
    get:(p)=> stored.get(p) ?? null,
    set:(p,e)=> stored.set(p,e),
    removePending:()=>{},
    removePendingForPath:()=>{},
    putPending:()=>{},
    save:async()=>{},
    getConflict:()=>null,
    putConflict:()=>{},
    paths:()=>[],
    pending:()=>[],
  };
  const plugin = { settings:{ syncPoisonObsidianFiles:false, syncIntervalSec:3, remotePollIntervalSec:30, vaultId:"v1" }, t:tStub, setSyncState:()=>{}, openConflictModal:()=>{} };
  const app={ vault };
  const { SyncEngine, cleanup } = await loadSyncEngine();
  try{
    const engine=new SyncEngine(app, api, baseline, plugin);
    // prepare localMeta manually
    const localHash=await shaHex(enc("hello"));
    const action={ kind:"upload", path:"note.md", local:{path:"note.md", hash:localHash, size:5, mtime:1000}, baseRevision:1, operationID:"op1" };
    await engine.applyAction("v1", action);
    const entry=stored.get("note.md");
    assert.ok(entry);
    assert.equal(entry.baseText, "hello");
    assert.equal(entry.serverRevision,2);
    // empty text
    vault._files.set("empty.md", {bytes:enc(""), mtime:1001});
    const h2=await shaHex(enc(""));
    const api2={ async uploadV2(v,t){ const hh=await shaHex(new Uint8Array(t.content)); return {path:t.path,type:"markdown",hash:hh,size:0,mtime:2001,revision:3,deleted:false}; } };
    const engine2=new SyncEngine({vault}, api2, baseline, plugin);
    await engine2.applyAction("v1", {kind:"upload", path:"empty.md", local:{path:"empty.md", hash:h2, size:0, mtime:1001}, baseRevision:3, operationID:"op2"});
    const e2=stored.get("empty.md");
    assert.ok(e2);
    assert.equal(e2.baseText, "");
    assert.ok("baseText" in e2);
    // binary should omit baseText
    vault._files.set("image.png", {bytes:enc("binarycontent"), mtime:1002});
    const h3=await shaHex(enc("binarycontent"));
    const api3={ async uploadV2(v,t){ const hh=await shaHex(new Uint8Array(t.content)); return {path:t.path,type:"attachment",hash:hh,size:13,mtime:2002,revision:4,deleted:false}; } };
    const engine3=new SyncEngine({vault}, api3, baseline, plugin);
    await engine3.applyAction("v1", {kind:"upload", path:"image.png", local:{path:"image.png", hash:h3, size:13, mtime:1002}, baseRevision:4, operationID:"op3"});
    const e3=stored.get("image.png");
    assert.ok(e3);
    assert.equal(e3.baseText, undefined);
    assert.equal("baseText" in e3, false);
  } finally { await cleanup(); }
});

test("planActions emits reconcile for both-changed live files but delete/edit remains conflict", async () => {
  const { SyncEngine, cleanup } = await loadSyncEngine();
  try{
    const contentLocal=enc("local changed");
    const file={__tfile:true, path:"note.md", content:contentLocal.buffer, stat:{mtime:20,size:contentLocal.byteLength}};
    const vault={
      getAbstractFileByPath(p){ return p==="note.md"?file:null; },
      async readBinary(f){ return f.content; },
      getFiles(){ return [file]; }
    };
    const baselineEntry={ serverRevision:1, serverHash:await shaHex(enc("base")), serverDeleted:false, localHash:await shaHex(enc("base")), localMTime:10, localSize:4, baseText:"base" };
    const baseline={
      get:(p)=> p==="note.md"?baselineEntry:null,
      getConflict:()=>null,
      paths:()=>["note.md"],
      pending:()=>[],
      removePending:()=>{}
    };
    const plugin={ settings:{ syncPoisonObsidianFiles:false, syncIntervalSec:3, remotePollIntervalSec:30 }, t:tStub };
    const engine=new SyncEngine({vault}, {}, baseline, plugin);
    // both-changed live: local changed (hash diff), remote changed (revision 2 diff)
    const remoteLive={ path:"note.md", type:"markdown", hash:await shaHex(enc("remote changed")), size:14, mtime:30, revision:2, deleted:false };
    const actions=await engine.planActions(false, new Map([["note.md", remoteLive]]), []);
    assert.equal(actions.length,1);
    assert.equal(actions[0].kind, "reconcile");
    // delete/edit should remain conflict: local deleted, remote changed
    const vault2={ getAbstractFileByPath:()=>null, getFiles:()=>[] };
    const engine2=new SyncEngine({vault: vault2}, {}, baseline, plugin);
    const remoteLive2={ path:"note.md", type:"markdown", hash:await shaHex(enc("remote2")), size:7, mtime:31, revision:3, deleted:false };
    const actions2=await engine2.planActions(false, new Map([["note.md", remoteLive2]]), []);
    assert.equal(actions2[0].kind, "conflict");
    // remote tombstone vs local edit => conflict
    const remoteDel={ path:"note.md", type:"markdown", hash:"", size:0, mtime:32, revision:4, deleted:true };
    const engine3=new SyncEngine({vault}, {}, baseline, plugin);
    const actions3=await engine3.planActions(false, new Map([["note.md", remoteDel]]), []);
    assert.equal(actions3[0].kind, "conflict");
  } finally { await cleanup(); }
});

test("planActions reconcile for no-baseline mismatch, adopt still not reconcile when hashes equal", async () => {
  const { SyncEngine, cleanup } = await loadSyncEngine();
  try{
    const bytes=enc("local");
    const file={__tfile:true, path:"new.md", content:bytes.buffer, stat:{mtime:10,size:bytes.length}};
    const vault={ getAbstractFileByPath(p){return p==="new.md"?file:null;}, async readBinary(f){return f.content;}, getFiles(){return [file];}};
    const baseline={ get:()=>null, getConflict:()=>null, paths:()=>[], pending:()=>[] };
    const plugin={ settings:{ syncPoisonObsidianFiles:false, syncIntervalSec:3, remotePollIntervalSec:30 }, t:tStub };
    const engine=new SyncEngine({vault}, {}, baseline, plugin);
    const remote={ path:"new.md", type:"markdown", hash:await shaHex(enc("remote")), size:6, mtime:20, revision:1, deleted:false };
    const acts=await engine.planActions(false, new Map([["new.md", remote]]), []);
    assert.equal(acts[0].kind, "reconcile");
    // identical hashes should adopt, not reconcile
    const h=await shaHex(enc("same"));
    const file2={__tfile:true, path:"same.md", content:enc("same").buffer, stat:{mtime:10,size:4}};
    const vault2={ getAbstractFileByPath(p){return p==="same.md"?file2:null;}, async readBinary(f){return f.content;}, getFiles:()=>[file2]};
    const engine2=new SyncEngine({vault: vault2}, {}, baseline, plugin);
    const remote2={ path:"same.md", type:"markdown", hash:h, size:4, mtime:20, revision:1, deleted:false };
    const acts2=await engine2.planActions(false, new Map([["same.md", remote2]]), []);
    assert.equal(acts2[0].kind, "adopt");
  } finally { await cleanup(); }
});

// Resolver direct tests
test("resolver clean text merge call ordering/persistence and success state", async () => {
  const { module: resolverMod, cleanup: rc } = await loadModule("src/ordinary-sync-conflict-resolver.ts");
  const { module: faMod, cleanup: fc } = await loadModule("src/ordinary-sync-file-access.ts");
  try{
    assert.ok(resolverMod.OrdinarySyncConflictResolver);
    // setup vault and baseline
    globalThis.window={ setTimeout:(fn)=>{ fn(); return 1; }, clearTimeout:()=>{} };
    const vault=makeVault(new Map([["note.md", {bytes:enc("a\nb\nc\nd"), mtime:10}]]));
    const baselineStore=new Map();
    const baseline={
      get:(p)=> baselineStore.get(p) ?? null,
      set:(p,e)=> baselineStore.set(p,e),
      save: async()=>{ saveCalls++; },
      putPending:(op)=>{ pending.push(op); },
      removePending:(id)=>{ pending=pending.filter(o=>o.id!==id); },
      removePendingForPath:(p)=>{ pending=pending.filter(o=>o.path!==p); },
      putConflict:async()=>{},
      getConflict:()=>null,
    };
    let saveCalls=0;
    let pending=[];
    let createId="fresh-op-1";
    const deps={
      vaultId:"v1",
      baseline,
      fileAccess: new faMod.OrdinarySyncFileAccess(vault, ()=>{}, ()=>123),
      api: {
        async downloadV2(v,p,rev){
          assert.equal(rev,2);
          const b=enc("a\nb\nc\nD");
          const h=await shaHex(b);
          return { content:b.buffer.slice(b.byteOffset,b.byteOffset+b.byteLength), meta:{path:"note.md", type:"markdown", hash:h, size:b.length, mtime:30, revision:2, deleted:false} };
        },
        async uploadV2(v,input){
          uploadCalls++;
          assert.equal(input.baseRevision,2);
          assert.equal(input.operationID, "fresh-op-1");
          const h=await shaHex(new Uint8Array(input.content));
          return { path:input.path, type:"markdown", hash:h, size:new Uint8Array(input.content).byteLength, mtime:40, revision:3, deleted:false };
        }
      },
      recordConflict: async()=>{ throw new Error("should not conflict"); },
      createOperationID:()=>createId,
      now:()=>999,
    };
    const baseText="a\nb\nc\nd";
    // baseText is original, local is "A\nb\nc\nd", remote is "a\nb\nc\nD" -> merge should be "A\nb\nc\nD"
    // setup baseline with baseText
    baselineStore.set("note.md", { serverRevision:1, serverHash:await shaHex(enc("a\nb\nc\nd")), serverDeleted:false, localHash:await shaHex(enc("A\nb\nc\nd")), localMTime:10, localSize:7, baseText });
    // put local changed
    vault._files.set("note.md", {bytes:enc("A\nb\nc\nd"), mtime:10});
    const localHash=await shaHex(enc("A\nb\nc\nd"));
    const remoteMeta={ path:"note.md", type:"markdown", hash:await shaHex(enc("a\nb\nc\nD")), size:7, mtime:30, revision:2, deleted:false };
    let uploadCalls=0;
    const resolver=new resolverMod.OrdinarySyncConflictResolver(deps);
    const result=await resolver.resolve({ path:"note.md", expectedHash:localHash, remote:remoteMeta });
    assert.equal(uploadCalls,1);
    assert.ok(saveCalls>=2);
    // pending cleared after success
    assert.equal(pending.length,0);
    const final=baselineStore.get("note.md");
    assert.equal(final.baseText, "A\nb\nc\nD");
    assert.equal(final.serverRevision,3);
  } finally { await rc(); await fc(); }
});

test("resolver overlapping text conflict: no write/no upload/modal for markdown", async () => {
  const { module: resolverMod, cleanup: rc } = await loadModule("src/ordinary-sync-conflict-resolver.ts");
  const { module: faMod, cleanup: fc } = await loadModule("src/ordinary-sync-file-access.ts");
  try{
    globalThis.window={ setTimeout:(f)=>{f(); return 1;}, clearTimeout:()=>{} };
    const vault=makeVault(new Map([["note.md", {bytes:enc("a\nX\nc"), mtime:10}]]));
    const baselineStore=new Map();
    const baseline={
      get:(p)=> baselineStore.get(p)??null,
      set:(p,e)=> baselineStore.set(p,e),
      save: async()=>{},
      putPending:()=>{},
      removePending:()=>{},
      removePendingForPath:()=>{},
    };
    baselineStore.set("note.md", { serverRevision:1, serverHash:await shaHex(enc("a\nb\nc")), serverDeleted:false, localHash:await shaHex(enc("a\nX\nc")), localMTime:10, localSize:5, baseText:"a\nb\nc" });
    const localHash=await shaHex(enc("a\nX\nc"));
    let conflictCalled=false;
    let modalForPath=null;
    const deps={
      vaultId:"v1",
      baseline,
      fileAccess:new faMod.OrdinarySyncFileAccess(vault, ()=>{}, ()=>123),
      api:{
        async downloadV2(v,p,rev){ const b=enc("a\nY\nc"); return {content:b.buffer.slice(b.byteOffset,b.byteOffset+b.byteLength), meta:{path:"note.md", type:"markdown", hash:await shaHex(b), size:b.length, mtime:30, revision:2, deleted:false}}; },
        async uploadV2(){ throw new Error("should not upload"); }
      },
      recordConflict: async(path,local,remote)=>{
        conflictCalled=true;
        assert.equal(path,"note.md");
        assert.equal(remote.type,"markdown");
        // ensure local bytes untouched
        const cur=vault._files.get("note.md").bytes;
        assert.deepEqual(cur, enc("a\nX\nc"));
      },
      createOperationID:()=>"id",
      now:()=>1,
    };
    const resolver=new resolverMod.OrdinarySyncConflictResolver(deps);
    await resolver.resolve({path:"note.md", expectedHash:localHash, remote:{path:"note.md", type:"markdown", hash:await shaHex(enc("a\nY\nc")), size:5, mtime:30, revision:2, deleted:false}});
    assert.equal(conflictCalled,true);
    const cur2=vault._files.get("note.md").bytes;
    assert.deepEqual(cur2, enc("a\nX\nc"));
  } finally { await rc(); await fc(); }
});

test("resolver binary sibling exact + remote canonical + pending sibling", async () => {
  const { module: resolverMod, cleanup: rc } = await loadModule("src/ordinary-sync-conflict-resolver.ts");
  const { module: faMod, cleanup: fc } = await loadModule("src/ordinary-sync-file-access.ts");
  try{
    globalThis.window={ setTimeout:(f)=>{f(); return 1;}, clearTimeout:()=>{} };
    const localBytes=new Uint8Array([1,2,3,4]);
    const remoteBytes=new Uint8Array([5,6,7,8]);
    const vault=makeVault(new Map([["image.png", {bytes:localBytes.slice(), mtime:10}]]));
    const baselineStore=new Map();
    const baseline={
      get:(p)=> baselineStore.get(p)??null,
      set:(p,e)=> baselineStore.set(p,e),
      save: async()=>{ saveCalls++; },
      putPending:(op)=>{ pending.push(op); },
      removePending:(id)=>{ pending=pending.filter(o=>o.id!==id); },
      removePendingForPath:(p)=>{ pending=pending.filter(o=>o.path!==p); },
    };
    baselineStore.set("image.png", { serverRevision:1, serverHash:await shaHex(localBytes), serverDeleted:false, localHash:await shaHex(localBytes), localMTime:10, localSize:4, baseText:undefined });
    let saveCalls=0; let pending=[];
    const deps={
      vaultId:"v1",
      baseline,
      fileAccess:new faMod.OrdinarySyncFileAccess(vault, ()=>{}, ()=> new Date("2024-01-02T03:04:05.006Z").getTime()),
      api:{
        async downloadV2(v,p,rev){ return {content:remoteBytes.buffer.slice(remoteBytes.byteOffset,remoteBytes.byteOffset+remoteBytes.byteLength), meta:{path:"image.png", type:"attachment", hash:await shaHex(remoteBytes), size:4, mtime:30, revision:2, deleted:false}}; },
        async uploadV2(){ throw new Error("no upload"); }
      },
      recordConflict: async()=>{ throw new Error("should not conflict"); },
      createOperationID:()=>"sibling-op",
      now:()=>1,
    };
    const localHash=await shaHex(localBytes);
    const resolver=new resolverMod.OrdinarySyncConflictResolver(deps);
    await resolver.resolve({path:"image.png", expectedHash:localHash, remote:{path:"image.png", type:"attachment", hash:await shaHex(remoteBytes), size:4, mtime:30, revision:2, deleted:false}});
    // sibling created
    const siblingPath="image_conflict_2024-01-02T03-04-05-006Z.png";
    const sib=vault._files.get(siblingPath);
    assert.ok(sib);
    assert.deepEqual(sib.bytes, localBytes);
    // canonical installed remote
    const canon=vault._files.get("image.png");
    assert.deepEqual(canon.bytes, remoteBytes);
    const entry=baselineStore.get("image.png");
    assert.equal(entry.serverHash, await shaHex(remoteBytes));
    assert.equal(entry.baseText, undefined);
    assert.equal("baseText" in entry, false);
    // sibling pending
    assert.equal(pending.length,1);
    assert.equal(pending[0].path, siblingPath);
  } finally { await rc(); await fc(); }
});

test("resolver local mutation guard leaves canonical untouched and records conflict", async () => {
  const { module: resolverMod, cleanup: rc } = await loadModule("src/ordinary-sync-conflict-resolver.ts");
  const { module: faMod, cleanup: fc } = await loadModule("src/ordinary-sync-file-access.ts");
  try{
    globalThis.window={ setTimeout:(f)=>{f(); return 1;}, clearTimeout:()=>{} };
    const vault=makeVault(new Map([["note.md", {bytes:enc("mutated after plan"), mtime:10}]]));
    const baselineStore=new Map();
    const baseline={
      get:(p)=> baselineStore.get(p)??null,
      set:(p,e)=> baselineStore.set(p,e),
      save: async()=>{},
      putPending:()=>{},
      removePending:()=>{},
      removePendingForPath:()=>{},
    };
    baselineStore.set("note.md", { serverRevision:1, serverHash:await shaHex(enc("base")), serverDeleted:false, localHash:await shaHex(enc("old local")), localMTime:10, localSize:9, baseText:"base" });
    let conflictCalled=false;
    const deps={
      vaultId:"v1",
      baseline,
      fileAccess:new faMod.OrdinarySyncFileAccess(vault, ()=>{}, ()=>123),
      api:{
        async downloadV2(v,p,rev){ const b=enc("remote"); return {content:b.buffer.slice(b.byteOffset,b.byteOffset+b.byteLength), meta:{path:"note.md", type:"markdown", hash:await shaHex(b), size:6, mtime:30, revision:2, deleted:false}}; },
        async uploadV2(){ throw new Error("no upload"); }
      },
      recordConflict: async()=>{ conflictCalled=true; },
      createOperationID:()=>"id",
      now:()=>1,
    };
    const expectedHash=await shaHex(enc("old local")); // stale expected, but current is mutated
    const resolver=new resolverMod.OrdinarySyncConflictResolver(deps);
    await resolver.resolve({path:"note.md", expectedHash, remote:{path:"note.md", type:"markdown", hash:await shaHex(enc("remote")), size:6, mtime:30, revision:2, deleted:false}});
    assert.equal(conflictCalled,true);
    assert.deepEqual(vault._files.get("note.md").bytes, enc("mutated after plan"));
  } finally { await rc(); await fc(); }
});

test("resolver upload-time 409 uses fresh ID and authoritative revision", async () => {
  const { module: resolverMod, cleanup: rc } = await loadModule("src/ordinary-sync-conflict-resolver.ts");
  const { module: faMod, cleanup: fc } = await loadModule("src/ordinary-sync-file-access.ts");
  try{
    globalThis.window={ setTimeout:(f)=>{f(); return 1;}, clearTimeout:()=>{} };
    const vault=makeVault(new Map([["note.md", {bytes:enc("A\nb\nc\nd"), mtime:10}]]));
    const baselineStore=new Map();
    baselineStore.set("note.md", { serverRevision:1, serverHash:await shaHex(enc("a\nb\nc\nd")), serverDeleted:false, localHash:await shaHex(enc("A\nb\nc\nd")), localMTime:10, localSize:7, baseText:"a\nb\nc\nd" });
    const baseline={
      get:(p)=> baselineStore.get(p)??null,
      set:(p,e)=> baselineStore.set(p,e),
      save: async()=>{},
      putPending:(op)=>{ pending.push(op); },
      removePending:(id)=>{ pending=pending.filter(o=>o.id!==id); },
      removePendingForPath:(p)=>{ pending=pending.filter(o=>o.path!==p); },
    };
    let pending=[{id:"old-pending", kind:"upsert", path:"note.md", createdAt:1}];
    // mimic real BaselineStore putPending filtering by path
    baseline.putPending = (op)=>{ pending = pending.filter(o=> o.path!==op.path && o.oldPath!==op.path); pending.push(op); };
    const deps={
      vaultId:"v1",
      baseline,
      fileAccess:new faMod.OrdinarySyncFileAccess(vault, ()=>{}, ()=>123),
      api:{
        async downloadV2(v,p,rev){ const b=enc("a\nb\nc\nD"); return {content:b.buffer.slice(b.byteOffset,b.byteOffset+b.byteLength), meta:{path:"note.md", type:"markdown", hash:await shaHex(b), size:7, mtime:30, revision:5, deleted:false}}; },
        async uploadV2(v,input){
          assert.equal(input.baseRevision,5);
          assert.equal(input.operationID,"fresh-id-2");
          const h=await shaHex(new Uint8Array(input.content));
          return {path:input.path, type:"markdown", hash:h, size:7, mtime:40, revision:6, deleted:false};
        }
      },
      recordConflict: async()=>{ throw new Error("should merge"); },
      createOperationID:()=>"fresh-id-2",
      now:()=>2,
    };
    const resolver=new resolverMod.OrdinarySyncConflictResolver(deps);
    const localHash=await shaHex(enc("A\nb\nc\nd"));
    await resolver.resolve({path:"note.md", expectedHash:localHash, remote:{path:"note.md", type:"markdown", hash:await shaHex(enc("a\nb\nc\nD")), size:7, mtime:30, revision:5, deleted:false}});
    // fresh ID persisted then cleared on success - old pending should be replaced
    assert.equal(pending.length,0);
  } finally { await rc(); await fc(); }
});

test("resolver second 409 bounded no third upload pending retained", async () => {
  const { module: resolverMod, cleanup: rc } = await loadModule("src/ordinary-sync-conflict-resolver.ts");
  const { module: faMod, cleanup: fc } = await loadModule("src/ordinary-sync-file-access.ts");
  try{
    globalThis.window={ setTimeout:(f)=>{f(); return 1;}, clearTimeout:()=>{} };
    const vault=makeVault(new Map([["note.md", {bytes:enc("A\nb\nc\nd"), mtime:10}]]));
    const baselineStore=new Map();
    baselineStore.set("note.md", { serverRevision:1, serverHash:await shaHex(enc("a\nb\nc\nd")), serverDeleted:false, localHash:await shaHex(enc("A\nb\nc\nd")), localMTime:10, localSize:7, baseText:"a\nb\nc\nd" });
    const baseline={
      get:(p)=> baselineStore.get(p)??null,
      set:(p,e)=> baselineStore.set(p,e),
      save: async()=>{},
      putPending:(op)=>{ pending.push(op); },
      removePending:(id)=>{ pending=pending.filter(o=>o.id!==id); },
      removePendingForPath:(p)=>{ pending=pending.filter(o=>o.path!==p); },
    };
    let pending=[];
    let uploadAttempts=0;
    const deps={
      vaultId:"v1",
      baseline,
      fileAccess:new faMod.OrdinarySyncFileAccess(vault, ()=>{}, ()=>123),
      api:{
        async downloadV2(v,p,rev){ const b=enc("a\nb\nc\nD"); return {content:b.buffer.slice(b.byteOffset,b.byteOffset+b.byteLength), meta:{path:"note.md", type:"markdown", hash:await shaHex(b), size:7, mtime:30, revision:5, deleted:false}}; },
        async uploadV2(v,input){
          uploadAttempts++;
          const err=new Error("conflict");
          err.status=409;
          err.current={ path:"note.md", type:"markdown", hash:"h", size:7, mtime:35, revision:6, deleted:false };
          throw err;
        }
      },
      recordConflict: async()=>{ throw new Error("should not conflict for merged"); },
      createOperationID:()=>"fresh-id-3",
      now:()=>3,
    };
    const resolver=new resolverMod.OrdinarySyncConflictResolver(deps);
    const localHash=await shaHex(enc("A\nb\nc\nd"));
    const res=await resolver.resolve({path:"note.md", expectedHash:localHash, remote:{path:"note.md", type:"markdown", hash:await shaHex(enc("a\nb\nc\nD")), size:7, mtime:30, revision:5, deleted:false}});
    assert.equal(uploadAttempts,1);
    assert.equal(pending.length,1);
    assert.equal(pending[0].id,"fresh-id-3");
    // ensure not thrown
    assert.ok(res);
  } finally { await rc(); await fc(); }
});

test("resolver transport failure pending retained handled", async () => {
  const { module: resolverMod, cleanup: rc } = await loadModule("src/ordinary-sync-conflict-resolver.ts");
  const { module: faMod, cleanup: fc } = await loadModule("src/ordinary-sync-file-access.ts");
  try{
    globalThis.window={ setTimeout:(f)=>{f(); return 1;}, clearTimeout:()=>{} };
    const vault=makeVault(new Map([["note.md", {bytes:enc("A\nb\nc\nd"), mtime:10}]]));
    const baselineStore=new Map();
    baselineStore.set("note.md", { serverRevision:1, serverHash:await shaHex(enc("a\nb\nc\nd")), serverDeleted:false, localHash:await shaHex(enc("A\nb\nc\nd")), localMTime:10, localSize:7, baseText:"a\nb\nc\nd" });
    const baseline={
      get:(p)=> baselineStore.get(p)??null,
      set:(p,e)=> baselineStore.set(p,e),
      save: async()=>{},
      putPending:(op)=>{ pending.push(op); },
      removePending:(id)=>{ pending=pending.filter(o=>o.id!==id); },
      removePendingForPath:(p)=>{ pending=pending.filter(o=>o.path!==p); },
    };
    let pending=[];
    const deps={
      vaultId:"v1",
      baseline,
      fileAccess:new faMod.OrdinarySyncFileAccess(vault, ()=>{}, ()=>123),
      api:{
        async downloadV2(v,p,rev){ const b=enc("a\nb\nc\nD"); return {content:b.buffer.slice(b.byteOffset,b.byteOffset+b.byteLength), meta:{path:"note.md", type:"markdown", hash:await shaHex(b), size:7, mtime:30, revision:5, deleted:false}}; },
        async uploadV2(){ throw new Error("network failure"); }
      },
      recordConflict: async()=>{ throw new Error("should not"); },
      createOperationID:()=>"fresh-id-4",
      now:()=>4,
    };
    const resolver=new resolverMod.OrdinarySyncConflictResolver(deps);
    const localHash=await shaHex(enc("A\nb\nc\nd"));
    const res=await resolver.resolve({path:"note.md", expectedHash:localHash, remote:{path:"note.md", type:"markdown", hash:await shaHex(enc("a\nb\nc\nD")), size:7, mtime:30, revision:5, deleted:false}});
    assert.equal(pending.length,1);
    assert.equal(pending[0].id,"fresh-id-4");
    assert.ok(res);
  } finally { await rc(); await fc(); }
});

function structuralConflict(current) {
  const error = new Error("conflict");
  error.status = 409;
  error.current = current;
  return error;
}

async function createRunOnceConflictFixture(retryUpload) {
  const { SyncEngine, cleanup } = await loadSyncEngine();
  const baseBytes = enc("a\nb\nc\nd");
  const localBytes = enc("A\nb\nc\nd");
  const remoteBytes = enc("a\nb\nc\nD");
  const mergedBytes = enc("A\nb\nc\nD");
  const baseHash = await shaHex(baseBytes);
  const localHash = await shaHex(localBytes);
  const remoteHash = await shaHex(remoteBytes);
  const vault = makeVault(new Map([["note.md", { bytes: localBytes, mtime: 10 }]]));
  const stored = new Map([
    ["note.md", {
      serverRevision: 1,
      serverHash: baseHash,
      serverDeleted: false,
      localHash: baseHash,
      localMTime: 1,
      localSize: baseBytes.byteLength,
      baseText: "a\nb\nc\nd",
    }],
  ]);
  let cursor = 0;
  let pending = [{ id: "old-op", kind: "upsert", path: "note.md", createdAt: 1 }];
  const conflicts = new Map();
  const events = [];
  const uploadBodies = [];
  const acknowledgements = [];
  const remote = {
    path: "note.md",
    type: "markdown",
    hash: remoteHash,
    size: remoteBytes.byteLength,
    mtime: 30,
    revision: 5,
    deleted: false,
  };
  const baseline = {
    get: (path) => stored.get(path) ?? null,
    set: (path, entry) => stored.set(path, entry),
    remove: (path) => stored.delete(path),
    paths: () => [...stored.keys()],
    pending: () => [...pending],
    conflicts: () => [...conflicts.values()],
    putPending: (operation) => {
      pending = pending.filter((existing) => existing.path !== operation.path);
      pending.push(operation);
    },
    removePending: (id) => {
      pending = pending.filter((operation) => operation.id !== id);
    },
    removePendingForPath: (path) => {
      pending = pending.filter((operation) => operation.path !== path && operation.oldPath !== path);
    },
    getConflict: (path) => conflicts.get(path) ?? null,
    putConflict: (conflict) => conflicts.set(conflict.path, conflict),
    removeConflict: (path) => conflicts.delete(path),
    load: async () => {},
    save: async () => { events.push("save"); },
    bindVault: () => false,
    getCursor: () => cursor,
    setCursor: (next) => {
      cursor = Math.max(cursor, next);
      events.push(`cursor:${next}`);
    },
  };
  let uploadCalls = 0;
  const api = {
    hasToken: () => true,
    changes: async () => ({ files: [], next_cursor: 5, has_more: false, recovery_snapshot: false }),
    manifest: async () => ({ files: [], next_cursor: 5, has_more: false, recovery_snapshot: false }),
    syncStrategy: async () => ({ policy: "user_choice", effective_mode: "short_poll", min_debounce_sec: 3, long_poll_wait_sec: 30 }),
    downloadV2: async (_vaultId, path, revision) => {
      assert.equal(path, "note.md");
      assert.equal(revision, 5);
      return {
        content: remoteBytes.buffer.slice(remoteBytes.byteOffset, remoteBytes.byteOffset + remoteBytes.byteLength),
        meta: remote,
      };
    },
    uploadV2: async (_vaultId, input) => {
      uploadCalls += 1;
      uploadBodies.push(new TextDecoder().decode(new Uint8Array(input.content)));
      if (uploadCalls === 1) throw structuralConflict(remote);
      return retryUpload(input, uploadCalls);
    },
    deleteV2: async () => { throw new Error("unused"); },
    renameV2: async () => { throw new Error("unused"); },
    acknowledge: async (vaultId, nextCursor) => {
      acknowledgements.push([vaultId, nextCursor]);
      events.push(`ack:${nextCursor}`);
    },
    isClockDriftLarge: () => false,
    getTimeOffset: () => 0,
  };
  const plugin = {
    settings: {
      vaultId: "v1",
      syncIntervalSec: 3,
      remotePollIntervalSec: 30,
      syncPoisonObsidianFiles: false,
      incrementalCheck: true,
      maxConcurrency: 1,
    },
    t: tStub,
    setSyncState: () => {},
    openConflictModal: () => {},
  };
  globalThis.window = {
    setTimeout: () => 1,
    clearTimeout: () => {},
    setInterval: () => 1,
    clearInterval: () => {},
  };

  return {
    cleanup,
    runOnce: () => new SyncEngine({ vault }, api, baseline, plugin).runOnce({ forceFull: false }),
    state: { acknowledgements, cursor: () => cursor, events, pending: () => pending, stored, uploadBodies, uploadCalls: () => uploadCalls },
    mergedBytes,
  };
}

test("runOnce resolves structural upload 409 through download, merge, and one retry before ACK", async () => {
  const fixture = await createRunOnceConflictFixture(async (input) => {
    const bytes = new Uint8Array(input.content);
    const hash = await shaHex(bytes);
    return { path: "note.md", type: "markdown", hash, size: bytes.byteLength, mtime: 40, revision: 6, deleted: false };
  });
  try {
    const succeeded = await fixture.runOnce();

    assert.equal(succeeded, true);
    assert.deepEqual(fixture.state.uploadBodies, ["A\nb\nc\nd", "A\nb\nc\nD"]);
    assert.equal(fixture.state.uploadCalls(), 2);
    assert.equal(fixture.state.stored.get("note.md").baseText, "A\nb\nc\nD");
    assert.equal(fixture.state.stored.get("note.md").serverRevision, 6);
    assert.deepEqual(fixture.state.pending(), []);
    assert.equal(fixture.state.cursor(), 5);
    assert.deepEqual(fixture.state.acknowledgements, [["v1", 5]]);
    assert.ok(fixture.state.events.indexOf("ack:5") < fixture.state.events.indexOf("cursor:5"));
  } finally {
    await fixture.cleanup();
  }
});

test("runOnce retains staged retry after a second structural 409 without cursor ACK", async () => {
  const fixture = await createRunOnceConflictFixture(async () => {
    throw structuralConflict({ path: "note.md", type: "markdown", hash: "newer", size: 8, mtime: 41, revision: 6, deleted: false });
  });
  try {
    const succeeded = await fixture.runOnce();

    assert.equal(succeeded, false);
    assert.equal(fixture.state.uploadCalls(), 2);
    assert.equal(fixture.state.pending().length, 1);
    assert.equal(fixture.state.pending()[0].kind, "upsert");
    assert.equal(fixture.state.stored.get("note.md").serverRevision, 5);
    assert.equal(fixture.state.cursor(), 0);
    assert.deepEqual(fixture.state.acknowledgements, []);
  } finally {
    await fixture.cleanup();
  }
});

test("runOnce retains staged retry after transport failure without cursor ACK", async () => {
  const fixture = await createRunOnceConflictFixture(async () => {
    throw new Error("transport failed");
  });
  try {
    const succeeded = await fixture.runOnce();

    assert.equal(succeeded, false);
    assert.equal(fixture.state.uploadCalls(), 2);
    assert.equal(fixture.state.pending().length, 1);
    assert.equal(fixture.state.stored.get("note.md").serverRevision, 5);
    assert.equal(fixture.state.cursor(), 0);
    assert.deepEqual(fixture.state.acknowledgements, []);
  } finally {
    await fixture.cleanup();
  }
});
