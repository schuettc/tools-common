package localweb

import (
	"sync"
	"time"
)

// TabSilence is how long a tab with no open stream may go without checking in
// before it counts as closed. A hidden tab polls once a minute and Chrome runs
// a long-hidden tab's timers about once a minute, so three minutes allows for
// one missed check-in.
const TabSilence = 3 * time.Minute

// ChoiceKind is what Pick says to do for a page.
type ChoiceKind int

const (
	// TabNone: no tab is open; open the browser.
	TabNone ChoiceKind = iota
	// TabHere: a tab already shows the page; do nothing.
	TabHere
	// TabMove: move Choice.Tab, a front-page tab, to the page.
	TabMove
	// TabElsewhere: only tabs on other pages are open; move none of them.
	TabElsewhere
)

// Choice is Pick's answer. Tab names the tab for TabHere and TabMove.
type Choice struct {
	Kind ChoiceKind
	Tab  string
}

// Tabs is the list of a tool's open page tabs, which a tool's handlers feed
// from the tab id and page each stream and poll carries (live.js sends them
// as the tab and page query parameters). A page is the tool's own name for
// what a tab shows; "" is the front page. Tabs owns no routes. Safe for
// concurrent use.
type Tabs struct {
	now  func() time.Time
	mu   sync.Mutex
	tabs map[string]*tab
}

type tab struct {
	page    string
	seen    time.Time
	streams int
}

// NewTabs returns an empty list that reads the time from now (time.Now when
// nil).
func NewTabs(now func() time.Time) *Tabs {
	if now == nil {
		now = time.Now
	}
	return &Tabs{now: now, tabs: map[string]*tab{}}
}

// get returns id's entry, made if new, showing page and seen now. Callers
// hold mu.
func (t *Tabs) get(id, page string) *tab {
	tb := t.tabs[id]
	if tb == nil {
		tb = &tab{}
		t.tabs[id] = tb
	}
	tb.page = page
	tb.seen = t.now()
	return tb
}

// Seen records that tab id checked in (a poll) showing page.
func (t *Tabs) Seen(id, page string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.get(id, page)
}

// Connected records that tab id opened a stream showing page. The tab counts
// as open until done is called (once; later calls do nothing), and from then
// on until it has been silent for TabSilence.
func (t *Tabs) Connected(id, page string) (done func()) {
	t.mu.Lock()
	t.get(id, page).streams++
	t.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			t.mu.Lock()
			defer t.mu.Unlock()
			if tb := t.tabs[id]; tb != nil {
				tb.streams--
				tb.seen = t.now()
			}
		})
	}
}

// Moved records that tab id now shows page, after the tool told it to move,
// so the next Pick for page answers TabHere.
func (t *Tabs) Moved(id, page string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if tb := t.tabs[id]; tb != nil {
		tb.page = page
	}
}

// Pick says what to do to show page: TabHere when an open tab already shows it,
// else TabMove the front-page tab seen most recently (a tab holding a stream is
// seen now), else TabElsewhere when only tabs on other pages are open, else TabNone.
// It forgets closed tabs.
func (t *Tabs) Pick(page string) Choice {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	var front string
	var frontSeen time.Time
	open := false
	for id, tb := range t.tabs {
		seen := tb.seen
		if tb.streams > 0 {
			seen = now
		}
		if now.Sub(seen) > TabSilence {
			delete(t.tabs, id)
			continue
		}
		open = true
		switch {
		case tb.page == page:
			return Choice{Kind: TabHere, Tab: id}
		case tb.page == "" && (front == "" || seen.After(frontSeen)):
			front, frontSeen = id, seen
		}
	}
	switch {
	case front != "":
		return Choice{Kind: TabMove, Tab: front}
	case open:
		return Choice{Kind: TabElsewhere}
	}
	return Choice{Kind: TabNone}
}
