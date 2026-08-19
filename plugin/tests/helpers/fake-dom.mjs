// 最小可观察假 DOM：暴露 children / classes / attrs / listeners，供视图测试使用。
export class FakeElement {
  constructor(tag = "div") {
    this.tag = tag;
    this.children = [];
    this.classes = new Set();
    this.attrs = {};
    this.listeners = new Map();
    this.text = "";
  }

  createDiv(options = {}) {
    return this.createEl("div", options);
  }

  createEl(tag, options = {}) {
    const el = new FakeElement(tag);
    if (options.cls) {
      const names = Array.isArray(options.cls) ? options.cls : String(options.cls).split(/\s+/);
      el.addClass(...names);
    }
    if (options.text !== undefined) el.text = options.text;
    if (options.href !== undefined) el.attrs.href = options.href;
    if (options.attr) {
      for (const [name, value] of Object.entries(options.attr)) el.attrs[name] = value;
    }
    this.children.push(el);
    return el;
  }

  empty() {
    this.children = [];
  }

  addClass(...names) {
    for (const name of names) this.classes.add(name);
  }

  removeClass(...names) {
    for (const name of names) this.classes.delete(name);
  }

  setText(text) {
    this.text = text;
  }

  setAttribute(name, value) {
    this.attrs[name] = value;
  }

  addEventListener(type, listener) {
    const listeners = this.listeners.get(type) ?? [];
    listeners.push(listener);
    this.listeners.set(type, listeners);
  }

  click() {
    for (const listener of this.listeners.get("click") ?? []) listener();
  }

  hasClass(name) {
    return this.classes.has(name);
  }

  /** 自身与全部后代的文本拼接（仅用于路径等数据断言）。 */
  getText() {
    return this.text + this.children.map((child) => child.getText()).join("");
  }

  /** 收集与空格分隔选择器（如 ".oss-sidebar-conflict button"）匹配的后代。 */
  querySelectorAll(selector) {
    const tokens = selector.trim().split(/\s+/).filter(Boolean);
    const matches = [];
    const tokenMatches = (el, token) =>
      token.startsWith(".") ? el.hasClass(token.slice(1)) : el.tag === token;
    const walk = (node, depth) => {
      for (const child of node.children) {
        if (tokenMatches(child, tokens[depth])) {
          if (depth === tokens.length - 1) {
            matches.push(child);
            walk(child, depth);
          } else {
            walk(child, depth + 1);
          }
        } else {
          walk(child, depth);
        }
      }
    };
    walk(this, 0);
    return matches;
  }
}
