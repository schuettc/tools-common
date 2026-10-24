package localweb

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// osascript is the command PutFile runs; a test points it at a fake so it
// never touches the real clipboard.
var osascript = "osascript"

// CanShare reports whether this machine can put a file on the clipboard for
// a paste to attach: macOS only (from galley).
func CanShare() bool { return runtime.GOOS == "darwin" }

// PutFile puts the file itself, not its path, on the macOS clipboard, so
// pasting into a chat attaches it. The path is an argument to the script,
// never part of its text (from galley).
func PutFile(path string) error {
	out, err := exec.Command(osascript,
		"-e", "on run argv",
		"-e", "set the clipboard to (POSIX file (item 1 of argv))",
		"-e", "end run",
		path).CombinedOutput()
	if err != nil {
		if msg := strings.TrimSpace(string(out)); msg != "" {
			return fmt.Errorf("could not put %s on the clipboard: %s", filepath.Base(path), msg)
		}
		return fmt.Errorf("could not put %s on the clipboard: %w", filepath.Base(path), err)
	}
	return nil
}
