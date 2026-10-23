package claudeplugin

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var galley = Spec{
	Name:        "galley",
	DisplayName: "galley",
	Bin:         "galley",
	ServerName:  "galley",
	Description: "Review documents in the browser; rounds land in the session that wrote them.",
	Author:      "Court Schuett",
	AuthorURL:   "https://galley.tools",
	Homepage:    "https://galley.tools",
	Repository:  "https://github.com/subaud-tools/galley",
	License:     "BUSL-1.1",
	Keywords:    []string{"review", "markdown"},
}

func TestRenderPluginManifest(t *testing.T) {
	files, err := Render(galley)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(files[".claude-plugin/plugin.json"], &m); err != nil {
		t.Fatal(err)
	}
	if m["name"] != "galley" || m["mcpServers"] != "./.mcp.json" || m["license"] != "BUSL-1.1" ||
		m["homepage"] != "https://galley.tools" || m["repository"] != "https://github.com/subaud-tools/galley" {
		t.Fatalf("plugin.json: %v", m)
	}
	if a, _ := m["author"].(map[string]any); a["name"] != "Court Schuett" || a["url"] != "https://galley.tools" {
		t.Fatalf("author: %v", m["author"])
	}
	if _, ok := m["version"]; ok {
		t.Fatal("plugin.json must not pin a version: Claude Code then versions the plugin by commit")
	}
}

func TestRenderMCPServerRunsChannel(t *testing.T) {
	s := galley
	s.ServerName, s.Bin = "casebook", "tackle-casebook"
	files, err := Render(s)
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(files[".mcp.json"], &m); err != nil {
		t.Fatal(err)
	}
	srv, ok := m.MCPServers["casebook"]
	if !ok || srv.Command != "tackle-casebook" || len(srv.Args) != 1 || srv.Args[0] != "channel" {
		t.Fatalf(".mcp.json: %s", files[".mcp.json"])
	}
}

func TestRenderHooksModule(t *testing.T) {
	s := galley
	s.Name, s.Bin = "my\"tool", "bin/with'quote"
	files, err := Render(s)
	if err != nil {
		t.Fatal(err)
	}
	var h struct {
		Modules []string `json:"modules"`
	}
	if err := json.Unmarshal(files["hooks/hooks.json"], &h); err != nil || len(h.Modules) != 1 || h.Modules[0] != "./register.js" {
		t.Fatalf("hooks.json: %s (%v)", files["hooks/hooks.json"], err)
	}
	js := string(files["hooks/register.js"])
	for _, want := range []string{`const NAME = "my\"tool"`, `const BIN = "bin/with'quote"`, `'watch'`, "CLAUDE_CODE_SESSION_ID", "turn.complete"} {
		if !strings.Contains(js, want) {
			t.Fatalf("register.js lacks %q", want)
		}
	}
	if strings.Contains(js, "{{") {
		t.Fatal("register.js has an unfilled template marker")
	}
	if node, err := exec.LookPath("node"); err == nil {
		cmd := exec.Command(node, "--check", "--input-type=module")
		cmd.Stdin = strings.NewReader(js)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("register.js is not valid JavaScript: %v\n%s", err, out)
		}
	}
}

func TestRenderRefusesIncompleteSpec(t *testing.T) {
	for _, s := range []Spec{{Bin: "b", ServerName: "s"}, {Name: "n", ServerName: "s"}, {Name: "n", Bin: "b"}} {
		if _, err := Render(s); err == nil {
			t.Fatalf("want an error for %+v", s)
		}
	}
}

// recorder is a testing.TB that keeps what AssertCurrent reports.
type recorder struct {
	testing.TB
	errs []string
}

func (r *recorder) Helper()                      {}
func (r *recorder) Name() string                 { return "TestPluginIsCurrent" }
func (r *recorder) Errorf(f string, args ...any) { r.errs = append(r.errs, fmt.Sprintf(f, args...)) }

func TestAssertCurrentPassesOnAFreshRender(t *testing.T) {
	t.Setenv(UpdateEnv, "")
	dir := t.TempDir()
	if err := Write(dir, galley); err != nil {
		t.Fatal(err)
	}
	r := &recorder{TB: t}
	AssertCurrent(r, dir, galley)
	if len(r.errs) != 0 {
		t.Fatalf("fresh render reported drift: %v", r.errs)
	}
}

func TestAssertCurrentCatchesDrift(t *testing.T) {
	t.Setenv(UpdateEnv, "")
	dir := t.TempDir()
	if err := Write(dir, galley); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "hooks", "register.js"), []byte("// edited by hand\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, ".mcp.json")); err != nil {
		t.Fatal(err)
	}
	r := &recorder{TB: t}
	AssertCurrent(r, dir, galley)
	all := strings.Join(r.errs, "\n")
	for _, want := range []string{"hooks/register.js", ".mcp.json", UpdateEnv + "=1 go test -run '^TestPluginIsCurrent$'"} {
		if !strings.Contains(all, want) {
			t.Fatalf("drift report lacks %q:\n%s", want, all)
		}
	}
}

func TestAssertCurrentRegeneratesWhenAsked(t *testing.T) {
	t.Setenv(UpdateEnv, "1")
	dir := t.TempDir()
	r := &recorder{TB: t}
	AssertCurrent(r, dir, galley)
	if len(r.errs) != 0 {
		t.Fatalf("update mode reported: %v", r.errs)
	}
	t.Setenv(UpdateEnv, "")
	AssertCurrent(r, dir, galley)
	if len(r.errs) != 0 {
		t.Fatalf("after update, still drift: %v", r.errs)
	}
}
