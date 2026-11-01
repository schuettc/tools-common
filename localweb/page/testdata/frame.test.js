import { test, eq, assert, fixture } from './harness.js';
import { frame, collapsible, narrowMode } from '/_kit/frame.js';
import { lineTip, wireTip } from '/_kit/tip.js';

const parts = (extra = {}) => ({
  name: 'tooltest',
  left: { panel: 'index', label: 'The plan' },
  centre: { label: 'Cards' },
  right: { panel: 'thread', label: 'Thread' },
  ...extra,
});

function clean() {
  for (const k of Object.keys(localStorage)) if (k.includes('.collapsed.')) localStorage.removeItem(k);
}

function mount(f) {
  const app = document.createElement('div');
  app.className = 'kit-app';
  app.append(document.createElement('div'), f.left, f.centre, f.right);
  fixture().append(app);
  return app;
}

test('frame: three columns of the app grid, each side with its toggle and body', () => {
  clean();
  const f = frame(parts());
  eq([f.left.tagName, f.centre.tagName, f.right.tagName], ['ASIDE', 'MAIN', 'ASIDE']);
  eq([f.left.className, f.centre.className, f.right.className],
    ['kit-list kit-col kit-left-col', 'kit-read kit-col kit-mid', 'kit-rail kit-col kit-right-col']);
  eq([f.left.getAttribute('aria-label'), f.centre.getAttribute('aria-label'), f.right.getAttribute('aria-label')], ['The plan', 'Cards', 'Thread']);
  eq([...f.left.children].map(c => c.className), ['kit-col-toggle', 'kit-col-body']);
  assert(f.left.lastChild === f.leftBody && f.right.lastChild === f.rightBody, 'the bodies are the handles');
  eq(f.left.firstChild.dataset.panel, 'index');
});

test('frame: a side the caller leaves out is not drawn', () => {
  clean();
  const f = frame(parts({ right: undefined }));
  assert(f.right === null && f.rightBody === null, 'no right column');
  assert(f.left && f.centre, 'the others are there');
});

test('frame: the toggle folds its side and says so', () => {
  clean();
  const f = frame(parts());
  mount(f);
  f.left.style.transition = 'none'; // the fold's slide would hide the width
  const t = f.left.firstChild;
  eq([t.textContent, t.title, t.getAttribute('aria-expanded')], ['‹', 'Hide the index', 'true']);
  eq(f.right.firstChild.textContent, '›');
  t.click();
  assert(f.left.classList.contains('kit-collapsed'), 'folded');
  eq([t.textContent, t.title, t.getAttribute('aria-expanded')], ['›', 'Show the index', 'false']);
  eq([getComputedStyle(f.left).width, getComputedStyle(f.leftBody).display], ['40px', 'none']);
  t.click();
  assert(!f.left.classList.contains('kit-collapsed'), 'unfolded');
  eq(getComputedStyle(f.left).width, '288px');
});

test('frame: a fold is remembered under <name>.collapsed.<panel>', () => {
  clean();
  const f = frame(parts());
  f.right.firstChild.click();
  eq(localStorage.getItem('tooltest.collapsed.thread'), '1');
  f.right.firstChild.click();
  eq(localStorage.getItem('tooltest.collapsed.thread'), '');
});

test('frame: it opens with the state diazo users already have, and another tool is apart', () => {
  clean();
  localStorage.setItem('diazo.collapsed.index', '1');
  assert(frame(parts({ name: 'diazo' })).left.classList.contains('kit-collapsed'), 'diazo.collapsed.index folds it');
  assert(!frame(parts({ name: 'other' })).left.classList.contains('kit-collapsed'), 'another tool has its own');
  clean();
});

test('frame: a thread with no messages folds and its first message opens it, leaving what is remembered alone', () => {
  clean();
  const f = frame(parts());
  f.setRightEmpty(true, 'F1');
  assert(f.right.classList.contains('kit-collapsed'), 'folded with nothing to show');
  eq(f.right.firstChild.title, 'Show the thread');
  f.setRightEmpty(false, 'F1');
  assert(!f.right.classList.contains('kit-collapsed'), 'the first message opens it');
  eq(localStorage.getItem('tooltest.collapsed.thread'), ''); // collapsible writes '' as it starts
});

test('frame: a press wins over the empty rule until the card changes', () => {
  clean();
  const f = frame(parts());
  f.setRightEmpty(true, 'F1');
  f.right.firstChild.click();
  f.setRightEmpty(true, 'F1');
  assert(!f.right.classList.contains('kit-collapsed'), 'opened by hand, empty stays open');
  f.right.firstChild.click();
  f.setRightEmpty(false, 'F1');
  assert(f.right.classList.contains('kit-collapsed'), 'folded by hand, a message leaves it folded');
  eq(localStorage.getItem('tooltest.collapsed.thread'), '1');
  f.setRightEmpty(false, 'F2');
  assert(!f.right.classList.contains('kit-collapsed'), 'the next card takes the rule');
  f.setRightEmpty(true, 'F1');
  assert(f.right.classList.contains('kit-collapsed'), 'the press went with its card: back on F1 the rule holds');
});

