import { test, eq, assert } from './harness.js';
import { live } from '/_kit/live.js';
import { fakeFetch, fakeTimers, fakeEventSource, flush } from './fakes.js';

function setup(extra = {}) {
  const f = fakeFetch(), timers = fakeTimers(), es = fakeEventSource();
  const events = [], statuses = [];
  const handle = live({
    events: '/api/events', poll: '/api/state',
    onEvent: e => events.push(e), onStatus: s => statuses.push(s),
    EventSource: es.FakeES, fetch: f.fetch,
    setTimeout: timers.setTimeout, clearTimeout: timers.clearTimeout,
    randomUUID: () => 'T1',
    ...extra,
  });
  return { f, timers, es, events, statuses, handle };
}

test('live: stream delivers in order and tracks cursor', () => {
  const { es, events, statuses, handle } = setup();
  eq(es.last().url, '/api/events?tab=T1&page=');
  es.last().emitOpen();
  es.last().emitMessage('1', { type: 'a', data: 1 });
  es.last().emitMessage('2', { type: 'b', data: { x: 2 } });
  eq(events, [{ type: 'a', data: 1, cursor: '1' }, { type: 'b', data: { x: 2 }, cursor: '2' }]);
  eq(handle.cursor(), '2');
  eq(statuses, ['live']);
  handle.stop();
});

test('live: starts from a given cursor', () => {
  const { es, handle } = setup({ cursor: 'c 9' });
  eq(es.last().url, '/api/events?tab=T1&page=&since=c%209');
  handle.stop();
});

test('live: error falls back to polling', async () => {
  const { f, timers, es, events, statuses, handle } = setup();
  const first = es.last();
  first.emitOpen();
  first.emitMessage('5', { type: 'a', data: 1 });
  first.emitError();
  assert(first.closed, 'client closes the broken stream itself');
  eq(statuses.at(-1), 'polling');
  eq(timers.pending.size, 1);
  eq([...timers.pending.values()][0].ms, 2000, 'polls every 2 s');

  f.queue.push(f.json({ cursor: '7', events: [{ type: 'b', data: 2 }, { type: 'c', data: 3 }] }));
  timers.tick();
  await flush();
  eq(f.calls[0].url, '/api/state?tab=T1&page=&since=5');
  eq(events.slice(1), [{ type: 'b', data: 2, cursor: '7' }, { type: 'c', data: 3, cursor: '7' }]);
  eq(handle.cursor(), '7');
  const second = es.last();
  assert(second !== first, 'reopens the stream');
  eq(second.url, '/api/events?tab=T1&page=&since=7');
  eq(timers.pending.size, 1, 'next poll scheduled');

  second.emitOpen();
  eq(statuses.at(-1), 'live');
  eq(timers.pending.size, 0, 'open cancels polling');
  handle.stop();
});

test('live: poll failure is down', async () => {
  const { f, timers, es, statuses, handle } = setup();
  es.last().emitError();
  f.queue.push(new Error('refused'));
  timers.tick();
  await flush();
  eq(statuses.at(-1), 'down');
  eq(timers.pending.size, 1, 'keeps trying');
  f.queue.push(f.text('nope', 500));
  timers.tick();
  await flush();
  eq(statuses.at(-1), 'down', 'a 500 is still down');
  f.queue.push(f.json({ cursor: '1', events: [] }));
  timers.tick();
  await flush();
  eq(statuses.at(-1), 'polling');
  handle.stop();
});

test('live: flap keeps one timer and resumes from cursor', async () => {
  const { f, timers, es, handle } = setup();
  es.last().emitOpen();
  es.last().emitMessage('1', { type: 'a' });
  for (let i = 2; i <= 4; i++) {
    es.last().emitError();
    es.last().emitError(); // a second error on a closed stream changes nothing
    assert(timers.pending.size <= 1, `one timer after flap ${i}`);
    f.queue.push(f.json({ cursor: String(i), events: [{ type: 'p' }] }));
    timers.tick();
    await flush();
    eq(es.last().url, `/api/events?tab=T1&page=&since=${i}`);
    assert(timers.pending.size <= 1, 'still one timer');
    es.last().emitOpen();
    eq(timers.pending.size, 0);
  }
  eq(es.made.filter(e => !e.closed).length, 1, 'only one open stream');
  handle.stop();
});

