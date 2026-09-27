// A tiny test runner for the kit's JS, driven by browser_test.go in headless
// Chrome. run.html imports a suite, awaits run(), and writes the result.
const tests = [];

export function test(name, fn) { tests.push({ name, fn }); }

export function assert(cond, msg) {
  if (!cond) throw new Error(msg || 'assertion failed');
}

export function eq(actual, expected, msg) {
  const a = JSON.stringify(actual), e = JSON.stringify(expected);
  if (a !== e) throw new Error(`${msg ? msg + ': ' : ''}got ${a}, want ${e}`);
}

export function fixture() {
  const fx = document.getElementById('fx');
  fx.replaceChildren();
  return fx;
}

export async function run() {
  const failures = [];
  for (const t of tests) {
    try {
      await Promise.race([
        t.fn(),
        new Promise((_, rej) => setTimeout(() => rej(new Error('timeout')), 5000)),
      ]);
    } catch (e) {
      failures.push(`${t.name}: ${e && e.message ? e.message : e}`);
    }
  }
  const head = failures.length ? `FAIL ${failures.length}` : `PASS ${tests.length}`;
  return [head, ...failures].join('\n');
}
