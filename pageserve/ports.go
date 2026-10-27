package pageserve

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"

	tools "github.com/subaud-tools/tools-common"
)

// Port reuse for a tool with many pages (galley). Each page server binds a
// fresh random port on every start, so a restart (to pick up an .html
// rewrite, say) moves the URL and 404s the reviewer's open tab on its next
// action. The last port a page served on is remembered here so the next
// start can offer it back; the tab survives a restart without anyone pinning
// --port. (A one-page tool has localweb's own port hint.)
//
// The hint lives UNDER the live directory (<dir>/ports), NOT beside the
// document: it is machine state, not review state, and keeping it out of the
// working tree means no .gitignore entry and no file the reviewer sees. It is
// keyed by the page's ABSOLUTE PATH — the server's Room carries a per-launch
// instance token and is deliberately unstable, so it cannot key a fact that
// must outlive the launch that wrote it.
//
// It sits in a `ports` SUBDIRECTORY for the same reason session presence does
// (see sub): List/Inspect glob "*.json" in the directory itself and do not
// recurse, so a hint in its root would be read as an entry, fail Validate for
// its missing room, and be reported as a refused page on every scan.
//
// It is only ever a HINT. The caller tries the port and falls back to a free
// one if it is taken, so a stale entry — the port claimed by something else
// since — costs one failed bind, never a refusal to serve.

type portHint struct {
	Path string `json:"path"`
	Port int    `json:"port"`
}

// portHintPath names the hint file for a page. The absolute path is hashed so
// the filename is fixed-length and carries no separators; the path is stored
// inside the file too, so a hash collision is caught on read rather than
// handing back another page's port.
func (l Live) portHintPath(pageAbsPath string) (string, error) {
	dir, err := l.sub("ports")
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(pageAbsPath))
	return filepath.Join(dir, "port-"+hex.EncodeToString(sum[:])[:16]+".json"), nil
}

// LastPort returns the port a page last served on, if one was remembered and
// the record is for this exact page. Any error — no hint, unreadable bytes, a
// hash collision, a nonsense port — is reported as "no hint" so the caller
// simply binds a free port.
func (l Live) LastPort(page string) (int, bool) {
	abs, err := filepath.Abs(page)
	if err != nil {
		return 0, false
	}
	name, err := l.portHintPath(abs)
	if err != nil {
		return 0, false
	}
	raw, err := os.ReadFile(name)
	if err != nil {
		return 0, false
	}
	var h portHint
	if err := json.Unmarshal(raw, &h); err != nil {
		return 0, false
	}
	if h.Path != abs || h.Port < 1 || h.Port > 65535 {
		return 0, false
	}
	return h.Port, true
}

// SaveLastPort remembers the port a page is now serving on. A failure to
// write is silently dropped: the reuse is an optimisation, and a review that
// cannot persist a hint still serves.
func (l Live) SaveLastPort(page string, port int) {
	abs, err := filepath.Abs(page)
	if err != nil {
		return
	}
	name, err := l.portHintPath(abs)
	if err != nil {
		return
	}
	raw, err := json.Marshal(portHint{Path: abs, Port: port})
	if err != nil {
		return
	}
	_ = tools.WriteFileAtomic(name, raw, 0o600)
}
