package tools

import (
	"bytes"
	"encoding/json"
	"flag"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/subaud-tools/tools-common/auth"
)

func newTestApp() *App {
	return New(Config{Name: "kempt", Domain: "kempt.tools", Version: Version{Number: "0.1.0", Commit: "abc"}})
}

func TestDispatch(t *testing.T) {
	tests := []struct {
		name     string
		register *Command
		args     []string
		wantCode int
		wantErr  string
		wantOut  string
	}{
		{name: "no args", args: nil, wantCode: 2, wantErr: "usage: kempt"},
		{name: "unknown", args: []string{"bogus"}, wantCode: 2, wantErr: `kempt: unknown command "bogus"`},
		{
			name:     "ok command",
			register: &Command{Name: "ok", Run: func(a []string, out, errw io.Writer) error { return nil }},
			args:     []string{"ok"},
			wantCode: 0,
		},
		{
			name:     "usage error",
			register: &Command{Name: "u", Run: func(a []string, out, errw io.Writer) error { return UsageError{Msg: "need arg"} }},
			args:     []string{"u"},
			wantCode: 2,
			wantErr:  "kempt u: need arg",
		},
		{
			name:     "runtime error",
			register: &Command{Name: "r", Run: func(a []string, out, errw io.Writer) error { return io.EOF }},
			args:     []string{"r"},
			wantCode: 1,
			wantErr:  "kempt r: EOF",
		},
		{name: "--version alias", args: []string{"--version"}, wantCode: 0, wantOut: "kempt "},
		{name: "-h alias", args: []string{"-h"}, wantCode: 0, wantOut: "usage: kempt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newTestApp()
			if tt.register != nil {
				a.Register(*tt.register)
			}
			var out, errw bytes.Buffer
			code := a.Dispatch(tt.args, &out, &errw)
			if code != tt.wantCode {
				t.Errorf("code = %d, want %d", code, tt.wantCode)
			}
			if tt.wantErr != "" && !strings.Contains(errw.String(), tt.wantErr) {
				t.Errorf("stderr = %q, want contains %q", errw.String(), tt.wantErr)
			}
			if tt.wantOut != "" && !strings.Contains(out.String(), tt.wantOut) {
				t.Errorf("stdout = %q, want contains %q", out.String(), tt.wantOut)
			}
		})
	}
}

