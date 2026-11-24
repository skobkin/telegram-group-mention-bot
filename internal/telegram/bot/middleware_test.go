package bot

import (
	"testing"

	t "github.com/mymmrac/telego"
	th "github.com/mymmrac/telego/telegohandler"
)

func TestPanicRecoveryKeepsUpdateLoopAlive(tt *testing.T) {
	handler := th.PanicRecoveryHandler(recoverFromPanic)

	// A nil context makes ctx.Next panic, which must be recovered and not propagated.
	if err := handler(nil, t.Update{}); err != nil {
		tt.Fatalf("expected recovered panic to yield nil error, got %v", err)
	}
}

func TestChatMigrationHandlesBothServiceMessages(tt *testing.T) {
	cases := []struct {
		name         string
		msg          t.Message
		from, to     int64
		wantMigrates bool
	}{
		{"old chat gets migrate_to", t.Message{Chat: t.Chat{ID: -1}, MigrateToChatID: -100}, -1, -100, true},
		{"new chat gets migrate_from", t.Message{Chat: t.Chat{ID: -100}, MigrateFromChatID: -1}, -1, -100, true},
		{"ordinary message", t.Message{Chat: t.Chat{ID: -1}}, 0, 0, false},
	}

	for _, tc := range cases {
		tt.Run(tc.name, func(tt *testing.T) {
			from, to, ok := chatMigration(&tc.msg)
			if ok != tc.wantMigrates || from != tc.from || to != tc.to {
				tt.Fatalf("got (%d, %d, %v), want (%d, %d, %v)", from, to, ok, tc.from, tc.to, tc.wantMigrates)
			}
		})
	}
}
