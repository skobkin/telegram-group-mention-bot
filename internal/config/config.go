// Package config reads application settings from the environment.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
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
	// OwnerIDs are the Telegram user IDs allowed to use owner tools (/stats). Empty disables them.
	OwnerIDs []int64
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

	owners, err := parseIDList("BOT_OWNER_IDS", getenv("BOT_OWNER_IDS"))
	if err != nil {
		return Config{}, err
	}

	cfg.OwnerIDs = owners

	return cfg, nil
}

// parseIDList parses a comma-separated list of Telegram user IDs; empty input gives no IDs.
func parseIDList(name, raw string) ([]int64, error) {
	var ids []int64

	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		id, err := strconv.ParseInt(part, 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("invalid %s: %q is not a positive user ID", name, part)
		}

		ids = append(ids, id)
	}

	return ids, nil
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
