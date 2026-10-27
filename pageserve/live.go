package pageserve

// Moved from galley's internal/registry, with the tool's name and its
// directory as parameters (Live) instead of "galley" and ~/.galley/live.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	tools "github.com/subaud-tools/tools-common"
)

// Version is the schema generation of an entry and of a session presence
// record, written as `v` on every file this build writes.
//
// THE DIRECTORY IS FORWARD-COMPATIBLE AND MUST STAY THAT WAY. Two builds of a
// tool run against one live directory as a matter of course: a server started
// before an upgrade keeps its entry alive for hours, and the channel that
// scans for it is a different, longer-lived process that may be older or newer
// than every server it finds. So an unknown KEY is ignored — a strict decoder
// here would turn "one process was upgraded" into "the channel can no longer
// see my pages", with no upgrade order that avoids it — and an unknown
// VERSION is REPORTED AND LEFT ALONE rather than reaped, because reaping is a
// deletion and this build cannot judge the liveness of a record whose rules it
// does not know.
//
// What makes a rename loud here is not the decoder, which cannot see a key it
// was never told about: it is TestTheEntryKeysAreTheContract, which pins the
// key set and goes red on the commit that renames one.
//
// A file with no `v` is generation 1 — the shape the field was added to.
const Version = 1

// future reports whether v names a generation newer than this build knows.
// An absent v (0) never is.
func future(v int) bool { return v > Version }

// Entry is one running page server: the superset of a one-page tool's
// advert (url, base, token, pid, version, stale, started_at) and galley's
// registry entry (v, url, room, page, pid, owner), under the same JSON names,
// so a file either wrote reads back here with every field, and a file
// written here reads back with either's type. Fields one of them never had
// are omitted when empty, so each file keeps its shape.
type Entry struct {
	// V is the schema generation — see Version. Absent on every entry written
	// before this field existed, which reads as generation 1.
	V     int    `json:"v"`
	URL   string `json:"url"`            // page URL; a one-page tool's carries ?t= for the first load
	Base  string `json:"base,omitempty"` // http://127.0.0.1:<port>
	Token string `json:"token,omitempty"`
	// Room is the entry's file name without .json (and galley's websocket
	// gate); a one-page tool's is ServeRoom.
	Room string `json:"room,omitempty"`
	// Page is the absolute path of what the page shows (galley's document).
	Page string `json:"page,omitempty"`
	PID  int    `json:"pid"`
	// Owner is the session the page wakes; empty is UNOWNED, claimable by any
	// channel (see Claim).
	Owner   string `json:"owner,omitempty"`
	Version string `json:"version,omitempty"`
	// Stale: the binary this serve runs was replaced; Client.Fresh restarts it.
	Stale     bool      `json:"stale,omitempty"`
	StartedAt time.Time `json:"started_at,omitzero"`
}

// Live is a tool's directory of entries: one JSON file per running page,
// written on start, removed on exit, reaped on read when the writing process
// is gone. Tool names the tool in errors and problems.
type Live struct {
	Tool string
	Dir  string
}

// LiveFor is tool's live directory under its state directory,
// tools.StateDir(tool)/live, where a one-page tool's serve.json has always
// been.
func LiveFor(tool string) Live {
	return Live{Tool: tool, Dir: filepath.Join(tools.StateDir(tool), "live")}
}

// dir creates the directory on demand so Write never races a fresh machine.
//
// 0o700, not 0o755: the directory is per-user by design, and an entry's room
// can be a secret — galley's room token is the FILENAME, the same token its
// websocket gate checks — so a world-readable listing hands any local user the
// key to every live room.
func (l Live) dir() (string, error) {
	if err := os.MkdirAll(l.Dir, 0o700); err != nil {
		return "", err
	}
	_ = os.Chmod(l.Dir, 0o700)
	return l.Dir, nil
}

