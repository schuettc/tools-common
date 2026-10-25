package skills

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
)

// Refresh rewrites, for each harness whose home directory exists, the
// installed <skills dir>/<name>/SKILL.md that is the tool's own and differs
// from body, and returns the paths it rewrote. It never creates a skill
// that is not installed, and skips one that is not the tool's own (another
// name:, or a symlink beneath the skills directory). A harness it cannot
// read or write is reported in the error, and the rest are still refreshed.
func Refresh(name string, body []byte) ([]string, error) {
	if err := oneComponent(name); err != nil {
		return nil, err
	}
	var (
		rewrote []string
		errs    []error
	)
	for _, h := range harnesses {
		if !h.installed() {
			continue
		}
		dest, err := refreshOne(h, name, body)
		var notOwn *notOwnError
		switch {
		case errors.Is(err, fs.ErrNotExist) || errors.As(err, &notOwn):
		case err != nil:
			errs = append(errs, fmt.Errorf("%s: %w", h.Label, err))
		case dest != "":
			rewrote = append(rewrote, dest)
		}
	}
	return rewrote, errors.Join(errs...)
}

// refreshOne rewrites the harness's copy of the skill when it differs from
// body, returning its path ("" when it already matches).
func refreshOne(h harness, name string, body []byte) (string, error) {
	home, err := h.dir()
	if err != nil {
		return "", err
	}
	s, err := openSkill(filepath.Join(home, "skills"), name, false)
	if err != nil {
		return "", err
	}
	defer s.close()
	if bytes.Equal(s.old, body) {
		return "", nil
	}
	if err := s.write(body); err != nil {
		return "", err
	}
	return s.dest, nil
}
