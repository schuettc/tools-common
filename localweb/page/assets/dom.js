// DOM builders for the kit's components. Each returns plain elements styled by
// kit.css; the CSS classes are the contract and also work from static HTML.

// h creates an element. attrs: class → className, on<event> → listener,
// dataset → data-*, booleans set or remove the attribute; string children are
// text (never HTML). null, undefined and false children are skipped.
export function h(tag, attrs, ...children) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v === undefined || v === null) continue;
    if (k === 'class') el.className = v;
    else if (k === 'dataset') Object.assign(el.dataset, v);
    else if (k.startsWith('on') && typeof v === 'function') el.addEventListener(k.slice(2), v);
    else if (typeof v === 'boolean') {
      if (k in el) el[k] = v;
      else el.toggleAttribute(k, v);
    } else if (k in el && k !== 'list' && k !== 'form') el[k] = v;
    else el.setAttribute(k, v);
  }
  for (const c of children.flat()) {
    if (c === null || c === undefined || c === false) continue;
    el.append(c instanceof Node ? c : String(c));
  }
  return el;
}

const count = n => (n === undefined || n === null || n === '' ? null : h('span', { class: 'kit-n' }, String(n)));

const LIVE_TEXT = { live: 'live', polling: 'polling', down: 'offline' };

// bar is the top bar: brand, section controls with counts, status text, the
// live pill, theme, and the one filled primary button.
export function bar(o) {
  const ctls = new Map();
  for (const s of o.sections || []) {
    ctls.set(s.id, h('button', { class: 'kit-ctl', type: 'button', dataset: { id: s.id }, onclick: () => o.onSection && o.onSection(s.id) }, s.label, count(s.count)));
  }
  const status = h('span', { class: 'kit-status' }, o.status || '');
  const pill = h('span', { class: 'kit-live', hidden: true, role: 'status' });
  const themeControl = o.theme || h('button', { class: 'kit-ctl', type: 'button' }, 'theme');
  const primary = h('button', { class: 'kit-primary', type: 'button', hidden: true });
  let run = null;
  primary.addEventListener('click', () => run && run());

  const handle = {
    el: h('header', { class: 'kit-bar' },
      h('span', { class: 'kit-brand' }, o.brand.mark || null, o.brand.name),
      ...ctls.values(), status, h('span', { class: 'kit-sp' }), pill, themeControl, primary),
    themeControl,
    setSection(id) {
      for (const [k, el] of ctls) {
        el.classList.toggle('on', k === id);
        if (k === id) el.setAttribute('aria-current', 'page');
        else el.removeAttribute('aria-current');
      }
    },
    setCount(id, n) {
      const el = ctls.get(id);
      if (!el) return;
      el.querySelector('.kit-n')?.remove();
      const c = count(n);
      if (c) el.append(c);
    },
    setStatus(text) {
      status.textContent = text;
    },
    setLive(s) {
      pill.hidden = false;
      pill.dataset.state = s;
      pill.replaceChildren(h('i'), LIVE_TEXT[s] || s);
    },
    setPrimary(p) {
      run = p ? p.run : null;
      primary.hidden = !p;
      primary.textContent = p ? p.label : '';
    },
  };
  handle.setSection(o.active);
  handle.setPrimary(o.primary || null);
  return handle;
}

function chipRow(group, chips, onChip) {
  return (chips || []).map(c =>
    h('button', { class: 'kit-chip' + (c.on ? ' on' : ''), type: 'button', dataset: { id: c.id }, onclick: () => onChip && onChip(group, c.id) }, c.label, count(c.count)));
}

