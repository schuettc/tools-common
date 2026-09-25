package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestWriteFileAtomicCreatesParentsAndSetsExactPerm(t *testing.T) {
	old := syscallUmask(0o077)
	defer syscallUmask(old)
	p := filepath.Join(t.TempDir(), "a", "b", "f.txt")
	if err := WriteFileAtomic(p, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	fi, _ := os.Stat(p)
	if string(b) != "hello" || fi.Mode().Perm() != 0o644 {
		t.Fatalf("content %q perm %v", b, fi.Mode().Perm())
	}
}

func TestWriteFileAtomicReplacesAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	_ = os.WriteFile(p, []byte("old"), 0o600)
	if err := WriteFileAtomic(p, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	entries, _ := os.ReadDir(dir)
	if string(b) != "new" || len(entries) != 1 {
		t.Fatalf("content %q, dir entries %d", b, len(entries))
	}
}

func TestWriteFileAtomicFailureKeepsOriginal(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	_ = os.WriteFile(p, []byte("original"), 0o600)
	if err := os.Chmod(dir, 0o500); err != nil { // no create in dir
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o700)
	if err := WriteFileAtomic(p, []byte("new"), 0o600); err == nil {
		t.Fatal("expected an error writing into a read-only dir")
	}
	b, _ := os.ReadFile(p)
	if string(b) != "original" {
		t.Fatalf("original clobbered: %q", b)
	}
}

func TestWriteFileAtomicRenameFailureKeepsTemp(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target")
	if err := os.Mkdir(target, 0o700); err != nil { // renaming a file over a non-empty dir fails
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(target, "x"), nil, 0o600)
	err := WriteFileAtomic(target, []byte("keep me"), 0o600)
	if err == nil || !strings.Contains(err.Error(), "temp kept") {
		t.Fatalf("want a 'temp kept' error, got %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(dir, ".target-*.tmp"))
	if len(matches) != 1 {
		t.Fatalf("temp file not kept: %v", matches)
	}
	if b, _ := os.ReadFile(matches[0]); string(b) != "keep me" {
		t.Fatalf("temp content %q", b)
	}
}

func TestWriteFileAtomicConcurrentWriters(t *testing.T) {
	p := filepath.Join(t.TempDir(), "f")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if err := WriteFileAtomic(p, []byte(strings.Repeat("x", 1000+i)), 0o600); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	b, _ := os.ReadFile(p)
	if len(b) < 1000 || strings.Trim(string(b), "x") != "" {
		t.Fatalf("torn or corrupt content, len %d", len(b))
	}
}

func TestPIDAlive(t *testing.T) {
	if !PIDAlive(os.Getpid()) {
		t.Fatal("self must be alive")
	}
	if PIDAlive(0) || PIDAlive(-1) {
		t.Fatal("pid <= 0 must be dead")
	}
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil { // Run waits, so the child is reaped
		t.Fatal(err)
	}
	if PIDAlive(cmd.Process.Pid) {
		t.Fatal("reaped child reported alive")
	}
}
