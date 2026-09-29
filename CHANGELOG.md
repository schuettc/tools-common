# Changelog

Root module releases are tagged `vX.Y.Z`; the separate `sqlitedb` module is
tagged `sqlitedb/vX.Y.Z`. Releases before this file are described by their
tags and PRs.

## Unreleased

### Changed
- Adopted the .tools family CI standard: the standard `justfile` (family
  `justfile.head` verbatim plus `tools-common` slots), `lefthook.yml`, a
  standard `ci.yml` (tools-actions `go-ci@v0.10.1` gate plus `extra` and
  `browser` jobs that fold in the stdlib-only guard, the `sqlitedb` module
  checks, and the `localweb/page` browser tests), and `.github/dependabot.yml`.
- Cleared the family Go gate's lint findings (bodyclose, errcheck, gosec,
  staticcheck, unparam) with no change to the exported API or behaviour.

## v0.11.0 (2026-09-28)

### Added
- `localweb/page`: `list({search: {placeholder?, value?, onInput}})`, a search
  field in the list header (kit tokens: mono, 6px, hairline), and the handle's
  `setSearch(text)` (no `onInput`). `onInput` fires on every input, no
  debounce. `/` focuses it: a family default registered only when the list
  has search, so a page's own `/` clashes. In the field only Esc acts: it
  clears the text first, then leaves the field.

## v0.10.1 (2026-09-28)

### Fixed
- `localweb/page` review fixes for v0.10.0:
  - `sheet({returnFocus})`: if the opener left the page while the sheet was
    open, focus goes to `returnFocus`, else the main region (`main`,
    `[role=main]` or `.kit-read`), else body, instead of nowhere.
  - `list.setItems` paints once, even when it drops rows that left the list.
  - The `?` overlay closes on a click anywhere in it again.
  - `Selection.range` docs: if either end is not in `orderedIds`, it toggles
    the target.
- Tests: the destroyed-list test now fails if `destroy()` doesn't unsubscribe,
  and the harness's `eq` refuses DOM nodes (it compared them as `{}`, so the
  focus assertions were vacuous; they now compare identity, and pass).

## v0.10.0 (2026-09-28)

### Added
- `localweb/page`: `createSelection()`, a selection store several views bind
  to (`ids`, `has`, `toggle`, `range(anchorId, id, orderedIds)`, `all`,
  `deselect`, `clear`, `anchor`, `onChange`), and `list({selection})`, plus the
  handle's `selection` and `destroy()`. A list without one creates its own, so
  existing callers are unchanged.
- `sheet({title, body, actions, onClose})` → `{el, close()}`: a modal sheet
  with a backdrop, Esc and backdrop-click to close, focus containment and
  return, and page keys suspended while open; `sheetOpen()`.

### Changed
- The list's toggle, range and by-id logic now lives in the selection store.
  A range anchor is an id, so shift-range works across a re-render.
- The `?` overlay is built on `sheet` (it gains the backdrop and focus
  handling); `?` or Esc still closes it.

### Fixed
- The kit's browser tests no longer flake when headless Chrome runs the page
  without window focus (focus events are fired explicitly in the tests).

## v0.9.1 (2026-09-28)

### Fixed
- `localweb/page`: a 401 poll is `stale` even if a stream opened while the
  poll was in flight (it used to be dropped as out of date, leaving `live`).
- `setLive` picks its label without relying on a second fallback; `staleText`
  applies only to `stale`.
- `.kit-app` scrolls sideways below its minimum width instead of overflowing
  (reading column minimum 320px). Removed a dead branch in the Esc binding.

## v0.9.0 (2026-09-28)

### Added
- `localweb/page`, for casebook's page and cull's review page:
  - Stale session: `ApiError.isStale`, `createApi({onStale})` (called once),
    a `stale` status from `live()` on a 401 poll (it stops for good), and
    `bar({staleText})` for the tool's own wording.
  - `.kit-app` grid (bar; list | reading column | optional `.kit-rail`).
  - Selection by row id across pages: `selectedIds()`, `selectAll(ids)`,
    `deselect(ids)`, `clearSelection()`. Tested to 500 rendered rows.
  - `setStatus(text, {tone})` and `setLive(state, text)`.

### Changed
- Keys: `⌥` bindings match the physical key (macOS changes the character);
  while the `?` overlay is open only `Esc` and `?` act; a key another handler
  already took (`preventDefault`) is left alone.
- List selection is by identity, so a row that leaves the list stays selected
  by id until `deselect`/`clearSelection`; `onSelect` fires when the id set or
  the rendered selection changes.

## v0.8.2 (2026-09-28)

