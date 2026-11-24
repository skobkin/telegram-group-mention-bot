package migrations

import (
	"context"
	"database/sql"
	"strings"
)

// caseInsensitiveNamesAndAliases lowercases existing group names (merging groups whose
// names collide within a chat after lowercasing) and adds the group_aliases table.
func caseInsensitiveNamesAndAliases(ctx context.Context, tx *sql.Tx) error {
	if err := lowercaseGroupNames(ctx, tx); err != nil {
		return err
	}

	return exec(ctx, tx,
		`CREATE TABLE IF NOT EXISTS group_aliases (
			id INTEGER PRIMARY KEY,
			chat_id INTEGER,
			alias TEXT,
			group_id INTEGER,
			CONSTRAINT fk_group_aliases_group FOREIGN KEY (group_id) REFERENCES mention_groups(id) ON DELETE CASCADE
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_chat_alias ON group_aliases (chat_id, alias)`,
		`CREATE INDEX IF NOT EXISTS idx_group_aliases_group ON group_aliases (group_id)`,
	)
}

func lowercaseGroupNames(ctx context.Context, tx *sql.Tx) error {
	type group struct {
		id     int64
		chatID int64
		name   string
	}

	rows, err := tx.QueryContext(ctx, `SELECT id, chat_id, name FROM mention_groups ORDER BY id`)
	if err != nil {
		return err
	}

	var groups []group

	for rows.Next() {
		var g group
		if err := rows.Scan(&g.id, &g.chatID, &g.name); err != nil {
			_ = rows.Close()

			return err
		}

		groups = append(groups, g)
	}

	if err := rows.Err(); err != nil {
		_ = rows.Close()

		return err
	}

	_ = rows.Close()

	type key struct {
		chatID int64
		name   string
	}

	// The group with the lowest ID keeps the name; later duplicates are merged into it.
	// Merging happens before any rename so the unique (name, chat_id) index never clashes.
	keepers := make(map[key]group)

	var renames []group

	for _, g := range groups {
		normalized := strings.ToLower(strings.TrimSpace(g.name))
		k := key{g.chatID, normalized}

		keeper, exists := keepers[k]
		if !exists {
			keepers[k] = g

			if normalized != g.name {
				renames = append(renames, group{id: g.id, name: normalized})
			}

			continue
		}

		if err := mergeGroup(ctx, tx, g.id, keeper.id); err != nil {
			return err
		}
	}

	for _, r := range renames {
		if _, err := tx.ExecContext(ctx, `UPDATE mention_groups SET name = ? WHERE id = ?`, r.name, r.id); err != nil {
			return err
		}
	}

	return nil
}

// mergeGroup moves the members of src into dst and deletes src.
func mergeGroup(ctx context.Context, tx *sql.Tx, src, dst int64) error {
	return exec2(ctx, tx,
		stmt{`INSERT OR IGNORE INTO group_members (group_id, user_id) SELECT ?, user_id FROM group_members WHERE group_id = ?`, []any{dst, src}},
		stmt{`DELETE FROM group_members WHERE group_id = ?`, []any{src}},
		stmt{`DELETE FROM mention_groups WHERE id = ?`, []any{src}},
	)
}

type stmt struct {
	sql  string
	args []any
}

func exec2(ctx context.Context, tx *sql.Tx, statements ...stmt) error {
	for _, s := range statements {
		if _, err := tx.ExecContext(ctx, s.sql, s.args...); err != nil {
			return err
		}
	}

	return nil
}
