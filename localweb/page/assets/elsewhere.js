// elsewhere.js: the loop that keeps the bar's "also open" list current. The
// tool's read says what to list; moved from diazo's readPlans.

// keepElsewhere returns the call to make whenever the list may have changed.
// Each call runs read() for the items and sets the bar only when they changed
// (their onClick aside), so an open list stays open across unrelated events. A
// call while a read is running reads once more after it, never twice at once.
// A failed read leaves the list as it was.
export function keepElsewhere(bar, read) {
  let sig = null;
  let busy = false;
  let again = false;
  return async () => {
    again = true;
    if (busy) return;
    busy = true;
    while (again) {
      again = false;
      try {
        const items = await read();
        const next = JSON.stringify(items);
        if (next === sig) continue;
        sig = next;
        bar.setElsewhere(items);
      } catch {
        // The next call reads it again.
      }
    }
    busy = false;
  };
}
