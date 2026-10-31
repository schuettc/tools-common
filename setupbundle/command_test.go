package setupbundle_test

import (
	"bytes"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"testing"

	tools "github.com/subaud-tools/tools-common"
	"github.com/subaud-tools/tools-common/setupbundle"
)

func TestBundleCommandWritesOnlyReleaseArchive(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source fs.FS
		want   map[string][]byte
	}{
		{"assets", fixture(), files},
		{"config-only", nil, map[string][]byte{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("HOME", home)
			dest := filepath.Join(t.TempDir(), "proj_setup.tar.gz")
			app := tools.New(tools.Config{Name: "proj"})
			app.Register(setupbundle.BundleCommand("proj", "dev", setupbundle.Source{Manifest: manifest, Files: tc.source}))
			var out, errw bytes.Buffer
			if code := app.Dispatch([]string{"setup-bundle", "--version", "1.2.3", "--out", dest}, &out, &errw); code != 0 {
				t.Fatalf("exit %d: %s", code, &errw)
			}
			data, err := os.ReadFile(dest)
			if err != nil {
				t.Fatal(err)
			}
			b, err := setupbundle.Read(data)
			if err != nil {
				t.Fatal(err)
			}
			if b.Tool != "proj" || b.Version != "1.2.3" || !bytes.Equal(b.Manifest, manifest) || !maps.EqualFunc(b.Files, tc.want, bytes.Equal) {
				t.Fatalf("wrong bundle: %+v", b)
			}
			entries, err := os.ReadDir(home)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatalf("HOME modified: %v", entries)
			}
		})
	}
}
