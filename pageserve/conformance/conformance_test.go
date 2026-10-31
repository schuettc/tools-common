package conformance

import (
	"regexp"
	"testing"
	"time"
)

func scratchServer(t *testing.T) Server {
	t.Helper()
	return Server{Tool: "scratch", Binary: Build(t, "./testdata/scratch"), Args: []string{"serve"}, Channel: true, Quiet: time.Second}
}

// A server built only from pageserve passes, with no code of its own for the
// rules.
func TestAServerBuiltOnlyFromPageservePasses(t *testing.T) {
	Check(t, scratchServer(t))
}

// Each rule fails on a server that breaks it, which proves the check can fail.
func TestEachRuleFailsOnAServerThatBreaksIt(t *testing.T) {
	was := staleWait
	staleWait = 2 * time.Second
	t.Cleanup(func() { staleWait = was })
	s := scratchServer(t)
	for _, tc := range []struct {
		rule string
		want string // in the failure
	}{
		{"token", "changed across a restart"},
		{"idle", "exited"},
		{"stale", "never marked stale"},
		{"tabs", "tabs.json"},
		{"build", "X-Tool-Build"},
		{"skills", "skills install"},
	} {
		t.Run(tc.rule, func(t *testing.T) {
			broken := s
			broken.Env = []string{"SCRATCH_BREAK=" + tc.rule}
			err := runRule(t, broken, tc.rule)
			if err == nil {
				t.Fatal("the rule passed on a server that breaks it")
			}
			if !regexp.MustCompile(tc.want).MatchString(err.Error()) {
				t.Fatalf("failed for another reason: %v", err)
			}
		})
	}
}

// A tool with no channel has no skill to ship.
func TestAToolWithNoChannelNeedsNoSkillsCommand(t *testing.T) {
	s := scratchServer(t)
	s.Channel = false
	s.Env = []string{"SCRATCH_BREAK=skills"}
	if err := runRule(t, s, "skills"); err != nil {
		t.Fatal(err)
	}
}
