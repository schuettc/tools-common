# Changelog

Root module releases are tagged `vX.Y.Z`; the separate `sqlitedb` and
`mermaidkit` modules are tagged `sqlitedb/vX.Y.Z` and `mermaidkit/vX.Y.Z`. Releases before this file are described by their
tags and PRs.

## 0.38.0

### Added
- `setupbundle.Source` and `BundleCommand` provide the shared build-only `setup-bundle --version X.Y.Z --out PATH` command. Producers supply canonical declaration bytes and a scoped filesystem; nil assets mean an intentionally empty bundle filesystem, not a fallback to local files.
- `skills.UserTargets()` returns portable default user skill destinations from the same harness table the direct installer uses, without reading HOME or environment overrides.

## 0.37.0

### Added
- `skills.Owns(name, body)` exposes the existing skill installer's ownership check so declarative setup can recognize a tool-installed skill without copying its frontmatter parser.

## 0.36.0

### Added
- `setupbundle`: a tool release's setup bundle, `<tool>_setup.tar.gz`, published beside its binaries. `Build(tool, version, manifest, files)` writes `setup.json` (`format: 1`, tool, version), `setup.toml` and each regular file of `files` under `files/`, with the same bytes for the same input; `Read` returns a `Bundle` (`Tool`, `Version`, `Manifest`, `Files`) and refuses another `setup.json` format, a symlink or other non-regular member, and any path outside `files/`. The root module remains stdlib-only.

## 0.35.0

### Changed
- The page kit's card header (`.kit-card-head`) is one line at a fixed 18 px, with or without a state pill: a `.kit-pill` in it is sized to the line, so moving between cards never shifts what is under the header. Nothing wraps; the key (`.kit-card-key`, pushed right) gives way first, then the crumb (`.kit-card-crumb`), each with an ellipsis. `card()` makes a string head its crumb. Headers that were about 13 px grow to 18 px.

## 0.34.0

### Added
- `sessiontheme`: shared window hues and persistent session shade slots, deterministic dark terminal palettes, pi 1.4 theme JSON rendering, and the typed resolved-theme query/header contract. The root module remains stdlib-only.

## 0.33.1

### Fixed
- A channel's wait loop logs each delivery the tool's `Wake` refuses (`<tool> channel: refused delivery: ` with its size and field names, never their values) instead of dropping it silently. diazo's channel dropped its overdue-wait wakes this way and left no trace.

## mermaidkit 0.2.0

### Changed
- The diagram check moved to its own package, `github.com/subaud-tools/tools-common/mermaidkit/check`: `check.NewParser(check.Bundle{...})` and `(*Parser).Check(text)`, otherwise as in 0.1.0. A tool that checks diagrams changes its import path (diazo); one that renders keeps `mermaidkit.Render` as it was (galley). `mermaidkit.Bundle` and `check.Bundle` are one type, so a tool that does both builds it once.
- Why: in one package, a tool that only renders carried mermaid's whole JavaScript parser too, and one that only checks carried the markdown renderer. Now each links only what it uses. A program that only calls `Render` builds 5.1 MB stripped on darwin/arm64, down from 12.2 MB with 0.1.0 (galley's binary had grown by 6.7 MB, 32%, when it took up 0.1.0), and one that only checks drops goldmark.

## 0.33.0

