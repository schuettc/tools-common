// frontpage.js: a front page's list, moved from diazo's. The parts take plain
// values: a tool works out its own rows, counts and groups, keeps what is
// chosen and redraws on a press.
import { h } from './dom.js';

// filterRow is a section of the left column: All with the total, then one row
// per item. Several can be chosen; All is on when none is. onToggle hears the
// value pressed, '' for All.
export function filterRow({ label, items, chosen, onToggle }) {
  const row = ({ value, label: text, count }) => {
    const on = value ? chosen.includes(value) : !chosen.length;
    return h('li', { class: `kit-index-row${on ? ' on' : ''}` },
      h('button', {
        class: 'kit-index-main kit-filter',
        type: 'button',
        'data-value': value,
        'aria-pressed': String(on),
        onclick: () => onToggle(value),
      },
      h('span', { class: 'kit-index-title' }, text),
      h('span', { class: 'kit-meta' }, String(count))));
  };
  const total = items.reduce((n, it) => n + it.count, 0);
  return h('section', { class: 'kit-index-section kit-filter-row' },
    h('div', { class: 'kit-label' }, label),
    h('ul', { class: 'kit-index-rows' }, row({ value: '', label: 'All', count: total }), items.map(row)));
}

// statusPills draws the pills as a stage track with no arrows, since statuses
// aren't a sequence. A pill with no count is hidden unless chosen. The value ''
// is the tool's All, on when none is chosen.
export function statusPills({ pills, chosen, onToggle, label = 'Filter by status' }) {
  const on = value => (value ? chosen.includes(value) : !chosen.length);
  return h('nav', { class: 'kit-stages', 'aria-label': label },
    pills.filter(p => on(p.value) || p.count).map(p =>
      h('button', {
        class: `kit-stage${on(p.value) ? ' now' : ''}`,
        type: 'button',
        'data-value': p.value,
        'aria-pressed': String(on(p.value)),
        onclick: () => onToggle(p.value),
      }, p.label, h('span', { class: 'kit-pill-n' }, String(p.count)))));
}

function group({ key, title, rows, empty, edge }, startFolded, onFold) {
  const head = h('button', { class: 'kit-label kit-group-head', type: 'button' }, `${title} · ${rows.length}`);
  const sec = h('section', { class: `kit-card kit-group${edge ? ` edge-${edge}` : ''}`, 'data-group': key },
    head,
    rows.length ? h('ul', { class: 'kit-group-rows' }, rows) : h('p', { class: 'kit-empty' }, empty));
  const set = on => {
    sec.classList.toggle('kit-folded', on);
    head.setAttribute('aria-expanded', String(!on));
  };
  head.addEventListener('click', () => {
    const on = !sec.classList.contains('kit-folded');
    set(on);
    onFold(key, on);
  });
  set(startFolded);
  return sec;
}

// groupList draws named groups of rows (each an <li>), each a card that folds
// in place. folded holds the keys that start folded; onFold hears each press
// with the key and whether it is now folded.
export function groupList({ groups, folded, onFold }) {
  return h('div', { class: 'kit-groups' }, groups.map(g => group(g, folded.includes(g.key), onFold)));
}
