// JSON API client for a tool's /api/. A browser page is authenticated by
// localweb's HttpOnly cookie, and a same-origin fetch write carries the Origin
// localweb requires, so the page never handles the token. A caller that has
// the token (a test, a non-browser client) passes it and it is sent as
// X-Local-Token.
//
// A 401 means the server restarted and this page's token is gone: the error's
// isStale is true, and onStale (if given) is called once, so the page can show
// the tool's stale message.

export class ApiError extends Error {
  constructor(status, message) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
  }

  /** 401: the server no longer knows this page's token (it restarted). */
  get isStale() {
    return this.status === 401;
  }
}

export function createApi(opts = {}) {
  const base = opts.base ?? '/api';
  const doFetch = opts.fetch || ((...a) => fetch(...a));
  let staleSeen = false;

  async function request(method, path, body, query) {
    let url = base + path;
    if (query) {
      const q = new URLSearchParams(query).toString();
      if (q) url += (url.includes('?') ? '&' : '?') + q;
    }
    const headers = {};
    if (opts.token) headers['X-Local-Token'] = opts.token;
    const init = { method, headers, credentials: 'same-origin' };
    if (body !== undefined) {
      headers['Content-Type'] = 'application/json';
      init.body = JSON.stringify(body);
    }
    const res = await doFetch(url, init);
    const text = res.status === 204 ? '' : await res.text();
    if (!res.ok) {
      let message = text.trim();
      try {
        const j = JSON.parse(text);
        if (j && typeof j.error === 'string') message = j.error;
      } catch {
        // plain text (localweb's http.Error): keep it
      }
      const err = new ApiError(res.status, message || res.statusText || `HTTP ${res.status}`);
      if (err.isStale && !staleSeen) {
        staleSeen = true;
        if (opts.onStale) opts.onStale(err);
      }
      throw err;
    }
    return text ? JSON.parse(text) : null;
  }

  return {
    get: (path, query) => request('GET', path, undefined, query),
    post: (path, body) => request('POST', path, body),
    put: (path, body) => request('PUT', path, body),
    del: path => request('DELETE', path),
  };
}
