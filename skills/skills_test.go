package skills_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tools "github.com/subaud-tools/tools-common"
	"github.com/subaud-tools/tools-common/skills"
)

const body = "---\nname: tool\ndescription: the tool's skill\n---\nnew body\n"

// home points HOME at a fresh temp dir, clears each harness's home
// variable, and returns it.
func home(t *testing.T) string {
	t.Helper()
	h := t.TempDir()
	t.Setenv("HOME", h)
	for _, k := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "PI_CODING_AGENT_DIR"} {
		t.Setenv(k, "")
	}
	return h
}

func mkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, p, s string) {
	t.Helper()
	mkdir(t, filepath.Dir(p))
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func install(agents ...string) (string, string, error) {
	var out, errw bytes.Buffer
	err := skills.Install("tool", []byte(body), agents, &out, &errw)
	return out.String(), errw.String(), err
}

func exitCode(err error) int {
	var ee *tools.ExitError
	if errors.As(err, &ee) {
		return ee.Code
	}
	return -1
}

// harnesses makes the three harness homes, pi's skills directory a symlink
// (as a dotfiles install makes it), and returns each one's skills directory
// as it is written through.
func harnesses(t *testing.T, h string) []string {
	t.Helper()
	mkdir(t, filepath.Join(h, ".claude"))
	mkdir(t, filepath.Join(h, ".codex"))
	mkdir(t, filepath.Join(h, ".pi/agent"))
	dots := t.TempDir()
	if err := os.Symlink(dots, filepath.Join(h, ".pi/agent/skills")); err != nil {
		t.Fatal(err)
	}
	return []string{filepath.Join(h, ".claude/skills"), filepath.Join(h, ".codex/skills"), dots}
}

// Install writes the skill into each harness found, again and again, one
// line per harness, and into no harness that is not there.
func TestInstallWritesEachHarness(t *testing.T) {
	h := home(t)
	dirs := harnesses(t, h)
	for range 2 {
		out, errw, err := install()
		if err != nil {
			t.Fatalf("%v: %s %s", err, out, errw)
		}
		for _, d := range dirs {
			if got := read(t, filepath.Join(d, "tool/SKILL.md")); got != body {
				t.Errorf("%s: %q", d, got)
			}
		}
		if strings.Count(out, "\n") != 3 {
			t.Errorf("want one line per harness:\n%s", out)
		}
	}

	h = home(t)
	mkdir(t, filepath.Join(h, ".claude"))
	if out, errw, err := install(); err != nil {
		t.Fatalf("%v: %s %s", err, out, errw)
	}
	for _, d := range []string{".codex", ".pi"} {
		if _, err := os.Stat(filepath.Join(h, d)); err == nil {
			t.Errorf("installed for %s, which is not there", d)
		}
	}
}

// A named agent is installed even when its home is missing; an unknown one
// is a usage error, and no harness at all exits 2.
func TestInstallAgentsAndNoHarness(t *testing.T) {
	h := home(t)
	if out, errw, err := install("codex"); err != nil {
		t.Fatalf("%v: %s %s", err, out, errw)
	}
	if got := read(t, filepath.Join(h, ".codex/skills/tool/SKILL.md")); got != body {
		t.Errorf("codex: %q", got)
	}
	var ue tools.UsageError
	if _, _, err := install("cursor"); !errors.As(err, &ue) {
		t.Errorf("unknown agent: want a usage error, got %v", err)
	}
	home(t)
	if _, _, err := install(); exitCode(err) != 2 {
		t.Errorf("no harness: want exit 2, got %v", err)
	}
}

// A harness's home variable, when absolute, is where its skills go.
func TestInstallFollowsTheHomeVariable(t *testing.T) {
	h := home(t)
	codex := t.TempDir()
	t.Setenv("CODEX_HOME", codex)
	if out, errw, err := install(); err != nil {
		t.Fatalf("%v: %s %s", err, out, errw)
	}
	if got := read(t, filepath.Join(codex, "skills/tool/SKILL.md")); got != body {
		t.Errorf("CODEX_HOME: %q", got)
	}
	if _, err := os.Stat(filepath.Join(h, ".codex")); err == nil {
		t.Error("wrote ~/.codex despite CODEX_HOME")
	}
}

// An existing SKILL.md of another name is never replaced.
func TestInstallRefusesAForeignSkill(t *testing.T) {
	h := home(t)
	foreign := filepath.Join(h, ".claude/skills/tool/SKILL.md")
	write(t, foreign, "---\nname: other\n---\n")
	_, errw, err := install("claude-code")
	if exitCode(err) != 1 || !strings.Contains(errw, "not tool's") {
		t.Fatalf("%v %s", err, errw)
	}
	if got := read(t, foreign); got != "---\nname: other\n---\n" {
		t.Error("a foreign skill was overwritten")
	}
}

// Nothing beneath the skills directory may be a symlink: neither the
// skill's directory nor its SKILL.md.
func TestInstallRefusesASymlinkBeneath(t *testing.T) {
	for _, which := range []string{"dir", "file"} {
		t.Run(which, func(t *testing.T) {
			h := home(t)
			skillsDir := filepath.Join(h, ".claude/skills")
			target := t.TempDir()
			keep := filepath.Join(target, "SKILL.md")
			write(t, keep, "---\nname: tool\n---\nkeep me\n")
			switch which {
			case "dir":
				mkdir(t, skillsDir)
				if err := os.Symlink(target, filepath.Join(skillsDir, "tool")); err != nil {
					t.Fatal(err)
				}
			case "file":
				mkdir(t, filepath.Join(skillsDir, "tool"))
				if err := os.Symlink(keep, filepath.Join(skillsDir, "tool/SKILL.md")); err != nil {
					t.Fatal(err)
				}
			}
			_, errw, err := install("claude-code")
			if exitCode(err) != 1 || !strings.Contains(errw, "not tool's") {
				t.Fatalf("%v %s", err, errw)
			}
			if !strings.Contains(read(t, keep), "keep me") {
				t.Error("written through a symlink")
			}
		})
	}
}

// The new SKILL.md is a fresh file renamed into place, with the old one's
// mode: a SKILL.md hard-linked to a file elsewhere leaves that file's bytes.
func TestInstallWritesAFreshFile(t *testing.T) {
	h := home(t)
	outside := filepath.Join(t.TempDir(), "outside")
	write(t, outside, "---\nname: tool\n---\nkeep me\n")
	if err := os.Chmod(outside, 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(h, ".claude/skills/tool/SKILL.md")
	mkdir(t, filepath.Dir(dest))
	if err := os.Link(outside, dest); err != nil {
		t.Fatal(err)
	}
	if out, errw, err := install("claude-code"); err != nil {
		t.Fatalf("%v: %s %s", err, out, errw)
	}
	if got := read(t, outside); !strings.Contains(got, "keep me") {
		t.Errorf("the linked file was written: %q", got)
	}
	if got := read(t, dest); got != body {
		t.Errorf("dest: %q", got)
	}
	if st, err := os.Stat(dest); err != nil || st.Mode().Perm() != 0o600 {
		t.Errorf("mode not kept: %v %v", st.Mode(), err)
	}
	ents, _ := os.ReadDir(filepath.Dir(dest))
	if len(ents) != 1 {
		t.Errorf("left something beside SKILL.md: %v", ents)
	}
}

// Refresh rewrites each installed copy of the tool's own skill that differs
// from body, on every harness (pi's skills directory a symlink), and returns
// the paths it rewrote.
func TestRefreshRewritesAStaleCopy(t *testing.T) {
	h := home(t)
	var want []string
	for _, d := range harnesses(t, h) {
		write(t, filepath.Join(d, "tool/SKILL.md"), "---\nname: tool\n---\nold body\n")
	}
	got, err := skills.Refresh("tool", []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{".claude/skills", ".codex/skills", ".pi/agent/skills"} {
		p := filepath.Join(h, d, "tool/SKILL.md")
		want = append(want, p)
		if s := read(t, p); s != body {
			t.Errorf("%s: %q", p, s)
		}
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("rewrote %v, want %v", got, want)
	}
	if again, err := skills.Refresh("tool", []byte(body)); err != nil || len(again) != 0 {
		t.Errorf("second refresh: %v %v", again, err)
	}
}

// A copy that matches is left alone (its mtime unchanged), a missing one is
// not created, and a SKILL.md of another name is not touched.
func TestRefreshLeavesWhatIsNotStale(t *testing.T) {
	h := home(t)
	harnesses(t, h)
	match := filepath.Join(h, ".claude/skills/tool/SKILL.md")
	write(t, match, body)
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(match, old, old); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(h, ".pi/agent/skills/tool/SKILL.md")
	write(t, foreign, "---\nname: other\n---\n")

	got, err := skills.Refresh("tool", []byte(body))
	if err != nil || len(got) != 0 {
		t.Fatalf("rewrote %v, %v", got, err)
	}
	if st, err := os.Stat(match); err != nil || !st.ModTime().Equal(old) {
		t.Errorf("matching copy touched: %v %v", st.ModTime(), err)
	}
	if _, err := os.Stat(filepath.Join(h, ".codex/skills")); err == nil {
		t.Error("refresh created a skill that was not installed")
	}
	if s := read(t, foreign); s != "---\nname: other\n---\n" {
		t.Error("a foreign skill was rewritten")
	}
}

// Refresh never writes through a symlink beneath the skills directory, and
// with no harness at all it does nothing.
func TestRefreshSkipsSymlinksAndNoHarness(t *testing.T) {
	h := home(t)
	if got, err := skills.Refresh("tool", []byte(body)); err != nil || len(got) != 0 {
		t.Fatalf("no harness: %v %v", got, err)
	}
	keep := filepath.Join(t.TempDir(), "SKILL.md")
	write(t, keep, "---\nname: tool\n---\nkeep me\n")
	mkdir(t, filepath.Join(h, ".claude/skills/tool"))
	if err := os.Symlink(keep, filepath.Join(h, ".claude/skills/tool/SKILL.md")); err != nil {
		t.Fatal(err)
	}
	got, err := skills.Refresh("tool", []byte(body))
	if err != nil || len(got) != 0 || !strings.Contains(read(t, keep), "keep me") {
		t.Errorf("written through a symlink: %v %v", got, err)
	}
}

// A skill's name is one directory under the skills directory: a name with a
// path in it is refused, so no directory in between can be a symlink.
func TestNameIsOneComponent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	t.Setenv("PI_CODING_AGENT_DIR", "")
	if err := os.MkdirAll(filepath.Join(home, ".claude", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"group/tool", "..", ".", ""} {
		if err := skills.Install(name, []byte("---\nname: "+name+"\n---\n"), nil, bytes.NewBuffer(nil), bytes.NewBuffer(nil)); err == nil {
			t.Errorf("Install(%q): want an error", name)
		}
		if _, err := skills.Refresh(name, []byte("x")); err == nil {
			t.Errorf("Refresh(%q): want an error", name)
		}
	}
}

// Owns is the ownership rule Install and Refresh apply, for a reader that
// only inspects: a SKILL.md is the named skill's own when its front matter's
// name: is that name.
func TestOwns(t *testing.T) {
	own := []byte("---\nname: proj\ndescription: d\n---\nbody\n")
	if !skills.Owns("proj", own) {
		t.Error("Owns(proj, proj's skill) = false")
	}
	if skills.Owns("diazo", own) {
		t.Error("Owns(diazo, proj's skill) = true")
	}
	if skills.Owns("proj", []byte("no front matter\n")) {
		t.Error("Owns(proj, no front matter) = true")
	}
	if skills.Owns("", []byte("---\ndescription: d\n---\n")) {
		t.Error("Owns(\"\", nameless skill) = true")
	}
}

func TestUserTargetsStayPortableAndIndependent(t *testing.T) {
	home(t)
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	targets := skills.UserTargets()
	want := map[string]string{"claude-code": "~/.claude/skills", "codex": "~/.codex/skills", "pi": "~/.pi/agent/skills"}
	if len(targets) != len(want) {
		t.Fatalf("targets = %v", targets)
	}
	for name, path := range want {
		if targets[name] != path {
			t.Errorf("%s = %q, want %q", name, targets[name], path)
		}
	}
	targets["pi"] = "changed"
	if skills.UserTargets()["pi"] != want["pi"] {
		t.Fatal("caller changed shared targets")
	}
}

func TestOwnsWithMarkersRequiresMudaNameAndStringMarker(t *testing.T) {
	const header = "---\nname: muda\ndescription: Use when starting or resuming delivery-waste optimization in a GitHub repository.\nmuda-version: \">=0.1.0\"\nmuda-owned: muda\n---\n"
	for _, tc := range []struct {
		label, body string
		want        bool
	}{
		{"actual muda header", header, true},
		{"quoted values", "---\nname: 'muda'\nmuda-owned: \"muda\"\n---\n", true},
		{"foreign same name", "---\nname: muda\n---\n", false},
		{"boolean is not ownership", "---\nname: muda\nmuda-owned: true\n---\n", false},
		{"wrong skill name", "---\nname: muda-diagnose\nmuda-owned: muda\n---\n", false},
		{"marker outside header", "---\nname: muda\n---\nmuda-owned: muda\n", false},
	} {
		t.Run(tc.label, func(t *testing.T) {
			if got := skills.OwnsWithMarkers("muda", []byte(tc.body), map[string]string{"muda-owned": "muda"}); got != tc.want {
				t.Fatalf("ownership = %v, want %v", got, tc.want)
			}
		})
	}
}
