package bot

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"telegram-group-mention-bot/internal/storage"

	"github.com/getsentry/sentry-go"
	ta "github.com/mymmrac/telego/telegoapi"
)

type countingTransport struct {
	mu     sync.Mutex
	events int
}

func (c *countingTransport) Configure(sentry.ClientOptions) {}
func (c *countingTransport) SendEvent(*sentry.Event)        { c.mu.Lock(); c.events++; c.mu.Unlock() }
func (c *countingTransport) Flush(time.Duration) bool       { return true }
func (c *countingTransport) FlushWithContext(context.Context) bool {
	return true
}
func (c *countingTransport) Close() {}

func (c *countingTransport) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.events
}

// bindSentry points the global hub at a counting transport for the duration of the test.
func bindSentry(tb *testing.T) *countingTransport {
	tb.Helper()

	transport := &countingTransport{}

	client, err := sentry.NewClient(sentry.ClientOptions{Dsn: "https://k@s.example.com/1", Transport: transport})
	if err != nil {
		tb.Fatal(err)
	}

	hub := sentry.CurrentHub()
	previous := hub.Client()
	hub.BindClient(client)
	tb.Cleanup(func() { hub.BindClient(previous) })

	return transport
}

func TestPanicIsReportedOnce(tb *testing.T) {
	transport := bindSentry(tb)

	if err := recoverFromPanic("boom"); err != nil {
		tb.Fatalf("recovery must let the update loop continue, got %v", err)
	}

	if transport.count() != 1 {
		tb.Fatalf("expected one event for the panic, got %d", transport.count())
	}
}

func TestOnlyUnexpectedErrorsAreReported(tb *testing.T) {
	transport := bindSentry(tb)

	for _, err := range []error{
		storage.ErrNotFound,
		fmt.Errorf("find: %w", storage.ErrAlreadyExists),
		storage.ErrEmptyGroupName,
		context.Canceled,
		fmt.Errorf("send: %w", context.DeadlineExceeded),
		&ta.Error{ErrorCode: 403, Description: "Forbidden: bot was kicked"},
		&ta.Error{ErrorCode: 429, Description: "Too Many Requests"},
	} {
		logError("bot: test", err)
	}

	if transport.count() != 0 {
		tb.Fatalf("expected errors must not be reported, got %d events", transport.count())
	}

	logError("bot: test", errors.New("database exploded"))

	if transport.count() != 1 {
		tb.Fatalf("an unexpected error is reported exactly once, got %d", transport.count())
	}
}

func TestNoReportingWithoutClient(tb *testing.T) {
	hub := sentry.CurrentHub()
	previous := hub.Client()
	hub.BindClient(nil)
	tb.Cleanup(func() { hub.BindClient(previous) })

	// must be a harmless no-op
	logError("bot: test", errors.New("x"))

	_ = recoverFromPanic("boom")
}
