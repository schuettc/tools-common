// Package skills installs a tool's agent skill, embedded in its binary, as
// <skills dir>/<name>/SKILL.md for each agent harness (Claude Code, Codex,
// pi), and refreshes the copies already installed when the binary's skill
// changes. A file is written only when it is the tool's own: its front
// matter's name: is the tool's name.
package skills

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	tools "github.com/subaud-tools/tools-common"
)

// skillFile is the file name of a skill, whatever the harness.
const skillFile = "SKILL.md"

// harness is one agent harness: its home directory (beneath $HOME, unless
// Env names an absolute one), whose presence means it is installed, and the
// skills directory in it.
type harness struct {
	Name, Label, Home, Env string
}

var harnesses = []harness{
	{Name: "claude-code", Label: "Claude Code", Home: ".claude", Env: "CLAUDE_CONFIG_DIR"},
	{Name: "codex", Label: "Codex", Home: ".codex", Env: "CODEX_HOME"},
	{Name: "pi", Label: "pi", Home: ".pi/agent", Env: "PI_CODING_AGENT_DIR"},
}

// UserTargets returns a fresh map of canonical harness names to portable default
// user skills-directory templates. Unlike Install, it does not expand HOME or
// environment overrides: these destinations are suitable for published setup.
func UserTargets() map[string]string {
	targets := make(map[string]string, len(harnesses))
	for _, h := range harnesses {
		targets[h.Name] = "~/" + h.Home + "/skills"
	}
	return targets
}

// dir is the harness's home directory.
func (h harness) dir() (string, error) {
	if v := os.Getenv(h.Env); filepath.IsAbs(v) {
		return filepath.Clean(v), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, h.Home), nil
}

// installed reports whether the harness's home directory exists.
func (h harness) installed() bool {
	d, err := h.dir()
	if err != nil {
		return false
	}
	st, err := os.Stat(d)
	return err == nil && st.IsDir()
}

// Install writes body as <skills dir>/<name>/SKILL.md for each agent named
// (claude-code, codex, pi), or, with none named, for each harness whose
// home directory exists. It prints one line per harness to out and each
// failure to errw, and fails with exit code 2 when there is no harness and
// 1 when any harness was not installed.
func Install(name string, body []byte, agents []string, out, errw io.Writer) error {
	if err := oneComponent(name); err != nil {
		return err
	}
	var targets []harness
	for _, n := range agents {
		i := -1
		for j, h := range harnesses {
			if h.Name == n {
				i = j
			}
		}
		if i < 0 {
			return tools.UsageError{Msg: fmt.Sprintf("unknown agent %q: claude-code, codex or pi", n)}
		}
		targets = append(targets, harnesses[i])
	}
	if len(agents) == 0 {
		for _, h := range harnesses {
			if h.installed() {
				targets = append(targets, h)
			}
		}
	}
	if len(targets) == 0 {
		return tools.Exitf(2, "no agent harness found: name one with --agent")
	}
	failed := 0
	for _, h := range targets {
		dest, err := installOne(h, name, body)
		if err != nil {
			failed++
			_, _ = fmt.Fprintf(errw, "%-12s %v\n", h.Label+":", err)
			continue
		}
		_, _ = fmt.Fprintf(out, "%-12s %s\n", h.Label+":", dest)
	}
	if failed > 0 {
		return tools.Exitf(1, "%d harness(es) not installed", failed)
	}
	return nil
}

// installOne writes body as the harness's <skills dir>/<name>/SKILL.md,
// making the skills directory when missing.
func installOne(h harness, name string, body []byte) (string, error) {
	home, err := h.dir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, "skills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	s, err := openSkill(dir, name, true)
	if err != nil {
		return "", err
	}
	defer s.close()
	if err := s.write(body); err != nil {
		return "", err
	}
	return s.dest, nil
}

// notOwnError is a skill directory or SKILL.md that is not the tool's to
// write: a symlink, not a plain directory or file, or another skill.
type notOwnError struct{ msg string }

func (e *notOwnError) Error() string { return e.msg }

// skill is one opened <skills dir>/<name>: the directory as a root of its
// own, and the SKILL.md in it (old and info nil when there is none).
type skill struct {
	dest  string
	dir   *os.Root
	old   []byte
	info  fs.FileInfo
	close func()
}

