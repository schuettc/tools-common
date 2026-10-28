// The family page tools' browser probe scaffolding: the check, wait and
// cleanup helpers, launching chromium (or skipping when none is installed),
// and the geometry helpers. Copied from tools-common (localweb/page/build/) by
// a tool's build and checked against the version it pins, so edit it there.
// Each tool's probe keeps its own fixture and its own checks:
//
//   import { check, launchOrSkip, onCleanup, run } from './<copied>/probe.mjs';
//   run(async () => {
//     const browser = await launchOrSkip();
//     if (!browser) return;
//     const serve = await startServe();
//     onCleanup(() => serve.stop());
//     ...
//   });
//
// KIT_BROWSER=required turns a missing chromium into a failure (CI's browser
// job); otherwise the probe says it skipped and passes. playwright-core is the
// tool's own devDependency.

import pkg from 'playwright-core';

const { chromium } = pkg;

/** KIT_BROWSER=required: a missing browser fails instead of skipping. */
export const required = process.env.KIT_BROWSER === 'required';

let passes = 0;
let fails = 0;

/** Count and print one check. */
export function check(label, ok, detail = '') {
  if (ok) {
    console.log(`  ✓ ${label}`);
    passes++;
  } else {
    console.error(`  ✗ ${label}${detail ? ` — ${detail}` : ''}`);
    fails++;
  }
}

export const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

/** fn's first truthy answer within ms, polled every 50 ms; false if none.
 * A throw counts as not yet. */
export async function until(fn, ms = 3000) {
  const end = Date.now() + ms;
  for (;;) {
    let v;
    try {
      v = await fn();
    } catch {
      v = false;
    }
    if (v) return v;
    if (Date.now() > end) return false;
    await sleep(50);
  }
}

const cleanups = [];
let cleaning = null;

/** Run fn at cleanup (the end of run, or SIGINT/SIGTERM), last added first.
 * A cleanup that throws is ignored: what it closes may already be gone. */
export function onCleanup(fn) {
  cleanups.push(fn);
}

/** Run every cleanup once. */
export function cleanup() {
  cleaning ??= (async () => {
    while (cleanups.length) {
      try {
        await cleanups.pop()();
      } catch {
        // already gone
      }
    }
  })();
  return cleaning;
}
process.on('SIGINT', () => cleanup().finally(() => process.exit(130)));
process.on('SIGTERM', () => cleanup().finally(() => process.exit(143)));

/** A headless chromium, closed at cleanup; null when none is installed,
 * unless KIT_BROWSER=required, when it throws. */
export async function launchOrSkip(options = { headless: true }) {
  let browser;
  try {
    browser = await chromium.launch(options);
  } catch (err) {
    if (required)
      throw new Error(`probe: no chromium (KIT_BROWSER=required): ${err}`);
    console.log('probe: no chromium; skipped');
    return null;
  }
  onCleanup(() => browser.close());
  return browser;
}

/** Run main, then cleanup, print the count and exit: 1 if a check failed or
 * main threw. */
export function run(main) {
  return Promise.resolve()
    .then(main)
    .catch((err) => {
      console.error(err);
      fails++;
    })
    .finally(async () => {
      await cleanup();
      console.log(`\nprobe: ${passes} passed, ${fails} failed`);
      process.exit(fails ? 1 : 0);
    });
}

/** A box that is drawn: some width and height. */
export const drawn = (b) => !!b && b.width > 0 && b.height > 0;
/** a lies inside b (to the pixel). */
export const inside = (a, b) =>
  drawn(a) &&
  drawn(b) &&
  a.x >= b.x - 1 &&
  a.y >= b.y - 1 &&
  a.x + a.width <= b.x + b.width + 1 &&
  a.y + a.height <= b.y + b.height + 1;
/** a and b on one row, a left of b, not overlapping. */
export const sideBySide = (a, b) =>
  drawn(a) && drawn(b) && Math.abs(a.y - b.y) < 2 && a.x + a.width <= b.x;
