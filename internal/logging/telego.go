package logging

import (
	"fmt"
	"log/slog"
)

// TelegoLogger adapts slog to the telego.Logger interface so library output
// goes through the same (scrubbing) handler as the rest of the application.
type TelegoLogger struct{}

// Debugf logs at debug level.
func (TelegoLogger) Debugf(format string, args ...any) {
	slog.Debug("telego: " + fmt.Sprintf(format, args...))
}

// Errorf logs at error level.
func (TelegoLogger) Errorf(format string, args ...any) {
	slog.Error("telego: " + fmt.Sprintf(format, args...))
}
