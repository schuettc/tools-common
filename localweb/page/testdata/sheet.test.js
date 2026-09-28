import { test, eq, assert, fixture, focusEl } from './harness.js';
import { sheet } from '/_kit/sheet.js';
import { createKeys } from '/_kit/keys.js';
import { h } from '/_kit/dom.js';

const press = (key, opts = {}, el = document.activeElement || document.body) => {
  const e = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true, ...opts });
  el.dispatchEvent(e);
  return e;
};

test('sheet: builds a dialog with a backdrop, title, body and actions', () => {
  let ran = 0;
  const s = sheet({ title: 'decide 4', body: h('p', null, 'close 4 items'), actions: [{ label: 'decide', fill: true, run: () => ran++ }, { label: 'cancel' }] });
  try {
    assert(s.el.classList.contains('kit-float') && s.el.classList.contains('kit-sheet'));
    eq([s.el.getAttribute('role'), s.el.getAttribute('aria-modal')], ['dialog', 'true']);
    assert(document.querySelector('.kit-backdrop'), 'backdrop');
    eq(s.el.querySelector('.kit-label').textContent, 'decide 4');
    s.el.querySelector('.kit-btn.fill').click();
    eq(ran, 1);
    eq(getComputedStyle(s.el).boxShadow.includes('rgba(0, 0, 0, 0.16)'), true, 'uses --kit-lift');
  } finally { s.close(); }
  assert(!document.querySelector('.kit-sheet') && !document.querySelector('.kit-backdrop'), 'close removes both');
});

test('sheet: Esc closes it and focus returns', () => {
  const opener = h('button', null, 'decide');
  fixture().append(opener);
  opener.focus();
  let closed = 0;
  const input = h('input');
  const s = sheet({ title: 't', body: input, onClose: () => closed++ });
  assert(s.el.contains(document.activeElement), 'focus moves inside');
  press('Escape');
  eq(closed, 1);
  assert(!s.el.isConnected, 'removed');
  eq(document.activeElement, opener, 'focus returned');
  s.close();
  eq(closed, 1, 'close is idempotent');
});

test('sheet: backdrop click closes', () => {
  const s = sheet({ title: 't' });
  document.querySelector('.kit-backdrop').click();
  assert(!s.el.isConnected);
});

test('sheet: focus stays inside', () => {
  const outside = h('button', null, 'outside');
  fixture().append(outside);
  const a = h('button', null, 'a'), b = h('button', null, 'b');
  const s = sheet({ title: 't', body: h('div', null, a, b) });
  try {
    b.focus();
    const e = press('Tab');
    assert(e.defaultPrevented, 'tab is trapped at the end');
    eq(document.activeElement, a, 'wraps to the first');
    press('Tab', { shiftKey: true });
    eq(document.activeElement, b, 'shift-tab wraps to the last');
    focusEl(outside);
    assert(s.el.contains(document.activeElement), 'focus pulled back in');
  } finally { s.close(); }
});

test('sheet: page keys do not fire while it is open', () => {
  const calls = [];
  const keys = createKeys({ list: { move: d => calls.push(d), open() {}, toggle() {}, toggleRange() {} } });
  let ran = 0;
  keys.register({ keys: '1', label: 'keep', run: () => ran++ });
  const s = sheet({ title: 't', body: h('button', null, 'x') });
  try {
    press('j', {}, document.body); press('1', {}, document.body); press('?', {}, document.body);
    eq([calls.length, ran], [0, 0]);
    assert(!document.querySelector('.kit-keys'), 'no help over a sheet');
  } finally { s.close(); }
  press('1', {}, document.body);
  eq(ran, 1, 'keys resume after close');
  keys.destroy();
});

test('sheet: only the top sheet takes Esc', () => {
  const s1 = sheet({ title: 'one' });
  const s2 = sheet({ title: 'two' });
  press('Escape');
  assert(!s2.el.isConnected && s1.el.isConnected);
  press('Escape');
  assert(!s1.el.isConnected);
});

test('sheet: the ? overlay is a sheet', () => {
  const keys = createKeys();
  try {
    press('?', {}, document.body);
    const ov = document.querySelector('.kit-keys');
    assert(ov && ov.classList.contains('kit-sheet'), 'help uses the sheet');
    assert(document.querySelector('.kit-backdrop'));
    press('?');
    assert(!document.querySelector('.kit-keys') && !document.querySelector('.kit-backdrop'), '? closes it');
  } finally { keys.destroy(); }
});
