package localweb

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
)

func start(t *testing.T, port int) (*Server, context.CancelFunc) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("LWTEST_HOME", "")
	api := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = fmt.Fprint(w, "api:"+r.Method) })
	ctx, cancel := context.WithCancel(context.Background())
	s, err := Start(ctx, Config{Tool: "lwtest", Assets: fstest.MapFS{"index.html": {Data: []byte("page")}}, API: api, Port: port})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = s.Wait() })
	return s, cancel
}

var noRedirect = &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

// result is do's view of a response: it reads and closes the body, so callers
// (and bodyclose) never leak a connection.
type result struct {
	Status  int
	Header  http.Header
	Cookies []*http.Cookie
	Body    string
}

func do(t *testing.T, s *Server, method, path string, mut func(*http.Request)) result {
	t.Helper()
	req, _ := http.NewRequest(method, "http://"+s.Addr()+path, nil)
	if mut != nil {
		mut(req)
	}
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return result{Status: resp.StatusCode, Header: resp.Header, Cookies: resp.Cookies(), Body: string(b)}
}

func TestBindsLoopbackOnly(t *testing.T) {
	s, _ := start(t, 0)
	host, _, _ := net.SplitHostPort(s.Addr())
	if host != "127.0.0.1" || !strings.HasPrefix(s.URL, "http://127.0.0.1:") || !strings.Contains(s.URL, "?t="+s.Token) {
		t.Fatalf("addr %s url %s", s.Addr(), s.URL)
	}
}

func TestServesAssetsWithoutToken(t *testing.T) {
	s, _ := start(t, 0)
	r := do(t, s, "GET", "/", nil)
	if r.Status != 200 || r.Body != "page" {
		t.Fatalf("%d %q", r.Status, r.Body)
	}
}

func TestAPIRequiresToken(t *testing.T) {
	s, _ := start(t, 0)
	if r := do(t, s, "GET", "/api/x", nil); r.Status != 401 {
		t.Fatalf("no token: %d", r.Status)
	}
	r := do(t, s, "POST", "/api/x", func(q *http.Request) { q.Header.Set(TokenHeader, s.Token) })
	if r.Status != 200 || r.Body != "api:POST" {
		t.Fatalf("header token: %d %q", r.Status, r.Body)
	}
}

func TestTokenQueryRedirectsAndSetsCookie(t *testing.T) {
	s, _ := start(t, 0)
	r := do(t, s, "GET", "/board?t="+s.Token, nil)
	if r.Status != http.StatusSeeOther || r.Header.Get("Location") != "/board" {
		t.Fatalf("status %d location %q", r.Status, r.Header.Get("Location"))
	}
	var ck *http.Cookie
	for _, c := range r.Cookies {
		if c.Name == "lwtest_token" {
			ck = c
		}
	}
	if ck == nil || ck.Value != s.Token || !ck.HttpOnly || ck.SameSite != http.SameSiteStrictMode {
		t.Fatalf("cookie %+v", ck)
	}
	if r := do(t, s, "GET", "/board?t=wrong", nil); r.Status == http.StatusSeeOther {
		t.Fatal("wrong token must not set a cookie")
	}
}

func TestCookieAuthNeedsSameOriginForWrites(t *testing.T) {
	s, _ := start(t, 0)
	withCookie := func(q *http.Request) { q.AddCookie(&http.Cookie{Name: "lwtest_token", Value: s.Token}) }
	if r := do(t, s, "GET", "/api/x", withCookie); r.Status != 200 {
		t.Fatalf("cookie GET: %d", r.Status)
	}
	if r := do(t, s, "POST", "/api/x", withCookie); r.Status != 403 {
		t.Fatalf("cookie POST without Origin: %d", r.Status)
	}
	bad := func(q *http.Request) { withCookie(q); q.Header.Set("Origin", "http://evil.example") }
	if r := do(t, s, "POST", "/api/x", bad); r.Status != 403 {
		t.Fatalf("cross-origin POST: %d", r.Status)
	}
	good := func(q *http.Request) { withCookie(q); q.Header.Set("Origin", "http://"+s.Addr()) }
	if r := do(t, s, "POST", "/api/x", good); r.Status != 200 {
		t.Fatalf("same-origin POST: %d", r.Status)
	}
}

