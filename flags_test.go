package tools

import (
	"bytes"
	"flag"
	"io"
	"strings"
	"testing"
)

// helpCmd registers a command that declares -yes/-y plus a string flag and
// parses via ParseFlags, so a test can drive the shared help/flag behavior end
// to end through Dispatch.
func helpCmd() Command {
	return Command{Name: "do", Summary: "do a thing", Run: func(args []string, out, errw io.Writer) error {
		fs := flag.NewFlagSet("do", flag.ContinueOnError)
		fs.String("manifest", "", "path to manifest")
		yes := YesFlag(fs, "skip the confirmation prompt")
		if err := ParseFlags(fs, args, out); err != nil {
			return err
		}
		if *yes {
			out.Write([]byte("skipped prompt\n"))
		} else {
			out.Write([]byte("would prompt\n"))
		}
		return nil
	}}
}

func TestParseFlagsHelpListsFlagsAndExitsClean(t *testing.T) {
	for _, arg := range []string{"-h", "--help"} {
		a := newTestApp()
		a.Register(helpCmd())
		var out, errw bytes.Buffer
		code := a.Dispatch([]string{"do", arg}, &out, &errw)
		if code != 0 {
			t.Fatalf("%s: code = %d, want 0; err=%s", arg, code, errw.String())
		}
		got := out.String()
		for _, want := range []string{"Usage of do", "-manifest", "-yes", "-y"} {
			if !strings.Contains(got, want) {
				t.Fatalf("%s: help missing %q; got:\n%s", arg, want, got)
			}
		}
		if strings.Contains(errw.String(), "help requested") {
			t.Fatalf("%s: leaked raw flag error: %q", arg, errw.String())
		}
	}
}

func TestYesFlagBothSpellings(t *testing.T) {
	for _, arg := range []string{"-yes", "-y", "--yes"} {
		a := newTestApp()
		a.Register(helpCmd())
		var out, errw bytes.Buffer
		code := a.Dispatch([]string{"do", arg}, &out, &errw)
		if code != 0 {
			t.Fatalf("%s: code = %d, want 0; err=%s", arg, code, errw.String())
		}
		if !strings.Contains(out.String(), "skipped prompt") {
			t.Fatalf("%s: flag not honored; out=%q", arg, out.String())
		}
	}
}

func TestSetUsageRendersRichHelp(t *testing.T) {
	a := newTestApp()
	a.Register(Command{Name: "serve", Run: func(args []string, out, errw io.Writer) error {
		fs := flag.NewFlagSet("serve", flag.ContinueOnError)
		fs.Int("port", 0, "port to listen on")
		SetUsage(fs, "kempt serve <page> [flags]", "Serves a page as a live document.")
		if err := ParseFlags(fs, args, out); err != nil {
			return err
		}
		return nil
	}})
	var out, errw bytes.Buffer
	code := a.Dispatch([]string{"serve", "-h"}, &out, &errw)
	if code != 0 {
		t.Fatalf("code = %d, want 0; err=%s", code, errw.String())
	}
	got := out.String()
	for _, want := range []string{"Usage: kempt serve <page> [flags]", "Serves a page as a live document.", "-port"} {
		if !strings.Contains(got, want) {
			t.Fatalf("rich help missing %q; got:\n%s", want, got)
		}
	}
}

func TestParseFlagsBadFlagIsUsageError(t *testing.T) {
	a := newTestApp()
	a.Register(helpCmd())
	var out, errw bytes.Buffer
	code := a.Dispatch([]string{"do", "-nope"}, &out, &errw)
	if code != 2 {
		t.Fatalf("code = %d, want 2; err=%s", code, errw.String())
	}
	if !strings.Contains(errw.String(), "not defined") {
		t.Fatalf("stderr = %q, want contains 'not defined'", errw.String())
	}
}

func TestPrintJSON(t *testing.T) {
	var b bytes.Buffer
	if err := PrintJSON(&b, map[string]int{"n": 2}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(b.String(), `"n": 2`) || !strings.HasSuffix(b.String(), "\n") {
		t.Fatalf("got %q", b.String())
	}
}

func TestFlagsOf(t *testing.T) {
	fs := flag.NewFlagSet("x", flag.ContinueOnError)
	fs.Int("port", 8080, "port to listen on")
	fs.Bool("quiet", false, "suppress output")
	got := FlagsOf(fs)
	if len(got) != 2 {
		t.Fatalf("len = %d", len(got))
	}
	// sorted by name: port, quiet
	if got[0].Name != "port" || got[0].Type != "int" || got[0].Default != "8080" || got[0].Usage != "port to listen on" {
		t.Fatalf("port info wrong: %+v", got[0])
	}
	if got[1].Name != "quiet" || got[1].Default != "false" {
		t.Fatalf("quiet info wrong: %+v", got[1])
	}
}

func TestSplitArgsInterspersed(t *testing.T) {
	fs := flag.NewFlagSet("new", flag.ContinueOnError)
	fs.String("agent", "", "agent")
	fs.Bool("no-sidebar", false, "suppress")
	flagArgs, pos := SplitArgs(fs, []string{"proj/work", "--agent", "claude", "--no-sidebar"})
	if len(pos) != 1 || pos[0] != "proj/work" {
		t.Fatalf("positional = %v", pos)
	}
	// --agent claude paired; --no-sidebar recognized as bool (no value swallowed)
	if err := fs.Parse(flagArgs); err != nil {
		t.Fatalf("parse: %v (flagArgs=%v)", err, flagArgs)
	}
	if fs.Lookup("agent").Value.String() != "claude" || fs.Lookup("no-sidebar").Value.String() != "true" {
		t.Fatalf("flags wrong")
	}
}

func TestSplitArgsDanglingValueFlag(t *testing.T) {
	fs := flag.NewFlagSet("x", flag.ContinueOnError)
	fs.String("subject", "", "subject")
	fs.String("intent", "", "intent")
	// --subject has no value and is followed by --intent: must not swallow --intent.
	flagArgs, _ := SplitArgs(fs, []string{"--subject", "--intent", "fyi"})
	_ = fs.Parse(flagArgs)
	if fs.Lookup("intent").Value.String() != "fyi" {
		t.Fatalf("intent wrongly bound; flagArgs=%v", flagArgs)
	}
}
