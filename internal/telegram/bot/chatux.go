package bot

import (
	"context"
	"log/slog"
	"slices"
	"time"

	t "github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

const (
	ackReaction     = "👍"
	deleteTimeout   = 10 * time.Second
	menuSyncTimeout = 15 * time.Second
)

// react acknowledges a message with a reaction. It reports false when that did not work
// (reactions disabled in the chat, message too old, ...), so the caller can reply with text.
func (b *Bot) react(ctx context.Context, message *t.Message) bool {
	err := b.api.SetMessageReaction(ctx, &t.SetMessageReactionParams{
		ChatID:    tu.ID(message.Chat.ID),
		MessageID: message.MessageID,
		Reaction:  []t.ReactionType{tu.ReactionEmoji(ackReaction)},
	})
	if err != nil {
		slog.Debug("bot: Reaction failed, falling back to a text reply", "error", err, "chat_id", message.Chat.ID)

		return false
	}

	return true
}

// respond answers a command with its outcome: successful changes are acknowledged with a
// reaction on the command (text reply as a fallback); everything else is a text reply.
func (b *Bot) respond(ctx context.Context, message *t.Message, out outcome) {
	if out.ok && b.react(ctx, message) {
		return
	}

	b.sendMessage(ctx, message.Chat.ID, out.render(), message)
}

// scheduleDelete removes messages after the configured AUTO_DELETE_SECONDS. Failures (the bot
// lacks delete rights, the message is gone) are logged at debug level and ignored. Pending
// deletions are lost when the bot restarts.
func (b *Bot) scheduleDelete(chatID int64, messageIDs ...int) {
	if b.autoDelete <= 0 {
		return
	}

	run := func() {
		ctx, cancel := context.WithTimeout(context.Background(), deleteTimeout)
		defer cancel()

		for _, id := range slices.Compact(slices.Sorted(slices.Values(messageIDs))) {
			err := b.api.DeleteMessage(ctx, &t.DeleteMessageParams{ChatID: tu.ID(chatID), MessageID: id})
			if err != nil {
				slog.Debug("bot: Auto-delete failed", "error", err, "chat_id", chatID, "message_id", id)
			}
		}
	}

	if b.afterFunc != nil {
		b.afterFunc(b.autoDelete, run)

		return
	}

	time.AfterFunc(b.autoDelete, run)
}

// menuCommands builds the Telegram command menu from the command table.
func menuCommands(commands []command) []t.BotCommand {
	var menu []t.BotCommand

	for _, cmd := range commands {
		if cmd.hidden {
			continue
		}

		names := cmd.menu
		if len(names) == 0 {
			names = cmd.names[:1]
		}

		for _, name := range names {
			menu = append(menu, t.BotCommand{Command: name, Description: truncate(cmd.help, 256)})
		}
	}

	return menu
}

func truncate(s string, limit int) string {
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}

	return string(runes[:limit-1]) + "…"
}

// syncCommandMenu registers the commands for private and group chats, only when the list
// Telegram has differs from the table. Failures are logged and never stop the bot.
func (b *Bot) syncCommandMenu(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, menuSyncTimeout)
	defer cancel()

	want := menuCommands(b.commands())

	scopes := []struct {
		name  string
		scope t.BotCommandScope
	}{
		{"private", tu.ScopeAllPrivateChats()},
		{"groups", tu.ScopeAllGroupChats()},
	}

	for _, s := range scopes {
		have, err := b.api.GetMyCommands(ctx, &t.GetMyCommandsParams{Scope: s.scope})
		if err == nil && slices.Equal(have, want) {
			slog.Debug("bot: Command menu is up to date", "scope", s.name)

			continue
		}

		if err := b.api.SetMyCommands(ctx, &t.SetMyCommandsParams{Commands: want, Scope: s.scope}); err != nil {
			slog.Warn("bot: Failed to register the command menu", "error", err, "scope", s.name)

			continue
		}

		slog.Info("bot: Command menu registered", "scope", s.name, "commands", len(want))
	}
}
