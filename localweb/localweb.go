// Package localweb is the shell every family local page needs: a loopback-only
// HTTP server with a per-launch token, DNS-rebinding and cross-site request
// guards, embedded assets, and a remembered port. The page itself is the
// tool's. See the tools-common v0.4.0 spec §1.4 for the security model.
package localweb

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tools "github.com/subaud-tools/tools-common"
)

// TokenHeader is how non-browser clients (CLI, channel process) authenticate.
const TokenHeader = "X-Local-Token" //nolint:gosec // G101: an HTTP header name, not a credential

// shutdownWait is how long a stopping server waits for its requests to finish.
const shutdownWait = 2 * time.Second

// Config configures Start.
type Config struct {
	Tool   string       // names the cookie and the port-hint dir (StateDir(Tool))
	Assets fs.FS        // served at / without a token
	API    http.Handler // mounted at /api/, token required; nil → 404
	Port   int          // >0: exactly this port; 0: remembered port, else free
	// Token, when set, is used instead of a new one, so a tool that keeps it
	// across restarts keeps its open tabs working. 64 hex characters.
	Token string
}

// Server is a running local page server.
type Server struct {
	// URL is http://127.0.0.1:<port>/?t=<token>, the address to open.
	URL string
	// Token is the per-launch secret used for cookie exchange and header auth.
	Token string

	cfg  Config
	ln   net.Listener
	srv  *http.Server
	done chan error
	// shutdown closes once the shutdown has ended; stuck is set before it does.
	shutdown chan struct{}
	stuck    bool
}

// Start binds 127.0.0.1 and serves until ctx is cancelled, then lets the requests
// it is answering finish, for up to shutdownWait.
func Start(ctx context.Context, c Config) (*Server, error) {
	ln, err := listen(c)
	if err != nil {
		return nil, err
	}
	token := c.Token
	if token == "" {
		tok := make([]byte, 32)
		if _, err := rand.Read(tok); err != nil {
			_ = ln.Close()
			return nil, err
		}
		token = hex.EncodeToString(tok)
	} else if b, err := hex.DecodeString(token); err != nil || len(b) != 32 {
		_ = ln.Close()
		return nil, fmt.Errorf("localweb: Config.Token must be 64 hex characters")
	}
	s := &Server{Token: token, cfg: c, ln: ln, done: make(chan error, 1), shutdown: make(chan struct{})}
	s.URL = "http://" + ln.Addr().String() + "/?t=" + s.Token
	s.srv = &http.Server{Handler: s, ReadHeaderTimeout: 10 * time.Second}
	savePort(c.Tool, ln.Addr().(*net.TCPAddr).Port)
	go func() {
		err := s.srv.Serve(ln)
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
			<-s.shutdown // Serve returns as Shutdown starts, not as it ends
		}
		s.done <- err
	}()
	//nolint:gosec // G118: ctx is already cancelled here; Shutdown needs a fresh timeout context
	go func() {
		<-ctx.Done()
		defer close(s.shutdown)
		sctx, cancel := context.WithTimeout(context.Background(), shutdownWait)
		defer cancel()
		if err := s.srv.Shutdown(sctx); err != nil {
			s.stuck = true
			_ = s.srv.Close()
		}
	}()
	return s, nil
}

// Addr is the bound host:port.
func (s *Server) Addr() string { return s.ln.Addr().String() }

// Wait blocks until the server stops and its requests have finished, or
// shutdownWait has passed (see Stuck); nil after a clean shutdown.
func (s *Server) Wait() error { return <-s.done }

// Stuck says, once Wait has returned, that requests were still running when
// shutdownWait ran out. Their connections are closed; their handlers may not
// have returned.
func (s *Server) Stuck() bool {
	select {
	case <-s.shutdown:
		return s.stuck
	default:
		return false
	}
}

// cookieName carries the port: browsers keep cookies per host, not per
// port, so two serves of one tool on 127.0.0.1 would otherwise overwrite
// each other's sign-in.
func (s *Server) cookieName() string {
	_, port, _ := net.SplitHostPort(s.Addr())
	return s.cfg.Tool + "_token_" + port
}

// ServeHTTP applies the host guard, the first-load token exchange, API auth,
// and otherwise serves Assets.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, port, _ := net.SplitHostPort(s.Addr())
	if r.Host != "127.0.0.1:"+port && r.Host != "localhost:"+port {
		http.Error(w, "forbidden host", http.StatusForbidden)
		return
	}
	if t := r.URL.Query().Get("t"); t != "" && r.Method == http.MethodGet && s.tokenOK(t) {
		//nolint:gosec // G124: loopback http; Secure would break the cookie, HttpOnly+SameSite=Strict suffice
		http.SetCookie(w, &http.Cookie{Name: s.cookieName(), Value: s.Token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		q := r.URL.Query()
		q.Del("t")
		loc := r.URL.Path
		if enc := q.Encode(); enc != "" {
			loc += "?" + enc
		}
		//nolint:gosec // G710: loc is this request's own path, a same-origin relative redirect
		http.Redirect(w, r, loc, http.StatusSeeOther)
		return
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		s.serveAPI(w, r)
		return
	}
	if s.cfg.Assets == nil {
		http.NotFound(w, r)
		return
	}
	http.FileServerFS(s.cfg.Assets).ServeHTTP(w, r)
}

func (s *Server) serveAPI(w http.ResponseWriter, r *http.Request) {
	headerAuth := s.tokenOK(r.Header.Get(TokenHeader))
	cookieAuth := false
	if c, err := r.Cookie(s.cookieName()); err == nil {
		cookieAuth = s.tokenOK(c.Value)
	}
	if !headerAuth && !cookieAuth {
		http.Error(w, "token required", http.StatusUnauthorized)
		return
	}
	safe := r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions
	if !headerAuth && !safe && r.Header.Get("Origin") != "http://"+r.Host {
		http.Error(w, "cross-origin write refused", http.StatusForbidden)
		return
	}
	if s.cfg.API == nil {
		http.NotFound(w, r)
		return
	}
	s.cfg.API.ServeHTTP(w, r)
}

func (s *Server) tokenOK(t string) bool {
	return t != "" && subtle.ConstantTimeCompare([]byte(t), []byte(s.Token)) == 1
}

func hintPath(tool string) string { return filepath.Join(tools.StateDir(tool), "port") }

func listen(c Config) (net.Listener, error) {
	if c.Port > 0 {
		return net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(c.Port))
	}
	if b, err := os.ReadFile(hintPath(c.Tool)); err == nil {
		if p, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil && p > 0 && p < 65536 {
			if ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(p)); err == nil {
				return ln, nil
			}
		}
	}
	return net.Listen("tcp", "127.0.0.1:0")
}

func savePort(tool string, port int) {
	_ = tools.WriteFileAtomic(hintPath(tool), []byte(strconv.Itoa(port)+"\n"), 0o600)
}
