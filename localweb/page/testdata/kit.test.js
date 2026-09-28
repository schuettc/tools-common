import { test, eq } from './harness.js';
import * as kit from '/_kit/kit.js';

test('kit: entry exposes everything', () => {
  const want = ['initTheme', 'createKeys', 'createApi', 'ApiError', 'live', 'h', 'bar', 'list', 'facts', 'card', 'buttons', 'codeBlock', 'fold', 'noteField'];
  eq(want.filter(n => typeof kit[n] !== 'function'), []);
});
