package storage

import (
	"context"
	"fmt"
	"os"
)

// Stats are usage counters for the bot operator.
type Stats struct {
	Chats       int64 // chats that have at least one group
	Groups      int64
	Users       int64
	Memberships int64
	DBSizeBytes int64 // size of the database file, 0 if it cannot be determined
}

// Stats returns read-only usage counters.
func (s *Storage) Stats(ctx context.Context) (Stats, error) {
	var stats Stats

	queries := []struct {
		dst *int64
		sql string
	}{
		{&stats.Chats, "SELECT COUNT(DISTINCT chat_id) FROM mention_groups"},
		{&stats.Groups, "SELECT COUNT(*) FROM mention_groups"},
		{&stats.Users, "SELECT COUNT(*) FROM users"},
		{&stats.Memberships, "SELECT COUNT(*) FROM group_members"},
	}

	for _, q := range queries {
		if err := s.db.WithContext(ctx).Raw(q.sql).Scan(q.dst).Error; err != nil {
			return Stats{}, fmt.Errorf("%w stats: %w", ErrGet, err)
		}
	}

	if info, err := os.Stat(s.path); err == nil {
		stats.DBSizeBytes = info.Size()
	}

	return stats, nil
}