### Added
- `wiregen`: writes a page tool's `wire.d.ts` from its Go view structs and checks the committed file, the generator galley, diazo and casebook each carried in their `wire_test.go`. `Generate(roots, Options)` and `CheckCommitted(path, roots, Options, update)` (the drift test: an error naming the first differing line, or with `update` the file rewritten); `Options` holds the tool's `Header`, the `Regenerate` command its errors name, and `AllowUnknown`. It is the union of the three: a named string type with `Values()` is a union (wrapped one value per line past 80 columns, as prettier writes it), untagged embedded structs are promoted (pointer or not), `omitzero` is optional, a `[]byte` (base64) and a type with `MarshalText` are `string`, references are followed across packages, maps are `Record<K, V>` unless the file declares a type named `Record` (then `{ [key: K]: V }`), and two Go types with one TypeScript name, or a field it can only call `unknown` (an interface, a `json.RawMessage`, a type with its own `MarshalJSON`, named in the error), are refused unless `AllowUnknown`.
- `mermaidkit`, a separate module (`github.com/subaud-tools/tools-common/mermaidkit`, tagged `mermaidkit/vX.Y.Z`, because it needs goja and goldmark): diazo's diagram check and galley's themed page. `NewParser(Bundle)` (refusing an empty `Global`) and `Check(text)` parse a diagram with mermaid's own parser in goja and returns mermaid's reason (its first two lines). `Render(md, Theme, Bundle, docDir)` turns markdown into one self-contained HTML page (sidebar, local images embedded, the bundle inlined only when a diagram needs it) with `Theme`, `Palette`, `Font`, `BaseTheme()`, `ReadTheme(name, dir)` and `ImageTypes`. `Bundle{JS, Global}` is the tool's own built bundle and its esbuild `--global-name`; `Render` too refuses an empty `Global`. `just verify-extra` gates it as it does sqlitedb.
- `localweb/page/build/mermaid-entry.ts`: the mermaid bundle's entry a tool's build copies.
- `localweb/page/build/eslint.base.mjs`: the family's eslint rules (defect classes only), as `base({ root, ignores, testFiles, maxTestFiles, ambientGlobals })` for a tool's `eslint.config.mjs`: `root` is the tool's `import.meta.dirname`; `testFiles` (default `['*.test.ts']`, `[]` for none) are linted in a default project, `maxTestFiles` raises that project's cap (an error with `testFiles: []`); `ambientGlobals` (default on) turns `no-undef` off for `.ts`.
- `localweb/page/build/probe.mjs`: a browser probe's scaffolding, `check`, `counts`, `until` (4000 ms by default), `sleep`, `onCleanup` and `cleanup` (in the order added, so the browser closes before the serve fixture stops), `launchOrSkip({ required })` (null with no chromium, a failure when `required`, by default `KIT_BROWSER=required`), `run(main, { after })` (cleanup, then `after`, whose checks count, then the count and exit 1 on a failed check) and the geometry helpers `drawn`, `inside`, `sideBySide`. `just verify-slow` tests both in a scratch tool.

## 0.32.1

### Fixed
- `<tool> report` gives up after 5 seconds (`pageserve.ReportTimeout`). It ran on the client's 90-second long-poll timeout, so a serve that took the connection and never answered held the Claude Code plugin's hook that long.

### Added
- `Commands.Open`: opens the page for `<tool> serve` instead of the system opener, so a tool's own switch to keep the browser closed covers it.

## 0.32.0

