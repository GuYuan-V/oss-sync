import assert from "node:assert/strict";
import test from "node:test";
import { mkdtemp, mkdir, readFile, writeFile, readdir, unlink, rmdir, rm } from "node:fs/promises";
import { existsSync, statSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, dirname, resolve } from "node:path";
import { createHash } from "node:crypto";
import { loadEntry } from "./helpers/obsidian-loader.mjs";

const localPath = "协作oss/alice/notes/topic/doc.md";
const entry = { id: 1, vault_id: "owner-vault", file_id: 7, file_path: "notes/topic/doc.md", owner_username: "alice", collaborator_username: "bob", status: "accepted", created_at: "2026-10-16T00:00:00Z" };
const hash = (content) => createHash("sha256").update(content).digest("hex");

async function fixture(t) {
  const root = await mkdtemp(join(tmpdir(), "oss-collab-cleanup-"));
  const entryFile = join(root, "entry.ts");
  await writeFile(entryFile, `
    export { CollabManager } from ${JSON.stringify(resolve("src/collab-manager.ts"))};
    export { BaselineStore } from ${JSON.stringify(resolve("src/baseline.ts"))};
    export { CollaborationFileVault } from ${JSON.stringify(resolve("src/collaboration-file-vault.ts"))};
    export { TFile } from "obsidian";
  `);
  const { module: mod, cleanup } = await loadEntry(entryFile);
  const originalWindow = globalThis.window;
  const originalNotices = globalThis.__ossNotices;
  globalThis.window = globalThis;
  globalThis.__ossNotices = [];
  const calls = [];
  const controls = { deleteError: false, directoryError: false, listError: false, saveError: false, apiError: false, beforeRmdir: null };
  const put = async (path, content = "shared") => {
    await mkdir(dirname(join(root, path)), { recursive: true });
    await writeFile(join(root, path), content);
  };
  const exists = (path) => existsSync(join(root, path));
  const vault = {
    getAbstractFileByPath(path) {
      if (!exists(path)) return null;
      return statSync(join(root, path)).isFile() ? Object.assign(new mod.TFile(), { path }) : { path };
    },
    async readBinary(file) {
      const buffer = await readFile(join(root, file.path));
      return buffer.buffer.slice(buffer.byteOffset, buffer.byteOffset + buffer.byteLength);
    },
    async delete(file) {
      calls.push(["delete", file.path]);
      if (controls.deleteError) throw new Error("EACCES file");
      await unlink(join(root, file.path));
    },
    adapter: {
      exists: async (path) => exists(path),
      read: async (path) => readFile(join(root, path), "utf8"),
      async write(path, content) {
        if (controls.saveError) throw new Error("EACCES state");
        await writeFile(join(root, path), content);
      },
      async list(path) {
        calls.push(["list", path]);
        if (controls.listError) throw new Error("EACCES list");
        const items = await readdir(join(root, path), { withFileTypes: true });
        return { files: items.filter(e => !e.isDirectory()).map(e => `${path}/${e.name}`), folders: items.filter(e => e.isDirectory()).map(e => `${path}/${e.name}`) };
      },
      async rmdir(path, recursive) {
        calls.push(["rmdir", path, recursive]);
        assert.equal(recursive, false);
        assert.ok(path === "协作oss" || path.startsWith("协作oss/"));
        await controls.beforeRmdir?.(path);
        if (controls.directoryError) throw new Error("EACCES directory");
        await rmdir(join(root, path));
      },
    },
  };
  await put(localPath);
  const baseline = new mod.BaselineStore(vault);
  await baseline.load();
  baseline.bindCollaborationAccount("bob");
  const seed = (e = entry, path = localPath) => baseline.setCollaboration(e.vault_id, e.file_id, {
    vaultId: e.vault_id, fileId: e.file_id, localPath: path, serverHash: hash("shared"), localHash: hash("shared"), serverRevision: 1, baseText: "shared", pending: null, conflict: null,
  });
  seed();
  await baseline.save();
  let rows = [{ ...entry, status: "revoked" }];
  const api = {
    hasToken: () => true,
    async collabLeave() { calls.push(["leave"]); if (controls.apiError) throw new Error("API failure"); rows = rows.map(e => e.id === entry.id ? { ...e, status: "revoked" } : e); },
    async collabRevoke() { calls.push(["revoke"]); if (controls.apiError) throw new Error("API failure"); rows = rows.map(e => e.id === entry.id ? { ...e, status: "revoked" } : e); },
    collabList: async () => ({ collaborations: rows }),
    collabInbox: async () => ({ collaborations: [] }),
    downloadCollabContent: async () => { throw new Error("No download expected"); },
  };
  const plugin = { baseline, settings: { vaultId: "my-vault", username: "bob" }, t: key => key };
  const manager = new mod.CollabManager({ vault }, api, plugin, () => {});
  t.after(async () => {
    manager.stop();
    globalThis.window = originalWindow;
    globalThis.__ossNotices = originalNotices;
    await cleanup();
    await rm(root, { recursive: true, force: true });
  });
  return { root, mod, vault, baseline, manager, seed, put, exists, controls, calls, api, plugin, setRows: next => { rows = next; }, notices: globalThis.__ossNotices };
}

