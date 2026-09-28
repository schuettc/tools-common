import { test, eq, assert, fixture } from './harness.js';
import { h, bar, list, facts, card, buttons, codeBlock, fold, noteField } from '/_kit/dom.js';
import { createKeys } from '/_kit/keys.js';
import { initTheme } from '/_kit/theme.js';

const cs = el => getComputedStyle(el);

test('dom: h escapes text and wires attributes', () => {
  let clicked = 0;
  const p = h('p', { class: 'a b', onclick: () => clicked++, dataset: { id: '7' }, hidden: false, title: 't' }, '<b>', null, false, h('i', null, 'x'));
  eq(p.innerHTML, '&lt;b&gt;<i>x</i>');
  eq([p.className, p.dataset.id, p.title, p.hasAttribute('hidden')], ['a b', '7', 't', false]);
  p.click();
  eq(clicked, 1);
  eq(h('input', { disabled: true }).disabled, true);
});

test('dom: bar', () => {
  let section = '', pressed = 0;
  const b = bar({
    brand: { name: 'cull' },
    sections: [{ id: 'label', label: 'label', count: '37/150' }, { id: 'eval', label: 'eval' }],
    active: 'label',
    onSection: id => (section = id),
    status: 'synced',
    primary: { label: 'Next unlabeled', run: () => pressed++ },
  });
  fixture().append(b.el);
  const el = b.el;
  assert(el.classList.contains('kit-bar'));
  eq(el.querySelector('.kit-brand').textContent, 'cull');
  const ctl = el.querySelector('.kit-ctl[data-id="label"]');
  eq(ctl.textContent, 'label37/150');
  assert(ctl.classList.contains('on'), 'active section on');
  b.setCount('label', '38/150');
  eq(ctl.querySelector('.kit-n').textContent, '38/150');
  el.querySelector('.kit-ctl[data-id="eval"]').click();
  eq(section, 'eval');
  b.setSection('eval');
  assert(!ctl.classList.contains('on') && el.querySelector('[data-id="eval"]').classList.contains('on'), 'setSection moves on');
  eq(el.querySelectorAll('.kit-primary').length, 1);
  el.querySelector('.kit-primary').click();
  eq(pressed, 1);
  b.setPrimary(null);
  assert(el.querySelector('.kit-primary').hidden, 'primary hidden');
  b.setPrimary({ label: 'Activate', run() {} });
  const prim = el.querySelector('.kit-primary');
  eq([prim.hidden, prim.textContent], [false, 'Activate']);
  const pill = el.querySelector('.kit-live');
  assert(pill.hidden, 'pill hidden until a live client reports');
  b.setLive('polling');
  eq([pill.hidden, pill.dataset.state, pill.textContent], [false, 'polling', 'polling']);
  b.setLive('down');
  eq(pill.textContent, 'offline');
  b.setStatus('offline · 2 queued');
  eq(el.querySelector('.kit-status').textContent, 'offline · 2 queued');
  assert(b.themeControl && b.themeControl.textContent === 'theme', 'theme control');
});

function makeList(extra = {}) {
  const opened = [], sels = [];
  const items = ['a', 'b', 'c', 'd', 'e'].map(k => ({ k }));
  const l = list({
    label: 'label',
    views: [{ id: 'unlabeled', label: 'unlabeled', count: 5, on: true }, { id: 'keep', label: 'keep' }],
    row: it => ({ key: `go · ${it.k}_test.go`, title: `Test${it.k}`, meta: '1d', selectable: true }),
    onOpen: (it, i) => opened.push([it.k, i]),
    onSelect: s => sels.push(s.map(x => x.k)),
    foot: h('span', null, 'foot'),
    ...extra,
  });
  l.setItems(items);
  fixture().append(l.el);
  return { l, opened, sels, items };
}

test('dom: list selection and open', () => {
  const { l, opened, sels } = makeList();
  const rows = () => [...l.el.querySelectorAll('.kit-row')];
  eq(rows().length, 5);
  eq(l.current(), 0);
  l.move(1); l.move(1);
  eq(l.current(), 2);
  assert(rows()[2].classList.contains('cur'), 'cursor row marked');
  eq(opened.length, 0, 'moving does not open by default');
  l.open();
  assert(rows()[2].classList.contains('open') && rows()[2].getAttribute('aria-current') === 'true', 'open row');
  eq(opened, [['c', 2]]);
  l.move(10);
  eq(l.current(), 4, 'clamped');
  l.move(-10);
  eq(l.current(), 0, 'clamped low');
  l.toggle(2);
  l.toggleRange(4);
  eq(l.selected().map(x => x.k), ['c', 'd', 'e']);
  assert(rows()[3].classList.contains('sel') && rows()[3].querySelector('.kit-box'), 'selected row and box');
  eq(sels.at(-1), ['c', 'd', 'e']);
  rows()[1].click();
  eq(opened.at(-1), ['b', 1], 'row click opens');
  rows()[0].querySelector('.kit-box').click();
  eq(l.selected().map(x => x.k), ['a', 'c', 'd', 'e'], 'box click toggles, does not open');
  eq(opened.length, 2);
  eq(l.el.querySelector('.kit-kicker').textContent, 'go · a_test.go');
  eq(l.el.querySelector('.kit-title').textContent, 'Testa');
  eq(l.el.querySelector('.kit-foot').textContent, 'foot');
});