test('live: a poll answered after the stream reopened is dropped', async () => {
  const { f, timers, es, events, statuses, handle } = setup();
  es.last().emitError();
  f.queue.push(f.json({ cursor: '1', events: [] }));
  timers.tick();
  await flush();
  const reopened = es.last(); // connecting; the next poll is scheduled
  let release;
  f.queue.push(() => new Promise(r => { release = r; }));
  timers.tick(); // poll 2 in flight
  reopened.emitOpen(); // the stream wins the race
  release(new Response('{"cursor":"9","events":[{"type":"late"}]}'));
  await flush();
  eq(events.length, 0, 'late poll not delivered; the stream covers it');
  eq(statuses.at(-1), 'live');
  eq(handle.cursor(), '1', 'cursor not moved by the late poll');
  eq(timers.pending.size, 0, 'no polling while live');
  eq(es.made.length, 2, 'no extra stream');
  handle.stop();
});

test('live: stop cleans up', async () => {
  const { f, timers, es, events, statuses, handle } = setup();
  es.last().emitError();
  f.queue.push(f.json({ cursor: '3', events: [{ type: 'x' }] }));
  timers.tick();
  handle.stop();
  await flush();
  eq(timers.pending.size, 0, 'no timers');
  assert(es.made.every(e => e.closed), 'every stream closed');
  eq(events.length, 0, 'late poll not delivered');
  const n = statuses.length;
  es.last().emitOpen();
  eq(statuses.length, n, 'no status after stop');
});

test('live: a throwing onEvent does not stop updates', async () => {
  const seen = [];
  const { f, timers, es, statuses, handle } = setup({ onEvent: e => { seen.push(e.type); if (e.type === 'bad') throw new Error('page bug'); } });
  const orig = console.error;
  console.error = () => {};
  try {
    es.last().emitOpen();
    es.last().emitMessage('1', { type: 'bad' });
    es.last().emitMessage('2', { type: 'ok' });
    es.last().emitError();
    f.queue.push(f.json({ cursor: '4', events: [{ type: 'bad' }, { type: 'after' }] }));
    timers.tick();
    await flush();
    eq(seen, ['bad', 'ok', 'bad', 'after'], 'every event delivered');
    eq(statuses.at(-1), 'polling');
    eq(es.last().url, '/api/events?tab=T1&page=&since=4', 'stream reopened');
    eq(timers.pending.size, 1, 'next poll scheduled');
  } finally {
    console.error = orig;
    handle.stop();
  }
});

test('live: a poll that lands while the reopened stream is still connecting replaces it', async () => {
  const { f, timers, es, events, handle } = setup();
  es.last().emitError();
  f.queue.push(f.json({ cursor: '1', events: [] }));
  timers.tick();
  await flush();
  const connecting = es.last();
  eq(connecting.url, '/api/events?tab=T1&page=&since=1');
  f.queue.push(f.json({ cursor: '2', events: [{ type: 'x' }] }));
  timers.tick(); // the stream has not opened: this poll delivers and moves the cursor
  await flush();
  eq(events.map(e => e.type), ['x']);
  assert(connecting.closed, 'the stale connecting stream is closed');
  eq(es.last().url, '/api/events?tab=T1&page=&since=2', 'reopened from the new cursor');
  es.last().emitOpen();
  eq(timers.pending.size, 0);
  handle.stop();
});

test('live: a 401 poll is stale and stops everything', async () => {
  const { f, timers, es, statuses, handle } = setup();
  es.last().emitError();
  f.queue.push(f.text('token required', 401));
  timers.tick();
  await flush();
  eq(statuses.at(-1), 'stale');
  eq(timers.pending.size, 0, 'no more polling');
  assert(es.made.every(e => e.closed), 'no stream');
  eq(es.made.length, 1, 'no reopen');
  handle.stop();
});

test('live: a 401 after the stream had opened is still stale', async () => {
  const { f, timers, es, statuses, handle } = setup();
  es.last().emitOpen(); // epoch moves
  es.last().emitError();
  f.queue.push(f.text('token required', 401));
  timers.tick();
  await flush();
  eq(statuses.at(-1), 'stale');
  eq(timers.pending.size, 0);
  handle.stop();
});

test('live: a 401 that lands after a reopened stream connected is still stale', async () => {
  const { f, timers, es, statuses, handle } = setup();
  es.last().emitError();
  f.queue.push(f.json({ cursor: '1', events: [] }));
  timers.tick();
  await flush();
  let release;
  f.queue.push(() => new Promise(r => { release = r; }));
  timers.tick();
  es.last().emitOpen(); // epoch moves while the 401 is in flight
  release(new Response('token required', { status: 401 }));
  await flush();
  eq(statuses.at(-1), 'stale');
  assert(es.made.every(e => e.closed), 'stream closed');
  handle.stop();
});

