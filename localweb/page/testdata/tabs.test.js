import { test, eq, assert, fixture } from './harness.js';
import { railTabs } from '/_kit/tabs.js';
import { frame } from '/_kit/frame.js';

const KEY = 'tabtest.railTab';

function clean() {
  localStorage.removeItem(KEY);
  for (const k of Object.keys(localStorage)) if (k.startsWith('tabtest.collapsed.')) localStorage.removeItem(k);
}

const make = (extra = {}) => {
  const a = document.createElement('p');
  const b = document.createElement('p');
  const t = railTabs({
    name: 'tabtest',
    tabs: [{ id: 'drafts', label: 'Drafts', count: 2, body: a }, { id: 'chat', label: 'Chat', count: 1, body: b }],
    ...extra,
  });
  return { t, a, b };
};
const tabs = t => [...t.el.querySelectorAll('[role=tab]')];
const shown = t => [...t.el.querySelectorAll('[role=tabpanel]')].filter(p => !p.hidden);

test('railTabs: a row of tabs, each with its label and count', () => {
  clean();
  const { t } = make();
  eq(tabs(t).map(b => b.textContent), ['Drafts2', 'Chat1']);
  eq(tabs(t).map(b => b.querySelector('.kit-n').textContent), ['2', '1']);
  eq(t.el.querySelector('[role=tablist]').className, 'kit-rail-tabrow');
});

test('railTabs: one body shows, the first with nothing said', () => {
  clean();
  const { t, a } = make();
  eq(shown(t).length, 1);
  assert(shown(t)[0].firstChild === a, 'the first tab is open');
  eq(tabs(t).map(b => b.getAttribute('aria-selected')), ['true', 'false']);
  eq(tabs(t).map(b => b.classList.contains('on')), [true, false]);
});

test('railTabs: open names the tab to start on', () => {
  clean();
  const { t, b } = make({ open: 'chat' });
  assert(shown(t)[0].firstChild === b, 'chat is open');
});

test('railTabs: a press opens its tab, hides the other and tells the tool', () => {
  clean();
  const heard = [];
  const { t, b } = make({ onOpen: id => heard.push(id) });
  tabs(t)[1].click();
  eq(shown(t).length, 1);
  assert(shown(t)[0].firstChild === b, 'chat is open');
  eq(heard, ['chat']);
});

test('railTabs: a body keeps what it holds while hidden', () => {
  clean();
  const box = document.createElement('textarea');
  const t = railTabs({ name: 'tabtest', tabs: [{ id: 'a', label: 'A', body: box }, { id: 'b', label: 'B', body: document.createElement('p') }] });
  box.value = 'half a sentence';
  t.setOpen('b');
  t.setOpen('a');
  eq(t.el.querySelector('textarea').value, 'half a sentence');
  assert(t.el.querySelector('textarea') === box, 'the same box');
});

test('railTabs: setOpen opens a tab without telling the tool, and an unknown one changes nothing', () => {
  clean();
  const heard = [];
  const { t, a, b } = make({ onOpen: id => heard.push(id) });
  t.setOpen('chat');
  assert(shown(t)[0].firstChild === b, 'chat is open');
  eq(heard, []);
  t.setOpen('chat');
  t.setOpen('nope');
  assert(shown(t)[0].firstChild === b, 'still chat');
  t.setOpen('drafts');
  assert(shown(t)[0].firstChild === a, 'back to drafts');
});

test('railTabs: setCount changes a count, 0 shows and nothing hides it', () => {
  clean();
  const { t } = make();
  const n = () => tabs(t)[1].querySelector('.kit-n');
  t.setCount('chat', 7);
  eq(n().textContent, '7');
  t.setCount('chat', 0);
  eq([n().textContent, n().hidden], ['0', false]);
  t.setCount('chat', null);
  eq(n().hidden, true);
  t.setCount('chat', 3);
  eq([n().textContent, n().hidden], ['3', false]);
  t.setCount('nope', 3);
});

test('railTabs: a tab made with no count draws none', () => {
  clean();
  const t = railTabs({ name: 'tabtest', tabs: [{ id: 'a', label: 'A', body: document.createElement('p') }] });
  fixture().append(t.el);
  eq(tabs(t)[0].querySelector('.kit-n').offsetWidth, 0);
});

test('railTabs: the open tab is remembered per tool, over open', () => {
  clean();
  tabs(make().t)[1].click();
  eq(localStorage.getItem(KEY), 'chat');
  const { t, b } = make({ open: 'drafts' });
  assert(shown(t)[0].firstChild === b, 'the pressed tab comes back');
  make().t.setOpen('drafts');
  eq(localStorage.getItem(KEY), 'drafts');
  const other = document.createElement('p');
  const t2 = railTabs({ name: 'tabother', tabs: [{ id: 'x', label: 'X', body: other }] });
  eq(shown(t2).length, 1);
  eq(localStorage.getItem('tabother.railTab'), null);
});

test('railTabs: a remembered tab that is gone falls back to open', () => {
  clean();
  localStorage.setItem(KEY, 'gone');
  const { t, b } = make({ open: 'chat' });
  assert(shown(t)[0].firstChild === b, 'chat is open');
});

test('railTabs: storage that throws costs only the remembering', () => {
  clean();
  const real = Object.getOwnPropertyDescriptor(window, 'localStorage');
  Object.defineProperty(window, 'localStorage', { configurable: true, get() { throw new DOMException('denied', 'SecurityError'); } });
  try {
    const { t, b } = make();
    tabs(t)[1].click();
    assert(shown(t)[0].firstChild === b, 'chat opens');
  } finally {
    Object.defineProperty(window, 'localStorage', real);
  }
});

test('railTabs: under the toggle line, folded with the column, the same tab open again', () => {
  clean();
  const f = frame({ name: 'tabtest', centre: { label: 'Cards' }, right: { panel: 'thread', label: 'Thread' } });
  f.right.style.transition = 'none';
  const { t, b } = make({ open: 'chat' });
  f.rightBody.append(t.el);
  const app = document.createElement('div');
  app.className = 'kit-app';
  app.append(document.createElement('div'), document.createElement('div'), f.centre, f.right);
  fixture().append(app);
  const row = t.el.querySelector('[role=tablist]');
  assert(row.getBoundingClientRect().top >= f.right.firstChild.getBoundingClientRect().bottom, 'the row is under the toggle');
  f.setRightEmpty(true, 'C1');
  eq(row.offsetWidth, 0, 'folded');
  f.setRightEmpty(false, 'C2');
  assert(row.offsetWidth > 0, 'open');
  assert(shown(t)[0].firstChild === b, 'chat is still open');
});
