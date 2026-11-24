// Package secretscrub removes registered application secrets from arbitrary
// text, log records and error chains, so that logs and error reports cannot
// retain credential material. Matching is literal: a registered secret value
// is replaced wherever it appears, whatever the surrounding text.
package secretscrub

import (
	"sort"
	"strings"
)

// Secret names one piece of credential material for scrubbing.
type Secret struct {
	// Name is a stable label embedded in the replacement marker (for
	// example "telegram-token").
	Name string
	// Value is the exact literal replaced everywhere it appears. Empty
	// values are ignored. Values must be genuine secrets: short or generic
	// strings would corrupt surrounding text, and there is deliberately no
	// length floor that could silently disable redaction of a short real
	// key.
	Value string
}

// Scrubber replaces registered secret values with "[REDACTED:<name>]". It is
// immutable after New and safe for concurrent use.
type Scrubber struct {
	names    []string // parallel to values
	values   []string // longest first
	replacer *strings.Replacer

	// Omission markers, computed at construction time so none of them can
	// reintroduce a registered value (see safeMarker). Composite marks the
	// wholesale failure of Scrub; truncated and unserializable close the
	// value walk's depth cap and serialization boundary (see ScrubValue).
	compositeMarker      string
	truncatedMarker      string
	unserializableMarker string
}

// New builds a Scrubber. Empty values are skipped and duplicate values are
// collapsed (the first name wins). Secrets are ordered longest-value-first,
// so a value that is a prefix of another is replaced as part of the longer
// match.
//
// Replacement is a single left-to-right pass that never rescans emitted
// markers, and every marker is verified at construction time to contain no
// registered value: a secret equal to (or embedded in) a marker string is
// otherwise reintroduced by the redaction itself, and sequential
// replacements could corrupt earlier markers. Markers fall back to the
// name-less "[REDACTED]" and then to an empty replacement, in that order —
// so the result of Scrub can never carry a registered value, whatever the
// configured secrets are.
func New(secrets []Secret) *Scrubber {
	byValue := make(map[string]string, len(secrets))
	for _, secret := range secrets {
		if secret.Value == "" {
			continue
		}

		if _, dup := byValue[secret.Value]; dup {
			continue
		}

		byValue[secret.Value] = secret.Name
	}

	s := &Scrubber{}
	for value, name := range byValue {
		s.values = append(s.values, value)
		s.names = append(s.names, name)
	}

	// Map iteration order is nondeterministic; sorting by descending value
	// length makes replacement deterministic and prefix-correct.
	order := make([]int, len(s.values))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool {
		return len(s.values[order[a]]) > len(s.values[order[b]])
	})

	names := make([]string, len(order))
	values := make([]string, len(order))
	for i, idx := range order {
		names[i] = s.names[idx]
		values[i] = s.values[idx]
	}
	s.names = names
	s.values = values

	pairs := make([]string, 0, len(s.values)*2)
	for i := range s.values {
		pairs = append(pairs, s.values[i], s.safeMarker(s.names[i]))
	}

	if len(pairs) > 0 {
		s.replacer = strings.NewReplacer(pairs...)
	}

	// The omission markers are chosen after the values are known, so none of
	// them can itself carry a registered value and reintroduce it into
	// scrubbed output.
	s.compositeMarker = s.safeMarker("composite")
	s.truncatedMarker = s.safeMarker("depth-limit")
	s.unserializableMarker = s.safeMarker("unserializable")

	return s
}

// safeMarker picks the replacement string for one secret: the first of the
// standard marker, the name-less fallback, and the empty replacement that
// contains no registered value. Empty always qualifies, so the search
// terminates with a secret-free result.
func (s *Scrubber) safeMarker(name string) string {
	for _, candidate := range []string{marker(name), "[REDACTED]", ""} {
		if !s.containsAnyValue(candidate) {
			return candidate
		}
	}

	return ""
}

func (s *Scrubber) containsAnyValue(text string) bool {
	for _, value := range s.values {
		if strings.Contains(text, value) {
			return true
		}
	}

	return false
}

// Scrub replaces every occurrence of every registered secret value in text
// with "[REDACTED:<name>]". Text carrying no secret is returned unchanged,
// and scrubbing is idempotent: the pass never rescans its own output and
// every marker is constructed to contain no registered value.
//
// Because the pass emits the result in one sweep, the complete result is
// verified once before it is returned. Removing a secret can splice the
// text around it into another one — with "REDACTED" and "ab" registered,
// scrubbing "aREDACTEDb" joins the outer text into "ab" — so a result that
// still carries any registered value is replaced wholesale by the composite
// marker: redaction fails closed, without an unbounded replacement loop.
func (s *Scrubber) Scrub(text string) string {
	if s == nil || s.replacer == nil || !s.Contains(text) {
		return text
	}

	scrubbed := s.replacer.Replace(text)

	if s.Contains(scrubbed) {
		return s.compositeMarker
	}

	return scrubbed
}

// TruncatedMarker returns the marker ScrubValue substitutes for subtrees its
// walk cannot complete before the depth cap. Like every marker, it was
// chosen at construction time to contain no registered value.
func (s *Scrubber) TruncatedMarker() string {
	if s == nil {
		return ""
	}

	return s.truncatedMarker
}

// UnserializableMarker returns the marker the serialization boundary of
// ScrubValue substitutes for values it cannot inspect (marshal failures,
// panicking marshalers). Like every marker, it was chosen at construction
// time to contain no registered value.
func (s *Scrubber) UnserializableMarker() string {
	if s == nil {
		return ""
	}

	return s.unserializableMarker
}

// Contains reports whether text carries any registered secret value. It is a
// cheap pre-check for callers that only need the verdict.
func (s *Scrubber) Contains(text string) bool {
	if s == nil {
		return false
	}

	for _, value := range s.values {
		if strings.Contains(text, value) {
			return true
		}
	}

	return false
}

// Leaks reports whether any error reachable from err carries a registered
// secret in its message. The traversal mirrors sentry-go's exception
// conversion (Unwrap() []error, then Unwrap() error, then Cause() error), so
// a false result means the whole chain is safe to serialize.
func (s *Scrubber) Leaks(err error) bool {
	if err == nil {
		return false
	}

	if s.Contains(err.Error()) {
		return true
	}

	// Order matches sentry-go exception.go convertErrorDFS.
	switch v := err.(type) { //nolint:errorlint // enumerating unwrap structure exactly like sentry-go, not matching a target
	case interface{ Unwrap() []error }:
		for _, cause := range v.Unwrap() {
			if s.Leaks(cause) {
				return true
			}
		}

		return false
	case interface{ Unwrap() error }:
		return s.Leaks(v.Unwrap())
	case interface{ Cause() error }:
		return s.Leaks(v.Cause())
	default:
		return false
	}
}

func marker(name string) string {
	return "[REDACTED:" + name + "]"
}
