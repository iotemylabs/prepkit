// Package store is the SQLite persistence layer.
//
// Conventions: TEXT UUIDs, ISO-8601 date strings parsed in Go (never SQLite
// date functions), NULL for absent optional values. Invariants the schema
// cannot express — container-parentage cycles, and the rule that an item's
// quantity is NULL whenever it has lots — are enforced here, on write,
// inside the writing transaction.
//
// Nothing in this package logs. Row content includes fields classified
// sensitive (serial, cost, needs, document bodies); errors returned upward
// carry ids, not field values.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/iotemylabs/prepkit/internal/model"
	"github.com/iotemylabs/prepkit/migrations"

	_ "modernc.org/sqlite"
)

var (
	// ErrNotFound is returned when the identified record does not exist.
	ErrNotFound = errors.New("not found")
	// ErrContainerCycle is returned when a container write would make a
	// container its own ancestor.
	ErrContainerCycle = errors.New("container parentage would form a cycle")
	// ErrHasLots is returned when setting an item's own quantity while lots
	// exist; the effective quantity is derived from lots so the two can
	// never disagree.
	ErrHasLots = errors.New("item has lots; its quantity is derived from them and cannot be set directly")
	// ErrInUse is returned when deleting a record that other records still
	// reference (a container with children or contents).
	ErrInUse = errors.New("record is still referenced by other records")
	// ErrValidation wraps all field-level validation failures.
	ErrValidation = errors.New("validation failed")
)

func vErrf(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrValidation, fmt.Sprintf(format, args...))
}

// Store wraps the database. A single connection is used: this is a
// single-household application and serialized writes are simpler than
// busy-handling.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the database at path and applies any
// pending migrations.
func Open(path string) (*Store, error) {
	return open(path, 0)
}

// open applies migrations up to version upTo (0 = all); tests use partial
// application to prove migrations run over populated databases.
func open(path string, upTo int) (*Store, error) {
	dsn := "file:" + path + "?_pragma=foreign_keys(1)&_pragma=journal_mode(wal)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(context.Background(), upTo); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.ensureInstance(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// migrate applies every embedded migration whose version is not yet in
// schema_migrations, in version order, each in its own transaction.
func (s *Store) migrate(ctx context.Context, upTo int) error {
	if _, err := s.db.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("creating schema_migrations: %w", err)
	}

	applied := map[int]bool{}
	rows, err := s.db.QueryContext(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return err
	}
	type mig struct {
		version int
		name    string
	}
	var pending []mig
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		numStr, _, ok := strings.Cut(name, "_")
		if !ok {
			return fmt.Errorf("migration %q: name must be NNN_description.sql", name)
		}
		v, err := strconv.Atoi(numStr)
		if err != nil {
			return fmt.Errorf("migration %q: bad version prefix: %w", name, err)
		}
		if applied[v] || (upTo > 0 && v > upTo) {
			continue
		}
		pending = append(pending, mig{v, name})
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].version < pending[j].version })

	for _, m := range pending {
		sqlText, err := fs.ReadFile(migrations.FS, m.name)
		if err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, string(sqlText)); err != nil {
			tx.Rollback()
			return fmt.Errorf("applying migration %s: %w", m.name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
			m.version, time.Now().UTC().Format(time.RFC3339)); err != nil {
			tx.Rollback()
			return fmt.Errorf("recording migration %s: %w", m.name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("committing migration %s: %w", m.name, err)
		}
	}
	return nil
}

func (s *Store) ensureInstance(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO instance (id, instance_id, created_at) VALUES (1, ?, ?)`,
		NewID(), time.Now().UTC().Format(time.RFC3339))
	return err
}

// InstanceID returns the stable per-installation identifier.
func (s *Store) InstanceID(ctx context.Context) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `SELECT instance_id FROM instance WHERE id = 1`).Scan(&id)
	return id, err
}

// inTx runs fn inside a transaction, committing on nil and rolling back on
// error.
func (s *Store) inTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// --- NULL conversion helpers -------------------------------------------

// nz maps "" to NULL on write.
func nz(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// nzf maps a nil *float64 to NULL on write.
func nzf(f *float64) any {
	if f == nil {
		return nil
	}
	return *f
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// str maps NULL to "" on read.
func str(ns sql.NullString) string { return ns.String }

// fptr maps NULL to nil on read.
func fptr(nf sql.NullFloat64) *float64 {
	if !nf.Valid {
		return nil
	}
	v := nf.Float64
	return &v
}

// validDateField checks an optional date field.
func validDateField(field, value string) error {
	if value == "" {
		return nil
	}
	if _, err := model.ParseDate(value); err != nil {
		return vErrf("%s: %v", field, err)
	}
	return nil
}
