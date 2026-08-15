import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { build } from "esbuild";

export async function loadApiClient() {
  const dir = await mkdtemp(join(tmpdir(), "oss-api-client-"));
  const outfile = join(dir, "api.mjs");
  await build({
    entryPoints: ["src/api.ts"],
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
              export async function requestUrl(options) {
                globalThis.__ossRequests.push(options);
                if (globalThis.__ossResponse === undefined) {
                  throw new Error("unexpected request");
                }
                return globalThis.__ossResponse;
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
    OSSApiClient: module.OSSApiClient,
    cleanup: () => rm(dir, { recursive: true, force: true }),
  };
}
