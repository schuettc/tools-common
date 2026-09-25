// Package harness captures a coding-agent harness session's identity from
// the process environment or a hook's stdin payload. It is the family's one
// resolution rule for "which session is this?"; callers must not read the
// underlying variables directly.
//
// Resolution rule (settled 2026-09-24):
//  1. AGENT_SESSION_CHILD=1 with a non-empty AGENT_SESSION_ID: the agent id.
//     Only a parent that runs this process as part of its own session sets
//     the marker (pi-claude-bridge, next to the id it stamps).
//  2. else CLAUDE_CODE_SESSION_ID (or a hook payload's session_id).
//  3. else AGENT_SESSION_ID.
//  4. else no session ("").
//
// Both ids are set together in two situations that need opposite answers: a
// bridge child (belongs to the outer pi session; the bridge declares it with
// the marker) and a Claude session launched from inside pi (inherits the pi
// id but is a new conversation; no marker). Ownership is declared, never
// guessed.
package harness

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const (
	envClaude = "CLAUDE_CODE_SESSION_ID"
	envAgent  = "AGENT_SESSION_ID"
	envChild  = "AGENT_SESSION_CHILD"
)

// Capture holds the identity fields available to a session.
type Capture struct {
	// SessionID is the resolved session id (see the package rule); "" when
	// not running under a known harness.
	SessionID string
	// ClaudeID is the harness-native id: CLAUDE_CODE_SESSION_ID, or a hook
	// payload's session_id (Claude Code and Codex both send one). Raw.
	ClaudeID string
	// AgentID is AGENT_SESSION_ID (pi, via pi-quiet; or stamped by a parent). Raw.
	AgentID string
	// Child reports AGENT_SESSION_CHILD=1 with a non-empty AgentID: this
	// process belongs to the AgentID session.
	Child bool
	// CWD is the session's working directory, the raw material for Alias and
	// Project derivation.
	CWD string
	// TranscriptPath is the conversation's transcript file, when a hook
	// payload provided one. Payload-only.
	TranscriptPath string
}

// resolve applies the package resolution rule.
func resolve(claude, agent string, child bool) string {
	if child && agent != "" {
		return agent
	}
	if claude != "" {
		return claude
	}
	return agent
}

// FromEnv captures identity from the process environment and the working
// directory, resolving SessionID by the package rule.
func FromEnv() Capture {
	cwd, _ := os.Getwd()
	c := Capture{ClaudeID: os.Getenv(envClaude), AgentID: os.Getenv(envAgent), CWD: cwd}
	c.Child = os.Getenv(envChild) == "1" && c.AgentID != ""
	c.SessionID = resolve(c.ClaudeID, c.AgentID, c.Child)
	return c
}

// FromHookPayload captures identity from a hook's stdin payload
// ({"session_id", "cwd", "transcript_path"}), falling back to FromEnv for any
// field the payload doesn't carry. The payload's session_id is the
// harness-native id (ClaudeID). Invalid or empty JSON is tolerated: hooks
// must never fail on input shape.
func FromHookPayload(payload []byte) Capture {
	var p struct {
		SessionID      string `json:"session_id"`
		CWD            string `json:"cwd"`
		TranscriptPath string `json:"transcript_path"`
	}
	_ = json.Unmarshal(payload, &p)
	c := FromEnv()
	if p.SessionID != "" {
		c.ClaudeID = p.SessionID
	}
	if p.CWD != "" {
		c.CWD = p.CWD
	}
	if p.TranscriptPath != "" {
		c.TranscriptPath = p.TranscriptPath
	}
	c.SessionID = resolve(c.ClaudeID, c.AgentID, c.Child)
	return c
}

// Alias derives the fallback agent alias from the working directory — its
// basename, mirroring how the operator's per-project tmux sessions are named
// after their directory. "" when there is no usable directory ($MUSTER_ALIAS
// and explicit arguments always take precedence over this in callers).
func (c Capture) Alias() string {
	base := filepath.Base(c.CWD)
	if base == "." || base == string(filepath.Separator) || base == "" {
		return ""
	}
	return base
}

