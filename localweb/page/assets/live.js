// Live updates: a server-sent event stream, with 2-second polling while the
// stream is down.
//
// Wire contract (each tool's Go handlers implement it):
//   GET <events>?since=<cursor>   text/event-stream, headers flushed on
//                                 connect (so the client sees it open before
//                                 the first event). Every message is the
//                                 default "message" event; its id is the
//                                 cursor after it and its data is JSON
//                                 {type, data}. since is omitted when the
//                                 client has no cursor yet.
//   GET <poll>?since=<cursor>     JSON {cursor, events: [{type, data}]}: what
//                                 happened after since, and the new cursor.
//
// A 401 from the poll means the server restarted and the token is gone:
// status becomes "stale" and the client stops for good (no stream, no polls).
//
// The client closes a broken stream itself and reopens it with the newest
// cursor, rather than letting the browser retry with a stale Last-Event-ID.

export function live(opts) {
  const ES = opts.EventSource || globalThis.EventSource;
  const doFetch = opts.fetch || ((...a) => fetch(...a));
  const setT = opts.setTimeout || ((f, ms) => setTimeout(f, ms));
  const clearT = opts.clearTimeout || (id => clearTimeout(id));
  const interval = opts.interval ?? 2000;
  const onStatus = opts.onStatus || (() => {});

  let cursor = opts.cursor || '';
  let es = null;
  let esOpen = false;
  let timer = null;
  let status = '';
  let stopped = false;
  let epoch = 0; // bumped whenever the stream opens; stale polls are dropped

  const withSince = url => (cursor ? `${url}${url.includes('?') ? '&' : '?'}since=${encodeURIComponent(cursor)}` : url);

  function setStatus(s) {
    if (stopped || s === status) return;
    status = s;
    onStatus(s);
  }

  // deliver never lets a page handler's exception stop the stream or polling.
  function deliver(type, data, c) {
    cursor = c;
    try {
      opts.onEvent({ type, data, cursor: c });
    } catch (err) {
      console.error('live: onEvent threw', err);
    }
  }

  function schedule() {
    if (timer !== null || stopped) return;
    timer = setT(poll, interval);
  }

  function cancelPoll() {
    if (timer !== null) clearT(timer);
    timer = null;
  }

  function openStream() {
    const s = new ES(withSince(opts.events));
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
      if (stopped || es !== s) return;
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
      const res = await doFetch(withSince(opts.poll), { credentials: 'same-origin' });
      if (res.status === 401) {
        if (stopped || mine !== epoch) return;
        setStatus('stale');
        halt();
        return;
      }
      if (!res.ok) throw new Error(`poll: HTTP ${res.status}`);
      body = await res.json();
    } catch {
      if (stopped || mine !== epoch) return;
      setStatus('down');
      schedule();
      return;
    }
    if (stopped || mine !== epoch || status === 'live') return;
    for (const e of body.events || []) deliver(e.type, e.data, body.cursor);
    if (body.cursor) cursor = body.cursor;
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

  function halt() {
    stopped = true;
    cancelPoll();
    if (es) es.close();
    es = null;
  }

  openStream();

  return {
    cursor: () => cursor,
    stop: halt,
  };
}
