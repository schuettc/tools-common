// frame.js: the page's three columns, the index, the centre and the thread.
// Each side folds to a thin strip with its toggle, and the page remembers it
// per tool and panel. The columns go straight into a .kit-app grid, after its
// bar. narrowMode shows one column at a time on a page that turns it on.
import { h } from './dom.js';
import { remembered } from './remembered.js';

// What narrowMode needs of a column collapsible drew: its toggle and how to
// redraw it. A column in held is narrow mode's: its press and face are the state's.
const drawn = new WeakMap();
const held = new WeakMap();

// collapsible puts a toggle and body in col. side says which way it folds and
// name (the tool's) and panel make the key it is remembered under,
// <name>.collapsed.<panel>; folded starts it folded whatever the page remembers.
// onPress hears each press of the toggle. It returns a function that folds or
// opens the side without remembering it.
export function collapsible({ col, body, panel, side, name, folded, onPress }) {
  const key = `${name}.collapsed.${panel}`;
  const toggle = h('button', { class: 'kit-col-toggle', type: 'button', 'data-panel': panel });
  const face = () => {
    const narrow = held.get(col);
    const on = narrow ? !narrow.open() : col.classList.contains('kit-collapsed');
    toggle.textContent = on === (side === 'left') ? '›' : '‹';
    toggle.title = on ? `Show the ${panel}` : `Hide the ${panel}`;
    toggle.setAttribute('aria-expanded', String(!on));
  };
  const set = (on, keep = true) => {
    col.classList.toggle('kit-collapsed', on);
    face();
    if (keep) remembered.set(key, on ? '1' : '');
  };
  drawn.set(col, face);
  toggle.addEventListener('click', () => {
    const narrow = held.get(col);
    if (narrow) {
      narrow.press();
      return;
    }
    set(!col.classList.contains('kit-collapsed'));
    if (onPress) onPress();
  });
  col.append(toggle, body);
  if (folded) set(true, false);
  else set(remembered.get(key) === '1');
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
    remembered.set(pressedOn, shown);
  });
  const mid = h('main', { class: 'kit-read kit-col kit-mid', 'aria-label': centre.label });
  const setRightEmpty = (empty, card = '') => {
    if (!r) return;
    shown = card;
    if (pressed === undefined) pressed = remembered.get(pressedOn) === card ? card : null;
    if (pressed === card) return;
    if (pressed !== null && remembered.get(pressedOn) === pressed) remembered.remove(pressedOn);
    pressed = null;
    foldRight(empty);
  };
  return { left: l, leftBody, centre: mid, right: r, rightBody, setRightEmpty };
}

// narrowMode shows one thing at a time at 1080 wide or less: the centre, or a
// side (a column collapsible drew) in the centre's place, the other a strip. The
// class kit-narrow drives the layout, so the fold and what is remembered of it
// keep their meaning for the wide page: while narrow they are ignored and never
// written. A page opens on the centre; leaving narrow goes back to it. show(side)
// takes 'left', 'right' or null, setCount(side, n) puts a count under a strip's
// toggle (none at zero), onShow(side) hears each change, and stop lets go of the
// window.
export function narrowMode({ root, left, right, onShow }) {
  const cols = { left, right };
  const narrow = matchMedia('(width <= 1080px)');
  let shown = null;
  const redraw = () => {
    for (const col of [left, right]) if (col) drawn.get(col)();
  };
  const show = side => {
    const next = narrow.matches && cols[side] ? side : null;
    if (next === shown) return;
    shown = next;
    root.classList.toggle('kit-narrow-left', shown === 'left');
    root.classList.toggle('kit-narrow-right', shown === 'right');
    redraw();
    if (onShow) onShow(shown);
  };
  const hold = (side, on) => {
    const col = cols[side];
    if (!col) return;
    if (on) held.set(col, { open: () => shown === side, press: () => show(shown === side ? null : side) });
    else held.delete(col);
  };
  const follow = () => {
    root.classList.toggle('kit-narrow', narrow.matches);
    hold('left', narrow.matches);
    hold('right', narrow.matches);
    if (!narrow.matches) show(null);
    redraw();
  };
  narrow.addEventListener('change', follow);
  follow();
  const setCount = (side, n) => {
    const col = cols[side];
    if (!col) return;
    let badge = col.querySelector(':scope > .kit-col-count');
    if (!n) {
      if (badge) badge.remove();
      return;
    }
    if (!badge) {
      badge = h('span', { class: 'kit-col-count' });
      col.firstElementChild.after(badge);
    }
    badge.textContent = String(n);
  };
  return { show, setCount, stop: () => narrow.removeEventListener('change', follow) };
}
