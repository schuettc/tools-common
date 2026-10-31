import { h, bar, list, facts, card, buttons, codeBlock, fold, noteField, messagePill, MESSAGE_STATES, createKeys, initTheme, frame, lineTip, wireTip, indexRow, indexSection, thread, stageTrack, workingLine, waitBadges, createDrafts, questionButton, QUESTIONS, sendBar, keepElsewhere, filterRow, statusPills, groupList } from '/_kit/kit.js';

const MARK = `<svg viewBox="0 0 64 64"><rect width="64" height="64" rx="14" fill="#14161d"/><path d="M13 22H28M13 32H26M13 42H28" stroke="#9aa0ab" stroke-width="3.6" stroke-linecap="round"/><path d="M13 22H28" stroke="#d98f66" stroke-width="3.6" stroke-linecap="round"/><path d="M33 17V47" stroke="#d98f66" stroke-width="2.4"/><rect x="38" y="18" width="14" height="28" rx="3" fill="none" stroke="#d98f66" stroke-width="2.4"/></svg>`;
const mark = () => {
  const s = h('span');
  s.innerHTML = MARK; // a fixed, trusted literal
  return s.firstChild;
};

const app = document.getElementById('app');
const view = new URLSearchParams(location.search).get('view') || 'attention';

// ------------------------------------------------------------ casebook: attention
const ITEMS = [
  ['pr', 'bettor-help-platform#673', 'bump vitest from 4.1.10 to 5.0.0', '14d'],
  ['pr', 'bettor-help-platform#672', 'bump the minor-and-patch group (5 updates)', '14d'],
  ['pr', 'bettor-help-platform#671', 'bump eslint-plugin-simple-import-sort to 14', '14d'],
  ['pr', 'bettor-help-platform#670', 'bump the minor-and-patch group (3 updates)', '14d'],
  ['issue', 'muster#112', 'nudge intermittently fails to submit into Codex panes', '39d', 'pi proposes keep'],
  ['issue', 'codex-reviewer#4', 'Your plugin is featured on the HOL Registry', '86d'],
  ['issue', 'codex-reviewer#3', 'Add plugin icon for Codex marketplace display', '134d'],
  ['issue', 'cdk-create-ami#194', 'Updating an already creating image does not work', '3y'],
  ['pr', 'aws-samples/amazon-chime-sdk#241', 'fix: add basic IVR', '4y'],
].map(([kind, key, title, age, sub]) => ({ kind, key, title, age, sub }));

function attention(b) {
  const read = h('main', { class: 'kit-read' });
  const count = h('span', { class: 'kit-n' }, '0 selected');
  const l = list({
    label: 'attention',
    views: [
      { id: 'waiting', label: 'waiting on you', count: 46, on: true },
      { id: 'new', label: 'new', count: 131 },
      { id: 'due', label: 'due', count: 6 },
      { id: 'proposed', label: 'proposed', count: 18 },
      { id: 'board', label: '▦ board' },
    ],
    row: it => ({ key: `${it.kind} · ${it.key}`, title: it.title, meta: it.age, sub: it.sub, selectable: true }),
    onOpen: it => read.replaceChildren(item(it)),
    onSelect: sel => {
      count.textContent = `${sel.length} selected`;
      b.setPrimary(sel.length ? { label: `Decide ${sel.length}`, run() {} } : null);
    },
    foot: h('span', { style: 'display:contents' }, count, h('span', { style: 'margin-left:auto' }), buttons([{ label: 'make rule…' }])),
  });
  l.setItems(ITEMS);
  for (let i = 0; i < 4; i++) l.toggle(i);
  l.open(4);
  createKeys({ list: l });
  return [l.el, read];
}

// One line per message state, as a tool would write it under the pill.
const MESSAGE_LINES = {
  sent: 'mid-turn: read when this turn ends',
  with_agent: 'in pi · 2s ago',
  working: 'rewriting the understanding',
  answered: 'posted the corrected understanding',
  declined: 'already covered by DCSN2',
  failed: 'the folder is locked by another session',
  no_reply: 'the turn ended without a reply',
};

