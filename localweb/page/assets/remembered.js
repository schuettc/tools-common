// remembered.js: what the page kit remembers for a reader, in localStorage.
// A throw means nothing is remembered: a frame sandboxed without
// allow-same-origin can't reach localStorage at all, and the page still draws.
export const remembered = {
  get(key) {
    try {
      return localStorage.getItem(key);
    } catch {
      return null;
    }
  },
  set(key, value) {
    try {
      localStorage.setItem(key, value);
    } catch {
      // not remembered, still applied
    }
  },
  remove(key) {
    try {
      localStorage.removeItem(key);
    } catch {
      // nothing to forget
    }
  },
};