func TestRegisterOverridesBuiltin(t *testing.T) {
	a := newTestApp()
	a.Register(Command{Name: "update", Run: func(args []string, out, errw io.Writer) error {
		_, _ = io.WriteString(out, "custom update ran\n")
		return nil
	}})
	var out, errw bytes.Buffer
	code := a.Dispatch([]string{"update"}, &out, &errw)
	if code != 0 {
		t.Fatalf("code = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "custom update ran") {
		t.Errorf("stdout = %q, want custom update", out.String())
	}
}

func TestDispatchJSONErrorEnvelope(t *testing.T) {
	a := newTestApp()
	a.Register(Command{Name: "p", Run: func(_ []string, _, _ io.Writer) error {
		return Exitf(4, "the editor stopped").WithHint("start `galley edit`")
	}})
	var out, errw bytes.Buffer
	code := a.Dispatch([]string{"p", "--json"}, &out, &errw)
	if code != 4 {
		t.Fatalf("code = %d, want 4", code)
	}
	var got map[string]any
	if err := json.Unmarshal(errw.Bytes(), &got); err != nil {
		t.Fatalf("stderr not JSON: %q (%v)", errw.String(), err)
	}
	if got["error"] != "the editor stopped" || got["hint"] != "start `galley edit`" || got["code"].(float64) != 4 {
		t.Fatalf("envelope wrong: %v", got)
	}
}

func TestCommandRichFieldsAndGroups(t *testing.T) {
	a := New(Config{
		Name: "kempt", Domain: "kempt.tools",
		Version: Version{Number: "0.3.0"},
		Groups:  []Group{{Key: "core", Heading: "Core"}},
	})
	a.Register(Command{
		Name: "apply", Summary: "converge", Synopsis: "apply [flags]",
		Help: "Applies the plan.", Group: "core",
		NewFlags: func() *flag.FlagSet { return flag.NewFlagSet("apply", flag.ContinueOnError) },
		Run:      func(_ []string, _, _ io.Writer) error { return nil },
	})
	if got := a.groupList(); len(got) != 1 || got[0].Heading != "Core" {
		t.Fatalf("groups = %v", got)
	}
	// A bare {Name,Summary,Run} command still registers and runs.
	var out, errw bytes.Buffer
	if code := a.Dispatch([]string{"apply"}, &out, &errw); code != 0 {
		t.Fatalf("code = %d", code)
	}
}

func TestDispatchHelpForCommand(t *testing.T) {
	a := newTestApp()
	a.Register(Command{
		Name: "serve", Summary: "serve", Synopsis: "serve <page>",
		Help: "Serves it.", Run: func(_ []string, _, _ io.Writer) error { return nil },
	})
	for _, arg := range []string{"-h", "--help"} {
		var out, errw bytes.Buffer
		code := a.Dispatch([]string{"serve", arg}, &out, &errw)
		if code != 0 {
			t.Fatalf("%s: code %d", arg, code)
		}
		if !strings.Contains(out.String(), "Usage: kempt serve <page>") || !strings.Contains(out.String(), "Serves it.") {
			t.Fatalf("%s: help wrong: %q", arg, out.String())
		}
	}
}

func TestHelpSubcommandForNamed(t *testing.T) {
	a := newTestApp()
	a.Register(Command{Name: "serve", Summary: "serve", Help: "Serves it.", Run: func(_ []string, _, _ io.Writer) error { return nil }})
	var out, errw bytes.Buffer
	if code := a.Dispatch([]string{"help", "serve"}, &out, &errw); code != 0 {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(out.String(), "Serves it.") {
		t.Fatalf("help serve wrong: %q", out.String())
	}
}

func TestDispatchHelpForCommandWithoutHelpText(t *testing.T) {
	a := newTestApp()
	a.Register(Command{
		Name: "query", Summary: "query", Synopsis: "query [flags]",
		NewFlags: func() *flag.FlagSet {
			fs := flag.NewFlagSet("query", flag.ContinueOnError)
			fs.String("filter", "", "filter results")
			return fs
		},
		Run: func(_ []string, _, _ io.Writer) error { return nil },
	})
	for _, arg := range []string{"-h", "--help"} {
		var out, errw bytes.Buffer
		code := a.Dispatch([]string{"query", arg}, &out, &errw)
		if code != 0 {
			t.Fatalf("%s: code %d", arg, code)
		}
		s := out.String()
		if !strings.Contains(s, "Usage: kempt query [flags]") || !strings.Contains(s, "-filter") {
			t.Fatalf("%s: help wrong: %q", arg, s)
		}
	}
}

func TestManBuiltin(t *testing.T) {
	a := newTestApp()
	a.Register(Command{Name: "send", Summary: "send", Help: "Sends it.", Run: func(_ []string, _, _ io.Writer) error { return nil }})
	var out, errw bytes.Buffer
	if code := a.Dispatch([]string{"man"}, &out, &errw); code != 0 {
		t.Fatalf("code %d", code)
	}
	if !strings.Contains(out.String(), ".TH KEMPT 1") {
		t.Fatalf("man output wrong: %q", out.String())
	}
}

func TestCommandsBuiltinJSON(t *testing.T) {
	a := newTestApp()
	a.Register(Command{Name: "serve", Summary: "serve", Run: func(_ []string, _, _ io.Writer) error { return nil }})
	var out, errw bytes.Buffer
	if code := a.Dispatch([]string{"commands", "--json"}, &out, &errw); code != 0 {
		t.Fatalf("code %d", code)
	}
	var got []map[string]any
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not JSON: %q", out.String())
	}
	names := map[string]bool{}
	for _, c := range got {
		names[c["name"].(string)] = true
	}
	if !names["serve"] || !names["help"] || !names["man"] || !names["commands"] {
		t.Fatalf("index missing commands: %v", names)
	}
}

func TestSelfRoutedRefused(t *testing.T) {
	a := newTestApp()
	a.Register(Command{Name: "daemon", Summary: "run the daemon", Run: nil}) // self-routed
	var out, errw bytes.Buffer
	code := a.Dispatch([]string{"daemon"}, &out, &errw)
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
	if !strings.Contains(errw.String(), "daemon") {
		t.Fatalf("refusal should name the command: %q", errw.String())
	}
	// but it IS listed in the index
	var out2, errw2 bytes.Buffer
	a.Dispatch([]string{"commands", "--json"}, &out2, &errw2)
	if !strings.Contains(out2.String(), "daemon") {
		t.Fatalf("self-routed command should be listed: %q", out2.String())
	}
}

// Every tool inherits login and logout, one sign-in for the machine.
func TestLoginAndLogoutAreBuiltIn(t *testing.T) {
	var started map[string]string
	removed := 0
	mux := http.NewServeMux()
	mux.HandleFunc("POST /machine/start", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&started)
		_, _ = io.WriteString(w, `{"device_code":"D","user_code":"ZJQG-NLSQ","url":"https://subaud.tools/account/machine","interval":5,"expires_in":600}`)
	})
	mux.HandleFunc("POST /machine/poll", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"status":"allowed","token":"sbt_T","account":"court@example.test"}`)
	})
	mux.HandleFunc("DELETE /machine/self", func(w http.ResponseWriter, r *http.Request) {
		removed++
		_, _ = io.WriteString(w, `{"removed":"m1"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := newTestApp()
	path := filepath.Join(t.TempDir(), "token")
	a.auth = &auth.Client{API: srv.URL, Path: path, Sleep: func(time.Duration) {}}
	var out, errw bytes.Buffer
	if code := a.Dispatch([]string{"login", "--name", "ci-box"}, &out, &errw); code != 0 {
		t.Fatalf("login: exit %d: %s", code, errw.String())
	}
	if started["name"] != "ci-box" || started["os"] != runtime.GOOS || !strings.Contains(out.String(), "type ZJQG-NLSQ") {
		t.Fatalf("started %v, out %q", started, out.String())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("token file after the first run: %v", err)
	}
	if code := a.Dispatch([]string{"login"}, &out, &errw); code != 0 {
		t.Fatalf("login again: exit %d: %s", code, errw.String())
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if code := a.Dispatch([]string{"logout"}, &out, &errw); code != 0 || removed != 1 {
		t.Fatalf("logout: exit %d, removed %d: %s", code, removed, errw.String())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("token file after logout: %v", err)
	}
}

// login --wait signs in in one run, polling until the machine is allowed.
func TestLoginWaitSignsInInOneRun(t *testing.T) {
	polls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("POST /machine/start", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"device_code":"D","user_code":"ZJQG-NLSQ","url":"https://subaud.tools/account/machine","interval":5,"expires_in":600}`)
	})
	mux.HandleFunc("POST /machine/poll", func(w http.ResponseWriter, r *http.Request) {
		polls++
		if polls < 2 {
			_, _ = io.WriteString(w, `{"status":"pending"}`)
			return
		}
		_, _ = io.WriteString(w, `{"status":"allowed","token":"sbt_T","account":"court@example.test"}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := newTestApp()
	path := filepath.Join(t.TempDir(), "token")
	a.auth = &auth.Client{API: srv.URL, Path: path, Sleep: func(time.Duration) {}}
	var out, errw bytes.Buffer
	if code := a.Dispatch([]string{"login", "--wait"}, &out, &errw); code != 0 || polls != 2 {
		t.Fatalf("login --wait: exit %d, polls %d: %s", code, polls, errw.String())
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
