package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const testToken = "sbt_6BJIVEOXHYYRWEJUT6PJNAR7SJNZANJUJQS5ISSFCMQOITF6SUHQ"

// fakeAPI answers start, and each poll with the next of its answers.
type fakeAPI struct {
	mu      sync.Mutex
	answers []string // poll statuses; "allowed" hands over testToken
	started map[string]string
	removed []string // bearer tokens DELETE /machine/self was called with
}

func (f *fakeAPI) handler(t *testing.T) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /machine/start", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		_ = json.NewDecoder(r.Body).Decode(&f.started)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code": "DEVICECODE", "user_code": "ZJQG-NLSQ",
			"url": "https://subaud.tools/account/machine", "interval": 5, "expires_in": 600,
		})
	})
	mux.HandleFunc("POST /machine/poll", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		var in struct {
			DeviceCode string `json:"device_code"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		if in.DeviceCode != "DEVICECODE" || len(f.answers) == 0 {
			t.Errorf("poll %q with %d answers left", in.DeviceCode, len(f.answers))
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "expired"})
			return
		}
		status := f.answers[0]
		f.answers = f.answers[1:]
		out := map[string]string{"status": status}
		if status == "allowed" {
			out["token"], out["account"] = testToken, "court@example.test"
		}
		_ = json.NewEncoder(w).Encode(out)
	})
	mux.HandleFunc("DELETE /machine/self", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.removed = append(f.removed, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		_ = json.NewEncoder(w).Encode(map[string]string{"removed": "m1"})
	})
	return mux
}

// client returns a Client on a fake API with polls answered in turn, its
// token file in a temp dir and its waits recorded instead of slept.
func client(t *testing.T, answers ...string) (*Client, *fakeAPI, *[]time.Duration) {
	t.Helper()
	f := &fakeAPI{answers: answers}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	var waits []time.Duration
	return &Client{
		API:   srv.URL,
		HTTP:  srv.Client(),
		Path:  filepath.Join(t.TempDir(), "subaud", "token"),
		Sleep: func(d time.Duration) { waits = append(waits, d) },
	}, f, &waits
}

func TestLoginWritesTheTokenOnceAllowed(t *testing.T) {
	c, f, _ := client(t, "pending", "allowed")
	var out bytes.Buffer
	if err := c.Login(context.Background(), "courts-mbp", "darwin", &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Go to https://subaud.tools/account/machine and type ZJQG-NLSQ\n") ||
		!strings.Contains(out.String(), "court@example.test") {
		t.Fatalf("out = %q", out.String())
	}
	if f.started["name"] != "courts-mbp" || f.started["os"] != "darwin" {
		t.Fatalf("started = %v", f.started)
	}
	b, err := os.ReadFile(c.Path)
	if err != nil || strings.TrimSpace(string(b)) != testToken {
		t.Fatalf("token file = %q, %v", b, err)
	}
}

func TestTheTokenFileIsPrivate(t *testing.T) {
	c, _, _ := client(t, "allowed")
	if err := c.Login(context.Background(), "courts-mbp", "darwin", &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(c.Path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("token file mode %v", info.Mode().Perm())
	}
}

func TestADeniedLoginWritesNothing(t *testing.T) {
	c, _, _ := client(t, "pending", "denied")
	err := c.Login(context.Background(), "courts-mbp", "darwin", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(c.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("token file: %v", err)
	}
}

func TestAnExpiredLoginWritesNothing(t *testing.T) {
	c, _, _ := client(t, "pending", "expired")
	err := c.Login(context.Background(), "courts-mbp", "darwin", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(c.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("token file: %v", err)
	}
}

func TestSlowDownLengthensTheWait(t *testing.T) {
	c, _, waits := client(t, "pending", "slow_down", "pending", "allowed")
	if err := c.Login(context.Background(), "courts-mbp", "darwin", &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{5 * time.Second, 5 * time.Second, 10 * time.Second, 10 * time.Second}
	if len(*waits) != len(want) {
		t.Fatalf("waits = %v", *waits)
	}
	for i := range want {
		if (*waits)[i] != want[i] {
			t.Fatalf("waits = %v, want %v", *waits, want)
		}
	}
}

func TestLogoutRemovesTheMachineAndTheFile(t *testing.T) {
	c, f, _ := client(t, "allowed")
	if err := c.Login(context.Background(), "courts-mbp", "darwin", &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if err := c.Logout(context.Background(), &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if len(f.removed) != 1 || f.removed[0] != testToken {
		t.Fatalf("removed = %v", f.removed)
	}
	if _, err := os.Stat(c.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("token file: %v", err)
	}
}

func TestTheEnvironmentTokenWinsOverTheFile(t *testing.T) {
	t.Setenv("SUBAUD_HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(TokenPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(TokenPath(), []byte("sbt_FROMFILE\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(TokenEnv, "")
	if got, err := Token(); err != nil || got != "sbt_FROMFILE" {
		t.Fatalf("Token() = %q, %v", got, err)
	}
	t.Setenv(TokenEnv, "sbt_FROMENV")
	if got, err := Token(); err != nil || got != "sbt_FROMENV" {
		t.Fatalf("Token() = %q, %v", got, err)
	}
}
