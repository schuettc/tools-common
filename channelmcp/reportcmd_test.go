package channelmcp

import (
	"bytes"
	"errors"
	"testing"

	tools "github.com/subaud-tools/tools-common"
)

type delivered struct {
	Session string
	Event   ReportEvent
	Token   string
}

// reportApp mounts ReportCommand the one way a tool does and records what it
// delivers.
func reportApp(t *testing.T, fail error) (*tools.App, *[]delivered) {
	t.Helper()
	var got []delivered
	app := tools.New(tools.Config{Name: "diazo", Domain: "diazo.tools"})
	app.Register(ReportCommand(func(session string, e ReportEvent, token string) error {
		got = append(got, delivered{session, e, token})
		return fail
	}))
	return app, &got
}

func claudeSession(t *testing.T, id string) {
	t.Helper()
	t.Setenv("AGENT_SESSION_CHILD", "")
	t.Setenv("AGENT_SESSION_ID", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", id)
}

func TestReportCommandDeliversLandedAndTurnEnded(t *testing.T) {
	claudeSession(t, "cc-1")
	app, got := reportApp(t, nil)
	var out, errw bytes.Buffer
	if code := app.Dispatch([]string{"report", "landed", "t7"}, &out, &errw); code != 0 {
		t.Fatalf("report landed exit %d: %s", code, errw.String())
	}
	if code := app.Dispatch([]string{"report", "turn-ended"}, &out, &errw); code != 0 {
		t.Fatalf("report turn-ended exit %d: %s", code, errw.String())
	}
	want := []delivered{{"cc-1", ReportLanded, "t7"}, {"cc-1", ReportTurnEnded, ""}}
	if len(*got) != 2 || (*got)[0] != want[0] || (*got)[1] != want[1] {
		t.Fatalf("delivered %+v, want %+v", *got, want)
	}
	if out.Len() != 0 {
		t.Errorf("report printed %q; it prints nothing", out.String())
	}
}

// A bridge child's turns belong to the hosting pi session, whose channel
// reports for it: the command delivers nothing, as watch hears nothing.
func TestReportCommandIsQuietInABridgeChild(t *testing.T) {
	t.Setenv("CLAUDE_CODE_SESSION_ID", "cc-1")
	t.Setenv("AGENT_SESSION_ID", "pi-1")
	t.Setenv("AGENT_SESSION_CHILD", "1")
	app, got := reportApp(t, nil)
	if code := app.Dispatch([]string{"report", "turn-ended"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if len(*got) != 0 {
		t.Fatalf("a bridge child delivered %+v", *got)
	}
}

func TestReportCommandRefusesBadArguments(t *testing.T) {
	claudeSession(t, "cc-1")
	app, got := reportApp(t, nil)
	for _, args := range [][]string{{}, {"landed"}, {"landed", ""}, {"landed", "t7", "x"}, {"turn-ended", "t7"}, {"held", "t7"}, {"turn_ended"}} {
		if code := app.Dispatch(append([]string{"report"}, args...), &bytes.Buffer{}, &bytes.Buffer{}); code != 2 {
			t.Errorf("report %q: exit %d, want 2", args, code)
		}
	}
	if len(*got) != 0 {
		t.Fatalf("bad arguments delivered %+v", *got)
	}
}

func TestReportCommandFailsWhenDeliveryFails(t *testing.T) {
	claudeSession(t, "cc-1")
	app, _ := reportApp(t, errors.New("serve is not running"))
	var errw bytes.Buffer
	if code := app.Dispatch([]string{"report", "landed", "t7"}, &bytes.Buffer{}, &errw); code != 1 || !bytes.Contains(errw.Bytes(), []byte("serve is not running")) {
		t.Fatalf("exit %d, stderr %q", code, errw.String())
	}
}

func TestReportCommandNeedsASession(t *testing.T) {
	claudeSession(t, "")
	app, got := reportApp(t, nil)
	if code := app.Dispatch([]string{"report", "turn-ended"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 1 || len(*got) != 0 {
		t.Fatalf("exit %d, delivered %+v", code, *got)
	}
}
