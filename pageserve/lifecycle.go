package pageserve

// Starting, finding and stopping a tool's serve from outside it. Moved from
// cull's internal/cull/serve/lifecycle.go, with diazo's Ensure.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	tools "github.com/subaud-tools/tools-common"
	"github.com/subaud-tools/tools-common/localweb"
)

// Process is how a tool's serve is started in the background.
type Process struct {
	Live Live // where serve advertises itself; Live.Tool names the tool
	// Args run serve detached from the binary, e.g. serve --foreground
	// --no-open; Start adds --port N when asked for a port.
	Args []string
	// Dir is the working directory serve runs in ("": the caller's).
	Dir string
}

// newCommand builds the detached process; tests replace it.
var newCommand = exec.Command

// Start runs exe with p.Args in its own process group, logging to
// StateDir/serve.log, and waits for its entry.
func (p Process) Start(exe string, port int) (Entry, error) {
	tool := p.Live.Tool
	if exe == "" {
		return Entry{}, fmt.Errorf("can't find the %s binary to start the server", tool)
	}
	state := tools.StateDir(tool)
	if err := os.MkdirAll(state, 0o700); err != nil {
		return Entry{}, err
	}
	logPath := filepath.Join(state, "serve.log")
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return Entry{}, err
	}
	defer func() { _ = logf.Close() }()
	args := append([]string(nil), p.Args...)
	if port > 0 {
		args = append(args, "--port", strconv.Itoa(port))
	}
	cmd := newCommand(exe, args...)
	cmd.Dir = p.Dir
	cmd.Stdout, cmd.Stderr = logf, logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return Entry{}, err
	}
	pid := cmd.Process.Pid
	exited := make(chan struct{})
	go func() { _ = cmd.Wait(); close(exited) }()
	for i := 0; i < 100; i++ {
		// Any live serve will do: a concurrent starter may have won the race
		// and our child exited with "already running".
		if e, err := p.Live.Running(); err == nil {
			return e, nil
		}
		select {
		case <-exited:
			// One last look: the winner's entry may have just landed.
			if e, err := p.Live.Running(); err == nil {
				return e, nil
			}
			return Entry{}, fmt.Errorf("%s serve (pid %d) exited before it started; see %s", tool, pid, logPath)
		case <-time.After(100 * time.Millisecond):
		}
	}
	return Entry{}, fmt.Errorf("%s serve (pid %d) didn't start; see %s", tool, pid, logPath)
}

// Ensure returns the live serve's entry, starting serve from exe when none
// is running. Every channel calls it when it finds serve gone, often many at
// once after a restart, so it holds StateDir/start.lock across "find the
// entry, else start serve and wait for its entry": the next one in finds the
// live serve and uses it.
func (p Process) Ensure(exe string) (Entry, error) {
	state := tools.StateDir(p.Live.Tool)
	if err := os.MkdirAll(state, 0o700); err != nil {
		return Entry{}, err
	}
	unlock, err := lockFile(filepath.Join(state, "start.lock"))
	if err != nil {
		return Entry{}, err
	}
	defer unlock()
	if e, err := p.Live.Running(); err == nil {
		return e, nil
	}
	return p.Start(exe, 0)
}

// Stop asks the live serve to exit and waits for its entry to go.
func Stop(ctx context.Context, l Live) error {
	e, err := l.Running()
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.Base+"/api/stop", strings.NewReader("{}"))
	if err != nil {
		return err
	}
	req.Header.Set(localweb.TokenHeader, e.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	for i := 0; i < 50; i++ {
		if _, err := l.Running(); errors.Is(err, ErrNotRunning) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("%s serve (pid %d) did not stop", l.Tool, e.PID)
}

// lockFile takes an exclusive lock on path, shared with every process on the
// machine, waiting for it; unlock gives it back. The kernel lets go of a lock
// whose process died.
func lockFile(path string) (unlock func(), err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("locking %s: %w", path, err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}

// Executable is the running binary with symlinks resolved, "" when it
// can't be found: the exe a tool hands Start and Ensure.
func Executable() string {
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}
