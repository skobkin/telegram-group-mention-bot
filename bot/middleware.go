package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"telegram-group-mention-bot/storage"

	t "github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
)

// logUpdate is a middleware that logs all incoming updates
func (b *Bot) logUpdate(ctx *th.Context, update t.Update) error {
	updateType := "unknown"
	var details string
	var text string

	switch {
	case update.Message != nil:
		updateType = "message"
		msg := update.Message
		text = msg.Text

		// Determine if we should mask the text based on log level
		shouldMask := !slog.Default().Enabled(context.Background(), slog.LevelDebug)

		// Only log full text for commands or messages with @ mentions
		if strings.HasPrefix(text, "/") {
			// Keep commands as is
		} else if strings.Contains(text, "@") && shouldMask {
			// For messages with @ mentions, keep @ and the word after it, mask the rest
			words := strings.Fields(text)
			for i, word := range words {
				if strings.HasPrefix(word, "@") {
					// Keep this word as is
					words[i] = word
				} else {
					// Mask other words
					words[i] = strings.Repeat("*", len(word))
				}
			}
			text = strings.Join(words, " ")
		} else if shouldMask {
			text = "[redacted]"
		}

		details = fmt.Sprintf("chat_type: %s, from: %d, chat: %d, text: %q", msg.Chat.Type, msg.From.ID, msg.Chat.ID, text)
	case update.CallbackQuery != nil:
		updateType = "callback_query"
		cb := update.CallbackQuery
		details = fmt.Sprintf("from: %d, data: %q", cb.From.ID, cb.Data)
	}

	slog.Info("bot:middleware: Incoming update", "type", updateType, "update_id", update.UpdateID, "details", details)
	return ctx.Next(update)
}

// syncUserData is a middleware that updates user data when a new message arrives in a group or supergroup chat
func (b *Bot) syncUserData(ctx *th.Context, update t.Update) error {
	if update.Message == nil || update.Message.From == nil {
		return ctx.Next(update)
	}

	msg := update.Message
	from := msg.From

	// Only process messages from group or supergroup chats
	if msg.Chat.Type != t.ChatTypeGroup && msg.Chat.Type != t.ChatTypeSupergroup {
		return ctx.Next(update)
	}

	slog.Debug("bot:middleware: Updating user data", "user_id", from.ID, "username", from.Username, "first_name", from.FirstName, "last_name", from.LastName, "chat_id", msg.Chat.ID, "chat_type", msg.Chat.Type)

	// Update user data in the database
	_, err := b.storage.CreateOrUpdateUser(
		from.ID,
		from.Username,
		from.FirstName,
		from.LastName,
	)
	if err != nil {
		slog.Error("bot:middleware: Failed to update user data",
			"user_id", from.ID,
			"username", from.Username,
			"error", err)
	}

	return ctx.Next(update)
}

func (b *Bot) migrateChat(ctx *th.Context, update t.Update) error {
	if update.Message == nil {
		return ctx.Next(update)
	}

	msg := update.Message
	if msg.MigrateToChatID == 0 || msg.MigrateFromChatID == 0 {
		return ctx.Next(update)
	}

	slog.Info("bot:middleware: Chat migration detected", "from_chat_id", msg.MigrateFromChatID, "to_chat_id", msg.MigrateToChatID)

	err := b.storage.MigrateChatGroups(msg.MigrateFromChatID, msg.MigrateToChatID)
	if err != nil {
		slog.Error("bot:middleware: Failed to migrate chat groups", "error", err, "from_chat_id", msg.MigrateFromChatID, "to_chat_id", msg.MigrateToChatID)
		return err
	}

	slog.Info("bot:middleware: Chat groups migrated", "from_chat_id", msg.MigrateFromChatID, "to_chat_id", msg.MigrateToChatID)

	return ctx.Next(update)
}

// addToAllGroup is a middleware that adds users to the "all" group if it exists in the chat
func (b *Bot) addToAllGroup(ctx *th.Context, update t.Update) error {
	if update.Message == nil || update.Message.From == nil {
		return ctx.Next(update)
	}

	msg := update.Message
	from := msg.From

	// Only process messages from group or supergroup chats
	if msg.Chat.Type != t.ChatTypeGroup && msg.Chat.Type != t.ChatTypeSupergroup {
		return ctx.Next(update)
	}

	slog.Debug("bot:middleware: Checking for 'all' group", "chat_id", msg.Chat.ID, "user_id", from.ID)

	// Check if "all" group exists in this chat
	allGroup, err := b.storage.GetGroup("all", msg.Chat.ID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			slog.Debug("bot:middleware: 'all' group not found", "chat_id", msg.Chat.ID)
			return ctx.Next(update)
		}
		// Some other error occurred
		slog.Error("bot:middleware: Failed to check for 'all' group", "error", err, "chat_id", msg.Chat.ID)
		return ctx.Next(update)
	}

	// Check if user is already a member of the "all" group
	isMember, err := b.storage.IsMember(allGroup.ID, from.ID)
	if err != nil {
		slog.Error("bot:middleware: Failed to check membership in 'all' group", "error", err, "group_id", allGroup.ID, "user_id", from.ID)
		return ctx.Next(update)
	}

	if isMember {
		slog.Debug("bot:middleware: User already a member of 'all' group", "group_id", allGroup.ID, "user_id", from.ID)
		return ctx.Next(update)
	}

	slog.Debug("bot:middleware: Adding user to 'all' group", "group_id", allGroup.ID, "user_id", from.ID)

	// Create a minimal User object with just the ID field
	newMinimalMember := &storage.User{
		ID: from.ID,
	}

	// Add user to the "all" group
	err = b.storage.AddMember(allGroup.ID, newMinimalMember)
	if err != nil {
		slog.Error("bot:middleware: Failed to add user to 'all' group", "error", err, "group_id", allGroup.ID, "user_id", from.ID)
	} else {
		slog.Info("bot:middleware: Added user to 'all' group", "group_id", allGroup.ID, "user_id", from.ID)
	}

	return ctx.Next(update)
}