// openSkill opens dir/name and reads the SKILL.md in it. dir may be a
// symlink (a dotfiles install links the whole directory); name and SKILL.md
// beneath it may not be, and an existing SKILL.md must be the named skill's
// own (else a *notOwnError). With mk, a missing name is made; without it, a
// missing name or SKILL.md is fs.ErrNotExist. Every read and write goes
// through an os.Root on the skill's directory, checked to be the one that
// was Lstat'd.
func openSkill(dir, name string, mk bool) (*skill, error) {
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, err
	}
	root, err := os.OpenRoot(real)
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	s := &skill{dest: filepath.Join(dir, name, skillFile)}
	notOwn := &notOwnError{fmt.Sprintf("%s is not a plain directory: not %s's to write", filepath.Join(dir, name), name)}
	st, err := root.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) && mk {
		if err = root.Mkdir(name, 0o755); err == nil {
			st, err = root.Lstat(name)
		}
	}
	switch {
	case err != nil:
		return nil, err
	case st.Mode()&os.ModeSymlink != 0 || !st.IsDir():
		return nil, notOwn
	}
	sub, err := root.OpenRoot(name)
	if err != nil {
		return nil, err
	}
	if now, err := sub.Stat("."); err != nil || !os.SameFile(st, now) {
		_ = sub.Close()
		return nil, notOwn
	}
	s.dir, s.close = sub, func() { _ = sub.Close() }
	if err := s.read(name, mk); err != nil {
		s.close()
		return nil, err
	}
	return s, nil
}

// read reads the skill's SKILL.md into s.old and s.info, checking it is a
// regular file that is the named skill's own.
func (s *skill) read(name string, mk bool) error {
	notOwn := &notOwnError{fmt.Sprintf("%s is not a regular file: not %s's to replace", s.dest, name)}
	st, err := s.dir.Lstat(skillFile)
	switch {
	case errors.Is(err, fs.ErrNotExist) && mk:
		return nil
	case err != nil:
		return err
	case !st.Mode().IsRegular():
		return notOwn
	}
	f, err := s.dir.Open(skillFile)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	if now, err := f.Stat(); err != nil || !os.SameFile(st, now) {
		return notOwn
	}
	old, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	if n := skillName(old); n != name {
		return &notOwnError{fmt.Sprintf("%s is another skill (name: %q), not %s's to replace", s.dest, n, name)}
	}
	s.old, s.info = old, st
	return nil
}

// write writes body to a new temporary file beside SKILL.md (O_EXCL, which
// never follows a symlink, and a random name), synced and given the old
// file's mode, then renames it into place: nothing already beside SKILL.md
// is written through, and a SKILL.md hard-linked elsewhere keeps the other
// file's bytes.
func (s *skill) write(body []byte) error {
	var (
		tmp string
		f   *os.File
		err error
	)
	for range 10 {
		tmp = "." + skillFile + "-" + rand.Text() + ".tmp"
		f, err = s.dir.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if !errors.Is(err, fs.ErrExist) {
			break
		}
	}
	if err != nil {
		return err
	}
	fail := func(err error) error {
		_ = f.Close()
		_ = s.dir.Remove(tmp)
		return err
	}
	if _, err := f.Write(body); err != nil {
		return fail(err)
	}
	if s.info != nil {
		if err := f.Chmod(s.info.Mode().Perm()); err != nil {
			return fail(err)
		}
	}
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		_ = s.dir.Remove(tmp)
		return err
	}
	if err := s.dir.Rename(tmp, skillFile); err != nil {
		_ = s.dir.Remove(tmp)
		return err
	}
	return nil
}

// Owns reports whether b, a SKILL.md, is the named skill's own: its front
// matter's name: is name, the rule Install and Refresh apply before they
// replace one. It is for readers that only inspect, such as a setup tool
// deciding whether an installed skill is the tool's to update.
func Owns(name string, b []byte) bool {
	return OwnsWithMarkers(name, b, nil)
}

// skillName is the name in a skill's front matter ("" for none).
func skillName(b []byte) string {
	value, _ := frontmatterValue(b, "name")
	return value
}

// frontmatterValue uses the installer's simple first-field recognition, not YAML.
func frontmatterValue(b []byte, key string) (string, bool) {
	sc := bufio.NewScanner(bytes.NewReader(b))
	if !sc.Scan() || strings.TrimSpace(sc.Text()) != "---" {
		return "", false
	}
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "---" {
			return "", false
		}
		if v, ok := strings.CutPrefix(l, key+":"); ok {
			return strings.Trim(strings.TrimSpace(v), `"'`), true
		}
	}
	return "", false
}

// oneComponent refuses a skill name that is not one plain directory name, so
// no directory between the skills directory and the skill can be a symlink.
func oneComponent(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\\`) {
		return fmt.Errorf("skill name %q: must be one directory name", name)
	}
	return nil
}

// OwnsWithMarkers applies Owns' name rule and requires every declared marker to
// be present with its exact string value in the front matter. It uses the same
// simple field recognition and quote trimming as the name rule, not a YAML
// parser. Nil or empty markers retain name-only ownership. Adoption callers
// must check both the existing and desired assets with the same policy.
func OwnsWithMarkers(name string, body []byte, markers map[string]string) bool {
	if oneComponent(name) != nil || skillName(body) != name {
		return false
	}
	for key, want := range markers {
		if value, found := frontmatterValue(body, key); !found || value != want {
			return false
		}
	}
	return true
}
