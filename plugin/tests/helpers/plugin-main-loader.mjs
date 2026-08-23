// Derived from existing esbuild obsidian loaders; bundles plugin/src/main.ts with an expanded Obsidian stub.
import { mkdtemp, rm } from "node:fs/promises";
import { existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { pathToFileURL } from "node:url";
import { build } from "esbuild";

const OBSIDIAN_STUB = `
  export class Component {}
  export class Plugin extends Component {
    constructor(app, manifest) {
      super();
      this.app = app;
      this.manifest = manifest;
      this._data = null;
    }
    async loadData() { return this._data; }
    async saveData(data) { this._data = data; }
    addRibbonIcon(icon, title, cb) {
      const el = new globalThis.FakeElement("ribbon");
      return el;
    }
    addStatusBarItem() {
      const el = new globalThis.FakeElement("status");
      el.empty = el.empty.bind(el);
      el.addClass = el.addClass.bind(el);
      el.removeClass = el.removeClass.bind(el);
      el.createSpan = () => new globalThis.FakeElement("span");
      el.onClickEvent = () => {};
      el.setText = el.setText.bind(el);
      return el;
    }
    registerView() {}
    registerEvent() {}
    addSettingTab() {}
    addCommand(cmd) { return cmd; }
  }
  export class App {}
  export class Notice {
    constructor(message) { globalThis.__ossNotices?.push(message); }
  }
  export class TAbstractFile {}
  export class TFile {}
  export class TFolder {}
  export class Vault {
    static recurseChildren() {}
  }
  export class Modal {
    constructor(app) {
      this.app = app;
      this.modalEl = new globalThis.FakeElement("modal");
      this.titleEl = new globalThis.FakeElement("h1");
      this.contentEl = new globalThis.FakeElement("div");
    }
    open() {}
    close() {}
  }
  export class Setting {
    constructor(containerEl) { this.containerEl = containerEl; }
    setName() { return this; }
    setDesc() { return this; }
    addToggle(cb) { if (cb) cb({ setValue(){return this;}, onChange(){return this;}}); return this; }
    addText(cb) { if (cb) cb({ setPlaceholder(){return this;}, setValue(){return this;}, onChange(){return this;}}); return this; }
    addButton(cb) { if (cb) cb({ setButtonText(){return this;}, onClick(){return this;}}); return this; }
  }
  export class PluginSettingTab {
    constructor(app, plugin) { this.app = app; this.plugin = plugin; this.containerEl = new globalThis.FakeElement("div"); }
    display() {}
    hide() {}
  }
  export class ItemView {
    constructor(leaf) { this.leaf = leaf; this.containerEl = leaf.containerEl; this.contentEl = leaf.contentEl; }
  }
  export class WorkspaceLeaf {}
  export class ButtonComponent {}
  export function getLanguage() { return "en"; }
  export async function requestUrl() {
    globalThis.__ossRequests?.push(arguments);
    return { status: 200, json: {}, text: "", arrayBuffer: new ArrayBuffer(0), headers: {} };
  }
  export function setIcon() {}
`;

function obsidianStubPlugin() {
  return {
    name: "obsidian-stub",
    setup(builder) {
      builder.onResolve({ filter: /^obsidian$/ }, () => ({
        path: "obsidian",
        namespace: "obsidian-stub",
      }));
      builder.onLoad({ filter: /.*/, namespace: "obsidian-stub" }, () => ({
        contents: OBSIDIAN_STUB,
        loader: "js",
      }));
    },
  };
}

export async function loadMain() {
  const dir = await mkdtemp(join(tmpdir(), "oss-main-"));
  const outfile = join(dir, "bundle.mjs");
  const candidateA = resolve("plugin/src/main.ts");
  const candidateB = resolve("src/main.ts");
  const entry = existsSync(candidateA) ? candidateA : candidateB;
  try {
    await build({
      entryPoints: [entry],
      outfile,
      bundle: true,
      platform: "node",
      format: "esm",
      plugins: [obsidianStubPlugin()],
    });
    const module = await import(pathToFileURL(outfile).href);
    return { module, cleanup: () => rm(dir, { recursive: true, force: true }) };
  } catch (error) {
    await rm(dir, { recursive: true, force: true });
    throw error;
  }
}
