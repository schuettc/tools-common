package pageserve

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// AN ADVERT AS AN OLDER GALLEY WROTE IT — literal bytes, no `v`. One
// `~/.galley/live/` is shared by every galley on the machine and an editor
// started before an upgrade keeps advertising for hours, so this is not a
// migration case: it is Tuesday.
func TestAnAdvertWrittenBeforeVersionsIsStillFound(t *testing.T) {
	l, dir := tempLive(t)
	body := fmt.Sprintf(`{"url":"http://127.0.0.1:7788","room":"doc-old","page":"/tmp/doc.md","pid":%d,"owner":"sess-1"}`, os.Getpid())
	if err := os.WriteFile(filepath.Join(dir, "doc-old.json"), []byte(body+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, problems, err := l.Inspect()
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 0 {
		t.Fatalf("an older advert is not a problem: %v", problems)
	}
	if len(got) != 1 {
		t.Fatalf("got %d adverts, want the old one — a channel that stops seeing editors is the failure", len(got))
	}
	if got[0].V != 0 || got[0].PID != os.Getpid() || got[0].Owner != "sess-1" || got[0].URL != "http://127.0.0.1:7788" {
		t.Fatalf("advert = %+v", got[0])
	}
	if future(got[0].V) {
		t.Fatal("an absent version must read as the first generation")
	}
}

// A NEWER ADVERT IS REPORTED AND LEFT ALONE. Reaping is a deletion, and this
// build has just said it cannot read the record's rules — deleting it would
// take a live editor's only advert away from every other reader on the machine.
func TestAnAdvertFromANewerGalleyIsReportedAndNotReaped(t *testing.T) {
	l, dir := tempLive(t)
	name := filepath.Join(dir, "doc-new.json")
	body := fmt.Sprintf(`{"v":99,"url":"http://127.0.0.1:7789","room":"doc-new","page":"/tmp/doc.md","pid":%d,"mood":"cheerful"}`, os.Getpid())
	if err := os.WriteFile(name, []byte(body+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, problems, err := l.Inspect()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("a record this build cannot read must not be acted on: %+v", got)
	}
	if len(problems) != 1 || !strings.Contains(problems[0].Reason, "v99") {
		t.Fatalf("problems = %v, want the file named and the reason given", problems)
	}
	if _, err := os.Stat(name); err != nil {
		t.Fatalf("the advert was deleted — it is somebody else's: %v", err)
	}
}

// A newer PRESENCE record answers "not live" and is likewise left on disk. The
// section comment says which way to err: absence means only that nothing HERE
// is listening.
func TestASessionFromANewerGalleyIsNotLiveAndNotReaped(t *testing.T) {
	l, dir := tempLive(t)
	sub := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(sub, "sess-9.json")
	body := fmt.Sprintf(`{"v":99,"session":"sess-9","pid":%d}`, os.Getpid())
	if err := os.WriteFile(name, []byte(body+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if l.SessionLive("sess-9") {
		t.Fatal("this build cannot read a newer record's rules and must not vouch for it")
	}
	if _, err := os.Stat(name); err != nil {
		t.Fatalf("the presence file was deleted — that is this build answering a question it cannot: %v", err)
	}
}

// A presence record from an older galley is exactly the shape this field was
// added to, and must keep answering.
func TestASessionWrittenBeforeVersionsIsStillLive(t *testing.T) {
	l, dir := tempLive(t)
	sub := filepath.Join(dir, "sessions")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"session":"sess-old","pid":%d}`, os.Getpid())
	if err := os.WriteFile(filepath.Join(sub, "sess-old.json"), []byte(body+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !l.SessionLive("sess-old") {
		t.Fatal("an older presence record stopped answering — every document that session owns just lost its owner")
	}
}

// THE KEY SET IS THE CONTRACT. The directory tolerates unknown keys by
// design, so nothing on the reading side can notice a rename; this list is the
// only check that can. Each row is one shape on disk today: galley's registry
// entry, a one-page tool's advert (diazo's with stale), and the presence
// record.
//
// `session` is the case it is most needed for: Session.ID is spelled `session`
// on disk, so the Go name and the key already differ and a rename on either
// side looks harmless from the other.
func TestTheEntryKeysAreTheContract(t *testing.T) {
	at := time.Date(2026, 10, 19, 20, 57, 20, 0, time.UTC)
	for _, tc := range []struct {
		name string
		val  any
		want string
	}{
		{"galley Entry", Entry{V: 1, URL: "u", Room: "r", Page: "p", PID: 1, Owner: "o"}, "v,url,room,page,pid,owner"},
		{"one-page Entry", Entry{V: 1, URL: "u", Base: "b", Token: "t", PID: 1, Version: "0.3.0", StartedAt: at},
			"v,url,base,token,pid,version,started_at"},
		{"stale Entry", Entry{V: 1, URL: "u", Base: "b", Token: "t", PID: 1, Version: "0.3.0", Stale: true, StartedAt: at},
			"v,url,base,token,pid,version,stale,started_at"},
		{"Session", Session{V: 1, ID: "s", PID: 1}, "v,session,pid"},
	} {
		keys, err := topKeys(tc.val)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(keys, ","); got != tc.want {
			t.Errorf("%s keys = %s\n              want %s\n"+
				"a renamed key decodes to a zero value in silence: PID 0 is a corpse, an empty owner is unowned", tc.name, got, tc.want)
		}
	}
}

// topKeys is the JSON key set v marshals to, at the top level only, in the
// order encoding/json emits them.
func topKeys(v any) ([]string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	if _, err := dec.Token(); err != nil { // {
		return nil, err
	}
	var out []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		out = append(out, tok.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			return nil, err
		}
	}
	return out, nil
}
