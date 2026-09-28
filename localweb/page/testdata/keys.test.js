import { test, eq, assert, fixture } from './harness.js';
import { createKeys } from '/_kit/keys.js';

// press dispatches a keydown on el (default: body) and returns the event.
function press(key, opts = {}, el = document.body) {
  const e = new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true, ...opts });
  el.dispatchEvent(e);
  return e;
}

function fakeList() {
  const calls = [];
  return {
    calls,
    move: d => calls.push(`move${d}`),
    open: () => calls.push('open'),
    toggle: () => calls.push('toggle'),
    toggleRange: () => calls.push('range'),
  };
}

test('keys: defaults drive list', () => {
  const list = fakeList();
  const keys = createKeys({ list });
  try {
    for (const [k, o] of [['j'], ['k'], ['ArrowDown'], ['ArrowUp'], ['o'], ['Enter'], ['x'], ['X', { shiftKey: true }]]) {
      assert(press(k, o).defaultPrevented, `${k} handled`);
    }
    eq(list.calls, ['move1', 'move-1', 'move1', 'move-1', 'open', 'open', 'toggle', 'range']);
  } finally { keys.destroy(); }
});

test('keys: register page keys', () => {
  const keys = createKeys({ list: fakeList() });
  const ran = [];
  try {
    for (const k of ['1', '2', '3', 'n', 'u']) keys.register({ keys: k, label: k, run: () => ran.push(k) });
    for (const k of ['1', '2', '3', 'n', 'u']) press(k);
    eq(ran, ['1', '2', '3', 'n', 'u']);
    let err;
    try { keys.register({ keys: 'j', label: 'mine', run() {} }); } catch (e) { err = e; }
    assert(err && /clash.*j/.test(err.message), `j clash: ${err && err.message}`);
    err = null;
    try { keys.register({ keys: '1', label: 'again', run() {} }); } catch (e) { err = e; }
    assert(err && /clash/.test(err.message), 'duplicate 1 clashes');
    err = null;
    try { keys.register({ keys: 'o a', label: 'prefix', run() {} }); } catch (e) { err = e; }
    assert(err && /clash/.test(err.message), 'a sequence starting with a bound key clashes');
  } finally { keys.destroy(); }
});

test('keys: sequences', () => {
  let t = 0;
  const keys = createKeys({ now: () => t });
  let ran = 0;
  try {
    keys.register({ keys: 'g a', label: 'attention', run: () => ran++ });
    assert(press('g').defaultPrevented, 'g is held as a prefix');
    t += 500; press('a');
    eq(ran, 1, 'g a runs');
    press('g'); t += 1100;
    assert(!press('a').defaultPrevented, 'a after timeout is not handled');
    eq(ran, 1, 'timed-out sequence does not run');
  } finally { keys.destroy(); }
});

test('keys: field focus swallows page keys', () => {
  const list = fakeList();
  const keys = createKeys({ list });
  let ran = 0, fieldRan = 0;
  try {
    keys.register({ keys: '1', label: 'keep', run: () => ran++ });
    keys.register({ keys: '⌘↵', label: 'send', run: () => fieldRan++, inField: true });
    const input = document.createElement('input');
    fixture().append(input);
    input.focus();
    for (const k of ['1', 'j', '?']) assert(!press(k, {}, input).defaultPrevented, `${k} not prevented in field`);
    press('1', { isComposing: true }, input);
    eq([ran, list.calls.length], [0, 0], 'nothing ran while typing');
    assert(!document.querySelector('.kit-keys'), 'no overlay from ? in a field');
    press('Enter', { metaKey: true }, input);
    eq(fieldRan, 1, 'inField binding fires in the field');
    press('Escape', {}, input);
    assert(document.activeElement !== input, 'Esc blurs the field');
    press('1', { isComposing: true });
    eq(ran, 0, 'composition never triggers');
  } finally { keys.destroy(); }
});

test('keys: unregistered modifier combos pass through', () => {
  const list = fakeList();
  const keys = createKeys({ list });
  try {
    assert(!press('r', { metaKey: true }).defaultPrevented, '⌘r passes');
    assert(!press('f', { ctrlKey: true }).defaultPrevented, '⌃f passes');
    assert(!press('j', { metaKey: true }).defaultPrevented, '⌘j passes');
    eq(list.calls, [], 'no defaults ran');
    assert(!press('q').defaultPrevented, 'an unbound key passes');
  } finally { keys.destroy(); }
});

test('keys: enter on a button is left to the button', () => {
  const list = fakeList();
  const keys = createKeys({ list });
  try {
    const b = document.createElement('button');
    fixture().append(b);
    assert(!press('Enter', {}, b).defaultPrevented, 'enter on a button not taken');
    eq(list.calls, []);
  } finally { keys.destroy(); }
});

test('keys: help lists defaults then page keys', () => {
  const keys = createKeys({ list: fakeList() });
  try {
    keys.register({ keys: '1', label: 'keep', run() {}, group: 'label' });
    const b = keys.bindings();
    eq(b[0].group, 'family');
    eq(b[b.length - 1], { keys: '1', label: 'keep', group: 'label' });
    press('?');
    const ov = document.querySelector('.kit-float.kit-keys');
    assert(ov, 'overlay open');
    for (const label of ['next', 'open', 'select', 'keep']) assert(ov.textContent.includes(label), `lists ${label}`);
    press('Escape');
    assert(!document.querySelector('.kit-keys'), 'Esc closes');
    keys.showHelp(true);
    assert(document.querySelector('.kit-keys'), 'showHelp opens');
    keys.showHelp(false);
    assert(!document.querySelector('.kit-keys'), 'showHelp(false) closes');
  } finally { keys.destroy(); }
});

test('keys: unregister and destroy', () => {
  const keys = createKeys();
  let ran = 0;
  const off = keys.register({ keys: 'n', label: 'note', run: () => ran++ });
  press('n'); off(); press('n');
  eq(ran, 1, 'unregistered');
  keys.register({ keys: 'u', label: 'undo', run: () => ran++ });
  keys.destroy();
  press('u'); press('?');
  eq(ran, 1, 'destroyed');
  assert(!document.querySelector('.kit-keys'), 'no overlay after destroy');
});
