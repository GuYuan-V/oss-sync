import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { build } from "esbuild";
import { existsSync } from "node:fs";
function resolveEntry(e) { const alt = join("plugin", e); return existsSync(e) ? e : existsSync(alt) ? alt : e; }
const OBSIDIAN_STUB = `export class TFile{constructor(p,c){this.path=p;this._content=c;this.stat={mtime:1,size:c.length}}static [Symbol.hasInstance](i){return !!i&&typeof i.path==="string"&&"_content"in i&&"stat"in i}}export class Notice{constructor(m){globalThis.__ossNotices=globalThis.__ossNotices||[];globalThis.__ossNotices.push(m)}}export const App=class{}`;
export async function loadRemoteModules() {
  const dir = await mkdtemp(join(tmpdir(), "oss-remote-"));
  const out = join(dir, "bundle.mjs");
  await build({ entryPoints: [resolveEntry("src/collaboration-remote-sync.ts")], outfile: out, bundle: true, platform: "node", format: "esm", plugins: [{ name: "obsidian-stub", setup(b) { b.onResolve({ filter: /^obsidian$/ }, () => ({ path: "obsidian", namespace: "obsidian-stub" })); b.onLoad({ filter: /.*/, namespace: "obsidian-stub" }, () => ({ contents: OBSIDIAN_STUB, loader: "js" })); } }] });
  const mod = await import(pathToFileURL(out).href);
  return { mod, cleanup: () => rm(dir, { recursive: true, force: true }) };
}
function makeCoordinator(mod, explicit) {
  if (explicit) return explicit;
  return new mod.CollaborationSyncCoordinator();
}
export function createRemoteSync(mod, { baseline, vault, api, onChange = () => {}, now = () => 1700000000000, createOperationID = () => "op-fixed-001", getAccepted, getUsername = () => "collab-user", coordinator } = {}) {
  const plugin = { t: (k) => k };
  const coord = makeCoordinator(mod, coordinator);
  return new mod.CollaborationRemoteSync({ baseline, vault, api, plugin, getAccepted: getAccepted ?? (() => [{ vaultId: "vault-1", fileId: 42, localPath: "协作oss/owner/Shared.md" }]), getUsername, now, createOperationID, onChange, coordinator: coord });
}
export async function sha256Hex(text) {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(text));
  return Array.from(new Uint8Array(digest)).map((b) => b.toString(16).padStart(2, "0")).join("");
}
