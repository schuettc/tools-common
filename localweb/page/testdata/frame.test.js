import { test, eq, assert, fixture } from './harness.js';
import { frame, collapsible } from '/_kit/frame.js';
import { lineTip, wireTip } from '/_kit/tip.js';

const parts = (extra = {}) => ({
  name: 'tooltest',
  left: { panel: 'index', label: 'The plan' },
  centre: { label: 'Cards' },
  right: { panel: 'thread', label: 'Thread' },
  ...extra,
});

function clean() {
  for (const k of Object.keys(localStorage)) if (k.includes('.collapsed.')) localStorage.removeItem(k);
}

function mount(f) {
  const app = document.createElement('div');
  app.className = 'kit-app';
  app.append(document.createElement('div'), f.left, f.centre, f.right);
  fixture().append(app);
  return app;
}

test('frame: three columns of the app grid, each side with its toggle and body', () => {
  clean();
  const f = frame(parts());
  eq([f.left.tagName, f.centre.tagName, f.right.tagName], ['ASIDE', 'MAIN', 'ASIDE']);
  eq([f.left.className, f.centre.className, f.right.className],
    ['kit-list kit-col kit-left-col', 'kit-read kit-col kit-mid', 'kit-rail kit-col kit-right-col']);
  eq([f.left.getAttribute('aria-label'), f.centre.getAttribute('aria-label'), f.right.getAttribute('aria-label')], ['The plan', 'Cards', 'Thread']);
  eq([...f.left.children].map(c => c.className), ['kit-col-toggle', 'kit-col-body']);
  assert(f.left.lastChild === f.leftBody && f.right.lastChild === f.rightBody, 'the bodies are the handles');
  eq(f.left.firstChild.dataset.panel, 'index');
});

test('frame: a side the caller leaves out is not drawn', () => {
  clean();
  const f = frame(parts({ right: undefined }));
  assert(f.right === null && f.rightBody === null, 'no right column');
  assert(f.left && f.centre, 'the others are there');
});

test('frame: the toggle folds its side and says so', () => {
  clean();
  const f = frame(parts());
  mount(f);
  f.left.style.transition = 'none'; // the fold's slide would hide the width
  const t = f.left.firstChild;
  eq([t.textContent, t.title, t.getAttribute('aria-expanded')], ['‹', 'Hide the index', 'true']);
  eq(f.right.firstChild.textContent, '›');
  t.click();
  assert(f.left.classList.contains('kit-collapsed'), 'folded');
  eq([t.textContent, t.title, t.getAttribute('aria-expanded')], ['›', 'Show the index', 'false']);
  eq([getComputedStyle(f.left).width, getComputedStyle(f.leftBody).display], ['40px', 'none']);
  t.click();
  assert(!f.left.classList.contains('kit-collapsed'), 'unfolded');
  eq(getComputedStyle(f.left).width, '288px');
});

test('frame: a fold is remembered under <name>.collapsed.<panel>', () => {
  clean();
  const f = frame(parts());
  f.right.firstChild.click();
  eq(localStorage.getItem('tooltest.collapsed.thread'), '1');
  f.right.firstChild.click();
  eq(localStorage.getItem('tooltest.collapsed.thread'), '');
});

test('frame: it opens with the state diazo users already have, and another tool is apart', () => {
  clean();
  localStorage.setItem('diazo.collapsed.index', '1');
  assert(frame(parts({ name: 'diazo' })).left.classList.contains('kit-collapsed'), 'diazo.collapsed.index folds it');
  assert(!frame(parts({ name: 'other' })).left.classList.contains('kit-collapsed'), 'another tool has its own');
  clean();
});

test('frame: a thread with no messages folds and its first message opens it, leaving what is remembered alone', () => {
  clean();
  const f = frame(parts());
  f.setRightEmpty(true, 'F1');
  assert(f.right.classList.contains('kit-collapsed'), 'folded with nothing to show');
  eq(f.right.firstChild.title, 'Show the thread');
  f.setRightEmpty(false, 'F1');
  assert(!f.right.classList.contains('kit-collapsed'), 'the first message opens it');
  eq(localStorage.getItem('tooltest.collapsed.thread'), ''); // collapsible writes '' as it starts
});

