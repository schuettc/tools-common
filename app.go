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
	"sort"
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
	Synopsis string                                          // arg shape after the name; "" → just the name
	Help     string                                          // long-form for help <cmd>/man; "" → omitted
	Group    string                                          // group key; "" → default bucket
	NewFlags func() *flag.FlagSet                            // side-effect-free flag constructor; nil → no flags
	Run      func(args []string, out, errw io.Writer) error // nil → self-routed (Task 8)
}

// Config configures a family App.
type Config struct {
	Name    string  // e.g. "kempt"
	Domain  string  // e.g. "kempt.tools"
	Version Version // the tool's own ldflags-stamped values
	Groups  []Group // optional; empty → flat usage
}

// App is an instance-scoped CLI: it holds the tool name, domain, version, and
// command registry (no package globals).
type App struct {
	name     string
	domain   string
	version  Version
	registry map[string]Command
	groups   []Group
	dlHost   string
	client   *http.Client
	exePath  func() (string, error)
}

// New builds an App, sets dlHost = "https://"+cfg.Domain, and auto-registers
// the three built-in commands: version, help, update.
func New(cfg Config) *App {
	a := &App{
		name:     cfg.Name,
		domain:   cfg.Domain,
		version:  cfg.Version,
		registry: map[string]Command{},
		groups:   cfg.Groups,
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
		Summary: "show usage",
		Run: func(args []string, out, errw io.Writer) error {
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
	return a
}

// Register adds or overrides a command by Name. A tool can override a built-in
// (e.g. wrap "update" with domain-specific logic).
func (a *App) Register(cmd Command) { a.registry[cmd.Name] = cmd }

func (a *App) groupList() []Group { return a.groups }

func (a *App) usage(w io.Writer) {
	fmt.Fprintf(w, "usage: %s <command> [args]\n", a.name)
	fmt.Fprintln(w, "\ncommands:")
	names := make([]string, 0, len(a.registry))
	for n := range a.registry {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		fmt.Fprintf(w, "  %-10s %s\n", n, a.registry[n].Summary)
	}
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
	cmd, ok := a.registry[name]
	if !ok {
		fmt.Fprintf(errw, "%s: unknown command %q\n\n", a.name, name)
		a.usage(errw)
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
