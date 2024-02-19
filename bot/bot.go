package bot

import (
	"errors"
	"git.skobk.in/skobkin/telegram-group-mention-bot/db"
	"github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
	tu "github.com/mymmrac/telego/telegoutil"
	"log/slog"
)

var (
	ErrGetMe          = errors.New("cannot retrieve api user")
	ErrUpdatesChannel = errors.New("cannot get updates channel")
	ErrHandlerInit    = errors.New("cannot initialize handler")
)

type Bot struct {
	api *telego.Bot
	db  *db.Database
}

func NewBot(api *telego.Bot, db *db.Database) *Bot {
	return &Bot{
		api: api,
		db:  db,
	}
}

func (b *Bot) Run() error {
	botUser, err := b.api.GetMe()
	if err != nil {
		slog.Error("Cannot retrieve api user", err)

		return ErrGetMe
	}

	slog.Info("Running api as", map[string]any{
		"id":       botUser.ID,
		"username": botUser.Username,
		"name":     botUser.FirstName,
		"is_bot":   botUser.IsBot,
	})

	updates, err := b.api.UpdatesViaLongPolling(nil)
	if err != nil {
		slog.Error("Cannot get update channel", err)

		return ErrUpdatesChannel
	}

	bh, err := th.NewBotHandler(b.api, updates)
	if err != nil {
		slog.Error("Cannot initialize bot handler", err)

		return ErrHandlerInit
	}

	defer bh.Stop()
	defer b.api.StopLongPolling()

	bh.Handle(b.startHandler, th.CommandEqual("start"))
	bh.Handle(b.mentionHandler, th.CommandEqual("mention"))
	bh.Handle(b.helpHandler, th.Any())

	bh.Start()

	return nil
}

func (b *Bot) mentionHandler(bot *telego.Bot, update telego.Update) {
	slog.Info("/mention")

	// Finding or creating user
	_, err := b.db.FindOrCreateTelegramUser(update.Message.From.ID)
	if err != nil {
		slog.Error("Cannot find or create user in the DB")

		return
	}

	_, err = bot.SendMessage(tu.Messagef(
		tu.ID(update.Message.Chat.ID),
		"Hello %s!", update.Message.From.FirstName,
	))
	if err != nil {
		slog.Error("Cannot send a message", err)
	}
}

func (b *Bot) startHandler(bot *telego.Bot, update telego.Update) {
	slog.Info("/start")

	_, err := bot.SendMessage(tu.Messagef(
		tu.ID(update.Message.Chat.ID),
		"Hello %s!", update.Message.From.FirstName,
	))
	if err != nil {
		slog.Error("Cannot send a message", err)
	}
}

func (b *Bot) helpHandler(bot *telego.Bot, update telego.Update) {
	slog.Info("/help")

	_, err := bot.SendMessage(tu.Messagef(
		tu.ID(update.Message.Chat.ID),
		"Instructions:\r\n"+
			"- 1"+
			"- 2",
	))
	if err != nil {
		slog.Error("Cannot send a message", err)
	}
}
