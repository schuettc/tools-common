package pageserve

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tools "github.com/subaud-tools/tools-common"
)

const testSkill = "---\nname: cull\ndescription: test\n---\nbody\n"

func skill() ([]byte, error) { return []byte(testSkill), nil }

// `<tool> skills install --agent pi` installs the tool's skill under pi's
// skills directory.
func TestSkillsInstall(t *testing.T) {
	pi := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", pi)
	cmd := SkillsCommand("cull", skill)
	if cmd.Name != "skills" || cmd.Synopsis != "install [--agent NAME]…" {
		t.Fatalf("%+v", cmd)
	}
	var out, errw bytes.Buffer
	if err := cmd.Run([]string{"install", "--agent", "pi"}, &out, &errw); err != nil {
		t.Fatalf("%v: %s", err, errw.String())
	}
	b, err := os.ReadFile(filepath.Join(pi, "skills", "cull", "SKILL.md"))
	if err != nil || string(b) != testSkill {
		t.Fatalf("%q %v", b, err)
	}
	var u tools.UsageError
	if err := cmd.Run([]string{"remove"}, &out, &errw); !errors.As(err, &u) || u.Msg != "expected: cull skills install [--agent NAME]…" {
		t.Fatalf("%v", err)
	}
}

// The channel refreshes an installed copy of the skill that differs from the
// binary's, and says where.
func TestChannelRefreshesTheSkill(t *testing.T) {
	pi := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", pi)
	path := filepath.Join(pi, "skills", "cull", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: cull\n---\nold\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var errw bytes.Buffer
	Commands{Spec: Spec{Tool: "cull"}, Skill: skill}.refreshSkill(&errw)
	if b, _ := os.ReadFile(path); string(b) != testSkill {
		t.Fatalf("skill not refreshed: %q", b)
	}
	if !strings.Contains(errw.String(), "cull channel: refreshed the cull skill at ") {
		t.Fatalf("log %q", errw.String())
	}
}

func cullCommands(t *testing.T) Commands {
	t.Helper()
	t.Setenv("CULL_HOME", t.TempDir())
	return Commands{
		Spec:    cullSpec(),
		Process: Process{Live: LiveFor("cull"), Args: []string{"serve", "--foreground", "--no-open"}},
		Group:   "check",
	}
}

func TestServeStopWhenNotRunning(t *testing.T) {
	cmd := cullCommands(t).ServeCommand(Help{Synopsis: "[--stop]", Summary: "open the page"})
	var out bytes.Buffer
	if err := cmd.Run([]string{"--stop"}, &out, io.Discard); err != nil || out.String() != "cull serve is not running\n" {
		t.Fatalf("%v %q", err, out.String())
	}
	if cmd.Group != "check" || cmd.Summary != "open the page" {
		t.Fatalf("%+v", cmd)
	}
}

func TestServeRunsTheForegroundStep(t *testing.T) {
	c := cullCommands(t)
	var gotPort int
	c.Serve = func(_ context.Context, port int, _ io.Writer) error { gotPort = port; return nil }
	if err := c.ServeCommand(Help{}).Run([]string{"--foreground", "--port", "4321"}, io.Discard, io.Discard); err != nil || gotPort != 4321 {
		t.Fatalf("%v port %d", err, gotPort)
	}
}

// serve checks its arguments with Page first, then opens the running serve's
// page at Page's fragment, printing Page's note.
func TestServeOpensTheRunningPage(t *testing.T) {
	c := cullCommands(t)
	live := c.Process.Live
	if err := live.Advertise(Entry{URL: "http://127.0.0.1:1/?t=x", Base: "http://127.0.0.1:1", Token: "x", PID: os.Getpid()}); err != nil {
		t.Fatal(err)
	}
	c.Page = func(_ context.Context, args []string) (string, string, error) {
		if len(args) != 1 {
			return "", "", tools.UsageError{Msg: "serve takes at most one path"}
		}
		return "#/p/3", "no review for " + args[0] + " yet", nil
	}
	var opened string
	orig := openBrowser
	openBrowser = func(u string) error { opened = u; return nil }
	t.Cleanup(func() { openBrowser = orig })
	var out bytes.Buffer
	if err := c.ServeCommand(Help{}).Run([]string{"repo"}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if want := "cull serve is running: http://127.0.0.1:1\nno review for repo yet\n"; out.String() != want {
		t.Fatalf("out %q, want %q", out.String(), want)
	}
	if opened != "http://127.0.0.1:1/?t=x#/p/3" {
		t.Fatalf("opened %q", opened)
	}
	var u tools.UsageError
	if err := c.ServeCommand(Help{}).Run(nil, &out, io.Discard); !errors.As(err, &u) {
		t.Fatalf("Page's refusal: %v", err)
	}
	c.Page = nil
	if err := c.ServeCommand(Help{}).Run([]string{"repo"}, &out, io.Discard); !errors.As(err, &u) || u.Msg != "serve takes no arguments" {
		t.Fatalf("no Page: %v", err)
	}
}

// With no serve running, serve starts one in the background with the
// tool's args.
func TestServeStartsServe(t *testing.T) {
	c := cullCommands(t)
	var args []string
	orig := newCommand
	newCommand = func(_ string, a ...string) *exec.Cmd {
		args = a
		script := `printf '{"url":"http://127.0.0.1:1/?t=x","base":"http://127.0.0.1:1","pid":%d}' $$ > "$1"; sleep 3`
		return exec.Command("sh", "-c", script, "sh", c.Process.Live.ServePath())
	}
	t.Cleanup(func() { newCommand = orig })
	if err := os.MkdirAll(c.Process.Live.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := c.ServeCommand(Help{}).Run([]string{"--no-open"}, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	if e, err := c.Process.Live.Running(); err == nil {
		if p, _ := os.FindProcess(e.PID); p != nil {
			_ = p.Kill()
		}
	}
	if out.String() != "cull serve started: http://127.0.0.1:1\n" || strings.Join(args, " ") != "serve --foreground --no-open" {
		t.Fatalf("out %q args %v", out.String(), args)
	}
}

func TestChannelAndWatchTakeNoArguments(t *testing.T) {
	c := cullCommands(t)
	for _, cmd := range []tools.Command{c.ChannelCommand(strings.NewReader(""), Help{}), c.WatchCommand(Help{})} {
		var u tools.UsageError
		if err := cmd.Run([]string{"x"}, io.Discard, io.Discard); !errors.As(err, &u) || u.Msg != cmd.Name+" takes no arguments" {
			t.Errorf("%s: %v", cmd.Name, err)
		}
		if err := cmd.Run([]string{"-h"}, io.Discard, io.Discard); !errors.Is(err, flag.ErrHelp) {
			t.Errorf("%s -h: %v", cmd.Name, err)
		}
	}
}
