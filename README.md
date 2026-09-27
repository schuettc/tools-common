# tools-common

The shared CLI foundation for the [.tools](https://subaud.tools) family
(kempt, muster, galley, tackle). It factors the three things every family
binary hand-rolls: **version reporting**, **self-update** from the family
`/dl` download standard, and **command dispatch**.

One module, **stdlib-only** (no external dependencies): the root package `tools`
(CLI foundation and small helpers) plus focused subpackages, each importable on
its own. Subpackages never import each other; `localweb` imports only the root.

| Import | What |
|---|---|
| `tools-common` (package `tools`) | CLI foundation (`App`, `Command`, help/man/`commands --json`, `ExitError`, version, self-update); `WriteFileAtomic`, `PIDAlive`, `ConfigDir`/`StateDir`/`CacheDir`/`DataDir`/`EnsureDir` |
| `tools-common/channelmcp` | claude/channel stdio server (newline-delimited JSON-RPC 2.0) |
| `tools-common/harness` | session identity: the family resolution rule over `CLAUDE_CODE_SESSION_ID`, `AGENT_SESSION_ID`, `AGENT_SESSION_CHILD` |
| `tools-common/localweb` | loopback local-page server: per-launch token, rebinding/CSRF guards, remembered port, `OpenBrowser` |

**Separate module:** `github.com/schuettc/tools-common/sqlitedb` has its own
`go.mod` and `sqlitedb/vX.Y.Z` tags, because it needs the SQLite driver
(`modernc.org/sqlite`). Importing the root or any package above never pulls it
in. See [SQLite databases](#sqlite-databases-sqlitedb).

## Usage

```go
package main

import (
	"os"

	tools "github.com/schuettc/tools-common"
)

// Stamped by CI via `-ldflags -X`.
var (
	version = "dev"
	commit  = ""
	date    = ""
)

func main() {
	app := tools.New(tools.Config{
		Name:    "kempt",
		Domain:  "kempt.tools",
		Version: tools.Version{Number: version, Commit: commit, Date: date},
	})

	app.Register(tools.Command{
		Name:    "sync",
		Summary: "converge to the desired state",
		Run:     runSync,
	})

	os.Exit(app.Dispatch(os.Args[1:], os.Stdout, os.Stderr))
}
```

`New` auto-registers three built-in commands:

- `version` — prints `"<Name> <Version>"` (e.g. `kempt 0.1.0 (abc, 2026-08-30)`).
- `help` — prints usage.
- `update` — self-updates via the `/dl` contract below.

A tool can override any built-in by registering a command with the same `Name`
(e.g. wrap `update` with domain-specific convergence).

### Exit codes (family-wide)

- `0` ok
- `1` runtime error
- `2` usage error (return `tools.UsageError{Msg: "..."}` from a command's `Run`)

`--version`/`-v` alias the `version` command; `--help`/`-h` alias `help`.

## Self-update — the `/dl` family download contract

`<tool> update` means the same thing everywhere. `SelfUpdate`:

1. GET `https://<domain>/dl/<tool>/latest` → bare semver (leading `v` tolerated).
2. If it equals the current version → no-op.
3. Otherwise download
   `https://<domain>/dl/<tool>/<version>/<tool>_<os>_<arch>.tar.gz` and its
   `.sha256` sidecar.
4. **Fail-closed** sha256 verify against the sidecar.
5. Extract the `<tool>` binary from the tar.gz.
6. Atomically replace the running binary (stage `.<tool>.new.<pid>` →
   `os.Rename`), so a running process keeps its inode.

## Version format

`Version.String()` renders `"<Number> (<Commit>, <Date>)"`, eliding gracefully:

| Number | Commit | Date | Output |
|---|---|---|---|
| `0.1.0` | `abc` | `2026-08-30` | `0.1.0 (abc, 2026-08-30)` |
| `0.1.0` | `abc` | — | `0.1.0 (abc)` |
| `0.1.0` | — | — | `0.1.0` |
| — | — | — | `dev` |

## Session identity rule (`harness`)

1. `AGENT_SESSION_CHILD=1` with a non-empty `AGENT_SESSION_ID` → the agent id
   (a parent declared this process part of its session; today pi-claude-bridge).
2. else `CLAUDE_CODE_SESSION_ID` (or a hook payload's `session_id`).
3. else `AGENT_SESSION_ID`.
4. else no session.

Only a parent that runs a process as part of its own session may set
`AGENT_SESSION_CHILD`. Tools never read these variables directly.

## SQLite databases (`sqlitedb`)

```
go get github.com/schuettc/tools-common/sqlitedb@sqlitedb/v0.1.0
```

One way to open a tool's local database: modernc.org/sqlite, WAL, a 5 s busy
timeout, one connection, foreign keys on, the database and its `-wal`/`-shm`
files `0600` (parent directory created `0700`), and schema changes as an
append-only list of steps tracked in `PRAGMA user_version`.

```go
db, err := sqlitedb.Open(ctx, filepath.Join(tools.StateDir("casebook"), "casebook.db"), sqlitedb.Options{
	Migrations: []sqlitedb.Step{
		sqlitedb.SQL(`CREATE TABLE sessions (id TEXT PRIMARY KEY, ...);`), // 0 -> 1
		backfillLabels, // 1 -> 2: func(ctx context.Context, tx *sql.Tx) error
	},
})
err = db.Tx(ctx, func(tx *sql.Tx) error { ... }) // commit on nil; roll back on error or panic
```

- Each step runs in its own transaction with the `user_version` bump inside
  it: a failed step leaves the database at the previous version.
- A database newer than the list is refused with `*sqlitedb.NewerError`
  (both versions in the message).
- Steps are append-only; fix a mistake with a new step.
- `NoForeignKeys` leaves enforcement off, for a schema that was never written
  for it (muster).
- **Adopting an existing unversioned database** (muster's case: tables but
  `user_version` 0): make step 1 idempotent (the old schema as `CREATE TABLE
  IF NOT EXISTS`, `ALTER`s that tolerate `duplicate column name`, re-runnable
  backfills) and set `AdoptUnversioned: true`. Step 1 then runs once over the
  existing tables and the database is at version 1. Without the flag, such a
  database is refused with `ErrUnversioned` rather than meet a first step
  written for an empty file.
- The package decides nothing about schemas: no table helpers, no ORM.

**One driver version for the family:** sqlitedb pins `modernc.org/sqlite`
v1.59.0. muster and galley are on v1.53.0 today; adopting the module moves
them to v1.59.0.

## License

[Apache-2.0](LICENSE) © 2026 Court Schuett. tools-common is permissively
licensed so every family binary — MIT or source-available — can import it.
