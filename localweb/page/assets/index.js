// index.js: the left index, sections of rows that each open something, with a
// dot on what needs the reader. They know nothing about what a row is: the
// caller works out its attention and state.
import { h } from './dom.js';
import { lineTip, wireTip } from './tip.js';

// attentionDot is a row's needs-you dot, which says so in a tip.
export function attentionDot() {
  const dot = h('span', { class: 'kit-attn-dot', role: 'button', tabindex: '0', 'aria-label': 'needs you' });
  const tip = lineTip('Needs you');
  const box = h('span', { class: 'kit-tip-at kit-attn-at' }, dot, tip);
  wireTip(box, dot, tip);
  return box;
}

// indexRow is one row: a button that calls select(id) with its title and meta,
// then extra (nodes), then the dot when attention is set. on, attention, waits
// and draft are its classes.
export function indexRow(o) {
  const states = [o.on && ' on', o.attention && ' attn', o.waits && ' waits', o.draft && ' draft'];
  return h('li', { class: 'kit-index-row' + states.filter(Boolean).join(''), 'data-card': o.id },
    h('button', { class: 'kit-index-main', type: 'button', onclick: () => o.select && o.select(o.id) },
      h('span', { class: 'kit-index-title' }, o.title),
      o.meta ? h('span', { class: 'kit-meta' }, o.meta) : null),
    o.extra,
    o.attention ? attentionDot() : null);
}

// indexSection is a named group of rows, or its empty words with none. open,
// when given, turns the name into a link that calls it.
export function indexSection(o) {
  return h('section', { class: 'kit-index-section' },
    o.open
      ? h('button', { class: 'kit-label kit-label-link', type: 'button', onclick: o.open }, `${o.name} ›`)
      : h('div', { class: 'kit-label' }, o.name),
    o.rows.length
      ? h('ul', { class: 'kit-index-rows' }, o.rows)
      : h('p', { class: 'kit-empty' }, o.empty));
}
