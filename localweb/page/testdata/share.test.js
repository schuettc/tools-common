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

test('share: sharedLine names the file copied', () => {
  eq(sharedLine('/Users/x/docs/plan.html'), 'plan.html copied · paste to attach');
  eq(SHARE_REPLY_MS, 5000);
});
