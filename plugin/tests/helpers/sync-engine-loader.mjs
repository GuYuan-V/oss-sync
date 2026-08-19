import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { build } from "esbuild";

export async function loadSyncEngine() {
  const dir = await mkdtemp(join(tmpdir(), "oss-sync-engine-"));
  const outfile = join(dir, "sync-engine.mjs");
  await build({
    entryPoints: ["src/sync-engine.ts"],
    outfile,
    bundle: true,
    platform: "node",
    format: "esm",
    plugins: [
      {
        name: "obsidian-stub",
        setup(builder) {
          builder.onResolve({ filter: /^obsidian$/ }, () => ({
            path: "obsidian",
            namespace: "obsidian-stub",
          }));
          builder.onLoad({ filter: /.*/, namespace: "obsidian-stub" }, () => ({
            contents: `
              export class App {}
              export class Vault {}
              export class Notice {}
              export class TFile {
                static [Symbol.hasInstance](value) {
                  return value?.__tfile === true;
                }
              }
              export async function requestUrl() {
                throw new Error("not implemented");
              }
            `,
            loader: "js",
          }));
        },
      },
    ],
  });
  const module = await import(pathToFileURL(outfile).href);
  return {
    SyncEngine: module.SyncEngine,
    cleanup: () => rm(dir, { recursive: true, force: true }),
  };
}
