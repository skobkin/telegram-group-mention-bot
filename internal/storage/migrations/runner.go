// Package migrations applies versioned SQLite schema migrations tracked in
// PRAGMA user_version. Each migration runs in its own transaction together
// with the version bump, so a failed step leaves the database untouched.
package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
)

type step struct {
	version int
	name    string
	apply   func(ctx context.Context, tx *sql.Tx) error
}

// steps is ordered by version. Append new migrations at the end; never edit applied ones.
var steps = []step{
	{version: 1, name: "baseline", apply: baseline},
	{version: 2, name: "case_insensitive_names_and_aliases", apply: caseInsensitiveNamesAndAliases},
}

// TargetVersion is the schema version produced by the latest migration.
func TargetVersion() int {
	return steps[len(steps)-1].version
}

// Apply brings the database up to the latest schema version.
func Apply(ctx context.Context, db *sql.DB) error {
	version, err := readVersion(ctx, db)
	if err != nil {
		return err
	}

	if version > TargetVersion() {
		return fmt.Errorf("database schema version %d is newer than supported version %d", version, TargetVersion())
	}

	for _, s := range steps {
		if version >= s.version {
			continue
		}

		slog.Info("storage: Applying migration", "version", s.version, "name", s.name)

		if err := applyStep(ctx, db, s); err != nil {
			return fmt.Errorf("apply migration %d (%s): %w", s.version, s.name, err)
		}

		version = s.version
	}

	return nil
}

func applyStep(ctx context.Context, db *sql.DB, s step) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	if err := s.apply(ctx, tx); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", s.version)); err != nil {
		return fmt.Errorf("set schema version: %w", err)
	}

	return tx.Commit()
}

func readVersion(ctx context.Context, db *sql.DB) (int, error) {
	var version int
	if err := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}

	return version, nil
}

func exec(ctx context.Context, tx *sql.Tx, statements ...string) error {
	for _, stmt := range statements {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("%w (statement: %.60s...)", err, stmt)
		}
	}

	return nil
}

func hasColumn(ctx context.Context, tx *sql.Tx, table, column string) (bool, error) {
	rows, err := tx.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			cid        int
			name, typ  string
			notNull    int
			defaultVal sql.NullString
			pk         int
		)
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultVal, &pk); err != nil {
			return false, err
		}

		if name == column {
			return true, nil
		}
	}

	return false, rows.Err()
}
