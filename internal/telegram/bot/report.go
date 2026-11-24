package bot

import (
	"errors"
	"log/slog"

	"telegram-group-mention-bot/internal/storage"
	"telegram-group-mention-bot/internal/support/errreport"

	ta "github.com/mymmrac/telego/telegoapi"
)

// logError logs an unexpected failure and reports it to Sentry. It is the only place in the bot
// that reports errors (storage logs but never reports), so one failure produces one event.
func logError(msg string, err error, attrs ...any) {
	slog.Error(msg, append([]any{"error", err}, attrs...)...)

	if !isExpected(err) {
		errreport.Capture(err)
	}
}

// isExpected reports whether err is an ordinary outcome that is not worth an event: user
// mistakes (missing or duplicate names) and Telegram refusing because the bot was removed or
// blocked (403) or is rate limited (429).
func isExpected(err error) bool {
	if errors.Is(err, storage.ErrNotFound) || errors.Is(err, storage.ErrAlreadyExists) ||
		errors.Is(err, storage.ErrEmptyGroupName) {
		return true
	}

	var apiErr *ta.Error
	if errors.As(err, &apiErr) {
		return apiErr.ErrorCode == 403 || apiErr.ErrorCode == 429
	}

	return false
}
