package errreport

import (
	"github.com/getsentry/sentry-go"

	"telegram-group-mention-bot/internal/support/secretscrub"
)

// beforeSend scrubs every registered secret from the string-bearing parts of an event, in place,
// and always returns the event (returning nil would drop it). Stack frames, SDK data and module
// lists are skipped: they describe this binary and never embed environment-derived secrets.
func beforeSend(scrubber *secretscrub.Scrubber) func(*sentry.Event, *sentry.EventHint) *sentry.Event {
	return func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
		if event != nil && scrubber != nil {
			scrubEvent(scrubber, event)
		}

		return event
	}
}

func scrubEvent(s *secretscrub.Scrubber, event *sentry.Event) {
	event.Message = s.Scrub(event.Message)
	event.Transaction = s.Scrub(event.Transaction)
	event.ServerName = s.Scrub(event.ServerName)

	// Scope-sourced payloads are shared between concurrent events, so scrubbed copies are
	// event-owned instead of being mutated in place.
	if len(event.Fingerprint) > 0 {
		fingerprint := make([]string, len(event.Fingerprint))
		for i, part := range event.Fingerprint {
			fingerprint[i] = s.Scrub(part)
		}

		event.Fingerprint = fingerprint
	}

	for i := range event.Exception {
		event.Exception[i].Type = s.Scrub(event.Exception[i].Type)
		event.Exception[i].Value = s.Scrub(event.Exception[i].Value)

		if mechanism := event.Exception[i].Mechanism; mechanism != nil {
			mechanism.Description = s.Scrub(mechanism.Description)
			mechanism.Data = s.ScrubMap(mechanism.Data)
		}
	}

	for i, crumb := range event.Breadcrumbs {
		if crumb == nil {
			continue
		}

		owned := *crumb
		owned.Message = s.Scrub(crumb.Message)
		owned.Category = s.Scrub(crumb.Category)
		owned.Type = s.Scrub(crumb.Type)
		owned.Data = s.ScrubMap(crumb.Data)
		event.Breadcrumbs[i] = &owned
	}

	if event.Request != nil {
		event.Request.URL = s.Scrub(event.Request.URL)
		event.Request.Data = s.Scrub(event.Request.Data)
		event.Request.QueryString = s.Scrub(event.Request.QueryString)
		event.Request.Cookies = s.Scrub(event.Request.Cookies)
		scrubStringMap(s, event.Request.Headers)
		scrubStringMap(s, event.Request.Env)
	}

	event.User.ID = s.Scrub(event.User.ID)
	event.User.Email = s.Scrub(event.User.Email)
	event.User.IPAddress = s.Scrub(event.User.IPAddress)
	event.User.Username = s.Scrub(event.User.Username)
	event.User.Name = s.Scrub(event.User.Name)

	if event.User.Data != nil {
		data := make(map[string]string, len(event.User.Data))
		for key, value := range event.User.Data {
			data[s.Scrub(key)] = s.Scrub(value)
		}

		event.User.Data = data
	}

	for i := range event.Threads {
		event.Threads[i].Name = s.Scrub(event.Threads[i].Name)
	}

	scrubStringMap(s, event.Tags)
	event.Extra = s.ScrubMap(event.Extra)

	if len(event.Contexts) > 0 {
		contexts := make(map[string]sentry.Context, len(event.Contexts))
		for name, data := range event.Contexts {
			contexts[s.Scrub(name)] = s.ScrubMap(data)
		}

		event.Contexts = contexts
	}
}

// scrubStringMap scrubs keys and values in place; the maps it sees are event-owned.
func scrubStringMap(s *secretscrub.Scrubber, values map[string]string) {
	updates := make([][2]string, 0, len(values))

	for key, value := range values {
		updates = append(updates, [2]string{s.Scrub(key), s.Scrub(value)})
		delete(values, key)
	}

	for _, pair := range updates {
		values[pair[0]] = pair[1]
	}
}
