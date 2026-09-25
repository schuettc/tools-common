package tools

import (
	"os"
	"path/filepath"
	"testing"
)

func clearDirEnv(t *testing.T) {
	for _, k := range []string{"XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME", "MY_TOOL_HOME"} {
		t.Setenv(k, "")
	}
}

func TestDirsDefaultToHome(t *testing.T) {
	clearDirEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	cases := map[string]string{
		ConfigDir("my-tool"): filepath.Join(home, ".config", "my-tool"),
		StateDir("my-tool"):  filepath.Join(home, ".local", "state", "my-tool"),
		CacheDir("my-tool"):  filepath.Join(home, ".cache", "my-tool"),
		DataDir("my-tool"):   filepath.Join(home, ".local", "share", "my-tool"),
	}
	for got, want := range cases {
		if got != want {
			t.Errorf("got %s want %s", got, want)
		}
	}
}

func TestDirsHonorXDG(t *testing.T) {
	clearDirEnv(t)
	x := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(x, "c"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(x, "s"))
	if got := ConfigDir("my-tool"); got != filepath.Join(x, "c", "my-tool") {
		t.Errorf("config %s", got)
	}
	if got := StateDir("my-tool"); got != filepath.Join(x, "s", "my-tool") {
		t.Errorf("state %s", got)
	}
}

func TestToolHomeOverridesAll(t *testing.T) {
	clearDirEnv(t)
	h := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", "/elsewhere")
	t.Setenv("MY_TOOL_HOME", h)
	if ConfigDir("my-tool") != filepath.Join(h, "config") || StateDir("my-tool") != filepath.Join(h, "state") ||
		CacheDir("my-tool") != filepath.Join(h, "cache") || DataDir("my-tool") != filepath.Join(h, "data") {
		t.Fatalf("override not applied: %s %s", ConfigDir("my-tool"), StateDir("my-tool"))
	}
}

func TestRelativeEnvIgnored(t *testing.T) {
	clearDirEnv(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "relative/config")
	t.Setenv("MY_TOOL_HOME", "relative/home")
	if got := ConfigDir("my-tool"); got != filepath.Join(home, ".config", "my-tool") {
		t.Fatalf("relative value used: %s", got)
	}
}

func TestEnsureDirCreates0700(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x", "y")
	if err := EnsureDir(p); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(p)
	if err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o700 {
		t.Fatalf("stat %v err %v", fi, err)
	}
}
