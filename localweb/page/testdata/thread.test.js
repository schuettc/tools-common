import { test, eq, assert, fixture } from './harness.js';
import { thread, threadMessage } from '/_kit/thread.js';

const AT = '2026-10-30T12:00:00Z';
const noteBox = () => {
  const box = document.createElement('textarea');
  return { el: box, box };
};

test('threadMessage: yours is drawn as You with where it stands', () => {
  const m = threadMessage({ id: 7, from: 'court', words: 'your note', at: AT, text: 'Why this?', state: 'working', stateLine: 'reading it' });
  eq([m.tagName, m.className, m.dataset.msg], ['LI', 'kit-thread-msg court', '7']);
  const head = m.querySelector('.kit-thread-msg-head');
  eq(head.children[0].textContent, 'You');
  eq(head.children[1].textContent, 'your note');
  assert(head.querySelector('.kit-meta').textContent, 'when it went');
  const pill = head.querySelector('.kit-pill');
  eq([pill.dataset.state, pill.textContent], ['working', 'working']);
  eq(head.querySelector('.kit-msg-line').textContent, 'reading it');
  eq(m.querySelector('.kit-thread-msg-text').textContent, 'Why this?');
});

test('threadMessage: the agent has no state to show', () => {
  const m = threadMessage({ id: 8, from: 'agent', at: AT, text: 'Because.', state: 'answered' });
  eq(m.className, 'kit-thread-msg agent');
  eq(m.querySelector('b').textContent, 'Agent');
  assert(!m.querySelector('.kit-pill'), 'no pill');
  eq(m.querySelector('.kit-thread-msg-head').children.length, 2);
});

test('threadMessage: a date it cannot read shows as it came', () => {
  eq(threadMessage({ id: 1, from: 'agent', at: 'soon' }).querySelector('.kit-meta').textContent, 'soon');
});

test('threadMessage: a link goes after the words and opens through the caller', () => {
  let opened = 0;
  const m = threadMessage({ id: 1, from: 'court', at: AT, words: 'your note', link: { text: 'DCSN1', href: '#DCSN1', open: () => opened++ } });
  const words = m.querySelector('.kit-thread-msg-head').children[1];
  eq(words.textContent, 'your note · DCSN1');
  const a = words.querySelector('a');
  eq([a.className, a.getAttribute('href')], ['kit-thread-ref', '#DCSN1']);
  const e = new MouseEvent('click', { cancelable: true, bubbles: true });
  a.dispatchEvent(e);
  eq([opened, e.defaultPrevented], [1, true]);
});

test('threadMessage: attachments come from the caller, under its text', () => {
  const shot = document.createElement('figure');
  const m = threadMessage({ id: 1, from: 'court', at: AT, text: 'see', shot }, v => v.shot);
  assert(m.lastChild === shot, 'last in the message');
});

test('thread: its parts in order, with one message each', () => {
  const t = thread({ id: 'DCSN1', name: 'Offline sends', key: 'a/b/DCSN1', messages: [{ id: 1, from: 'agent', at: AT, text: 'x' }, { id: 2, from: 'court', at: AT, text: 'y' }], note: noteBox() });
  eq([t.className, t.dataset.thread], ['kit-thread', 'DCSN1']);
  eq([...t.children].map(c => c.className), ['kit-label', 'kit-thread-name', 'kit-key kit-thread-key', 'kit-thread-msgs', 'kit-note-line']);
  eq([t.children[0].textContent, t.children[1].textContent], ['Thread', 'Offline sends']);
  eq(t.querySelectorAll('.kit-thread-msg').length, 2);
});

test('thread: attachments reach each message', () => {
  const t = thread({ id: 'a', name: 'n', messages: [{ id: 1, from: 'court', at: AT }], attachments: () => document.createElement('figure') });
  assert(t.querySelector('.kit-thread-msg figure'), 'drawn');
});

test('thread: with no id, it asks for a card', () => {
  const t = thread({ empty: 'Select a card to see its thread.' });
  eq([t.className, t.children.length, t.firstChild.className, t.textContent], ['kit-thread', 1, 'kit-empty', 'Select a card to see its thread.']);
});

test('thread: no messages says so', () => {
  const t = thread({ id: 'a', name: 'n', messages: [] });
  eq(t.querySelector('.kit-empty').textContent, 'No messages yet.');
});

test('thread: no key, no key line', () => {
  assert(!thread({ id: 'decisions', name: 'n', messages: [] }).querySelector('.kit-thread-key'), 'none');
});

test('thread: the key copies the link to the card, then says so', async () => {
  const wrote = [];
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText: async s => wrote.push(s) } });
  try {
    const t = thread({ id: 'plan', link: 'overview', name: 'n', key: 'a/b/plan', messages: [] });
    const b = t.querySelector('.kit-thread-key button');
    eq([b.className, b.title, b.textContent], ['kit-label-link', 'Copy link', 'a/b/plan']);
    b.click();
    await Promise.resolve();
    await Promise.resolve();
    eq(wrote, [`${location.origin}${location.pathname}${location.search}#overview`]);
    eq(b.textContent, 'Copied');
  } finally {
    delete navigator.clipboard;
  }
});

test('thread: read only, the key is plain and there is no note box', () => {
  const t = thread({ id: 'a', name: 'n', key: 'a/b/c', messages: [], readOnly: true, note: noteBox() });
  eq(t.querySelector('.kit-thread-key').textContent, 'a/b/c');
  assert(!t.querySelector('.kit-thread-key button'), 'no copy');
  assert(!t.querySelector('.kit-note-line'), 'no note');
});

test('thread: Enter in the note box sends, and so does its button', () => {
  let sent = 0;
  const note = { ...noteBox(), send: () => sent++ };
  const t = thread({ id: 'a', name: 'n', messages: [], note });
  note.box.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }));
  eq(sent, 1);
  const b = t.querySelector('.kit-note-line .kit-btn');
  eq(b.textContent, 'Send note');
  b.click();
  eq(sent, 2);
});

test('thread: the column draws messages with their edge', () => {
  const t = thread({ id: 'a', name: 'n', messages: [{ id: 1, from: 'agent', at: AT }] });
  fixture().append(t);
  eq(getComputedStyle(t.querySelector('.kit-thread-msg')).borderLeftWidth, '3px');
});

test('thread: with no messages given, it says there are none', () => {
  const t = thread({ id: 'a', name: 'Example' });
  eq(t.querySelector('.kit-empty').textContent, 'No messages yet.');
});
