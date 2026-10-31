// tabs.js: tabs in the right column, one body shown at a time. What each body
// holds is the tool's, and it stays in the page while hidden, so a half-written
// box keeps its words. The whole thing goes in the frame's rightBody, so
// setRightEmpty still folds the column around it.
import { h } from './dom.js';

// A sandboxed frame throws on touching localStorage. That costs the remembering
// and nothing else.
function recall(key) {
  try {
    return localStorage.getItem(key);
  } catch {
    return null;
  }
}

function remember(key, id) {
  try {
    localStorage.setItem(key, id);
  } catch {
    // not remembered
  }
}

// railTabs takes tabs ({ id, label, count, body }), open (the tab to start on
// when the page remembers none, default the first), name (the tool's, for the key
// it remembers under, <name>.railTab) and onOpen, which hears each press.
// setOpen(id) opens a tab from the tool, and is remembered too; setCount(id, n)
// changes a count, and null or undefined leaves none.
export function railTabs({ name, tabs, open, onOpen }) {
  const key = `${name}.railTab`;
  const parts = new Map(tabs.map(t => {
    const n = h('span', { class: 'kit-n' });
    const tab = h('button', { class: 'kit-ctl', type: 'button', role: 'tab', onclick: () => {
      setOpen(t.id);
      if (onOpen) onOpen(t.id);
    } }, t.label, n);
    const panel = h('div', { class: 'kit-rail-panel', role: 'tabpanel', 'aria-label': t.label }, t.body);
    return [t.id, { tab, panel, n }];
  }));
  const show = id => {
    for (const [each, p] of parts) {
      p.tab.classList.toggle('on', each === id);
      p.tab.setAttribute('aria-selected', String(each === id));
      p.panel.hidden = each !== id;
    }
  };
  const setOpen = id => {
    if (!parts.has(id)) return;
    show(id);
    remember(key, id);
  };
  const setCount = (id, n) => {
    const p = parts.get(id);
    if (!p) return;
    p.n.hidden = n === null || n === undefined;
    p.n.textContent = p.n.hidden ? '' : String(n);
  };
  for (const t of tabs) setCount(t.id, t.count);
  const start = [recall(key), open].find(id => parts.has(id));
  show(start ?? tabs[0]?.id);
  const each = [...parts.values()];
  const el = h('div', { class: 'kit-rail-tabs' },
    h('div', { class: 'kit-rail-tabrow', role: 'tablist' }, each.map(p => p.tab)),
    each.map(p => p.panel));
  return { el, setOpen, setCount };
}
