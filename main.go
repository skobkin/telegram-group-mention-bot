package main

import (
	"log/slog"
	"os"

	"telegram-group-mention-bot/bot"
	"telegram-group-mention-bot/storage"

	"github.com/joho/godotenv"
)

func main() {
	// Configure structured logging
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Load environment variables
	if err := godotenv.Load(); err != nil {
		slog.Warn("Failed to load .env file", "error", err)
	}

	// Get configuration from environment
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		slog.Error("TELEGRAM_BOT_TOKEN environment variable is required")
		os.Exit(1)
	}

	dbPath := os.Getenv("DATABASE_PATH")
	if dbPath == "" {
		dbPath = "data.sqlite"
	}

	// Initialize storage
	storage, err := storage.New(dbPath)
	if err != nil {
		slog.Error("Failed to initialize storage", "error", err)
		os.Exit(1)
	}

	// Initialize bot
	bot, err := bot.New(token, storage)
	if err != nil {
		slog.Error("Failed to initialize bot", "error", err)
		os.Exit(1)
	}

	// Start bot
	slog.Info("Starting bot...")
	if err := bot.Start(); err != nil {
		slog.Error("Failed to start bot", "error", err)
		os.Exit(1)
	}

	// Wait for interrupt signal
	select {}
}