test('dom: list keeps open and selection across setItems by key', () => {
  const { l, items } = makeList();
  l.open(3);
  l.toggle(1);
  l.setItems([{ k: 'z' }, ...items]);
  eq(l.el.querySelector('.kit-row.open .kit-title').textContent, 'Testd');
  eq(l.selected().map(x => x.k), ['b']);
});

test('dom: list openOnMove and chips', () => {
  let chip = null;
  const { l, opened } = makeList({ openOnMove: true, onChip: (g, id) => (chip = [g, id]) });
  l.move(1);
  eq(opened, [['b', 1]]);
  const chips = l.el.querySelectorAll('.kit-chip');
  eq(chips.length, 2);
  assert(chips[0].classList.contains('on'));
  chips[1].click();
  eq(chip, ['view', 'keep']);
  l.setChips('view', [{ id: 'keep', label: 'keep', count: 3, on: true }]);
  eq(l.el.querySelector('.kit-chip.on').textContent, 'keep3');
});

test('dom: list drives the keyboard layer', () => {
  const { l, opened } = makeList();
  const keys = createKeys({ list: l });
  try {
    for (const k of ['j', 'j', 'o']) document.body.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true }));
    eq(opened, [['c', 2]]);
  } finally { keys.destroy(); }
});

test('dom: reading column pieces', () => {
  const f = facts([['status', 'new'], ['age', h('b', null, '39d')]]);
  assert(f.classList.contains('kit-facts'));
  eq(f.querySelectorAll('b').length, 2);
  let accepted = 0;
  const c = card({ edge: 'agent', head: 'pi proposes · keep', body: 'Still valid.', actions: [{ label: 'accept', run: () => accepted++, fill: true }, { label: 'reject' }] });
  assert(c.classList.contains('kit-card') && c.classList.contains('edge-agent'));
  eq(c.querySelector('.kit-card-head').textContent, 'pi proposes · keep');
  c.querySelector('.kit-btn.fill').click();
  eq(accepted, 1);
  const bs = buttons([{ label: 'close', danger: true, run() {} }, { label: 'keep', disabled: true, run() {} }]);
  assert(bs.classList.contains('kit-btns') && bs.firstChild.classList.contains('danger') && bs.lastChild.disabled);
});

test('dom: codeBlock line counts', () => {
  const lines = el => el.querySelector('.kit-gutter').textContent.split('\n').length;
  eq(lines(codeBlock('a\r\nb\n')), 2, 'CRLF and trailing newline');
  eq(lines(codeBlock('a\n\nb')), 3, 'blank line kept');
  eq(lines(codeBlock('')), 1, 'empty source is one empty line');
  const src = Array.from({ length: 600 }, (_, i) => `\tfunc line${i}() { return "${'x'.repeat(20)}" }`).join('\n') + '\n';
  assert(src.length > 21000, `size ${src.length}`);
  const big = codeBlock(src, { start: 10 });
  eq(lines(big), 600);
  eq(big.querySelectorAll('pre').length, 2);
  eq(big.querySelector('.kit-gutter').textContent.split('\n')[0], '10');
  eq(big.querySelector('.kit-src').textContent, src.replace(/\n$/, ''));
  const hl = codeBlock('x := 1', { highlight: s => h('span', { class: 'kw' }, s) });
  assert(hl.querySelector('.kit-src .kw'), 'highlight hook used');
  fixture().append(big);
  const pre = big.querySelector('.kit-src');
  eq(cs(pre).whiteSpace, 'pre');
  eq(cs(big).overflowX, 'auto');
});

test('dom: fold', () => {
  const f = fold('setup', h('p', null, 'body'));
  eq(f.tagName, 'DETAILS');
  assert(f.classList.contains('kit-fold') && !f.open);
  eq(f.querySelector('summary.kit-label').textContent, 'setup');
  f.open = true;
  eq(f.querySelector('p').textContent, 'body');
  assert(fold('x', 'y', { open: true }).open);
});

