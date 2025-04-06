package bot

import (
	"fmt"
	"log/slog"
	"strings"

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
		text := msg.Text
		// Only log full text for commands or messages with @ mentions
		if strings.HasPrefix(text, "/") {
			// Keep commands as is
		} else if strings.Contains(text, "@") {
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
		} else {
			text = "[redacted]"
		}
		details = fmt.Sprintf("from: %d, chat: %d, text: %q", msg.From.ID, msg.Chat.ID, text)
	case update.CallbackQuery != nil:
		updateType = "callback_query"
		cb := update.CallbackQuery
		details = fmt.Sprintf("from: %d, data: %q", cb.From.ID, cb.Data)
	}

	slog.Info("bot: Incoming update", "type", updateType, "update_id", update.UpdateID, "details", details)

	return ctx.Next(update)
}
