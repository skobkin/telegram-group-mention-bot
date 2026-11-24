package bot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"telegram-group-mention-bot/internal/storage"

	t "github.com/mymmrac/telego"
)

const testBotToken = "123456789:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// fakeTelegram serves the minimum Bot API surface needed by Run.
func fakeTelegram(t *testing.T, updatePolls *atomic.Int32) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasSuffix(r.URL.Path, "/getMe"):
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":123456789,"is_bot":true,"first_name":"Test","username":"test_bot"}}`))
		case strings.HasSuffix(r.URL.Path, "/getUpdates"):
			updatePolls.Add(1)
			time.Sleep(50 * time.Millisecond)
			_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

func TestRunStopsOnContextCancel(tt *testing.T) {
	var polls atomic.Int32

	srv := fakeTelegram(tt, &polls)

	store, err := storage.New(filepath.Join(tt.TempDir(), "test.sqlite"))
	if err != nil {
		tt.Fatal(err)
	}

	tt.Cleanup(func() { _ = store.Close() })

	api, err := t.NewBot(testBotToken, t.WithAPIServer(srv.URL))
	if err != nil {
		tt.Fatal(err)
	}

	b := &Bot{bot: api, storage: store}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() { done <- b.Run(ctx) }()

	deadline := time.After(3 * time.Second)

	for polls.Load() == 0 {
		select {
		case <-deadline:
			tt.Fatal("bot never started polling")
		case <-time.After(10 * time.Millisecond):
		}
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			tt.Fatalf("Run returned error on graceful shutdown: %v", err)
		}
	case <-time.After(5 * time.Second):
		tt.Fatal("Run did not return after context cancellation")
	}
}
