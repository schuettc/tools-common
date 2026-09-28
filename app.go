// Package tools is the shared CLI foundation for the .tools family (kempt,
// muster, galley, tackle). It factors the three things every family binary
// hand-rolls: version reporting, self-update from the family `/dl` download
// standard, and command dispatch.
//
// Usage:
//
//	app := tools.New(tools.Config{
//		Name:    "kempt",
//		Domain:  "kempt.tools",
//		Version: tools.Version{Number: version, Commit: commit, Date: date},
//	})
//	app.Register(tools.Command{Name: "sync", Summary: "...", Run: runSync})
//	os.Exit(app.Dispatch(os.Args[1:], os.Stdout, os.Stderr))
//
// stdlib-only.
package tools

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// UsageError signals a usage (exit code 2) error from a command's Run.
type UsageError struct{ Msg string }

func (e UsageError) Error() string { return e.Msg }

// Group is one heading in grouped usage, in display order.
type Group struct{ Key, Heading string }

// Command is a registrable subcommand.
type Command struct {
	Name     string
	Summary  string
	Synopsis string   // arg shape after the name; "" → just the name
	Help     string   // long-form for help <cmd>/man; "" → omitted
	Group    string   // group key; "" → default bucket
	Aliases  []string // extra words that dispatch to this command; not listed as rows
	// Subcommands names the command's own sub-verbs. `<cmd> -h` shows this
	// command's help; `<cmd> <sub> ... -h` is passed through to Run, so each
	// sub-verb can print its own flags.
	Subcommands []string
	NewFlags    func() *flag.FlagSet                           // side-effect-free flag constructor; nil → no flags
	Run         func(args []string, out, errw io.Writer) error // nil → self-routed (Task 8)
}

// Config configures a family App.
type Config struct {
	Name    string  // e.g. "kempt"
	Domain  string  // e.g. "kempt.tools"
	Version Version // the tool's own ldflags-stamped values
	Groups  []Group // optional; empty → flat usage
	About   string  // optional overview: shown by `help` above the command list and as the man DESCRIPTION
}

// App is an instance-scoped CLI: it holds the tool name, domain, version, and
// command registry (no package globals).
type App struct {
	name     string
	domain   string
	version  Version
	registry map[string]Command
	aliases  map[string]string // alias → canonical Name
	groups   []Group
	about    string
	dlHost   string
	client   *http.Client
	exePath  func() (string, error)
}

// New builds an App, sets dlHost = "https://"+cfg.Domain, and auto-registers
// the five built-in commands: version, help, update, man, commands.
func New(cfg Config) *App {
	a := &App{
		name:     cfg.Name,
		domain:   cfg.Domain,
		version:  cfg.Version,
		registry: map[string]Command{},
		aliases:  map[string]string{},
		groups:   cfg.Groups,
		about:    cfg.About,
		dlHost:   "https://" + cfg.Domain,
		client:   http.DefaultClient,
		exePath:  os.Executable,
	}
	a.Register(Command{
		Name:    "version",
		Summary: "print " + a.name + " version",
		Run: func(args []string, out, errw io.Writer) error {
			fmt.Fprintf(out, "%s %s\n", a.name, a.version.String())
			return nil
		},
	})
	a.Register(Command{
		Name:    "help",
		Summary: "show usage, or `help <command>` for one command",
		Run: func(args []string, out, errw io.Writer) error {
			if len(args) > 0 {
				if c, ok := a.lookup(args[0]); ok {
					HelpFor(out, a.name, c)
					return nil
				}
				return UsageError{Msg: fmt.Sprintf("unknown command %q", args[0])}
			}
			if a.about != "" {
				fmt.Fprintf(out, "%s\n\n", strings.TrimRight(a.about, "\n"))
			}
			a.usage(out)
			return nil
		},
	})
	a.Register(Command{
		Name:    "update",
		Summary: "update " + a.name + " to the latest release",
		Run: func(args []string, out, errw io.Writer) error {
			updated, newVersion, err := a.SelfUpdate(out, errw)
			if err != nil {
				return err
			}
			if updated {
				fmt.Fprintf(out, "%s updated to %s\n", a.name, newVersion)
			} else {
				fmt.Fprintf(out, "%s is already the latest (%s)\n", a.name, a.version.Number)
			}
			return nil
		},
	})
	a.Register(Command{
		Name:    "man",
		Summary: "print a roff man page",
		Run: func(_ []string, out, errw io.Writer) error {
			cmds := a.commands()
			fmt.Fprint(out, manPage(a.name, a.domain, a.about, a.groups, cmds))
			return nil
		},
	})
	a.Register(Command{
		Name:    "commands",
		Summary: "list commands (--json for the machine-readable index)",
		NewFlags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("commands", flag.ContinueOnError)
			fs.Bool("json", false, "emit the machine-readable command index")
			return fs
		},
		Run: func(args []string, out, errw io.Writer) error {
			cmds := a.commands()
			if hasJSONFlag(args) {
				b, err := CommandsJSON(a.name, cmds)
				if err != nil {
					return err
				}
				fmt.Fprintf(out, "%s\n", b)
				return nil
			}
			GroupedUsage(out, a.name, a.groups, cmds)
			return nil
		},
	})
	return a
}