// validRoom rejects any Room that could carry a path component out of the
// directory. This package is the last line of defense between a
// caller-supplied room name and a file write or delete on disk, so a room
// like "../../../etc/cron.d/x" — or anything else filepath.Base would not
// hand back unchanged — is refused rather than joined into a path.
func validRoom(room string) error {
	if room == "" || room == "." || room == ".." || filepath.Base(room) != room {
		return fmt.Errorf("pageserve: invalid room %q", room)
	}
	return nil
}

// Validate is the one description of an entry Inspect will report, and it is
// applied on BOTH sides — Write refuses to record what List would refuse to
// report, so an advertiser learns at the moment it writes instead of being
// silently invisible to every channel forever.
//
// EVERYTHING HERE REACHES SOMEWHERE IT MATTERS. The room is a FILENAME (and
// galley's websocket gate). The URL is an address this process will issue
// requests to. And the page is rendered into the channel's notification text,
// which lands in a model's context — so a newline in it could forge a second
// line of a `<channel …>` tag. None of the three is this package's to trust:
// two of them come from a caller's environment and all three come off disk,
// where anything may have written them.
func (e Entry) Validate() error {
	if err := validRoom(e.Room); err != nil {
		return err
	}
	if e.PID <= 0 {
		return fmt.Errorf("pageserve: entry %q has no pid", e.Room)
	}
	if err := validURL(e.URL); err != nil {
		return err
	}
	if err := validPage(e.Page); err != nil {
		return err
	}
	// An owner is compared, never joined into a path (see sessionToken), so
	// the only rule is that it cannot carry something no session id could.
	if len(e.Owner) > 256 || hasControl(e.Owner) {
		return errors.New("pageserve: implausible owner")
	}
	return nil
}

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// validURL keeps the entry to what a page server can actually be: an HTTP
// address with a host. Deliberately NOT restricted to loopback, even though
// every server binds 127.0.0.1 today — a bind address is the server's decision
// to change, and a validator that quietly outlives that decision would present
// as a channel that never attaches.
func validURL(raw string) error {
	if raw == "" || len(raw) > 2048 || hasControl(raw) {
		return errors.New("pageserve: implausible url")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("pageserve: unparseable url %q", raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("pageserve: url scheme %q is not http", u.Scheme)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("pageserve: url %q has no host", raw)
	}
	if u.User != nil {
		return fmt.Errorf("pageserve: url %q carries credentials", raw)
	}
	return nil
}

// validPage: an absolute path, because that is what an advertiser writes
// (filepath.Abs) and because a relative one is meaningless to a reader in
// another working directory — the exact silent mismatch a channel cannot
// diagnose.
func validPage(p string) error {
	if p == "" || len(p) > 4096 || hasControl(p) {
		return errors.New("pageserve: implausible page")
	}
	if !filepath.IsAbs(p) {
		return fmt.Errorf("pageserve: page %q is not absolute", p)
	}
	return nil
}

// Write records e as <room>.json. It is atomic — temp file then rename — so a
// channel's scan never reads half an entry.
func (l Live) Write(e Entry) error {
	// Stamped here, not asked of the caller: an advertiser has no way to know
	// which generation the bytes it hands over will be written in, and a record
	// naming a version it was not written by is worse than one naming none.
	e.V = Version
	if err := e.Validate(); err != nil {
		return err
	}
	return l.write(e)
}

func (l Live) write(e Entry) error {
	dir, err := l.dir()
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	return tools.WriteFileAtomic(filepath.Join(dir, e.Room+".json"), append(raw, '\n'), 0o600)
}

// Remove deletes room's entry and its claim lock. A missing entry is not an
// error.
func (l Live) Remove(room string) error {
	if err := validRoom(room); err != nil {
		return err
	}
	dir, err := l.dir()
	if err != nil {
		return err
	}
	// The claim lock is a sibling of the entry (see Claim); it is not a *.json
	// file, so Inspect never sees it, but it should not outlive the entry.
	_ = os.Remove(filepath.Join(dir, room+".lock"))
	err = os.Remove(filepath.Join(dir, room+".json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// Claim atomically takes ownership of room's entry for newOwner, but ONLY when
// it is currently unowned or its owner's session is no longer live. It
// returns whether this call took ownership.
//
// This is what makes a page reach EXACTLY ONE channel. Without it, an
// unowned entry under two channels' roots (nested scopes: a parent and a
// child directory) is claimable by both, so every wake fans out to both
// sessions — the cross-session "broadcast". The first channel to attach
// stamps itself here; the next scanner then reads a live owner and declines.
// Unlike simply refusing an unowned entry, this leaves no silence: the
// channel that claimed it IS the listener.
//
// Serialized against other Claim callers on the same room by an advisory lock
// on a sibling <room>.lock file. The lock is on a SEPARATE, stable inode on
// purpose: Write replaces <room>.json via temp+rename, so an flock taken on
// the entry's own fd would not survive the swap — the loser would read the
// pre-rename inode and re-claim. The lock file is never a *.json entry, so
// Inspect ignores it; Remove deletes it with the entry.
func (l Live) Claim(room, newOwner string) (bool, error) {
	if newOwner == "" {
		return false, errors.New("pageserve: an empty owner cannot claim")
	}
	if err := validRoom(room); err != nil {
		return false, err
	}
	dir, err := l.dir()
	if err != nil {
		return false, err
	}
	lock, err := os.OpenFile(filepath.Join(dir, room+".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return false, err
	}
	defer func() { _ = lock.Close() }()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX); err != nil {
		return false, err
	}
	defer func() { _ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN) }()

	raw, err := os.ReadFile(filepath.Join(dir, room+".json"))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil // the entry is gone; there is nothing to claim
		}
		return false, err
	}
	var e Entry
	if err := json.Unmarshal(raw, &e); err != nil {
		return false, err
	}
	switch {
	case e.Owner == newOwner:
		return true, nil // already ours
	case e.Owner != "" && l.SessionLive(e.Owner):
		return false, nil // spoken for by a live session — not ours to take
	}
	e.Owner = newOwner
	if err := l.Write(e); err != nil {
		return false, err
	}
	return true, nil
}

// List reads every entry, reaping the dead: an entry whose PID no longer runs
// is removed rather than reported, because a stale entry tells a channel to
// attach to nothing — the "nobody listening" failure inverted.
func (l Live) List() ([]Entry, error) {
	entries, _, err := l.Inspect()
	return entries, err
}

// Problem is one file in the directory that could not be reported as a
// page, and why. It exists because SILENCE IS THE DEFECT: a channel that
// drops a file on the floor and says nothing leaves a reviewer typing into a
// page whose entry was refused for a reason nobody can see.
type Problem struct {
	File   string // base name, as a human would find it in the directory
	Reason string
}

func (p Problem) String() string { return p.File + ": " + p.Reason }

// Inspect is List plus the files it declined and why. List is the hot path
// (every scan); Inspect is what a diagnostic reads.
//
// THREE RULES, IN THIS ORDER, AND THE ORDER IS THE POINT:
//
//  1. VALIDATE, so a file this package would never have written is never
//     reported and never deleted — it is somebody else's, and reporting the
//     problem is the whole of what we owe it.
//  2. REAP THE DEAD, unchanged: a PID that no longer runs is a fact about the
//     world, and the file is ours to remove.
//  3. ONE ENTRY PER URL, because two entries at one address are one server,
//     and attaching to both turns one press into two rounds of work
//     proposed against one ask (measured, twice). The newest entry wins —
//     a re-advertise is the most recent truth about that server — with the
//     filename as a deterministic tie-break.
func (l Live) Inspect() ([]Entry, []Problem, error) {
	dir, err := l.dir()
	if err != nil {
		return nil, nil, err
	}
	names, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, nil, err
	}
	var problems []Problem
	type candidate struct {
		e    Entry
		name string
		mod  time.Time
	}
	var kept []candidate
	for _, name := range names {
		base := filepath.Base(name)
		raw, err := os.ReadFile(name)
		if err != nil {
			problems = append(problems, Problem{base, "unreadable: " + err.Error()})
			continue
		}
		var e Entry
		if err := json.Unmarshal(raw, &e); err != nil {
			problems = append(problems, Problem{base, "not valid JSON; left for a human"})
			continue
		}
		if future(e.V) {
			// REPORTED AND LEFT ALONE — never reaped. See Version: an entry
			// from a newer build may say things about liveness this build
			// cannot read, and deleting it would take a live page's only
			// entry away from every other reader on the machine too. It is
			// declined for the same reason rule 1 declines a file this package
			// would never have written: it is somebody else's.
			problems = append(problems, Problem{base, fmt.Sprintf(
				"entry is at schema v%d and this %s knows v%d; left for a newer %s", e.V, l.Tool, Version, l.Tool)})
			continue
		}
		if err := e.Validate(); err != nil {
			problems = append(problems, Problem{base, err.Error()})
			continue
		}
		// The filename IS the room — it is what Remove deletes and what
		// galley's websocket gate checks — so an entry naming a different one
		// is a lie about which room it is, and no channel should act on it.
		if room := strings.TrimSuffix(base, ".json"); room != e.Room {
			problems = append(problems, Problem{base, fmt.Sprintf("names room %q but is filed as %q", e.Room, room)})
			continue
		}
		if !tools.PIDAlive(e.PID) {
			_ = os.Remove(name)
			continue
		}
		mod := time.Time{}
		if st, err := os.Stat(name); err == nil {
			mod = st.ModTime()
		}
		kept = append(kept, candidate{e, base, mod})
	}
	// Newest first, filename as the tie-break, so the survivor of a duplicate
	// is the same one on every scan.
	sort.SliceStable(kept, func(i, j int) bool {
		if !kept[i].mod.Equal(kept[j].mod) {
			return kept[i].mod.After(kept[j].mod)
		}
		return kept[i].name < kept[j].name
	})
	seen := map[string]string{}
	var out []Entry
	for _, c := range kept {
		if first, dup := seen[c.e.URL]; dup {
			problems = append(problems, Problem{c.name,
				"a second entry for the server already advertised by " + first + "; ignored so one press is one wake"})
			continue
		}
		seen[c.e.URL] = c.name
		out = append(out, c.e)
	}
	return out, problems, nil
}

