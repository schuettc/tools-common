package tools

import (
	"os"
	"path/filepath"
	"strings"
)

// ConfigDir returns the tool's config directory (see toolDir for the rules).
// It does not create it; use EnsureDir.
func ConfigDir(tool string) string { return toolDir(tool, "config", "XDG_CONFIG_HOME", ".config") }

// StateDir returns the tool's state directory (spools, caches that matter,
// sockets, port hints).
func StateDir(tool string) string {
	return toolDir(tool, "state", "XDG_STATE_HOME", filepath.Join(".local", "state"))
}

// CacheDir returns the tool's cache directory (safe to delete).
func CacheDir(tool string) string { return toolDir(tool, "cache", "XDG_CACHE_HOME", ".cache") }

// DataDir returns the tool's data directory.
func DataDir(tool string) string {
	return toolDir(tool, "data", "XDG_DATA_HOME", filepath.Join(".local", "share"))
}

// EnsureDir creates path and its parents with mode 0700.
func EnsureDir(path string) error { return os.MkdirAll(path, 0o700) }

// toolDir: an absolute $<TOOL>_HOME (upper-cased, '-'→'_') wins and yields
// $<TOOL>_HOME/<kind>; otherwise an absolute $<xdgVar> base, else
// $HOME/<homeRel>, joined with the tool name. Relative values are ignored
// (XDG base-dir spec) so a tool never writes relative to its cwd. These are
// for NEW paths; existing tools keep their current locations.
func toolDir(tool, kind, xdgVar, homeRel string) string {
	envName := strings.ToUpper(strings.ReplaceAll(tool, "-", "_")) + "_HOME"
	if h := os.Getenv(envName); filepath.IsAbs(h) {
		return filepath.Join(h, kind)
	}
	if b := os.Getenv(xdgVar); filepath.IsAbs(b) {
		return filepath.Join(b, tool)
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), tool, kind)
	}
	return filepath.Join(home, homeRel, tool)
}