test('dom: noteField', () => {
  const commits = [];
  const n = noteField({ value: 'old', placeholder: 'note', onCommit: v => commits.push(v) });
  let ran = 0;
  const keys = createKeys();
  keys.register({ keys: '1', label: 'keep', run: () => ran++ });
  try {
    fixture().append(n);
    assert(n.classList.contains('kit-note'));
    n.focus();
    n.value = 'flaky on CI';
    n.dispatchEvent(new KeyboardEvent('keydown', { key: '1', bubbles: true, cancelable: true }));
    eq(ran, 0, 'page key does not fire in the note');
    n.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }));
    eq(commits, ['flaky on CI']);
    n.blur();
    eq(commits.length, 1, 'unchanged blur does not recommit');
    n.focus();
    n.value = 'scratch';
    n.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true }));
    eq(n.value, 'flaky on CI', 'Esc reverts');
    assert(document.activeElement !== n, 'and blurs');
    n.focus();
    n.value = 'by blur';
    n.blur();
    eq(commits, ['flaky on CI', 'by blur']);
  } finally { keys.destroy(); }
});

test('dom: styles apply', () => {
  initTheme('dom').set('light');
  const fx = fixture();
  const b = bar({ brand: { name: 'x' }, sections: [{ id: 's', label: 's' }], primary: { label: 'Go', run() {} } });
  const c = card({ head: 'h', body: 'b' });
  const btn = buttons([{ label: 'b', run() {} }]).firstChild;
  const l = list({ label: 'l', row: i => ({ title: i }) });
  l.setItems(['one']);
  fx.append(b.el, c, btn, l.el);
  eq(cs(b.el.querySelector('.kit-primary')).backgroundColor, 'rgb(164, 81, 42)');
  eq(cs(c).borderRadius, '8px');
  eq(cs(btn).borderRadius, '6px');
  assert(!/Mono|monospace/.test(cs(l.el.querySelector('.kit-title')).fontFamily), 'titles are sans');
  assert(/Mono|monospace/.test(cs(b.el.querySelector('.kit-ctl')).fontFamily), 'controls are mono');
  eq(cs(l.el.querySelector('.kit-title')).fontSize, '14.5px');
  initTheme('dom').set('system');
});

test('dom: setItems reports a selection it changed', () => {
  const sels = [];
  const l = list({ label: 'l', row: it => ({ id: it.id, key: it.id, title: it.title, selectable: true }), onSelect: s => sels.push(s.map(x => x.id)) });
  l.setItems([{ id: 'a', title: 'A' }, { id: 'b', title: 'B' }, { id: 'c', title: 'C' }]);
  l.toggle(0); l.toggle(1); l.open(2);
  sels.length = 0;
  l.setItems([{ id: 'a', title: 'A retitled' }, { id: 'c', title: 'C' }]);
  eq(l.selected().map(x => x.id), ['a'], 'id keeps a through a title edit; b is not rendered');
  eq(l.selectedIds(), ['a', 'b'], 'b stays selected by id (paging), until deselected');
  eq(sels, [['a']], 'onSelect told about the change');
  eq(l.el.querySelector('.kit-row.open .kit-title').textContent, 'C', 'open row kept by id');
  sels.length = 0;
  l.setItems([{ id: 'a', title: 'A retitled' }, { id: 'c', title: 'C' }]);
  eq(sels, [], 'no call when nothing changed');
});

test('dom: a lost cursor stays near where it was', () => {
  const l = list({ label: 'l', row: it => ({ key: it, title: it }) });
  l.setItems(['a', 'b', 'c', 'd']);
  l.move(2);
  l.setItems(['a', 'b', 'd']);
  eq(l.current(), 2, 'cursor keeps its index when its row vanished');
});

test('dom: bar stale pill and status tone', () => {
  const b = bar({ brand: { name: 'x' }, staleText: 'restarted · continued in a new tab' });
  fixture().append(b.el);
  b.setLive('stale');
  const pill = b.el.querySelector('.kit-live');
  eq([pill.dataset.state, pill.textContent], ['stale', 'restarted · continued in a new tab']);
  b.setLive('down', 'offline · 3 queued');
  eq(pill.textContent, 'offline · 3 queued', 'text override');
  eq(bar({ brand: { name: 'y' } }).el.querySelector('.kit-live').hidden, true);
  const plain = bar({ brand: { name: 'z' } });
  plain.setLive('stale');
  eq(plain.el.querySelector('.kit-live').textContent, 'stale', 'default stale text');

  initTheme('dom').set('light');
  b.setStatus('offline · 3 queued', { tone: 'danger' });
  const st = b.el.querySelector('.kit-status');
  eq(st.dataset.tone, 'danger');
  eq(cs(st).color, 'rgb(194, 42, 42)');
  b.setStatus('synced', { tone: 'signal' });
  eq(cs(st).color, 'rgb(164, 81, 42)');
  b.setStatus('synced');
  assert(!st.hasAttribute('data-tone'), 'no tone = muted default');
  eq(cs(st).color, 'rgb(89, 96, 111)');
  initTheme('dom').set('system');
});

