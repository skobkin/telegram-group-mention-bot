package secretscrub

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

const testToken = "123456789:AAH-secretSecretSecret_123456789012"

func TestScrubReplacesRegisteredSecrets(t *testing.T) {
	s := New(TelegramTokenSecrets(testToken))

	got := s.Scrub("GET https://api.telegram.org/bot" + testToken + "/getMe")
	if strings.Contains(got, "AAH-secret") {
		t.Fatalf("secret leaked: %s", got)
	}

	if !strings.Contains(got, "[REDACTED:telegram-token]") {
		t.Fatalf("missing marker: %s", got)
	}
}

func TestScrubIgnoresEmptyAndIsIdempotent(t *testing.T) {
	s := New([]Secret{{Name: "empty", Value: ""}, {Name: "tok", Value: "abcdef-secret"}})

	if got := s.Scrub("nothing here"); got != "nothing here" {
		t.Fatalf("unexpected change: %q", got)
	}

	once := s.Scrub("x abcdef-secret y")
	if twice := s.Scrub(once); twice != once {
		t.Fatalf("not idempotent: %q vs %q", once, twice)
	}
}

func TestLeaksWalksWrappedChain(t *testing.T) {
	s := New(TelegramTokenSecrets(testToken))
	err := fmt.Errorf("outer: %w", errors.Join(errors.New("fine"), errors.New("bad "+testToken)))

	if !s.Leaks(err) {
		t.Fatal("expected leak to be detected in joined chain")
	}

	if s.Leaks(errors.New("clean")) {
		t.Fatal("clean error reported as leaking")
	}
}

func TestMarkerNeverReintroducesSecret(t *testing.T) {
	s := New([]Secret{{Name: "x", Value: "[REDACTED:x]"}})
	if got := s.Scrub("a [REDACTED:x] b"); strings.Contains(got, "[REDACTED:x]") {
		t.Fatalf("marker reintroduced the secret: %q", got)
	}
}