// list is the list panel: a section label, view and filter chips, rows, and
// a footer. It satisfies the keyboard layer's ListNav. The cursor (.cur) is
// where j/k are; the open row (.open) is the one in the reading column.
export function list(o) {
  let items = [];
  let rows = [];
  let cur = -1;
  let opened = -1;
  let anchor = -1;
  const sel = new Set();
  const views = h('div', { class: 'kit-chips', dataset: { group: 'view' } });
  const filters = h('div', { class: 'kit-chips', dataset: { group: 'filter' } });
  const body = h('div', { class: 'kit-rows', role: 'list' });
  const el = h('aside', { class: 'kit-list' },
    h('div', { class: 'kit-lh' }, h('div', { class: 'kit-eyebrow' }, o.label), views, filters),
    body,
    o.foot ? h('div', { class: 'kit-foot' }, o.foot) : null);

  const keyOf = (item, i) => {
    const r = o.row(item);
    return r.key !== undefined ? r.key + '\u0000' + r.title : 'i' + i;
  };
  const clamp = i => Math.max(0, Math.min(items.length - 1, i));

  function paint() {
    rows.forEach((r, i) => {
      r.classList.toggle('cur', i === cur);
      r.classList.toggle('open', i === opened);
      r.classList.toggle('sel', sel.has(i));
      if (i === opened) r.setAttribute('aria-current', 'true');
      else r.removeAttribute('aria-current');
      const box = r.querySelector('.kit-box');
      if (box) box.setAttribute('aria-checked', String(sel.has(i)));
    });
  }

  function selectedChanged() {
    paint();
    if (o.onSelect) o.onSelect(handle.selected());
  }

  function render() {
    rows = items.map((item, i) => {
      const r = o.row(item);
      const box = r.selectable
        ? h('span', { class: 'kit-box', role: 'checkbox', 'aria-checked': 'false', onclick: e => { e.stopPropagation(); e.shiftKey ? handle.toggleRange(i) : handle.toggle(i); } })
        : null;
      return h('div', { class: 'kit-row' + (box ? ' has-box' : ''), role: 'listitem', onclick: e => (e.shiftKey && box ? handle.toggleRange(i) : handle.open(i)) },
        box,
        h('div', { class: 'kit-row-main' },
          r.key !== undefined ? h('div', { class: 'kit-kicker' }, r.key) : null,
          h('div', { class: 'kit-title' }, r.title),
          r.sub ? h('div', { class: 'kit-sub' }, r.sub) : null),
        r.meta !== undefined ? h('span', { class: 'kit-meta' }, r.meta) : null);
    });
    body.replaceChildren(...rows);
    paint();
  }

  const handle = {
    el,
    setItems(next) {
      const old = items;
      const openKey = opened >= 0 ? keyOf(old[opened], opened) : null;
      const curKey = cur >= 0 && old[cur] !== undefined ? keyOf(old[cur], cur) : null;
      const selKeys = new Set([...sel].map(i => keyOf(old[i], i)));
      items = next.slice();
      const keys = items.map(keyOf);
      opened = openKey === null ? -1 : keys.indexOf(openKey);
      cur = curKey === null ? -1 : keys.indexOf(curKey);
      if (cur < 0) cur = opened >= 0 ? opened : items.length ? 0 : -1;
      sel.clear();
      keys.forEach((k, i) => selKeys.has(k) && sel.add(i));
      anchor = -1;
      render();
    },
    setChips(group, chips) {
      (group === 'view' ? views : filters).replaceChildren(...chipRow(group, chips, o.onChip));
    },
    move(d) {
      if (!items.length) return;
      cur = clamp((cur < 0 ? 0 : cur) + d);
      paint();
      rows[cur].scrollIntoView?.({ block: 'nearest' });
      if (o.openOnMove) handle.open(cur);
    },
    open(i = cur) {
      if (i < 0 || i >= items.length) return;
      cur = opened = i;
      paint();
      if (o.onOpen) o.onOpen(items[i], i);
    },
    toggle(i = cur) {
      if (i < 0 || i >= items.length || !o.row(items[i]).selectable) return;
      if (sel.has(i)) sel.delete(i);
      else sel.add(i);
      anchor = i;
      selectedChanged();
    },
    toggleRange(i = cur) {
      if (i < 0 || i >= items.length) return;
      if (anchor < 0) return handle.toggle(i);
      const on = sel.has(anchor);
      const [a, b] = anchor < i ? [anchor, i] : [i, anchor];
      for (let j = a; j <= b; j++) {
        if (!o.row(items[j]).selectable) continue;
        if (on) sel.add(j);
        else sel.delete(j);
      }
      anchor = i;
      selectedChanged();
    },
    selected: () => [...sel].sort((a, b) => a - b).map(i => items[i]),
    current: () => cur,
  };
  handle.setChips('view', o.views);
  handle.setChips('filter', o.filters);
  return handle;
}

// facts is the reading column's facts line: muted labels, bold values.
export function facts(pairs) {
  return h('div', { class: 'kit-facts' }, pairs.map(([label, value]) =>
    h('span', null, label, ' ', value instanceof Node && value.tagName === 'B' ? value : h('b', null, value))));
}

// buttons is a row of lowercase mono buttons: fill marks the one primary,
// danger is a destructive verb (red text).
export function buttons(bs) {
  return h('div', { class: 'kit-btns' }, bs.map(b =>
    h('button', {
      class: 'kit-btn' + (b.fill ? ' fill' : '') + (b.danger ? ' danger' : ''),
      type: 'button',
      disabled: !!b.disabled,
      onclick: () => b.run && b.run(),
    }, b.label)));
}

// card is a colour-edged card that holds its own actions. edge: signal (needs
// you), agent (the agent's), wait (queued / draft).
export function card(o) {
  return h('div', { class: 'kit-card' + (o.edge ? ' edge-' + o.edge : '') },
    o.head ? h('div', { class: 'kit-card-head' }, o.head) : null,
    o.body !== undefined ? h('div', { class: 'kit-card-body' }, o.body) : null,
    o.actions && o.actions.length ? buttons(o.actions) : null);
}

// codeBlock shows source with line numbers: a gutter <pre> and a source <pre>,
// two text nodes however long the file. It scrolls sideways, never wraps.
export function codeBlock(src, o = {}) {
  const start = o.start ?? 1;
  const lines = src.split(/\r?\n/);
  if (lines.length > 1 && lines[lines.length - 1] === '') lines.pop();
  const text = lines.join('\n');
  const gutter = lines.map((_, i) => start + i).join('\n');
  return h('div', { class: 'kit-code' },
    h('pre', { class: 'kit-gutter', 'aria-hidden': 'true' }, gutter),
    h('pre', { class: 'kit-src' }, o.highlight ? o.highlight(text) : text));
}

// fold is a collapsible section: a small-caps label with a disclosure.
export function fold(label, content, o = {}) {
  return h('details', { class: 'kit-fold', open: !!o.open },
    h('summary', { class: 'kit-label' }, label),
    h('div', { class: 'kit-fold-body' }, content));
}

// noteField is an inline note: ↵ or blur commits a changed value, Esc
// reverts and leaves. While it has focus the keyboard layer ignores page keys.
export function noteField(o) {
  let committed = o.value || '';
  const input = h('input', { class: 'kit-note', type: 'text', value: committed, placeholder: o.placeholder || '' });
  const commit = () => {
    if (input.value === committed) return;
    committed = input.value;
    o.onCommit(committed);
  };
  input.addEventListener('keydown', e => {
    if (e.key === 'Enter' && !e.isComposing) {
      e.preventDefault();
      commit();
    } else if (e.key === 'Escape') {
      input.value = committed;
      input.blur();
    }
  });
  input.addEventListener('blur', commit);
  return input;
}
