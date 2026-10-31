package pageserve

// The commands every page tool has: serve, channel, watch, report and skills.
// Moved from cull's internal/cull/cli (serve.go, channel.go, commands.go) and
// sift's skills.go, with the tool's name and its own steps as parameters.

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tools "github.com/subaud-tools/tools-common"
	"github.com/subaud-tools/tools-common/channelmcp"
	"github.com/subaud-tools/tools-common/localweb"
	"github.com/subaud-tools/tools-common/skills"
)

// Commands builds a page tool's shared commands.
type Commands struct {
	Spec    Spec    // the channel's; Spec.Tool names the tool
	Process Process // how serve starts in the background; Process.Live is where it is found
	Group   string  // the help group serve, channel and watch are listed under
	// Serve runs serve in this process until ctx ends: `<tool> serve
	// --foreground`, which the background start runs.
	Serve func(ctx context.Context, port int, errw io.Writer) error
	// Foreground: `<tool> serve` runs serve in this process with no flag and
	// opens no browser, for a tool whose one server its channel starts and
	// whose page the agent opens (diazo).
	Foreground bool
	// Page checks serve's arguments before serve is found or started, and
	// returns the fragment of the page to open ("#/p/3") and a line to print
	// once serve runs ("" for none). nil: serve takes no arguments.
	Page func(ctx context.Context, args []string) (fragment, note string, err error)
	// Skill reads the tool's skill as this binary was built with it; the
	// channel refreshes each installed copy that differs. nil: none.
	Skill func() ([]byte, error)
	// Open opens the page for `<tool> serve` instead of the system browser
	// opener, so a tool's own switch (casebook's CASEBOOK_NO_BROWSER) covers
	// it. nil: the system opener.
	Open func(url string) error
	// Around, when set, runs the channel: it calls run, which is ch.Run, and
	// its answer is the command's, so a tool can announce presence around it.
	Around func(ctx context.Context, ch *Channel, run func() error) error
}

// Help is a command's help: the argument shape after its name, its one-line
// summary, and its long description.
type Help struct {
	Synopsis, Summary, Long string
}

// openBrowser opens the page; tests replace it.
var openBrowser = localweb.OpenBrowser

func (c Commands) tool() string { return c.Spec.Tool }

// client is the channel's client: it finds serve, else starts it from this
// binary. The wait loop, watch and report use its Passive copy.
func (c Commands) client() *Client {
	return NewClient(c.Process.Live, func() (Entry, error) { return c.Process.Ensure(Executable()) })
}

func newFlags(name, usage, long string, def func(fs *flag.FlagSet)) func() *flag.FlagSet {
	return func() *flag.FlagSet {
		fs := flag.NewFlagSet(name, flag.ContinueOnError)
		tools.SetUsage(fs, usage, long)
		if def != nil {
			def(fs)
		}
		return fs
	}
}

// parse accepts flags anywhere among the positionals.
func parse(fs *flag.FlagSet, args []string, out io.Writer) ([]string, error) {
	flagArgs, pos := tools.SplitArgs(fs, args)
	if err := tools.ParseFlags(fs, flagArgs, out); err != nil {
		return nil, err
	}
	return pos, nil
}

func boolFlag(fs *flag.FlagSet, name string) bool {
	return fs.Lookup(name).Value.(flag.Getter).Get().(bool)
}

