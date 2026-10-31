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

const namedSkill = "---\nname: reviewing-a-document\ndescription: test\n---\nbody\n"

func namedCommands() Commands {
	return Commands{Spec: Spec{Tool: "galley"}, SkillName: "reviewing-a-document",
		Skill: func() ([]byte, error) { return []byte(namedSkill), nil }}
}

// A skill named apart from its tool is installed under its own name.
func TestSkillsInstallUsesTheSkillsName(t *testing.T) {
	pi := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", pi)
	cmd := namedCommands().SkillsCommand()
	var out, errw bytes.Buffer
	if err := cmd.Run([]string{"install", "--agent", "pi"}, &out, &errw); err != nil {
		t.Fatalf("%v: %s", err, errw.String())
	}
	if b, err := os.ReadFile(filepath.Join(pi, "skills", "reviewing-a-document", "SKILL.md")); err != nil || string(b) != namedSkill {
		t.Fatalf("%q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(pi, "skills", "galley")); !os.IsNotExist(err) {
		t.Fatalf("installed under the tool's name: %v", err)
	}
	var u tools.UsageError
	if err := cmd.Run([]string{"remove"}, &out, &errw); !errors.As(err, &u) || u.Msg != "expected: galley skills install [--agent NAME]…" {
		t.Fatalf("%v", err)
	}
}

// With no SkillName the skill goes under the tool's name.
func TestSkillsCommandDefaultsToTheToolsName(t *testing.T) {
	pi := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", pi)
	var out, errw bytes.Buffer
	c := Commands{Spec: Spec{Tool: "cull"}, Skill: skill}
	if err := c.SkillsCommand().Run([]string{"install", "--agent", "pi"}, &out, &errw); err != nil {
		t.Fatalf("%v: %s", err, errw.String())
	}
	if _, err := os.Stat(filepath.Join(pi, "skills", "cull", "SKILL.md")); err != nil {
		t.Fatal(err)
	}
}

// The channel refreshes a skill named apart from its tool.
func TestChannelRefreshesASkillNamedApart(t *testing.T) {
	pi := t.TempDir()
	t.Setenv("PI_CODING_AGENT_DIR", pi)
	path := filepath.Join(pi, "skills", "reviewing-a-document", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("---\nname: reviewing-a-document\n---\nold\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var errw bytes.Buffer
	namedCommands().refreshSkill(&errw)
	if b, _ := os.ReadFile(path); string(b) != namedSkill {
		t.Fatalf("skill not refreshed: %q", b)
	}
	if !strings.Contains(errw.String(), "galley channel: refreshed the reviewing-a-document skill at ") {
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

type readSpy struct{ read bool }

func (r *readSpy) Read([]byte) (int, error) { r.read = true; return 0, io.EOF }

// Commands.Around wraps the channel's run, so a tool can announce presence
// around it, and its answer is the command's.
func TestChannelRunsInsideAround(t *testing.T) {
	c := cullCommands(t)
	stdin := &readSpy{}
	var before, after bool
	c.Around = func(_ context.Context, ch *Channel, run func() error) error {
		before = !stdin.read && ch.Spec.Tool == "cull"
		err := run()
		after = stdin.read
		if err != nil {
			return err
		}
		return errors.New("from around")
	}
	err := c.ChannelCommand(stdin, Help{}).Run(nil, io.Discard, io.Discard)
	if !before || !after {
		t.Fatalf("before %v after %v", before, after)
	}
	if err == nil || err.Error() != "from around" {
		t.Fatalf("err %v", err)
	}
}

// channels.json runs `galley channel --scope .`: --scope is the session's
// folder, resolved from where the channel starts.
func TestChannelScopeIsTheSessionsFolder(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "other")
	if err := os.Mkdir(other, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("AGENT_SESSION_ID", "s-1")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	for _, tc := range []struct {
		args []string
		cwd  string
	}{
		{[]string{"--scope", "."}, dir},
		{[]string{"--scope", other}, other},
		{[]string{"--scope=other"}, other},
	} {
		c := cullCommands(t)
		var got *Channel
		c.Around = func(_ context.Context, ch *Channel, _ func() error) error { got = ch; return nil }
		if err := c.ChannelCommand(strings.NewReader(""), Help{}).Run(tc.args, io.Discard, io.Discard); err != nil {
			t.Fatalf("%v: %v", tc.args, err)
		}
		if got.ID.CWD != tc.cwd || got.scope() != tc.cwd || got.ID.Label != "pi · "+filepath.Base(tc.cwd) {
			t.Errorf("%v: id %+v scope %q", tc.args, got.ID, got.scope())
		}
		if root, _ := got.Root(); root != tc.cwd {
			t.Errorf("%v: Spec.Root got %q", tc.args, root)
		}
	}
}
