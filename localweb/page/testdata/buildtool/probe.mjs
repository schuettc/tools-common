// The scratch tool's probe on the shared scaffolding: the geometry and wait
// helpers checked in node, then a browser launched or skipped, with casebook's
// own rule for when one is required (SCRATCH_CI stands in for its CI) and its
// floor on how many checks a full run passes (PROBE_MIN_CHECKS).
import {
  check,
  counts,
  drawn,
  inside,
  launchOrSkip,
  onCleanup,
  run,
  sideBySide,
  until,
} from './build/probe.mjs';

const main = async () => {
  const box = { x: 0, y: 0, width: 100, height: 100 };
  check('a box with size is drawn', drawn(box));
  check(
    'an empty box is not drawn',
    !drawn({ ...box, width: 0 }) && !drawn(null),
  );
  check('inside', inside({ x: 10, y: 10, width: 20, height: 20 }, box));
  check('not inside', !inside({ x: 90, y: 0, width: 20, height: 20 }, box));
  check('side by side', sideBySide(box, { ...box, x: 100 }));
  check('not side by side', !sideBySide(box, { ...box, x: 50 }));
  let n = 0;
  check(
    'until polls to a truthy answer',
    (await until(() => ++n >= 3)) === true,
  );
  check('until gives up with false', (await until(() => false, 100)) === false);
  const t0 = Date.now();
  check(
    'until waits 4000 ms by default',
    (await until(() => Date.now() - t0 > 3600)) === true,
  );
  // Cleanups run in the order they were added: the browser (added at
  // launch) closes before the serve fixture stops, as every tool did.
  order = [];
  onCleanup(() => order.push('first'));
  onCleanup(() => {
    throw new Error('a cleanup that throws is ignored');
  });
  const browser = await launchOrSkip({
    required:
      process.env.KIT_BROWSER === 'required' || !!process.env.SCRATCH_CI,
  });
  launched = true;
  onCleanup(() =>
    order.push(browser?.isConnected() ? 'serve, browser still open' : 'serve'),
  );
  onCleanup(() => order.push('build'));
  want = ['first', 'serve', 'build'];

  if (!browser) return;
  const page = await browser.newPage();
  await page.setContent('<p style="width:50px;height:20px">x</p>');
  check(
    'a page draws in chromium',
    drawn(await page.locator('p').boundingBox()),
  );
};

let launched = false;
let want = [];
let order = [];
const min = Number(process.env.PROBE_MIN_CHECKS || 0);

run(main, {
  after() {
    if (!launched) return;
    check(
      'cleanups ran in the order added, the browser closed first',
      JSON.stringify(order) === JSON.stringify(want),
      JSON.stringify(order),
    );
    const { passes } = counts();
    if (min) check(`at least ${min} checks passed`, passes >= min, `${passes}`);
  },
});
