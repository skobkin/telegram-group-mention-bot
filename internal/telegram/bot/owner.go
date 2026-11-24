package bot

import (
	"context"
	"fmt"
	"time"

	"telegram-group-mention-bot/internal/buildinfo"

	t "github.com/mymmrac/telego"
)

// isOwner reports whether the user is one of the configured bot owners.
func (b *Bot) isOwner(userID int64) bool {
	_, ok := b.owners[userID]

	return ok
}

// handleStats shows usage counters. It only answers owners in a private chat;
// everyone else gets no response at all.
func (b *Bot) handleStats(ctx context.Context, message t.Message) error {
	if message.Chat.Type != t.ChatTypePrivate || message.From == nil || !b.isOwner(message.From.ID) {
		return nil
	}

	stats, err := b.storage.Stats(ctx)
	if err != nil {
		logError("bot: Failed to collect stats", err)
		b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2("Failed to collect statistics."), &message)

		return nil
	}

	text := fmt.Sprintf("Version: %s\nUptime: %s\nChats with groups: %d\nGroups: %d\nUsers: %d\nMemberships: %d\nDatabase size: %s",
		buildinfo.Version,
		time.Since(b.startedAt).Round(time.Second),
		stats.Chats, stats.Groups, stats.Users, stats.Memberships,
		formatBytes(stats.DBSizeBytes),
	)

	b.sendMessage(ctx, message.Chat.ID, escapeMarkdownV2(text), &message)

	return nil
}

func formatBytes(n int64) string {
	const unit = 1024

	if n < unit {
		return fmt.Sprintf("%d B", n)
	}

	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}

	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGT"[exp])
}