function item(it) {
  return h('div', { class: 'kit-doc' },
    h('div', { class: 'kit-kick' }, `${it.kind} · schuettc/${it.key} · incoming`),
    h('h1', { class: 'kit-h1' }, it.title),
    facts([['status', 'new'], ['waiting on you', it.age], ['opened', '2026-08-18'], ['last reply', 'none from you']]),
    h('p', null, "The Enter is absorbed by Codex's paste-burst heuristic."),
    card({ edge: 'agent', head: 'pi proposes · keep', body: h('span', null, 'Still valid after #151; the race is in ', h('code', null, 'nudge.go:88'), '.'), actions: [{ label: 'accept', fill: true }, { label: 'change…' }, { label: 'reject' }] }),
    h('div', { class: 'kit-label' }, 'card headers: the same height with or without a pill'),
    h('div', { style: 'display:grid; grid-template-columns:1fr 1fr; gap:14px' },
      card({ edge: 'signal', head: [h('span', { class: 'kit-card-crumb' }, 'build › build1'), h('span', { class: 'kit-pill wait' }, 'waiting · 4 of 6'), h('span', { class: 'kit-card-key' }, 'schuettc/proj/BUILD1')], body: 'With a pill.' }),
      card({ edge: 'signal', head: [h('span', { class: 'kit-card-crumb' }, 'build'), h('span', { class: 'kit-card-key' }, 'schuettc/proj/build')], body: 'Without one.' })),
    h('div', { class: 'kit-label' }, 'decide'),
    buttons([{ label: 'keep' }, { label: 'wait…' }, { label: 'watch…' }, { label: 'close', danger: true }, { label: 'ignore' }]),
    h('div', { class: 'kit-label' }, 'messages'),
    h('div', { style: 'display:flex; flex-wrap:wrap; gap:14px 22px' }, MESSAGE_STATES.map(({ state }) => messagePill(state, MESSAGE_LINES[state]))),
    h('div', { class: 'kit-label' }, 'evidence'),
    h('p', null, 'Linked branch ', h('code', null, 'fix/codex-enter'), ' exists locally, 0 unpushed.'),
    h('div', { class: 'kit-label' }, 'history'),
    h('p', null, h('code', null, '09-24'), ' pi ran ', h('code', null, 'gh issue view 112')));
}

// ------------------------------------------------------------------ cull: label
const TESTS = ['TestEnvLoadsDotenv', 'TestEnvRejectsWorldReadable', 'TestExecPassesKey', 'TestExecMissingKey', 'TestPopupCancel', 'TestPopupWritesAtomically']
  .map((name, i) => ({ name, file: i < 4 ? 'internal/creel/env_test.go' : 'internal/creel/popup_test.go', label: i < 2 ? ['keep', 'cut'][i] : '' }));

function source(name) {
  const body = Array.from({ length: 560 }, (_, i) => `\tif got := env.Get("KEY_${i}"); got != want[${i}] {\n\t\tt.Fatalf("KEY_${i} = %q, want %q", got, want[${i}])\n\t}`);
  return `func ${name}(t *testing.T) {\n\tdir := t.TempDir()\n${body.slice(0, 180).join('\n')}\n}\n`;
}

function labelView(b) {
  const read = h('main', { class: 'kit-read' });
  let open = null;
  const done = () => TESTS.filter(t => t.label).length;
  const l = list({
    label: 'label · blind',
    views: [
      { id: 'unlabeled', label: 'unlabeled', count: TESTS.length - done(), on: true },
      { id: 'keep', label: 'keep' }, { id: 'cut', label: 'cut' }, { id: 'review', label: 'review' },
    ],
    row: t => ({ key: `go · ${t.file}`, title: t.name, meta: t.label || '·' }),
    openOnMove: true,
    onOpen: t => { open = t; read.replaceChildren(test(t)); },
  });
  l.setItems(TESTS);
  l.open(2);
  const history = [];
  const setLabel = v => {
    if (!open) return;
    history.push([open, open.label]);
    open.label = v;
    l.setItems(TESTS);
    b.setCount('label', `${done()}/${TESTS.length}`);
    l.move(1);
  };
  const keys = createKeys({ list: l });
  keys.register({ keys: '1', label: 'keep', run: () => setLabel('keep'), group: 'label' });
  keys.register({ keys: '2', label: 'cut', run: () => setLabel('cut'), group: 'label' });
  keys.register({ keys: '3', label: 'review', run: () => setLabel('review'), group: 'label' });
  keys.register({ keys: 'n', label: 'note', run: () => document.querySelector('.kit-note')?.focus(), group: 'label' });
  keys.register({ keys: 'u', label: 'undo', group: 'label', run: () => {
    const last = history.pop();
    if (!last) return;
    last[0].label = last[1];
    l.setItems(TESTS);
    b.setCount('label', `${done()}/${TESTS.length}`);
  } });

  function test(t) {
    const src = source(t.name);
    return h('div', { class: 'kit-doc' },
      h('div', { class: 'kit-kick' }, `go · ${t.file}`),
      h('h1', { class: 'kit-h1' }, t.name),
      facts([['repo', 'schuettc/creel'], ['lines', String(src.split('\n').length - 1)], ['size', `${(src.length / 1024).toFixed(1)} KB`]]),
      codeBlock(src, { start: 41 }),
      fold('setup', codeBlock('func newEnv(t *testing.T) *Env {\n\tt.Helper()\n\treturn &Env{dir: t.TempDir()}\n}\n', { start: 12 })),
      fold('code under test', codeBlock('func (e *Env) Get(k string) string {\n\treturn e.vals[k]\n}\n', { start: 88 })),
      h('div', { class: 'kit-label' }, 'label'),
      buttons([{ label: '1 keep', run: () => setLabel('keep') }, { label: '2 cut', danger: true, run: () => setLabel('cut') }, { label: '3 review', run: () => setLabel('review') }]),
      h('div', { class: 'kit-label' }, 'note'),
      noteField({ placeholder: 'why (n)', onCommit() {} }));
  }
  b.setCount('label', `${done()}/${TESTS.length}`);
  return [l.el, read];
}

