import { test, eq, assert, fixture } from './harness.js';
import { stagePill, stageTrack, workingLine, waitBadges } from '/_kit/track.js';

test('stagePill: its label with its count, as a plain step when it opens nothing', () => {
  const p = stagePill({ key: 'findings', label: 'Findings', count: '3 of 5', state: 'flagged' });
  eq([p.tagName, p.className, p.dataset.stage, p.textContent], ['SPAN', 'kit-stage flagged plain', 'findings', 'Findings · 3 of 5']);
});

test('stagePill: done reads with a tick', () => {
  eq(stagePill({ key: 'a', label: 'Baseline', state: 'done' }).textContent, '✓ Baseline');
});

test('stagePill: the stage you are at says so', () => {
  eq(stagePill({ key: 'a', label: 'Fix', state: 'now' }).getAttribute('aria-current'), 'step');
  assert(!stagePill({ key: 'a', label: 'Fix', state: '' }).hasAttribute('aria-current'), 'only now');
});

test('stagePill: live is a class of its own', () => {
  assert(stagePill({ key: 'a', label: 'Fix', state: 'now', live: true }).classList.contains('live'), 'live');
});

test('stagePill: with open it is a button that opens', () => {
  let opened = 0;
  const p = stagePill({ key: 'a', label: 'Fix', state: '' }, () => opened++);
  eq([p.tagName, p.type], ['BUTTON', 'button']);
  assert(!p.classList.contains('plain'), 'not plain');
  p.click();
  eq(opened, 1);
});

const detail = { title: 'Open', rows: [{ id: 'F1', text: 'A cache', meta: 'measured' }, { text: 'No id' }], note: 'And the rest.' };

test('stagePill: detail adds an ⓘ with its tip, in one grid of three cells', () => {
  const box = stagePill({ key: 'a', label: 'Fix', state: '', detail });
  fixture().append(box);
  eq(box.className, 'kit-tip-at');
  const more = box.querySelector('.kit-stage .kit-more');
  eq([more.getAttribute('role'), more.getAttribute('aria-label')], ['button', 'More on Fix']);
  const tip = box.querySelector('.kit-tip');
  eq([tip.dataset.tip, tip.querySelector('b').textContent, tip.querySelector('.kit-tip-note').textContent], ['a', 'Open', 'And the rest.']);
  eq([...tip.querySelectorAll('.kit-tip-row')].map(r => [...r.children].map(c => c.textContent)), [['F1', 'A cache', 'measured'], ['', 'No id', '']]);
  eq(getComputedStyle(tip.querySelector('.kit-tip-rows')).display, 'grid');
});

test('stagePill: a press on the ⓘ shows the tip and does not open the stage', () => {
  let opened = 0;
  const box = stagePill({ key: 'a', label: 'Fix', state: '', detail }, () => opened++);
  fixture().append(box);
  box.querySelector('.kit-more').click();
  assert(box.classList.contains('show'), 'shown');
  eq(opened, 0);
});

test('stageTrack: its stages joined by arrows, each opening what it names', () => {
  const got = [];
  const t = stageTrack({
    label: 'Where this job stands',
    open: id => got.push(id),
    stages: [
      { key: 'a', label: 'Baseline', state: 'done', opens: 'baseline' },
      { key: 'b', label: 'Findings', state: 'now', opens: 'findings' },
      { key: 'c', label: 'Verify', state: '' },
    ],
  });
  eq([t.tagName, t.className, t.getAttribute('aria-label')], ['NAV', 'kit-stages', 'Where this job stands']);
  eq([...t.children].map(c => c.textContent), ['✓ Baseline', '→', 'Findings', '→', 'Verify']);
  eq(t.children[1].className, 'kit-arrow');
  eq(t.children[1].getAttribute('aria-hidden'), 'true');
  t.children[2].click();
  eq(got, ['findings']);
  assert(t.children[4].classList.contains('plain'), 'one that opens nothing is plain');
});

test('stageTrack: a parked track says so in its class', () => {
  eq(stageTrack({ stages: [], parked: true }).className, 'kit-stages parked');
});

test('stageTrack: a stage the caller built takes its place, and after follows them', () => {
  const tab = document.createElement('button');
  const badge = document.createElement('span');
  const t = stageTrack({ stages: [{ key: 'a', label: 'A', state: '' }, tab], after: [badge, null] });
  eq(t.children.length, 4);
  assert(t.children[2] === tab && t.children[3] === badge, 'in order');
});

test('waitBadges: none, nothing', () => {
  eq(waitBadges([]), []);
});

test('waitBadges: a wait on you comes first, in your colours', () => {
  const [first, more] = waitBadges([{ text: 'waiting on Alex · 1', who: 'Alex' }, { text: 'waiting on you · 2', who: 'you' }]);
  eq([first.className, first.dataset.badge, first.dataset.who, first.textContent], ['kit-pill kit-badge t-you', 'wait', 'you', 'waiting on you · 2']);
  eq(more.querySelector('.kit-more-waits').textContent, 'and 1 more');
});

test('waitBadges: anyone else is in the wait colours, and one wait has no more', () => {
  const all = waitBadges([{ text: 'waiting on Alex · 1', who: 'Alex' }]);
  eq(all.length, 1);
  eq(all[0].className, 'kit-pill kit-badge t-wait');
});

test('waitBadges: the rest sit in a tip that drops the repeated words', () => {
  const [, more] = waitBadges([{ text: 'waiting on you · 2', who: 'you' }, { text: 'waiting on Alex · 1', who: 'Alex' }]);
  fixture().append(more);
  eq([more.className, more.dataset.badge], ['kit-tip-at kit-waits-at', 'wait-more']);
  const tip = more.querySelector('.kit-tip');
  eq([tip.dataset.tip, tip.querySelector('b').textContent, tip.querySelector('.kit-tip-row').textContent], ['waits', 'Also waiting on', 'Alex · 1']);
  more.querySelector('.kit-more-waits').click();
  assert(more.classList.contains('show'), 'a press shows it');
});

test('workingLine: the line with its pulsing dot, nothing with no text', () => {
  const p = workingLine({ text: 'Writing the plan' });
  eq([p.tagName, p.className, p.getAttribute('role'), p.hasAttribute('tabindex'), p.children.length], ['P', 'kit-working', 'status', false, 2]);
  eq(p.firstChild.getAttribute('aria-hidden'), 'true');
  eq([p.lastChild.className, p.lastChild.textContent], ['kit-agent-text', 'Writing the plan']);
  eq(workingLine({ text: '' }), null);
});

test('workingLine: a slot keeps its height with nothing in it', () => {
  const s = workingLine({ text: '', slot: true });
  fixture().append(s);
  eq([s.className, s.children.length, getComputedStyle(s).height], ['kit-agent-slot', 0, '28px']);
});

test('workingLine: in a slot a long line shows in full in a tip', () => {
  const s = workingLine({ text: 'Writing the plan', slot: true });
  fixture().append(s);
  eq(s.className, 'kit-agent-slot kit-tip-at');
  const line = s.querySelector('.kit-working');
  eq(line.getAttribute('tabindex'), '0');
  eq(s.querySelector('.kit-tip').textContent, 'Writing the plan');
  line.click();
  assert(s.classList.contains('show'), 'a press shows it');
});

test('workingLine: a message not settled yet reads as pending', () => {
  eq(workingLine({ text: 'sent', pending: true, slot: true }).querySelector('.kit-working').className, 'kit-working kit-pending');
});
