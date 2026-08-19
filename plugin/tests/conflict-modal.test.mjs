import assert from "node:assert/strict";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { pathToFileURL } from "node:url";
import { build } from "esbuild";

class FakeTextNode {
  constructor(content) {
    this.content = content;
  }
}

class FakeElement {
  constructor(tag = "div") {
    this.tag = tag;
    this.children = [];
    this.classes = new Set();
    this.text = "";
    this.style = {};
    return new Proxy(this, {
      set(target, property, value, receiver) {
        if (property === "inner" + "HTML") {
          throw new Error("unsafe markup writes are forbidden");
        }
        return Reflect.set(target, property, value, receiver);
      },
    });
  }

  createDiv(options = {}) {
    return this.createEl("div", options);
  }

  createSpan(options = {}) {
    return this.createEl("span", options);
  }

  createEl(tag, options = {}) {
    const element = new FakeElement(tag);
    if (options.cls) element.addClass(...String(options.cls).split(/\s+/));
    if (options.text !== undefined) element.setText(options.text);
    this.children.push(element);
    return element;
  }

  addClass(...names) {
    for (const name of names) this.classes.add(name);
  }

  empty() {
    this.children = [];
  }

  setText(text) {
    this.text = text;
  }

  appendChild(node) {
    this.children.push(node);
    return node;
  }

  get visibleText() {
    if (this.children.length === 0) return this.text || "";
    let result = "";
    for (const child of this.children) {
      if (child instanceof FakeTextNode) {
        result += child.content;
      } else if (child instanceof FakeElement && child.tag !== "wbr") {
        result += child.visibleText;
      }
    }
    return result;
  }

  hasClass(name) {
    return this.classes.has(name);
  }
}

function findByClass(element, className) {
  const matches = [];
  for (const child of element.children) {
    if (child instanceof FakeTextNode) continue;
    if (child.hasClass(className)) matches.push(child);
    matches.push(...findByClass(child, className));
  }
  return matches;
}

