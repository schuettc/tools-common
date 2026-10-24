package localweb

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// fakeOsascript puts a script in place of osascript that writes its arguments,
// one per line, to a file and exits with code, printing stderr first. The
// tests never touch the real clipboard.
func fakeOsascript(t *testing.T, code int, stderr string) string {
	t.Helper()
	dir := t.TempDir()
	args := filepath.Join(dir, "args")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\n' \"$a\"; done > '" + args + "'\n"
	if stderr != "" {
		script += "printf '%s\\n' '" + stderr + "' >&2\n"
	}
	script += "exit " + strconv.Itoa(code) + "\n"
	bin := filepath.Join(dir, "osascript")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	old := osascript
	osascript = bin
	t.Cleanup(func() { osascript = old })
	return args
}

func TestPutFilePassesThePathAsAnArgument(t *testing.T) {
	args := fakeOsascript(t, 0, "")
	path := `/tmp/a "quoted" doc.html`
	if err := PutFile(path); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(args)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	if lines[len(lines)-1] != path {
		t.Fatalf("last argument = %q, want the path %q", lines[len(lines)-1], path)
	}
	for _, l := range lines[:len(lines)-1] {
		if strings.Contains(l, "quoted") {
			t.Errorf("the path is in the script's text: %q", l)
		}
	}
	if !strings.Contains(string(b), "set the clipboard to (POSIX file (item 1 of argv))") {
		t.Errorf("script does not put the file on the clipboard:\n%s", b)
	}
}

func TestPutFileReportsTheClipboardsError(t *testing.T) {
	fakeOsascript(t, 1, "not allowed")
	err := PutFile("/tmp/doc.md")
	if err == nil {
		t.Fatal("want an error")
	}
	if want := "could not put doc.md on the clipboard: not allowed"; err.Error() != want {
		t.Fatalf("error = %q, want %q", err, want)
	}
}
