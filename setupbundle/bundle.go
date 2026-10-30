// Package setupbundle builds and reads a tool release's setup bundle, the
// <tool>_setup.tar.gz published beside its binaries: setup.json (format,
// tool, version), setup.toml (the declarative integration fragment, kept as
// bytes) and the regular files it references under files/. The bundle's
// format is its own, independent of the manifest schema setup.toml uses.
package setupbundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"time"
)

const (
	format       = 1 // the setup.json format this package writes and reads
	metaName     = "setup.json"
	manifestName = "setup.toml"
	filesDir     = "files/"
)

// Bundle is a read bundle: Files maps each path relative to files/ to its
// bytes.
type Bundle struct {
	Tool     string
	Version  string
	Manifest []byte
	Files    map[string][]byte
}

type meta struct {
	Format  int    `json:"format"`
	Tool    string `json:"tool"`
	Version string `json:"version"`
}

// Build returns the gzipped tar of a bundle for tool at version, with
// manifest as setup.toml and every regular file of files (rooted at what
// becomes files/). The same input always gives the same bytes.
func Build(tool, version string, manifest []byte, files fs.FS) ([]byte, error) {
	m, err := json.Marshal(meta{Format: format, Tool: tool, Version: version})
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	add := func(name string, body []byte) error {
		h := &tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body)), ModTime: time.Unix(0, 0), Format: tar.FormatPAX}
		if err := tw.WriteHeader(h); err != nil {
			return err
		}
		_, err := tw.Write(body)
		return err
	}
	if err := add(metaName, m); err != nil {
		return nil, err
	}
	if err := add(manifestName, manifest); err != nil {
		return nil, err
	}
	err = fs.WalkDir(files, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("setup bundle: %s is not a regular file", p)
		}
		body, err := fs.ReadFile(files, p)
		if err != nil {
			return err
		}
		return add(filesDir+p, body)
	})
	if err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Read validates and reads a bundle. It refuses a setup.json format other
// than this package's, and any member other than setup.json, setup.toml and regular
// files whose paths stay beneath files/ (so never a symlink).
func Read(data []byte) (*Bundle, error) {
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("setup bundle: %w", err)
	}
	tr := tar.NewReader(zr)
	b := &Bundle{Files: map[string][]byte{}}
	var m meta
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("setup bundle: %w", err)
		}
		if h.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("setup bundle: %s is not a regular file", h.Name)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("setup bundle: %w", err)
		}
		switch rel, inFiles := strings.CutPrefix(h.Name, filesDir); {
		case h.Name == metaName:
			if err := json.Unmarshal(body, &m); err != nil {
				return nil, fmt.Errorf("setup bundle: %s: %w", metaName, err)
			}
		case h.Name == manifestName:
			b.Manifest = body
		case inFiles && fs.ValidPath(rel) && rel != ".":
			b.Files[rel] = body
		default:
			return nil, fmt.Errorf("setup bundle: %s is outside files/", h.Name)
		}
	}
	if m.Format != format {
		return nil, fmt.Errorf("setup bundle: %s format %d is not supported (want %d)", metaName, m.Format, format)
	}
	b.Tool, b.Version = m.Tool, m.Version
	return b, nil
}
