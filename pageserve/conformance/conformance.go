// Package conformance tests a tool's server against the server rules in
// PAGE-STANDARD.md, from outside, as `claudeplugin.AssertCurrent` and
// `wiregen.CheckCommitted` do for their contracts: a tool's own CI runs it
// against its own binary, so a server that drifts from a rule fails its own
// build.
//
//	func TestServerConforms(t *testing.T) {
//		conformance.Check(t, conformance.Server{
//			Tool: "cull", Binary: conformance.Build(t, "../../cmd/cull"),
//			Args: []string{"serve", "--foreground", "--no-open"}, Channel: true,
//		})
//	}
//
// Every rule starts the binary fresh in a scratch state directory ($<TOOL>_HOME)
// and a scratch $HOME, from a copy it can replace.
package conformance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	tools "github.com/subaud-tools/tools-common"
	"github.com/subaud-tools/tools-common/localweb"
	"github.com/subaud-tools/tools-common/pageserve"
)

// Server tells Check how to start a tool's serve.
type Server struct {
	Tool   string   // the tool's name, as its Live names it
	Binary string   // the built tool (see Build)
	Args   []string // run serve in the foreground, opening no browser
	Env    []string // added to the serve's environment: a tool's own short test clock
	// Channel: the tool has a channel, so it must ship a skill.
	Channel bool
	// SkillName names the skill when it isn't named after the tool. "": Tool.
	SkillName string
	// Quiet is how long serve must stay up with no request at all (0: 3s).
	// Nothing in pageserve exits for being idle: a tool that adds a timer of its
	// own is caught only when Env shortens it to under this.
	Quiet time.Duration
}

// staleWait is how long the stale mark may take after an install; tests
// shorten it.
var staleWait = 10 * time.Second

// startWait is how long serve may take to write its entry, or to stop.
const startWait = 10 * time.Second

var rules = []struct {
	name string
	run  func(*testing.T, *scratch) error
}{
	{"token", keepsToken},
	{"idle", staysUp},
	{"stale", marksStale},
	{"tabs", keepsTabs},
	{"build", saysBuild},
	{"skills", shipsSkill},
	{"stop", stopTakesHeader},
}

// Check asserts every server rule against s, one subtest per rule.
func Check(t *testing.T, s Server) {
	t.Helper()
	for _, r := range rules {
		t.Run(r.name, func(t *testing.T) {
			if err := runRule(t, s, r.name); err != nil {
				t.Error(err)
			}
		})
	}
}

// Build compiles the tool at pkg (a path from the test's directory) and returns
// the binary, in a folder that goes with the test.
func Build(t *testing.T, pkg string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "tool")
	if out, err := exec.Command("go", "build", "-o", bin, pkg).CombinedOutput(); err != nil {
		t.Fatalf("go build %s: %v\n%s", pkg, err, out)
	}
	return bin
}

func runRule(t *testing.T, s Server, name string) error {
	t.Helper()
	for _, r := range rules {
		if r.name == name {
			return r.run(t, newScratch(t, s))
		}
	}
	return fmt.Errorf("no rule %q", name)
}

// scratch is one rule's scratch directories and the copy of the binary it runs.
type scratch struct {
	s    Server
	exe  string
	home string // the serve's $HOME
	live pageserve.Live
	proc *process
}

func newScratch(t *testing.T, s Server) *scratch {
	t.Helper()
	dir := t.TempDir()
	t.Setenv(strings.ToUpper(strings.ReplaceAll(s.Tool, "-", "_"))+"_HOME", filepath.Join(dir, "tool"))
	home := filepath.Join(dir, "home")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	c := &scratch{s: s, exe: filepath.Join(dir, "bin", filepath.Base(s.Binary)), home: home, live: pageserve.LiveFor(s.Tool)}
	b, err := os.ReadFile(s.Binary)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(c.exe), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(c.exe, b, 0o700); err != nil { //nolint:gosec // G306: it runs
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if c.proc != nil {
			_ = c.proc.cmd.Process.Kill()
			<-c.proc.exited
		}
	})
	return c
}

// env is the environment for the tool: its scratch home, with no harness
// directory of the machine's to install into.
func (c *scratch) env() []string {
	env := append(os.Environ(), "HOME="+c.home,
		"CLAUDE_CONFIG_DIR="+filepath.Join(c.home, ".claude"),
		"CODEX_HOME="+filepath.Join(c.home, ".codex"),
		"PI_CODING_AGENT_DIR="+filepath.Join(c.home, ".pi", "agent"))
	return append(env, c.s.Env...)
}

