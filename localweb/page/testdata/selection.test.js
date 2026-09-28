import { test, eq, assert, fixture } from './harness.js';
import { createSelection } from '/_kit/selection.js';
import { list } from '/_kit/dom.js';

test('selection: store basics and onChange', () => {
  const s = createSelection();
  const seen = [];
  const off = s.onChange(ids => seen.push(ids.slice()));
  s.toggle('a'); s.toggle('b'); s.toggle('a');
  eq(s.ids(), ['b']);
  assert(s.has('b') && !s.has('a'));
  s.all(['c', 'd']);
  s.deselect(['b']);
  eq(s.ids(), ['c', 'd']);
  s.range('c', 'f', ['a', 'b', 'c', 'd', 'e', 'f']);
  eq(s.ids().sort(), ['c', 'd', 'e', 'f'], 'range takes the anchor’s state');
  s.range('a', 'c', ['a', 'b', 'c', 'd']);
  eq(s.ids().sort(), ['d', 'e', 'f'], 'an unselected anchor clears the range');
  s.clear();
  eq(s.ids(), []);
  eq(seen.length, 8, 'toggle ×3, all, deselect, two ranges, clear');
  off();
  s.toggle('z');
  eq(seen.length, 8, 'unsubscribed');
  s.clear(); s.clear();
  eq(s.anchor(), null, 'clear resets the anchor');
});

test('selection: no change, no notification', () => {
  const s = createSelection();
  let n = 0;
  s.onChange(() => n++);
  s.all([]); s.deselect(['x']); s.clear();
  eq(n, 0);
});

const rowsOf = l => [...l.el.querySelectorAll('.kit-row')];
const mk = (selection, extra = {}) => list({ label: 'l', selection, row: it => ({ id: it, title: it, selectable: true }), ...extra });

test('selection: range across a re-render', () => {
  const l = mk();
  const items = ['a', 'b', 'c', 'd', 'e'];
  l.setItems(items);
  l.toggle(1);
  l.setItems(items.slice()); // re-render
  l.toggleRange(3);
  eq(l.selectedIds().sort(), ['b', 'c', 'd']);
});

test('selection: two lists share one store', () => {
  const store = createSelection();
  const sels = [];
  const flat = mk(store, { onSelect: s => sels.push(s.length) });
  const lane = mk(store);
  fixture().append(flat.el, lane.el);
  flat.setItems(['a', 'b', 'c', 'd']);
  lane.setItems(['b', 'd']);
  flat.toggle(1);
  assert(rowsOf(lane)[0].classList.contains('sel'), 'lane shows b selected');
  lane.toggle(1);
  assert(rowsOf(flat)[3].classList.contains('sel'), 'flat shows d selected');
  eq(flat.selectedIds().sort(), ['b', 'd']);
  eq(sels.at(-1), 2, 'flat onSelect fired for the lane’s change');
  store.clear();
  eq([rowsOf(flat).filter(r => r.classList.contains('sel')).length, rowsOf(lane).filter(r => r.classList.contains('sel')).length], [0, 0]);
  assert(flat.selection === store, 'handle exposes the store');
  lane.destroy();
  store.toggle('a');
  eq(rowsOf(lane).filter(r => r.classList.contains('sel')).length, 0, 'a destroyed list stops listening');
});

test('selection: a list without a store gets its own', () => {
  const a = mk(), b = mk();
  a.setItems(['x']); b.setItems(['x']);
  a.toggle(0);
  eq([a.selectedIds(), b.selectedIds()], [['x'], []]);
  assert(a.selection && a.selection !== b.selection);
});