for (const action of ["leave", "revoke", "refresh"]) {
  test(`${action} deletes tracked file and all empty collaboration parents`, async (t) => {
    const f = await fixture(t);
    if (action === "refresh") await f.manager.refresh();
    else await f.manager[action](entry);
    assert.equal(f.exists("协作oss"), false);
    assert.equal(f.baseline.getCollaboration(entry.vault_id, entry.file_id), null);
    const persisted = JSON.parse(await readFile(join(f.root, ".oss-sync-state.json"), "utf8"));
    assert.deepEqual(persisted.collaborationFiles, {});
  });
}

test("missing file still cleans parents after partial directory cleanup", async (t) => {
  const f = await fixture(t);
  await unlink(join(f.root, localPath));
  await rmdir(join(f.root, "协作oss/alice/notes/topic"));
  await f.manager.leave(entry);
  assert.equal(f.exists("协作oss"), false);
});

for (const sibling of ["other.md", ".hidden", "doc_conflict.md", "attachment.png", "empty-folder/"]) {
  test(`preserves sibling ${sibling} and its parents`, async (t) => {
    const f = await fixture(t);
    const path = `协作oss/alice/notes/topic/${sibling}`;
    if (sibling.endsWith("/")) await mkdir(join(f.root, path));
    else await f.put(path, "keep");
    await f.manager.leave(entry);
    assert.equal(f.exists(localPath), false);
    assert.equal(f.exists(path), true);
    assert.equal(f.exists("协作oss/alice/notes/topic"), true);
  });
}

test("preserves other collaborators, local files and Vault root", async (t) => {
  const f = await fixture(t);
  const keep = ["协作oss/alice/other.md", "协作oss/carol/other.md", "personal.md"];
  for (const path of keep) await f.put(path, "keep");
  await f.manager.leave(entry);
  for (const path of keep) assert.equal(await readFile(join(f.root, path), "utf8"), "keep");
  assert.equal(f.exists("协作oss/alice/notes"), false);
  assert.equal(f.exists(""), true);
});

for (const failure of ["deleteError", "directoryError", "listError", "saveError"]) {
  test(`${failure} reports incomplete cleanup, retains baseline and retries after restart`, async (t) => {
    const f = await fixture(t);
    f.controls[failure] = true;
    await f.manager.leave(entry);
    assert.deepEqual(f.notices, ["collab.cleanupFailed"]);
    assert.ok(f.baseline.getCollaboration(entry.vault_id, entry.file_id));
    const reloaded = new f.mod.BaselineStore(f.vault);
    await reloaded.load();
    assert.ok(reloaded.getCollaboration(entry.vault_id, entry.file_id));
    f.controls[failure] = false;
    const restarted = new f.mod.CollabManager({ vault: f.vault }, f.api, { ...f.plugin, baseline: reloaded }, () => {});
    try { await restarted.refresh(); } finally { restarted.stop(); }
    assert.equal(f.exists("协作oss"), false);
    assert.equal(reloaded.getCollaboration(entry.vault_id, entry.file_id), null);
  });
}

test("failed remote leave preserves file and baseline without local cleanup", async (t) => {
  const f = await fixture(t);
  f.controls.apiError = true;
  await f.manager.leave(entry);
  assert.deepEqual(f.notices, ["collab.leaveFailed"]);
  assert.equal(f.exists(localPath), true);
  assert.ok(f.baseline.getCollaboration(entry.vault_id, entry.file_id));
  assert.equal(f.calls.some(([method]) => method === "delete"), false);
});

test("unsynced local edits are preserved and reported instead of deleted", async (t) => {
  const f = await fixture(t);
  await f.put(localPath, "my unsynced edit");
  await f.manager.leave(entry);
  assert.equal(await readFile(join(f.root, localPath), "utf8"), "my unsynced edit");
  assert.ok(f.baseline.getCollaboration(entry.vault_id, entry.file_id));
  assert.deepEqual(f.notices, ["collab.cleanupFailed"]);
});

test("a path without a baseline is preserved when the owner revokes", async (t) => {
  const f = await fixture(t);
  f.baseline.removeCollaboration(entry.vault_id, entry.file_id);
  await f.baseline.save();
  await f.manager.revoke(entry);
  assert.equal(f.exists(localPath), true);
  assert.equal(f.calls.some(([method]) => method === "delete"), false);
});

test("accepted collaboration at the same local path protects the shared file", async (t) => {
  const f = await fixture(t);
  const other = { ...entry, id: 2, vault_id: "another-vault", file_id: 8 };
  f.seed(other);
  await f.baseline.save();
  f.setRows([{ ...entry, status: "revoked" }, other]);
  await f.manager.refresh();
  assert.equal(f.exists(localPath), true);
  assert.equal(f.baseline.getCollaboration(entry.vault_id, entry.file_id), null);
  assert.ok(f.baseline.getCollaboration(other.vault_id, other.file_id));
});