// Browsers allow about six connections to one host; a stream per hidden tab
// used them up and froze every tab. A hidden tab lets go and comes back.
function fakeDocument(state = 'visible') {
  const listeners = [];
  return {
    visibilityState: state,
    addEventListener: (t, f) => t === 'visibilitychange' && listeners.push(f),
    removeEventListener: (t, f) => { const i = listeners.indexOf(f); if (i >= 0) listeners.splice(i, 1); },
    set(s) { this.visibilityState = s; for (const f of [...listeners]) f(); },
    listeners,
  };
}

test('live: a hidden tab closes its stream and checks in once a minute; visible again reopens from its cursor', async () => {
  const doc = fakeDocument();
  const { f, timers, es, events, statuses, handle } = setup({ document: doc });
  const first = es.last();
  first.emitOpen();
  first.emitMessage('4', { type: 'a', data: 1 });
  doc.set('hidden');
  assert(first.closed, 'hidden: stream closed');
  eq([...timers.pending.values()].map(t => t.ms), [60000], 'hidden: one check-in a minute away');
  first.emitError(); // a late error from the closed stream changes nothing
  eq(timers.pending.size, 1);
  eq(f.calls.length, 0, 'nothing fetched until the minute is up');

  f.queue.push(f.json({ cursor: '6', events: [{ type: 'b', data: 2 }] }));
  timers.tick();
  await flush();
  eq(f.calls[0].url, '/api/state?tab=T1&page=&since=4', 'the check-in is the poll, with the tab');
  eq(events.map(e => [e.type, e.cursor]), [['a', '4'], ['b', '6']], 'its events delivered as usual');
  eq(es.made.length, 1, 'hidden: no stream reopened');
  eq([...timers.pending.values()].map(t => t.ms), [60000], 'the next check-in a minute later');
  eq(statuses, ['live'], 'no status change while hidden');

  f.queue.push(new Error('refused'));
  timers.tick();
  await flush();
  eq([...timers.pending.values()].map(t => t.ms), [60000], 'a failed check-in tries again in a minute');

  doc.set('visible');
  eq(timers.pending.size, 0, 'visible: check-ins stop');
  const second = es.last();
  assert(second !== first, 'visible: a new stream');
  eq(second.url, '/api/events?tab=T1&page=&since=6', 'picks up after what it had');
  second.emitOpen();
  second.emitMessage('7', { type: 'c', data: 3 });
  eq(events.map(e => e.cursor), ['4', '6', '7']);
  handle.stop();
  eq(doc.listeners.length, 0, 'stop removes the listener');
});

test('live: the hidden check-in interval is injectable', () => {
  const doc = fakeDocument();
  const { timers, es, handle } = setup({ document: doc, hiddenInterval: 5 });
  es.last().emitOpen();
  doc.set('hidden');
  eq([...timers.pending.values()].map(t => t.ms), [5]);
  handle.stop();
  eq(timers.pending.size, 0);
});

test('live: a check-in still in flight when the tab is shown is dropped', async () => {
  const doc = fakeDocument();
  const { f, timers, es, events, handle } = setup({ document: doc });
  es.last().emitOpen();
  es.last().emitMessage('1', { type: 'a' });
  doc.set('hidden');
  let release;
  f.queue.push(() => new Promise(r => { release = r; }));
  timers.tick(); // check-in in flight
  doc.set('visible'); // the new stream starts from cursor 1 and covers it
  release(new Response('{"cursor":"2","events":[{"type":"late"}]}'));
  await flush();
  eq(events.map(e => e.type), ['a'], 'not delivered twice');
  eq(handle.cursor(), '1');
  eq(es.made.filter(e => !e.closed).length, 1, 'one stream');
  handle.stop();
});

test('live: a 401 check-in while hidden is stale', async () => {
  const doc = fakeDocument();
  const { f, timers, es, statuses, handle } = setup({ document: doc });
  es.last().emitOpen();
  doc.set('hidden');
  f.queue.push(f.text('token required', 401));
  timers.tick();
  await flush();
  eq(statuses.at(-1), 'stale');
  eq(timers.pending.size, 0);
  doc.set('visible');
  eq(es.made.length, 1, 'stale: nothing reopens');
  handle.stop();
});

