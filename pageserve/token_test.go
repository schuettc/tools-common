package pageserve

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	tools "github.com/subaud-tools/tools-common"
	"github.com/subaud-tools/tools-common/localweb"
)

// A tab opened before a restart holds the old token: it still authorises.
func TestATokenSurvivesARestart(t *testing.T) {
	t.Setenv("CULL_HOME", t.TempDir())
	live := LiveFor("cull")
	r := runHere(t, Options{})
	first, err := live.Running()
	if err != nil {
		t.Fatal(err)
	}
	if err := Stop(context.Background(), live); err != nil {
		t.Fatal(err)
	}
	wait(t, r)

	r = runHere(t, Options{})
	second, err := live.Running()
	if err != nil {
		t.Fatal(err)
	}
	if second.Token != first.Token {
		t.Fatalf("the token changed across a restart: %q, then %q", first.Token, second.Token)
	}
	req, _ := http.NewRequest("GET", second.Base+"/api/ping", nil)
	req.Header.Set(localweb.TokenHeader, first.Token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the old token got %d", resp.StatusCode)
	}
	if err := Stop(context.Background(), live); err != nil {
		t.Fatal(err)
	}
	wait(t, r)
}

// Of serves started at once in an empty state directory, one serves, the rest
// refuse, and the file holds the token the winner advertises.
func TestServesStartedAtOnceUseOneToken(t *testing.T) {
	t.Setenv("CULL_HOME", t.TempDir())
	live := LiveFor("cull")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan struct{})
	var once sync.Once
	done := make(chan error, 2)
	for range 2 {
		go func() {
			done <- Run(ctx, Options{Live: live, Assets: testPage, API: testAPI(), Ready: func(string) { once.Do(func() { close(ready) }) }})
		}()
	}
	<-ready
	e, err := live.Running()
	if err != nil {
		t.Fatal(err)
	}
	if err := <-done; err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("the second serve: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(tools.StateDir("cull"), "token"))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(b)); got != e.Token {
		t.Fatalf("the file holds %q, the entry %q", got, e.Token)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// The first writer sets the token for every taker, however many race it.
func TestKeptTokenIsFirstWriterWins(t *testing.T) {
	dir := t.TempDir()
	got := make([]string, 16)
	errs := make([]error, 16)
	var wg sync.WaitGroup
	for i := range got {
		wg.Go(func() { got[i], errs[i] = keptToken(dir) })
	}
	wg.Wait()
	for i := range got {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if got[i] != got[0] {
			t.Fatalf("taker %d got %q, taker 0 %q", i, got[i], got[0])
		}
	}
	st, err := os.Stat(filepath.Join(dir, "token"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("token file %v", st.Mode().Perm())
	}
	es, _ := os.ReadDir(dir)
	if len(es) != 1 {
		t.Fatalf("left behind: %v", es)
	}
}

// A file that holds anything but a token is replaced, and the new one kept.
func TestKeptTokenReplacesAFileThatIsNotAToken(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "token"), []byte("not a token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := keptToken(dir)
	if err != nil || len(a) != 64 {
		t.Fatalf("%q %v", a, err)
	}
	b, err := keptToken(dir)
	if err != nil || b != a {
		t.Fatalf("%q, then %q: %v", a, b, err)
	}
}

// A token another account could read is no secret: it is replaced, and the new
// file is private.
func TestKeptTokenReplacesAFileOthersCanRead(t *testing.T) {
	state := t.TempDir()
	path := filepath.Join(state, "token")
	loose := strings.Repeat("ab", 32)
	if err := os.WriteFile(path, []byte(loose+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := keptToken(state)
	if err != nil {
		t.Fatal(err)
	}
	if got == loose {
		t.Fatal("kept a token others could read")
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o, want 600", st.Mode().Perm())
	}
}
