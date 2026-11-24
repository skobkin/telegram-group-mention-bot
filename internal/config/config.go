// Package config reads application settings from the environment.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

const defaultDatabasePath = "data.sqlite"

// ErrTokenRequired is returned when TELEGRAM_BOT_TOKEN is not set.
var ErrTokenRequired = errors.New("TELEGRAM_BOT_TOKEN environment variable is required")

// Config is the application configuration.
type Config struct {
	// TelegramToken is the bot token from @BotFather.
	TelegramToken string
	// DatabasePath is the SQLite database file path.
	DatabasePath string
}

// Load builds the configuration using getenv (typically os.Getenv).
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		TelegramToken: getenv("TELEGRAM_BOT_TOKEN"),
		DatabasePath:  getenv("DATABASE_PATH"),
	}

	if cfg.TelegramToken == "" {
		return Config{}, ErrTokenRequired
	}

	if cfg.DatabasePath == "" {
		cfg.DatabasePath = defaultDatabasePath
	}

	return cfg, nil
}

// ParseLogLevel converts debug/info/warn/error (case-insensitive) to a slog level.
func ParseLogLevel(raw string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("invalid LOG_LEVEL %q (want debug, info, warn or error)", raw)
	}
}
