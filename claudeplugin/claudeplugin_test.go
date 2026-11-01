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
	// Watches is each watch's argv.
	Watches  [][]string `json:"watches"`
	Restarts int        `json:"restarts"`
	// Reports is each `BIN report ...` the mod ran, with its env.
	Reports []struct {
		Argv []string          `json:"argv"`
		Env  map[string]string `json:"env"`
	} `json:"reports"`
	Logs []string `json:"logs"`
	// Order is "start <event>" and "end <event>" for each report, as they
	// happened.
	Order []string `json:"order"`
}

// modOpts shapes runMod's fake Claude Code.
type modOpts struct {
	// Submit is the JavaScript value $.prompt.submit resolves with; "" is {}.
	Submit string
	// TurnComplete runs the turn.complete hook once the events are submitted.
	TurnComplete bool
	// ReportCode is the exit code of every `BIN report` the mod runs.
	ReportCode int
	// ReportMs is how long each `BIN report` takes to exit.
	ReportMs int
	// SubmitMs is how long $.prompt.submit takes to resolve: a submit made
	// mid-turn stays pending until the running turn completes.
	SubmitMs int
}

// runMod runs a rendered register.js under node with a fake mods API: each
// spawned watch prints out on stdout, then exits 0. It reports every
// submitted prompt, how many times watch was spawned and how many restarts
// were scheduled on $.clock.after. A $.clock.after callback due in under 2 s
// (shorter than any restart delay) runs on a real timer, and the harness waits
// for it and for every submit before turn.complete and before it reports; a
// restart is counted and not run. $.env.get finds nothing (plain Claude Code),
// which the 0.17.0 mod asks.
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
const run = { submitted: [], watches: [], spawns: 0, restarts: 0, reports: [], logs: [], order: [] }
const waits = []
const sleep = ms => new Promise(r => setTimeout(r, ms))
async function settle() {
  await sleep(200)
  for (let n = 0; n < waits.length; n = waits.length) await Promise.all(waits)
  await sleep(200)
}
const $ = {
  session: { id: async () => 'sid' },
  env: { get: async () => '' },
  process: { spawn: ({ argv, env }) => {
    const at = argv.indexOf('report')
    if (at > 0) {
      run.reports.push({ argv, env })
      const what = argv[at + 1]
      run.order.push('start ' + what)
      const result = new Promise(r => setTimeout(() => { run.order.push('end ' + what); r({ code: ` + strconv.Itoa(o.ReportCode) + ` }) }, ` + strconv.Itoa(o.ReportMs) + `))
      return { result, return() {}, async *[Symbol.asyncIterator]() { await result } }
    }
    run.spawns++
    run.watches.push(argv)
    return { result: Promise.resolve({ code: 0 }), return() {}, async *[Symbol.asyncIterator]() { yield { stream: 'stdout', text: out } } }
  } },
  prompt: { submit: ({ text }) => {
    run.submitted.push(text)
    const p = sleep(` + strconv.Itoa(o.SubmitMs) + `).then(() => (` + submit + `))
    waits.push(p)
    return p
  } },
  ui: { log(text) { run.logs.push(text) } },
  clock: { after(ms, fn) {
    if (ms >= 2000) {
      run.restarts++
      return
    }
    waits.push(sleep(ms).then(fn))
  } },
}
await hooks['session.start']($, {}, async e => e)
await settle()
if (` + strconv.FormatBool(o.TurnComplete) + `) {
  await hooks['turn.complete']($, {}, async e => e)
  await settle()
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

// TestNoGroupLeavesTheOldTemplateUnchanged: the 0.17.0 template, which knows
// nothing of groups, renders with only its name and binary filled in.
func TestNoGroupLeavesTheOldTemplateUnchanged(t *testing.T) {
	old, err := os.ReadFile(filepath.Join("testdata", "register-v0.17.0.js"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := renderJS(string(old), galley)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.NewReplacer("{{.NameJS}}", `"galley"`, "{{.BinJS}}", `"galley"`).Replace(string(old))
	if string(got) != want {
		t.Fatalf("0.17.0 template rendered differently:\n%s", got)
	}
}

// TestGroupGoesBeforeWatchAndReport: a tool with a group runs `BIN GROUP watch`
// and `BIN GROUP report ...`; one with none runs `BIN watch` and `BIN report ...`.
func TestGroupGoesBeforeWatchAndReport(t *testing.T) {
	for _, tc := range []struct {
		group string
		words []string
	}{
		{"", []string{galley.Bin}},
		{"pages", []string{galley.Bin, "pages"}},
	} {
		s := galley
		s.Group = tc.group
		files, err := Render(s)
		if err != nil {
			t.Fatal(err)
		}
		r := runModWith(t, files["hooks/register.js"], `{"text":"hi","report":"t7"}`+"\n", modOpts{TurnComplete: true})
		watch := append(slices.Clone(tc.words), "watch")
		if len(r.Watches) != 1 || !slices.Equal(r.Watches[0], watch) {
			t.Fatalf("group %q: watches %q, want %q", tc.group, r.Watches, watch)
		}
		landed := append(slices.Clone(tc.words), "report", "landed", "t7")
		ended := append(slices.Clone(tc.words), "report", "turn-ended")
		if len(r.Reports) != 2 || !slices.Equal(r.Reports[0].Argv, landed) || !slices.Equal(r.Reports[1].Argv, ended) {
			t.Fatalf("group %q: reports %+v, want %q then %q", tc.group, r.Reports, landed, ended)
		}
	}
}

// TestGroupChangesOnlyTheTwoArgvsAndTheLogs: taking the group's word out of a
// grouped mod gives the ungrouped mod, byte for byte: it appears in the two
// argvs and in the five log lines that name the command.
func TestGroupChangesOnlyTheTwoArgvsAndTheLogs(t *testing.T) {
	plain, err := Render(galley)
	if err != nil {
		t.Fatal(err)
	}
	s := galley
	s.Group = "pages"
	grouped, err := Render(s)
	if err != nil {
		t.Fatal(err)
	}
	js := string(grouped["hooks/register.js"])
	if n := strings.Count(js, `"pages", `); n != 2 {
		t.Fatalf("the group appears %d times, want 2", n)
	}
	if n := strings.Count(js, `" pages" + `); n != 5 {
		t.Fatalf("the group appears in %d log lines, want 5", n)
	}
	stripped := strings.ReplaceAll(strings.ReplaceAll(js, `"pages", `, ""), `" pages" + `, "")
	if stripped != string(plain["hooks/register.js"]) {
		t.Fatal("the group changed more of register.js than its two argvs and its logs")
	}
	for p, b := range plain {
		if p != "hooks/register.js" && !bytes.Equal(b, grouped[p]) {
			t.Fatalf("the group changed %s", p)
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

// TestModReportsTurnEndedOnTurnComplete: a turn.complete after one of this
// mod's events landed runs `BIN report turn-ended` in this session; the server
// works out what it ends.
func TestModReportsTurnEndedOnTurnComplete(t *testing.T) {
	files, err := Render(galley)
	if err != nil {
		t.Fatal(err)
	}
	r := runModWith(t, files["hooks/register.js"], `{"text":"hi","report":"t7"}`+"\n", modOpts{TurnComplete: true})
	if len(r.Reports) != 2 || strings.Join(r.Reports[1].Argv, " ") != galley.Bin+" report turn-ended" || r.Reports[1].Env["CLAUDE_CODE_SESSION_ID"] != "sid" {
		t.Fatalf("reports %+v", r.Reports)
	}
}

// TestModReportsOneAtATime: each report is its own process making its own
// call to the tool's server, so the mod runs them one after another; a
// turn-ended that overtook its turn's landed would end the wrong messages.
func TestModReportsOneAtATime(t *testing.T) {
	files, err := Render(galley)
	if err != nil {
		t.Fatal(err)
	}
	r := runModWith(t, files["hooks/register.js"], `{"text":"hi","report":"t7"}`+"\n", modOpts{TurnComplete: true, ReportMs: 300})
	want := []string{"start landed", "end landed", "start turn-ended"}
	if len(r.Order) < 3 || !slices.Equal(r.Order[:3], want) {
		t.Fatalf("order %q, want it to start with %q", r.Order, want)
	}
}

// TestModReportsNoTurnEndedWhenNothingLanded: a turn in which none of this
// mod's events landed runs nothing, so seven family plugins don't start seven
// binaries at the end of every turn.
func TestModReportsNoTurnEndedWhenNothingLanded(t *testing.T) {
	files, err := Render(galley)
	if err != nil {
		t.Fatal(err)
	}
	r := runModWith(t, files["hooks/register.js"], `{"text":"hi"}`+"\n", modOpts{TurnComplete: true})
	if len(r.Reports) != 0 {
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

// TestModReportsHeldWhileTheSubmitIsPending: a submit still pending after a
// second means the agent is mid-turn; the mod runs `BIN report held` before
// that line's landed, and turn-ended after both.
func TestModReportsHeldWhileTheSubmitIsPending(t *testing.T) {
	files, err := Render(galley)
	if err != nil {
		t.Fatal(err)
	}
	r := runModWith(t, files["hooks/register.js"], `{"text":"hi","report":"t7"}`+"\n", modOpts{SubmitMs: 1500, TurnComplete: true})
	want := []string{galley.Bin + " report held t7", galley.Bin + " report landed t7", galley.Bin + " report turn-ended"}
	var got []string
	for _, rep := range r.Reports {
		got = append(got, strings.Join(rep.Argv, " "))
		if rep.Env["CLAUDE_CODE_SESSION_ID"] != "sid" {
			t.Fatalf("report %q ran without the session: %+v", rep.Argv, rep.Env)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("reports %q, want %q", got, want)
	}
}

// TestModReportsNoHeldForAPromptThatStartsAtOnce: an idle session's submit
// resolves at once, so only landed runs; and a line with no token (a tool not
// on the lifecycle) reports nothing however long its submit waits.
func TestModReportsNoHeldForAPromptThatStartsAtOnce(t *testing.T) {
	files, err := Render(galley)
	if err != nil {
		t.Fatal(err)
	}
	fast := runModWith(t, files["hooks/register.js"], `{"text":"hi","report":"t7"}`+"\n", modOpts{})
	if len(fast.Reports) != 1 || strings.Join(fast.Reports[0].Argv, " ") != galley.Bin+" report landed t7" {
		t.Fatalf("an idle session's prompt reported %+v", fast.Reports)
	}
	plain := runModWith(t, files["hooks/register.js"], `{"text":"hi"}`+"\n", modOpts{SubmitMs: 1500})
	if len(plain.Reports) != 0 {
		t.Fatalf("a line with no token reported %+v", plain.Reports)
	}
}
