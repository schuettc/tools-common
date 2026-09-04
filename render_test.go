package tools

import (
	"bytes"
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
