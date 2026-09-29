# ---- .tools family standard: identical in every family repo ----------------
# `just verify` is exactly what CI runs: this tool's `prepare` (files the build
# needs, e.g. an embedded asset), the family gate (tools-actions go-ci, at the
# version .github/workflows/ci.yml pins), then this tool's `verify-extra`.
# The pre-push hook (lefthook.yml) runs it too, so local and CI never differ.
set shell := ["bash", "-euo", "pipefail", "-c"]

default: verify

verify: prepare gate verify-extra

# Everything: verify plus this tool's slow checks (browser, containers), which
# CI runs as their own required jobs.
verify-all: verify verify-slow

# The family Go gate: gofmt, vet, golangci-lint (family config), race tests,
# cross-build. Fetched once per tools-actions version into ~/.cache.
gate:
    #!/usr/bin/env bash
    set -euo pipefail
    v="$(grep -oE 'go-ci@v[0-9]+\.[0-9]+\.[0-9]+' .github/workflows/ci.yml | head -1 | cut -d@ -f2)"
    f="${XDG_CACHE_HOME:-$HOME/.cache}/tools-actions/$v/go-ci/local.sh"
    [ -f "$f" ] || { mkdir -p "$(dirname "$f")"; curl -fsSL "https://raw.githubusercontent.com/schuettc/tools-actions/$v/go-ci/local.sh" -o "$f"; }
    bash "$f"

fmt:
    gofmt -w $(git ls-files '*.go')

# Install the lefthook hooks into this clone's own .git/hooks (once per
# clone). A global core.hooksPath (casebook's recorder) forwards to them;
# plain `lefthook install` refuses to run under one.
hooks:
    #!/usr/bin/env bash
    set -euo pipefail
    d="$(cd "$(git rev-parse --git-common-dir)" && pwd)/hooks"
    git config --local core.hooksPath "$d"
    trap 'git config --local --unset core.hooksPath' EXIT
    lefthook install --force >/dev/null
    echo "lefthook hooks installed in $d"

# ---- tools-common ----
# Files the gate needs that are not committed (built before the gate). tools-common has none.
prepare:

# Slow checks CI runs as their own job: the localweb/page kit's JS suites, run
# in headless Chrome (KIT_BROWSER=required fails if no browser is found). The
# root gate's `go test` skips them without a browser.
verify-slow:
    cd localweb/page && KIT_BROWSER=required go test ./...

# Checks beyond the root Go gate (CI runs this too): the root module must stay
# stdlib-only, and the separate sqlitedb module (own go.mod and sqlitedb/vX.Y.Z
# tags, so the gate's ./... never reaches it) gets the family gate's Go checks.
verify-extra:
    #!/usr/bin/env bash
    set -euo pipefail
    # A tool that imports tools-common must never pull in a third-party module.
    deps="$(go list -m all | tail -n +2)"
    if [ -n "$deps" ]; then echo "root module gained dependencies:"; echo "$deps"; exit 1; fi
    # sqlitedb is its own module: gofmt, vet, race tests, build.
    cd sqlitedb
    test -z "$(gofmt -l .)" || { gofmt -l .; echo "gofmt needed in sqlitedb; run just fmt"; exit 1; }
    go vet ./...
    go test -race ./...
    go build ./...
