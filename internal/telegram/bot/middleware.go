package bot

import (
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"telegram-group-mention-bot/internal/storage"

	t "github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
)

// recoverFromPanic logs a recovered handler panic with its stack and lets the update loop continue.
func recoverFromPanic(recovered any) error {
	slog.Error("bot:middleware: Recovered from panic in handler", "panic", fmt.Sprint(recovered), "stack", string(debug.Stack()))

	return nil
}

// logUpdate is a middleware that logs all incoming updates.
// It never logs message text or user names, only identifiers and sizes.
func (b *Bot) logUpdate(ctx *th.Context, update t.Update) error {
	attrs := []any{"update_id", update.UpdateID}

	switch {
	case update.Message != nil:
		msg := update.Message
		attrs = append(attrs, "type", "message", "chat_type", msg.Chat.Type, "chat_id", msg.Chat.ID, "text_length", len(msg.Text))
		if msg.From != nil {
			attrs = append(attrs, "from_id", msg.From.ID)
		}
	case update.CallbackQuery != nil:
		attrs = append(attrs, "type", "callback_query", "from_id", update.CallbackQuery.From.ID)
	default:
		attrs = append(attrs, "type", "other")
	}

	slog.Info("bot:middleware: Incoming update", attrs...)

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

	slog.Debug("bot:middleware: Updating user data", "user_id", from.ID, "chat_id", msg.Chat.ID, "chat_type", msg.Chat.Type)

	// Update user data in the database
	_, err := b.storage.CreateOrUpdateUser(ctx,
		from.ID,
		from.Username,
		from.FirstName,
		from.LastName,
	)
	if err != nil {
		slog.Error("bot:middleware: Failed to update user data",
			"user_id", from.ID,
			"error", err)
	}

	return ctx.Next(update)
}

// chatMigration extracts the old and new chat IDs from a migration service message.
// Telegram sends two separate service messages when a group is upgraded to a supergroup:
// migrate_to_chat_id in the old chat and migrate_from_chat_id in the new one.
func chatMigration(msg *t.Message) (from, to int64, ok bool) {
	switch {
	case msg.MigrateToChatID != 0:
		return msg.Chat.ID, msg.MigrateToChatID, true
	case msg.MigrateFromChatID != 0:
		return msg.MigrateFromChatID, msg.Chat.ID, true
	default:
		return 0, 0, false
	}
}

// migrateChat moves mention groups to the new chat ID when a chat is upgraded to a supergroup.
// Whichever service message arrives first does the work; the second one is a no-op.
func (b *Bot) migrateChat(ctx *th.Context, update t.Update) error {
	if update.Message == nil {
		return ctx.Next(update)
	}

	from, to, ok := chatMigration(update.Message)
	if !ok {
		return ctx.Next(update)
	}

	slog.Info("bot:middleware: Chat migration detected", "from_chat_id", from, "to_chat_id", to)

	if err := b.storage.MigrateChatGroups(ctx, from, to); err != nil {
		slog.Error("bot:middleware: Failed to migrate chat groups", "error", err, "from_chat_id", from, "to_chat_id", to)

		return err
	}

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
	allGroup, err := b.storage.GetGroup(ctx, groupNameAll, msg.Chat.ID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			slog.Debug("bot:middleware: 'all' group not found", "chat_id", msg.Chat.ID)

			return ctx.Next(update)
		}
		// Some other error occurred
		slog.Error("bot:middleware: Failed to check for 'all' group", "error", err, "chat_id", msg.Chat.ID)

		return ctx.Next(update)
	}

	// Idempotent insert; the user row was created by syncUserData
	added, err := b.storage.AddMember(ctx, allGroup.ID, &storage.User{ID: from.ID})
	if err != nil {
		slog.Error("bot:middleware: Failed to add user to 'all' group", "error", err, "group_id", allGroup.ID, "user_id", from.ID)
	} else if added {
		slog.Info("bot:middleware: Added user to 'all' group", "group_id", allGroup.ID, "user_id", from.ID)
	}

	return ctx.Next(update)
}