// process is a running serve.
type process struct {
	cmd    *exec.Cmd
	exited chan struct{}
	mu     sync.Mutex
	output strings.Builder
}

func (p *process) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.output.Write(b)
}

func (p *process) log() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return strings.TrimSpace(p.output.String())
}

// start runs serve and waits for its entry.
func (c *scratch) start() (pageserve.Entry, error) {
	cmd := exec.Command(c.exe, c.s.Args...)
	cmd.Env = c.env()
	p := &process{cmd: cmd, exited: make(chan struct{})}
	cmd.Stdout, cmd.Stderr = p, p
	if err := cmd.Start(); err != nil {
		return pageserve.Entry{}, err
	}
	c.proc = p
	go func() { _ = cmd.Wait(); close(p.exited) }()
	deadline := time.Now().Add(startWait)
	for time.Now().Before(deadline) {
		select {
		case <-p.exited:
			return pageserve.Entry{}, fmt.Errorf("serve exited as it started: %s", p.log())
		case <-time.After(20 * time.Millisecond):
		}
		if e, err := c.live.Running(); err == nil {
			return e, nil
		}
	}
	return pageserve.Entry{}, fmt.Errorf("serve wrote no entry in %v: %s", startWait, p.log())
}

// stop asks serve to stop, the way a tool's channel does, and waits for it.
func (c *scratch) stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), startWait)
	defer cancel()
	if err := pageserve.Stop(ctx, c.live); err != nil {
		return err
	}
	select {
	case <-c.proc.exited:
		return nil
	case <-ctx.Done():
		return errors.New("serve's entry went but the process did not exit")
	}
}

// answer is what a call got back.
type answer struct {
	status int
	build  string // the build header
	body   []byte
}

