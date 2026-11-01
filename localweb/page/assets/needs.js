// needs.js: getting to what needs the reader. needsPill is the header's count
// as a button, nextLine the line after an answer, arrive what opening a thing
// does on screen. What is on the list, and what opening an item shows, are the
// tool's own.
import { h } from './dom.js';

const words = n => `${n} ${n === 1 ? 'needs' : 'need'} you`;

const same = (a, b) => JSON.stringify(a.map(({ id, label, detail }) => [id, label, detail]))
  === JSON.stringify(b.map(({ id, label, detail }) => [id, label, detail]));

// needsPill takes items ({ id, label, detail }) in the order to answer them and
// open, which hears the id to open. It returns { el, set }: one item opens on a
// press, several open a menu naming each, none hides it. Mount el again on a
// redraw and call set with the items: the same items leave an open menu alone,
// so a live update takes nothing from under the pointer.
export function needsPill({ items, open }) {
  const el = h('button', { class: 'kit-needs', type: 'button' });
  let now = [];
  let menu = null;

  const row = it => h('button', {
    class: 'kit-also-row kit-needs-row', type: 'button', role: 'menuitem',
    onclick: () => { close(); open(it.id); },
  }, h('span', { class: 'kit-also-name' }, it.label), it.detail ? h('span', { class: 'kit-also-detail' }, it.detail) : null);

  const onKey = e => { if (e.key === 'Escape' && !e.isComposing) close(); };
  const onDown = e => { if (!menu.contains(e.target) && !el.contains(e.target)) close(); };
  function close() {
    if (!menu) return;
    menu.remove();
    menu = null;
    el.setAttribute('aria-expanded', 'false');
    document.removeEventListener('keydown', onKey, true);
    document.removeEventListener('pointerdown', onDown, true);
  }
  function show() {
    menu = h('div', { class: 'kit-float kit-also-menu kit-needs-menu', role: 'menu' }, now.map(row));
    document.body.append(menu);
    const r = el.getBoundingClientRect();
    menu.style.top = `${r.bottom + 4}px`;
    menu.style.left = `${Math.max(8, Math.min(r.left, document.documentElement.clientWidth - menu.offsetWidth - 8))}px`;
    el.setAttribute('aria-expanded', 'true');
    document.addEventListener('keydown', onKey, true);
    document.addEventListener('pointerdown', onDown, true);
  }
  el.addEventListener('click', () => {
    if (now.length === 1) open(now[0].id);
    else if (menu) close();
    else show();
  });

  function set(next) {
    if (same(now, next) && el.textContent) return;
    now = next.slice();
    const several = now.length > 1;
    el.hidden = !now.length;
    el.textContent = `${words(now.length)} ${several ? '▾' : '→'}`;
    if (several) {
      el.setAttribute('aria-haspopup', 'menu');
      el.setAttribute('aria-expanded', String(!!menu));
      menu?.replaceChildren(...now.map(row));
    } else {
      close();
      el.removeAttribute('aria-haspopup');
      el.removeAttribute('aria-expanded');
    }
  }
  set(items);
  return { el, set, close };
}

// nextLine is what follows an answer: how many are left, and a button naming
// next ({ id, label }) that calls open. With none left it says so, with no
// button. It never moves the page itself.
export function nextLine({ left, next, open }) {
  return h('div', { class: 'kit-next', role: 'status' },
    h('span', { class: 'kit-next-words' }, left > 0 ? `✓ Sent. ${left} more ${left === 1 ? 'needs' : 'need'} you.` : '✓ Sent. Nothing else needs you.'),
    left > 0 && next
      ? h('button', { class: 'kit-needs', type: 'button', onclick: () => open(next.id) }, `Next: ${next.label} →`)
      : null);
}

// arrive shows el, with its answer control on screen: the element marked
// data-answer, else a text box in it, else nothing more than el itself. On one
// too tall for the screen the control wins. A text box is focused.
export function arrive(el) {
  const box = 'textarea, input[type=text]';
  const control = el.matches('[data-answer]') || el.matches(box)
    ? el
    : el.querySelector('[data-answer]') ?? el.querySelector(box);
  el.scrollIntoView({ block: 'start' });
  if (!control) return;
  control.scrollIntoView({ block: 'nearest' });
  const reply = control.matches(box) ? control : control.querySelector(box);
  if (reply) reply.focus({ preventScroll: true });
}
