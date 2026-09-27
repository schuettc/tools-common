import { test, eq, fixture } from './harness.js';

test('smoke: harness runs and kit assets load', async () => {
  const kit = await import('/_kit/kit.js');
  eq(typeof kit, 'object');
  eq(fixture().id, 'fx');
});