test('dom: selection by id beyond the rendered page', () => {
  const sels = [];
  const l = list({ label: 'l', row: it => ({ id: it, title: it, selectable: true }), onSelect: s => sels.push(s.length) });
  const page1 = Array.from({ length: 200 }, (_, i) => 'a' + i);
  const page2 = Array.from({ length: 200 }, (_, i) => 'b' + i);
  const all = [...page1, ...page2, 'c0'];
  l.setItems(page1);
  l.selectAll(all);
  eq(l.selectedIds().length, all.length, 'ids beyond the page count');
  eq(l.selected().length, 200, 'selected() is the rendered ones');
  eq(l.el.querySelectorAll('.kit-row.sel').length, 200);
  l.setItems(page2);
  eq(l.selectedIds().length, all.length, 'survives paging');
  eq(l.el.querySelectorAll('.kit-row.sel').length, 200);
  l.toggle(0);
  assert(!l.selectedIds().includes('b0'), 'toggle off by id');
  eq(l.selectedIds().length, all.length - 1);
  l.deselect(['a0', 'a1']);
  eq(l.selectedIds().length, all.length - 3, 'deselect drops ids off-page');
  l.clearSelection();
  eq([l.selectedIds().length, l.el.querySelectorAll('.kit-row.sel').length], [0, 0]);
  eq(sels.at(-1), 0, 'onSelect on clear');
});

test('dom: 500 rows render and move', () => {
  const l = list({ label: 'l', row: it => ({ id: it, key: 'k', title: it, selectable: true }) });
  fixture().append(l.el);
  const items = Array.from({ length: 500 }, (_, i) => 'r' + i);
  const t0 = performance.now();
  l.setItems(items);
  l.move(499); l.toggle(); l.toggleRange(0);
  const ms = performance.now() - t0;
  eq(l.el.querySelectorAll('.kit-row').length, 500);
  eq(l.selectedIds().length, 500);
  assert(ms < 1000, `500 rows took ${ms} ms`);
});

test('dom: app layout with a rail', () => {
  const fx = fixture();
  const app = h('div', { class: 'kit-app', style: 'width:1440px;height:900px' },
    bar({ brand: { name: 'x' } }).el,
    list({ label: 'l', row: i => ({ title: i }) }).el,
    h('main', { class: 'kit-read' }, 'read'),
    h('aside', { class: 'kit-rail' }, 'rail'));
  fx.append(app);
  const w = sel => Math.round(app.querySelector(sel).getBoundingClientRect().width);
  eq([w('.kit-bar'), w('.kit-list'), w('.kit-read'), w('.kit-rail')], [1440, 400, 680, 360]);
  const top = sel => Math.round(app.querySelector(sel).getBoundingClientRect().top - app.getBoundingClientRect().top);
  eq([top('.kit-list'), top('.kit-rail')], [52, 52], 'panes sit under the bar');
  const noRail = h('div', { class: 'kit-app', style: 'width:1000px;height:600px' },
    bar({ brand: { name: 'y' } }).el, list({ label: 'l', row: i => ({ title: i }) }).el, h('main', { class: 'kit-read' }));
  fx.append(noRail);
  eq(Math.round(noRail.querySelector('.kit-read').getBoundingClientRect().width), 600, 'rail is optional');
});

test('dom: setLive labels without a staleText', () => {
  const b = bar({ brand: { name: 'x' } });
  const pill = b.el.querySelector('.kit-live');
  for (const [s, want] of [['live', 'live'], ['polling', 'polling'], ['down', 'offline'], ['stale', 'stale']]) {
    b.setLive(s);
    eq(pill.textContent, want, s);
  }
  const t = bar({ brand: { name: 'y' }, staleText: 'restarted' });
  t.setLive('live');
  eq(t.el.querySelector('.kit-live').textContent, 'live', 'staleText only for stale');
});

test('dom: the app grid scrolls below its minimum width', () => {
  const app = h('div', { class: 'kit-app', style: 'width:600px;height:400px' },
    bar({ brand: { name: 'x' } }).el, list({ label: 'l', row: i => ({ title: i }) }).el,
    h('main', { class: 'kit-read' }), h('aside', { class: 'kit-rail' }));
  fixture().append(app);
  eq(cs(app).overflowX, 'auto');
  assert(app.scrollWidth > 600, `content keeps its width (${app.scrollWidth})`);
  assert(app.querySelector('.kit-read').getBoundingClientRect().width >= 320, 'reading column keeps a minimum');
});
