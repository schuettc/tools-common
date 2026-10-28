package mermaidkit

import (
	"os"
	"strings"
	"sync"
	"testing"
)

// broken is the diagram diazo's parser test was written for (round 3 of the
// delete-a-plan folder): the ";" in a message ends the statement, so mermaid
// will not parse it.
const broken = "sequenceDiagram\n  participant C as Court (page)\n  participant D as diazo serve\n  participant O as old session\n  participant N as new session\n  C->>D: ⋯ Hand to a new session\n  D->>N: start in the plan's project: take over folder X\n  N->>D: diazo_open X\n  D->>D: owner = new session; unanswered messages go to it\n  D-->>O: the plan moved to another session\n  O->>D: diazo_update X\n  D-->>O: refused: not the owner\n  C->>D: next press\n  D->>N: delivered"

func fake(t *testing.T) *Parser {
	t.Helper()
	b, err := os.ReadFile("testdata/fake-mermaid.js")
	if err != nil {
		t.Fatal(err)
	}
	return NewParser(Bundle{JS: b, Global: "fakeMermaid"})
}

// checkBroken is diazo's check: mermaid's reason, cut to its first two
// lines, and nil once the diagram is fixed.
func checkBroken(t *testing.T, p *Parser) {
	t.Helper()
	err := p.Check(broken)
	if err == nil || !strings.Contains(err.Error(), "Parse error on line 9") {
		t.Fatalf("broken diagram: %v, want mermaid's parse error on line 9", err)
	}
	if n := strings.Count(err.Error(), "\n"); n > 1 {
		t.Fatalf("error has %d lines, want mermaid's first two: %q", n+1, err)
	}
	if err := p.Check(strings.ReplaceAll(broken, ";", ",")); err != nil {
		t.Fatalf("fixed diagram: %v", err)
	}
}

func TestCheck(t *testing.T) { checkBroken(t, fake(t)) }

// The real thing, run against a tool's built bundle:
// MERMAIDKIT_BUNDLE=../../diazo/internal/serve/assets/mermaid.js MERMAIDKIT_GLOBAL=diazoMermaid.
func TestCheckWithARealBundle(t *testing.T) {
	path := os.Getenv("MERMAIDKIT_BUNDLE")
	if path == "" {
		t.Skip("set MERMAIDKIT_BUNDLE (and MERMAIDKIT_GLOBAL) to a tool's built mermaid.js")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	checkBroken(t, NewParser(Bundle{JS: b, Global: os.Getenv("MERMAIDKIT_GLOBAL")}))
}

func TestCheckReasonFromAThrow(t *testing.T) {
	err := fake(t).Check("throw")
	if err == nil || !strings.Contains(err.Error(), "thrown at once") || strings.Contains(err.Error(), "line three") {
		t.Fatalf("got %v, want the throw's first two lines", err)
	}
}

func TestCheckAPromiseThatNeverSettles(t *testing.T) {
	if err := fake(t).Check("hang"); err == nil || !strings.Contains(err.Error(), "did not finish") {
		t.Fatalf("got %v", err)
	}
}

func TestABundleWithoutTheGlobalFailsEveryCheck(t *testing.T) {
	b, _ := os.ReadFile("testdata/fake-mermaid.js")
	for _, tc := range []struct{ js, global, want string }{
		{string(b), "otherMermaid", "defines no otherMermaid.default"},
		{"var m = {default: {}};", "m", "has no m.default.parse"},
		{"syntax error (", "m", "loading the bundle"},
	} {
		p := NewParser(Bundle{JS: []byte(tc.js), Global: tc.global})
		for range 2 {
			if err := p.Check("graph TD"); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%s: got %v, want %q", tc.global, err, tc.want)
			}
		}
	}
}

// One runtime, taking turns: concurrent checks give each its own answer.
func TestConcurrentChecks(t *testing.T) {
	p := fake(t)
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			text, bad := "graph TD", i%2 == 0
			if bad {
				text = broken
			}
			if err := p.Check(text); (err != nil) != bad {
				t.Errorf("check %d: %v", i, err)
			}
		})
	}
	wg.Wait()
}