### Added
- `update` prints the tool site's notice after its own output: it GETs `<site>/notice.txt` (ungated, 1 second timeout) and prints the trimmed text, cut to 2 KB, to stderr. No notice, an error or an empty body prints nothing and never fails the update. `App.PrintNotice` does the same for a tool that replaces `update`.
- `pageserve` does what casebook's own shell did, each as a hook or field a tool sets (a tool that sets none behaves as before):
  - `StatusError.ErrCode`: the JSON body's `code` (`"unknown_session"`), so two answers with one status can be told apart.
  - `Spec.PresenceBody(Identity)`: the body the channel posts to `/api/agent/presence`; `Identity.PID` is the harness's pid (the channel's parent); `Channel.Announce(ctx, client)` posts it, for a tool call that re-registers.
  - `Spec.OnServe(ctx, ch, entry)`: runs in the wait loop when the serve it reaches is a new process (a tool tells the agent what the restart interrupted), and the loop registers presence with it.
  - `Options.Stream`: answers `GET /api/events` instead of the `Events` log, for a page that streams from the tool's own event bus.
  - `Options.Busy`: while it answers true, serve stays up past `Idle` (for a page whose open tab should keep its serve alive).
  - `Options.Entry(*Entry)`: edits the entry before it is written (the server's own `StartedAt`); `Entry.Reopened` (`reopened`, omitted when false).
  - `Client.Passive()`: a copy of a client that never starts serve.

### Fixed
- `<tool> <command> -h` and the man page name the command when its `Synopsis` gives only the arguments: `Usage: diazo skills install [--agent NAME]…`, not `Usage: diazo install [--agent NAME]…`. A `Synopsis` that already starts with the command's name prints as before. Every pageserve command, and `login`, had lost its name.

### Changed
- An ungated binary whose site refuses `/dl/<tool>/latest` with 403 or 404 (a gated `/dl` refusing an unsigned request) says the site refused it with that status and, in case the site now needs a signed-in machine, names https://subaud.tools/account/ and the site's install command (`curl -fsSL https://<site>/install.sh | sh`), and `update` exits 1. A gated binary's errors are unchanged.
- A page tool's channel no longer restarts a page server that stopped after being idle; the next tool call starts it. The channel's wait loop, its reports, `<tool> watch` and `<tool> report` never start serve, so serve's idle exit (8 hours in cull and sift) now happens, and while no serve runs the loop logs nothing.

## 0.31.1

### Fixed
- `pageserve.Run` calls `OnStart` before it writes the entry, so a channel that finds a running serve finds the tool's own start-up done. Before, a channel could call the tool's API in the moment between the entry and `OnStart` (cull's page URL was still unset).

## 0.31.0

### Added
- `pageserve`: the page-tool server shell diazo, sift, cull and casebook
  each carried a copy of, with galley's registry, so each tool keeps only
  its own routes and channel tools. Built on `channelmcp`, `harness`,
  `localweb` and `skills`; the tool's name parameterizes every message.
  - `Entry`: the superset of a one-page tool's advert (`url`, `base`,
    `token`, `pid`, `version`, `stale`, `started_at`) and galley's registry
    entry (`v`, `url`, `room`, `page`, `pid`, `owner`), under the same JSON
    names, with the fields one of them never had omitted when empty, so
    each file keeps its shape and an old binary still reads a new one's.
  - `Live{Tool, Dir}` (`LiveFor(tool)` is `StateDir(tool)/live`): `Write`,
    `Remove`, `Claim` (an flock on the sibling `<room>.lock`: exactly one
    channel owns an unowned or orphaned entry), `List`/`Inspect` (reap dead
    pids, drop a second entry for one URL, report every file declined as a
    `Problem`; an entry from a newer version is reported and left alone),
    session presence (`AnnounceSession`, `WithdrawSession`, `SessionLive`),
    `LogDir`, `Token`, and per-page port hints (`LastPort`,
    `SaveLastPort`). Moved from galley's `internal/registry`.
  - A one-page tool's entry, `live/serve.json`: `Running`, `Advertise`,
    `Withdraw`, `ErrNotRunning`.
  - `Events`: the event log the page kit's `live()` reads, `Emit`,
    `Cursor`, and the `Poll` and `Stream` handlers, with `Tabs` check-ins.
  - `Run(ctx, Options)`: the serve process: one per machine (under
    `serve.lock`), its entry while it runs, `/api/events`, `/api/poll`,
    `/api/stop`, the idle exit, and an `OnStart` hook for the tool's own
    steps. `Process{Live, Args, Dir}.Start`/`Ensure` start it in the
    background; `Stop` stops it.
  - `Client` (`Do`, `Report`, `Fresh`, `StatusError`, `ErrNoServe`,
    `Query`) and `Channel` (`New`, `ForSession`, `Run`, `Watch`, `Root`)
    from a tool's `Spec`: its tools, its project root, how it reads a wait's
    answer (`Wake`), and whether its serve takes presence.
  - `Commands`: the `serve`, `channel`, `watch` and `report` commands from
    a tool's `Spec`, `Process` and hooks; `SkillsCommand(tool, skill)` is
    `<tool> skills install [--agent NAME]…`.

## 0.30.0

### Added
- `Config.Product`: the product a gated tool's `update` asks `/links` for,
  when one product covers several tools (tackle's six). Left empty it is
  `Name`, as before.

## 0.29.0

### Fixed
- `login`: a sign-in whose code expired (its time passed, or the service
  says so) is replaced in the same run, printing a new code to type, instead
  of ending with an error and nothing to act on. A denied sign-in still stops
  and says to run login again.

## 0.28.0

### Added
- `Config.Gated`: a tool whose `/dl` is closed to anyone. Its `update`
  fetches `latest`, the tarball and the `.sha256` through signed links from
  the access service; with no token, or one the service refuses, it says to
  run `<tool> login`, and for a tool the account doesn't own it names the
  account page. Ungated tools are unchanged and never ask for links.
- `auth.Link(ctx, product, path)`: a five-minute signed download URL from
  `POST /links`, asked with the machine's token. No token is
  `auth.ErrNotSignedIn` (nothing is asked), a refused one
  `auth.ErrSignInRefused`, a product not owned `*auth.NotOwnedError` with the
  account page.
- `login --wait`: signs in in one run, waiting while the machine is allowed
  (what `login` did in 0.26.0).

### Changed
- `login` signs in over two short runs, so nothing waits on a person. The
  first prints the URL and code, saves the sign-in at
  `ConfigDir("subaud")/pending` (mode 600) and exits 0; the next asks once
  and, once the machine is allowed, saves the token. A run while it is still
  pending says so and exits 0; a denied or expired code is dropped, and the
  next run starts over. `auth.Client.Login` is now the two-run sign-in;
  `auth.Client.LoginWait` is the one-run sign-in.

## 0.27.0

### Added
- The Claude Code mod reports held: when a line with a `report` token is
  submitted and `prompt.submit` has not resolved a second later (the agent is
  mid-turn, and the prompt waits for the turn to complete), it runs
  `<bin> report held <token>` in the session, on the same chain as every
  other report, so held always reaches the server before that line's landed.
  A line with no token runs nothing. A tool's plugin must be regenerated to
  get it.
- `channelmcp.ReportCommand` accepts `<tool> report held <token>` and
  delivers `ReportHeld` with the token.

## 0.26.0

### Added
- `login` and `logout`, built-in commands every tool inherits. `login` signs
  the machine in to the subaud.tools account by device code: it prints a URL
  and a code to type there, polls until the machine is allowed, denied or the
  code expires, and stores the machine's token at `ConfigDir("subaud")/token`
  (mode 600), shared by every tool. `logout` removes the machine from the
  account and deletes the token; it keeps the file on a network error.
- `auth`: `auth.Token()` returns `$SUBAUD_TOKEN`, else the stored token, for
  downloaders. The API base is fixed in code, so the environment can't send
  the token elsewhere.

## 0.25.0

### Added
- `localweb/page`: `submitOnEnter(field, submit)` in the kit's new `enter.js`
  (re-exported by `kit.js`, typed in `kit.d.ts`): a plain Enter calls
  `submit` and prevents its new line; Shift-Enter, a held (repeating) Enter
  and an IME's Enter (`isComposing` or keyCode 229) keep the key's usual
  meaning. It imports nothing, so a page can copy it alone. galley's contract,
  moved into the kit so every family page that takes a message uses one rule.

### Changed
- `noteField` commits on Enter through `submitOnEnter`: a held Enter,
  Shift-Enter and keyCode 229 no longer commit.

## 0.24.0

### Added
- `skills`: a tool's embedded agent skill, installed and kept current.
  `Install(name, body, agents, out, errw)` writes `body` as
  `<skills dir>/<name>/SKILL.md` for each agent named (claude-code, codex,
  pi), or for each harness whose home exists (`CLAUDE_CONFIG_DIR`,
  `CODEX_HOME`, `PI_CODING_AGENT_DIR` when absolute, else `~/.claude`,
  `~/.codex`, `~/.pi/agent`): exit 2 with no harness, 1 when any harness
  failed. Moved from tackle's `skills/install.go` (diazo's is a copy), with
  its rules: the skills directory may be a symlink, nothing beneath it may
  be, an existing SKILL.md is replaced only when its `name:` is the tool's,
  and each write is a fresh file renamed into place with the old mode.
- `skills.Refresh(name, body)`: for each harness whose home exists,
  rewrites the installed copy of the tool's own skill when it differs from
  `body`, and returns the paths it rewrote. It never creates a missing skill
  and skips a foreign one (another `name:`, or a symlink beneath the skills
  directory); a matching copy is not touched. A tool calls it when its
  channel starts.

## 0.23.0

### Added
- `msgstate`: the one lifecycle of a page message (sent, with_agent, working,
  answered, declined, failed, no_reply), each state's label, `Final(s)` and
  `Allowed(from, to)` for the moves the lifecycle spec draws. `State` has a
  `Values()` method, so a tool's wire generator emits it as a union.
  `Acknowledge(from, to)` is for the agent's replies: it lets a message still
  `sent` (its landed report not in yet) be settled or marked working.
- Page kit `messagePill(state, line)`: a message's state pill
  (`.kit-pill[data-state]`, one variant per state in the kit's colours) with
  its label and the line under it. The labels come from `msgstate.js`
  (`MESSAGE_STATES`), generated from `msgstate`; a test fails when they drift.
  The demo page shows every state.
- `channelmcp.Handler.OnReport(event, token)`: the server accepts the client
  notification `notifications/channels/report` with
  `{"event": "held"|"landed"|"turn_ended", "report": "<token>"}` (held and
  landed need the token) and hands it to OnReport, in order. A malformed
  report is ignored and logged; with no OnReport, reports are ignored. The
  token rides on an event as the meta key `report` (`channelmcp.MetaReport`),
  and each `<tool> watch` line also prints it as a top-level `report` field.
- `channelmcp.ReportCommand(deliver)`: `<tool> report landed <token>` and
  `<tool> report turn-ended`, mounted with `app.Register`. It reads the
  session as watch does, hands the report to the tool's own `deliver`
  callback, prints nothing, and does nothing in a bridge child.
- The Claude Code mod reports back: when a line's prompt starts a turn
  (`prompt.submit` resolves without a drop) it runs `<bin> report landed
  <token>` with the line's `report` token, and on a `turn.complete` after one
  of its lines landed it runs `<bin> report turn-ended`, both in the session,
  one report at a time so they reach the server in order. A failed report goes
  to the debug log and is not retried. A tool's plugin must be regenerated, and
  its binary must mount `ReportCommand` in the same release.

## 0.22.0

### Fixed
- localweb: the sign-in cookie is named per port (`<tool>_token_<port>`), so
  two serves of one tool on one machine (a branch build beside the live one)
  no longer sign each other's page out. A browser's old `<tool>_token` cookie
  is ignored.

## 0.20.0

### Added
- Page kit `bar()`: an optional `brand.href`. With it the brand is a link
  there (a tool's home page), drawn in the bar's own colour and underlined
  only on hover or focus; without it the brand is plain text, as before.

## 0.19.0

### Added
- `localweb.CanShare()` (macOS only) and `localweb.PutFile(path)`, which puts
  the file itself on the macOS clipboard so a paste attaches it; the path is
  an argument to the script, never part of its text. Moved from galley.
- Page kit `share.js`: `shareMenu(button, items, onPick)` builds a Share ▾
  button's menu from `{label, act}` items, right-aligned under the button,
  closing on a pick, an outside click, Esc or a resize; with the labels
  (`SHARE_*`), `sharedLine(path)` and `SHARE_REPLY_MS`. It imports nothing
  else from the kit, so a page can bundle it alone. Moved from galley.
- Page kit `share.css`: styles the menu through `--share-*` properties
  (ground, ink, line, hover, lift, font) that default to the kit's tokens;
  `kit.css` imports it.

## 0.18.0

### Added
- `channelmcp.StartWatch(w, tool)`: the one start every tool's `<tool> watch`
  makes. In a bridge child (the harness Child rule: `AGENT_SESSION_CHILD=1`
  with a non-empty `AGENT_SESSION_ID`) it prints `{"stop": reason}` and
  reports don't run (the caller exits 0); otherwise it returns the `Lines`
  writer for the tool.
- `channelmcp.NewLinesFor(w, tool)`: each line also carries `text`, the whole
  prompt (`<tool> event`, a ` key=value` per meta pair, a newline, the
  content), exactly what the 0.17.0 mod builds. `content` and `meta` stay, so
  plugins installed from 0.17.0 keep working against a new binary. The line
  format is additive. `NewLines` (no tool name) writes no `text`.

### Changed
- The Claude Code mod is a thin shim, since kempt updates a tool's binary but
  not its installed plugin: it submits each line's `text` word for word, and
  on a `stop` line logs the reason and never restarts `watch`. It no longer
  formats events or reads the environment; the binary decides both. Each
  tool regenerates its plugin and calls `StartWatch` from its `watch`.

## 0.17.0

### Fixed
- A tool's Claude Code mod stays quiet inside pi's claude-bridge, where the
  bridge stamps `AGENT_SESSION_CHILD=1` with an `AGENT_SESSION_ID` on the
  Claude Code children it spawns. The event belongs to the hosting pi session,
  which its own channel carrier delivers, so the mod no longer spawns
  `<tool> watch` there and submits a second turn. Each tool regenerates its
  plugin to pick this up.

## 0.15.0

### Added
- `channelmcp.Pusher`, the interface a tool's wait loop sends events to;
  `*Server` satisfies it. `channelmcp.Lines` is a second Pusher that writes
  each event as one JSON line, `{"content":...,"meta":{...}}`, for
  `<tool> watch`, refusing the same meta keys `Server.Notify` refuses.
- `harness.Capture.IsClaude`: the resolved session is Claude Code's own
  (`CLAUDE_CODE_SESSION_ID` won the rule, not a bridge child).
- `claudeplugin`: renders a tool's Claude Code plugin from a `Spec`:
  `plugin.json` (no `version`, so Claude Code versions it by commit),
  `.mcp.json` running `<bin> channel`, and a mod (`hooks/hooks.json`,
  `hooks/register.js`) that runs `<bin> watch` for the session, starts a turn
  with each event (a meta value with whitespace, a quote or `=` is quoted, so
  it stays one value on the header line), resubmits a dropped one from `turn.complete`, and restarts
  `watch` when it ends. `AssertCurrent` fails a tool's test when its
  committed plugin differs and names the regenerate command.

## 0.13.1

### Fixed
- `live`: a hidden tab closes its event stream and polls nothing; shown
  again it reopens from its cursor. Browsers allow about six connections to
  one host, so a stream per background tab froze every tab of a tool.

## 0.13.0

### Added
- `localweb.Config.Token`: a tool can hand in the token to use (64 hex
  characters) instead of a new one per start, so its open tabs keep working
  across a restart. Empty keeps today's behaviour.

## 0.12.0 (sqlitedb 0.2.0)

### Changed
- Module path is now `github.com/subaud-tools/tools-common` (and
  `github.com/subaud-tools/tools-common/sqlitedb`); CI pins
  `subaud-tools/tools-actions` `go-ci@v0.11.0`.
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
