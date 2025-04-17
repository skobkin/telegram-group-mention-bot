package main

import (
	"flag"
	"log/slog"
	"os"
	"strings"

	"telegram-group-mention-bot/bot"
	"telegram-group-mention-bot/storage"

	"github.com/joho/godotenv"
)

func main() {
	// Parse command-line flags
	verbose := flag.Bool("v", false, "Enable verbose logging (LevelInfo)")
	veryVerbose := flag.Bool("vv", false, "Enable very verbose logging (LevelDebug)")
	flag.Parse()

	// Set up logging
	setLogLevel(*verbose, *veryVerbose)

	slog.Debug("main: Command-line flags parsed", "verbose", *verbose, "very_verbose", *veryVerbose)

	// Load environment variables
	if err := godotenv.Load(); err != nil {
		slog.Warn("main: Failed to load .env file", "error", err)
	} else {
		slog.Debug("main: Environment variables loaded from .env file", "env", os.Environ())
	}

	// Get configuration from environment
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		slog.Error("main: TELEGRAM_BOT_TOKEN environment variable is required")
		os.Exit(1)
	}

	dbPath := os.Getenv("DATABASE_PATH")
	if dbPath == "" {
		dbPath = "data.sqlite"
		slog.Debug("main: Using default database path", "path", dbPath)
	} else {
		slog.Debug("main: Using custom database path", "path", dbPath)
	}

	// Initialize storage
	slog.Debug("main: Initializing storage", "db_path", dbPath)
	storage, err := storage.New(dbPath)
	if err != nil {
		slog.Error("main: Failed to initialize storage", "error", err)
		os.Exit(1)
	}
	slog.Debug("main: Storage initialized successfully")

	// Initialize bot
	slog.Debug("main: Initializing bot")
	bot, err := bot.New(token, storage)
	if err != nil {
		slog.Error("main: Failed to initialize bot", "error", err)
		os.Exit(1)
	}
	slog.Debug("main: Bot initialized successfully")

	// Start bot
	slog.Info("main: Starting bot...")
	if err := bot.Start(); err != nil {
		slog.Error("main: Failed to start bot", "error", err)
		os.Exit(1)
	}
	slog.Info("main: Bot started successfully")

	// Wait for interrupt signal
	slog.Debug("main: Bot is running, waiting for interrupt signal")
	select {}
}

// setLogLevel configures the logging level based on the provided flags and environment variable
func setLogLevel(verbose, veryVerbose bool) {
	// Determine logging level based on flags and environment variable
	logLevel := slog.LevelWarn // Default level

	// Check environment variable first
	if envLevel := os.Getenv("LOG_LEVEL"); envLevel != "" {
		switch strings.ToLower(envLevel) {
		case "debug":
			logLevel = slog.LevelDebug
		case "info":
			logLevel = slog.LevelInfo
		case "warn":
			logLevel = slog.LevelWarn
		case "error":
			logLevel = slog.LevelError
		default:
			slog.Warn("main: Invalid LOG_LEVEL value", "value", envLevel)
		}
	}

	// Command-line flags take precedence over environment variable
	if veryVerbose {
		logLevel = slog.LevelDebug
	} else if verbose {
		logLevel = slog.LevelInfo
	}

	// Configure structured logging with JSON output
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	slog.Debug("main: Log level set to", "level", logLevel.String())
}
