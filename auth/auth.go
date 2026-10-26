// Package auth signs a machine in to its subaud.tools account, once for
// every family tool on it. Login asks the access service for a code, prints
// where to type it and waits while the buyer allows the machine on the
// account page; the service then hands over the machine's own token, kept
// in ConfigDir("subaud")/token with mode 600. Token is what the downloaders
// send.
//
// stdlib-only, and it doesn't import tools-common's root package, which
// registers login and logout from here.
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultAPI is the access service.
const DefaultAPI = "https://api.subaud.tools"

// TokenEnv names the environment variable whose token wins over the file,
// for CI runners.
const TokenEnv = "SUBAUD_TOKEN" //nolint:gosec // a variable's name, not a credential

// ErrNotSignedIn is returned by Token when there is no token.
var ErrNotSignedIn = errors.New("this machine is not signed in to subaud.tools: run `<tool> login`")

// TokenPath is ConfigDir("subaud")/token, by the same rules as
// tools.ConfigDir: an absolute $SUBAUD_HOME/config, else an absolute
// $XDG_CONFIG_HOME/subaud, else ~/.config/subaud.
func TokenPath() string {
	if h := os.Getenv("SUBAUD_HOME"); filepath.IsAbs(h) {
		return filepath.Join(h, "config", "token")
	}
	if b := os.Getenv("XDG_CONFIG_HOME"); filepath.IsAbs(b) {
		return filepath.Join(b, "subaud", "token")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(os.TempDir(), "subaud", "config", "token")
	}
	return filepath.Join(home, ".config", "subaud", "token")
}

// Token returns $SUBAUD_TOKEN if set, else the token file's;
// ErrNotSignedIn when there is neither.
func Token() (string, error) {
	if t := os.Getenv(TokenEnv); t != "" {
		return t, nil
	}
	return readToken(TokenPath())
}

func readToken(path string) (string, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNotSignedIn
	}
	if err != nil {
		return "", err
	}
	t := strings.TrimSpace(string(b))
	if t == "" {
		return "", ErrNotSignedIn
	}
	return t, nil
}

// Client talks to the access service. The zero value is ready to use.
type Client struct {
	API   string              // "" → DefaultAPI
	HTTP  *http.Client        // nil → a client with a 30s timeout
	Path  string              // the token file; "" → TokenPath()
	Sleep func(time.Duration) // nil → time.Sleep
}

func (c *Client) api() string {
	if c.API != "" {
		return strings.TrimRight(c.API, "/")
	}
	return DefaultAPI
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *Client) path() string {
	if c.Path != "" {
		return c.Path
	}
	return TokenPath()
}

func (c *Client) sleep(d time.Duration) {
	if c.Sleep != nil {
		c.Sleep(d)
		return
	}
	time.Sleep(d)
}

// slowDownBy is how much longer the tool waits after a slow_down.
const slowDownBy = 5 * time.Second

// Login signs this machine in: it prints where to type the code, waits
// while the buyer decides, then writes the token file and says which
// account the machine is signed in to.
func (c *Client) Login(ctx context.Context, name, osName string, out io.Writer) error {
	var st struct {
		DeviceCode string `json:"device_code"`
		UserCode   string `json:"user_code"`
		URL        string `json:"url"`
		Interval   int    `json:"interval"`
		ExpiresIn  int    `json:"expires_in"`
	}
	if err := c.call(ctx, http.MethodPost, "/machine/start", "", map[string]string{"name": name, "os": osName}, &st); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(out, "Go to %s and type %s\n", st.URL, st.UserCode)
	interval := time.Duration(st.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(st.ExpiresIn)*time.Second + time.Minute)
	for time.Now().Before(deadline) {
		c.sleep(interval)
		var p struct {
			Status  string `json:"status"`
			Token   string `json:"token"`
			Account string `json:"account"`
		}
		if err := c.call(ctx, http.MethodPost, "/machine/poll", "", map[string]string{"device_code": st.DeviceCode}, &p); err != nil {
			return err
		}
		switch p.Status {
		case "pending":
		case "slow_down":
			interval += slowDownBy
		case "denied":
			return errors.New("the sign-in was denied on the account page")
		case "expired":
			return errors.New("the code expired before it was allowed: run login again")
		case "allowed":
			if err := writeToken(c.path(), p.Token); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(out, "Signed in to %s. Every subaud tool on this machine uses this sign-in.\n", p.Account)
			return nil
		default:
			return fmt.Errorf("the access service answered %q", p.Status)
		}
	}
	return errors.New("the code expired before it was allowed: run login again")
}

// Logout removes this machine from its account and deletes the token file.
func (c *Client) Logout(ctx context.Context, out io.Writer) error {
	token, err := readToken(c.path())
	if errors.Is(err, ErrNotSignedIn) {
		_, _ = fmt.Fprintln(out, "This machine is not signed in.")
		return nil
	}
	if err != nil {
		return err
	}
	err = c.call(ctx, http.MethodDelete, "/machine/self", token, nil, nil)
	var refused *apiError
	alreadyRemoved := errors.As(err, &refused) && refused.status == http.StatusUnauthorized // on the account page
	if err != nil && !alreadyRemoved {
		return err
	}
	if err := os.Remove(c.path()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_, _ = fmt.Fprintln(out, "Signed out.")
	return nil
}

// apiError is a non-200 answer of the access service.
type apiError struct {
	status int
	code   string
}

func (e *apiError) Error() string {
	if e.code != "" {
		return fmt.Sprintf("the access service answered %d %s", e.status, e.code)
	}
	return fmt.Sprintf("the access service answered %d", e.status)
}

func (c *Client) call(ctx context.Context, method, path, token string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.api()+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return fmt.Errorf("reaching the access service: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("reading the access service's answer: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(b, &e)
		return &apiError{status: resp.StatusCode, code: e.Error}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("reading the access service's answer: %w", err)
	}
	return nil
}

// writeToken replaces the token file with mode 600, in a 700 directory.
func writeToken(path, token string) error {
	if token == "" {
		return errors.New("the access service sent no token")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".token-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.WriteString(token + "\n"); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