// ServeCommand is `<tool> serve [--no-open] [--port N] [--foreground]
// [--stop]`: it opens the page, starting serve in the background when it
// isn't running.
func (c Commands) ServeCommand(h Help) tools.Command {
	tool := c.tool()
	nf := newFlags("serve", strings.TrimSpace(tool+" serve "+h.Synopsis), h.Long, func(fs *flag.FlagSet) {
		fs.Bool("no-open", false, "don't open the browser")
		fs.Bool("foreground", false, "run in this process until stopped (used by the background start)")
		fs.Bool("stop", false, "stop the running server")
		fs.Int("port", 0, "port (default: the last one used, else a free one)")
	})
	run := func(args []string, out, errw io.Writer) error {
		fs := nf()
		pos, err := parse(fs, args, out)
		if err != nil {
			return err
		}
		if c.Page == nil && len(pos) > 0 {
			return tools.UsageError{Msg: "serve takes no arguments"}
		}
		ctx := context.Background()
		live := c.Process.Live
		if boolFlag(fs, "stop") {
			if err := Stop(ctx, live); errors.Is(err, ErrNotRunning) {
				_, _ = fmt.Fprintf(out, "%s serve is not running\n", tool)
				return nil
			} else if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(out, "%s serve stopped\n", tool)
			return nil
		}
		port := fs.Lookup("port").Value.(flag.Getter).Get().(int)
		if c.Foreground || boolFlag(fs, "foreground") {
			sctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
			defer stop()
			return c.Serve(sctx, port, errw)
		}
		var fragment, note string
		if c.Page != nil {
			if fragment, note, err = c.Page(ctx, pos); err != nil {
				return err
			}
		}
		e, err := live.Running()
		switch {
		case errors.Is(err, ErrNotRunning):
			if e, err = c.Process.Start(Executable(), port); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(out, "%s serve started: %s\n", tool, e.Base)
		case err != nil:
			return err
		default:
			_, _ = fmt.Fprintf(out, "%s serve is running: %s\n", tool, e.Base)
		}
		if note != "" {
			_, _ = fmt.Fprintln(out, note)
		}
		if !boolFlag(fs, "no-open") {
			open := openBrowser
			if c.Open != nil {
				open = c.Open
			}
			_ = open(e.URL + fragment)
		}
		return nil
	}
	return tools.Command{Name: "serve", Group: c.Group, Synopsis: h.Synopsis, Summary: h.Summary, NewFlags: nf, Run: run}
}

// ChannelCommand is `<tool> channel [--scope <dir>]`: the session's MCP server
// on stdin/stdout (Claude Code or pi starts it). --scope is the session's
// folder, instead of the one the harness says.
func (c Commands) ChannelCommand(stdin io.Reader, h Help) tools.Command {
	tool := c.tool()
	nf := newFlags("channel", strings.TrimSpace(tool+" channel "+h.Synopsis), h.Long, func(fs *flag.FlagSet) {
		fs.String("scope", "", "the session's folder (default: the harness's)")
	})
	run := func(args []string, out, errw io.Writer) error {
		fs := nf()
		pos, err := parse(fs, args, errw) // stdout is the MCP stream
		if err != nil {
			return err
		}
		if len(pos) > 0 {
			return tools.UsageError{Msg: "channel takes no arguments"}
		}
		c.refreshSkill(errw)
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		ch, err := sessionIn(c.Spec, c.client(), fs.Lookup("scope").Value.String())
		if err != nil {
			return err
		}
		ch.Log = errw
		run := func() error { return ch.Run(ctx, stdin, out) }
		if c.Around != nil {
			return c.Around(ctx, ch, run)
		}
		return run()
	}
	return tools.Command{Name: "channel", Group: c.Group, Synopsis: h.Synopsis, Summary: h.Summary, NewFlags: nf, Run: run}
}

// refreshSkill rewrites each installed copy of the tool's skill that differs
// from the one this binary was built with, so the skill an agent reads
// matches the binary it calls. It logs each path it rewrote; an error is
// logged and the channel starts anyway.
func (c Commands) refreshSkill(errw io.Writer) {
	if c.Skill == nil {
		return
	}
	tool := c.tool()
	body, err := c.Skill()
	if err == nil {
		var paths []string
		paths, err = skills.Refresh(tool, body)
		for _, p := range paths {
			_, _ = fmt.Fprintf(errw, "%s channel: refreshed the %s skill at %s\n", tool, tool, p)
		}
	}
	if err != nil {
		_, _ = fmt.Fprintf(errw, "%s channel: refreshing the %s skill: %v\n", tool, tool, err)
	}
}

