package tools

import (
	"strings"
	"testing"
)

// A leading "." or "'" would be read as a roff request, and a bare
// "quoted word" has its quotes eaten by .B/.TP's argument parser, which splits
// the synopsis into separate arguments. (Ported from muster's man renderer.)
func TestRoffEscapeNeutralizesLeadingDotAndQuotes(t *testing.T) {
	got := roffEscape(".dangerous line with \"a quote\" and a \\backslash\n'also a request")
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(line, ".") || strings.HasPrefix(line, "'") {
			t.Errorf("roffEscape left a line starting with a control character: %q", line)
		}
	}
	if strings.Contains(got, `"`) {
		t.Errorf("roffEscape left a literal double quote: %q", got)
	}
	if !strings.Contains(got, `\\backslash`) {
		t.Errorf("roffEscape did not double the backslash: %q", got)
	}
}

func TestManKeepsQuotedSynopsisWhole(t *testing.T) {
	page := ManPage("muster", "muster.tools", nil, []Command{{Name: "send", Synopsis: `send <target> "body"`, Summary: "send"}})
	if !strings.Contains(page, `.B send <target> \(dqbody\(dq`) {
		t.Fatalf("synopsis quotes not escaped:\n%s", page)
	}
}

// help <cmd> leads with the command's one-line summary, then its long help.
func TestHelpForShowsSummary(t *testing.T) {
	var b strings.Builder
	HelpFor(&b, "muster", Command{Name: "send", Synopsis: "send <target>", Summary: "Send a message.", Help: "Long form."})
	s := b.String()
	if !strings.Contains(s, "Usage: muster send <target>\n\nSend a message.\n\nLong form.") {
		t.Fatalf("help %q lacks the summary before the long help", s)
	}
}
