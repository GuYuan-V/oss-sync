import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { build } from "esbuild";

export async function loadBaselineStore() {
  const dir = await mkdtemp(join(tmpdir(), "oss-baseline-"));
  const outfile = join(dir, "baseline.mjs");
  await build({
    entryPoints: ["src/baseline.ts"],
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
            contents: "export class TFile {}",
            loader: "js",
          }));
        },
      },
    ],
  });
  const module = await import(pathToFileURL(outfile).href);
  return {
    BaselineStore: module.BaselineStore,
    cleanup: () => rm(dir, { recursive: true, force: true }),
  };
}
