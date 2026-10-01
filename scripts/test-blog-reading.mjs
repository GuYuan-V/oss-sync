import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import vm from 'node:vm';

for (const theme of ['default', 'papertrail']) {
  for (const reducedMotion of [false, true]) {
    const attributes = new Map();
    const listeners = new Map();
    const scrolls = [];
    const button = {
      style: {},
      classList: { toggle() {} },
      setAttribute: (name, value) => attributes.set(name, value),
      addEventListener: (event, handler) => listeners.set(event, handler),
    };
    let ready;
    const context = {
      document: {
        documentElement: { classList: { add() {}, remove() {} } },
        getElementById: () => null,
        querySelector: (selector) => selector === '[data-back-to-top]' ? button : null,
        addEventListener: (event, handler) => { if (event === 'DOMContentLoaded') ready = handler; },
      },
      window: {
        scrollY: 1500,
        matchMedia: () => ({ matches: reducedMotion }),
        addEventListener() {},
        scrollTo: (options) => scrolls.push(options),
      },
    };
    vm.runInNewContext(readFileSync(new URL(`../internal/blog/assets/${theme}/theme.js`, import.meta.url), 'utf8'), context);
    ready();
    assert.notEqual(button.style.opacity, '0', `${theme}: button must remain visible`);
    assert.notEqual(attributes.get('aria-hidden'), 'true', `${theme}: button must remain accessible`);
    assert.equal(typeof listeners.get('click'), 'function');
    listeners.get('click')();
    assert.equal(scrolls[0].top, 0);
    assert.equal(scrolls[0].behavior, reducedMotion ? 'auto' : 'smooth');
  }
}
console.log('Both blog themes: visible, accessible, and back-to-top scrolling passed.');
