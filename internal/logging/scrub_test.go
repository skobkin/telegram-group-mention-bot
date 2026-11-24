package logging

import (
	"bytes"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func newTestLogger(buf *bytes.Buffer, secrets ...string) *slog.Logger {
	inner := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})

	return slog.New(NewScrubHandler(NewScrubber(secrets...), inner))
}

func TestScrubHandlerRedactsSecrets(t *testing.T) {
	const token = "123456:SECRET-token"

	var buf bytes.Buffer

	logger := newTestLogger(&buf, token).With("pre", "has "+token).WithGroup("g")
	logger.Error("failed "+token,
		"string", "url/bot"+token+"/getMe",
		"err", errors.New("Post https://api/bot"+token+": timeout"),
		"nested", slog.Group("n", slog.String("k", token)),
		"any", []string{token},
		token, "key",
	)

	out := buf.String()
	if strings.Contains(out, token) {
		t.Fatalf("secret leaked into log output: %s", out)
	}

	if !strings.Contains(out, redacted) {
		t.Fatalf("expected redaction marker in %s", out)
	}
}

func TestScrubberIgnoresEmptySecret(t *testing.T) {
	if got := NewScrubber("").Scrub("text"); got != "text" {
		t.Fatalf("unexpected scrub result %q", got)
	}
}
