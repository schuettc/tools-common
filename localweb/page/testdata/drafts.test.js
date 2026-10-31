import { test, eq, assert, fixture } from './harness.js';
import { createDrafts, questionButton, QUESTIONS, sendBar } from '/_kit/drafts.js';

function type(box, text) {
  box.value = text;
  box.dispatchEvent(new Event('input', { bubbles: true }));
}

test('questionButton: not asked yet, a press runs it', () => {
  let ran = 0;
  const b = questionButton({ label: QUESTIONS.explain, asked: false, run: () => ran++ });
  eq([b.label, !!b.disabled], ['Explain this more', false]);
  b.run();
  eq(ran, 1);
});

test('questionButton: once asked it reads Sent and stays off', () => {
  let ran = 0;
  const b = questionButton({ label: QUESTIONS.smaller, asked: true, run: () => ran++ });
  eq([b.label, b.disabled], ['✓ Sent: Is there a smaller option?', true]);
  b.run();
  eq(ran, 0);
});

test('drafts: a box is a labelled textarea, and its draft survives a redraw', () => {
  const d = createDrafts();
  const first = d.box('note:a', 'A note', 3);
  eq([first.box.tagName, first.box.className, first.box.rows, first.box.placeholder, first.box.dataset.draft, first.box.getAttribute('aria-label')],
    ['TEXTAREA', 'kit-note kit-draft', 3, 'A note', 'note:a', 'A note']);
  eq(first.el.className, 'kit-draft-box');
  type(first.box, 'kept');
  eq(d.box('note:a', 'A note').box.value, 'kept');
});

test('drafts: wrap lets the caller hold the box in something of its own', () => {
  const d = createDrafts({ wrap: (box, key) => {
    const fig = document.createElement('figure');
    fig.dataset.key = key;
    fig.append(box);
    return fig;
  } });
  const { el, box } = d.box('k', 'p');
  eq([el.tagName, el.dataset.key], ['FIGURE', 'k']);
  assert(box.parentElement === el, 'the box is inside');
});

test('drafts: take returns the text, forgets it and empties every box drawn for it', () => {
  const d = createDrafts();
  const { box } = d.box('k', 'p');
  fixture().append(box);
  type(box, 'hello');
  eq(d.take('k'), 'hello');
  eq([box.value, d.get('k')], ['', '']);
  eq(d.take('k'), '');
});

test('drafts: restore puts text back, in the store and the box', () => {
  const d = createDrafts();
  const { box } = d.box('k', 'p');
  fixture().append(box);
  d.restore('k', 'refused');
  eq([box.value, d.get('k')], ['refused', 'refused']);
});

test('drafts: notes are the drafts under a prefix, by what follows it', () => {
  const d = createDrafts();
  d.restore('note:a', 'one');
  d.restore('thread:a', 'two');
  eq([...d.notes('note:')], [['a', 'one']]);
});

test('drafts: dirty is any pick or any text that is more than space', () => {
  const d = createDrafts();
  assert(!d.dirty(), 'empty');
  d.restore('k', '  ');
  assert(!d.dirty(), 'blank');
  d.restore('k', 'x');
  assert(d.dirty(), 'text');
  d.take('k');
  d.picks.set('DCSN1', 'A');
  assert(d.dirty(), 'a pick');
});

test('drafts: discard drops the picks and the drafts under a prefix, and no others', () => {
  const d = createDrafts();
  d.picks.set('DCSN1', 'A');
  d.restore('note:a', 'one');
  d.restore('thread:a', 'two');
  d.discard('note:');
  eq([d.picks.size, d.get('note:a'), d.get('thread:a')], [0, '', 'two']);
  eq([...d.keys()], ['thread:a']);
});

const bar = (more = {}) => sendBar({ count: '1 of 2 answered', sends: 2, hint: 'Unanswered ones stay open.', discard() {}, send() {}, ...more });

test('sendBar: what it holds, in order', () => {
  const b = bar();
  fixture().append(b);
  eq(b.className, 'kit-sendbar');
  eq([...b.children].map(c => c.className), ['kit-sendbar-count', 'kit-sendbar-hint', 'kit-btns']);
  eq([b.children[0].textContent, b.children[1].textContent], ['1 of 2 answered', 'Unanswered ones stay open.']);
  eq([...b.querySelectorAll('.kit-btn')].map(x => [x.textContent, x.classList.contains('fill')]), [['Discard drafts', false], ['Send 2', true]]);
  eq(getComputedStyle(b).display, 'flex');
});

test('sendBar: Send and Discard call the caller', () => {
  const got = [];
  const b = bar({ discard: () => got.push('discard'), send: () => got.push('send') });
  const [discard, send] = b.querySelectorAll('.kit-btn');
  discard.click();
  send.click();
  eq(got, ['discard', 'send']);
});

test('sendBar: nothing to send leaves both off', () => {
  eq([...bar({ sends: 0 }).querySelectorAll('.kit-btn')].map(x => [x.textContent, x.disabled]), [['Discard drafts', true], ['Send 0', true]]);
});

test('sendBar: the caller can add a button before them', () => {
  const b = bar({ extra: [{ label: 'Take all recommendations', disabled: true }] });
  const first = b.querySelector('.kit-btn');
  eq([first.textContent, first.disabled], ['Take all recommendations', true]);
  eq(b.querySelectorAll('.kit-btn').length, 3);
});