test('frame: a press holds over a reload on its card, and not on another', () => {
  clean();
  const f = frame(parts());
  f.setRightEmpty(false, 'F1');
  f.right.firstChild.click();
  const same = frame(parts());
  same.setRightEmpty(false, 'F1');
  assert(same.right.classList.contains('kit-collapsed'), 'still folded on its card');
  const other = frame(parts());
  other.setRightEmpty(false, 'F2');
  assert(!other.right.classList.contains('kit-collapsed'), 'another card takes the rule');
});

test('frame: a tab on another card leaves a press alone', () => {
  clean();
  const here = frame(parts());
  const there = frame(parts());
  here.setRightEmpty(false, 'plan-a/F1');
  there.setRightEmpty(false, 'plan-b/F1');
  here.right.firstChild.click();
  there.setRightEmpty(false, 'plan-b/F1');
  here.setRightEmpty(false, 'plan-a/F1');
  assert(here.right.classList.contains('kit-collapsed'), 'the press holds in its tab');
  const reloaded = frame(parts());
  reloaded.setRightEmpty(false, 'plan-a/F1');
  assert(reloaded.right.classList.contains('kit-collapsed'), 'and over a reload on its card');
});

test('frame: the empty rule does nothing to a page with no thread', () => {
  clean();
  frame(parts({ right: undefined })).setRightEmpty(true, 'F1');
});

test('collapsible: folded starts it folded and leaves what the page remembers alone', () => {
  clean();
  const col = document.createElement('aside');
  const body = document.createElement('div');
  collapsible({ col, body, panel: 'index', side: 'left', name: 'tooltest', folded: true });
  assert(col.classList.contains('kit-collapsed'), 'folded');
  eq(localStorage.getItem('tooltest.collapsed.index'), null);
});

// A window whose width the test sets: fire(true) drags it to 1080 or less.
function withWidth(run) {
  const real = window.matchMedia;
  const heard = new Set();
  const mq = {
    matches: true,
    addEventListener: (_, f) => heard.add(f),
    removeEventListener: (_, f) => heard.delete(f),
  };
  const queries = [];
  window.matchMedia = q => { queries.push(q); return mq; };
  const fire = on => {
    mq.matches = on;
    for (const f of [...heard]) f({ matches: on });
  };
  try {
    return run(fire, { queries, heard });
  } finally {
    window.matchMedia = real;
  }
}

function narrowed(extra = {}) {
  clean();
  const f = frame(parts());
  const app = mount(f);
  const seen = [];
  const n = narrowMode({ root: app, left: f.left, right: f.right, onShow: s => seen.push(s), ...extra });
  return { f, app, n, seen };
}

const stateOf = ({ app }) => (app.classList.contains('kit-narrow-left') ? 'left' : app.classList.contains('kit-narrow-right') ? 'right' : null);
const stored = () => Object.keys(localStorage).filter(k => k.includes('.collapsed.')).sort().map(k => [k, localStorage.getItem(k)]);

test('narrowMode: at 1080 or less the page is narrow on the card, and it follows the window', () => {
  withWidth((fire, { queries }) => {
    const p = narrowed();
    eq(queries, ['(width <= 1080px)']);
    assert(p.app.classList.contains('kit-narrow'), 'narrow');
    eq(stateOf(p), null);
    fire(false);
    assert(!p.app.classList.contains('kit-narrow'), 'wide again');
    fire(true);
    assert(p.app.classList.contains('kit-narrow'), 'narrow again');
  });
});

test('narrowMode: closed sides are 40px strips whatever kit-collapsed says, and the centre has the rest', () => {
  withWidth(() => {
    const { f, app } = narrowed();
    app.style.width = '800px';
    assert(!f.left.classList.contains('kit-collapsed'), 'the fold says open');
    eq([f.left.offsetWidth, f.right.offsetWidth, f.centre.offsetWidth], [40, 40, 720]);
    eq(getComputedStyle(f.leftBody).display, 'none');
    eq(app.scrollWidth, 800);
  });
});

test('narrowMode: a press moves the state, and the open side takes the centre track', () => {
  withWidth(() => {
    const p = narrowed();
    p.app.style.width = '800px';
    p.f.left.firstChild.click();
    eq(stateOf(p), 'left');
    eq(p.seen, ['left']);
    eq([p.f.left.offsetWidth, p.f.right.offsetWidth], [760, 40]);
    eq(getComputedStyle(p.f.centre).visibility, 'hidden');
    assert(getComputedStyle(p.f.leftBody).display !== 'none', 'the body shows');
    eq(p.app.scrollWidth, 800);
  });
});