async function loadConflictModal() {
  const dir = await mkdtemp(join(tmpdir(), "oss-conflict-modal-"));
  const outfile = join(dir, "conflict-modal.mjs");
  // Provide a minimal document stub so production code can call createTextNode.
  globalThis.document = { createTextNode: (t) => new FakeTextNode(t) };
  await build({
    entryPoints: ["src/conflict-modal.ts"],
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
              export class Notice { constructor(message) { this.message = message; } }
              export class Setting {
                constructor(container) { container.createDiv({ cls: "setting" }); }
                setName() { return this; }
                setDesc() { return this; }
                setHeading() { return this; }
                addButton(callback) {
                  callback({ setButtonText() { return this; }, setWarning() { return this; }, onClick() { return this; } });
                  return this;
                }
              }
              export class TFile {}
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
            `,
            loader: "js",
          }));
        },
      },
    ],
  });
  globalThis.FakeElement = FakeElement;
  const module = await import(pathToFileURL(outfile).href);
  return {
    ConflictModal: module.ConflictModal,
    cleanup: () => rm(dir, { recursive: true, force: true }),
  };
}

function pluginWithTranslations() {
  return {
    sidebarView: { refresh() {} },
    t(key, params = {}) {
      if (key === "conflict.omittedLines") return `omitted ${params.count}`;
      if (key === "conflict.title") return `Conflict: ${params.path}`;
      return key;
    },
  };
}

function createModal(ConflictModal, local, remote) {
  return new ConflictModal(
    { vault: { read: async () => local } },
    pluginWithTranslations(),
    {},
    { path: "Notes/A.md" },
    remote,
    async () => {}
  );
}

test("renders structured conflict rows without HTML and marks the modal", async () => {
  const { ConflictModal, cleanup } = await loadConflictModal();
  try {
    const local = ["0", "1", "2", "3", "4", "old"].join("\n");
    const remote = ["0", "1", "2", "3", "4", "new"].join("\n");
    const modal = createModal(ConflictModal, local, remote);

    await modal.onOpen();

    assert.ok(modal.modalEl.hasClass("oss-conflict-modal"));
    const preview = findByClass(modal.contentEl, "oss-diff-preview")[0];
    assert.ok(preview, "preview is rendered");
    assert.deepEqual(
      findByClass(preview, "oss-diff-marker").map((element) => element.text),
      ["", "", "-", "+"],
      "context, removed, and added rows retain structural markers"
    );
    assert.equal(findByClass(preview, "is-omitted")[0].text, "omitted 3");
  } finally {
    await cleanup();
  }
});

test("renders the no-change state while retaining resolution settings", async () => {
  const { ConflictModal, cleanup } = await loadConflictModal();
  try {
    const modal = createModal(ConflictModal, "same", "same");

    await modal.onOpen();

    assert.equal(findByClass(modal.contentEl, "oss-diff-empty")[0].text, "conflict.noChanges");
    assert.equal(findByClass(modal.contentEl, "setting").length, 5);
  } finally {
    await cleanup();
  }
});

test("inserts WBR before forward-slash and backslash path separators in diff text and preserves visible text", async () => {
  const { ConflictModal, cleanup } = await loadConflictModal();
  try {
    const local = [
      "# Projects/architecture.md",
      "# src\\utils\\helper.ts",
      "Content old",
    ].join("\n");
    const remote = [
      "# Projects/architecture.md",
      "# src\\utils\\helper.ts",
      "Content new",
    ].join("\n");
    const modal = createModal(ConflictModal, local, remote);

    await modal.onOpen();

    assert.equal(modal.titleEl.visibleText, "Conflict: Notes/A.md", "title text is preserved");
    assert.deepEqual(
      describeChildren(modal.titleEl),
      ["text:Conflict: Notes", "wbr", "text:/A.md"],
      "title receives the same semantic path break"
    );

    const preview = findByClass(modal.contentEl, "oss-diff-preview")[0];
    assert.ok(preview, "preview is rendered");

    // Verify diff structure is intact: one removed, one added, two context rows
    assert.deepEqual(
      findByClass(preview, "oss-diff-marker").map((el) => el.text),
      ["", "", "-", "+"],
      "markers unchanged"
    );

    const textSpans = findByClass(preview, "oss-diff-text");
    assert.equal(textSpans.length, 4, "four diff text spans");

    // Forward-slash path: "Projects/architecture.md"
    const fwdSpan = textSpans.find((s) => s.visibleText.includes("Projects/architecture.md"));
    assert.ok(fwdSpan, "forward-slash span exists");
    assert.deepEqual(
      describeChildren(fwdSpan),
      ["text:# Projects", "wbr", "text:/architecture.md"],
      "WBR appears immediately before the forward slash"
    );
    assert.equal(
      fwdSpan.visibleText,
      "# Projects/architecture.md",
      "forward-slash visible text matches original byte-for-byte"
    );

    // Backslash path: "src\utils\helper.ts"
    const bwdSpan = textSpans.find((s) => s.visibleText.includes("src\\utils\\helper.ts"));
    assert.ok(bwdSpan, "backslash span exists");
    assert.deepEqual(
      describeChildren(bwdSpan),
      ["text:# src", "wbr", "text:\\utils", "wbr", "text:\\helper.ts"],
      "WBR appears immediately before every backslash"
    );
    assert.equal(
      bwdSpan.visibleText,
      "# src\\utils\\helper.ts",
      "backslash visible text matches original byte-for-byte"
    );

    // Verify textContent (non-WBR text) is identical to original
    const composed = textSpans.map((s) => s.visibleText).join("");
    assert.ok(
      composed.includes("# Projects/architecture.md") && composed.includes("# src\\utils\\helper.ts"),
      "visible text composition preserves both separator types"
    );
  } finally {
    await cleanup();
  }
});

function describeChildren(element) {
  return element.children.map((child) =>
    child instanceof FakeTextNode ? `text:${child.content}` : child.tag
  );
}

test("closing the conflict modal empties content and refreshes the sidebar exactly once", async () => {
  const { ConflictModal, cleanup } = await loadConflictModal();
  try {
    let refreshCalls = 0;
    const plugin = { sidebarView: { refresh() { refreshCalls += 1; } } };
    const modal = new ConflictModal({}, plugin, {}, { path: "Notes/A.md" }, "remote content", async () => {});
    let emptyCalls = 0;
    modal.contentEl.empty = () => { emptyCalls += 1; };

    modal.onClose();

    assert.equal(emptyCalls, 1, "closing must clear the modal content");
    assert.equal(refreshCalls, 1, "closing must refresh the right sidebar exactly once");
  } finally {
    await cleanup();
  }
});
