import { test, eq, assert, fixture } from './harness.js';
import { needsPill, nextLine, arrive } from '/_kit/needs.js';

const items = [
  { id: 'D1', label: 'Decision: Offline sends', detail: 'Choose one of three' },
  { id: 'S1', label: 'Your step: Look at the page' },
  { id: 'Q1', label: 'Question on the spec' },
];
const menu = () => document.querySelector('.kit-needs-menu');
const rows = () => [...document.querySelectorAll('.kit-needs-menu [role=menuitem]')];
const clear = () => menu()?.remove();

test('needsPill: one item is a button that opens it', () => {
  clear();
  const heard = [];
  const p = needsPill({ items: items.slice(0, 1), open: id => heard.push(id) });
  fixture().append(p.el);
  eq(p.el.tagName, 'BUTTON');
  eq(p.el.textContent, '1 needs you →');
  p.el.click();
  eq(heard, ['D1']);
  assert(!menu(), 'no menu for one');
});

test('needsPill: several open a menu naming each in order, and a pick opens it', () => {
  clear();
  const heard = [];
  const p = needsPill({ items, open: id => heard.push(id) });
  fixture().append(p.el);
  eq(p.el.textContent, '3 need you ▾');
  eq(p.el.getAttribute('aria-expanded'), 'false');
  p.el.click();
  eq(heard, []);
  eq(p.el.getAttribute('aria-expanded'), 'true');
  eq(rows().map(r => r.textContent), ['Decision: Offline sendsChoose one of three', 'Your step: Look at the page', 'Question on the spec']);
  rows()[1].click();
  eq(heard, ['S1']);
  assert(!menu(), 'a pick closes it');
});

test('needsPill: none shows nothing', () => {
  clear();
  const p = needsPill({ items: [], open() { throw new Error('nothing to open'); } });
  fixture().append(p.el);
  assert(p.el.hidden, 'hidden with none');
});

test('needsPill: Escape and a press elsewhere close the menu', () => {
  clear();
  const p = needsPill({ items, open() {} });
  fixture().append(p.el);
  p.el.click();
  document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
  assert(!menu(), 'Escape closes it');
  p.el.click();
  document.body.dispatchEvent(new PointerEvent('pointerdown', { bubbles: true }));
  assert(!menu(), 'a press elsewhere closes it');
});

test('needsPill: set with the same items leaves an open menu and its rows alone', () => {
  clear();
  const p = needsPill({ items, open() {} });
  fixture().append(p.el);
  p.el.click();
  const was = rows()[0];
  p.set(items.map(i => ({ ...i })));
  assert(rows()[0] === was, 'the row under the pointer stays');
  p.set(items.slice(0, 2));
  eq(rows().length, 2);
  p.set(items.slice(0, 1));
  assert(!menu(), 'one left needs no menu');
  eq(p.el.textContent, '1 needs you →');
  p.set([]);
  assert(p.el.hidden, 'none left hides it');
});

test('nextLine: says how many are left, with a button naming the next', () => {
  const heard = [];
  const l = nextLine({ left: 2, next: { id: 'S1', label: 'Your step: Look at the page' }, open: id => heard.push(id) });
  eq(l.querySelector('.kit-next-words').textContent, '✓ Sent. 2 more need you.');
  const b = l.querySelector('button');
  eq(b.textContent, 'Next: Your step: Look at the page →');
  b.click();
  eq(heard, ['S1']);
  eq(nextLine({ left: 1, next: { id: 'S1', label: 'x' }, open() {} }).querySelector('.kit-next-words').textContent, '✓ Sent. 1 more needs you.');
});

test('nextLine: with none left it says so and has no button', () => {
  const l = nextLine({ left: 0, open() {} });
  eq(l.textContent, '✓ Sent. Nothing else needs you.');
  assert(!l.querySelector('button'), 'no button at zero');
});

const page = control => {
  const box = document.createElement('div');
  box.style.cssText = 'height:200px;overflow:auto';
  const card = document.createElement('div');
  card.append(Object.assign(document.createElement('div'), { style: 'height:150px' }), control);
  box.append(Object.assign(document.createElement('div'), { style: 'height:1000px' }), card, Object.assign(document.createElement('div'), { style: 'height:1000px' }));
  fixture().append(box);
  return { box, card };
};
const inside = (el, box) => {
  const a = el.getBoundingClientRect();
  const b = box.getBoundingClientRect();
  return a.top >= b.top && a.bottom <= b.bottom;
};

test('arrive: a marked control comes on screen, and the card with it where it fits', () => {
  const b = document.createElement('button');
  b.dataset.answer = '';
  b.textContent = 'Approve';
  const { box, card } = page(b);
  arrive(card);
  assert(inside(b, box), 'the control is on screen');
  assert(inside(card, box), 'the card is on screen');
  assert(document.activeElement !== b, 'a button is not focused');
});

test('arrive: on a card taller than the screen the control wins', () => {
  const b = document.createElement('button');
  b.dataset.answer = '';
  const { box, card } = page(b);
  card.firstChild.style.height = '600px';
  arrive(card);
  assert(inside(b, box), 'the control is on screen');
});

test('arrive: a reply box is focused', () => {
  const t = document.createElement('textarea');
  const { box, card } = page(t);
  arrive(card);
  assert(inside(t, box), 'the box is on screen');
  assert(document.activeElement === t, 'the box has focus');
});

test('arrive: a reply box given itself is focused', () => {
  const t = document.createElement('textarea');
  const { box } = page(t);
  arrive(t);
  assert(inside(t, box), 'the box is on screen');
  assert(document.activeElement === t, 'the box has focus');
});
