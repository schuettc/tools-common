import { test, eq, assert, fixture } from './harness.js';
import { shareMenu, sharedLine, SHARE_LABEL, SHARE_REPLY_MS } from '/_kit/share.js';

const items = [
  { label: 'Download HTML', act: 'download-html' },
  { label: 'Share HTML', act: 'share-html' },
  { label: 'Copy link', act: 'copy-link' },
];

function setup(onPick = () => {}) {
  const bar = document.createElement('div');
  bar.style.cssText = 'display:flex;justify-content:flex-end;padding:20px';
  const button = document.createElement('button');
  button.textContent = SHARE_LABEL;
  bar.append(button);
  fixture().append(bar);
  return { button, menu: shareMenu(button, items, onPick) };
}

test('share: the menu holds the items in order, hidden until the button opens it', () => {
  const { button, menu } = setup();
  try {
    eq([...menu.el.querySelectorAll('[role="menuitem"]')].map(b => b.textContent), ['Download HTML', 'Share HTML', 'Copy link']);
    assert(menu.el.hidden, 'starts hidden');
    button.click();
    assert(!menu.el.hidden, 'the button opens it');
    button.click();
    assert(menu.el.hidden, 'the button closes it again');
  } finally { menu.destroy(); }
});

test('share: it opens right-aligned under its button', () => {
  const { button, menu } = setup();
  try {
    button.click();
    const b = button.getBoundingClientRect(), m = menu.el.getBoundingClientRect();
    assert(Math.abs(m.right - b.right) < 1, `menu right ${m.right}, button right ${b.right}`);
    assert(m.top >= b.bottom, `menu top ${m.top}, button bottom ${b.bottom}`);
  } finally { menu.destroy(); }
});

test('share: a pick calls back with its item and closes the menu', () => {
  const picked = [];
  const { button, menu } = setup((act, item) => picked.push([act, item.label]));
  try {
    button.click();
    menu.el.querySelectorAll('[role="menuitem"]')[1].click();
    eq(picked, [['share-html', 'Share HTML']]);
    assert(menu.el.hidden, 'closed after the pick');
  } finally { menu.destroy(); }
});

test('share: an outside click, Escape and a resize close it', () => {
  const { button, menu } = setup();
  try {
    button.click();
    document.body.click();
    assert(menu.el.hidden, 'outside click');
    button.click();
    const e = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true });
    document.body.dispatchEvent(e);
    assert(menu.el.hidden, 'Escape');
    button.click();
    window.dispatchEvent(new Event('resize'));
    assert(menu.el.hidden, 'resize');
  } finally { menu.destroy(); }
  assert(!menu.el.isConnected, 'destroy removes the menu');
});

test('share: a heading is a label over its items, not pressable and out of the keyboard', () => {
  const picked = [];
  const button = document.createElement('button');
  fixture().append(button);
  const menu = shareMenu(button, [
    { label: 'This page', heading: true },
    { label: 'Download Markdown', act: 'md' },
    { label: 'Everything', heading: true },
    { label: 'Download HTML', act: 'html' },
  ], (act) => picked.push(act));
  try {
    button.click();
    eq([...menu.el.children].map(c => c.textContent), ['This page', 'Download Markdown', 'Everything', 'Download HTML']);
    const rows = [...menu.el.querySelectorAll('[role="menuitem"]')];
    eq(rows.map(b => b.textContent), ['Download Markdown', 'Download HTML']);
    const heading = menu.el.children[0];
    assert(heading.matches('.kit-share-heading'), `heading is ${heading.outerHTML}`);
    eq(heading.tagName, 'DIV');
    eq(heading.tabIndex, -1);
    heading.click();
    assert(!menu.el.hidden, 'pressing a heading leaves the menu open');
    eq(picked, []);
    rows[1].click();
    eq(picked, ['html']);
  } finally { menu.destroy(); }
});

test('share: sharedLine names the file copied', () => {
  eq(sharedLine('/Users/x/docs/plan.html'), 'plan.html copied · paste to attach');
  eq(SHARE_REPLY_MS, 5000);
});

test("share: a row keeps the page's line height, so a page's menu is as tall as before", () => {
  document.body.style.lineHeight = '1.6';
  const { button, menu } = setup();
  try {
    button.click();
    const row = menu.el.querySelector('[role="menuitem"]');
    const cs = getComputedStyle(row);
    eq(cs.fontSize, '12px');
    assert(Math.abs(parseFloat(cs.lineHeight) - 19.2) < 0.01, `line height ${cs.lineHeight}`);
  } finally { menu.destroy(); document.body.style.lineHeight = ''; }
});