// call is one GET with a token.
func call(e pageserve.Entry, token, path string) (answer, error) {
	req, err := http.NewRequest(http.MethodGet, e.Base+path, nil)
	if err != nil {
		return answer{}, err
	}
	req.Header.Set(localweb.TokenHeader, token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return answer{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	return answer{resp.StatusCode, resp.Header.Get(pageserve.BuildHeader), b}, err
}

// keepsToken: the token survives a restart, so an open tab keeps working.
func keepsToken(_ *testing.T, c *scratch) error {
	first, err := c.start()
	if err != nil {
		return err
	}
	if err := c.stop(); err != nil {
		return err
	}
	second, err := c.start()
	if err != nil {
		return err
	}
	if second.Token != first.Token {
		return errors.New("the token changed across a restart: open tabs would stop working")
	}
	got, err := call(second, first.Token, "/api/poll")
	if err != nil {
		return err
	}
	if got.status != http.StatusOK {
		return fmt.Errorf("the token from before the restart got %d", got.status)
	}
	return c.stop()
}

// staysUp: serve doesn't exit for having nothing to do.
func staysUp(_ *testing.T, c *scratch) error {
	if _, err := c.start(); err != nil {
		return err
	}
	quiet := c.s.Quiet
	if quiet <= 0 {
		quiet = 3 * time.Second
	}
	select {
	case <-c.proc.exited:
		return fmt.Errorf("serve exited after less than %v with no request: %s", quiet, c.proc.log())
	case <-time.After(quiet):
	}
	return c.stop()
}

// marksStale: an install over the binary sets the entry's stale, which the next
// tool call acts on, and tells the page.
func marksStale(_ *testing.T, c *scratch) error {
	e, err := c.start()
	if err != nil {
		return err
	}
	if e.Stale {
		return errors.New("marked stale before any install")
	}
	b, err := os.ReadFile(c.exe)
	if err != nil {
		return err
	}
	// As an install does: a new file renamed over the running one.
	if err := os.WriteFile(c.exe+".new", append(b, 0), 0o700); err != nil { //nolint:gosec // G306: it runs
		return err
	}
	if err := os.Rename(c.exe+".new", c.exe); err != nil {
		return err
	}
	deadline := time.Now().Add(staleWait)
	for {
		if now, err := c.live.Running(); err == nil && now.Stale {
			break
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("the entry was never marked stale in the %v after the binary was replaced", staleWait)
		}
		time.Sleep(20 * time.Millisecond)
	}
	// The entry is written before the event goes out.
	for {
		a, err := call(e, e.Token, "/api/poll")
		if err != nil {
			return err
		}
		var got struct {
			Events []struct {
				Type string `json:"type"`
			} `json:"events"`
		}
		if err := json.Unmarshal(a.body, &got); err != nil {
			return err
		}
		for _, ev := range got.Events {
			if ev.Type == "stale" {
				return c.stop()
			}
		}
		if time.Now().After(deadline) {
			return errors.New("no stale event on /api/poll: an open page would not hear of the install")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// keepsTabs: a poll that names its tab checks it in, and the list outlives a
// restart, so the next page open reuses that tab. From outside the list is its
// file: a check-in after the restart writes it again, and the first tab must
// still be there.
func keepsTabs(_ *testing.T, c *scratch) error {
	e, err := c.start()
	if err != nil {
		return err
	}
	const first, second = "conformance-first", "conformance-second"
	if _, err := call(e, e.Token, "/api/poll?tab="+first+"&page=p"); err != nil {
		return err
	}
	path := filepath.Join(tools.StateDir(c.s.Tool), "tabs.json")
	has := func(tab string) (bool, error) {
		b, err := os.ReadFile(path)
		if err != nil {
			return false, fmt.Errorf("the open tabs: %w", err)
		}
		return strings.Contains(string(b), `"`+tab+`"`), nil
	}
	if ok, err := has(first); err != nil {
		return err
	} else if !ok {
		return errors.New("a poll that named its tab was not checked in to tabs.json")
	}
	if err := c.stop(); err != nil {
		return err
	}
	if e, err = c.start(); err != nil {
		return err
	}
	if _, err := call(e, e.Token, "/api/poll?tab="+second+"&page=p"); err != nil {
		return err
	}
	if ok, err := has(second); err != nil {
		return err
	} else if !ok {
		return errors.New("a poll after the restart was not checked in to tabs.json")
	}
	if ok, err := has(first); err != nil {
		return err
	} else if !ok {
		return errors.New("tabs.json lost the tab from before the restart: the next page open would open another")
	}
	return c.stop()
}

var buildForm = regexp.MustCompile(`^.*\+[0-9a-f]{12}$`)

// saysBuild: every API response says which build is serving.
func saysBuild(_ *testing.T, c *scratch) error {
	e, err := c.start()
	if err != nil {
		return err
	}
	for _, path := range []string{"/api/poll", "/api/conformance-no-such-route"} {
		got, err := call(e, e.Token, path)
		if err != nil {
			return err
		}
		if !buildForm.MatchString(got.build) {
			return fmt.Errorf("%s: %s is %q, want <version>+<hash>", path, pageserve.BuildHeader, got.build)
		}
	}
	return c.stop()
}

// stopTakesHeader: POST /api/stop takes the token header only, so a page served
// by the tool, which holds the cookie, can't stop its server. It sends the
// page's own Origin, past localweb's cross-origin guard.
func stopTakesHeader(_ *testing.T, c *scratch) error {
	e, err := c.start()
	if err != nil {
		return err
	}
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	first, err := noFollow.Get(e.URL)
	if err != nil {
		return err
	}
	_ = first.Body.Close()
	if len(first.Cookies()) == 0 {
		return errors.New("the page's URL set no cookie to try the stop with")
	}
	req, err := http.NewRequest(http.MethodPost, e.Base+"/api/stop", strings.NewReader("{}"))
	if err != nil {
		return err
	}
	for _, ck := range first.Cookies() {
		req.AddCookie(ck)
	}
	req.Header.Set("Origin", e.Base)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	if resp.StatusCode < 300 {
		return fmt.Errorf("POST /api/stop with the cookie alone got %d: a page could stop its server", resp.StatusCode)
	}
	select {
	case <-c.proc.exited:
		return errors.New("POST /api/stop with the cookie alone stopped the server")
	case <-time.After(300 * time.Millisecond):
	}
	return c.stop()
}

// shipsSkill: a tool with a channel installs its skill.
func shipsSkill(_ *testing.T, c *scratch) error {
	if !c.s.Channel {
		return nil
	}
	cmd := exec.Command(c.exe, "skills", "install", "--agent", "pi")
	cmd.Env = c.env()
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("`%s skills install --agent pi`: %w\n%s", c.s.Tool, err, out)
	}
	name := c.s.SkillName
	if name == "" {
		name = c.s.Tool
	}
	found := false
	_ = filepath.WalkDir(c.home, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(path, filepath.Join(name, "SKILL.md")) {
			found = true
		}
		return nil
	})
	if !found {
		return fmt.Errorf("`%s skills install` wrote no %s/SKILL.md", c.s.Tool, name)
	}
	return nil
}
