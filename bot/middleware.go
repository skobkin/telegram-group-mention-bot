package bot

import (
	"fmt"
	"log/slog"

	t "github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
)

// logUpdate is a middleware that logs all incoming updates
func (b *Bot) logUpdate(ctx *th.Context, update t.Update) error {
	updateType := "unknown"
	var details string

	switch {
	case update.Message != nil:
		updateType = "message"
		msg := update.Message
		details = fmt.Sprintf("from: %d, chat: %d, text: %q",
			msg.From.ID, msg.Chat.ID, msg.Text)
	case update.EditedMessage != nil:
		updateType = "edited_message"
		msg := update.EditedMessage
		details = fmt.Sprintf("from: %d, chat: %d, text: %q",
			msg.From.ID, msg.Chat.ID, msg.Text)
	case update.CallbackQuery != nil:
		updateType = "callback_query"
		cb := update.CallbackQuery
		details = fmt.Sprintf("from: %d, data: %q", cb.From.ID, cb.Data)
	}

	slog.Info("bot: Incoming update", "type", updateType, "update_id", update.UpdateID, "details", details)

	return ctx.Next(update)
}
