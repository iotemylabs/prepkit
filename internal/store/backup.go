package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
)

// Backup writes a consistent, self-contained copy of the database to dest. It
// is safe while the server is running: VACUUM INTO reads one snapshot and
// folds in anything still in the write-ahead log, which copying prepkit.db by
// hand would miss.
//
// The copy holds everything, including document kinds that never appear in an
// export, and it is not encrypted. dest is therefore created owner-only, and
// an existing file is refused rather than overwritten.
func (s *Store) Backup(ctx context.Context, dest string) (err error) {
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("creating backup file: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("creating backup file: %w", err)
	}
	defer func() {
		if err != nil {
			os.Remove(dest)
		}
	}()

	if _, err := s.db.ExecContext(ctx, `VACUUM INTO ?`, dest); err != nil {
		return fmt.Errorf("writing backup: %w", err)
	}
	return verifyBackup(ctx, dest)
}

// verifyBackup proves the file just written is a readable database, so a bad
// backup is reported now rather than discovered during a restore.
func verifyBackup(ctx context.Context, path string) error {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		return fmt.Errorf("verifying backup: %w", err)
	}
	defer db.Close()

	var result string
	if err := db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&result); err != nil {
		return fmt.Errorf("verifying backup: %w", err)
	}
	if result != "ok" {
		return errors.New("verifying backup: integrity check failed: " + result)
	}
	return nil
}
