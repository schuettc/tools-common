package tools

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
)

// SetUsage installs a rich per-command help function on fs, adopting galley's
// pattern family-wide: -h (and a bad flag) print a one-line usage, a prose
// description, then the flag defaults. Call it after declaring flags and before
// ParseFlags, which routes help and parse errors through it.
//
//	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
//	port := fs.Int("port", 0, "port to listen on")
//	tools.SetUsage(fs, "galley serve <page.html> [flags]",
//		"Serves a review page as a live document…")
//	if err := tools.ParseFlags(fs, args, out); err != nil {
//		return err
//	}
//
// The usage line and prose are written to fs.Output(), which ParseFlags points
// at the caller's writer, so help never hardcodes os.Stderr. A command that
// wants only the flag listing skips SetUsage and gets the generic fallback.
func SetUsage(fs *flag.FlagSet, usage, long string) {
	fs.Usage = func() {
		w := fs.Output()
		if usage != "" {
			fmt.Fprintf(w, "Usage: %s\n\n", usage)
		}
		if long = strings.TrimSpace(long); long != "" {
			fmt.Fprintf(w, "%s\n\n", long)
		}
		fs.PrintDefaults()
	}
}

// ParseFlags parses fs against args with a uniform help story for every family
// command. On -h/-help it renders the command's help to out and returns
// flag.ErrHelp; Dispatch treats a Run that returns flag.ErrHelp as a clean
// (exit 0) result, so a caller can simply return the error. Any other parse
// error renders the help too (galley's usageErr behavior) and is wrapped as a
// UsageError (exit 2).
//
// Without this, a command whose FlagSet has SetOutput(io.Discard) turns -h into
// a bare "flag: help requested" with no flag listing — which is how a real -yes
// flag came to look as though it did not exist. Callers declare their flags on
// fs (optionally SetUsage), then:
//
//	if err := tools.ParseFlags(fs, args, out); err != nil {
//		return err
//	}
func ParseFlags(fs *flag.FlagSet, args []string, out io.Writer) error {
	// Suppress flag's own auto-usage; renderUsage drives it so help always goes
	// to out (not the FlagSet's default os.Stderr) in exactly one format.
	fs.SetOutput(io.Discard)
	switch err := fs.Parse(args); {
	case err == nil:
		return nil
	case errors.Is(err, flag.ErrHelp):
		renderUsage(fs, out)
		return flag.ErrHelp
	default:
		renderUsage(fs, out)
		return UsageError{Msg: err.Error()}
	}
}

// renderUsage writes fs's help to out via fs.Usage — the caller's rich SetUsage
// function when one is installed, otherwise flag's own default ("Usage of
// <name>:" followed by the flag defaults). NewFlagSet always installs the
// default, so fs.Usage is never nil.
func renderUsage(fs *flag.FlagSet, out io.Writer) {
	fs.SetOutput(out)
	fs.Usage()
}

// YesFlag registers the confirmation-skip flag under both -yes and its -y
// shorthand on fs, sharing one destination so either spelling works. Every
// command that carries a [y/N] prompt uses this so the flag and its shorthand
// are spelled once, family-wide.
func YesFlag(fs *flag.FlagSet, usage string) *bool {
	yes := new(bool)
	fs.BoolVar(yes, "yes", false, usage)
	fs.BoolVar(yes, "y", false, "shorthand for -yes")
	return yes
}

// PrintJSON writes v as indented JSON followed by a newline. It is the one
// spelling of the --json payload shape, so every command's structured output
// matches.
func PrintJSON(w io.Writer, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", b)
	return err
}
