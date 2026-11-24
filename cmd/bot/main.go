package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"telegram-group-mention-bot/internal/buildinfo"
	"telegram-group-mention-bot/internal/config"
	"telegram-group-mention-bot/internal/logging"
	"telegram-group-mention-bot/internal/storage"
	"telegram-group-mention-bot/internal/support/errreport"
	"telegram-group-mention-bot/internal/support/secretscrub"
	"telegram-group-mention-bot/internal/telegram/bot"

	"github.com/joho/godotenv"
)

func main() {
	os.Exit(run())
}

func run() int {
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

		return 1
	}

	flushReports, err := errreport.Init(errreport.Config{
		DSN:         cfg.SentryDSN,
		Environment: cfg.SentryEnvironment,
		Release:     buildinfo.Version,
	}, scrubber)
	if err != nil {
		slog.Error("main: Sentry initialisation failed, continuing without it", "error", err)
	}

	defer flushReports()

	if cfg.SentryDSN != "" && err == nil {
		slog.Info("main: Sentry reporting enabled")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	slog.Info("main: Starting", "app", buildinfo.AppName, "version", buildinfo.Version)

	store, err := storage.New(cfg.DatabasePath)
	if err != nil {
		slog.Error("main: Failed to initialize storage", "error", err)

		return 1
	}

	b, err := bot.New(cfg.TelegramToken, store, bot.Options{
		OwnerIDs:          cfg.OwnerIDs,
		AutoDelete:        cfg.AutoDelete,
		GroupCooldown:     cfg.MentionGroupCooldown,
		UserCooldown:      cfg.MentionUserCooldown,
		MaxMentionMembers: cfg.MentionMaxMembers,
	})
	if err != nil {
		slog.Error("main: Failed to initialize bot", "error", err)

		return 1
	}

	err = b.Run(ctx)
	if closeErr := store.Close(); closeErr != nil {
		slog.Error("main: Failed to close storage", "error", closeErr)
	}

	if err != nil {
		slog.Error("main: Bot stopped with error", "error", err)

		return 1
	}

	slog.Info("main: Shutdown complete")

	return 0
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
