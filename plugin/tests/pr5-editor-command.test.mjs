import assert from "node:assert/strict";
import test from "node:test";
import { loadMain } from "./helpers/plugin-main-loader.mjs";

for (const scenario of ["unchanged", "edited", "switched", "renamed", "failed"]) {
  test(`server command preserves document state: ${scenario}`, async () => {
    const { module, cleanup } = await loadMain();
    try {
      const plugin = new module.default({}, { id: "oss-sync", version: "0.1.0" });
      let resolve, reject;
      plugin.api.runServerPluginHook = () => new Promise((yes, no) => { resolve = yes; reject = no; });
      let text = "initial document";
      let writes = 0;
      const editor = { getValue: () => text, setValue: value => { text = value; writes++; } };
      const view = { file: { path: "first.md" } };
      const pending = plugin.runEditorPluginCommand("format-plugin", { id: "format", name: "editor.command" }, editor, view);
      if (scenario === "edited") text += "\nnew user edit";
      if (scenario === "switched") view.file = { path: "second.md" };
      if (scenario === "renamed") view.file.path = "renamed.md";
      if (scenario === "failed") reject(new Error("request failed"));
      else resolve({ content: "FORMATTED INITIAL DOCUMENT" });
      await pending;
      assert.equal(writes, scenario === "unchanged" ? 1 : 0);
      if (scenario === "unchanged") assert.equal(text, "FORMATTED INITIAL DOCUMENT");
      if (scenario === "edited") assert.match(text, /new user edit/);
    } finally { await cleanup(); }
  });
}
