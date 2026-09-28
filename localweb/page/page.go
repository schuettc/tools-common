// Package page is the family's galley-style local page kit: one stylesheet of
// tokens and components plus vanilla ES modules (theme, keyboard, API, live
// updates, DOM builders), embedded and served under /_kit/ beside a tool's own
// page. Pair it with localweb:
//
//	localweb.Start(ctx, localweb.Config{Tool: "cull", Assets: page.With(webFS), API: api})
//
// See tools-ops docs/superpowers/specs/2026-09-26-localweb-page-kit-design.md.
package page

import (
	"embed"
	"io"
	"io/fs"
	"sort"
	"strings"
	"time"
)

// Prefix is where the kit's assets are served: a page imports "/_kit/kit.js".
const Prefix = "_kit"

//go:embed assets
var assets embed.FS

// FS is the kit's embedded assets, rooted at the asset directory.
func FS() fs.FS {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		panic(err) // the embed directive guarantees the directory
	}
	return sub
}

// With returns an fs.FS that serves the tool's own assets at / and the kit's
// under /_kit/. The kit wins on the prefix; a tool file at _kit/... is
// unreachable. tool may be nil for a page made only of kit files.
func With(tool fs.FS) fs.FS { return overlay{tool: tool, kit: FS()} }

type overlay struct{ tool, kit fs.FS }

// kitName maps a name under the prefix to the kit's own name.
func kitName(name string) (string, bool) {
	if name == Prefix {
		return ".", true
	}
	if rest, ok := strings.CutPrefix(name, Prefix+"/"); ok {
		return rest, true
	}
	return "", false
}

func (o overlay) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	if name == "." {
		return o.openRoot()
	}
	if k, ok := kitName(name); ok {
		f, err := o.kit.Open(k)
		if err != nil || k != "." {
			return f, err
		}
		return renamed{f.(fs.ReadDirFile)}, nil
	}
	if o.tool == nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return o.tool.Open(name)
}

func (o overlay) ReadDir(name string) ([]fs.DirEntry, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrInvalid}
	}
	if k, ok := kitName(name); ok {
		return fs.ReadDir(o.kit, k)
	}
	if name != "." {
		if o.tool == nil {
			return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
		}
		return fs.ReadDir(o.tool, name)
	}
	var out []fs.DirEntry
	if o.tool != nil {
		es, err := fs.ReadDir(o.tool, ".")
		if err != nil {
			return nil, err
		}
		for _, e := range es {
			if e.Name() != Prefix {
				out = append(out, e)
			}
		}
	}
	info, err := fs.Stat(o.kit, ".")
	if err != nil {
		return nil, err
	}
	out = append(out, fs.FileInfoToDirEntry(renamedInfo{info}))
	sort.Slice(out, func(i, j int) bool { return out[i].Name() < out[j].Name() })
	return out, nil
}

// openRoot is the overlay's "." : the tool's root (or an empty one), listing
// the kit's directory in place of any tool _kit.
func (o overlay) openRoot() (fs.File, error) {
	entries, err := o.ReadDir(".")
	if err != nil {
		return nil, err
	}
	var info fs.FileInfo = emptyRoot{}
	if o.tool != nil {
		if info, err = fs.Stat(o.tool, "."); err != nil {
			return nil, err
		}
	}
	return &rootDir{info: info, entries: entries}, nil
}

// renamed is the kit's root directory, opened as _kit.
type renamed struct{ fs.ReadDirFile }

func (r renamed) Stat() (fs.FileInfo, error) {
	info, err := r.ReadDirFile.Stat()
	if err != nil {
		return nil, err
	}
	return renamedInfo{info}, nil
}

type renamedInfo struct{ fs.FileInfo }

func (renamedInfo) Name() string { return Prefix }

// rootDir is an open "." listing a fixed set of entries.
type rootDir struct {
	info    fs.FileInfo
	entries []fs.DirEntry
	off     int
}

func (d *rootDir) Stat() (fs.FileInfo, error) { return d.info, nil }
func (d *rootDir) Close() error               { return nil }
func (d *rootDir) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: ".", Err: fs.ErrInvalid}
}

func (d *rootDir) ReadDir(n int) ([]fs.DirEntry, error) {
	rest := d.entries[d.off:]
	if n <= 0 {
		d.off = len(d.entries)
		return rest, nil
	}
	if len(rest) == 0 {
		return nil, io.EOF
	}
	if n > len(rest) {
		n = len(rest)
	}
	d.off += n
	return rest[:n], nil
}

// emptyRoot describes "." when there is no tool FS.
type emptyRoot struct{}

func (emptyRoot) Name() string       { return "." }
func (emptyRoot) Size() int64        { return 0 }
func (emptyRoot) Mode() fs.FileMode  { return fs.ModeDir | 0o555 }
func (emptyRoot) ModTime() time.Time { return time.Time{} }
func (emptyRoot) IsDir() bool        { return true }
func (emptyRoot) Sys() any           { return nil }
