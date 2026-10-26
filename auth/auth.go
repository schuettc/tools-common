// Package auth signs a machine in to its subaud.tools account, once for
// every family tool on it. Login asks the access service for a code and
// prints where to type it; once the buyer allows the machine on the account
// page, the next Login gets the machine's own token, kept in
// ConfigDir("subaud")/token with mode 600. Token is what the downloaders
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

// ErrSignInRefused is returned by Link when the service refuses the token:
// the machine was removed on the account page, or the token is wrong.
var ErrSignInRefused = errors.New("subaud.tools refused this machine's sign-in: run `<tool> login` to sign in again")

// DefaultAccount is the account page, for a refusal that names none.
const DefaultAccount = "https://subaud.tools/account/"

// NotOwnedError is returned by Link when the account doesn't own the
// product; Account is the page to buy it on.
type NotOwnedError struct {
	Product string
	Account string
}

func (e *NotOwnedError) Error() string {
	return fmt.Sprintf("this machine's subaud.tools account doesn't own %s: get it at %s", e.Product, e.Account)
}

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
func Token() (string, error) { return (&Client{}).token() }

func (c *Client) token() (string, error) {
	if t := os.Getenv(TokenEnv); t != "" {
		return t, nil
	}
	return readToken(c.path())
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

// pending is a sign-in in progress, kept between login's two runs. The
// device code is the secret the service hands the token to: it stays in
// the file and is never printed.
type pending struct {
	DeviceCode string `json:"device_code"`
	UserCode   string `json:"user_code"`
	URL        string `json:"url"`
	Interval   int    `json:"interval"`
	// Expires is unix seconds, the form install.sh writes too, so a sign-in
	// either one starts the other can finish.
	Expires int64 `json:"expires"`
}

func (p *pending) expired() bool { return time.Now().Unix() >= p.Expires }

func (c *Client) pendingPath() string { return filepath.Join(filepath.Dir(c.path()), "pending") }

var errDenied = errors.New("the sign-in was denied on the account page: run login again to start a new one")

var errExpired = errors.New("the code expired before it was allowed: run login again")

// Login signs this machine in over two short runs, so nothing waits on a
// person. The first run starts a sign-in, saves it at PendingPath and prints
// where to type the code. The next run asks once: allowed writes the token
// file, still pending says so, and denied or expired drops the sign-in so
// the run after starts a new one.
func (c *Client) Login(ctx context.Context, name, osName string, out io.Writer) error {
	begin := func() error {
		started, err := c.start(ctx, name, osName)
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(out, "Go to %s and type %s\n", started.URL, started.UserCode)
		_, _ = fmt.Fprintln(out, "Waiting for approval: run login again once you've allowed this machine.")
		return nil
	}
	p := c.readPending()
	if p == nil {
		return begin()
	}
	// An expired code can't be allowed any more: replace it in this run, so
	// the person always has a code to type.
	if p.expired() {
		c.dropPending()
		return begin()
	}
	status, err := c.poll(ctx, p, out)
	if errors.Is(err, errExpired) {
		return begin()
	}
	if err != nil {
		return err
	}
	if status != "allowed" {
		if p.UserCode != "" {
			_, _ = fmt.Fprintf(out, "Still waiting for approval: go to %s and type %s, then run login again.\n", p.URL, p.UserCode)
		} else {
			_, _ = fmt.Fprintln(out, "Still waiting for approval: allow this machine on the account page, then run login again.")
		}
	}
	return nil
}

// LoginWait signs this machine in in one run, for a person at a terminal:
// it prints where to type the code (a sign-in already started keeps its
// code), then asks every interval until the buyer decides or the code
// expires.
func (c *Client) LoginWait(ctx context.Context, name, osName string, out io.Writer) error {
	p := c.readPending()
	// One install.sh started carries no code to show, so start afresh.
	if p == nil || p.expired() || p.UserCode == "" {
		var err error
		if p, err = c.start(ctx, name, osName); err != nil {
			return err
		}
	}
	_, _ = fmt.Fprintf(out, "Go to %s and type %s\n", p.URL, p.UserCode)
	interval := time.Duration(p.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := time.Unix(p.Expires, 0).Add(time.Minute)
	for time.Now().Before(deadline) {
		c.sleep(interval)
		status, err := c.poll(ctx, p, out)
		if err != nil {
			return err
		}
		switch status {
		case "allowed":
			return nil
		case "slow_down":
			interval += slowDownBy
		}
	}
	c.dropPending()
	return errExpired
}

// start asks the service for a code and saves it as the pending sign-in.
func (c *Client) start(ctx context.Context, name, osName string) (*pending, error) {
	var st struct {
		pending
		ExpiresIn int `json:"expires_in"`
	}
	if err := c.call(ctx, http.MethodPost, "/machine/start", "", map[string]string{"name": name, "os": osName}, &st); err != nil {
		return nil, err
	}
	p := st.pending
	p.Expires = time.Now().Add(time.Duration(st.ExpiresIn) * time.Second).Unix()
	b, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	if err := writePrivate(c.pendingPath(), b); err != nil {
		return nil, err
	}
	return &p, nil
}

// poll asks once how the pending sign-in stands. allowed writes the token
// file; denied and expired drop the sign-in and return an error; pending
// and slow_down are returned as they are.
func (c *Client) poll(ctx context.Context, p *pending, out io.Writer) (string, error) {
	var r struct {
		Status  string `json:"status"`
		Token   string `json:"token"`
		Account string `json:"account"`
	}
	if err := c.call(ctx, http.MethodPost, "/machine/poll", "", map[string]string{"device_code": p.DeviceCode}, &r); err != nil {
		return "", err
	}
	switch r.Status {
	case "pending", "slow_down":
		return r.Status, nil
	case "denied":
		c.dropPending()
		return "", errDenied
	case "expired":
		c.dropPending()
		return "", errExpired
	case "allowed":
		if r.Token == "" {
			return "", errors.New("the access service sent no token")
		}
		if err := writePrivate(c.path(), []byte(r.Token+"\n")); err != nil {
			return "", err
		}
		c.dropPending()
		_, _ = fmt.Fprintf(out, "Signed in to %s. Every subaud tool on this machine uses this sign-in.\n", r.Account)
		return r.Status, nil
	default:
		return "", fmt.Errorf("the access service answered %q", r.Status)
	}
}

// readPending returns the pending sign-in, or nil when there is none or
// the file can't be read as one.
func (c *Client) readPending() *pending {
	b, err := os.ReadFile(c.pendingPath())
	if err != nil {
		return nil
	}
	var p pending
	if json.Unmarshal(b, &p) != nil || p.DeviceCode == "" {
		return nil
	}
	return &p
}

func (c *Client) dropPending() { _ = os.Remove(c.pendingPath()) }

// Link asks the access service for a five-minute signed URL of path (e.g.
// "hail/latest" or "hail/0.4.0/hail_darwin_arm64.tar.gz") in product's
// downloads, with this machine's token, using the default Client.
func Link(ctx context.Context, product, path string) (string, error) {
	return (&Client{}).Link(ctx, product, path)
}

// Link asks the access service for a signed URL of path in product's
// downloads. With no token it asks nothing and returns ErrNotSignedIn; a
// refused token is ErrSignInRefused and a product the account doesn't own a
// *NotOwnedError.
func (c *Client) Link(ctx context.Context, product, path string) (string, error) {
	token, err := c.token()
	if err != nil {
		return "", err
	}
	var r struct {
		URL string `json:"url"`
	}
	err = c.call(ctx, http.MethodPost, "/links", token, map[string]string{"product": product, "path": path}, &r)
	var refused *apiError
	if errors.As(err, &refused) {
		switch refused.status {
		case http.StatusUnauthorized:
			return "", ErrSignInRefused
		case http.StatusForbidden:
			account := refused.account
			if account == "" {
				account = DefaultAccount
			}
			return "", &NotOwnedError{Product: product, Account: account}
		}
	}
	if err != nil {
		return "", err
	}
	if r.URL == "" {
		return "", errors.New("the access service sent no link")
	}
	return r.URL, nil
}

// Logout removes this machine from its account and deletes the token file.
func (c *Client) Logout(ctx context.Context, out io.Writer) error {
	// A sign-in still waiting would otherwise sign the machine back in on the
	// next login with an approval given before this logout.
	c.dropPending()
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
	status  int
	code    string
	account string
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
			Error   string `json:"error"`
			Account string `json:"account"`
		}
		_ = json.Unmarshal(b, &e)
		return &apiError{status: resp.StatusCode, code: e.Error, account: e.Account}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("reading the access service's answer: %w", err)
	}
	return nil
}

// writePrivate replaces path with data, mode 600, in a 700 directory.
func writePrivate(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, "."+filepath.Base(path)+"-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(f.Name()) }()
	if _, err := f.Write(data); err != nil {
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
