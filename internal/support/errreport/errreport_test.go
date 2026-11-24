package errreport

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"

	"telegram-group-mention-bot/internal/support/secretscrub"
)

const testToken = "123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

type recordingTransport struct {
	mu     sync.Mutex
	events []*sentry.Event
}

func (r *recordingTransport) Configure(sentry.ClientOptions) {}
func (r *recordingTransport) SendEvent(e *sentry.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.events = append(r.events, e)
}
func (r *recordingTransport) Flush(time.Duration) bool              { return true }
func (r *recordingTransport) FlushWithContext(context.Context) bool { return true }
func (r *recordingTransport) Close()                                {}

func (r *recordingTransport) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.events)
}

func newHub(t *testing.T) (*sentry.Hub, *recordingTransport) {
	t.Helper()

	scrubber := secretscrub.New(secretscrub.TelegramTokenSecrets(testToken))
	transport := &recordingTransport{}

	opts := clientOptions(Config{DSN: "https://publickey@sentry.example.com/1", Release: "1.2.3"}, scrubber)
	opts.Transport = transport

	client, err := sentry.NewClient(opts)
	if err != nil {
		t.Fatal(err)
	}

	return sentry.NewHub(client, sentry.NewScope()), transport
}

func TestIsContextError(t *testing.T) {
	for name, tc := range map[string]struct {
		err  error
		want bool
	}{
		"nil":      {nil, false},
		"plain":    {errors.New("x"), false},
		"canceled": {context.Canceled, true},
		"wrapped":  {fmt.Errorf("get: %w", context.DeadlineExceeded), true},
		"joined":   {errors.Join(errors.New("x"), context.Canceled), true},
	} {
		if got := IsContextError(tc.err); got != tc.want {
			t.Errorf("%s: got %v, want %v", name, got, tc.want)
		}
	}
}

func TestCaptureSkipsContextErrors(t *testing.T) {
	hub, transport := newHub(t)

	captureWith(hub, nil)
	captureWith(hub, context.Canceled)
	captureWith(hub, fmt.Errorf("storage: %w", context.DeadlineExceeded))

	if transport.count() != 0 {
		t.Fatalf("cancellation must produce no event, got %d", transport.count())
	}

	captureWith(hub, errors.New("real failure"))

	if transport.count() != 1 {
		t.Fatalf("expected exactly one event, got %d", transport.count())
	}

	if got := transport.events[0].Release; got != "1.2.3" {
		t.Fatalf("release must be the build version, got %q", got)
	}
}

func TestPanicProducesOneScrubbedEvent(t *testing.T) {
	hub, transport := newHub(t)

	func() {
		defer func() { capturePanicWith(hub, recover()) }()

		panic("handler exploded with token " + testToken)
	}()

	if transport.count() != 1 {
		t.Fatalf("expected one event, got %d", transport.count())
	}

	event := transport.events[0]
	event.Extra = nil

	dump := fmt.Sprintf("%+v %+v", event.Message, event.Exception)
	if strings.Contains(dump, testToken) || strings.Contains(dump, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA") {
		t.Fatalf("token leaked into the event: %s", dump)
	}

	if !strings.Contains(dump, "handler exploded") {
		t.Fatalf("scrubbing must keep the rest of the message: %s", dump)
	}
}

func TestErrorEventsAreScrubbed(t *testing.T) {
	hub, transport := newHub(t)

	scope := hub.Scope()
	scope.SetTag("secret", testToken)
	scope.SetContext("detail", sentry.Context{"value": "bad " + testToken})
	scope.AddBreadcrumb(&sentry.Breadcrumb{Message: "calling " + testToken, Data: map[string]any{"url": testToken}}, 10)

	captureWith(hub, errors.New("Post https://api.telegram.org/bot"+testToken+"/sendMessage: refused"))

	event := transport.events[0]
	dump := fmt.Sprintf("%+v|%+v|%+v|%+v", event.Exception, event.Tags, event.Contexts, event.Breadcrumbs[0])

	if strings.Contains(dump, "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA") {
		t.Fatalf("token leaked: %s", dump)
	}
}

func TestInitWithoutDSNDoesNothing(t *testing.T) {
	flush, err := Init(Config{}, nil)
	if err != nil || flush == nil {
		t.Fatalf("Init without DSN must succeed with a no-op flush, got %v", err)
	}

	flush()

	if sentry.CurrentHub().Client() != nil {
		t.Fatal("nothing may be initialised without a DSN")
	}
}

func TestInitRejectsInvalidDSN(t *testing.T) {
	flush, err := Init(Config{DSN: "not a dsn"}, nil)
	if err == nil || flush == nil {
		t.Fatalf("expected an error and a usable flush, got %v", err)
	}
}