test('live: a tab that starts hidden checks in at once and opens no stream until shown', async () => {
  const doc = fakeDocument('hidden');
  const { f, timers, es, handle } = setup({ document: doc, page: 'PLAN1' });
  eq(es.made.length, 0);
  eq([...timers.pending.values()].map(t => t.ms), [0], 'checks in now');
  f.queue.push(f.json({ cursor: '3', events: [] }));
  timers.tick();
  await flush();
  eq(f.calls[0].url, '/api/state?tab=T1&page=PLAN1');
  eq([...timers.pending.values()].map(t => t.ms), [60000]);
  doc.set('visible');
  eq(es.made.length, 1);
  eq(es.last().url, '/api/events?tab=T1&page=PLAN1&since=3');
  handle.stop();
});

test('live: the tab id and page go on the stream and the poll', async () => {
  const { f, timers, es, handle } = setup({ page: 'PLAN 1', randomUUID: () => 'a-b' });
  eq(handle.tab(), 'a-b');
  eq(es.last().url, '/api/events?tab=a-b&page=PLAN%201');
  es.last().emitError();
  f.queue.push(f.json({ cursor: '1', events: [] }));
  timers.tick();
  await flush();
  eq(f.calls[0].url, '/api/state?tab=a-b&page=PLAN%201');
  eq(es.last().url, '/api/events?tab=a-b&page=PLAN%201&since=1');
  handle.stop();
});

test('live: each page load gets its own tab id from crypto.randomUUID', () => {
  const es = fakeEventSource();
  const opts = { events: '/e', poll: '/p', onEvent() {}, EventSource: es.FakeES, setTimeout() {}, clearTimeout() {} };
  const a = live(opts), b = live(opts);
  assert(/^[0-9a-f-]{36}$/.test(a.tab()), `uuid: ${a.tab()}`);
  assert(a.tab() !== b.tab(), 'two loads, two ids');
  eq(es.made[0].url, `/e?tab=${a.tab()}&page=`);
  a.stop();
  b.stop();
});

test('live: onShow runs only for a show event naming this tab, and not through onEvent', async () => {
  const shown = [];
  const { f, timers, es, events, handle } = setup({ onShow: p => shown.push(p) });
  es.last().emitOpen();
  es.last().emitMessage('1', { type: 'show', data: { tab: 'OTHER', page: 'PLAN2' } });
  es.last().emitMessage('2', { type: 'show', data: { tab: 'T1', page: 'PLAN1' } });
  es.last().emitMessage('3', { type: 'x', data: 1 });
  eq(shown, ['PLAN1']);
  eq(events.map(e => e.type), ['x'], 'show events are not page events');
  eq(handle.cursor(), '3');
  es.last().emitError();
  f.queue.push(f.json({ cursor: '5', events: [{ type: 'show', data: { tab: 'T1', page: 'PLAN3' } }] }));
  timers.tick();
  await flush();
  eq(shown, ['PLAN1', 'PLAN3'], 'a polled show moves too');
  eq(handle.cursor(), '5');
  handle.stop();
});

test('live: a throwing onShow does not stop updates', () => {
  const { es, events, handle } = setup({ onShow: () => { throw new Error('page bug'); } });
  const orig = console.error;
  console.error = () => {};
  try {
    es.last().emitOpen();
    es.last().emitMessage('1', { type: 'show', data: { tab: 'T1', page: 'P' } });
    es.last().emitMessage('2', { type: 'x' });
    eq(events.map(e => e.type), ['x']);
  } finally {
    console.error = orig;
    handle.stop();
  }
});

test('live: a stale event reaches onNewBuild and onEvent', () => {
  let n = 0;
  const { es, events, handle } = setup({ onNewBuild: () => n++ });
  es.last().emitOpen();
  es.last().emitMessage('1', { type: 'stale', data: {} });
  eq(n, 1);
  eq(events.map(e => e.type), ['stale']);
  es.last().emitMessage('2', { type: 'other', data: {} });
  eq(n, 1, 'only a stale event');
  handle.stop();
});

test('live: a throwing onNewBuild does not stop the stream', () => {
  const { es, events, handle } = setup({ onNewBuild: () => { throw new Error('boom'); } });
  es.last().emitOpen();
  es.last().emitMessage('1', { type: 'stale', data: {} });
  es.last().emitMessage('2', { type: 'after', data: {} });
  eq(events.map(e => e.type), ['stale', 'after']);
  handle.stop();
});
