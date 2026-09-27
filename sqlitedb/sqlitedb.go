// Package sqlitedb opens a family tool's local SQLite database the family way
// and migrates it. It is a separate module (its own go.mod and
// sqlitedb/vX.Y.Z tags) so the root tools-common module stays stdlib-only and
// a tool that never touches SQLite never pulls in the driver.
//
// The conventions, in one place:
//
//   - driver modernc.org/sqlite (pure Go, cross-compiles with CGO_ENABLED=0);
//   - WAL, so readers are not blocked by the writer;
//   - a 5 s busy timeout, because two processes of one tool may write at once;
//   - one connection (SetMaxOpenConns(1)): local databases are small and the
//     work is bursty, so a pool buys nothing and costs SQLITE_BUSY;
//   - foreign keys on, unless Options.NoForeignKeys;
//   - the database and its -wal and -shm files 0600, the parent directory
//     created 0700 when missing;
//   - schema changes as an append-only list of steps, tracked in SQLite's own
//     PRAGMA user_version. Each step runs in its own transaction with the
//     version bump inside it, so an interrupted upgrade stops at a version
//     that exists. A database newer than the list is refused.
//
// The package decides nothing about schemas: no table helpers, no ORM.
package sqlitedb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// Step moves the schema from one version to the next. It runs inside tx; the
// caller of Open never sees a half-applied step. A step may do more than DDL
// (backfills, data rewrites) as long as it uses tx.
//
// Steps are append-only: an existing entry has already run on somebody's
// machine, so fix a mistake with a new step, never by editing an old one.
type Step func(ctx context.Context, tx *sql.Tx) error

// SQL is a Step that executes one string, which may hold several statements
// separated by semicolons.
func SQL(stmts string) Step {
	return func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, stmts)
		return err
	}
}

// Options configures Open. The zero value is the family default with no
// migrations.
type Options struct {
	// Migrations[i] moves the schema from user_version i to i+1. The
	// database's target version is len(Migrations).
	Migrations []Step

	// NoForeignKeys leaves SQLite's foreign-key enforcement off (SQLite's own
	// default). The family default is on; this exists so a tool whose schema
	// was never written for enforcement (muster) can adopt the module without
	// a behaviour change.
	NoForeignKeys bool

	// AdoptUnversioned declares that Migrations[0] is idempotent, so a
	// database that already has tables but was never versioned
	// (user_version 0) may be upgraded by running it: typically CREATE TABLE
	// IF NOT EXISTS plus ALTERs that tolerate an existing column. Without it,
	// Open refuses such a database (ErrUnversioned) rather than run a first
	// step that was written for an empty file.
	AdoptUnversioned bool
}

// DB is an open, migrated database.
type DB struct {
	*sql.DB
	// Path is the file the database lives in.
	Path string
}

// ErrUnversioned is returned (wrapped) by Open for a database at
// user_version 0 that already holds schema objects, when the caller did not
// set Options.AdoptUnversioned.
var ErrUnversioned = errors.New("database has tables but no schema version")

// NewerError is returned by Open when the database was written by a newer
// binary than this one: its user_version is past the end of Migrations.
type NewerError struct {
	Path  string
	Have  int // the database's user_version
	Known int // len(Migrations): the newest version this binary knows
}

func (e *NewerError) Error() string {
	return fmt.Sprintf("%s: schema version %d is newer than this binary knows (%d); update the tool", e.Path, e.Have, e.Known)
}

// Open opens (creating if needed) the database at path, applies the family
// pragmas, runs any pending Migrations, and makes the files 0600.
func Open(ctx context.Context, path string, opts Options) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	// Create the file 0600 before SQLite does. SQLite gives the -wal and -shm
	// files the main file's mode, so this is what keeps all three private
	// from the first byte instead of from the chmod below.
	if f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600); err != nil {
		return nil, err
	} else if err := f.Close(); err != nil {
		return nil, err
	}

	dsn := "file:" + filepath.ToSlash(path) + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	if !opts.NoForeignKeys {
		dsn += "&_pragma=foreign_keys(1)"
	}
	sdb, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	sdb.SetMaxOpenConns(1)
	d := &DB{DB: sdb, Path: path}
	if err := d.migrate(ctx, opts); err != nil {
		_ = sdb.Close()
		return nil, err
	}
	if err := secure(path); err != nil {
		_ = sdb.Close()
		return nil, err
	}
	return d, nil
}

// secure makes the database and its sidecar files 0600. It matters for a
// database created before this module (or by another tool) with looser modes.
func secure(path string) error {
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Chmod(p, 0o600); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func (d *DB) migrate(ctx context.Context, opts Options) error {
	have, err := d.Version(ctx)
	if err != nil {
		return err
	}
	known := len(opts.Migrations)
	if have > known {
		return &NewerError{Path: d.Path, Have: have, Known: known}
	}
	if have == 0 && known > 0 && !opts.AdoptUnversioned {
		var n int
		if err := d.QueryRowContext(ctx,
			`SELECT count(*) FROM sqlite_master WHERE name NOT LIKE 'sqlite_%'`).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return fmt.Errorf("%s: %w (set AdoptUnversioned if the first migration is idempotent)", d.Path, ErrUnversioned)
		}
	}
	for ; have < known; have++ {
		if err := d.Tx(ctx, func(tx *sql.Tx) error {
			if err := opts.Migrations[have](ctx, tx); err != nil {
				return err
			}
			// Pragmas take no bound parameters; have is our own loop counter.
			_, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", have+1))
			return err
		}); err != nil {
			return fmt.Errorf("%s: migration %d: %w", d.Path, have+1, err)
		}
	}
	return nil
}

// Version reports the database's schema version (PRAGMA user_version).
func (d *DB) Version(ctx context.Context) (int, error) {
	var v int
	err := d.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v)
	return v, err
}

// Tx runs fn in one transaction: it commits when fn returns nil and rolls
// back when fn returns an error or panics (the panic is re-raised).
func (d *DB) Tx(ctx context.Context, fn func(*sql.Tx) error) (err error) {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
