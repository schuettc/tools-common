// track.js: where a piece of work stands. The stage track, the badges for who
// it waits on, and the agent's one working line. A stage is plain values, so
// each tool or job gives its own.
import { h } from './dom.js';
import { lineTip, wireTip } from './tip.js';

// What a stage's ⓘ shows: its title, a row per thing with its state, and a
// closing note. One grid of id, text and state: every row keeps all three
// cells, so the texts line up and only they wrap.
function stageTip(s) {
  const d = s.detail;
  return h('span', { class: 'kit-tip', role: 'tooltip', 'data-tip': s.key },
    h('b', null, d.title),
    d.rows?.length
      ? h('span', { class: 'kit-tip-rows' }, d.rows.map(r =>
        h('span', { class: 'kit-tip-row' },
          h('span', { class: 'kit-tip-id' }, r.id ?? ''),
          h('span', null, r.text),
          h('span', { class: 'kit-tip-meta' }, r.meta ?? ''))))
      : null,
    d.note ? h('span', { class: 'kit-tip-note' }, d.note) : null);
}

// stagePill is one stage: its label and count, ✓ once done, a pulsing dot while
// something runs in it, and an ⓘ when it has more to say. state is the class it
// draws in (done, now, flagged or none); open, when given, is what a press on it
// does.
export function stagePill(s, open) {
  const label = s.count ? `${s.label} · ${s.count}` : s.label;
  const tip = s.detail ? stageTip(s) : null;
  const more = tip
    ? h('span', { class: 'kit-more', role: 'button', tabindex: '0', 'aria-label': `More on ${label}` })
    : null;
  const pill = h(open ? 'button' : 'span', {
    class: ['kit-stage', s.state, !open && 'plain', s.live && 'live'].filter(Boolean).join(' '),
    type: open ? 'button' : null,
    'data-stage': s.key,
    'aria-current': s.state === 'now' ? 'step' : null,
    onclick: open,
  }, s.state === 'done' ? `✓ ${label}` : label, more);
  if (!tip) return pill;
  const box = h('span', { class: 'kit-tip-at' }, pill, tip);
  wireTip(box, more, tip);
  return box;
}

// stageTrack joins the stages with arrows. A stage whose opens is set presses
// through to open(opens); an element in stages (a tab of the caller's) takes its
// place as it is. after holds what follows on the line: wait badges, counts.
export function stageTrack(o) {
  return h('nav', { class: 'kit-stages' + (o.parked ? ' parked' : ''), 'aria-label': o.label },
    o.stages.flatMap((s, i) => [
      i ? h('span', { class: 'kit-arrow', 'aria-hidden': 'true' }, '→') : null,
      s instanceof Node ? s : stagePill(s, o.open && s.opens != null ? () => o.open(s.opens) : null),
    ]),
    o.after);
}

// "and 3 more": what else it waits on, in a tip like a stage's.
function moreWaits(waits) {
  const more = h('span', { class: 'kit-more-waits', role: 'button', tabindex: '0' }, `and ${waits.length} more`);
  const tip = h('span', { class: 'kit-tip', role: 'tooltip', 'data-tip': 'waits' },
    h('b', null, 'Also waiting on'),
    h('span', { class: 'kit-tip-rows' }, waits.map(w =>
      h('span', { class: 'kit-tip-row' },
        h('span', { class: 'kit-tip-id' }),
        h('span', null, w.text.replace(/^waiting on /, '')),
        h('span', { class: 'kit-tip-meta' })))));
  const box = h('span', { class: 'kit-tip-at kit-waits-at', 'data-badge': 'wait-more' }, more, tip);
  wireTip(box, more, tip);
  return box;
}

// waitBadges is who it waits on: one badge, on you if any is, and the rest
// behind "and N more". Each wait is { text, who }; who 'you' is the reader.
export function waitBadges(waits) {
  if (!waits.length) return [];
  const first = waits.find(w => w.who === 'you') ?? waits[0];
  const rest = waits.filter(w => w !== first);
  const badge = h('span', {
    class: `kit-pill kit-badge t-${first.who === 'you' ? 'you' : 'wait'}`,
    'data-badge': 'wait',
    'data-who': first.who,
  }, first.text);
  return rest.length ? [badge, moreWaits(rest)] : [badge];
}

// workingLine is the agent's one line with a pulsing dot, or null with no text.
// With slot it is kept at one line's height whether it holds anything or not, so
// nothing moves when a line comes or goes; a long line is cut with an ellipsis
// and shows in full in a tip. pending draws it as a message not settled yet.
export function workingLine(o) {
  const line = o.text
    ? h('p', { class: 'kit-working' + (o.pending ? ' kit-pending' : ''), role: 'status', tabindex: o.slot ? '0' : null },
      h('i', { 'aria-hidden': 'true' }),
      h('span', { class: 'kit-agent-text' }, o.text))
    : null;
  if (!o.slot) return line;
  const slot = h('div', { class: 'kit-agent-slot' });
  if (!line) return slot;
  const tip = lineTip(o.text);
  slot.classList.add('kit-tip-at');
  slot.append(line, tip);
  wireTip(slot, line, tip);
  return slot;
}
