// Package config reads application settings from the environment.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
)

const (
	defaultDatabasePath         = "data.sqlite"
	defaultGroupCooldownSeconds = 30
	defaultUserCooldownSeconds  = 10
)

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
	// MentionGroupCooldown is the minimum time between two mentions of the same group in a chat (0 disables).
	MentionGroupCooldown time.Duration
	// MentionUserCooldown is the minimum time between two mentions by the same user in a chat (0 disables).
	MentionUserCooldown time.Duration
	// MentionMaxMembers caps how many members one mention post pings (0 means unlimited).
	MentionMaxMembers int
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

	groupSeconds, err := parseNonNegativeInt(getenv, "MENTION_GROUP_COOLDOWN_SECONDS", defaultGroupCooldownSeconds)
	if err != nil {
		return Config{}, err
	}

	userSeconds, err := parseNonNegativeInt(getenv, "MENTION_USER_COOLDOWN_SECONDS", defaultUserCooldownSeconds)
	if err != nil {
		return Config{}, err
	}

	cfg.MentionGroupCooldown = time.Duration(groupSeconds) * time.Second
	cfg.MentionUserCooldown = time.Duration(userSeconds) * time.Second

	cfg.MentionMaxMembers, err = parseNonNegativeInt(getenv, "MENTION_MAX_MEMBERS", 0)
	if err != nil {
		return Config{}, err
	}

	return cfg, nil
}

// parseNonNegativeInt reads an integer >= 0 from the environment, using def when unset.
func parseNonNegativeInt(getenv func(string) string, name string, def int) (int, error) {
	raw := strings.TrimSpace(getenv(name))
	if raw == "" {
		return def, nil
	}

	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, fmt.Errorf("invalid %s: %q is not a non-negative integer", name, raw)
	}

	return value, nil
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
