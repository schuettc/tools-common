// Live updates: a server-sent event stream, with 2-second polling while the
// stream is down.
//
// Wire contract (each tool's Go handlers implement it):
//   GET <events>?tab=<id>&page=<page>&since=<cursor>
//                                 text/event-stream, headers flushed on
//                                 connect (so the client sees it open before
//                                 the first event). Every message is the
//                                 default "message" event; its id is the
//                                 cursor after it and its data is JSON
//                                 {type, data}. since is omitted when the
//                                 client has no cursor yet.
//   GET <poll>?tab=<id>&page=<page>&since=<cursor>
//                                 JSON {cursor, events: [{type, data}]}: what
//                                 happened after since, and the new cursor.
//
// tab is a random id made once per page load; page is the tool's name for
// what the tab shows ("" for its front page). Both are on every request, so
// the server can keep its list of open tabs (localweb.Tabs): a tab is open
// while its stream is, and for a while after each poll.
//
// A {type: "show", data: {tab, page}} event asks one tab to show page: the
// tab it names runs onShow(page); every other tab ignores it. Show events
// never reach onEvent.
//
// A 401 from the poll means the server restarted and the token is gone:
// status becomes "stale" and the client stops for good (no stream, no polls).
//
// The client closes a broken stream itself and reopens it with the newest
// cursor, rather than letting the browser retry with a stale Last-Event-ID.
//
// A hidden tab holds no connection: browsers allow about six to one host, so
// a stream per background tab froze every tab of the tool. It closes the
// stream when the tab is hidden and, while hidden, polls once a minute
// instead (Chrome runs a long-hidden tab's timers about once a minute
// anyway): one short request that checks the tab in and brings the events
// since its cursor, such as a show. A tab that loads hidden checks in at once.
// When shown, it stops polling and reopens the stream from its cursor.

export function live(opts) {
  const ES = opts.EventSource || globalThis.EventSource;
  const doFetch = opts.fetch || ((...a) => fetch(...a));
  const setT = opts.setTimeout || ((f, ms) => setTimeout(f, ms));
  const clearT = opts.clearTimeout || (id => clearTimeout(id));
  const interval = opts.interval ?? 2000;
  const hiddenInterval = opts.hiddenInterval ?? 60000;
  const tab = (opts.randomUUID || (() => crypto.randomUUID()))();
  const page = opts.page ?? '';
  const onStatus = opts.onStatus || (() => {});
  const doc = opts.document || globalThis.document;

  let cursor = opts.cursor || '';
  let es = null;
  let esOpen = false;
  let timer = null;
  let status = '';
  let stopped = false;
  let epoch = 0; // bumped whenever the stream opens; stale polls are dropped

  const withQuery = url =>
    `${url}${url.includes('?') ? '&' : '?'}tab=${encodeURIComponent(tab)}&page=${encodeURIComponent(page)}` +
    (cursor ? `&since=${encodeURIComponent(cursor)}` : '');

  function setStatus(s) {
    if (stopped || s === status) return;
    status = s;
    onStatus(s);
  }

  // deliver never lets a page handler's exception stop the stream or polling.
  function deliver(type, data, c) {
    cursor = c;
    try {
      if (type !== 'show') opts.onEvent({ type, data, cursor: c });
      else if (data && data.tab === tab && opts.onShow) opts.onShow(data.page);
    } catch (err) {
      console.error(`live: ${type === 'show' ? 'onShow' : 'onEvent'} threw`, err);
    }
  }

  function schedule(ms = interval) {
    if (timer !== null || stopped) return;
    timer = setT(poll, ms);
  }

  function cancelPoll() {
    if (timer !== null) clearT(timer);
    timer = null;
  }

  function openStream() {
    const s = new ES(withQuery(opts.events));
    es = s;
    esOpen = false;
    s.onopen = () => {
      if (stopped || es !== s) return;
      esOpen = true;
      epoch++;
      cancelPoll();
      setStatus('live');
    };
    s.onmessage = m => {
      if (stopped || es !== s) return;
      let msg;
      try {
        msg = JSON.parse(m.data);
      } catch {
        return; // not ours
      }
      deliver(msg.type, msg.data, m.lastEventId || cursor);
    };
    s.onerror = () => {
      if (stopped || es !== s || hidden()) return;
      s.close();
      es = null;
      if (status !== 'down') setStatus('polling');
      schedule();
    };
  }

  async function poll() {
    timer = null;
    const mine = epoch;
    let body;
    try {
      const res = await doFetch(withQuery(opts.poll), { credentials: 'same-origin' });
      if (res.status === 401) {
        // stale whatever the epoch: a stream that opened meanwhile is dead too
        if (stopped) return;
        setStatus('stale');
        halt();
        return;
      }
      if (!res.ok) throw new Error(`poll: HTTP ${res.status}`);
      body = await res.json();
    } catch {
      if (stopped || mine !== epoch) return;
      if (hidden()) return schedule(hiddenInterval);
      setStatus('down');
      schedule();
      return;
    }
    if (stopped || mine !== epoch || (es && esOpen)) return;
    for (const e of body.events || []) deliver(e.type, e.data, body.cursor);
    if (body.cursor) cursor = body.cursor;
    if (hidden()) return schedule(hiddenInterval); // a check-in: no stream, no status
    setStatus('polling');
    // A stream still connecting was opened from an older cursor; replace it,
    // or it would replay what this poll just delivered.
    if (es && !esOpen) {
      es.close();
      es = null;
    }
    if (!es) openStream();
    schedule();
  }

  const hidden = () => !!doc && doc.visibilityState === 'hidden';

  function onVisibility() {
    if (stopped) return;
    cancelPoll();
    epoch++; // a poll in flight is dropped: the stream or the check-ins cover it
    if (hidden()) {
      if (es) es.close();
      es = null;
      schedule(hiddenInterval);
      return;
    }
    if (!es) openStream();
  }

  function halt() {
    stopped = true;
    cancelPoll();
    if (es) es.close();
    es = null;
    if (doc) doc.removeEventListener('visibilitychange', onVisibility);
  }

  if (doc) doc.addEventListener('visibilitychange', onVisibility);
  if (!hidden()) openStream();
  else schedule(0);

  return {
    tab: () => tab,
    cursor: () => cursor,
    stop: halt,
  };
}
