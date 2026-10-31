package pageserve

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/subaud-tools/tools-common/localweb"
)

var inTmux = Where{Socket: "/tmp/tmux-501/default", Pane: "%3"}

// fakeProj replaces proj for the test: answer gives each call's output and
// exit code by its subcommand, and every call is recorded.
func fakeProj(t *testing.T, answer func(sub string) ([]byte, int, error)) *[][]string {
	t.Helper()
	calls := &[][]string{}
	was := Proj
	Proj = func(_ context.Context, args ...string) ([]byte, int, error) {
		*calls = append(*calls, args)
		return answer(args[0])
	}
	t.Cleanup(func() { Proj = was })
	return calls
}

// withTerminal says the session's terminal state as terminal, and window succeeds.
func withTerminal(t *testing.T, terminal string) *[][]string {
	t.Helper()
	return fakeProj(t, func(sub string) ([]byte, int, error) {
		if sub == "terminal" {
			return []byte(terminal), 0, nil
		}
		return []byte("{}"), 0, nil
	})
}

func logged(t *testing.T) *bytes.Buffer {
	t.Helper()
	var b bytes.Buffer
	was := showLog
	showLog = &b
	t.Cleanup(func() { showLog = was })
	return &b
}

func eventsWithTabs() *Events {
	ev := NewEvents()
	ev.Tabs = localweb.NewTabs(nil)
	return ev
}

// The socket is TMUX up to its first comma, the pane TMUX_PANE; a session with
// only one of them has no Where.
func TestWhereFrom(t *testing.T) {
	for _, tc := range []struct {
		tmux, pane string
		want       Where
	}{
		{"/tmp/tmux-501/default,123,0", "%3", inTmux},
		{"", "", Where{}},
		{"/tmp/tmux-501/default,123,0", "", Where{}},
		{"", "%3", Where{}},
	} {
		env := map[string]string{"TMUX": tc.tmux, "TMUX_PANE": tc.pane}
		if got := WhereFrom(func(k string) string { return env[k] }); got != tc.want {
			t.Fatalf("%+v: %+v", tc, got)
		}
	}
}

func TestFromEnvCarriesWhere(t *testing.T) {
	t.Setenv("TMUX", "/tmp/tmux-501/default,123,0")
	t.Setenv("TMUX_PANE", "%3")
	if got := FromEnv().Where; got != inTmux {
		t.Fatalf("%+v", got)
	}
}

func TestTerminalOfReadsProj(t *testing.T) {
	calls := withTerminal(t, `{"state":"attached","session":"s","chrome":true,"window":true}`)
	got, err := TerminalOf(inTmux)
	if err != nil {
		t.Fatal(err)
	}
	if want := (Terminal{State: TerminalAttached, Session: "s", Chrome: true, Window: true}); *got != want {
		t.Fatalf("%+v", got)
	}
	want := []string{"terminal", "--socket", inTmux.Socket, "--pane", inTmux.Pane, "--json"}
	if !reflect.DeepEqual((*calls)[0], want) {
		t.Fatalf("%v", (*calls)[0])
	}
}

func TestTerminalGoAsksProjToShowIt(t *testing.T) {
	calls := withTerminal(t, `{"state":"attached"}`)
	if _, err := TerminalGo(inTmux); err != nil {
		t.Fatal(err)
	}
	want := []string{"terminal", "--socket", inTmux.Socket, "--pane", inTmux.Pane, "--go", "--json"}
	if !reflect.DeepEqual((*calls)[0], want) {
		t.Fatalf("%v", (*calls)[0])
	}
}

func TestTerminalOfOutsideTmuxIsNothing(t *testing.T) {
	calls := withTerminal(t, `{"state":"attached"}`)
	for _, w := range []Where{{}, {Socket: "/tmp/s"}, {Pane: "%3"}} {
		got, err := TerminalOf(w)
		if got != nil || err != nil || len(*calls) != 0 {
			t.Fatalf("%+v: %+v %v, %d calls", w, got, err, len(*calls))
		}
	}
}

