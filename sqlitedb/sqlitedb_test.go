package sqlitedb

import (
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var ctx = context.Background()

var (
	stepA = SQL(`CREATE TABLE parent (id INTEGER PRIMARY KEY);
CREATE TABLE child (id INTEGER PRIMARY KEY, parent_id INTEGER NOT NULL REFERENCES parent(id));`)
	stepB = SQL(`ALTER TABLE parent ADD COLUMN name TEXT NOT NULL DEFAULT ''`)
)

func dbPath(t *testing.T) string {
	return filepath.Join(t.TempDir(), "state", "tool.db")
}

func open(t *testing.T, path string, opts Options) *DB {
	t.Helper()
	d, err := Open(ctx, path, opts)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func version(t *testing.T, d *DB) int {
	t.Helper()
	v, err := d.Version(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestFreshDatabaseGetsConventionsAndSchema(t *testing.T) {
	d := open(t, dbPath(t), Options{Migrations: []Step{stepA, stepB}})
	if v := version(t, d); v != 2 {
		t.Fatalf("user_version %d, want 2", v)
	}
	var mode string
	if err := d.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode %q (%v), want wal", mode, err)
	}
	var busy int
	if err := d.QueryRow("PRAGMA busy_timeout").Scan(&busy); err != nil || busy != 5000 {
		t.Fatalf("busy_timeout %d (%v), want 5000", busy, err)
	}
	if n := d.Stats().MaxOpenConnections; n != 1 {
		t.Fatalf("MaxOpenConnections %d, want 1", n)
	}
	if _, err := d.Exec("INSERT INTO parent(id, name) VALUES (1, 'x')"); err != nil {
		t.Fatalf("stepB's column missing: %v", err)
	}
}

func TestForeignKeysOnByDefault(t *testing.T) {
	d := open(t, dbPath(t), Options{Migrations: []Step{stepA}})
	if _, err := d.Exec("INSERT INTO child(id, parent_id) VALUES (1, 42)"); err == nil {
		t.Fatal("foreign key not enforced")
	}
}

func TestNoForeignKeysLeavesEnforcementOff(t *testing.T) {
	d := open(t, dbPath(t), Options{Migrations: []Step{stepA}, NoForeignKeys: true})
	var on int
	if err := d.QueryRow("PRAGMA foreign_keys").Scan(&on); err != nil || on != 0 {
		t.Fatalf("foreign_keys %d (%v), want 0", on, err)
	}
	if _, err := d.Exec("INSERT INTO child(id, parent_id) VALUES (1, 42)"); err != nil {
		t.Fatalf("insert refused with enforcement off: %v", err)
	}
}

func TestFileModes(t *testing.T) {
	p := dbPath(t)
	d := open(t, p, Options{Migrations: []Step{stepA}})
	// A write keeps the -wal and -shm files on disk while the connection is open.
	if _, err := d.Exec("INSERT INTO parent(id) VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{p, p + "-wal", p + "-shm"} {
		fi, err := os.Stat(f)
		if err != nil {
			t.Fatalf("%s: %v", filepath.Base(f), err)
		}
		if got := fi.Mode().Perm(); got != 0o600 {
			t.Errorf("%s mode %v, want 0600", filepath.Base(f), got)
		}
	}
	fi, err := os.Stat(filepath.Dir(p))
	if err != nil {
		t.Fatal(err)
	}
	if got := fi.Mode().Perm(); got != 0o700 {
		t.Errorf("created parent dir mode %v, want 0700", got)
	}
}

func TestOpenTightensAnExistingLooseFile(t *testing.T) {
	p := dbPath(t)
	d := open(t, p, Options{})
	_ = d.Close()
	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	open(t, p, Options{})
	fi, _ := os.Stat(p)
	if got := fi.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode %v after reopen, want 0600", got)
	}
}

func TestUpgradeAcrossTwoSteps(t *testing.T) {
	p := dbPath(t)
	d := open(t, p, Options{Migrations: []Step{stepA}})
	if _, err := d.Exec("INSERT INTO parent(id) VALUES (7)"); err != nil {
		t.Fatal(err)
	}
	_ = d.Close()

	third := SQL(`CREATE TABLE notes (id INTEGER PRIMARY KEY)`)
	d = open(t, p, Options{Migrations: []Step{stepA, stepB, third}})
	if v := version(t, d); v != 3 {
		t.Fatalf("user_version %d, want 3", v)
	}
	var name string
	if err := d.QueryRow("SELECT name FROM parent WHERE id = 7").Scan(&name); err != nil {
		t.Fatalf("row written at v1 did not survive the upgrade: %v", err)
	}
	if _, err := d.Exec("INSERT INTO notes(id) VALUES (1)"); err != nil {
		t.Fatal(err)
	}
}

func TestFailingStepLeavesPreviousVersionIntact(t *testing.T) {
	p := dbPath(t)
	d := open(t, p, Options{Migrations: []Step{stepA}})
	_ = d.Close()

	boom := errors.New("boom")
	failing := func(ctx context.Context, tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `CREATE TABLE half (id INTEGER)`); err != nil {
			return err
		}
		return boom
	}
	_, err := Open(ctx, p, Options{Migrations: []Step{stepA, failing}})
	if !errors.Is(err, boom) || !strings.Contains(err.Error(), "migration 2") {
		t.Fatalf("Open error %v, want the step's error naming migration 2", err)
	}

	d = open(t, p, Options{Migrations: []Step{stepA}})
	if v := version(t, d); v != 1 {
		t.Fatalf("user_version %d after a failed step, want 1", v)
	}
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'half'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("failed step's table survived (%d, %v)", n, err)
	}
}

