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

## License

[Apache-2.0](LICENSE) © 2026 Court Schuett. tools-common is permissively
licensed so every family binary — MIT or source-available — can import it.
