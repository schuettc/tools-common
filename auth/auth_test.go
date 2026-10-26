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

func TestLoginWaitWritesTheTokenOnceAllowed(t *testing.T) {
	c, f, _ := client(t, "pending", "allowed")
	var out bytes.Buffer
	if err := c.LoginWait(context.Background(), "courts-mbp", "darwin", &out); err != nil {
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
	if err := c.LoginWait(context.Background(), "courts-mbp", "darwin", &bytes.Buffer{}); err != nil {
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
	err := c.LoginWait(context.Background(), "courts-mbp", "darwin", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(c.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("token file: %v", err)
	}
}

func TestAnExpiredLoginWritesNothing(t *testing.T) {
	c, _, _ := client(t, "pending", "expired")
	err := c.LoginWait(context.Background(), "courts-mbp", "darwin", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(c.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("token file: %v", err)
	}
}

func TestSlowDownLengthensTheWait(t *testing.T) {
	c, _, waits := client(t, "pending", "slow_down", "pending", "allowed")
	if err := c.LoginWait(context.Background(), "courts-mbp", "darwin", &bytes.Buffer{}); err != nil {
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
	if err := c.LoginWait(context.Background(), "courts-mbp", "darwin", &bytes.Buffer{}); err != nil {
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

// pendingPath is where the client keeps a sign-in in progress.
func pendingPath(c *Client) string { return filepath.Join(filepath.Dir(c.Path), "pending") }

func TestLoginStartsASignInAndExits(t *testing.T) {
	c, f, waits := client(t)
	var out bytes.Buffer
	if err := c.Login(context.Background(), "courts-mbp", "darwin", &out); err != nil {
		t.Fatal(err)
	}
	want := "Go to https://subaud.tools/account/machine and type ZJQG-NLSQ\n" +
		"Waiting for approval: run login again once you've allowed this machine.\n"
	if out.String() != want {
		t.Fatalf("out = %q, want %q", out.String(), want)
	}
	if f.started["name"] != "courts-mbp" || len(*waits) != 0 {
		t.Fatalf("started %v, waits %v", f.started, *waits)
	}
	info, err := os.Stat(pendingPath(c))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("pending file: %v, %v", info, err)
	}
	if _, err := os.Stat(c.Path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("token file: %v", err)
	}
}

func TestLoginAgainSignsInOnceAllowed(t *testing.T) {
	c, _, _ := client(t, "allowed")
	if err := c.Login(context.Background(), "courts-mbp", "darwin", &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := c.Login(context.Background(), "courts-mbp", "darwin", &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Signed in to court@example.test") || strings.Contains(out.String(), "DEVICECODE") {
		t.Fatalf("out = %q", out.String())
	}
	if b, err := os.ReadFile(c.Path); err != nil || strings.TrimSpace(string(b)) != testToken {
		t.Fatalf("token file = %q, %v", b, err)
	}
	if _, err := os.Stat(pendingPath(c)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending file: %v", err)
	}
}

func TestLoginAgainWhilePendingSaysSo(t *testing.T) {
	for _, status := range []string{"pending", "slow_down"} {
		c, _, _ := client(t, status)
		if err := c.Login(context.Background(), "courts-mbp", "darwin", &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := c.Login(context.Background(), "courts-mbp", "darwin", &out); err != nil {
			t.Fatalf("%s: %v", status, err)
		}
		if !strings.Contains(out.String(), "Still waiting for approval") || !strings.Contains(out.String(), "ZJQG-NLSQ") {
			t.Fatalf("%s: out = %q", status, out.String())
		}
		if _, err := os.Stat(pendingPath(c)); err != nil {
			t.Fatalf("%s: pending file: %v", status, err)
		}
	}
}

func TestLoginAgainAfterADenialOrExpiryStartsOver(t *testing.T) {
	for _, status := range []string{"denied", "expired"} {
		c, _, _ := client(t, status)
		if err := c.Login(context.Background(), "courts-mbp", "darwin", &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
		err := c.Login(context.Background(), "courts-mbp", "darwin", &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), status) || !strings.Contains(err.Error(), "run login again") {
			t.Fatalf("%s: err = %v", status, err)
		}
		if _, err := os.Stat(pendingPath(c)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s: pending file: %v", status, err)
		}
		if _, err := os.Stat(c.Path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s: token file: %v", status, err)
		}
	}
}

func TestAPendingSignInPastItsExpiryIsNotPolled(t *testing.T) {
	c, _, _ := client(t) // a poll would fail the test: no answers
	if err := os.MkdirAll(filepath.Dir(c.Path), 0o700); err != nil {
		t.Fatal(err)
	}
	old := `{"device_code":"DEVICECODE","user_code":"ZJQG-NLSQ","url":"https://subaud.tools/account/machine","interval":5,"expires":"2020-01-01T00:00:00Z"}`
	if err := os.WriteFile(pendingPath(c), []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}
	err := c.Login(context.Background(), "courts-mbp", "darwin", &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "run login again") {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(pendingPath(c)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending file: %v", err)
	}
}

func TestLoginWaitFinishesASignInAlreadyStarted(t *testing.T) {
	c, f, _ := client(t, "allowed")
	if err := c.Login(context.Background(), "courts-mbp", "darwin", &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	f.started = nil
	var out bytes.Buffer
	if err := c.LoginWait(context.Background(), "courts-mbp", "darwin", &out); err != nil {
		t.Fatal(err)
	}
	if f.started != nil || !strings.Contains(out.String(), "type ZJQG-NLSQ") {
		t.Fatalf("started again %v, out %q", f.started, out.String())
	}
	if _, err := os.Stat(c.Path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pendingPath(c)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pending file: %v", err)
	}
}

// linksAPI answers POST /links with status: 200 a URL for the path, else
// the access service's error body for it.
func linksAPI(t *testing.T, status int) (*Client, *[]map[string]string) {
	t.Helper()
	t.Setenv(TokenEnv, "")
	var asked []map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/links" {
			t.Errorf("%s %s", r.Method, r.URL.Path)
		}
		var in map[string]string
		_ = json.NewDecoder(r.Body).Decode(&in)
		in["bearer"] = strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		asked = append(asked, in)
		w.WriteHeader(status)
		switch status {
		case http.StatusOK:
			_ = json.NewEncoder(w).Encode(map[string]string{"url": "https://hail.tools/dl/" + in["path"] + "?Signature=S"})
		case http.StatusUnauthorized:
			_, _ = w.Write([]byte(`{"error": "sign_in", "account": "https://subaud.tools/account/"}`))
		case http.StatusForbidden:
			_, _ = w.Write([]byte(`{"error": "not_owned", "account": "https://subaud.tools/account/"}`))
		}
	}))
	t.Cleanup(srv.Close)
	c := &Client{API: srv.URL, HTTP: srv.Client(), Path: filepath.Join(t.TempDir(), "subaud", "token")}
	return c, &asked
}

func signIn(t *testing.T, c *Client) {
	t.Helper()
	if err := writePrivate(c.Path, []byte(testToken+"\n")); err != nil {
		t.Fatal(err)
	}
}

func TestLinkAsksWithTheMachinesToken(t *testing.T) {
	c, asked := linksAPI(t, http.StatusOK)
	signIn(t, c)
	url, err := c.Link(context.Background(), "hail", "hail/latest")
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://hail.tools/dl/hail/latest?Signature=S" {
		t.Fatalf("url = %q", url)
	}
	if len(*asked) != 1 || (*asked)[0]["product"] != "hail" || (*asked)[0]["path"] != "hail/latest" || (*asked)[0]["bearer"] != testToken {
		t.Fatalf("asked = %v", *asked)
	}
}

func TestLinkWithNoTokenNamesLoginAndAsksNothing(t *testing.T) {
	c, asked := linksAPI(t, http.StatusOK)
	_, err := c.Link(context.Background(), "hail", "hail/latest")
	if !errors.Is(err, ErrNotSignedIn) || !strings.Contains(err.Error(), "login") || len(*asked) != 0 {
		t.Fatalf("err = %v, asked %v", err, *asked)
	}
}

func TestARefusedTokenNamesLogin(t *testing.T) {
	c, _ := linksAPI(t, http.StatusUnauthorized)
	signIn(t, c)
	_, err := c.Link(context.Background(), "hail", "hail/latest")
	if !errors.Is(err, ErrSignInRefused) || !strings.Contains(err.Error(), "login") {
		t.Fatalf("err = %v", err)
	}
}

func TestAProductNotOwnedNamesTheAccountPage(t *testing.T) {
	c, _ := linksAPI(t, http.StatusForbidden)
	signIn(t, c)
	_, err := c.Link(context.Background(), "diazo", "diazo/latest")
	var notOwned *NotOwnedError
	if !errors.As(err, &notOwned) || notOwned.Account != "https://subaud.tools/account/" ||
		!strings.Contains(err.Error(), "diazo") || !strings.Contains(err.Error(), "https://subaud.tools/account/") {
		t.Fatalf("err = %v", err)
	}
}
