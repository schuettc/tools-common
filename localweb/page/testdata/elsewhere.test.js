import { test, eq } from './harness.js';
import { keepElsewhere } from '/_kit/elsewhere.js';

const fakeBar = () => {
  const sets = [];
  return { sets, setElsewhere: items => sets.push(items) };
};
const item = (label, detail) => ({ label, detail, onClick() {} });

// A read that waits until the test lets it go.
function held() {
  const waiting = [];
  let running = 0;
  let most = 0;
  return {
    waiting,
    most: () => most,
    read: () => new Promise((resolve, reject) => {
      most = Math.max(most, ++running);
      waiting.push({
        give: v => { running--; resolve(v); },
        fail: () => { running--; reject(new Error('no list')); },
      });
    }),
  };
}

test('keepElsewhere: each call reads the list and sets the bar', async () => {
  const b = fakeBar();
  const items = [item('a', 'needs you')];
  await keepElsewhere(b, async () => items)();
  eq(b.sets.length, 1);
  eq(b.sets[0], items);
});

test('keepElsewhere: the bar is set only when the list changed', async () => {
  const b = fakeBar();
  let detail = 'working';
  const keep = keepElsewhere(b, async () => [item('a', detail)]);
  await keep();
  await keep();
  eq(b.sets.length, 1, 'a new onClick on the same row is no change');
  detail = 'needs you';
  await keep();
  eq(b.sets.length, 2);
});

test('keepElsewhere: calls during a read read once more after it, never at once', async () => {
  const h = held();
  const b = fakeBar();
  const keep = keepElsewhere(b, h.read);
  const first = keep();
  keep();
  keep();
  eq(h.waiting.length, 1, 'the running read is the only one');
  h.waiting[0].give([item('old')]);
  await new Promise(r => setTimeout(r));
  eq(h.waiting.length, 2, 'one more read after it');
  h.waiting[1].give([item('new')]);
  await first;
  eq(h.waiting.length, 2);
  eq(h.most(), 1);
  eq(b.sets.at(-1)[0].label, 'new');
});

test('keepElsewhere: a failed read leaves the list, and the next call reads again', async () => {
  const h = held();
  const b = fakeBar();
  const keep = keepElsewhere(b, h.read);
  const first = keep();
  h.waiting[0].give([item('a')]);
  await first;
  const second = keep();
  h.waiting[1].fail();
  await second;
  eq(b.sets.length, 1, 'the list stays as it was');
  const third = keep();
  h.waiting[2].give([item('b')]);
  await third;
  eq(b.sets.at(-1)[0].label, 'b');
});

test('keepElsewhere: a call during a read that fails still reads once more', async () => {
  const h = held();
  const b = fakeBar();
  const keep = keepElsewhere(b, h.read);
  const first = keep();
  keep();
  h.waiting[0].fail();
  await new Promise(r => setTimeout(r));
  eq(h.waiting.length, 2);
  h.waiting[1].give([item('a')]);
  await first;
  eq(b.sets.length, 1);
});
