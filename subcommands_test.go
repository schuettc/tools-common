package tools

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func subApp(ran *[]string) *App {
	a := New(Config{Name: "galley", Domain: "galley.tools", Version: Version{Number: "1.0.0"}})
	a.Register(Command{
		Name:        "ledger",
		Summary:     "galley's memory",
		Synopsis:    "ledger <sync|rebuild|stats> [flags]",
		Help:        "The per-user index of review decisions.",
		Subcommands: []string{"sync", "rebuild", "stats"},
		Run: func(args []string, out, errw io.Writer) error {
			*ran = args
			return nil
		},
	})
	return a
}

// `<cmd> -h` shows the command's own help, which lists its sub-verbs.
func TestCommandHelpListsSubcommands(t *testing.T) {
	var ran []string
	var out, errw bytes.Buffer
	if code := subApp(&ran).Dispatch([]string{"ledger", "-h"}, &out, &errw); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if ran != nil {
		t.Fatalf("ledger -h ran the command with %v", ran)
	}
	if s := out.String(); !strings.Contains(s, "Usage: galley ledger <sync|rebuild|stats> [flags]") || !strings.Contains(s, "subcommands: sync, rebuild, stats") {
		t.Fatalf("help %q lacks the usage or the subcommands line", s)
	}
}

// `<cmd> <sub> ... -h` belongs to the sub-verb: it reaches Run untouched.
func TestSubcommandHelpPassesThrough(t *testing.T) {
	for _, args := range [][]string{{"ledger", "sync", "-h"}, {"ledger", "stats", "--json", "--help"}} {
		var ran []string
		var out, errw bytes.Buffer
		if code := subApp(&ran).Dispatch(args, &out, &errw); code != 0 {
			t.Fatalf("%v: exit %d", args, code)
		}
		if strings.Join(ran, " ") != strings.Join(args[1:], " ") {
			t.Fatalf("%v: Run got %v, want the args after the command", args, ran)
		}
		if out.Len() != 0 {
			t.Fatalf("%v: tools.App printed help %q instead of passing -h through", args, out.String())
		}
	}
}

// For a command that owns sub-verbs, only `<cmd> -h` is tools.App's. Any
// other word is the command's to judge: `ledger bogus -h` reaches Run, which
// rejects the unknown sub-verb, instead of exiting 0 with the parent's help.
func TestUndeclaredWordReachesRun(t *testing.T) {
	var ran []string
	var out, errw bytes.Buffer
	subApp(&ran).Dispatch([]string{"ledger", "bogus", "-h"}, &out, &errw)
	if strings.Join(ran, " ") != "bogus -h" {
		t.Fatalf("Run got %v, want [bogus -h]", ran)
	}
	if out.Len() != 0 {
		t.Fatalf("tools.App printed the parent's help for an unknown sub-verb: %q", out.String())
	}
}

func TestCommandsJSONCarriesSubcommands(t *testing.T) {
	var ran []string
	var out, errw bytes.Buffer
	subApp(&ran).Dispatch([]string{"commands", "--json"}, &out, &errw)
	if !strings.Contains(out.String(), `"subcommands": [
      "sync",
      "rebuild",
      "stats"
    ]`) {
		t.Fatalf("commands --json lacks the ledger subcommands:\n%s", out.String())
	}
	if !strings.Contains(out.String(), `"subcommands": []`) {
		t.Fatal("a command without sub-verbs must carry an empty array")
	}
}
