import { test, assert } from './harness.js';

// Fails on purpose: TestHarnessReportsFailure proves the harness can fail.
test('fail: on purpose', () => assert(false, 'deliberate'));
