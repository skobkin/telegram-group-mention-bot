package main

import (
	"git.skobk.in/skobkin/telegram-group-mention-bot/bot"
	"git.skobk.in/skobkin/telegram-group-mention-bot/db"
	"github.com/mymmrac/telego"
	"log/slog"
	"os"
)

func main() {
	botToken := os.Getenv("TELEGRAM_TOKEN")

	api, err := telego.NewBot(botToken, telego.WithDefaultDebugLogger())
	if err != nil {
		slog.Error("Failed to initialize Telegram API client", err)
		os.Exit(1)
	}

	database, err := db.NewDatabase("./data.sqlite")
	if err != nil {
		slog.Error("Cannot open the database", err)

		os.Exit(1)
	}

	err = database.Migrate()
	if err != nil {
		slog.Error("Cannot migrate the database", err)

		os.Exit(1)
	}

	botService := bot.NewBot(api, database)

	err = botService.Run()
	if err != nil {
		slog.Error("Running bot finished with an error", err)

		os.Exit(1)
	}

	slog.Info("Exiting")
}
