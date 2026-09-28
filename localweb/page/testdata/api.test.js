import { test, eq, assert } from './harness.js';
import { createApi, ApiError } from '/_kit/api.js';
import { fakeFetch } from './fakes.js';

test('api: json round trip', async () => {
  const f = fakeFetch();
  f.queue.push(f.json({ ok: true }));
  const api = createApi({ fetch: f.fetch });
  eq(await api.post('/label', { id: 1 }), { ok: true });
  const { url, init } = f.calls[0];
  eq(url, '/api/label');
  eq(init.method, 'POST');
  eq(init.body, '{"id":1}');
  eq(init.credentials, 'same-origin');
  eq(new Headers(init.headers).get('Content-Type'), 'application/json');
  eq(new Headers(init.headers).get('X-Local-Token'), null, 'no token header by default');

  f.queue.push(f.json([1]));
  const tok = createApi({ fetch: f.fetch, token: 't' });
  eq(await tok.get('/s', { since: '5' }), [1]);
  eq(f.calls[1].url, '/api/s?since=5');
  eq(f.calls[1].init.method, 'GET');
  eq(new Headers(f.calls[1].init.headers).get('X-Local-Token'), 't');
  eq(f.calls[1].init.body, undefined, 'GET has no body');

  f.queue.push(f.json({}), f.json({}));
  await tok.put('/x', { a: 1 });
  await tok.del('/x');
  eq([f.calls[2].init.method, f.calls[3].init.method], ['PUT', 'DELETE']);
});

test('api: plain-text error and 204', async () => {
  const f = fakeFetch();
  const api = createApi({ fetch: f.fetch });
  f.queue.push(f.text('token required\n', 401));
  let err;
  try { await api.get('/state'); } catch (e) { err = e; }
  assert(err instanceof ApiError, 'ApiError');
  eq([err.status, err.message], [401, 'token required']);

  f.queue.push(f.json({ error: 'stale' }, 409));
  err = null;
  try { await api.post('/label', {}); } catch (e) { err = e; }
  eq([err.status, err.message], [409, 'stale']);

  f.queue.push(f.text(null, 204));
  eq(await api.post('/label', {}), null);
  f.queue.push(f.text('', 200));
  eq(await api.get('/empty'), null, 'empty 200 is null');
});
