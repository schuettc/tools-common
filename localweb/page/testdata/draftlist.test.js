import { test, eq, assert, fixture, blurEl, focusEl } from './harness.js';
import { draftList, draftRow } from '/_kit/draftlist.js';

const DRAFTS = [
  { id: 'd1', quote: 'purged weekly', text: 'Legal said 72 hours.', meta: 'goes with the next Revise' },
  { id: 'd2', quote: 'in March', text: 'Drop the month.' },
];

function make(extra = {}) {
  const heard = [];
  const list = draftList({
    drafts: DRAFTS,
    empty: 'Nothing drafted.',
    onEdit: (id, text) => heard.push(['edit', id, text]),
    onDelete: id => heard.push(['delete', id]),
    onSelect: id => heard.push(['select', id]),
    ...extra,
  });
  const el = list.el;
  fixture().append(el);
  const rows = [...el.querySelectorAll('.kit-draft-row')];
  const box = i => rows[i].querySelector('textarea');
  return { list, el, heard, rows, box };
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

const NEW = { id: 'd3', quote: 'new one', text: 'Arrived while typing.' };
const shownIds = el => [...el.querySelectorAll('.kit-draft-row')].map(r => r.dataset.draftId);

test('redraw: typing in a box survives a new draft arriving: the words, the focus and the caret', () => {
  const { list, el, heard, rows, box } = make();
  const typing = box(1);
  typing.focus();
  typing.value = 'half a sentence';
  typing.setSelectionRange(4, 6);
  heard.length = 0;
  list.redraw({ drafts: [...DRAFTS, NEW] });
  eq(shownIds(el), ['d1', 'd2', 'd3']);
  assert(el.querySelectorAll('.kit-draft-row')[1] === rows[1], 'the same row');
  assert(document.activeElement === typing, 'the focus stays in the box');
  eq([typing.value, typing.selectionStart, typing.selectionEnd], ['half a sentence', 4, 6]);
  eq(heard, []);
  blurEl(typing);
  eq(heard, [['edit', 'd2', 'half a sentence']]);
});

test('redraw: a row keeps its focus when the others move around it', () => {
  const { list, el, box } = make();
  const typing = box(1);
  typing.focus();
  typing.value = 'half';
  list.redraw({ drafts: [DRAFTS[1], NEW, DRAFTS[0]] });
  eq(shownIds(el), ['d2', 'd3', 'd1']);
  assert(document.activeElement === typing, 'the focus stays in the box');
  eq(typing.value, 'half');
});

test('redraw: changes the quote, meta and mark of a kept row', () => {
  const { list, rows } = make({ selected: 'd1' });
  list.redraw({ selected: 'd2', drafts: [{ ...DRAFTS[0], quote: 'purged daily', meta: undefined }, { ...DRAFTS[1], meta: 'now with meta' }] });
  eq(rows[0].querySelector('.kit-draft-quote').textContent, 'purged daily');
  assert(!rows[0].querySelector('.kit-meta'), 'its meta went');
  eq(rows[1].querySelector('.kit-meta').textContent, 'now with meta');
  assert(rows[1].lastChild.contains(rows[1].querySelector('.kit-btn')), 'the meta sits above the buttons');
  eq(rows.map(r => r.classList.contains('on')), [false, true]);
});

test('redraw: new words from the tool replace a box with nothing unsaved, and never unsaved words', () => {
  const { list, heard, box } = make();
  box(1).value = 'mine';
  list.redraw({ drafts: [{ ...DRAFTS[0], text: 'from elsewhere' }, { ...DRAFTS[1], text: 'also from elsewhere' }] });
  eq([box(0).value, box(1).value], ['from elsewhere', 'mine']);
  blurEl(box(0));
  eq(heard, [], 'the tool gave those words, so there is nothing to save');
  blurEl(box(1));
  eq(heard, [['edit', 'd2', 'mine']]);
});

test('redraw: a draft removed goes, with its focused box and unsaved words, and nothing is saved', () => {
  const { list, el, heard, rows, box } = make();
  const typing = box(0);
  typing.focus();
  typing.value = 'half';
  heard.length = 0;
  list.redraw({ drafts: [DRAFTS[1]] });
  eq(shownIds(el), ['d2']);
  assert(!rows[0].isConnected, 'gone');
  blurEl(typing);
  eq(heard, []);
});

test('redraw: to no drafts shows the empty line, and back shows rows', () => {
  const { list, el } = make();
  list.redraw({ drafts: [] });
  eq(shownIds(el), []);
  eq(el.querySelector('.kit-empty').textContent, 'Nothing drafted.');
  list.redraw({ drafts: [NEW] });
  eq(shownIds(el), ['d3']);
  assert(!el.querySelector('.kit-empty'), 'the empty line went');
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