// ----------------------------------------------- the parts tools share (diazo's)
function parts() {
  const f = frame({
    name: 'kitdemo',
    left: { panel: 'index', label: 'The index' },
    centre: { label: 'The centre' },
    right: { panel: 'thread', label: 'Thread' },
  });
  const more = h('button', { class: 'kit-ctl', type: 'button' }, 'a tip');
  const tip = lineTip('Opens on hover, focus or a tap, and no column clips it.');
  const box = h('span', { class: 'kit-tip-at' }, more, tip);
  wireTip(box, more, tip);
  f.centre.append(h('div', { class: 'kit-label' }, 'frame and tip'), box);
  f.setRightEmpty(false, 'F2');
  f.centre.append(
    h('div', { class: 'kit-label' }, 'the thread folds when its card has no messages'),
    buttons([
      { label: 'a card with no messages', run: () => f.setRightEmpty(true, 'F3') },
      { label: 'a card with messages', run: () => f.setRightEmpty(false, 'F2') },
    ]));
  f.centre.append(
    h('div', { class: 'kit-label' }, 'stage track'),
    workingLine({ text: 'Reading the last ten builds', slot: true }),
    stageTrack({
      label: 'Where this job stands',
      open() {},
      stages: [
        { key: 'baseline', label: 'Baseline', state: 'done', opens: 'baseline' },
        { key: 'findings', label: 'Findings', count: '3 of 5', state: 'now', live: true, opens: 'findings', detail: {
          title: 'Open',
          rows: [{ id: 'F1', text: 'Cache misses on every build', meta: 'needs you' }, { id: 'F4', text: 'Waiting on another finding', meta: 'waiting' }],
          note: 'Answer them in place.',
        } },
        { key: 'plan', label: 'Fix plan', state: 'flagged', opens: 'plan' },
        { key: 'verify', label: 'Verify', state: '' },
      ],
      after: waitBadges([{ text: 'waiting on you · 2', who: 'you' }, { text: 'waiting on Alex · 1', who: 'Alex' }]),
    }));
  const drafts = createDrafts();
  const note = drafts.box('thread:F2', 'A note on this', 3);
  f.rightBody.append(thread({
    id: 'F2',
    name: 'The row in front of you',
    key: 'demo/project/F2',
    messages: [
      { id: 1, from: 'agent', at: '2026-10-30T12:00:00Z', text: 'Measured on the last ten builds.' },
      { id: 2, from: 'court', at: '2026-10-30T12:05:00Z', words: 'your note', text: 'Which ten?', state: 'working', stateLine: 'reading the runs' },
    ],
    note: { el: note.el, box: note.box, send() {} },
  }));
  f.centre.append(
    h('div', { class: 'kit-label' }, 'questions, drafts and Send'),
    buttons([
      questionButton({ label: QUESTIONS.explain, asked: false, run() {} }),
      questionButton({ label: QUESTIONS.smaller, asked: true, run() {} }),
      questionButton({ label: QUESTIONS.breaks, asked: false, run() {} }),
    ]),
    drafts.box('note:F1', 'A note on F1: it goes with your choice, or on its own.', 3).el,
    sendBar({ count: '1 of 2 answered', sends: 1, hint: 'Unanswered ones stay open.', discard() {}, send() {} }));
  const select = id => f.centre.dataset.open = id;
  f.leftBody.append(
    indexSection({ name: 'Findings', open() {}, empty: '', rows: [
      indexRow({ id: 'F1', title: 'Cache misses on every build', meta: 'needs you', attention: true, select }),
      indexRow({ id: 'F2', title: 'The row in front of you', meta: 'measured', on: true, select }),
      indexRow({ id: 'F3', title: 'A draft waits for Send', meta: 'draft · A', draft: true, select }),
      indexRow({ id: 'F4', title: 'Waiting on another finding', meta: 'waiting on F1', waits: true, select }),
    ] }),
    indexSection({ name: 'Later', rows: [], empty: 'Nothing for later.' }));
  frontPage(f);
  return f;
}

