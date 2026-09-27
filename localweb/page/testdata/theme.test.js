import { test, eq, assert, fixture } from './harness.js';
import { initTheme } from '/_kit/theme.js';

const root = document.documentElement;
const bg = () => getComputedStyle(document.body).backgroundColor;

function signalProbe() {
  const el = document.createElement('span');
  el.style.color = 'var(--kit-signal)';
  fixture().append(el);
  return () => getComputedStyle(el).color;
}

function reset() {
  localStorage.clear();
  root.removeAttribute('data-theme');
  root.style.removeProperty('--tool-signal-light');
}

test('theme: light and dark resolve tokens', () => {
  reset();
  const th = initTheme('t0');
  th.set('light');
  eq(bg(), 'rgb(244, 245, 248)', 'light bg');
  th.set('dark');
  eq(bg(), 'rgb(20, 22, 29)', 'dark bg');
  th.set('system');
});

test('theme: default signal is tackle rust', () => {
  reset();
  initTheme('t0').set('light');
  eq(signalProbe()(), 'rgb(164, 81, 42)');
  initTheme('t0').set('dark');
  eq(signalProbe()(), 'rgb(217, 143, 102)');
  reset();
});

test('theme: tool input sets signal', () => {
  reset();
  initTheme('t0').set('light');
  root.style.setProperty('--tool-signal-light', '#123456');
  eq(signalProbe()(), 'rgb(18, 52, 86)');
  reset();
});

test('theme: remembered', () => {
  reset();
  initTheme('t1').set('dark');
  eq(localStorage.getItem('t1.theme'), 'dark');
  eq(root.dataset.theme, 'dark');
  root.removeAttribute('data-theme');
  const again = initTheme('t1');
  eq(again.get(), 'dark');
  eq(root.dataset.theme, 'dark', 'init applies the remembered mode');
  again.set('system');
  eq(localStorage.getItem('t1.theme'), null);
  assert(!root.hasAttribute('data-theme'), 'system removes the attribute');
});

test('theme: cycle order', () => {
  reset();
  const th = initTheme('t3');
  eq([th.cycle(), th.cycle(), th.cycle()], ['light', 'dark', 'system']);
});

test('theme: control cycles and shows the mode', () => {
  reset();
  const btn = document.createElement('button');
  fixture().append(btn);
  const th = initTheme('t4', btn);
  btn.click();
  eq(th.get(), 'light');
  assert(btn.title.includes('light'), `title ${btn.title}`);
  reset();
});

test('theme: storage throws', () => {
  reset();
  const proto = Object.getPrototypeOf(localStorage);
  const { getItem, setItem, removeItem } = proto;
  proto.getItem = proto.setItem = proto.removeItem = () => { throw new Error('denied'); };
  try {
    const th = initTheme('t2');
    eq(th.get(), 'system');
    th.set('dark');
    eq(root.dataset.theme, 'dark', 'still applies without storage');
  } finally {
    Object.assign(proto, { getItem, setItem, removeItem });
    reset();
  }
});

test('theme: boot.js applies the remembered mode', async () => {
  reset();
  localStorage.setItem('t5.theme', 'dark');
  await new Promise((resolve, reject) => {
    const s = document.createElement('script');
    s.src = '/_kit/boot.js';
    s.dataset.tool = 't5';
    s.onload = resolve;
    s.onerror = reject;
    document.head.append(s);
  });
  eq(root.dataset.theme, 'dark');
  reset();
});
