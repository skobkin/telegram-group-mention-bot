package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

// baseline creates the schema for new databases and normalises databases
// created by earlier releases (which used GORM AutoMigrate):
//   - drops the long-gone created_at/updated_at/deleted_at columns,
//   - moves username/first_name/last_name from group_members into users,
//   - removes orphan rows that were left behind while foreign keys were not enforced.
func baseline(ctx context.Context, tx *sql.Tx) error {
	if err := exec(ctx, tx,
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY,
			username TEXT,
			first_name TEXT,
			last_name TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS mention_groups (
			id INTEGER PRIMARY KEY,
			name TEXT,
			chat_id INTEGER
		)`,
		`CREATE TABLE IF NOT EXISTS group_members (
			id INTEGER PRIMARY KEY,
			group_id INTEGER,
			user_id INTEGER,
			CONSTRAINT fk_mention_groups_members FOREIGN KEY (group_id) REFERENCES mention_groups(id) ON DELETE CASCADE,
			CONSTRAINT fk_group_members_user FOREIGN KEY (user_id) REFERENCES users(id)
		)`,
	); err != nil {
		return err
	}

	if err := migrateLegacyMemberData(ctx, tx); err != nil {
		return err
	}

	for _, table := range []string{"mention_groups", "group_members"} {
		for _, column := range []string{"created_at", "updated_at", "deleted_at"} {
			if err := dropColumnIfExists(ctx, tx, table, column); err != nil {
				return err
			}
		}
	}

	return exec(ctx, tx,
		`DELETE FROM group_members WHERE group_id NOT IN (SELECT id FROM mention_groups)`,
		`INSERT OR IGNORE INTO users (id) SELECT DISTINCT user_id FROM group_members`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_chat_group ON mention_groups (name, chat_id)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_group_user ON group_members (group_id, user_id)`,
	)
}

func migrateLegacyMemberData(ctx context.Context, tx *sql.Tx) error {
	legacy, err := hasColumn(ctx, tx, "group_members", "username")
	if err != nil {
		return err
	}

	if !legacy {
		return nil
	}

	if err := exec(ctx, tx,
		`INSERT INTO users (id, username, first_name, last_name)
		 SELECT user_id, username, first_name, last_name FROM group_members WHERE true
		 ON CONFLICT(id) DO UPDATE SET
			username = excluded.username,
			first_name = excluded.first_name,
			last_name = excluded.last_name`,
	); err != nil {
		return err
	}

	for _, column := range []string{"username", "first_name", "last_name"} {
		if err := dropColumnIfExists(ctx, tx, "group_members", column); err != nil {
			return err
		}
	}

	return nil
}

func dropColumnIfExists(ctx context.Context, tx *sql.Tx, table, column string) error {
	exists, err := hasColumn(ctx, tx, table, column)
	if err != nil {
		return err
	}

	if !exists {
		return nil
	}

	return exec(ctx, tx, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s", table, column))
}
