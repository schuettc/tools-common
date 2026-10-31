// draftlist.js: a list of drafts for the right column. Where each draft is
// stored is the tool's: the list shows what it is given and says what changed.
import { buttons, h } from './dom.js';
import { submitOnEnter } from './enter.js';

// draftRow is one draft ({ id, quote, text, meta }): the item it quotes, its words
// in a box, and delete. The row marks itself when pressed, so onSelect needn't
// redraw, and a redraw there would take the box from under the reader's click.
export function draftRow({ draft, on, onEdit, onDelete, onSelect }) {
  let saved = draft.text;
  const save = () => {
    if (box.value === saved) return;
    saved = box.value;
    if (onEdit) onEdit(draft.id, saved);
  };
  const select = () => {
    for (const r of li.parentElement?.querySelectorAll('.kit-draft-row.on') ?? []) r.classList.remove('on');
    li.classList.add('on');
    if (onSelect) onSelect(draft.id);
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
    saved = box.value; // the words go with the draft: its leaving the page must not save them
    if (onDelete) onDelete(draft.id);
  } }]);
  // The box keeps the focus through the press: a save on its blur could redraw
  // the row between the button's press and its release, and lose the click.
  foot.addEventListener('mousedown', e => e.preventDefault());
  const li = h('li', { class: `kit-draft-row${on ? ' on' : ''}`, 'data-draft-id': draft.id },
    h('button', { class: 'kit-draft-quote', type: 'button' }, draft.quote),
    box,
    draft.meta ? h('span', { class: 'kit-meta' }, draft.meta) : null,
    foot);
  li.addEventListener('click', e => {
    if (e.target !== box && !foot.contains(e.target)) select();
  });
  return li;
}

// draftList draws the rows, or the tool's empty line when there are none.
// selected is the id of the row to mark.
export function draftList({ drafts, empty, onEdit, onDelete, onSelect, selected }) {
  if (!drafts.length) return h('div', { class: 'kit-draft-list' }, h('p', { class: 'kit-empty' }, empty));
  return h('div', { class: 'kit-draft-list' },
    h('ul', { class: 'kit-draft-rows' }, drafts.map(draft =>
      draftRow({ draft, on: draft.id === selected, onEdit, onDelete, onSelect }))));
}
