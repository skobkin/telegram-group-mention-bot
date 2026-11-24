package config

import (
	"errors"
	"log/slog"
	"testing"
	"time"
)

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestLoadRequiresToken(t *testing.T) {
	if _, err := Load(env(nil)); !errors.Is(err, ErrTokenRequired) {
		t.Fatalf("expected ErrTokenRequired, got %v", err)
	}
}

func TestLoadDefaultsAndOverrides(t *testing.T) {
	cfg, err := Load(env(map[string]string{"TELEGRAM_BOT_TOKEN": "t"}))
	if err != nil || cfg.DatabasePath != "data.sqlite" {
		t.Fatalf("unexpected defaults: %+v, %v", cfg, err)
	}

	cfg, err = Load(env(map[string]string{"TELEGRAM_BOT_TOKEN": "t", "DATABASE_PATH": "/d.sqlite"}))
	if err != nil || cfg.DatabasePath != "/d.sqlite" {
		t.Fatalf("unexpected config: %+v, %v", cfg, err)
	}
}

func TestParseLogLevel(t *testing.T) {
	if level, err := ParseLogLevel(" DEBUG "); err != nil || level != slog.LevelDebug {
		t.Fatalf("unexpected: %v, %v", level, err)
	}

	if _, err := ParseLogLevel("loud"); err == nil {
		t.Fatal("expected error for invalid level")
	}
}

func TestLoadOwnerIDs(t *testing.T) {
	cfg, err := Load(env(map[string]string{"TELEGRAM_BOT_TOKEN": "t", "BOT_OWNER_IDS": " 12, 34 ,"}))
	if err != nil || len(cfg.OwnerIDs) != 2 || cfg.OwnerIDs[0] != 12 || cfg.OwnerIDs[1] != 34 {
		t.Fatalf("unexpected owners: %+v, %v", cfg.OwnerIDs, err)
	}

	if cfg, _ := Load(env(map[string]string{"TELEGRAM_BOT_TOKEN": "t"})); len(cfg.OwnerIDs) != 0 {
		t.Fatalf("owner tools must be disabled by default, got %v", cfg.OwnerIDs)
	}

	for _, bad := range []string{"abc", "-5", "0", "1,x"} {
		if _, err := Load(env(map[string]string{"TELEGRAM_BOT_TOKEN": "t", "BOT_OWNER_IDS": bad})); err == nil {
			t.Fatalf("expected error for BOT_OWNER_IDS=%q", bad)
		}
	}
}

func TestLoadMentionLimits(t *testing.T) {
	cfg, err := Load(env(map[string]string{"TELEGRAM_BOT_TOKEN": "t"}))
	if err != nil || cfg.MentionGroupCooldown != 30*time.Second || cfg.MentionUserCooldown != 10*time.Second || cfg.MentionMaxMembers != 0 {
		t.Fatalf("unexpected defaults: %+v, %v", cfg, err)
	}

	cfg, err = Load(env(map[string]string{
		"TELEGRAM_BOT_TOKEN":             "t",
		"MENTION_GROUP_COOLDOWN_SECONDS": "0",
		"MENTION_USER_COOLDOWN_SECONDS":  "5",
		"MENTION_MAX_MEMBERS":            "25",
	}))
	if err != nil || cfg.MentionGroupCooldown != 0 || cfg.MentionUserCooldown != 5*time.Second || cfg.MentionMaxMembers != 25 {
		t.Fatalf("unexpected overrides: %+v, %v", cfg, err)
	}

	for _, name := range []string{"MENTION_GROUP_COOLDOWN_SECONDS", "MENTION_USER_COOLDOWN_SECONDS", "MENTION_MAX_MEMBERS", "AUTO_DELETE_SECONDS"} {
		for _, bad := range []string{"-1", "x", "1.5"} {
			if _, err := Load(env(map[string]string{"TELEGRAM_BOT_TOKEN": "t", name: bad})); err == nil {
				t.Fatalf("expected error for %s=%q", name, bad)
			}
		}
	}
}

func TestLoadAutoDelete(t *testing.T) {
	cfg, _ := Load(env(map[string]string{"TELEGRAM_BOT_TOKEN": "t"}))
	if cfg.AutoDelete != 0 {
		t.Fatalf("auto-delete must be off by default, got %v", cfg.AutoDelete)
	}

	cfg, err := Load(env(map[string]string{"TELEGRAM_BOT_TOKEN": "t", "AUTO_DELETE_SECONDS": "60"}))
	if err != nil || cfg.AutoDelete != time.Minute {
		t.Fatalf("unexpected: %v, %v", cfg.AutoDelete, err)
	}
}

func TestLoadSentry(t *testing.T) {
	cfg, _ := Load(env(map[string]string{"TELEGRAM_BOT_TOKEN": "t"}))
	if cfg.SentryDSN != "" || cfg.SentryEnvironment != "" {
		t.Fatalf("sentry must be disabled by default: %+v", cfg)
	}

	cfg, _ = Load(env(map[string]string{"TELEGRAM_BOT_TOKEN": "t", "SENTRY_DSN": " https://k@s.example/1 ", "SENTRY_ENVIRONMENT": "prod"}))
	if cfg.SentryDSN != "https://k@s.example/1" || cfg.SentryEnvironment != "prod" {
		t.Fatalf("unexpected: %+v", cfg)
	}
}
