package bot

import (
	"errors"
	"git.skobk.in/skobkin/telegram-group-mention-bot/db"
	"github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
	tu "github.com/mymmrac/telego/telegoutil"
	"log/slog"
	"strings"
)

var (
	ErrGetMe          = errors.New("cannot retrieve api user")
	ErrUpdatesChannel = errors.New("cannot get updates channel")
	ErrHandlerInit    = errors.New("cannot initialize handler")
)

const contextUserKey = "user"

type Bot struct {
	api     *telego.Bot
	storage *db.Storage
}

func NewBot(api *telego.Bot, db *db.Storage) *Bot {
	return &Bot{
		api:     api,
		storage: db,
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

	bh.Use(b.userFillMiddleware)

	bh.Handle(b.startHandler, th.CommandEqual("start"))
	bh.Handle(b.createHandler, th.CommandEqual("create"))
	bh.Handle(b.deleteHandler, th.CommandEqual("delete"))
	bh.Handle(b.joinHandler, th.CommandEqual("join"))
	bh.Handle(b.leaveHandler, th.CommandEqual("leave"))
	bh.Handle(b.mentionHandler, th.CommandEqual("mention"))
	bh.Handle(b.helpHandler, th.Any())

	bh.Start()

	return nil
}

func (b *Bot) mentionHandler(bot *telego.Bot, update telego.Update) {
	slog.Info("/mention")

	_, _ = bot.SendMessage(tu.Message(
		tu.ID(update.Message.Chat.ID),
		"/mention is not implemented yet",
	))
}

func (b *Bot) createHandler(bot *telego.Bot, update telego.Update) {
	slog.Info("/create")

	args := strings.Split(update.Message.Text, " ")

	if len(args) < 3 {
		_, _ = bot.SendMessage(tu.Message(
			tu.ID(update.Message.Chat.ID),
			"Usage: /create %tag% %title%",
		))
	}

	chatId := update.Message.Chat.ID
	tag := args[1]
	title := args[2]

	_, err := b.storage.FindGroupByChatIdAndTag(chatId, tag)
	if err == nil {
		_, _ = bot.SendMessage(tu.Messagef(
			tu.ID(update.Message.Chat.ID),
			"Error: Group with tag '%s' already exists.",
			tag,
		))

		return
	}

	group, err := b.storage.CreateGroup(chatId, tag, title)
	if err != nil {
		slog.Error("Group couldn't be created in the storage")

		_, _ = bot.SendMessage(tu.Message(
			tu.ID(update.Message.Chat.ID),
			"Error: Database error. Try again later.",
		))

		return
	}

	_, err = bot.SendMessage(tu.Messagef(
		tu.ID(chatId),
		"Group with tag '%s' created",
		group.Tag,
	))
	if err != nil {
		slog.Error("Cannot send reply message", err)
	}
}

func (b *Bot) deleteHandler(bot *telego.Bot, update telego.Update) {
	slog.Info("/delete")

	_, _ = bot.SendMessage(tu.Message(
		tu.ID(update.Message.Chat.ID),
		"/delete is not implemented yet",
	))
}

func (b *Bot) joinHandler(bot *telego.Bot, update telego.Update) {
	slog.Info("/join")

	_, _ = bot.SendMessage(tu.Message(
		tu.ID(update.Message.Chat.ID),
		"/join is not implemented yet",
	))
}

func (b *Bot) leaveHandler(bot *telego.Bot, update telego.Update) {
	slog.Info("/leave")

	_, _ = bot.SendMessage(tu.Message(
		tu.ID(update.Message.Chat.ID),
		"/leave is not implemented yet",
	))
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

func (b *Bot) startHandler(bot *telego.Bot, update telego.Update) {
	slog.Info("/start")

	_, _ = bot.SendMessage(tu.Message(
		tu.ID(update.Message.Chat.ID),
		"/start is not implemented yet",
	))
}
