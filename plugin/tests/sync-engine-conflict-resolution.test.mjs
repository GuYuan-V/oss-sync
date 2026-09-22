import assert from "node:assert/strict";
import test from "node:test";
import {
  createConflictFixture,
  deferred,
  enc,
} from "./helpers/sync-engine-conflict-resolution-fixture.mjs";

for (const resolution of ["accept_remote", "force_local", "keep_both"]) {
  test(`attachment ${resolution} preserves exact binary bytes`, async () => {
    const localBytes = new Uint8Array([137, 80, 78, 71, 0, 255, 1]);
    const remoteBytes = new Uint8Array([137, 80, 78, 71, 0, 254, 2]);
    const fixture = await createConflictFixture({ localBytes, remoteBytes, path: "附件/image.png", type: "attachment" });
    try {
      const uploads = [];
      fixture.api.downloadV2 = async () => ({ content: remoteBytes.slice().buffer, meta: fixture.remote });
      fixture.api.uploadV2 = async (_vault, input) => {
        uploads.push(new Uint8Array(input.content));
        return { ...fixture.remote, hash: fixture.state.conflict().localHash, revision: 10 };
      };
      await fixture.engine.resolveConflict(fixture.path, resolution);
      assert.equal(fixture.state.conflict(), null);
      assert.deepEqual(fixture.state.vault.bytes(fixture.path), resolution === "force_local" ? localBytes : remoteBytes);
      if (resolution === "force_local") assert.deepEqual(uploads, [localBytes]);
      if (resolution === "keep_both") {
        const sibling = fixture.state.vault.paths().find((path) => path !== fixture.path);
        assert.deepEqual(fixture.state.vault.bytes(sibling), localBytes);
        assert.deepEqual(fixture.state.pending(), [{ kind: "upsert", path: sibling }]);
      }
    } finally { await fixture.cleanupFixture(); }
  });
}

test("upload 409 for identical bytes from a second device converges without a conflict or sibling", async () => {
  const content = new Uint8Array([137, 80, 78, 71, 0, 255]);
  const fixture = await createConflictFixture({ localBytes: content, remoteBytes: content, path: "附件/image.png", type: "attachment" });
  try {
    fixture.engine.dismissConflict(fixture.path);
    let attempts = 0;
    fixture.api.uploadV2 = async () => {
      attempts++;
      throw Object.assign(new Error("revision conflict"), { status: 409, current: fixture.remote });
    };
    fixture.api.downloadV2 = async () => ({ content: content.slice().buffer, meta: fixture.remote });
    const outcome = await fixture.engine.applyAction("vault-1", {
      kind: "upload", path: fixture.path, baseRevision: 0, operationID: "device-b-upload",
      local: { path: fixture.path, hash: fixture.remote.hash, size: content.length, mtime: 10 },
    });
    assert.equal(outcome.kind, "resolved");
    assert.equal(attempts, 1);
    assert.equal(fixture.state.conflict(), null);
    assert.deepEqual(fixture.state.vault.paths(), [fixture.path]);
    assert.deepEqual(fixture.state.vault.bytes(fixture.path), content);
    assert.deepEqual(fixture.state.pending(), []);
  } finally { await fixture.cleanupFixture(); }
});

test("identical persisted attachment conflict does not create another copy", async () => {
  const content = new Uint8Array([137, 80, 78, 71, 0, 255]);
  const fixture = await createConflictFixture({ localBytes: content, remoteBytes: content, path: "image.png", type: "attachment" });
  try {
    fixture.api.downloadV2 = async () => ({ content: content.slice().buffer, meta: fixture.remote });
    await fixture.engine.resolveConflict(fixture.path, "keep_both");
    assert.equal(fixture.state.conflict(), null);
    assert.deepEqual(fixture.state.vault.paths(), [fixture.path]);
    assert.deepEqual(fixture.state.pending(), []);
  } finally { await fixture.cleanupFixture(); }
});

test("accept remote refreshes obsolete attachment revision after a 409", async () => {
  const fixture = await createConflictFixture({ localBytes: enc("local"), remoteBytes: enc("remote"), path: "image.png", type: "attachment" });
  try {
    const current = { ...fixture.remote, revision: 10 };
    fixture.api.downloadV2 = async () => { throw Object.assign(new Error("changed"), { status: 409, current }); };
    await assert.rejects(fixture.engine.resolveConflict(fixture.path, "accept_remote"));
    assert.equal(fixture.state.conflict().remoteRevision, 10);
    assert.deepEqual(fixture.state.vault.bytes(fixture.path), enc("local"));
  } finally { await fixture.cleanupFixture(); }
});

test("resolveConflict force-local preserves an upload queued for a local change during the request", async () => {
  const localBytes = enc("local before force");
  const freshBytes = enc("local changed during force");
  const fixture = await createConflictFixture({ localBytes, remoteBytes: localBytes });
  try {
    const requestStarted = deferred();
    const response = deferred();
    const uploads = [];
    fixture.api.uploadV2 = async (_vaultID, input) => {
      uploads.push(new Uint8Array(input.content));
      fixture.events.push("upload");
      requestStarted.resolve();
      return response.promise;
    };

    const resolution = fixture.engine.resolveConflict(fixture.path, "force_local");
    await requestStarted.promise;

    assert.ok(
      fixture.events.indexOf(`remove-pending:${fixture.path}`) >= 0 &&
        fixture.events.indexOf(`remove-pending:${fixture.path}`) < fixture.events.indexOf("upload"),
      "obsolete pending work is cleared before the force upload starts",
    );
    fixture.state.vault.setFile(fixture.path, freshBytes);
    response.resolve(fixture.remote);
    await resolution;

    assert.deepEqual(uploads, [localBytes]);
    assert.deepEqual(fixture.state.vault.bytes(fixture.path), freshBytes);
    assert.deepEqual(fixture.state.pending(), [{ kind: "upsert", path: fixture.path }]);
    assert.equal(fixture.state.conflict(), null);
    assert.deepEqual(fixture.saves.at(-1), [{ kind: "upsert", path: fixture.path }]);
  } finally {
    await fixture.cleanupFixture();
  }
});

