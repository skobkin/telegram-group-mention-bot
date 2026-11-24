// Package errreport is the single entry point for reporting errors to Sentry.
//
// Reporting is optional: without a DSN nothing is initialised and every call is a no-op.
// Context cancellation and deadline errors are dropped (they are the normal outcome of a
// shutdown and fan out into many unactionable events), and every event is run through the
// secret scrubber before it leaves the process.
package errreport

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/getsentry/sentry-go"

	"telegram-group-mention-bot/internal/support/secretscrub"
)

const flushTimeout = 2 * time.Second

// Config selects whether and how events are reported.
type Config struct {
	DSN         string // empty disables reporting
	Environment string // optional, e.g. "production"
	Release     string // build version
}

// Init starts Sentry reporting. The returned function flushes pending events and must be
// called on shutdown; it is never nil. With an empty DSN nothing is initialised.
func Init(cfg Config, scrubber *secretscrub.Scrubber) (flush func(), err error) {
	if cfg.DSN == "" {
		return func() {}, nil
	}

	if err := sentry.Init(clientOptions(cfg, scrubber)); err != nil {
		return func() {}, fmt.Errorf("init sentry: %w", err)
	}

	return func() { sentry.Flush(flushTimeout) }, nil
}

func clientOptions(cfg Config, scrubber *secretscrub.Scrubber) sentry.ClientOptions {
	return sentry.ClientOptions{
		Dsn:              cfg.DSN,
		Environment:      cfg.Environment,
		Release:          cfg.Release,
		AttachStacktrace: true,
		SendDefaultPII:   false,
		BeforeSend:       beforeSend(scrubber),
	}
}

// IsContextError reports whether err signals context cancellation or deadline expiry anywhere in its chain.
func IsContextError(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// Capture reports err unless it is nil or a context error.
func Capture(err error) {
	captureWith(sentry.CurrentHub(), err)
}

func captureWith(hub *sentry.Hub, err error) {
	if err == nil || IsContextError(err) {
		return
	}

	hub.CaptureException(err)
}

// CapturePanic reports a recovered panic value (with the stack of the recovery site).
func CapturePanic(recovered any) {
	capturePanicWith(sentry.CurrentHub(), recovered)
}

func capturePanicWith(hub *sentry.Hub, recovered any) {
	if recovered == nil {
		return
	}

	hub.Recover(recovered)
}
