import assert from "node:assert/strict";
import test from "node:test";
import { existsSync, statSync } from "node:fs";
import { mkdtemp, mkdir, writeFile, readFile, readdir, rmdir, unlink, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { loadModule } from "./helpers/sync-engine-loader.mjs";

async function fixture(run) {
  const root = await mkdtemp(join(tmpdir(), "oss-empty-folders-"));
  const { module, cleanup } = await loadModule("src/ordinary-sync-file-access.ts");
  const removals = [];
  const listings = [];
  const suppressed = [];
  const options = {};
  const vault = {
    getAbstractFileByPath(path) {
      const disk = join(root, path);
      if (!existsSync(disk)) return null;
      const stat = statSync(disk);
      return { __tfile: stat.isFile(), path, stat: { mtime: stat.mtimeMs, size: stat.size } };
    },
    async readBinary(file) { return Uint8Array.from(await readFile(join(root, file.path))).buffer; },
    async delete(file) { assert.ok(file.__tfile); await unlink(join(root, file.path)); },
    adapter: {
      async list(path) {
        listings.push(path);
        assert.ok(path && path !== "/");
        if (options.failList) throw new Error("list denied");
        const entries = (await readdir(join(root, path), { withFileTypes: true }))
          .filter((entry) => !options.hideDotfiles || !entry.name.startsWith("."));
        return {
          files: entries.filter((entry) => !entry.isDirectory()).map((entry) => `${path}/${entry.name}`),
          folders: entries.filter((entry) => entry.isDirectory()).map((entry) => `${path}/${entry.name}`),
        };
      },
      async rmdir(path, recursive) {
        removals.push(path);
        assert.ok(path && path !== "/");
        assert.equal(recursive, false);
        await options.beforeRmdir?.(path);
        if (options.failRemove) throw new Error("remove denied");
        await rmdir(join(root, path));
      },
    },
  };
  const access = new module.OrdinarySyncFileAccess(vault, (path) => suppressed.push(path));
  const put = async (path, content = "note") => {
    await mkdir(join(root, path, ".."), { recursive: true });
    await writeFile(join(root, path), content);
  };
  const deleteFile = async (path) => {
    const snapshot = await access.readExact(path);
    return access.deleteExact(path, snapshot ? { kind: "hash", hash: snapshot.hash } : { kind: "absent" });
  };
  try {
    await run({ root, access, options, removals, suppressed, put, deleteFile });
    assert.ok([...listings, ...removals].every((path) => path && path !== "/" && !path.split("/").includes("..")));
  }
  finally { await cleanup(); await rm(root, { recursive: true, force: true }); }
}

test("remote deletion removes only the emptied ancestor chain and preserves the Vault root", () => fixture(async ({ root, put, deleteFile, removals, suppressed }) => {
  await put("notes/nested/a.md");
  await put("notes/nested/b.md");
  await mkdir(join(root, "unrelated"));
  assert.equal((await deleteFile("notes/nested/a.md")).kind, "deleted");
  assert.ok(existsSync(join(root, "notes/nested/b.md")));
  assert.deepEqual(removals, []);
  assert.equal((await deleteFile("notes/nested/b.md")).kind, "deleted");
  assert.deepEqual(removals, ["notes/nested", "notes"]);
  assert.ok(suppressed.includes("notes/nested"));
  assert.ok(suppressed.includes("notes"));
  assert.ok(existsSync(root));
  assert.ok(existsSync(join(root, "unrelated")));
  assert.equal(existsSync(join(root, "notes")), false);
}));

test("local notes, attachments, hidden files and empty child folders prevent ancestor cleanup", async () => {
  for (const keep of ["local.md", "image.png", ".hidden", "empty-child/"]) {
    await fixture(async ({ root, put, deleteFile, removals }) => {
      await put("notes/nested/deleted.md");
      if (keep.endsWith("/")) await mkdir(join(root, "notes", keep));
      else await put(`notes/${keep}`, "local data");
      await deleteFile("notes/nested/deleted.md");
      assert.deepEqual(removals, ["notes/nested"]);
      assert.ok(existsSync(join(root, "notes", keep)));
      if (!keep.endsWith("/")) assert.equal(await readFile(join(root, "notes", keep), "utf8"), "local data");
    });
  }
});

test("non-recursive removal protects hidden content omitted from the directory listing", () => fixture(async ({ root, put, deleteFile, options, removals }) => {
  await put("notes/deleted.md");
  await put("notes/.hidden", "hidden data");
  options.hideDotfiles = true;
  await deleteFile("notes/deleted.md");
  assert.deepEqual(removals, ["notes"]);
  assert.equal(await readFile(join(root, "notes/.hidden"), "utf8"), "hidden data");
}));

test("a concurrent local write makes directory removal fail without losing the new file", () => fixture(async ({ root, put, deleteFile, options, removals }) => {
  await put("notes/nested/deleted.md");
  options.beforeRmdir = async (path) => { await put(`${path}/new.md`, "new local data"); };
  await deleteFile("notes/nested/deleted.md");
  assert.deepEqual(removals, ["notes/nested"]);
  assert.equal(await readFile(join(root, "notes/nested/new.md"), "utf8"), "new local data");
}));

test("cleanup failure does not turn a completed file deletion into a sync failure", async () => {
  for (const failure of ["failList", "failRemove"]) {
    await fixture(async ({ root, put, deleteFile, options }) => {
      await put("notes/deleted.md");
      options[failure] = true;
      assert.equal((await deleteFile("notes/deleted.md")).kind, "deleted");
      assert.equal(existsSync(join(root, "notes/deleted.md")), false);
      assert.ok(existsSync(join(root, "notes")));
    });
  }
});

test("stale deletion preserves the file and skips cleanup, while absent retries clean empty parents", () => fixture(async ({ root, access, put, removals, deleteFile }) => {
  await put("notes/deleted.md", "local changes");
  assert.equal((await access.deleteExact("notes/deleted.md", { kind: "hash", hash: "old" })).kind, "stale");
  assert.deepEqual(removals, []);
  assert.equal(await readFile(join(root, "notes/deleted.md"), "utf8"), "local changes");
  await unlink(join(root, "notes/deleted.md"));
  await deleteFile("notes/deleted.md");
  assert.deepEqual(removals, ["notes"]);
  await put("root.md");
  await deleteFile("root.md");
  assert.deepEqual(removals, ["notes"]);
  assert.ok(existsSync(root));
}));
