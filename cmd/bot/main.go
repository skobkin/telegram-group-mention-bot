package main

import (
	"flag"
	"log/slog"
	"os"

	"telegram-group-mention-bot/internal/buildinfo"
	"telegram-group-mention-bot/internal/config"
	"telegram-group-mention-bot/internal/logging"
	"telegram-group-mention-bot/internal/storage"
	"telegram-group-mention-bot/internal/support/secretscrub"
	"telegram-group-mention-bot/internal/telegram/bot"

	"github.com/joho/godotenv"
)

func main() {
	verbose := flag.Bool("v", false, "Enable verbose logging (LevelInfo)")
	veryVerbose := flag.Bool("vv", false, "Enable very verbose logging (LevelDebug)")
	flag.Parse()

	// .env is optional; real environment variables take precedence over it.
	envErr := godotenv.Load()

	// The scrubber is built before any logging so the token can never be logged.
	scrubber := secretscrub.New(secretscrub.TelegramTokenSecrets(os.Getenv("TELEGRAM_BOT_TOKEN")))
	setupLogging(scrubber, *verbose, *veryVerbose)

	if envErr != nil {
		slog.Warn("main: Failed to load .env file", "error", envErr)
	}

	cfg, err := config.Load(os.Getenv)
	if err != nil {
		slog.Error("main: Invalid configuration", "error", err)
		os.Exit(1)
	}

	slog.Info("main: Starting", "app", buildinfo.AppName, "version", buildinfo.Version)

	store, err := storage.New(cfg.DatabasePath)
	if err != nil {
		slog.Error("main: Failed to initialize storage", "error", err)
		os.Exit(1)
	}

	b, err := bot.New(cfg.TelegramToken, store)
	if err != nil {
		slog.Error("main: Failed to initialize bot", "error", err)
		os.Exit(1)
	}

	if err := b.Start(); err != nil {
		slog.Error("main: Failed to start bot", "error", err)
		os.Exit(1)
	}

	slog.Info("main: Bot started successfully")

	select {}
}

// setupLogging configures JSON logging. Level precedence: -vv, -v, LOG_LEVEL, default warn.
func setupLogging(scrubber *secretscrub.Scrubber, verbose, veryVerbose bool) {
	level := slog.LevelWarn

	if raw := os.Getenv("LOG_LEVEL"); raw != "" {
		if parsed, err := config.ParseLogLevel(raw); err == nil {
			level = parsed
		} else {
			defer slog.Warn("main: Ignoring LOG_LEVEL", "error", err)
		}
	}

	switch {
	case veryVerbose:
		level = slog.LevelDebug
	case verbose:
		level = slog.LevelInfo
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(logging.NewScrubHandler(scrubber, handler)))
}