test('narrowMode: a press on the open side returns to the card', () => {
  withWidth(() => {
    const p = narrowed();
    p.f.right.firstChild.click();
    eq(stateOf(p), 'right');
    p.f.right.firstChild.click();
    eq(stateOf(p), null);
    eq(p.seen, ['right', null]);
    eq(getComputedStyle(p.f.centre).visibility, 'visible');
  });
});

test('narrowMode: a press on the other strip moves straight there', () => {
  withWidth(() => {
    const p = narrowed();
    p.f.left.firstChild.click();
    p.f.right.firstChild.click();
    eq(stateOf(p), 'right');
    eq(p.seen, ['left', 'right']);
  });
});

test('narrowMode: show moves the state and tells onShow only of a change', () => {
  withWidth(() => {
    const p = narrowed();
    p.n.show('right');
    p.n.show('right');
    p.n.show(null);
    eq(p.seen, ['right', null]);
  });
});

test('narrowMode: presses leave what is remembered untouched', () => {
  withWidth(() => {
    const p = narrowed();
    const before = stored();
    p.f.left.firstChild.click();
    p.f.right.firstChild.click();
    p.f.right.firstChild.click();
    eq(stored(), before);
    assert(!p.f.left.classList.contains('kit-collapsed') && !p.f.right.classList.contains('kit-collapsed'), 'the folds stay');
  });
});

test('narrowMode: setRightEmpty changes nothing visible while narrow, and its fold is there on widening', () => {
  withWidth(fire => {
    const p = narrowed();
    const t = p.f.right.firstChild;
    const look = () => [stateOf(p), t.textContent, t.title, t.getAttribute('aria-expanded'), p.f.right.offsetWidth];
    const shut = look();
    p.f.setRightEmpty(true, 'F1');
    p.f.setRightEmpty(false, 'F1');
    eq(look(), shut);
    t.click();
    const open = look();
    p.f.setRightEmpty(true, 'F2');
    eq(look(), open);
    fire(false);
    assert(p.f.right.classList.contains('kit-collapsed'), 'the empty rule holds once wide');
    eq(t.title, 'Show the thread');
  });
});

test('narrowMode: the toggle follows the state while narrow and the fold again when wide', () => {
  withWidth(fire => {
    const { f, n } = narrowed();
    const face = t => [t.textContent, t.title, t.getAttribute('aria-expanded')];
    const l = f.left.firstChild;
    const r = f.right.firstChild;
    eq(face(l), ['›', 'Show the index', 'false']);
    eq(face(r), ['‹', 'Show the thread', 'false']);
    l.click();
    eq(face(l), ['‹', 'Hide the index', 'true']);
    n.show('right');
    eq(face(l), ['›', 'Show the index', 'false']);
    eq(face(r), ['›', 'Hide the thread', 'true']);
    fire(false);
    eq(face(l), ['‹', 'Hide the index', 'true']);
    eq(face(r), ['›', 'Hide the thread', 'true']);
  });
});

test('narrowMode: leaving narrow clears the state, and a wide press folds and is remembered', () => {
  withWidth(fire => {
    const p = narrowed();
    p.f.left.firstChild.click();
    fire(false);
    eq(stateOf(p), null);
    eq(p.seen, ['left', null]);
    p.f.left.firstChild.click();
    assert(p.f.left.classList.contains('kit-collapsed'), 'folded');
    eq(localStorage.getItem('tooltest.collapsed.index'), '1');
    fire(true);
    eq(stateOf(p), null);
    fire(false);
    assert(p.f.left.classList.contains('kit-collapsed'), 'the wide choice survives narrow');
  });
});

test('narrowMode: show does nothing while the page is wide', () => {
  withWidth(fire => {
    const p = narrowed();
    fire(false);
    p.n.show('left');
    eq(stateOf(p), null);
    eq(p.seen, []);
  });
});

test('narrowMode: a count is a badge under the toggle, absent at zero, and shown only on a closed strip', () => {
  withWidth(fire => {
    const { f, n } = narrowed();
    const badge = () => f.right.querySelector(':scope > .kit-col-count');
    assert(!badge(), 'none to start');
    n.setCount('right', 3);
    eq(badge().textContent, '3');
    assert(f.right.firstChild.nextElementSibling === badge(), 'under the toggle');
    assert(badge().getBoundingClientRect().top >= f.right.firstChild.getBoundingClientRect().bottom, 'below it on the strip');
    n.setCount('right', 4);
    eq(f.right.querySelectorAll('.kit-col-count').length, 1);
    eq(badge().textContent, '4');
    n.show('right');
    eq(getComputedStyle(badge()).display, 'none');
    n.show(null);
    fire(false);
    eq(getComputedStyle(badge()).display, 'none');
    fire(true);
    n.setCount('right', 0);
    assert(!badge(), 'gone at zero');
  });
});

