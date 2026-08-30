package tools

import (
	"bytes"
	"io"
	"strings"
	"testing"
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
		io.WriteString(out, "custom update ran\n")
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
