// The scratch tool's linter: the shared base, as diazo, cull and sift use it.
import base from './build/eslint.base.mjs';

export default base({ root: import.meta.dirname, ignores: ['kit.d.ts'] });