test('narrowMode: a page with one side has only that strip', () => {
  withWidth(() => {
    clean();
    const f = frame(parts({ right: undefined }));
    const app = document.createElement('div');
    app.className = 'kit-app';
    app.append(document.createElement('div'), f.left, f.centre);
    fixture().append(app);
    const n = narrowMode({ root: app, left: f.left });
    n.setCount('right', 2);
    n.show('right');
    assert(!app.classList.contains('kit-narrow-right'), 'no such side');
    f.left.firstChild.click();
    assert(app.classList.contains('kit-narrow-left'), 'the side it has');
  });
});

test('narrowMode: stop lets go of the window', () => {
  withWidth((fire, { heard }) => {
    const { n } = narrowed();
    eq(heard.size, 1);
    n.stop();
    eq(heard.size, 0);
  });
});

function tipped(when) {
  const trigger = document.createElement('button');
  const tip = lineTip('all of it');
  const box = document.createElement('span');
  box.className = 'kit-tip-at';
  box.append(trigger, tip);
  const outside = document.createElement('button');
  fixture().append(box, outside);
  wireTip(box, trigger, tip, when);
  return { box, trigger, tip, outside };
}

test('tip: a press opens it in the window, on top of whatever scrolls', () => {
  const { box, trigger, tip } = tipped();
  eq([tip.getAttribute('role'), tip.className, tip.textContent], ['tooltip', 'kit-tip', 'all of it']);
  eq(getComputedStyle(tip).display, 'none');
  trigger.click();
  assert(box.classList.contains('show'), 'shown');
  eq(trigger.getAttribute('aria-expanded'), 'true');
  eq([getComputedStyle(tip).display, getComputedStyle(tip).position], ['block', 'fixed']);
  assert(tip.style.top && tip.style.left, 'placed');
});

test('tip: it goes over its box where the window has no room below', () => {
  const { box, trigger, tip } = tipped();
  box.style.cssText = `position:fixed; top:${innerHeight - 30}px; left:40px`;
  trigger.click();
  assert(parseFloat(tip.style.top) < box.getBoundingClientRect().top, `tip top ${tip.style.top}`);
});

test('tip: Esc closes it', () => {
  const { box, trigger } = tipped();
  trigger.click();
  trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
  assert(!box.classList.contains('show'), 'closed');
});

test('tip: the next tap elsewhere closes it', () => {
  const { box, trigger, outside } = tipped();
  trigger.click();
  outside.click();
  assert(!box.classList.contains('show'), 'closed');
  eq(trigger.getAttribute('aria-expanded'), 'false');
});

test('tip: Enter toggles it from the keyboard', () => {
  const { box, trigger } = tipped();
  trigger.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
  assert(box.classList.contains('show'), 'opened');
});

test('tip: a press with nothing to show goes to what is under it', () => {
  const { box, trigger } = tipped(() => false);
  let reached = false;
  box.parentElement.addEventListener('click', () => (reached = true), { once: true });
  trigger.click();
  assert(!box.classList.contains('show'), 'stays shut');
  assert(reached, 'the press carried on');
});

// A sandboxed frame without allow-same-origin (diazo's mockup view) throws on
// reaching localStorage at all.
function withoutStorage(run) {
  const was = Object.getOwnPropertyDescriptor(window, 'localStorage');
  Object.defineProperty(window, 'localStorage', {
    configurable: true,
    get() { throw new DOMException('denied', 'SecurityError'); },
  });
  try {
    run();
  } finally {
    Object.defineProperty(window, 'localStorage', was);
  }
}

test('frame: draws where storage is blocked, and folds without remembering', () => {
  clean();
  withoutStorage(() => {
    const f = frame(parts());
    mount(f);
    assert(!f.left.classList.contains('kit-collapsed'), 'nothing remembered: open');
    f.left.firstChild.click();
    assert(f.left.classList.contains('kit-collapsed'), 'the press still folds');
    f.setRightEmpty(true, 'c1');
    assert(f.right.classList.contains('kit-collapsed'), 'an empty thread folds');
    f.right.firstChild.click();
    assert(!f.right.classList.contains('kit-collapsed'), 'a press opens it');
    f.setRightEmpty(true, 'c1');
    assert(!f.right.classList.contains('kit-collapsed'), 'the press holds on its card in this tab');
    f.setRightEmpty(true, 'c2');
    assert(f.right.classList.contains('kit-collapsed'), 'a new card lets go of it');
  });
  eq(localStorage.getItem('tooltest.collapsed.index'), null);
});
