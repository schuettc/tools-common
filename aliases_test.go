package tools

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

func aliasApp(got *[]string) *App {
	a := New(Config{Name: "muster", Domain: "muster.tools", Version: Version{Number: "1.0.0"}})
	a.Register(Command{
		Name:     "inbox",
		Aliases:  []string{"get_inbox"},
		Summary:  "show unread threads",
		Synopsis: "inbox <alias>",
		Help:     "Lists threads with unread entries.",
		Run: func(args []string, out, errw io.Writer) error {
			*got = append([]string{"inbox"}, args...)
			return nil
		},
	})
	a.Register(Command{Name: "send", Summary: "send a message", Run: func([]string, io.Writer, io.Writer) error { return nil }})
	return a
}

func TestDispatchResolvesAlias(t *testing.T) {
	var got []string
	a := aliasApp(&got)
	var out, errw bytes.Buffer
	if code := a.Dispatch([]string{"get_inbox", "x"}, &out, &errw); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errw.String())
	}
	if strings.Join(got, " ") != "inbox x" {
		t.Fatalf("ran %v, want [inbox x]", got)
	}
}

func TestHelpResolvesAlias(t *testing.T) {
	var got []string
	for _, args := range [][]string{{"help", "get_inbox"}, {"get_inbox", "-h"}} {
		a := aliasApp(&got)
		var out, errw bytes.Buffer
		if code := a.Dispatch(args, &out, &errw); code != 0 {
			t.Fatalf("%v: exit %d, stderr %q", args, code, errw.String())
		}
		s := out.String()
		if !strings.Contains(s, "Usage: muster inbox <alias>") || !strings.Contains(s, "aliases: get_inbox") {
			t.Fatalf("%v: help %q lacks the inbox usage or its aliases line", args, s)
		}
	}
}

func TestUsageListsCanonicalNameOnly(t *testing.T) {
	var got []string
	a := aliasApp(&got)
	var out, errw bytes.Buffer
	a.Dispatch([]string{"help"}, &out, &errw)
	man := ManPage("muster", "muster.tools", nil, a.commands())
	for name, s := range map[string]string{"usage": out.String(), "man": man} {
		if !strings.Contains(s, "inbox") {
			t.Fatalf("%s lacks inbox: %q", name, s)
		}
		if strings.Contains(s, "get_inbox") {
			t.Fatalf("%s lists the alias as its own row: %q", name, s)
		}
	}
}

func TestCommandsJSONCarriesAliases(t *testing.T) {
	var got []string
	a := aliasApp(&got)
	var out, errw bytes.Buffer
	if code := a.Dispatch([]string{"commands", "--json"}, &out, &errw); code != 0 {
		t.Fatalf("exit %d", code)
	}
	var idx []struct {
		Name    string   `json:"name"`
		Aliases []string `json:"aliases"`
	}
	if err := json.Unmarshal(out.Bytes(), &idx); err != nil {
		t.Fatal(err)
	}
	seen := map[string][]string{}
	for _, c := range idx {
		if c.Aliases == nil {
			t.Fatalf("%s: aliases is null, want an array", c.Name)
		}
		seen[c.Name] = c.Aliases
	}
	if a := seen["inbox"]; len(a) != 1 || a[0] != "get_inbox" {
		t.Fatalf("inbox aliases %v", a)
	}
	if a := seen["send"]; len(a) != 0 {
		t.Fatalf("send aliases %v, want []", a)
	}
}

func TestRegisterPanicsOnAliasCollision(t *testing.T) {
	for name, cmd := range map[string]Command{
		"alias equals a command name":  {Name: "extra", Aliases: []string{"send"}},
		"alias equals another's alias": {Name: "extra", Aliases: []string{"get_inbox"}},
		"name equals another's alias":  {Name: "get_inbox"},
	} {
		t.Run(name, func(t *testing.T) {
			var got []string
			a := aliasApp(&got)
			defer func() {
				if recover() == nil {
					t.Fatal("Register did not panic")
				}
			}()
			a.Register(cmd)
		})
	}
}

// Overriding a command by Name (the documented way to replace a built-in)
// replaces its aliases too, rather than colliding with its own old ones.
func TestReRegisterReplacesAliases(t *testing.T) {
	var got []string
	a := aliasApp(&got)
	a.Register(Command{Name: "inbox", Aliases: []string{"get_inbox", "mail"}, Run: func([]string, io.Writer, io.Writer) error { return nil }})
	var out, errw bytes.Buffer
	if code := a.Dispatch([]string{"mail"}, &out, &errw); code != 0 {
		t.Fatalf("new alias: exit %d", code)
	}
	a.Register(Command{Name: "inbox", Run: func([]string, io.Writer, io.Writer) error { return nil }})
	if code := a.Dispatch([]string{"mail"}, &out, &errw); code != 2 {
		t.Fatalf("dropped alias still dispatches (exit %d)", code)
	}
}
