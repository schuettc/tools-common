import { test, eq, assert, fixture, blurEl } from './harness.js';
import { list } from '/_kit/dom.js';
import { createKeys } from '/_kit/keys.js';

const press = (key, el, opts = {}) => {
  const e = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true, ...opts });
  el.dispatchEvent(e);
  return e;
};
const type = (input, text) => {
  input.value = text;
  input.dispatchEvent(new InputEvent('input', { bubbles: true, data: text }));
};

function mk(extra = {}) {
  const inputs = [];
  const l = list({ label: 'attention', row: t => ({ id: t, title: t }), search: { placeholder: 'key or title', onInput: t => inputs.push(t), ...extra } });
  l.setItems(['a', 'b']);
  fixture().append(l.el);
  return { l, inputs, field: l.el.querySelector('.kit-search') };
}

test('search: rendered in the header with the chips, styled by the kit', () => {
  const { l, field } = mk({ value: 'muster' });
  assert(field && field.tagName === 'INPUT', 'an input');
  assert(l.el.querySelector('.kit-lh').contains(field), 'in the list header');
  eq([field.type, field.placeholder, field.value], ['search', 'key or title', 'muster']);
  const cs = getComputedStyle(field);
  eq(cs.borderRadius, '6px');
  assert(/Mono|monospace/.test(cs.fontFamily), 'mono');
  assert(!list({ label: 'x', row: t => ({ title: t }) }).el.querySelector('.kit-search'), 'no search, no field');
});

test('search: typing calls onInput (no debounce)', () => {
  const { inputs, field } = mk();
  type(field, 'm'); type(field, 'mu');
  eq(inputs, ['m', 'mu']);
});

test('search: setSearch updates the field without onInput', () => {
  const { l, inputs, field } = mk();
  l.setSearch('from url');
  eq([field.value, inputs.length], ['from url', 0]);
});

test('search: Esc clears the text, then leaves the field', () => {
  const { inputs, field } = mk();
  const keys = createKeys();
  try {
    field.focus();
    type(field, 'abc');
    press('Escape', field);
    eq(field.value, '');
    eq(inputs.at(-1), '', 'clearing reports the empty text');
    assert(document.activeElement === field, 'still in the field after the first Esc');
    press('Escape', field);
    assert(document.activeElement !== field, 'the second Esc leaves');
  } finally { keys.destroy(); }
});

test('search: / focuses the field, and page keys wait while it has focus', () => {
  const { l, field } = mk();
  const keys = createKeys({ list: l });
  let ran = 0;
  try {
    keys.register({ keys: '1', label: 'keep', run: () => ran++ });
    assert(press('/', document.body).defaultPrevented, '/ handled');
    assert(document.activeElement === field, '/ focuses the search');
    const before = l.current();
    for (const k of ['1', 'j', 'x', '?']) press(k, field);
    eq([ran, l.current()], [0, before], 'no page keys in the field');
    assert(!document.querySelector('.kit-keys'), 'no help overlay');
    blurEl(field);
  } finally { keys.destroy(); }
});

test('search: / is a default only when search is given', () => {
  const plain = list({ label: 'x', row: t => ({ title: t }) });
  const k1 = createKeys({ list: plain });
  try { k1.register({ keys: '/', label: 'mine', run() {} }); } finally { k1.destroy(); }
  const { l } = mk();
  const k2 = createKeys({ list: l });
  let err;
  try { k2.register({ keys: '/', label: 'mine', run() {} }); } catch (e) { err = e; } finally { k2.destroy(); }
  assert(err && /clash/.test(err.message), 'a page / clashes when the list has search');
});