func TestRejectsForeignHost(t *testing.T) {
	s, _ := start(t, 0)
	_, port, _ := net.SplitHostPort(s.Addr())
	r := do(t, s, "GET", "/api/x", func(q *http.Request) {
		q.Host = "evil.example:" + port
		q.Header.Set(TokenHeader, s.Token)
	})
	if r.Status != 403 {
		t.Fatalf("rebinding host: %d", r.Status)
	}
	r = do(t, s, "GET", "/", func(q *http.Request) { q.Host = "localhost:" + port })
	if r.Status != 200 {
		t.Fatalf("localhost host: %d", r.Status)
	}
}

func TestPortIsRememberedAndReused(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	s1, err := Start(ctx, Config{Tool: "lwtest", Assets: fstest.MapFS{}})
	if err != nil {
		t.Fatal(err)
	}
	addr := s1.Addr()
	cancel()
	_ = s1.Wait()
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	s2, err := Start(ctx2, Config{Tool: "lwtest", Assets: fstest.MapFS{}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { cancel2(); _ = s2.Wait() }()
	if s2.Addr() != addr {
		t.Fatalf("port not reused: %s then %s", addr, s2.Addr())
	}
}

func TestPortHintFallback(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	hint := filepath.Join(state, "lwtest", "port")
	_ = os.MkdirAll(filepath.Dir(hint), 0o700)

	_ = os.WriteFile(hint, []byte("not-a-port"), 0o600)
	ctx, cancel := context.WithCancel(context.Background())
	s, err := Start(ctx, Config{Tool: "lwtest", Assets: fstest.MapFS{}})
	if err != nil {
		t.Fatalf("garbage hint: %v", err)
	}
	cancel()
	_ = s.Wait()

	busy, _ := net.Listen("tcp", "127.0.0.1:0")
	defer func() { _ = busy.Close() }()
	_ = os.WriteFile(hint, []byte(strconv.Itoa(busy.Addr().(*net.TCPAddr).Port)), 0o600)
	ctx2, cancel2 := context.WithCancel(context.Background())
	s2, err := Start(ctx2, Config{Tool: "lwtest", Assets: fstest.MapFS{}})
	if err != nil {
		t.Fatalf("busy hint: %v", err)
	}
	if s2.Addr() == busy.Addr().String() {
		t.Fatal("reused a busy port")
	}
	cancel2()
	_ = s2.Wait()
}

func TestExplicitBusyPortErrors(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	busy, _ := net.Listen("tcp", "127.0.0.1:0")
	defer func() { _ = busy.Close() }()
	_, err := Start(context.Background(), Config{Tool: "lwtest", Assets: fstest.MapFS{}, Port: busy.Addr().(*net.TCPAddr).Port})
	if err == nil {
		t.Fatal("explicit busy port must error")
	}
}

func TestFreePort(t *testing.T) {
	p, err := FreePort()
	if err != nil || p <= 0 {
		t.Fatalf("%d %v", p, err)
	}
}

func TestNilAssetsServe404(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("LWTEST_HOME", "")
	ctx, cancel := context.WithCancel(context.Background())
	s, err := Start(ctx, Config{Tool: "lwtest"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = s.Wait() })
	_, port, _ := net.SplitHostPort(s.Addr())
	req, _ := http.NewRequest("GET", "http://"+s.Addr()+"/", nil)
	req.Host = "127.0.0.1:" + port
	resp, err := noRedirect.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("nil Assets: got %d, want 404", resp.StatusCode)
	}
}
