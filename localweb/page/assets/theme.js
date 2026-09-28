// Theme: follow the system, or a remembered light/dark override.
// localStorage["<tool>.theme"] holds "light" or "dark"; absent means system.

const MODES = ['system', 'light', 'dark'];

function read(key) {
  try {
    const v = localStorage.getItem(key);
    return v === 'light' || v === 'dark' ? v : 'system';
  } catch {
    return 'system'; // storage denied (privacy mode): follow the system
  }
}

function write(key, mode) {
  try {
    if (mode === 'system') localStorage.removeItem(key);
    else localStorage.setItem(key, mode);
  } catch {
    // not remembered, still applied
  }
}

function apply(mode) {
  const root = document.documentElement;
  if (mode === 'system') root.removeAttribute('data-theme');
  else root.dataset.theme = mode;
}

// initTheme applies the remembered mode and returns its controls. A control
// element, when given, cycles system → light → dark on click and shows the
// current mode in its title.
export function initTheme(tool, control) {
  const key = `${tool}.theme`;
  let mode = read(key);
  apply(mode);
  const show = () => {
    if (!control) return;
    control.title = `theme: ${mode}`;
    control.dataset.mode = mode;
  };
  const theme = {
    get: () => mode,
    set(m) {
      if (!MODES.includes(m)) throw new Error(`unknown theme mode: ${m}`);
      mode = m;
      apply(m);
      write(key, m);
      show();
    },
    cycle() {
      theme.set(MODES[(MODES.indexOf(mode) + 1) % MODES.length]);
      return mode;
    },
  };
  show();
  if (control) control.addEventListener('click', () => theme.cycle());
  return theme;
}
