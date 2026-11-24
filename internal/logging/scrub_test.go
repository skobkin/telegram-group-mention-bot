package logging

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"telegram-group-mention-bot/internal/support/secretscrub"
)

const token = "123456789:AAH-secretSecretSecret_123456789012"

func TestScrubHandlerNeverEmitsToken(t *testing.T) {
	var buf bytes.Buffer

	scrubber := secretscrub.New(secretscrub.TelegramTokenSecrets(token))
	logger := slog.New(NewScrubHandler(scrubber, slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))

	logger.With("pre", "has "+token).WithGroup("g").Error("failed "+token,
		"string", "https://api/bot"+token+"/getMe",
		"err", errors.New("Post: "+token),
		"group", slog.Group("n", slog.String("k", token)),
		"any", []string{token},
		token, "key",
	)

	out := buf.String()
	if strings.Contains(out, "AAH-secret") || strings.Contains(out, "123456789:AAH") {
		t.Fatalf("token leaked into log output: %s", out)
	}

	if !strings.Contains(out, "[REDACTED:") {
		t.Fatalf("expected redaction marker in %s", out)
	}
}