### Fixed
- `man` escapes double quotes and a leading `.` or `'` on a line. A quoted
  synopsis (muster's `send <target> "body"`) had its quotes eaten by `.B` and
  was split into separate arguments; a help line starting with `.` would have
  been read as a roff request.
- `help <cmd>` and `<cmd> -h` print the command's one-line summary between the
  usage line and the long help (it was only in the grouped list).

## v0.8.1 (2026-09-28)

### Fixed
- A command that declares `Subcommands` gets `-h` from tools.App only as
  `<cmd> -h`. Any other first word goes to `Run`, so a mistyped sub-verb
  (`galley ledger bogus -h`) is rejected by the command instead of exiting 0
  with the parent's help (v0.8.0 intercepted it).

## v0.8.0 (2026-09-28)

### Added
- `Command.Subcommands`: a command's own sub-verbs (galley `ledger
  sync|rebuild|stats`, muster `standing ...`). `<cmd> -h` shows the command's
  help, now with a `subcommands:` line; `<cmd> <sub> ... -h` is passed through
  to `Run` so the sub-verb prints its own flags. `commands --json` carries a
  `subcommands` array. Before this, a command with sub-verbs had to leave its
  Synopsis and Help empty, or `-h` never reached the sub-verb.
- Additive: commands without Subcommands behave as before.

## v0.7.0 (2026-09-27)

### Added
- `Config.About`: an optional overview. `help` prints it above the command
  list, and `man` uses it as the DESCRIPTION (blank-line paragraphs become
  `.PP` breaks). Bare invocation stays the short usage on stderr, exit 2.
  galley uses it for its push/pull and channel guidance.
- Additive: without `About`, `help` and `man` are unchanged.

## v0.6.0 (2026-09-27)

### Added
- `Command.Aliases`: extra words that dispatch to the same command, resolved
  by `Dispatch`, `help <alias>` and `<alias> -h`. `HelpFor` prints an
  `aliases:` line and `CommandsJSON` an `aliases` array (empty when none);
  grouped usage and `man` list only the canonical name.
- `Register` panics when a name or alias is already taken by a different
  command. Re-registering a `Name` (overriding a built-in) replaces its
  aliases.
- Additive: nothing in v0.5.0 changes for tools that set no aliases.

### Docs
- README lists all five built-in commands (`man` and `commands` were missing).

## v0.5.0 (2026-09-27)

### Added
- New package `localweb/page`: the family's galley-style local page kit, for
  docket's workbench and cull's label page. `page.With(toolFS)` serves the
  kit's embedded assets under `/_kit/` beside a tool's own page
  (`localweb.Config{Assets: page.With(webFS)}`). The assets are plain CSS and
  vanilla ES modules, with no build step and no node:
  - `kit.css`, the only place the `--kit-*` tokens are declared (one
    `light-dark()` block). Tools set only `--tool-signal-*` and
    `--tool-agent-*`, which default to tackle rust and wire.
  - `theme`, `keys`, `api`, `live` (SSE with a 2 s poll fallback) and `dom`
    (bar, list panel, facts, cards, buttons, code block, fold, note field),
    re-exported by `kit.js` and typed by `kit.d.ts`.
- Additive: nothing in v0.4.0 changes, and the root module stays stdlib-only.

### CI
- The kit's JS tests run in headless Chrome from `go test`. The `test` job
  sets `KIT_BROWSER=required`, so a missing browser fails instead of skipping.

## sqlitedb v0.1.0 (2026-09-27)

### Added
- New module `github.com/schuettc/tools-common/sqlitedb`: the family's SQLite
  conventions in one place, from casebook's working-state database, galley's
  ledger index and muster's store. `Open(ctx, path, Options)` with WAL, a 5 s
  busy timeout, one connection, foreign keys (on unless `NoForeignKeys`), files
  `0600` and a `0700` parent directory; `user_version` migrations as `Step`s
  (`func(ctx, *sql.Tx) error`, with `SQL` for plain statements), one
  transaction each; `*NewerError` for a newer database; `AdoptUnversioned` and
  `ErrUnversioned` for existing unversioned databases; `(*DB).Tx` and
  `(*DB).Version`. Pins `modernc.org/sqlite` v1.59.0.
- Several processes may open one new database at the same moment: the switch
  to WAL is retried while SQLite reports it busy, and migrations take the
  write lock at the start of each step and re-read the version, so each step
  runs exactly once.
- Paths are percent-encoded into the SQLite URI, so `?`, `#` and `%` in a
  path name that exact file.

### CI
- New `sqlitedb` job (gofmt, vet, `test -race`, build in the submodule) and a
  `stdlib-only` job that fails if the root module gains a dependency.
