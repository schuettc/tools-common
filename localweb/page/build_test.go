package page

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The files a tool's build copies from build/ are tested in a scratch page
// tool, testdata/buildtool: copied into its build/ as a tool's build copies
// them, its packages installed with npm ci, then its eslint config (the base,
// imported by a relative path) and its probe (on the scaffolding) are run.
// It needs node and the network, so it runs with the JS suites: only when
// KIT_BROWSER is set (just verify-slow, CI's browser job), and with
// KIT_BROWSER=required a missing npm fails.

func buildTool(t *testing.T) string {
	t.Helper()
	if os.Getenv("KIT_BROWSER") == "" {
		t.Skip("set KIT_BROWSER to test build/ in a scratch tool (needs npm and the network)")
	}
	if _, err := exec.LookPath("npm"); err != nil {
		if os.Getenv("KIT_BROWSER") == "required" {
			t.Fatal("KIT_BROWSER=required but no npm found")
		}
		t.Skip("no npm")
	}
	dir := filepath.Join(t.TempDir(), "tool")
	if err := os.CopyFS(dir, os.DirFS("testdata/buildtool")); err != nil {
		t.Fatal(err)
	}
	// The two files a tool without diagrams copies (mermaid-entry.ts is for
	// one that builds a mermaid bundle).
	if err := os.MkdirAll(filepath.Join(dir, "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"eslint.base.mjs", "probe.mjs"} {
		b, err := os.ReadFile(filepath.Join("build", f))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "build", f), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := runIn(t, dir, nil, "npm", "ci", "--no-audit", "--no-fund", "--ignore-scripts"); err != nil {
		t.Fatalf("npm ci: %v\n%s", err, out)
	}
	return dir
}

// runIn runs a command in dir with env added to the test's own, KIT_BROWSER
// removed so each run says what it wants.
func runIn(t *testing.T, dir string, env []string, name string, args ...string) (string, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "KIT_BROWSER=") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, env...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if e := (*exec.ExitError)(nil); errors.As(err, &e) {
		return e.ExitCode()
	}
	return -1
}

func TestBuildFilesInAScratchTool(t *testing.T) {
	dir := buildTool(t)

	t.Run("eslint passes with the base", func(t *testing.T) {
		if out, err := runIn(t, dir, nil, "npx", "eslint", ".", "--max-warnings", "0"); err != nil {
			t.Fatalf("eslint: %v\n%s", err, out)
		}
	})

	t.Run("eslint fails on a defect", func(t *testing.T) {
		bad := filepath.Join(dir, "bad.mjs")
		if err := os.WriteFile(bad, []byte("let x = 0;\nif ((x = 1)) console.log(x);\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.Remove(bad) }()
		out, err := runIn(t, dir, nil, "npx", "eslint", ".", "--max-warnings", "0")
		if exitCode(err) != 1 || !strings.Contains(out, "no-cond-assign") {
			t.Fatalf("eslint on a defect: %v, want exit 1 naming no-cond-assign\n%s", err, out)
		}
	})

	// What casebook and galley differ in: the test-file count, no default
	// project at all, and no ambient globals.
	t.Run("the base's options", func(t *testing.T) {
		const script = `import base from './build/eslint.base.mjs';
const ts = (c) => c.find((b) => b.languageOptions?.parserOptions);
const cb = ts(base({ root: '/r', maxTestFiles: 24, ambientGlobals: false, ignores: ['x.js'] }));
const g = ts(base({ root: '/r', testFiles: [] }));
console.log(JSON.stringify([
  cb.languageOptions.parserOptions,
  cb.rules['no-undef'],
  g.languageOptions.parserOptions.projectService,
  g.rules['no-undef'],
  base({ root: '/r', ignores: ['x.js'] })[0].ignores,
]));
try { base(); } catch (e) { console.log(e.message); }`
		out, err := runIn(t, dir, nil, "node", "--input-type=module", "-e", script)
		if err != nil {
			t.Fatalf("node: %v\n%s", err, out)
		}
		want := `[{"projectService":{"allowDefaultProject":["*.test.ts"],"maximumDefaultProjectFileMatchCount_THIS_WILL_SLOW_DOWN_LINTING":24},"tsconfigRootDir":"/r"},"error",true,"off",["node_modules/","x.js"]]` + "\n" +
			"eslint.base.mjs: pass root: import.meta.dirname\n"
		if out != want {
			t.Fatalf("got\n%s\nwant\n%s", out, want)
		}
	})

	// No chromium: playwright-core looks only in an empty browsers folder.
	none := []string{"PLAYWRIGHT_BROWSERS_PATH=" + t.TempDir()}

	t.Run("probe skips with no chromium", func(t *testing.T) {
		out, err := runIn(t, dir, none, "node", "probe.mjs")
		if err != nil || !strings.Contains(out, "probe: no chromium; skipped") || !strings.Contains(out, "probe: 8 passed, 0 failed") {
			t.Fatalf("probe: %v, want exit 0, skipped, 8 passed\n%s", err, out)
		}
	})

	t.Run("probe fails with no chromium under KIT_BROWSER=required", func(t *testing.T) {
		out, err := runIn(t, dir, append(none, "KIT_BROWSER=required"), "node", "probe.mjs")
		if exitCode(err) != 1 || !strings.Contains(out, "no chromium (KIT_BROWSER=required)") || !strings.Contains(out, "1 failed") {
			t.Fatalf("probe: %v, want exit 1 naming KIT_BROWSER=required\n%s", err, out)
		}
	})
}
