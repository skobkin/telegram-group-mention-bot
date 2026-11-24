// Package logging provides slog helpers that keep secrets out of log output.
package logging

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

const redacted = "[REDACTED]"

// Scrubber replaces registered secret values with a fixed marker.
type Scrubber struct {
	mu      sync.RWMutex
	secrets []string
}

// NewScrubber creates a scrubber; empty secrets are ignored.
func NewScrubber(secrets ...string) *Scrubber {
	s := &Scrubber{}
	for _, secret := range secrets {
		s.Register(secret)
	}

	return s
}

// Register adds a secret value that must never reach the logs.
func (s *Scrubber) Register(secret string) {
	if secret == "" {
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.secrets = append(s.secrets, secret)
}

// Scrub returns text with every registered secret replaced.
func (s *Scrubber) Scrub(text string) string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	for _, secret := range s.secrets {
		text = strings.ReplaceAll(text, secret, redacted)
	}

	return text
}

// NewScrubHandler wraps next so that every record is scrubbed before output.
func NewScrubHandler(scrubber *Scrubber, next slog.Handler) slog.Handler {
	return &scrubHandler{scrubber: scrubber, next: next}
}

type scrubHandler struct {
	scrubber *Scrubber
	next     slog.Handler
}

func (h *scrubHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *scrubHandler) Handle(ctx context.Context, record slog.Record) error {
	rebuilt := slog.NewRecord(record.Time, record.Level, h.scrubber.Scrub(record.Message), record.PC)
	record.Attrs(func(attr slog.Attr) bool {
		rebuilt.AddAttrs(h.scrubAttr(attr))

		return true
	})

	return h.next.Handle(ctx, rebuilt)
}

func (h *scrubHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	scrubbed := make([]slog.Attr, len(attrs))
	for i, attr := range attrs {
		scrubbed[i] = h.scrubAttr(attr)
	}

	return &scrubHandler{scrubber: h.scrubber, next: h.next.WithAttrs(scrubbed)}
}

func (h *scrubHandler) WithGroup(name string) slog.Handler {
	return &scrubHandler{scrubber: h.scrubber, next: h.next.WithGroup(h.scrubber.Scrub(name))}
}

// scrubAttr scrubs the key and value of an attribute. Strings, errors, groups
// and any other value are all rendered to text where needed, because custom
// types (for example HTTP client errors) may embed the secret.
func (h *scrubHandler) scrubAttr(attr slog.Attr) slog.Attr {
	attr.Key = h.scrubber.Scrub(attr.Key)
	attr.Value = attr.Value.Resolve()

	switch attr.Value.Kind() {
	case slog.KindString:
		attr.Value = slog.StringValue(h.scrubber.Scrub(attr.Value.String()))
	case slog.KindGroup:
		source := attr.Value.Group()
		group := make([]slog.Attr, len(source))

		for i := range source {
			group[i] = h.scrubAttr(source[i])
		}

		attr.Value = slog.GroupValue(group...)
	case slog.KindAny:
		if err, ok := attr.Value.Any().(error); ok {
			attr.Value = slog.StringValue(h.scrubber.Scrub(safeErrorText(err)))

			break
		}

		attr.Value = slog.StringValue(h.scrubber.Scrub(fmt.Sprint(attr.Value.Any())))
	}

	return attr
}

func safeErrorText(err error) (text string) {
	defer func() {
		if r := recover(); r != nil {
			text = fmt.Sprintf("!PANIC: %v", r)
		}
	}()

	return err.Error()
}
