package claudeplugin

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// UpdateEnv, set to 1, makes AssertCurrent write the plugin instead of
// comparing it.
const UpdateEnv = "CLAUDEPLUGIN_UPDATE"

// AssertCurrent fails t for each file in dir that differs from what s
// renders, or is missing, and names the command that regenerates them. Each
// tool calls it from a test of its own, with dir its committed plugin, so an
// edited copy or a newer template fails that repo's tests.
func AssertCurrent(t testing.TB, dir string, s Spec) {
	t.Helper()
	if os.Getenv(UpdateEnv) == "1" {
		if err := Write(dir, s); err != nil {
			t.Errorf("claudeplugin: writing %s: %v", dir, err)
		}
		return
	}
	files, err := Render(s)
	if err != nil {
		t.Errorf("claudeplugin: %v", err)
		return
	}
	regen := UpdateEnv + "=1 go test -run '^" + t.Name() + "$' ./..."
	for _, p := range files.paths() {
		got, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(p)))
		switch {
		case err != nil:
			t.Errorf("claudeplugin: %s is missing from %s; regenerate with: %s", p, dir, regen)
		case !bytes.Equal(got, files[p]):
			t.Errorf("claudeplugin: %s in %s differs from what tools-common renders; regenerate with: %s", p, dir, regen)
		}
	}
}
