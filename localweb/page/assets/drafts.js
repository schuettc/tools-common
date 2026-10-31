// drafts.js: what the reader says before it goes. Choices and notes stay drafts
// through every redraw until Send takes them together; the question buttons go
// at once, and say so. The action names and what a batch holds are the caller's.
import { buttons, h } from './dom.js';

// The three questions a card can always be asked.
export const QUESTIONS = {
  explain: 'Explain this more',
  smaller: 'Is there a smaller option?',
  breaks: "What breaks if we don't build this yet?",
};

// questionButton is a button for buttons(): once asked it reads Sent and stays
// off until the agent answers, so a second press can't queue a copy.
export function questionButton({ label, asked, run }) {
  return asked ? { label: `✓ Sent: ${label}`, disabled: true, run: () => {} } : { label, run };
}

function boxesOf(key) {
  return document.querySelectorAll(`[data-draft="${CSS.escape(key)}"]`);
}

// createDrafts keeps text by key and choices in picks, apart from the page, so
// a box drawn again for a key holds what was typed. wrap(box, key), when given,
// is what holds the box (a tool's thumbnails under it); it returns the element.
export function createDrafts(o = {}) {
  const texts = new Map();
  const picks = new Map();
  const take = key => {
    const v = texts.get(key) ?? '';
    texts.delete(key);
    for (const t of boxesOf(key)) t.value = '';
    return v;
  };
  return {
    picks,
    box(key, placeholder, rows = 2) {
      const t = h('textarea', { class: 'kit-note kit-draft', rows, placeholder, 'data-draft': key, 'aria-label': placeholder });
      t.value = texts.get(key) ?? '';
      t.addEventListener('input', () => texts.set(key, t.value));
      return { el: o.wrap ? o.wrap(t, key) : h('div', { class: 'kit-draft-box' }, t), box: t };
    },
    get: key => texts.get(key) ?? '',
    keys: () => texts.keys(),
    take,
    restore(key, text) {
      texts.set(key, text);
      for (const t of boxesOf(key)) t.value = text;
    },
    notes(prefix) {
      const out = new Map();
      for (const [k, v] of texts) if (k.startsWith(prefix)) out.set(k.slice(prefix.length), v);
      return out;
    },
    dirty: () => picks.size > 0 || [...texts.values()].some(v => v.trim() !== ''),
    discard(prefix) {
      picks.clear();
      for (const k of [...texts.keys()]) if (k.startsWith(prefix)) take(k);
    },
  };
}

// sendBar is the bar at the foot of a page of drafts: count and hint are its
// words, sends how many go. discard and send are the caller's; extra buttons go
// before them.
export function sendBar(o) {
  return h('div', { class: 'kit-sendbar' },
    h('span', { class: 'kit-sendbar-count' }, o.count),
    h('span', { class: 'kit-sendbar-hint' }, o.hint),
    buttons([
      ...(o.extra ?? []),
      { label: 'Discard drafts', disabled: o.sends === 0, run: o.discard },
      { label: `Send ${o.sends}`, fill: true, disabled: o.sends === 0, run: o.send },
    ]));
}
