// The scratch tool's probe on the shared scaffolding: the geometry and wait
// helpers checked in node, then a browser launched or skipped.
import {
  check,
  drawn,
  inside,
  launchOrSkip,
  onCleanup,
  run,
  sideBySide,
  until,
} from './build/probe.mjs';

run(async () => {
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
  let cleaned = false;
  onCleanup(() => {
    cleaned = true;
  });
  onCleanup(() => {
    throw new Error('a cleanup that throws is ignored');
  });
  process.on('exit', () => {
    if (!cleaned) {
      console.error('cleanup did not run');
      process.exitCode = 1;
    }
  });

  const browser = await launchOrSkip();
  if (!browser) return;
  const page = await browser.newPage();
  await page.setContent('<p style="width:50px;height:20px">x</p>');
  check(
    'a page draws in chromium',
    drawn(await page.locator('p').boundingBox()),
  );
});
