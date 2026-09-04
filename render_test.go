package tools

import (
	"bytes"
	"encoding/json"
	"flag"
	"strings"
	"testing"
)

func sampleCmds() []Command {
	return []Command{
		{Name: "send", Summary: "send a message", Group: "talk"},
		{Name: "inbox", Summary: "read inbox", Group: "watch"},
		{Name: "debug", Summary: "raw call"}, // no group → Other
	}
}

func TestGroupedUsageWithGroups(t *testing.T) {
	var b bytes.Buffer
	groups := []Group{{Key: "talk", Heading: "Talk"}, {Key: "watch", Heading: "Watch"}}
	GroupedUsage(&b, "muster", groups, sampleCmds())
	s := b.String()
	if !strings.Contains(s, "usage: muster") {
		t.Fatalf("no usage line: %q", s)
	}
	// Talk heading precedes Watch heading precedes Other; send under Talk.
	iTalk, iWatch, iOther := strings.Index(s, "Talk"), strings.Index(s, "Watch"), strings.Index(s, "Other")
	if !(iTalk < iWatch && iWatch < iOther) {
		t.Fatalf("group order wrong: %q", s)
	}
	if !strings.Contains(s, "send") || !strings.Contains(s, "debug") {
		t.Fatalf("commands missing: %q", s)
	}
}

func TestGroupedUsageFlat(t *testing.T) {
	var b bytes.Buffer
	GroupedUsage(&b, "kempt", nil, sampleCmds())
	s := b.String()
	if strings.Contains(s, "Talk") || strings.Contains(s, "Other") {
		t.Fatalf("flat usage should have no headings: %q", s)
	}
	if !strings.Contains(s, "send") {
		t.Fatalf("commands missing: %q", s)
	}
}

func TestHelpForRendersSynopsisHelpFlags(t *testing.T) {
	c := Command{
		Name: "serve", Synopsis: "serve <page> [flags]",
		Help: "Serves a page as a live document.",
		NewFlags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("serve", flag.ContinueOnError)
			fs.Int("port", 0, "port to listen on")
			return fs
		},
	}
	var b bytes.Buffer
	HelpFor(&b, "galley", c)
	s := b.String()
	for _, want := range []string{"Usage: galley serve <page> [flags]", "Serves a page as a live document.", "-port", "port to listen on"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in:\n%s", want, s)
		}
	}
}

func TestManPageStructure(t *testing.T) {
	cmds := []Command{{Name: "send", Summary: "send a message", Synopsis: "send <target>", Help: "Sends it.", Group: "talk"}}
	groups := []Group{{Key: "talk", Heading: "Talk"}}
	m := ManPage("muster", "muster.tools", groups, cmds)
	for _, want := range []string{`.TH MUSTER 1`, "NAME", "SYNOPSIS", "send", "Sends it."} {
		if !strings.Contains(m, want) {
			t.Fatalf("man page missing %q:\n%s", want, m)
		}
	}
}

func TestRoffEscape(t *testing.T) {
	if got := roffEscape(`a\b-c`); !strings.Contains(got, `\\`) {
		t.Fatalf("backslash not escaped: %q", got)
	}
}

func TestCommandsJSONShape(t *testing.T) {
	cmds := []Command{{
		Name: "serve", Summary: "serve", Synopsis: "serve <page>", Group: "core", Help: "Serves.",
		NewFlags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("serve", flag.ContinueOnError)
			fs.Int("port", 0, "port")
			return fs
		},
	}}
	b, err := CommandsJSON("galley", cmds)
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("not JSON: %v", err)
	}
	if got[0]["name"] != "serve" || got[0]["group"] != "core" {
		t.Fatalf("fields wrong: %v", got[0])
	}
	flags := got[0]["flags"].([]any)
	if len(flags) != 1 || flags[0].(map[string]any)["name"] != "port" {
		t.Fatalf("flags wrong: %v", flags)
	}
}
