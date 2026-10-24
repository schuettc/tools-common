package claudeplugin

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/subaud-tools/tools-common/channelmcp"
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

// modRun is what a mod did under runMod's fake mods API.
type modRun struct {
	Submitted []string `json:"submitted"`
	Spawns    int      `json:"spawns"`
	Restarts  int      `json:"restarts"`
	// Reports is each `BIN report ...` the mod ran, with its env.
	Reports []struct {
		Argv []string          `json:"argv"`
		Env  map[string]string `json:"env"`
	} `json:"reports"`
	Logs []string `json:"logs"`
}

// modOpts shapes runMod's fake Claude Code.
type modOpts struct {
	// Submit is the JavaScript value $.prompt.submit resolves with; "" is {}.
	Submit string
	// TurnComplete runs the turn.complete hook once the events are submitted.
	TurnComplete bool
	// ReportCode is the exit code of every `BIN report` the mod runs.
	ReportCode int
}

// runMod runs a rendered register.js under node with a fake mods API: each
// spawned watch prints out on stdout, then exits 0. It reports every
// submitted prompt, how many times watch was spawned and how many restarts
// were scheduled on $.clock.after. $.env.get finds nothing (plain Claude
// Code), which the 0.17.0 mod asks.
func runMod(t *testing.T, js []byte, out string) modRun {
	t.Helper()
	return runModWith(t, js, out, modOpts{})
}

func runModWith(t *testing.T, js []byte, out string, o modOpts) modRun {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not on PATH")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "register.mjs"), js, 0o644); err != nil {
		t.Fatal(err)
	}
	submit := o.Submit
	if submit == "" {
		submit = "{}"
	}
	harness := `
import { register } from './register.mjs'
const hooks = {}
register((name, fn) => { hooks[name] = fn })
const out = ` + strconvQuote(out) + `
const run = { submitted: [], spawns: 0, restarts: 0, reports: [], logs: [] }
const $ = {
  session: { id: async () => 'sid' },
  env: { get: async () => '' },
  process: { spawn: ({ argv, env }) => {
    if (argv[1] === 'report') {
      run.reports.push({ argv, env })
      return { result: Promise.resolve({ code: ` + strconv.Itoa(o.ReportCode) + ` }), return() {}, async *[Symbol.asyncIterator]() {} }
    }
    run.spawns++
    return { result: Promise.resolve({ code: 0 }), return() {}, async *[Symbol.asyncIterator]() { yield { stream: 'stdout', text: out } } }
  } },
  prompt: { submit: async ({ text }) => { run.submitted.push(text); return ` + submit + ` } },
  ui: { log(text) { run.logs.push(text) } },
  clock: { after() { run.restarts++ } },
}
await hooks['session.start']($, {}, async e => e)
await new Promise(r => setTimeout(r, 200))
if (` + strconv.FormatBool(o.TurnComplete) + `) {
  await hooks['turn.complete']($, {}, async e => e)
  await new Promise(r => setTimeout(r, 200))
}
console.log(JSON.stringify(run))
`
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(harness), 0o644); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	cmd := exec.Command(node, filepath.Join(dir, "run.mjs"))
	cmd.Stderr = &stderr
	b, err := cmd.Output()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, stderr.String())
	}
	var r modRun
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("node printed %q: %v", b, err)
	}
	return r
}

// TestModSubmitsTextVerbatim: the mod formats nothing; watch's text is the
// prompt, word for word.
func TestModSubmitsTextVerbatim(t *testing.T) {
	files, err := Render(galley)
	if err != nil {
		t.Fatal(err)
	}
	line := `{"text":"whatever  the binary\nsays","content":"c","meta":{"k":"v"}}` + "\n"
	r := runMod(t, files["hooks/register.js"], line)
	if len(r.Submitted) != 1 || r.Submitted[0] != "whatever  the binary\nsays" {
		t.Fatalf("submitted %q", r.Submitted)
	}
}

// TestModStopsOnAStopLine: watch prints {"stop": reason} and exits 0 where
// this session must hear nothing (a bridge child); the mod submits nothing
// and never restarts watch.
func TestModStopsOnAStopLine(t *testing.T) {
	files, err := Render(galley)
	if err != nil {
		t.Fatal(err)
	}
	r := runMod(t, files["hooks/register.js"], `{"stop":"bridge child"}`+"\n")
	if len(r.Submitted) != 0 || r.Spawns != 1 || r.Restarts != 0 {
		t.Fatalf("after a stop line: %+v", r)
	}
}