test("resolveConflict force-local preserves a delete queued when the local file disappears during the request", async () => {
  const localBytes = enc("local before force");
  const fixture = await createConflictFixture({ localBytes, remoteBytes: localBytes });
  try {
    const requestStarted = deferred();
    const response = deferred();
    fixture.api.uploadV2 = async () => {
      fixture.events.push("upload");
      requestStarted.resolve();
      return response.promise;
    };

    const resolution = fixture.engine.resolveConflict(fixture.path, "force_local");
    await requestStarted.promise;
    fixture.state.vault.removeFile(fixture.path);
    response.resolve(fixture.remote);
    await resolution;

    assert.equal(fixture.state.vault.bytes(fixture.path), null);
    assert.deepEqual(fixture.state.pending(), [{ kind: "delete", path: fixture.path }]);
    assert.equal(fixture.state.conflict(), null);
    assert.deepEqual(fixture.saves.at(-1), [{ kind: "delete", path: fixture.path }]);
  } finally {
    await fixture.cleanupFixture();
  }
});

test("resolveConflict accept-remote keeps a local upsert created after remote deletion", async () => {
  const localBytes = enc("local before remote delete");
  const appearedBytes = enc("local appeared after remote delete");
  const fixture = await createConflictFixture({ localBytes, remoteBytes: enc(""), remoteDeleted: true });
  try {
    fixture.state.vault.afterDelete((path) => fixture.state.vault.setFile(path, appearedBytes));

    await fixture.engine.resolveConflict(fixture.path, "accept_remote");

    assert.deepEqual(fixture.state.vault.bytes(fixture.path), appearedBytes);
    assert.deepEqual(fixture.state.pending(), [{ kind: "upsert", path: fixture.path }]);
    assert.equal(fixture.state.conflict(), null);
    assert.deepEqual(fixture.saves.at(-1), [{ kind: "upsert", path: fixture.path }]);
  } finally {
    await fixture.cleanupFixture();
  }
});

test("resolveConflict keep-both saves the sibling before downloading and retains it after remote application", async () => {
  const localBytes = enc("local conflict copy");
  const remoteBytes = enc("remote canonical");
  const fixture = await createConflictFixture({ localBytes, remoteBytes });
  try {
    const downloadStarted = deferred();
    const response = deferred();
    fixture.api.downloadV2 = async () => {
      fixture.events.push("download");
      downloadStarted.resolve();
      return response.promise;
    };

    const resolution = fixture.engine.resolveConflict(fixture.path, "keep_both");
    await downloadStarted.promise;

    const siblingPath = fixture.state.vault.paths().find((path) => path !== fixture.path);
    assert.ok(siblingPath, "sibling exists before remote download resolves");
    assert.deepEqual(fixture.state.vault.bytes(siblingPath), localBytes);
    assert.deepEqual(fixture.state.pending(), [{ kind: "upsert", path: siblingPath }]);
    assert.ok(
      fixture.events.indexOf(`save:upsert:${siblingPath}`) < fixture.events.indexOf("download"),
      "the sibling upsert is durably saved before remote canonical work starts",
    );
    response.resolve({ content: remoteBytes.slice().buffer, meta: fixture.remote });
    await resolution;

    assert.deepEqual(fixture.state.vault.bytes(fixture.path), remoteBytes);
    assert.deepEqual(fixture.state.vault.bytes(siblingPath), localBytes);
    assert.deepEqual(fixture.state.pending(), [{ kind: "upsert", path: siblingPath }]);
    assert.equal(fixture.state.conflict(), null);
  } finally {
    await fixture.cleanupFixture();
  }
});

test("resolveConflict keep-both retains the saved sibling and conflict when remote canonical application is stale", async () => {
  const localBytes = enc("local conflict copy");
  const competingBytes = enc("local competing change");
  const remoteBytes = enc("remote canonical");
  const fixture = await createConflictFixture({ localBytes, remoteBytes });
  try {
    fixture.api.downloadV2 = async () => {
      fixture.events.push("download");
      fixture.state.vault.setFile(fixture.path, competingBytes);
      return { content: remoteBytes.slice().buffer, meta: fixture.remote };
    };

    await assert.rejects(
      fixture.engine.resolveConflict(fixture.path, "keep_both"),
      /sync\.conflictNotFound/,
    );

    const siblingPath = fixture.state.vault.paths().find((path) => path !== fixture.path);
    assert.ok(siblingPath, "sibling exists after canonical application is rejected as stale");
    assert.deepEqual(fixture.state.vault.bytes(fixture.path), competingBytes);
    assert.deepEqual(fixture.state.vault.bytes(siblingPath), localBytes);
    assert.deepEqual(fixture.state.pending(), [{ kind: "upsert", path: siblingPath }]);
    assert.ok(fixture.state.conflict(), "the unresolved conflict remains available to the modal");
    assert.ok(
      fixture.events.indexOf(`save:upsert:${siblingPath}`) < fixture.events.indexOf("download"),
      "the sibling was saved before the stale remote attempt",
    );
  } finally {
    await fixture.cleanupFixture();
  }
});
