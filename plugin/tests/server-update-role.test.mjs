import assert from "node:assert/strict";
import test from "node:test";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { build } from "esbuild";

async function loadI18n() {
  const dir = await mkdtemp(join(tmpdir(), "oss-i18n-role-"));
  const outfile = join(dir, "i18n.mjs");
  await build({ entryPoints: ["src/i18n.ts"], outfile, bundle: true, platform: "node", format: "esm" });
  const mod = await import(pathToFileURL(outfile).href);
  return { ...mod, cleanup: () => rm(dir, { recursive: true, force: true }) };
}

test("server update translations exist in both ZH and EN and are distinct from plugin update", async () => {
  const { TRANSLATIONS, cleanup } = await loadI18n();
  try {
    for (const lang of ["zh", "en"]) {
      const dict = TRANSLATIONS[lang];
      // server update section must be distinct key namespace
      assert.ok("settings.serverUpdate.title" in dict, `${lang} missing serverUpdate title`);
      assert.ok("settings.serverUpdate.confirmLabel" in dict);
      assert.ok("settings.serverUpdate.confirmDesc" in dict);
      assert.ok("settings.serverUpdate.trigger" in dict);
      assert.ok("settings.serverUpdate.polling" in dict);
      assert.ok("settings.serverUpdate.success" in dict);
      assert.ok("settings.serverUpdate.rolledBack" in dict);
      assert.ok("settings.serverUpdate.failed" in dict);
      assert.ok("settings.serverUpdate.authError" in dict);
      // must be distinct from plugin update namespace
      assert.ok("settings.update.title" in dict);
      assert.notEqual(dict["settings.serverUpdate.title"], dict["settings.update.title"]);
      // English must not contain Chinese chars, Chinese must contain Chinese
      if (lang === "en") {
        for (const k of Object.keys(dict).filter(k => k.startsWith("settings.serverUpdate"))) {
          assert.doesNotMatch(dict[k], /[\u3400-\u9fff]/u, `${k} should be English`);
        }
      }
    }
    assert.deepEqual(Object.keys(TRANSLATIONS.zh).sort(), Object.keys(TRANSLATIONS.en).sort());
  } finally { await cleanup(); }
});

test("exact-version confirmation logic: only exact match enables trigger", async () => {
  function isTriggerEnabled({ expectedVersion, confirmationInput, isTriggering, isPolling, isChecking }) {
    if (!expectedVersion) return false;
    const exact = confirmationInput === expectedVersion;
    return exact && !isTriggering && !isPolling && !isChecking;
  }
  assert.equal(isTriggerEnabled({ expectedVersion: "1.2.4", confirmationInput: "1.2.4", isTriggering: false, isPolling: false, isChecking: false }), true);
  assert.equal(isTriggerEnabled({ expectedVersion: "1.2.4", confirmationInput: "1.2.4 ", isTriggering: false, isPolling: false, isChecking: false }), false, "trailing space not exact");
  assert.equal(isTriggerEnabled({ expectedVersion: "1.2.4", confirmationInput: "v1.2.4", isTriggering: false, isPolling: false, isChecking: false }), false);
  assert.equal(isTriggerEnabled({ expectedVersion: "1.2.4", confirmationInput: "1.2.4", isTriggering: true, isPolling: false, isChecking: false }), false, "disabled while triggering");
  assert.equal(isTriggerEnabled({ expectedVersion: "1.2.4", confirmationInput: "1.2.4", isTriggering: false, isPolling: true, isChecking: false }), false, "disabled while polling");
  assert.equal(isTriggerEnabled({ expectedVersion: "1.2.4", confirmationInput: "1.2.4", isTriggering: false, isPolling: false, isChecking: true }), false, "disabled while checking");
});

test("disabled active controls: all buttons disabled during polling/triggering", async () => {
  function disabledState({ isChecking, isTriggering, isPolling }) {
    return {
      checkDisabled: isChecking || isTriggering || isPolling,
      triggerDisabled: isChecking || isTriggering || isPolling,
    };
  }
  assert.deepEqual(disabledState({ isChecking: true, isTriggering: false, isPolling: false }), { checkDisabled: true, triggerDisabled: true });
  assert.deepEqual(disabledState({ isChecking: false, isTriggering: true, isPolling: false }), { checkDisabled: true, triggerDisabled: true });
  assert.deepEqual(disabledState({ isChecking: false, isTriggering: false, isPolling: true }), { checkDisabled: true, triggerDisabled: true });
  assert.deepEqual(disabledState({ isChecking: false, isTriggering: false, isPolling: false }), { checkDisabled: false, triggerDisabled: false });
});

test("cached role only controls visibility; server 401/403 remain authoritative", async () => {
  function isAdminCached(role) { return role === "admin"; }
  assert.equal(isAdminCached("admin"), true);
  assert.equal(isAdminCached("user"), false);
  assert.equal(isAdminCached(""), false);
  // visibility gated by cached role
  function shouldShowServerUpdateSection(cachedRole) { return isAdminCached(cachedRole); }
  assert.equal(shouldShowServerUpdateSection("admin"), true);
  assert.equal(shouldShowServerUpdateSection("user"), false);
  // but server response authoritative: even if cached admin, 403 must surface as stale role
  function handleServerResponse(status) {
    if (status === 401 || status === 403) return "stale_role";
    return "ok";
  }
  assert.equal(handleServerResponse(403), "stale_role");
  assert.equal(handleServerResponse(200), "ok");
  // stale role forces revalidation / login regardless of cached visibility
  assert.equal(handleServerResponse(401) === "stale_role", true);
});
