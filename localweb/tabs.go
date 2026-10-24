package localweb

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
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
	path string // where Keep saves the list; "" keeps it in memory only
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
	_ = t.save() // a failed write only costs a restart its memory
}

// Connected records that tab id opened a stream showing page. The tab counts
// as open until done is called (once; later calls do nothing), and from then
// on until it has been silent for TabSilence.
func (t *Tabs) Connected(id, page string) (done func()) {
	t.mu.Lock()
	t.get(id, page).streams++
	_ = t.save()
	t.mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			t.mu.Lock()
			defer t.mu.Unlock()
			if tb := t.tabs[id]; tb != nil {
				tb.streams--
				tb.seen = t.now()
				_ = t.save()
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
		_ = t.save()
	}
}

// Pick says what to do to show page: TabHere when an open tab already shows it,
// else TabMove the front-page tab active most recently (its stream opening or
// its last Seen; an open stream keeps a tab alive but does not rank it), else
// TabElsewhere when only tabs on other pages are open, else TabNone. It
// forgets closed tabs.
func (t *Tabs) Pick(page string) Choice {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	var front string
	var frontSeen time.Time
	open := false
	for id, tb := range t.tabs {
		if tb.streams == 0 && now.Sub(tb.seen) > TabSilence {
			delete(t.tabs, id)
			continue
		}
		open = true
		switch {
		case tb.page == page:
			return Choice{Kind: TabHere, Tab: id}
		case tb.page == "" && (front == "" || tb.seen.After(frontSeen)):
			front, frontSeen = id, tb.seen
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

// kept is the list as Keep saves it: when it was saved and each tab, a tab
// holding a stream then counting as seen at that moment.
type kept struct {
	Saved time.Time          `json:"saved"`
	Tabs  map[string]keptTab `json:"tabs"`
}

type keptTab struct {
	Page string    `json:"page"`
	Seen time.Time `json:"seen"`
}

// Keep loads the list saved at path and saves it there after every change, so
// a restarted server still knows the tabs open before it stopped (a hidden tab
// checks in only once a minute). A list saved longer ago than TabSilence, or
// none at all, starts empty. Call Save as the server stops, so tabs holding a
// stream count as seen then.
func (t *Tabs) Keep(path string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.path = path
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var k kept
	if err := json.Unmarshal(b, &k); err != nil {
		return nil // a damaged list is no list: tabs check in again
	}
	if t.now().Sub(k.Saved) > TabSilence {
		return nil
	}
	for id, kt := range k.Tabs {
		if _, ok := t.tabs[id]; !ok {
			t.tabs[id] = &tab{page: kt.Page, seen: kt.Seen}
		}
	}
	return nil
}

// Save writes the list now, if Keep named a file.
func (t *Tabs) Save() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.save()
}

// save writes the list to t.path (a temp file renamed over it, 0600). Callers
// hold mu.
func (t *Tabs) save() error {
	if t.path == "" {
		return nil
	}
	now := t.now()
	k := kept{Saved: now, Tabs: make(map[string]keptTab, len(t.tabs))}
	for id, tb := range t.tabs {
		seen := tb.seen
		if tb.streams > 0 {
			seen = now
		}
		k.Tabs[id] = keptTab{Page: tb.page, Seen: seen}
	}
	b, err := json.Marshal(k)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(t.path), ".tabs-*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), t.path)
}
