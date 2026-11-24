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
