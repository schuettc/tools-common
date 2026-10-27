package pageserve

import (
	"errors"
	"os"
	"testing"
)

func TestStaleAdvertRemoved(t *testing.T) {
	l := Live{Tool: "cull", Dir: t.TempDir()}
	if err := l.Advertise(Entry{PID: 1 << 30, Token: "x"}); err != nil {
		t.Fatal(err)
	}
	_, err := l.Running()
	if !errors.Is(err, ErrNotRunning) || err.Error() != "cull serve is not running" {
		t.Fatalf("%v", err)
	}
	if _, err := os.Stat(l.ServePath()); !os.IsNotExist(err) {
		t.Fatal("stale advert kept")
	}
}

func TestWithdrawLeavesOtherPID(t *testing.T) {
	l := Live{Tool: "cull", Dir: t.TempDir()}
	if err := l.Advertise(Entry{PID: 4242, Token: "x"}); err != nil {
		t.Fatal(err)
	}
	l.Withdraw(os.Getpid())
	if a, err := l.readServe(); err != nil || a.PID != 4242 {
		t.Fatalf("advert gone or changed: %+v %v", a, err)
	}
	l.Withdraw(4242)
	if _, err := os.Stat(l.ServePath()); !os.IsNotExist(err) {
		t.Fatalf("own advert kept: %v", err)
	}
}

func TestAdvertPrivate(t *testing.T) {
	l := Live{Tool: "cull", Dir: t.TempDir() + "/live"}
	if err := l.Advertise(Entry{PID: os.Getpid(), Token: "x"}); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(l.ServePath()); err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("advert %v %v", st, err)
	}
	if d, _ := os.Stat(l.Dir); d.Mode().Perm() != 0o700 {
		t.Fatalf("dir %v", d.Mode().Perm())
	}
	if e, err := l.Running(); err != nil || e.PID != os.Getpid() || e.Room != ServeRoom {
		t.Fatalf("%+v %v", e, err)
	}
}
