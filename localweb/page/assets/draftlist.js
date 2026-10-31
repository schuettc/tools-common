// draftlist.js: a list of drafts for the right column. Where each draft is
// stored is the tool's: the list shows what it is given and says what changed.
import { buttons, h } from './dom.js';
import { submitOnEnter } from './enter.js';

// makeRow is one draft ({ id, quote, text, meta }): the item it quotes, its words
// in a box, and delete. The row marks itself when pressed, so onSelect needn't
// redraw. update takes what the tool now says of the draft without touching
// words the reader hasn't saved; forget lets the words go unsaved when the row leaves.
function makeRow({ draft, on, onEdit, onDelete, onSelect }) {
  const { id } = draft;
  let saved = draft.text;
  const save = () => {
    if (box.value === saved) return;
    saved = box.value;
    if (onEdit) onEdit(id, saved);
  };
  const forget = () => {
    saved = box.value;
  };
  const select = () => {
    for (const r of li.parentElement?.querySelectorAll('.kit-draft-row.on') ?? []) r.classList.remove('on');
    li.classList.add('on');
    if (onSelect) onSelect(id);
  };
  const box = h('textarea', { class: 'kit-draft-words', rows: 1, value: draft.text, 'aria-label': `Your words on ${draft.quote}` });
  submitOnEnter(box, () => {
    save();
    box.blur();
  });
  box.addEventListener('blur', save);
  box.addEventListener('focus', () => {
    if (!li.classList.contains('on')) select();
  });
  const foot = buttons([{ label: 'Delete', run: () => {
    forget(); // the words go with the draft: its leaving the page must not save them
    if (onDelete) onDelete(id);
  } }]);
  // The box keeps the focus through the press: a save on its blur could redraw
  // the row between the button's press and its release, and lose the click.
  foot.addEventListener('mousedown', e => e.preventDefault());
  const quote = h('button', { class: 'kit-draft-quote', type: 'button' }, draft.quote);
  let meta = draft.meta ? h('span', { class: 'kit-meta' }, draft.meta) : null;
  const li = h('li', { class: `kit-draft-row${on ? ' on' : ''}`, 'data-draft-id': id }, quote, box, meta, foot);
  li.addEventListener('click', e => {
    if (e.target !== box && !foot.contains(e.target)) select();
  });
  const update = (next, nextOn) => {
    li.classList.toggle('on', nextOn);
    quote.textContent = next.quote;
    box.setAttribute('aria-label', `Your words on ${next.quote}`);
    if (next.meta && !meta) {
      meta = h('span', { class: 'kit-meta' });
      li.insertBefore(meta, foot);
    }
    if (next.meta) {
      meta.textContent = next.meta;
    } else if (meta) {
      meta.remove();
      meta = null;
    }
    if (box.value === saved && box.value !== next.text) box.value = next.text;
    saved = next.text;
  };
  return { li, update, forget };
}

// draftRow is one draft's row, on its own.
export function draftRow(o) {
  return makeRow(o).li;
}

// place puts the rows in order. A node that moves loses the focus, so the others
// move around the row that holds it, and a row already in place isn't touched.
function place(ul, lis) {
  const held = lis.find(li => li.contains(document.activeElement)) ?? lis[0];
  if (held.parentElement !== ul) ul.append(held);
  const at = lis.indexOf(held);
  let next = held;
  for (const li of lis.slice(0, at).reverse()) {
    if (li.nextSibling !== next) ul.insertBefore(li, next);
    next = li;
  }
  let prev = held;
  for (const li of lis.slice(at + 1)) {
    if (prev.nextSibling !== li) ul.insertBefore(li, prev.nextSibling);
    prev = li;
  }
}

// draftList draws the rows, or the tool's empty line when there are none.
// selected is the id of the row to mark. redraw({ drafts, selected }) draws again:
// a row whose draft id is still there is the same element, with its box, caret and
// unsaved words, so a live update can arrive while the reader types.
export function draftList({ drafts, empty, selected, onEdit, onDelete, onSelect }) {
  const rows = new Map();
  const ul = h('ul', { class: 'kit-draft-rows' });
  const none = h('p', { class: 'kit-empty' }, empty);
  const el = h('div', { class: 'kit-draft-list' });
  const redraw = ({ drafts, selected }) => {
    const ids = new Set(drafts.map(d => d.id));
    for (const [id, row] of rows) {
      if (ids.has(id)) continue;
      row.forget(); // removing a focused box can fire its blur
      row.li.remove();
      rows.delete(id);
    }
    for (const draft of drafts) {
      const on = draft.id === selected;
      if (rows.has(draft.id)) rows.get(draft.id).update(draft, on);
      else rows.set(draft.id, makeRow({ draft, on, onEdit, onDelete, onSelect }));
    }
    if (!drafts.length) {
      el.replaceChildren(none);
      return;
    }
    if (ul.parentElement !== el) el.replaceChildren(ul);
    place(ul, drafts.map(d => rows.get(d.id).li));
  };
  redraw({ drafts, selected });
  return { el, redraw };
}