// Project derives the project name by walking up from CWD to the enclosing
// git checkout. For a linked worktree (.git is a pointer file, "gitdir:
// <main>/.git/worktrees/<name>"), the project is the MAIN checkout's
// directory name, so every worktree of one repo shares its project — the
// same grouping the per-project tmux sockets ("proj-<project>") give
// pane-anchored agents. "" when CWD is not inside a git checkout.
func (c Capture) Project() string {
	dir := c.CWD
	for dir != "" {
		gitPath := filepath.Join(dir, ".git")
		if fi, err := os.Stat(gitPath); err == nil {
			if !fi.IsDir() {
				if main := mainCheckoutFromGitFile(gitPath); main != "" {
					return filepath.Base(main)
				}
			}
			return filepath.Base(dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

// mainCheckoutFromGitFile resolves a linked worktree's .git pointer file to
// the main checkout's path: "gitdir: /main/.git/worktrees/<name>" → "/main".
// "" when the file doesn't parse as a worktree pointer.
func mainCheckoutFromGitFile(gitPath string) string {
	b, err := os.ReadFile(gitPath)
	if err != nil {
		return ""
	}
	gd := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(b)), "gitdir:"))
	sep := string(filepath.Separator)
	if i := strings.Index(gd, sep+".git"+sep); i > 0 {
		return gd[:i]
	}
	return ""
}

// CustomTitle returns the conversation's user-set name: the customTitle of
// the LAST {"type":"custom-title"} record in the transcript at path. The
// record is written by an explicit naming gesture (/rename, `claude --name`,
// or muster's own prefix-T injection) and re-emitted through the file, so
// its presence is proof of intent — the signal the statusline's merged
// session_name field cannot provide (spec §2, verified 2026-08-05). Returns
// "" for an empty path, unreadable file, or a transcript with no record:
// callers treat "" as "no user-set name", never as an error — a hook must
// not fail on transcript shape.
func CustomTitle(transcriptPath string) string {
	if transcriptPath == "" {
		return ""
	}
	f, err := os.Open(transcriptPath)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	var title string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 10*1024*1024) // transcript lines can be huge (tool results)
	for sc.Scan() {
		line := sc.Bytes()
		if !bytes.Contains(line, []byte(`"custom-title"`)) {
			continue // cheap pre-filter; the unmarshal below is the authority
		}
		var rec struct {
			Type        string `json:"type"`
			CustomTitle string `json:"customTitle"`
		}
		if json.Unmarshal(line, &rec) == nil && rec.Type == "custom-title" && rec.CustomTitle != "" {
			title = rec.CustomTitle
		}
	}
	return title
}

// IsTeammate reports whether the transcript at path belongs to a fleet
// TEAMMATE session — a member spawned into a pane of some primary's tmux
// session. Members' transcripts carry a top-level teamName (with
// agentName) on their records from the first few lines; team LEADS and
// plain primaries never do (verified over 46 transcripts, 2026-08-06 —
// see the teammate-identity-refusal spec §2; a bare agent-name record
// does NOT discriminate, /rename'd primaries have those). The scan is
// bounded to the first 30 lines: the signal sits at the top by
// construction, and an unbounded scan would false-positive on
// conversation text that quotes transcripts. Empty path, unreadable
// file, or no match read as NOT a teammate — hooks fail open, they never
// block a session.
func IsTeammate(transcriptPath string) bool {
	if transcriptPath == "" {
		return false
	}
	f, err := os.Open(transcriptPath)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)
	for i := 0; i < 30 && sc.Scan(); i++ {
		line := sc.Bytes()
		if !bytes.Contains(line, []byte(`"teamName"`)) {
			continue
		}
		var rec struct {
			TeamName string `json:"teamName"`
		}
		if json.Unmarshal(line, &rec) == nil && rec.TeamName != "" {
			return true
		}
	}
	return false
}