test("a concurrent new file prevents recursive directory deletion", async (t) => {
  const f = await fixture(t);
  const keep = "协作oss/alice/notes/topic/new.md";
  f.controls.beforeRmdir = async () => { await f.put(keep, "new local file"); };
  await f.manager.leave(entry);
  assert.equal(await readFile(join(f.root, keep), "utf8"), "new local file");
  assert.equal(f.baseline.getCollaboration(entry.vault_id, entry.file_id), null);
  assert.ok(f.notices.includes("collab.left"));
});

for (const path of ["personal.md", "协作oss/../personal.md", "协作oss/alice/../../personal.md", "协作oss", "协作oss/alice", "协作oss/alice//doc.md"]) {
  test(`rejects unsafe cleanup path ${path}`, async (t) => {
    const f = await fixture(t);
    const access = new f.mod.CollaborationFileVault({ app: { vault: f.vault } });
    await assert.rejects(access.deleteLocal(path, hash("shared")), /Invalid collaboration cleanup path/);
    assert.equal(f.calls.length, 0);
  });
}

test("a directory replacing the tracked file is preserved", async (t) => {
  const f = await fixture(t);
  await unlink(join(f.root, localPath));
  await mkdir(join(f.root, localPath));
  await f.put(`${localPath}/personal.md`, "keep");
  await f.manager.leave(entry);
  assert.equal(await readFile(join(f.root, localPath, "personal.md"), "utf8"), "keep");
  assert.deepEqual(f.notices, ["collab.cleanupFailed"]);
  assert.ok(f.baseline.getCollaboration(entry.vault_id, entry.file_id));
});

test("revoke failure reports incomplete local cleanup and remote refresh retries it", async (t) => {
  const f = await fixture(t);
  f.controls.deleteError = true;
  await f.manager.revoke(entry);
  assert.deepEqual(f.notices, ["collab.cleanupFailed"]);
  assert.ok(f.baseline.getCollaboration(entry.vault_id, entry.file_id));
  f.controls.deleteError = false;
  await f.manager.refresh();
  assert.equal(f.exists("协作oss"), false);
});

function deferred() {
  let resolve;
  const promise = new Promise(r => { resolve = r; });
  return { promise, resolve };
}

test("a list request already in flight cannot recreate a file after leave", async (t) => {
  const f = await fixture(t);
  const started = deferred();
  const release = deferred();
  const originalList = f.api.collabList;
  let first = true;
  f.api.collabList = async () => {
    if (!first) return originalList();
    first = false;
    started.resolve();
    await release.promise;
    return { collaborations: [entry] };
  };
  const refreshing = f.manager.refresh();
  await started.promise;
  const leaving = f.manager.leave(entry);
  await new Promise(r => setImmediate(r));
  assert.equal(f.calls.some(([method]) => method === "leave"), false);
  release.resolve();
  await Promise.all([refreshing, leaving]);
  assert.equal(f.exists("协作oss"), false);
  assert.equal(f.baseline.getCollaboration(entry.vault_id, entry.file_id), null);
  assert.equal(f.notices.includes("collab.loadFailed"), false);
  assert.ok(f.notices.includes("collab.left"));
});

test("leave waits for a running collaboration download before cleaning its result", async (t) => {
  const f = await fixture(t);
  const started = deferred();
  const release = deferred();
  f.setRows([entry]);
  f.api.downloadCollabContent = async () => {
    started.resolve();
    await release.promise;
    const bytes = new TextEncoder().encode("shared");
    return { content: bytes.buffer, meta: { path: entry.file_path, hash: hash("shared"), revision: 1, deleted: false, type: "markdown", size: bytes.length, mtime: 1 } };
  };
  const refreshing = f.manager.refresh();
  await started.promise;
  const leaving = f.manager.leave(entry);
  await new Promise(r => setImmediate(r));
  assert.equal(f.calls.some(([method]) => method === "leave"), false);
  assert.equal(f.exists(localPath), true);
  release.resolve();
  await Promise.all([refreshing, leaving]);
  assert.equal(f.exists("协作oss"), false);
  assert.equal(f.baseline.getCollaboration(entry.vault_id, entry.file_id), null);
});

test("pending upload content is preserved even when localHash already records the edit", async (t) => {
  const f = await fixture(t);
  await f.put(localPath, "unsent pending content");
  const previous = f.baseline.getCollaboration(entry.vault_id, entry.file_id);
  f.baseline.setCollaboration(entry.vault_id, entry.file_id, {
    ...previous, localHash: hash("unsent pending content"), pending: { id: "pending-upload", createdAt: 1 },
  });
  await f.baseline.save();
  await f.manager.leave(entry);
  assert.equal(await readFile(join(f.root, localPath), "utf8"), "unsent pending content");
  assert.ok(f.baseline.getCollaboration(entry.vault_id, entry.file_id));
  assert.deepEqual(f.notices, ["collab.cleanupFailed"]);
});
