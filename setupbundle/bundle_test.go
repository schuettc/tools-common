package setupbundle_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"maps"
	"testing"
	"testing/fstest"

	"github.com/subaud-tools/tools-common/setupbundle"
)

var (
	manifest = []byte("schema = 2\n\n[packages.integration]\n")
	files    = map[string][]byte{
		"zsh/proj.zsh":                    []byte("proj() { command proj \"$@\"; }\n"),
		"skills/proj/SKILL.md":            []byte("---\nname: proj\ndescription: the proj skill\n---\nbody\n"),
		"skills/proj/references/hooks.md": []byte("# hooks\n"),
	}
)

func fixture() fstest.MapFS {
	m := fstest.MapFS{}
	for name, body := range files {
		m[name] = &fstest.MapFile{Data: body, Mode: 0o644}
	}
	return m
}

func TestBuildThenReadKeepsIdentityAndFiles(t *testing.T) {
	data, err := setupbundle.Build("proj", "1.2.3", manifest, fixture())
	if err != nil {
		t.Fatal(err)
	}
	b, err := setupbundle.Read(data)
	if err != nil {
		t.Fatal(err)
	}
	if b.Tool != "proj" || b.Version != "1.2.3" {
		t.Errorf("identity = %q %q, want proj 1.2.3", b.Tool, b.Version)
	}
	if !bytes.Equal(b.Manifest, manifest) {
		t.Errorf("manifest = %q, want %q", b.Manifest, manifest)
	}
	if !maps.EqualFunc(b.Files, files, bytes.Equal) {
		t.Errorf("files = %q, want %q", b.Files, files)
	}
}

func TestBuildIsDeterministic(t *testing.T) {
	a, err := setupbundle.Build("proj", "1.2.3", manifest, fixture())
	if err != nil {
		t.Fatal(err)
	}
	b, err := setupbundle.Build("proj", "1.2.3", manifest, fixture())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Error("two builds of the same input differ")
	}
}

// entry is one archive member for a hand-made bundle.
type entry struct {
	name, body, link string
	typ              byte
}

func archive(t *testing.T, entries ...entry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, e := range entries {
		typ := e.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		h := &tar.Header{Name: e.name, Typeflag: typ, Linkname: e.link, Mode: 0o644, Size: int64(len(e.body))}
		if typ != tar.TypeReg {
			h.Size = 0
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(e.body)[:h.Size]); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

const meta = `{"format":1,"tool":"proj","version":"1.2.3"}`

func TestReadRejectsUnsupportedFormat(t *testing.T) {
	ok := archive(t, entry{name: "setup.json", body: meta}, entry{name: "setup.toml", body: string(manifest)})
	if _, err := setupbundle.Read(ok); err != nil {
		t.Fatalf("Read refused format 1: %v", err)
	}
	data := archive(t,
		entry{name: "setup.json", body: `{"format":2,"tool":"proj","version":"1.2.3"}`},
		entry{name: "setup.toml", body: string(manifest)},
	)
	if _, err := setupbundle.Read(data); err == nil {
		t.Error("Read accepted setup.json format 2")
	}
}

func TestReadRejectsSourceOutsideBundle(t *testing.T) {
	ok := archive(t,
		entry{name: "setup.json", body: meta},
		entry{name: "setup.toml", body: string(manifest)},
		entry{name: "files/zsh/proj.zsh", body: "x"},
	)
	if _, err := setupbundle.Read(ok); err != nil {
		t.Fatalf("Read refused a regular file under files/: %v", err)
	}
	for name, e := range map[string]entry{
		"path escaping files/": {name: "files/../../.zshrc", body: "x"},
		"symlink out":          {name: "files/zsh/proj.zsh", typ: tar.TypeSymlink, link: "/etc/passwd"},
	} {
		t.Run(name, func(t *testing.T) {
			data := archive(t,
				entry{name: "setup.json", body: meta},
				entry{name: "setup.toml", body: string(manifest)},
				e,
			)
			if _, err := setupbundle.Read(data); err == nil {
				t.Errorf("Read accepted %s", e.name)
			}
		})
	}
}
