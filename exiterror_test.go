package tools

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestExitErrorCode(t *testing.T) {
	a := newTestApp()
	a.Register(Command{Name: "w", Run: func(_ []string, _, _ io.Writer) error {
		return Exitf(3, "timed out")
	}})
	var out, errw bytes.Buffer
	if code := a.Dispatch([]string{"w"}, &out, &errw); code != 3 {
		t.Fatalf("code = %d, want 3", code)
	}
	if !strings.Contains(errw.String(), "kempt w: timed out") {
		t.Fatalf("stderr = %q", errw.String())
	}
}

func TestExitErrorHintPrinted(t *testing.T) {
	a := newTestApp()
	a.Register(Command{Name: "p", Run: func(_ []string, _, _ io.Writer) error {
		return Exitf(1, "no thread").WithHint("run `kempt p` for the keys")
	}})
	var out, errw bytes.Buffer
	a.Dispatch([]string{"p"}, &out, &errw)
	if !strings.Contains(errw.String(), "run `kempt p` for the keys") {
		t.Fatalf("hint missing: %q", errw.String())
	}
}

func TestExitErrorZeroCodeCoercesToOne(t *testing.T) {
	a := newTestApp()
	a.Register(Command{Name: "z", Run: func(_ []string, _, _ io.Writer) error {
		return &ExitError{Code: 0, Msg: "bad"}
	}})
	var out, errw bytes.Buffer
	if code := a.Dispatch([]string{"z"}, &out, &errw); code != 1 {
		t.Fatalf("code = %d, want 1 (0 coerces)", code)
	}
}
