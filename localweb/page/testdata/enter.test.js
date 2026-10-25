import { test, eq, assert, fixture } from './harness.js';
import { submitOnEnter } from '/_kit/enter.js';
import { noteField } from '/_kit/dom.js';

// press dispatches a keydown on el and returns the event.
function press(el, key, opts = {}) {
  const e = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true, ...opts });
  el.dispatchEvent(e);
  return e;
}

function setup() {
  const box = document.createElement('textarea');
  fixture().append(box);
  let sent = 0;
  const got = submitOnEnter(box, () => sent++);
  return { box, got, sent: () => sent };
}

test('enter: returns the field', () => {
  const { box, got } = setup();
  assert(got === box);
});

test('enter: a plain Enter submits and is prevented', () => {
  const { box, sent } = setup();
  const e = press(box, 'Enter');
  eq(sent(), 1);
  eq(e.defaultPrevented, true);
});

test('enter: Shift-Enter, a repeat and IME input neither submit nor prevent', () => {
  for (const [name, opts] of [
    ['shift', { shiftKey: true }],
    ['repeat', { repeat: true }],
    ['isComposing', { isComposing: true }],
    ['keyCode 229', { keyCode: 229 }],
  ]) {
    const { box, sent } = setup();
    const e = press(box, 'Enter', opts);
    eq(sent(), 0, `${name} submitted`);
    eq(e.defaultPrevented, false, `${name} prevented`);
  }
});

test('enter: another key does nothing', () => {
  const { box, sent } = setup();
  const e = press(box, 'a');
  eq(sent(), 0);
  eq(e.defaultPrevented, false);
});

test('enter: noteField commits on Enter and not on a repeat', () => {
  const commits = [];
  const n = noteField({ value: '', onCommit: v => commits.push(v) });
  fixture().append(n);
  n.value = 'held';
  const r = press(n, 'Enter', { repeat: true });
  eq(commits, [], 'a repeat commits');
  eq(r.defaultPrevented, false);
  const e = press(n, 'Enter');
  eq(commits, ['held']);
  eq(e.defaultPrevented, true);
});
