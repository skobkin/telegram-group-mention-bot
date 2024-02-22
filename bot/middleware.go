package bot

import (
	"context"
	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegohandler"
	"github.com/mymmrac/telego/telegoutil"
	"log/slog"
)

func (b *Bot) userFillMiddleware(bot *telego.Bot, update telego.Update, next telegohandler.Handler) {
	// Get initial context
	ctx := update.Context()

	if update.Message != nil && update.Message.From != nil {
		userId := update.Message.From.ID
		user, err := b.storage.FindOrCreateTelegramUser(userId)
		if err != nil {
			slog.Error("Cannot transparently get user from the storage", userId, err)

			_, err = b.api.SendMessage(telegoutil.Message(
				telegoutil.ID(update.Message.Chat.ID),
				"Cannot find or create user in the DB. Please try again or report the problem.",
			))
			if err != nil {
				slog.Error("Cannot send error message", err)

				return
			}

			return
		}

		ctx = context.WithValue(ctx, contextUserKey, user)

		_, err = b.api.SendMessage(telegoutil.Message(
			telegoutil.ID(update.Message.Chat.ID),
			"User found!",
		))
	}

	update = update.WithContext(ctx)
	next(bot, update)
}

// TODO: Chat and ChatMember middlewares
