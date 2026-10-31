import { test, eq, assert, fixture, blurEl, focusEl } from './harness.js';
import { draftList, draftRow } from '/_kit/draftlist.js';

const DRAFTS = [
  { id: 'd1', quote: 'purged weekly', text: 'Legal said 72 hours.', meta: 'goes with the next Revise' },
  { id: 'd2', quote: 'in March', text: 'Drop the month.' },
];

function make(extra = {}) {
  const heard = [];
  const el = draftList({
    drafts: DRAFTS,
    empty: 'Nothing drafted.',
    onEdit: (id, text) => heard.push(['edit', id, text]),
    onDelete: id => heard.push(['delete', id]),
    onSelect: id => heard.push(['select', id]),
    ...extra,
  });
  fixture().append(el);
  const rows = [...el.querySelectorAll('.kit-draft-row')];
  const box = i => rows[i].querySelector('textarea');
  return { el, heard, rows, box };
}

const enter = (extra = {}) => new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true, ...extra });

test('draftList: one row per draft, with its quoted item, words and meta', () => {
  const { rows, box } = make();
  eq(rows.length, 2);
  eq(rows.map(r => r.querySelector('.kit-draft-quote').textContent), ['purged weekly', 'in March']);
  eq([box(0).value, box(1).value], ['Legal said 72 hours.', 'Drop the month.']);
  eq(rows[0].querySelector('.kit-meta').textContent, 'goes with the next Revise');
  assert(!rows[1].querySelector('.kit-meta'), 'no meta, no line');
});

test('draftList: with no drafts it shows the empty line the tool gave', () => {
  const { el, rows } = make({ drafts: [] });
  eq(rows.length, 0);
  eq(el.querySelector('.kit-empty').textContent, 'Nothing drafted.');
});

test('draftList: selected marks its row', () => {
  const { rows } = make({ selected: 'd2' });
  eq(rows.map(r => r.classList.contains('on')), [false, true]);
});

test('draftRow: leaving the box saves words that changed', () => {
  const { heard, box } = make();
  focusEl(box(0));
  heard.length = 0;
  box(0).value = 'Legal said 48.';
  blurEl(box(0));
  eq(heard, [['edit', 'd1', 'Legal said 48.']]);
});

test('draftRow: leaving the box with the same words saves nothing', () => {
  const { heard, box } = make();
  focusEl(box(0));
  blurEl(box(0));
  eq(heard.filter(e => e[0] === 'edit'), []);
});

test('draftRow: Enter saves once, and Shift-Enter breaks the line', () => {
  const { heard, box } = make();
  box(0).value = 'Now.';
  box(0).dispatchEvent(enter({ shiftKey: true }));
  eq(heard, []);
  const e = enter();
  box(0).dispatchEvent(e);
  assert(e.defaultPrevented, 'no new line');
  blurEl(box(0));
  eq(heard, [['edit', 'd1', 'Now.']]);
});

test('draftRow: delete tells the tool, without selecting or saving what was typed', () => {
  const { heard, rows, box } = make();
  box(0).value = 'half typed';
  rows[0].querySelector('.kit-btn').click();
  blurEl(box(0));
  eq(heard, [['delete', 'd1']]);
});

test('draftRow: pressing delete leaves the focus in the box, so a save cannot move the button', () => {
  const { rows } = make();
  const press = new MouseEvent('mousedown', { bubbles: true, cancelable: true });
  rows[0].querySelector('.kit-btn').dispatchEvent(press);
  assert(press.defaultPrevented, 'the press keeps the focus where it is');
});

test('draftRow: pressing a row selects it and moves the mark, and pressing it again tells the tool again', () => {
  const { heard, rows } = make({ selected: 'd2' });
  rows[0].querySelector('.kit-draft-quote').click();
  eq(heard, [['select', 'd1']]);
  eq(rows.map(r => r.classList.contains('on')), [true, false]);
  rows[0].click();
  eq(heard, [['select', 'd1'], ['select', 'd1']]);
});

test('draftRow: focus in the box selects the row, and clicks in its words after that do not', () => {
  const { heard, rows, box } = make();
  focusEl(box(1));
  eq(heard, [['select', 'd2']]);
  assert(rows[1].classList.contains('on'), 'marked');
  box(1).click();
  eq(heard, [['select', 'd2']]);
});

test('draftRow: a row stands alone, with no callbacks given', () => {
  const li = draftRow({ draft: DRAFTS[1], on: true });
  fixture().append(li);
  eq(li.tagName, 'LI');
  assert(li.classList.contains('on'), 'on');
  const box = li.querySelector('textarea');
  box.value = 'x';
  box.dispatchEvent(enter());
  li.querySelector('.kit-btn').click();
  li.click();
});