// Which ones are chosen is the tool's own, and so is the redraw after a press.
function frontPage(f) {
  const slot = h('div');
  const left = h('div');
  let projects = [];
  let statuses = [];
  const folded = ['built'];
  const toggled = (on, v) => (!v ? [] : on.includes(v) ? on.filter(x => x !== v) : [...on, v]);
  const line = text => h('li', { class: 'kit-row' }, h('span', { class: 'kit-title' }, text));
  const draw = () => {
    left.replaceChildren(filterRow({
      label: 'Projects',
      items: [{ value: 'diazo', label: 'diazo', count: 3 }, { value: 'sift', label: 'sift', count: 1 }],
      chosen: projects,
      onToggle: v => { projects = toggled(projects, v); draw(); },
    }));
    slot.replaceChildren(
      statusPills({
        pills: [{ value: '', label: 'All', count: 4 }, { value: 'needs', label: 'Needs you', count: 1 }, { value: 'built', label: 'Built', count: 3 }, { value: 'parked', label: 'Parked', count: 0 }],
        chosen: statuses,
        onToggle: v => { statuses = toggled(statuses, v); draw(); },
      }),
      groupList({
        groups: [
          { key: 'needs', title: 'Needs you', edge: 'signal', rows: [line('Cache misses on every build')], empty: 'Nothing is waiting on you.' },
          { key: 'waiting', title: 'Waiting', rows: [], empty: 'Nothing is waiting on anyone.' },
          { key: 'built', title: 'Built', rows: [line('The server standard'), line('The shared store'), line('The page parts')], empty: 'Nothing is built yet.' },
        ],
        folded,
        onFold(key, on) {
          folded.splice(0, folded.length, ...folded.filter(k => k !== key), ...(on ? [key] : []));
        },
      }));
  };
  draw();
  f.leftBody.append(left);
  f.centre.append(h('div', { class: 'kit-label' }, 'the front page: filter rows, status pills and groups that fold'), slot);
}

// ------------------------------------------------------------------- the page
const b = bar({
  brand: { name: view === 'label' ? 'cull' : 'casebook', mark: mark() },
  sections: view === 'label'
    ? [{ id: 'label', label: 'label', count: '' }, { id: 'eval', label: 'eval' }]
    : [{ id: 'attention', label: 'attention', count: 46 }, { id: 'rules', label: 'rules', count: 6 }, { id: 'apply', label: 'to apply', count: '1,166' }],
  active: view === 'label' ? 'label' : 'attention',
  onSection: id => { if (id === 'attention' || id === 'label') return; location.search = `?view=${view === 'label' ? 'attention' : 'label'}`; },
  status: view === 'label' ? 'blind · jev hidden' : 'synced 4m ago · courts-macbook-pro-2',
  primary: view === 'label' ? { label: 'Next unlabeled', run() {} } : null,
});
const theme = initTheme('kitdemo', b.themeControl);
const forced = new URLSearchParams(location.search).get('theme');
if (forced) theme.set(forced); // screenshots: ?theme=light|dark
b.setLive('live');
// The "also open" button: other work a click away, listed when pressed.
// keepElsewhere calls this whenever the list may have changed.
keepElsewhere(b, () => [
  { label: 'muster: unanswered requests', detail: 'needs you · Decisions', tone: 'you', onClick() {} },
  { label: 'Claude Code mods for channels', detail: 'working · Spec', tone: 'working', onClick() {} },
  { label: 'casebook triage', detail: 'idle · Plan', onClick() {} },
])();
if (view === 'parts') {
  // The three columns: ?view=parts.
  app.className = 'kit-app';
  const f = parts();
  app.append(b.el, f.left, f.centre, f.right);
} else if (view === 'label') {
  app.append(b.el, h('div', { class: 'kit-main' }, ...labelView(b)));
} else {
  // The app grid with a rail: the tool's own dock goes here (casebook's agent).
  app.className = 'kit-app';
  const rail = h('aside', { class: 'kit-rail' },
    h('div', { class: 'kit-lh' }, h('div', { class: 'kit-eyebrow' }, 'the tool’s rail')),
    h('div', { style: 'padding:0 16px' }, card({ edge: 'agent', head: 'pi · 3m', body: "Done: 38 close, 2 keep. They're in proposed." })));
  app.append(b.el, ...attention(b), rail);
  b.setStatus('offline · 3 queued', { tone: 'danger' });
}
document.body.dataset.ok = '1';
