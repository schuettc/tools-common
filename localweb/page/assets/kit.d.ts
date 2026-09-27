// Types for /_kit/kit.js, the tools-common localweb/page kit.
//
// TypeScript consumers bundle with esbuild and leave the kit external:
//   esbuild --external:/_kit/*
//   tsconfig "paths": { "/_kit/kit.js": ["<tools-common dir>/localweb/page/assets/kit.d.ts"] }
// where <tools-common dir> is `go list -m -f '{{.Dir}}' github.com/schuettc/tools-common`.

// ---- theme.js

/** system follows prefers-color-scheme; light and dark are remembered overrides. */
export type ThemeMode = 'system' | 'light' | 'dark';

export interface Theme {
  get(): ThemeMode;
  set(mode: ThemeMode): void;
  /** system → light → dark → system; returns the new mode. */
  cycle(): ThemeMode;
}

/** Apply the remembered mode (localStorage["<tool>.theme"]); a control, when given, cycles on click. */
export function initTheme(tool: string, control?: HTMLElement | null): Theme;

// ---- keys.js

/** Anything the family defaults can drive; list() returns one. */
export interface ListNav {
  move(d: number): void;
  open(): void;
  toggle(): void;
  toggleRange(): void;
}

export interface KeyBinding {
  /** "j", "1", "?", "↵", "↑", "↓", "Esc", modifiers "⌃⌥⇧⌘" as prefixes, sequences space-separated ("g a"). */
  keys: string;
  label: string;
  /** Return false to leave the event unhandled (not preventDefault-ed). */
  run(e: KeyboardEvent): void | boolean;
  /** Overlay group; defaults to "page". The family's own are "family". */
  group?: string;
  /** Fire even while a text field has focus (e.g. "⌘↵"). Single chords only. */
  inField?: boolean;
}

export interface Keys {
  /** Throws on a clash with a default or an earlier key. Returns an unregister function. */
  register(b: KeyBinding): () => void;
  /** Toggle, or force open/closed, the ? overlay. */
  showHelp(open?: boolean): void;
  bindings(): Array<{ keys: string; label: string; group: string }>;
  destroy(): void;
}

/** Family defaults: j/↓ k/↑ move, o/↵ open, x select, ⇧x range (when list is given); ? keys; Esc closes or leaves a field. */
export function createKeys(opts?: { list?: ListNav; target?: EventTarget; now?: () => number }): Keys;

// ---- api.js

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string);
}

export interface Api {
  get<T = unknown>(path: string, query?: Record<string, string>): Promise<T>;
  post<T = unknown>(path: string, body?: unknown): Promise<T>;
  put<T = unknown>(path: string, body?: unknown): Promise<T>;
  del<T = unknown>(path: string): Promise<T>;
}

/** JSON over the tool's /api/. 204 or an empty body resolves null; non-2xx throws ApiError. */
export function createApi(opts?: { base?: string; token?: string; fetch?: typeof fetch }): Api;

// ---- live.js

export type LiveStatus = 'live' | 'polling' | 'down';

export interface LiveEvent<T = unknown> {
  type: string;
  data: T;
  cursor: string;
}

export interface LiveOptions {
  /** SSE endpoint; every message is the default event, id = cursor, data = JSON {type, data}. */
  events: string;
  /** Poll endpoint: GET ?since=<cursor> → {cursor, events: [{type, data}]}. */
  poll: string;
  onEvent(e: LiveEvent): void;
  onStatus?(s: LiveStatus): void;
  cursor?: string;
  /** Poll interval in ms while the stream is down; default 2000. */
  interval?: number;
  EventSource?: typeof EventSource;
  fetch?: typeof fetch;
  setTimeout?: (fn: () => void, ms: number) => unknown;
  clearTimeout?: (id: unknown) => void;
}

export function live(opts: LiveOptions): { stop(): void; cursor(): string };

// ---- dom.js

type Child = Node | string | number | null | undefined | false;

/** Create an element: class, on<event>, dataset, boolean attributes; string children are text. */
export function h(tag: string, attrs?: Record<string, unknown> | null, ...children: Array<Child | Child[]>): HTMLElement;

export interface Section {
  id: string;
  label: string;
  /** Any string: "46", "37/150". */
  count?: string | number;
}

export interface Primary {
  label: string;
  run(): void;
}

export interface BarHandle {
  el: HTMLElement;
  themeControl: HTMLElement;
  setSection(id: string): void;
  setCount(id: string, count?: string | number | null): void;
  setStatus(text: string): void;
  setLive(s: LiveStatus): void;
  /** The one filled button; null hides it. */
  setPrimary(p: Primary | null): void;
}

export function bar(o: {
  brand: { name: string; mark?: Node };
  sections?: Section[];
  active?: string;
  onSection?(id: string): void;
  status?: string;
  primary?: Primary | null;
  theme?: HTMLElement;
}): BarHandle;

export interface Chip {
  id: string;
  label: string;
  count?: string | number;
  on?: boolean;
}

export interface Row {
  /** The mono kicker line, and (with title) the row's identity across setItems. */
  key?: string;
  title: string;
  meta?: string;
  sub?: string;
  selectable?: boolean;
}

export interface ListHandle<T> extends ListNav {
  el: HTMLElement;
  /** Keeps the open row, cursor and selection by row key + title. */
  setItems(items: T[]): void;
  setChips(group: 'view' | 'filter', chips: Chip[]): void;
  move(d: number): void;
  open(i?: number): void;
  toggle(i?: number): void;
  toggleRange(i?: number): void;
  selected(): T[];
  /** The cursor row's index, -1 when empty. */
  current(): number;
}

export function list<T>(o: {
  label: string;
  views?: Chip[];
  filters?: Chip[];
  onChip?(group: 'view' | 'filter', id: string): void;
  row(item: T): Row;
  onOpen?(item: T, i: number): void;
  onSelect?(selected: T[]): void;
  /** Open the row the cursor moves to (the reading column follows j/k). */
  openOnMove?: boolean;
  foot?: Node;
}): ListHandle<T>;

export function facts(pairs: Array<[string, string | Node]>): HTMLElement;

export interface Button {
  label: string;
  run?(): void;
  /** The one primary in a card. */
  fill?: boolean;
  /** A destructive verb: red text. */
  danger?: boolean;
  disabled?: boolean;
}

export function buttons(bs: Button[]): HTMLElement;

export function card(o: { edge?: 'signal' | 'agent' | 'wait'; head?: string; body?: Node | string; actions?: Button[] }): HTMLElement;

/** Line-numbered source: two <pre>s, scrolls sideways; highlight returns the source's content. */
export function codeBlock(src: string, o?: { start?: number; highlight?(src: string): Node }): HTMLElement;

export function fold(label: string, content: Node | string, o?: { open?: boolean }): HTMLDetailsElement;

/** ↵ or blur commits a changed value; Esc reverts and leaves. Page keys don't fire while it has focus. */
export function noteField(o: { value?: string; placeholder?: string; onCommit(v: string): void }): HTMLInputElement;
