// A page that reads an ambient wire type and the kit's global.
export function show(view: PendingView): HTMLElement {
  const el = kit.el('p');
  el.textContent = `${view.doc}: ${(view.changes || []).length} changes`;
  return el;
}