// --- session presence ---
//
// WHO IS STILL HERE. An entry carries the id of the session that opened the
// page, and without this nothing on the machine could answer "is that session
// still running?" — so an entry stamped by a session that has since restarted
// would be foreign to every channel forever, and nothing would say so.
//
// THE PID IN THE ENTRY CANNOT ANSWER IT: that PID is the SERVER's, and the
// server outlives the session that started it — a browser tab left open
// across a Claude Code restart is exactly the case, and it is an ordinary
// morning. The only process on the machine whose life is the session's own
// life is the session's CHANNEL: the harness spawns it at session start and it
// dies with the session. So a channel announces itself here, keyed by session
// id and carrying its own PID, and "is that session live" becomes the same
// question List already answers about servers — does this PID still run —
// with the same reaper.
//
// A presence file is ADVISORY. Its absence never means a session is dead in
// some verifiable sense; it means nothing here is listening for that session.

// Session is one live session's presence record.
//
// ID IS SPELLED `session` ON DISK, so the Go name and the key differ and a
// rename on either side looks harmless from the other — which is precisely the
// case the golden key-set test exists for. A record whose `session` key moved
// decodes to an empty ID, SessionLive's closing equality fails, and every
// page that session owns quietly stops being seen as owned.
type Session struct {
	// V is the schema generation — see Version.
	V   int    `json:"v"`
	ID  string `json:"session"`
	PID int    `json:"pid"`
}

