// Fakes for the network and the clock, shared by the api and live suites.

export function fakeFetch() {
  const calls = [];
  const queue = [];
  const fetch = (url, init = {}) => {
    calls.push({ url: String(url), init });
    const next = queue.shift();
    if (!next) return Promise.reject(new Error('no response queued'));
    return next instanceof Error ? Promise.reject(next) : Promise.resolve(next());
  };
  const json = (obj, status = 200) => () => new Response(JSON.stringify(obj), { status, headers: { 'Content-Type': 'application/json' } });
  const text = (body, status) => () => new Response(body, { status });
  return { fetch, calls, queue, json, text };
}

export function fakeTimers() {
  let id = 0;
  const pending = new Map();
  return {
    pending,
    setTimeout: (fn, ms) => { pending.set(++id, { fn, ms }); return id; },
    clearTimeout: i => pending.delete(i),
    // tick runs every timer due now (all of them: fakes have no clock).
    tick() {
      const due = [...pending.entries()];
      pending.clear();
      for (const [, t] of due) t.fn();
    },
  };
}

export function fakeEventSource() {
  const made = [];
  class FakeES {
    constructor(url) {
      this.url = url;
      this.closed = false;
      made.push(this);
    }
    close() { this.closed = true; }
    emitOpen() { this.onopen && this.onopen(new Event('open')); }
    emitMessage(id, obj) {
      this.onmessage && this.onmessage(new MessageEvent('message', { data: JSON.stringify(obj), lastEventId: id }));
    }
    emitError() { this.onerror && this.onerror(new Event('error')); }
  }
  return { FakeES, made, last: () => made[made.length - 1] };
}

// flush lets queued fetch resolutions run; a Response body resolves over
// several turns, so wait a few.
export async function flush() {
  for (let i = 0; i < 5; i++) await new Promise(r => setTimeout(r, 0));
}