func TestRefusesNewerDatabase(t *testing.T) {
	p := dbPath(t)
	d := open(t, p, Options{Migrations: []Step{stepA, stepB}})
	_ = d.Close()

	_, err := Open(ctx, p, Options{Migrations: []Step{stepA}})
	var newer *NewerError
	if !errors.As(err, &newer) {
		t.Fatalf("Open error %v, want *NewerError", err)
	}
	if newer.Have != 2 || newer.Known != 1 || newer.Path != p {
		t.Fatalf("NewerError %+v", newer)
	}
	if msg := err.Error(); !strings.Contains(msg, "2") || !strings.Contains(msg, "1") {
		t.Fatalf("message %q must name both versions", msg)
	}
}

func TestRefusesUnversionedDatabaseByDefault(t *testing.T) {
	p := dbPath(t)
	raw := rawDB(t, p)
	if _, err := raw.Exec(`CREATE TABLE legacy (id INTEGER)`); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()

	_, err := Open(ctx, p, Options{Migrations: []Step{stepA}})
	if !errors.Is(err, ErrUnversioned) {
		t.Fatalf("Open error %v, want ErrUnversioned", err)
	}
}

// musterSchema is muster's internal/store/schema.sql, unchanged through muster
// dev c0c411d:
// a real unversioned database, the case AdoptUnversioned exists for.
//
//go:embed testdata/muster-schema.sql
var musterSchema string

// musterAlters is muster's store.migrate() ALTER list at muster dev c0c411d.
// schema.sql does not carry the last several columns, so on a database built
// from schema.sql alone the later entries genuinely add columns.
var musterAlters = []string{
	`ALTER TABLE agents ADD COLUMN project TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE agents ADD COLUMN label TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE agents ADD COLUMN label_manual INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE agents ADD COLUMN last_read_at INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE events ADD COLUMN target TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE threads ADD COLUMN intent TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE agents ADD COLUMN last_read_entry_id INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE threads ADD COLUMN origin_project TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE agents ADD COLUMN departed INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE agents ADD COLUMN session_created INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE agents ADD COLUMN device_id TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE agents ADD COLUMN harness_session_id TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE agents ADD COLUMN superseded_by TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE agents ADD COLUMN device_name TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE agents ADD COLUMN transcript_path TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE agents ADD COLUMN last_read_standing_entry_id INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE threads ADD COLUMN standing INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE threads ADD COLUMN standing_key TEXT NOT NULL DEFAULT ''`,
	`ALTER TABLE threads ADD COLUMN standing_retracted INTEGER NOT NULL DEFAULT 0`,
	`ALTER TABLE threads ADD COLUMN wake INTEGER NOT NULL DEFAULT 0`,
}

// musterStep1 is how muster would adopt the module: its embedded schema
// (CREATE TABLE IF NOT EXISTS throughout) plus its additive ALTERs, each
// tolerating a column that is already there. Idempotent by construction.
// (muster's re-runnable backfills would follow the ALTERs in the same step.)
func musterStep1(ctx context.Context, tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, musterSchema); err != nil {
		return err
	}
	for _, ddl := range musterAlters {
		if _, err := tx.ExecContext(ctx, ddl); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			return err
		}
	}
	return nil
}

