package bot

import (
	"errors"
	"github.com/mymmrac/telego"
	"log/slog"
)

var (
	ErrGetMe          = errors.New("Cannot retrieve bot user")
	ErrUpdatesChannel = errors.New("Cannot get updates channel")
)

type Bot struct {
	bot *telego.Bot
}

func NewBot(bot *telego.Bot) *Bot {
	return &Bot{
		bot: bot,
	}
}

func (b *Bot) Run() error {
	botUser, err := b.bot.GetMe()
	if err != nil {
		slog.Error("Cannot retrieve bot user", err)

		return ErrGetMe
	}

	slog.Info("Running bot as", botUser)

	updates, err := b.bot.UpdatesViaLongPolling(nil)
	if err != nil {
		slog.Error("Cannot get update channel", err)

		return ErrUpdatesChannel
	}

	for update := range updates {
		b.handleUpdate(update)
	}

	return nil
}

func (b *Bot) handleUpdate(update telego.Update) {

}
