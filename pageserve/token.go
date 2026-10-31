package pageserve

// The kept token. Moved from diazo's internal/serve/token.go.

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	tools "github.com/subaud-tools/tools-common"
)

// keptToken returns the token in state/token (0600), making one the first time
// (or when the file holds anything but 64 hex characters), so a tab opened
// before a restart keeps working after it. The entry goes on stop; this file
// stays. Run takes it holding serve.lock, and of takers racing without it the
// first to write the file sets the token every one of them uses.
func keptToken(state string) (string, error) {
	path := filepath.Join(state, "token")
	if t, ok := readToken(path); ok {
		return t, nil
	}
	tok := make([]byte, 32)
	if _, err := rand.Read(tok); err != nil {
		return "", err
	}
	t := hex.EncodeToString(tok)
	tmp := path + "." + hex.EncodeToString(tok[:4]) + ".new"
	if err := os.WriteFile(tmp, []byte(t+"\n"), 0o600); err != nil {
		return "", err
	}
	defer func() { _ = os.Remove(tmp) }()
	// A link fails when the file is there: another serve wrote it first.
	if err := os.Link(tmp, path); err == nil {
		return t, nil
	}
	if t, ok := readToken(path); ok {
		return t, nil
	}
	// The file there is not a token: replace it.
	if err := tools.WriteFileAtomic(path, []byte(t+"\n"), 0o600); err != nil {
		return "", err
	}
	return t, nil
}

// readToken is the token in path, when it holds one.
func readToken(path string) (string, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	t := strings.TrimSpace(string(b))
	if d, err := hex.DecodeString(t); err == nil && len(d) == 32 {
		return t, true
	}
	return "", false
}
