package config

import (
	"errors"
	"log/slog"
	"testing"
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