// sub is a SUBDIRECTORY of the live directory. Inspect globs "*.json" in the
// directory itself and does not recurse, so presence records, logs and port
// hints there can never be mistaken for entries by a reader that has not been
// told about them.
func (l Live) sub(name string) (string, error) {
	dir, err := l.dir()
	if err != nil {
		return "", err
	}
	sub := filepath.Join(dir, name)
	return sub, os.MkdirAll(sub, 0o700)
}

// sessionFile names the presence file for a session id. A session id comes
// from the caller's harness, so it is not this package's to trust as a
// filename: anything that is not a plain, short, path-free token is HASHED
// rather than refused, because refusing would leave the session with no
// presence at all and turn an odd id into permanent invisibility.
func (l Live) sessionFile(id string) (string, error) {
	dir, err := l.sub("sessions")
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", errors.New("pageserve: empty session id")
	}
	return filepath.Join(dir, sessionToken(id)+".json"), nil
}

func sessionToken(id string) string {
	plain := len(id) <= 64 && id != "." && id != ".."
	if plain {
		for _, r := range id {
			if r != '-' && r != '_' && r != '.' &&
				(r < '0' || r > '9') && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
				plain = false
				break
			}
		}
	}
	if plain {
		return id
	}
	sum := sha256.Sum256([]byte(id))
	return "h-" + hex.EncodeToString(sum[:16])
}