func TestTerminalOfRefusesAnUnknownState(t *testing.T) {
	withTerminal(t, `{"state":"elsewhere"}`)
	if got, err := TerminalOf(inTmux); got != nil || err == nil || !strings.Contains(err.Error(), `unknown state "elsewhere"`) {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestTerminalOfSaysWhatProjSaid(t *testing.T) {
	fakeProj(t, func(string) ([]byte, int, error) { return []byte("no such pane\n"), 3, nil })
	if _, err := TerminalOf(inTmux); err == nil || err.Error() != "proj terminal exited 3: no such pane" {
		t.Fatalf("%v", err)
	}
}

func TestWindowPutsThePageOnTheRight(t *testing.T) {
	calls := withTerminal(t, "")
	if err := Window(inTmux, "http://x/p", false); err != nil {
		t.Fatal(err)
	}
	if err := Window(inTmux, "http://x/p", true); err != nil {
		t.Fatal(err)
	}
	base := []string{"window", "--socket", inTmux.Socket, "--pane", inTmux.Pane, "--right", "http://x/p"}
	if !reflect.DeepEqual((*calls)[0], append(append([]string{}, base...), "--json")) ||
		!reflect.DeepEqual((*calls)[1], append(append([]string{}, base...), "--keep-view", "--json")) {
		t.Fatalf("%v", *calls)
	}
}

func TestWindowSaysWhatProjSaid(t *testing.T) {
	fakeProj(t, func(string) ([]byte, int, error) { return []byte("no chrome\n"), 2, nil })
	if err := Window(inTmux, "u", false); err == nil || err.Error() != "proj window exited 2: no chrome" {
		t.Fatalf("%v", err)
	}
}

// A session with a Chrome window gets the page there, keeping the terminal's
// view, and no tab is touched.
func TestShowInTheSessionsWindow(t *testing.T) {
	calls := withTerminal(t, `{"state":"attached","window":true}`)
	ev := eventsWithTabs()
	ev.Tabs.Seen("front", "")
	if got := Show(inTmux, "http://x/p", ev, "p"); got != ShowWindow {
		t.Fatalf("%q", got)
	}
	last := (*calls)[len(*calls)-1]
	if last[0] != "window" || !strings.Contains(strings.Join(last, " "), "--keep-view") {
		t.Fatalf("%v", last)
	}
	if c := ev.Tabs.Pick("p"); c.Kind != localweb.TabMove {
		t.Fatalf("a tab was moved: %+v", c)
	}
}

// No window, no open tab: the caller opens the browser.
func TestShowOpensWithNoWindowAndNoTab(t *testing.T) {
	calls := withTerminal(t, `{"state":"detached"}`)
	if got := Show(inTmux, "u", eventsWithTabs(), "p"); got != ShowOpened {
		t.Fatalf("%q", got)
	}
	if len(*calls) != 1 {
		t.Fatalf("proj window ran: %v", *calls)
	}
}

// A tab already on the page takes it, and nothing moves.
func TestShowReusesTheTabOnThePage(t *testing.T) {
	withTerminal(t, `{"state":"detached"}`)
	ev := eventsWithTabs()
	ev.Tabs.Seen("t1", "p")
	if got := Show(inTmux, "u", ev, "p"); got != ShowReused {
		t.Fatalf("%q", got)
	}
	if evs, _, _ := ev.after(0); len(evs) != 0 {
		t.Fatalf("events %+v", evs)
	}
}

// A front-page tab is asked to move to the page, and counts as there from now.
func TestShowMovesAFrontPageTab(t *testing.T) {
	withTerminal(t, `{"state":"detached"}`)
	ev := eventsWithTabs()
	ev.Tabs.Seen("front", "")
	if got := Show(inTmux, "u", ev, "p"); got != ShowReused {
		t.Fatalf("%q", got)
	}
	evs, _, _ := ev.after(0)
	if len(evs) != 1 || evs[0].Type != "show" || string(evs[0].Data) != `{"page":"p","tab":"front"}` {
		t.Fatalf("events %+v", evs)
	}
	if c := ev.Tabs.Pick("p"); c.Kind != localweb.TabHere || c.Tab != "front" {
		t.Fatalf("%+v", c)
	}
}

// Tabs on other pages only list it in their bar: none moves.
func TestShowLeavesTabsOnOtherPages(t *testing.T) {
	withTerminal(t, `{"state":"detached"}`)
	ev := eventsWithTabs()
	ev.Tabs.Seen("t1", "other")
	if got := Show(inTmux, "u", ev, "p"); got != ShowReused {
		t.Fatalf("%q", got)
	}
	if evs, _, _ := ev.after(0); len(evs) != 0 {
		t.Fatalf("events %+v", evs)
	}
}

// Outside tmux there is no Where: proj never runs, and tabs answer.
func TestShowOutsideTmuxUsesTabs(t *testing.T) {
	calls := withTerminal(t, `{"state":"attached","window":true}`)
	log := logged(t)
	if got := Show(Where{}, "u", eventsWithTabs(), "p"); got != ShowOpened {
		t.Fatalf("%q", got)
	}
	if len(*calls) != 0 || log.Len() != 0 {
		t.Fatalf("calls %v, log %q", *calls, log)
	}
}

// proj failing, at either step, falls to the tabs, and the error is logged.
func TestShowFallsToTabsWhenProjFails(t *testing.T) {
	for _, tc := range []struct {
		name   string
		answer func(string) ([]byte, int, error)
		want   string
	}{
		{"not installed", func(string) ([]byte, int, error) { return nil, -1, errors.New("proj not found") }, "proj not found"},
		{"terminal fails", func(string) ([]byte, int, error) { return []byte("bad"), 1, nil }, "proj terminal exited 1: bad"},
		{"window fails", func(sub string) ([]byte, int, error) {
			if sub == "terminal" {
				return []byte(`{"state":"attached","window":true}`), 0, nil
			}
			return []byte("no chrome"), 2, nil
		}, "proj window exited 2: no chrome"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeProj(t, tc.answer)
			log := logged(t)
			if got := Show(inTmux, "u", eventsWithTabs(), "p"); got != ShowOpened {
				t.Fatalf("%q", got)
			}
			if !strings.Contains(log.String(), tc.want) {
				t.Fatalf("log %q", log)
			}
		})
	}
}

// Where goes into a tool's own call only when the session has one.
func TestWhereInto(t *testing.T) {
	body := map[string]any{"a": 1}
	Where{}.Into(body)
	if !reflect.DeepEqual(body, map[string]any{"a": 1}) {
		t.Fatalf("%v", body)
	}
	inTmux.Into(body)
	if !reflect.DeepEqual(body, map[string]any{"a": 1, "tmux_socket": inTmux.Socket, "tmux_pane": inTmux.Pane}) {
		t.Fatalf("%v", body)
	}
}