// Register adds or overrides a command by Name. A tool can override a built-in
// (e.g. wrap "update" with domain-specific logic); an override replaces the
// old command's aliases. Register panics when the Name or an alias is already
// taken by a different command: two words meaning different things is a
// registration bug, and panicking surfaces it in any test that builds the App.
func (a *App) Register(cmd Command) {
	if old, ok := a.registry[cmd.Name]; ok {
		for _, al := range old.Aliases {
			delete(a.aliases, al)
		}
	}
	if owner, ok := a.aliases[cmd.Name]; ok {
		panic(fmt.Sprintf("%s: command %q is already an alias of %q", a.name, cmd.Name, owner))
	}
	for _, al := range cmd.Aliases {
		if _, ok := a.registry[al]; ok && al != cmd.Name {
			panic(fmt.Sprintf("%s: alias %q of %q is already a command", a.name, al, cmd.Name))
		}
		if owner, ok := a.aliases[al]; ok {
			panic(fmt.Sprintf("%s: alias %q of %q is already an alias of %q", a.name, al, cmd.Name, owner))
		}
	}
	a.registry[cmd.Name] = cmd
	for _, al := range cmd.Aliases {
		a.aliases[al] = cmd.Name
	}
}

// lookup resolves a command word, canonical name or alias.
func (a *App) lookup(word string) (Command, bool) {
	if c, ok := a.registry[word]; ok {
		return c, true
	}
	if name, ok := a.aliases[word]; ok {
		return a.registry[name], true
	}
	return Command{}, false
}

// commands returns every registered command once (aliases are not rows).
func (a *App) commands() []Command {
	cmds := make([]Command, 0, len(a.registry))
	for _, c := range a.registry {
		cmds = append(cmds, c)
	}
	return cmds
}

func (a *App) groupList() []Group { return a.groups }

func (a *App) usage(w io.Writer) {
	GroupedUsage(w, a.name, a.groups, a.commands())
}

// Dispatch routes args to a registered command and returns the process exit
// code (0 ok, 1 runtime error, 2 usage error).
func (a *App) Dispatch(args []string, out, errw io.Writer) int {
	if len(args) == 0 {
		a.usage(errw)
		return 2
	}
	name := args[0]
	switch name {
	case "--version", "-v":
		name = "version"
	case "--help", "-h":
		name = "help"
	}
	jsonMode := hasJSONFlag(args[1:])
	cmd, ok := a.lookup(name)
	if !ok {
		fmt.Fprintf(errw, "%s: unknown command %q\n\n", a.name, name)
		a.usage(errw)
		return 2
	}
	if (cmd.Help != "" || cmd.Synopsis != "" || cmd.NewFlags != nil) && hasHelpArg(args[1:]) && !isSubcommand(cmd, args[1:]) {
		HelpFor(out, a.name, cmd)
		return 0
	}
	if cmd.Run == nil {
		a.writeErr(errw, name, jsonMode, 2, fmt.Sprintf("%q is handled by %s itself, not dispatch", name, a.name), "")
		return 2
	}
	if err := cmd.Run(args[1:], out, errw); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		var ee *ExitError
		if errors.As(err, &ee) {
			code := ee.Code
			if code == 0 {
				code = 1
			}
			a.writeErr(errw, name, jsonMode, code, ee.Msg, ee.Hint)
			return code
		}
		var ue UsageError
		if errors.As(err, &ue) {
			a.writeErr(errw, name, jsonMode, 2, ue.Msg, "")
			return 2
		}
		a.writeErr(errw, name, jsonMode, 1, err.Error(), "")
		return 1
	}
	return 0
}

// setDLHost overrides the download host; used by tests.
func (a *App) setDLHost(h string) { a.dlHost = h }

// hasJSONFlag reports whether a global --json/-json appears in args.
func hasJSONFlag(args []string) bool {
	for _, a := range args {
		if a == "--json" || a == "-json" {
			return true
		}
	}
	return false
}

// hasHelpArg reports whether -h or --help appears in args.
// isSubcommand reports whether args start with one of cmd's declared
// sub-verbs, whose -h belongs to the sub-verb rather than the command.
func isSubcommand(cmd Command, args []string) bool {
	if len(args) == 0 {
		return false
	}
	for _, s := range cmd.Subcommands {
		if args[0] == s {
			return true
		}
	}
	return false
}

func hasHelpArg(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			return true
		}
	}
	return false
}

// writeErr renders a command error to errw, as a JSON envelope when jsonMode,
// else as "name cmd: msg" (+ hint line).
func (a *App) writeErr(errw io.Writer, name string, jsonMode bool, code int, msg, hint string) {
	if jsonMode {
		env := map[string]any{"error": msg, "code": code}
		if hint != "" {
			env["hint"] = hint
		}
		_ = PrintJSON(errw, env)
		return
	}
	fmt.Fprintf(errw, "%s %s: %s\n", a.name, name, msg)
	if hint != "" {
		fmt.Fprintf(errw, "%s\n", hint)
	}
}