test('frame: a press wins over the empty rule until the card changes', () => {
  clean();
  const f = frame(parts());
  f.setRightEmpty(true, 'F1');
  f.right.firstChild.click();
  f.setRightEmpty(true, 'F1');
  assert(!f.right.classList.contains('kit-collapsed'), 'opened by hand, empty stays open');
  f.right.firstChild.click();
  f.setRightEmpty(false, 'F1');
  assert(f.right.classList.contains('kit-collapsed'), 'folded by hand, a message leaves it folded');
  eq(localStorage.getItem('tooltest.collapsed.thread'), '1');
  f.setRightEmpty(false, 'F2');
  assert(!f.right.classList.contains('kit-collapsed'), 'the next card takes the rule');
  f.setRightEmpty(true, 'F1');
  assert(f.right.classList.contains('kit-collapsed'), 'the press went with its card: back on F1 the rule holds');
});

test('frame: a press holds over a reload on its card, and not on another', () => {
  clean();
  const f = frame(parts());
  f.setRightEmpty(false, 'F1');
  f.right.firstChild.click();
  const same = frame(parts());
  same.setRightEmpty(false, 'F1');
  assert(same.right.classList.contains('kit-collapsed'), 'still folded on its card');
  const other = frame(parts());
  other.setRightEmpty(false, 'F2');
  assert(!other.right.classList.contains('kit-collapsed'), 'another card takes the rule');
});

test('frame: the empty rule does nothing to a page with no thread', () => {
  clean();
  frame(parts({ right: undefined })).setRightEmpty(true, 'F1');
});

test('collapsible: folded starts it folded and leaves what the page remembers alone', () => {
  clean();
  const col = document.createElement('aside');
  const body = document.createElement('div');
  collapsible({ col, body, panel: 'index', side: 'left', name: 'tooltest', folded: true });
  assert(col.classList.contains('kit-collapsed'), 'folded');
  eq(localStorage.getItem('tooltest.collapsed.index'), null);
});

function tipped(when) {
  const trigger = document.createElement('button');
  const tip = lineTip('all of it');
  const box = document.createElement('span');
  box.className = 'kit-tip-at';
  box.append(trigger, tip);
  const outside = document.createElement('button');
  fixture().append(box, outside);
  wireTip(box, trigger, tip, when);
  return { box, trigger, tip, outside };
}

test('tip: a press opens it in the window, on top of whatever scrolls', () => {
  const { box, trigger, tip } = tipped();
  eq([tip.getAttribute('role'), tip.className, tip.textContent], ['tooltip', 'kit-tip', 'all of it']);
  eq(getComputedStyle(tip).display, 'none');
  trigger.click();
  assert(box.classList.contains('show'), 'shown');
  eq(trigger.getAttribute('aria-expanded'), 'true');
  eq([getComputedStyle(tip).display, getComputedStyle(tip).position], ['block', 'fixed']);
  assert(tip.style.top && tip.style.left, 'placed');
});

test('tip: it goes over its box where the window has no room below', () => {
  const { box, trigger, tip } = tipped();
  box.style.cssText = `position:fixed; top:${innerHeight - 30}px; left:40px`;
  trigger.click();
  assert(parseFloat(tip.style.top) < box.getBoundingClientRect().top, `tip top ${tip.style.top}`);
});

test('tip: Esc closes it', () => {
  const { box, trigger } = tipped();
  trigger.click();
  trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
  assert(!box.classList.contains('show'), 'closed');
});

test('tip: the next tap elsewhere closes it', () => {
  const { box, trigger, outside } = tipped();
  trigger.click();
  outside.click();
  assert(!box.classList.contains('show'), 'closed');
  eq(trigger.getAttribute('aria-expanded'), 'false');
});

test('tip: Enter toggles it from the keyboard', () => {
  const { box, trigger } = tipped();
  trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
  assert(box.classList.contains('show'), 'opened');
});

test('tip: a press with nothing to show goes to what is under it', () => {
  const { box, trigger } = tipped(() => false);
  let reached = false;
  box.parentElement.addEventListener('click', () => (reached = true), { once: true });
  trigger.click();
  assert(!box.classList.contains('show'), 'stays shut');
  assert(reached, 'the press carried on');
});