func hasColumn(t *testing.T, d *DB, table, column string) bool {
	t.Helper()
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM pragma_table_info(?) WHERE name = ?`, table, column).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n == 1
}

func TestAdoptUnversionedMusterDatabase(t *testing.T) {
	p := dbPath(t)
	// An unversioned muster database from before its later ALTERs: no
	// user_version, no foreign keys, schema.sql applied as-is.
	raw := rawDB(t, p)
	if _, err := raw.Exec(musterSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO agents(alias, registered_at, last_seen) VALUES ('keeper', 1, 2)`); err != nil {
		t.Fatal(err)
	}
	_ = raw.Close()

	later := SQL(`CREATE TABLE IF NOT EXISTS later (id INTEGER PRIMARY KEY)`)
	d := open(t, p, Options{
		Migrations:       []Step{musterStep1, later},
		NoForeignKeys:    true,
		AdoptUnversioned: true,
	})
	if v := version(t, d); v != 2 {
		t.Fatalf("user_version %d, want 2", v)
	}
	var alias string
	if err := d.QueryRow(`SELECT alias FROM agents`).Scan(&alias); err != nil || alias != "keeper" {
		t.Fatalf("existing row lost: %q, %v", alias, err)
	}
	for _, col := range []string{"harness_session_id", "transcript_path"} {
		if !hasColumn(t, d, "agents", col) {
			t.Fatalf("step 1 did not add agents.%s to the adopted database", col)
		}
	}
	_ = d.Close()

	// An unversioned database that already has every column (muster today)
	// adopts too: every ALTER hits the duplicate-column guard.
	current := dbPath(t)
	raw = rawDB(t, current)
	if _, err := raw.Exec(musterSchema); err != nil {
		t.Fatal(err)
	}
	for _, ddl := range musterAlters {
		if _, err := raw.Exec(ddl); err != nil && !strings.Contains(err.Error(), "duplicate column name") {
			t.Fatal(err)
		}
	}
	_ = raw.Close()
	if v := version(t, open(t, current, Options{Migrations: []Step{musterStep1, later}, NoForeignKeys: true, AdoptUnversioned: true})); v != 2 {
		t.Fatalf("fully-altered database user_version %d, want 2", v)
	}

	// A fresh machine takes the same path from an empty file.
	fresh := open(t, dbPath(t), Options{Migrations: []Step{musterStep1, later}, NoForeignKeys: true, AdoptUnversioned: true})
	if v := version(t, fresh); v != 2 {
		t.Fatalf("fresh user_version %d, want 2", v)
	}
}

func TestTxCommitsOnNil(t *testing.T) {
	d := open(t, dbPath(t), Options{Migrations: []Step{stepA}})
	if err := d.Tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO parent(id) VALUES (1)")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if n := count(t, d); n != 1 {
		t.Fatalf("%d rows after commit, want 1", n)
	}
}

func TestTxRollsBackOnError(t *testing.T) {
	d := open(t, dbPath(t), Options{Migrations: []Step{stepA}})
	boom := errors.New("boom")
	err := d.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec("INSERT INTO parent(id) VALUES (1)"); err != nil {
			return err
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("Tx error %v, want boom", err)
	}
	if n := count(t, d); n != 0 {
		t.Fatalf("%d rows after rollback, want 0", n)
	}
}

func TestTxRollsBackOnPanicAndRepanics(t *testing.T) {
	d := open(t, dbPath(t), Options{Migrations: []Step{stepA}})
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("panic was swallowed")
			}
		}()
		_ = d.Tx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.Exec("INSERT INTO parent(id) VALUES (1)"); err != nil {
				return err
			}
			panic("mid-transaction")
		})
	}()
	// The single connection must be free again, or this would block.
	if n := count(t, d); n != 0 {
		t.Fatalf("%d rows after a panic, want 0", n)
	}
}

func rawDB(t *testing.T, p string) *sql.DB {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", "file:"+p+"?_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	return raw
}

func count(t *testing.T, d *DB) int {
	t.Helper()
	var n int
	if err := d.QueryRow("SELECT count(*) FROM parent").Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// A path with URI-significant characters must open that exact file: '?' would
// otherwise start the query string and '#' a fragment, leaving the module
// chmodding one file while SQLite writes another.
func TestPathWithURICharacters(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "odd ?#% dir")
	p := filepath.Join(dir, "a&b?c#d%20e.db")
	d := open(t, p, Options{Migrations: []Step{stepA}})
	if _, err := d.Exec("INSERT INTO parent(id) VALUES (1)"); err != nil {
		t.Fatal(err)
	}
	_ = d.Close()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if len(names) != 1 || names[0] != "a&b?c#d%20e.db" {
		t.Fatalf("files in dir: %q, want exactly the database", names)
	}
	d = open(t, p, Options{Migrations: []Step{stepA}})
	if n := count(t, d); n != 1 {
		t.Fatalf("reopened %d rows, want 1: a different file was opened", n)
	}
}
