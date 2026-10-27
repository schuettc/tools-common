package pageserve

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
)

// ownerOf reads the advert back and returns its recorded owner.
func ownerOf(t *testing.T, l Live, room string) string {
	t.Helper()
	got, err := l.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range got {
		if e.Room == room {
			return e.Owner
		}
	}
	t.Fatalf("advert %s vanished", room)
	return ""
}

func TestClaim(t *testing.T) {
	l, _ := tempLive(t)
	e := Entry{URL: "http://127.0.0.1:9/", Room: "content-claim", Page: "/tmp/x.md", PID: os.Getpid()}
	if err := l.Write(e); err != nil {
		t.Fatal(err)
	}

	// A live session claims an unowned advert.
	if err := l.AnnounceSession("sess-A"); err != nil {
		t.Fatal(err)
	}
	if ok, err := l.Claim(e.Room, "sess-A"); err != nil || !ok {
		t.Fatalf("claim of an unowned advert: ok=%v err=%v, want true", ok, err)
	}
	if got := ownerOf(t, l, e.Room); got != "sess-A" {
		t.Fatalf("owner = %q, want sess-A", got)
	}

	// A different live session cannot take it — its wakes would go to sess-A.
	if err := l.AnnounceSession("sess-B"); err != nil {
		t.Fatal(err)
	}
	if ok, err := l.Claim(e.Room, "sess-B"); err != nil || ok {
		t.Fatalf("claim over a live owner: ok=%v err=%v, want false", ok, err)
	}
	if got := ownerOf(t, l, e.Room); got != "sess-A" {
		t.Fatalf("owner moved to %q while sess-A was live", got)
	}

	// The owner re-claiming is idempotent.
	if ok, err := l.Claim(e.Room, "sess-A"); err != nil || !ok {
		t.Fatalf("owner re-claim: ok=%v err=%v, want true", ok, err)
	}

	// Once the owner's session is gone, the advert is claimable again (the
	// orphan-adoption case channel.claim already allows).
	if err := l.WithdrawSession("sess-A"); err != nil {
		t.Fatal(err)
	}
	if ok, err := l.Claim(e.Room, "sess-B"); err != nil || !ok {
		t.Fatalf("claim of an orphaned advert: ok=%v err=%v, want true", ok, err)
	}
	if got := ownerOf(t, l, e.Room); got != "sess-B" {
		t.Fatalf("owner = %q, want sess-B after adoption", got)
	}

	// An empty owner cannot claim (it would leave the advert unowned).
	if _, err := l.Claim(e.Room, ""); err == nil {
		t.Fatal("an empty owner must be refused")
	}

	// A missing advert is not claimable, and that is not an error.
	if ok, err := l.Claim("content-gone", "sess-A"); err != nil || ok {
		t.Fatalf("claim of a missing advert: ok=%v err=%v, want false, nil", ok, err)
	}
}

// TestClaimIsExclusiveUnderRace is the whole point: many channels scanning one
// unowned advert at once must produce EXACTLY ONE owner, so the editor's wakes
// reach one session and not every session under whose root it falls.
func TestClaimIsExclusiveUnderRace(t *testing.T) {
	l, _ := tempLive(t)
	e := Entry{URL: "http://127.0.0.1:9/", Room: "content-race", Page: "/tmp/x.md", PID: os.Getpid()}
	if err := l.Write(e); err != nil {
		t.Fatal(err)
	}

	const n = 8
	var wins int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("racer-%d", i)
		if err := l.AnnounceSession(id); err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if ok, err := l.Claim(e.Room, id); err == nil && ok {
				atomic.AddInt32(&wins, 1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if wins != 1 {
		t.Fatalf("exactly one channel must claim an unowned advert; got %d winners", wins)
	}
}
