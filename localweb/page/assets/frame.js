// frame.js: the page's three columns, the index, the centre and the thread.
// Each side folds to a thin strip with its toggle, and the page remembers it
// per tool and panel. The columns go straight into a .kit-app grid, after its
// bar.
import { h } from './dom.js';

// collapsible puts a toggle and body in col. side says which way it folds and
// name (the tool's) and panel make the key it is remembered under,
// <name>.collapsed.<panel>; folded starts it folded whatever the page remembers.
// onPress hears each press of the toggle. It returns a function that folds or
// opens the side without remembering it.
export function collapsible({ col, body, panel, side, name, folded, onPress }) {
  const key = `${name}.collapsed.${panel}`;
  const toggle = h('button', { class: 'kit-col-toggle', type: 'button', 'data-panel': panel });
  const set = (on, keep = true) => {
    col.classList.toggle('kit-collapsed', on);
    toggle.textContent = on === (side === 'left') ? '›' : '‹';
    toggle.title = on ? `Show the ${panel}` : `Hide the ${panel}`;
    toggle.setAttribute('aria-expanded', String(!on));
    if (keep) localStorage.setItem(key, on ? '1' : '');
  };
  toggle.addEventListener('click', () => {
    set(!col.classList.contains('kit-collapsed'));
    if (onPress) onPress();
  });
  col.append(toggle, body);
  if (folded) set(true, false);
  else set(localStorage.getItem(key) === '1');
  return on => set(on, false);
}

function side(name, which, cls, o, onPress) {
  if (!o) return [null, null, null];
  const col = h('aside', { class: `${cls} kit-col kit-${which}-col`, 'aria-label': o.label });
  const body = h('div', { class: 'kit-col-body' });
  const fold = collapsible({ col, body, panel: o.panel, side: which, name, folded: o.folded, onPress });
  return [col, body, fold];
}

// frame builds the columns. left and right are { panel, label, folded }, each
// optional; centre is { label }. The caller draws into leftBody, centre and
// rightBody, and appends left, centre and right to its grid. setRightEmpty(empty,
// card) tells it whether the thread of the card in front of the reader (its id)
// has messages: an empty one folds, and its first message opens it. The id must
// be unique across the tool (e.g. `${folder}/${card}`). A press on the toggle wins
// until the card changes. The tab holds it; it is also kept beside the fold, read
// only on the first call, so a reload on that card holds it and a tab on another
// card can't undo it.
export function frame({ name, left, centre, right }) {
  const [l, leftBody] = side(name, 'left', 'kit-list', left);
  const pressedOn = `${name}.collapsed.${right ? right.panel : ''}.card`;
  let shown = '';
  let pressed; // the card this tab's press holds on, null for none; undefined before the first call
  const [r, rightBody, foldRight] = side(name, 'right', 'kit-rail', right, () => {
    pressed = shown;
    localStorage.setItem(pressedOn, shown);
  });
  const mid = h('main', { class: 'kit-read kit-col kit-mid', 'aria-label': centre.label });
  const setRightEmpty = (empty, card = '') => {
    if (!r) return;
    shown = card;
    if (pressed === undefined) pressed = localStorage.getItem(pressedOn) === card ? card : null;
    if (pressed === card) return;
    if (pressed !== null && localStorage.getItem(pressedOn) === pressed) localStorage.removeItem(pressedOn);
    pressed = null;
    foldRight(empty);
  };
  return { left: l, leftBody, centre: mid, right: r, rightBody, setRightEmpty };
}
