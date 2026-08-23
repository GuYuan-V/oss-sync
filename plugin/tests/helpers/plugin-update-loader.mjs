import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { pathToFileURL } from "node:url";
import { build } from "esbuild";

/** 将指定插件更新模块 bundle 到临时目录后加载，返回其导出。 */
export async function loadUpdateModule(entry) {
  const dir = await mkdtemp(join(tmpdir(), "oss-update-module-"));
  const outfile = join(dir, "update.mjs");
  await build({
    entryPoints: [entry],
    outfile,
    bundle: true,
    platform: "node",
    format: "esm",
  });
  const module = await import(pathToFileURL(outfile).href);
  return { ...module, cleanup: () => rm(dir, { recursive: true, force: true }) };
}
