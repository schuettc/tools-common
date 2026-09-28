// The keyboard layer: family defaults, per-page registrations, and the ?
// overlay, all from one registry so the overlay cannot disagree with what is
// bound.
//
// Key syntax: a single key ("j", "1", "?", "."), a named key ("↵", "↑", "↓",
// "Esc"), optional modifier prefixes ("⌃", "⌥", "⇧", "⌘", in any order), and
// sequences separated by spaces ("g a", 1 s apart at most).

import { sheet, sheetOpen } from './sheet.js';

const NAMED = { Enter: '↵', ArrowUp: '↑', ArrowDown: '↓', Escape: 'Esc' };
const MODS = ['⌃', '⌥', '⇧', '⌘'];
const SEQ_TIMEOUT = 1000;
const FIELD = 'input, textarea, select, [contenteditable=""], [contenteditable="true"]';
const PRESSABLE = 'button, a[href], summary, [role="button"]';

// canon puts one chord's modifiers in a fixed order: "⌘⇧x" → "⇧⌘x".
function canon(chord) {
  let rest = chord;
  const mods = new Set();
  while (rest.length > 1 && MODS.includes(rest[0])) {
    mods.add(rest[0]);
    rest = rest.slice(1);
  }
  const key = rest.length === 1 ? rest.toLowerCase() : rest;
  return MODS.filter(m => mods.has(m)).join('') + key;
}

function parse(keys) {
  const seq = keys.trim().split(/\s+/).map(canon);
  if (!seq.length || seq.some(c => !c)) throw new Error(`bad key: "${keys}"`);
  return seq;
}

// chordOf maps a keydown to a chord, or null for a bare modifier press. Shift
// is part of the chord only where it didn't already choose the character: "?"
// stays "?", but a shifted letter is "⇧x".
function chordOf(e) {
  if (['Shift', 'Meta', 'Control', 'Alt'].includes(e.key)) return null;
  let key = NAMED[e.key] || e.key;
  // ⌥ changes the character on macOS (⌥a is "å"): match the physical key.
  const phys = e.altKey && /^(Key[A-Z]|Digit[0-9])$/.exec(e.code || '');
  if (phys) key = e.code.slice(-1);
  const letter = /^[a-z]$/i.test(key);
  if (key.length === 1) key = key.toLowerCase();
  const shift = e.shiftKey && (letter || key.length > 1);
  return (e.ctrlKey ? '⌃' : '') + (e.altKey ? '⌥' : '') + (shift ? '⇧' : '') + (e.metaKey ? '⌘' : '') + key;
}

const startsWith = (a, b) => b.length <= a.length && b.every((c, i) => a[i] === c);

export function createKeys(opts = {}) {
  const target = opts.target || document;
  const now = opts.now || (() => performance.now());
  const list = opts.list;
  const bound = [];
  let pending = [];
  let last = 0;
  let overlay = null;

  function add(b) {
    const seq = parse(b.keys);
    const clash = bound.find(o => startsWith(o.seq, seq) || startsWith(seq, o.seq));
    if (clash) throw new Error(`key clash: "${b.keys}" is taken by "${clash.keys}" (${clash.label})`);
    const entry = { ...b, seq, group: b.group || 'page' };
    bound.push(entry);
    return () => {
      const i = bound.indexOf(entry);
      if (i >= 0) bound.splice(i, 1);
    };
  }

  const fam = (keys, label, run) => add({ keys, label, run, group: 'family' });
  if (list) {
    fam('j', 'next', () => list.move(1));
    fam('↓', 'next', () => list.move(1));
    fam('k', 'previous', () => list.move(-1));
    fam('↑', 'previous', () => list.move(-1));
    fam('o', 'open', () => list.open());
    fam('↵', 'open', () => list.open());
    fam('x', 'select', () => list.toggle());
    fam('⇧x', 'select range', () => list.toggleRange());
  }
  fam('?', 'show keys', () => showHelp());
  // Esc with a sheet (or the ? overlay) open is the sheet's; here there is nothing to close.
  fam('Esc', 'close / leave field', () => false);

  function fire(b, e) {
    if (b.run(e) !== false) e.preventDefault();
  }

  function onKey(e) {
    if (e.isComposing || e.keyCode === 229 || e.defaultPrevented) return;
    const chord = chordOf(e);
    if (!chord) return;
    if (overlay && chord === '?') {
      showHelp(false); // ? toggles its own sheet; Esc is the sheet's
      e.preventDefault();
      return;
    }
    if (sheetOpen()) return; // a sheet is modal: page keys wait
    const el = e.target instanceof Element ? e.target : null;
    if (el && el.closest(FIELD)) {
      pending = [];
      if (chord === 'Esc') {
        el.blur();
        e.preventDefault();
        return;
      }
      const b = bound.find(o => o.inField && o.seq.length === 1 && o.seq[0] === chord);
      if (b) fire(b, e);
      return;
    }
    if ((chord === '↵' || chord === ' ') && el && el.closest(PRESSABLE)) return;

    const t = now();
    const tryseq = seq => {
      const exact = bound.find(o => o.seq.length === seq.length && startsWith(o.seq, seq));
      if (exact) {
        pending = [];
        fire(exact, e);
        return true;
      }
      if (bound.some(o => o.seq.length > seq.length && startsWith(o.seq, seq))) {
        pending = seq;
        last = t;
        e.preventDefault();
        return true;
      }
      return false;
    };
    const held = pending.length && t - last <= SEQ_TIMEOUT ? pending : [];
    if (held.length && tryseq([...held, chord])) return;
    pending = [];
    tryseq([chord]);
  }

  function groups() {
    const out = new Map();
    for (const b of bound) {
      const g = out.get(b.group) || new Map();
      const row = g.get(b.label) || [];
      row.push(b.keys);
      g.set(b.label, row);
      out.set(b.group, g);
    }
    return out;
  }

  function showHelp(open = !overlay) {
    if (!open) {
      if (overlay) overlay.close();
      return;
    }
    if (overlay || sheetOpen()) return;
    const body = document.createElement('div');
    for (const [group, rows] of groups()) {
      const g = document.createElement('div');
      g.className = 'kit-label';
      g.textContent = group;
      body.append(g);
      for (const [label, keys] of rows) {
        const row = document.createElement('div');
        row.className = 'kit-keys-row';
        const k = document.createElement('span');
        k.className = 'kit-keys-k';
        for (const key of keys) {
          const kbd = document.createElement('kbd');
          kbd.textContent = key;
          k.append(kbd);
        }
        const l = document.createElement('span');
        l.textContent = label;
        row.append(k, l);
        body.append(row);
      }
    }
    overlay = sheet({ title: 'keyboard shortcuts', body, onClose: () => (overlay = null) });
    overlay.el.classList.add('kit-keys');
    body.addEventListener('click', () => showHelp(false));
  }

  target.addEventListener('keydown', onKey);

  const keys = {
    register(b) {
      return add(b);
    },
    showHelp,
    bindings: () => bound.map(b => ({ keys: b.keys, label: b.label, group: b.group })),
    destroy() {
      target.removeEventListener('keydown', onKey);
      showHelp(false);
      bound.length = 0;
    },
  };
  return keys;
}
