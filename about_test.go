package tools

import (
	"bytes"
	"strings"
	"testing"
)

const aboutText = "galley — review before the one-way door.\n\nPull is the better loop."

func aboutApp() *App {
	return New(Config{Name: "galley", Domain: "galley.tools", Version: Version{Number: "1.0.0"}, About: aboutText})
}

func TestHelpShowsAboutAboveCommands(t *testing.T) {
	var out, errw bytes.Buffer
	if code := aboutApp().Dispatch([]string{"help"}, &out, &errw); code != 0 {
		t.Fatalf("exit %d", code)
	}
	s := out.String()
	a, u := strings.Index(s, "Pull is the better loop."), strings.Index(s, "usage: galley")
	if a < 0 || u < 0 || a > u {
		t.Fatalf("help must print About, then the usage listing:\n%s", s)
	}
}

// Bare invocation is a usage error: short, on stderr, no overview.
func TestBareUsageOmitsAbout(t *testing.T) {
	var out, errw bytes.Buffer
	if code := aboutApp().Dispatch(nil, &out, &errw); code != 2 {
		t.Fatalf("exit %d, want 2", code)
	}
	if strings.Contains(errw.String(), "Pull is the better loop.") {
		t.Fatalf("bare usage must not print About:\n%s", errw.String())
	}
}

func TestManDescriptionIsAbout(t *testing.T) {
	var out, errw bytes.Buffer
	aboutApp().Dispatch([]string{"man"}, &out, &errw)
	s := out.String()
	if !strings.Contains(s, ".SH DESCRIPTION\ngalley \\(em review before the one\\-way door.") && !strings.Contains(s, ".SH DESCRIPTION\ngalley — review before the one\\-way door.") {
		t.Fatalf("man DESCRIPTION must be About:\n%s", s)
	}
	if strings.Contains(s, "Commands for galley (galley.tools).") {
		t.Fatal("About replaces the default description")
	}
}

func TestNoAboutKeepsDefaults(t *testing.T) {
	a := New(Config{Name: "kempt", Domain: "kempt.tools", Version: Version{Number: "1.0.0"}})
	var out, errw bytes.Buffer
	a.Dispatch([]string{"help"}, &out, &errw)
	if !strings.HasPrefix(out.String(), "usage: kempt") {
		t.Fatalf("help without About must start with usage:\n%s", out.String())
	}
	out.Reset()
	a.Dispatch([]string{"man"}, &out, &errw)
	if !strings.Contains(out.String(), "Commands for kempt (kempt.tools).") {
		t.Fatal("default man description changed")
	}
}