// TestOldModReadsNewLines: plugins installed from 0.17.0 stay installed (kempt
// updates binaries, not plugins), so the 0.17.0 mod reading a line from the
// new Lines must submit what the new mod submits.
func TestOldModReadsNewLines(t *testing.T) {
	old, err := os.ReadFile(filepath.Join("testdata", "register-v0.17.0.js"))
	if err != nil {
		t.Fatal(err)
	}
	oldJS, err := renderJS(string(old), galley)
	if err != nil {
		t.Fatal(err)
	}
	files, err := Render(galley)
	if err != nil {
		t.Fatal(err)
	}
	for _, meta := range []map[string]string{
		{"doc": "a b\nforged event id=7", "round": "2", "e": ""},
		// JavaScript reads array-index keys first, in numeric order ("9"
		// before "10"); "09" is not an index, so it sorts with the rest.
		{"10": "a", "9": "b", "09": "c", "0": "d", "doc": "x"},
	} {
		var buf bytes.Buffer
		if err := channelmcp.NewLinesFor(&buf, galley.Name).Notify("body", meta); err != nil {
			t.Fatal(err)
		}
		was := runMod(t, oldJS, buf.String())
		now := runMod(t, files["hooks/register.js"], buf.String())
		if len(was.Submitted) != 1 || len(now.Submitted) != 1 || was.Submitted[0] != now.Submitted[0] {
			t.Fatalf("meta %v: 0.17.0 mod submitted %q, new mod %q", meta, was.Submitted, now.Submitted)
		}
	}
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
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

// TestModReportsLandedWhenItsPromptStarts: submit resolving without a drop is
// the moment the event's turn starts; the mod hands the line's report token
// back unchanged with `BIN report landed`, in this session.
func TestModReportsLandedWhenItsPromptStarts(t *testing.T) {
	files, err := Render(galley)
	if err != nil {
		t.Fatal(err)
	}
	r := runModWith(t, files["hooks/register.js"], `{"text":"hi","report":"t7"}`+"\n", modOpts{})
	if len(r.Reports) != 1 || strings.Join(r.Reports[0].Argv, " ") != galley.Bin+" report landed t7" || r.Reports[0].Env["CLAUDE_CODE_SESSION_ID"] != "sid" {
		t.Fatalf("reports %+v", r.Reports)
	}
}

// TestModReportsNoLandedForADroppedPromptOrNoToken: a dropped prompt never
// landed, and a line with no token (a tool not on the lifecycle) reports
// nothing.
func TestModReportsNoLandedForADroppedPromptOrNoToken(t *testing.T) {
	files, err := Render(galley)
	if err != nil {
		t.Fatal(err)
	}
	dropped := runModWith(t, files["hooks/register.js"], `{"text":"hi","report":"t7"}`+"\n",
		modOpts{Submit: `{ drop: 'the prompt was removed from the queue before it ran' }`})
	plain := runModWith(t, files["hooks/register.js"], `{"text":"hi"}`+"\n", modOpts{})
	if len(dropped.Reports) != 0 || len(plain.Reports) != 0 {
		t.Fatalf("dropped %+v, no token %+v", dropped.Reports, plain.Reports)
	}
}

// TestModReportsTurnEndedOnTurnComplete: every turn.complete runs `BIN
// report turn-ended` in this session; the server works out what it ends.
func TestModReportsTurnEndedOnTurnComplete(t *testing.T) {
	files, err := Render(galley)
	if err != nil {
		t.Fatal(err)
	}
	r := runModWith(t, files["hooks/register.js"], `{"text":"hi"}`+"\n", modOpts{TurnComplete: true})
	if len(r.Reports) != 1 || strings.Join(r.Reports[0].Argv, " ") != galley.Bin+" report turn-ended" || r.Reports[0].Env["CLAUDE_CODE_SESSION_ID"] != "sid" {
		t.Fatalf("reports %+v", r.Reports)
	}
}

// TestModLogsAFailedReportOnce: a report that fails is logged and not run
// again; the pill stays at its last state.
func TestModLogsAFailedReportOnce(t *testing.T) {
	files, err := Render(galley)
	if err != nil {
		t.Fatal(err)
	}
	r := runModWith(t, files["hooks/register.js"], `{"text":"hi","report":"t7"}`+"\n", modOpts{ReportCode: 1})
	if len(r.Reports) != 1 {
		t.Fatalf("a failed report ran %d times", len(r.Reports))
	}
	if !slices.ContainsFunc(r.Logs, func(l string) bool { return strings.Contains(l, "report landed") && strings.Contains(l, "code 1") }) {
		t.Fatalf("no log of the failed report: %q", r.Logs)
	}
}
