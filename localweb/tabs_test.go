package localweb

import (
	"testing"
	"time"
)

// clock is a settable now for Tabs.
type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newTabs() (*Tabs, *clock) {
	c := &clock{t: time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)}
	return NewTabs(c.now), c
}

func TestTabsPickNoneWhenNoTab(t *testing.T) {
	tabs, _ := newTabs()
	if got := tabs.Pick("PLAN1"); got != (Choice{Kind: TabNone}) {
		t.Fatalf("Pick = %+v, want None", got)
	}
}

func TestTabsPickHereWhenATabShowsThePage(t *testing.T) {
	tabs, _ := newTabs()
	tabs.Seen("front", "")
	tabs.Seen("plan", "PLAN1")
	if got := tabs.Pick("PLAN1"); got != (Choice{Kind: TabHere, Tab: "plan"}) {
		t.Fatalf("Pick = %+v, want TabHere plan", got)
	}
}

func TestTabsPickMovesTheFrontPageTab(t *testing.T) {
	tabs, _ := newTabs()
	tabs.Seen("other", "PLAN2")
	tabs.Seen("front", "")
	if got := tabs.Pick("PLAN1"); got != (Choice{Kind: TabMove, Tab: "front"}) {
		t.Fatalf("Pick = %+v, want TabMove front", got)
	}
}

func TestTabsPickMovesTheMostRecentFrontPageTab(t *testing.T) {
	tabs, c := newTabs()
	tabs.Seen("older", "")
	c.advance(time.Second)
	tabs.Seen("newer", "")
	c.advance(time.Second)
	tabs.Seen("older2", "PLAN2")
	if got := tabs.Pick("PLAN1"); got != (Choice{Kind: TabMove, Tab: "newer"}) {
		t.Fatalf("Pick = %+v, want TabMove newer", got)
	}
	c.advance(time.Second)
	tabs.Seen("older", "")
	if got := tabs.Pick("PLAN1"); got != (Choice{Kind: TabMove, Tab: "older"}) {
		t.Fatalf("after older checks in again: Pick = %+v, want TabMove older", got)
	}
}

// A front-page tab holding its stream is in use now, so it beats one that only
// checked in a minute ago.
func TestTabsPickAStreamingFrontPageTabIsTheMostRecent(t *testing.T) {
	tabs, c := newTabs()
	tabs.Connected("visible", "")
	c.advance(time.Minute)
	tabs.Seen("hidden", "")
	c.advance(time.Second)
	if got := tabs.Pick("PLAN1"); got != (Choice{Kind: TabMove, Tab: "visible"}) {
		t.Fatalf("Pick = %+v, want TabMove visible", got)
	}
}

func TestTabsPickElsewhereWhenOnlyOtherPagesAreOpen(t *testing.T) {
	tabs, _ := newTabs()
	tabs.Seen("a", "PLAN2")
	tabs.Seen("b", "PLAN3")
	if got := tabs.Pick("PLAN1"); got != (Choice{Kind: TabElsewhere}) {
		t.Fatalf("Pick = %+v, want Elsewhere", got)
	}
}

func TestTabsMovedPutsTheTabOnThePage(t *testing.T) {
	tabs, _ := newTabs()
	tabs.Seen("front", "")
	ch := tabs.Pick("PLAN1")
	tabs.Moved(ch.Tab, "PLAN1")
	if got := tabs.Pick("PLAN1"); got != (Choice{Kind: TabHere, Tab: "front"}) {
		t.Fatalf("second open: Pick = %+v, want TabHere front", got)
	}
	if got := tabs.Pick("PLAN2"); got != (Choice{Kind: TabElsewhere}) {
		t.Fatalf("the moved tab is no longer a front-page tab: Pick = %+v, want Elsewhere", got)
	}
}

func TestTabsSilentThreeMinutesIsClosed(t *testing.T) {
	tabs, c := newTabs()
	tabs.Seen("front", "")
	c.advance(3 * time.Minute)
	if got := tabs.Pick("PLAN1"); got.Kind != TabMove {
		t.Fatalf("at exactly 3 minutes: Pick = %+v, want Move", got)
	}
	c.advance(time.Second)
	if got := tabs.Pick("PLAN1"); got != (Choice{Kind: TabNone}) {
		t.Fatalf("after 3 minutes of silence: Pick = %+v, want None", got)
	}
}

func TestTabsAnOpenStreamKeepsATabAlive(t *testing.T) {
	tabs, c := newTabs()
	done := tabs.Connected("plan", "PLAN1")
	c.advance(10 * time.Minute)
	if got := tabs.Pick("PLAN1"); got != (Choice{Kind: TabHere, Tab: "plan"}) {
		t.Fatalf("stream open 10 minutes: Pick = %+v, want TabHere plan", got)
	}
	done()
	c.advance(3 * time.Minute)
	if got := tabs.Pick("PLAN1"); got.Kind != TabHere {
		t.Fatalf("3 minutes after the stream closed: Pick = %+v, want Here", got)
	}
	c.advance(time.Second)
	if got := tabs.Pick("PLAN1"); got != (Choice{Kind: TabNone}) {
		t.Fatalf("silent past 3 minutes after the stream closed: Pick = %+v, want None", got)
	}
}

func TestTabsTwoStreamsOnOneTabBothMustClose(t *testing.T) {
	tabs, c := newTabs()
	done1 := tabs.Connected("t", "")
	done2 := tabs.Connected("t", "")
	done1()
	done1() // a second call changes nothing
	c.advance(time.Hour)
	if got := tabs.Pick("PLAN1"); got.Kind != TabMove {
		t.Fatalf("one stream still open: Pick = %+v, want Move", got)
	}
	done2()
	c.advance(3*time.Minute + time.Second)
	if got := tabs.Pick("PLAN1"); got.Kind != TabNone {
		t.Fatalf("both closed and silent: Pick = %+v, want None", got)
	}
}