// WatchCommand is `<tool> watch`: the channel's presence and wait loop,
// printing each Send as one JSON line on stdout for the Claude Code plugin's
// mod.
func (c Commands) WatchCommand(h Help) tools.Command {
	tool := c.tool()
	nf := newFlags("watch", strings.TrimSpace(tool+" watch "+h.Synopsis), h.Long, nil)
	run := func(args []string, out, errw io.Writer) error {
		fs := nf()
		pos, err := parse(fs, args, out)
		if err != nil {
			return err
		}
		if len(pos) > 0 {
			return tools.UsageError{Msg: "watch takes no arguments"}
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return c.Watch(ctx, out, errw, c.client().Passive())
	}
	return tools.Command{Name: "watch", Group: c.Group, Synopsis: h.Synopsis, Summary: h.Summary, NewFlags: nf, Run: run}
}

// Watch is `<tool> watch` after its flags, until ctx ends, with the client to
// serve passed in so a test can run it.
func (c Commands) Watch(ctx context.Context, out, errw io.Writer, client *Client) error {
	// In a bridge child this prints the stop line and the mod never restarts
	// watch: the hosting pi session's channel delivers the Sends.
	lines, run, err := channelmcp.StartWatch(out, c.tool())
	if !run || err != nil {
		return err
	}
	ch := New(c.Spec, FromEnv(), client)
	ch.Log = errw
	ch.Push = lines
	return ch.Watch(ctx)
}

// ReportCommand is `<tool> report`, which the Claude Code plugin's mod runs:
// it posts the report to serve through the client `<tool> watch` uses, which
// never starts serve. A failed post is the command's error, which the mod
// logs; it is not retried.
func (c Commands) ReportCommand() tools.Command {
	return channelmcp.ReportCommand(func(session string, event channelmcp.ReportEvent, token string) error {
		ctx, cancel := context.WithTimeout(context.Background(), ReportTimeout)
		defer cancel()
		return c.client().Passive().Report(ctx, session, event, token)
	})
}

// ReportTimeout bounds `<tool> report`: the Claude Code plugin's hook runs
// it and waits, so a serve that takes the connection and never answers must
// not hold the hook for the client's long-poll timeout.
var ReportTimeout = 5 * time.Second

type agents []string

func (a *agents) String() string     { return strings.Join(*a, ",") }
func (a *agents) Set(v string) error { *a = append(*a, v); return nil }

// SkillsCommand is `<tool> skills install [--agent NAME]…`: it installs the
// tool's skill as skill reads it, for each agent harness found or named.
func SkillsCommand(tool string, skill func() ([]byte, error)) tools.Command {
	usage := tool + " skills install [--agent NAME]…"
	nf := newFlags("skills", usage,
		"Installs the "+tool+" skill this binary was built with, as <skills dir>/"+tool+"/SKILL.md, for\n"+
			"each agent harness found (Claude Code, Codex, pi: by their home directories), or for each\n"+
			"--agent named (claude-code, codex, pi). The skills directory may be a symlink; nothing\n"+
			"beneath it may be. An existing SKILL.md there is replaced only when it is "+tool+"'s own\n"+
			"(name: "+tool+"). Run it again after each update to keep the skill in step with the binary.",
		func(fs *flag.FlagSet) {
			fs.Var(new(agents), "agent", "a harness to install for (repeatable): claude-code, codex or pi")
		})
	run := func(args []string, out, errw io.Writer) error {
		fs := nf()
		pos, err := parse(fs, args, out)
		if err != nil {
			return err
		}
		if len(pos) != 1 || pos[0] != "install" {
			return tools.UsageError{Msg: "expected: " + usage}
		}
		body, err := skill()
		if err != nil {
			return tools.Exitf(2, "%v", err)
		}
		return skills.Install(tool, body, *fs.Lookup("agent").Value.(*agents), out, errw)
	}
	return tools.Command{
		Name: "skills", Group: "setup", Synopsis: "install [--agent NAME]…", Subcommands: []string{"install"},
		Summary:  "install the " + tool + " skill for each agent harness, the version this binary was built with",
		NewFlags: nf, Run: run,
	}
}