// Token is the filename a session id is kept under, exported for a caller
// that needs a filename derived from an id: galley names a server's log after
// the page it serves, and a page path is not a filename.
func Token(id string) string { return sessionToken(id) }

// LogDir is where a tool sends a server's stdout and stderr — a subdirectory,
// so a log is never mistaken for an entry. 0o700 like everything else here.
func (l Live) LogDir() (string, error) { return l.sub("logs") }

// AnnounceSession records that this process is listening for id. Called by a
// channel at startup; safe to call more than once.
func (l Live) AnnounceSession(id string) error {
	name, err := l.sessionFile(id)
	if err != nil {
		return err
	}
	raw, err := json.MarshalIndent(Session{V: Version, ID: id, PID: os.Getpid()}, "", "  ")
	if err != nil {
		return err
	}
	return tools.WriteFileAtomic(name, append(raw, '\n'), 0o600)
}

// WithdrawSession removes this session's presence record. Best-effort by
// design: a session that dies without running it leaves a record whose PID is
// dead, and SessionLive reaps that on the next read — the same guarantee the
// entry side has always had.
func (l Live) WithdrawSession(id string) error {
	name, err := l.sessionFile(id)
	if err != nil {
		return err
	}
	err = os.Remove(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	return err
}

// SessionLive reports whether a channel is currently listening for id. A
// record whose PID no longer runs is removed on read, exactly as List reaps a
// dead entry: a stale presence file would pin a page to a session that cannot
// receive anything, which is the silence this whole mechanism exists to end.
func (l Live) SessionLive(id string) bool {
	if id == "" {
		return false
	}
	name, err := l.sessionFile(id)
	if err != nil {
		return false
	}
	raw, err := os.ReadFile(name)
	if err != nil {
		return false
	}
	var s Session
	if json.Unmarshal(raw, &s) != nil {
		return false
	}
	if future(s.V) {
		// NOT LIVE, AND NOT REAPED. This build cannot read a newer record's
		// rules, and the section comment above says which way to err: a
		// presence file is advisory and its absence means only that nothing
		// HERE is listening. Removing it would be this build deciding a
		// question it has just admitted it cannot answer.
		return false
	}
	if !tools.PIDAlive(s.PID) {
		_ = os.Remove(name)
		return false
	}
	// The token is a hash for an unusual id, so confirm the record is really
	// the one asked for rather than a collision or a hand-edited file.
	return s.ID == id
}
