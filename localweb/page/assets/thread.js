// thread.js: the right column, the messages of what is in front of the reader
// and a box for one more. What a message says and links to, and what the note
// box holds, are the caller's.
import { buttons, h, messagePill } from './dom.js';
import { submitOnEnter } from './enter.js';

function when(at) {
  const d = new Date(at);
  return Number.isNaN(d.getTime()) ? at : d.toLocaleString([], { dateStyle: 'short', timeStyle: 'short' });
}

function linkTo(l) {
  return h('a', {
    class: 'kit-thread-ref',
    href: l.href,
    onclick: e => {
      e.preventDefault();
      l.open();
    },
  }, l.text);
}

// threadMessage is one message: who, its words and link, when, the state of one
// of yours, its text, then whatever attachments(m) draws.
export function threadMessage(m, attachments) {
  const court = m.from === 'court';
  return h('li', { class: `kit-thread-msg ${court ? 'court' : 'agent'}`, 'data-msg': String(m.id) },
    h('div', { class: 'kit-thread-msg-head' },
      h('b', null, court ? 'You' : 'Agent'),
      m.words || m.link
        ? h('span', null, m.words, m.link ? [m.words ? ' · ' : '', linkTo(m.link)] : null)
        : null,
      h('span', { class: 'kit-meta' }, when(m.at)),
      court && m.state ? messagePill(m.state, m.stateLine) : null),
    m.text ? h('p', { class: 'kit-thread-msg-text' }, m.text) : null,
    attachments ? attachments(m) : null);
}

// The key as a button that copies this page's address with #<target>.
function copyLink(key, target) {
  const b = h('button', { type: 'button', class: 'kit-label-link', title: 'Copy link' }, key);
  b.onclick = () =>
    void navigator.clipboard
      .writeText(`${location.origin}${location.pathname}${location.search}#${target}`)
      .then(() => {
        b.textContent = 'Copied';
        setTimeout(() => (b.textContent = key), 1500);
      });
  return b;
}

// thread is the column's content. With no id it holds only the empty words.
// key, when given, copies a link to #<link> (default the id); readOnly leaves it
// plain text and draws no note box. note is { el, box, send }: el goes in the
// column, and Enter in box or the Send note button calls send.
export function thread(o) {
  if (!o.id) return h('div', { class: 'kit-thread' }, h('p', { class: 'kit-empty' }, o.empty));
  const note = o.readOnly ? null : o.note;
  if (note?.send) submitOnEnter(note.box, note.send);
  return h('div', { class: 'kit-thread', 'data-thread': o.id },
    h('div', { class: 'kit-label' }, 'Thread'),
    h('p', { class: 'kit-thread-name' }, o.name),
    o.key
      ? h('p', { class: 'kit-key kit-thread-key' }, o.readOnly ? o.key : copyLink(o.key, o.link ?? o.id))
      : null,
    o.messages?.length
      ? h('ul', { class: 'kit-thread-msgs' }, o.messages.map(m => threadMessage(m, o.attachments)))
      : h('p', { class: 'kit-empty' }, 'No messages yet.'),
    note
      ? h('div', { class: 'kit-note-line' }, note.el, buttons([{ label: 'Send note', run: note.send }]))
      : null);
}
