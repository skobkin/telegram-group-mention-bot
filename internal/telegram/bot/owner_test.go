package bot

import (
	"context"
	"strings"
	"testing"

	t "github.com/mymmrac/telego"
)

func (h *harness) statsAs(from *t.User, chatType string) int {
	h.t.Helper()

	before := len(h.api.sent)
	msg := h.msg(from, "/stats")
	msg.Chat = t.Chat{ID: from.ID, Type: chatType}

	for _, cmd := range h.bot.commands() {
		if cmd.names[0] == "stats" {
			_ = cmd.handler(context.Background(), msg)
		}
	}

	return len(h.api.sent) - before
}

func TestStatsDisabledWithoutOwners(tb *testing.T) {
	h := newHarness(tb)

	for _, cmd := range h.bot.commands() {
		if cmd.names[0] == "stats" {
			tb.Fatal("/stats must not be registered when BOT_OWNER_IDS is empty")
		}
	}
}

func TestStatsOwnerGating(tb *testing.T) {
	h := newHarness(tb)
	h.bot.owners = map[int64]struct{}{7: {}}
	owner := h.user(7, "owner", "Owner")
	other := h.user(8, "other", "Other")
	h.group("devs", 7, 8)

	if n := h.statsAs(other, t.ChatTypePrivate); n != 0 {
		tb.Fatalf("non-owner must get no response, got %d messages", n)
	}

	if n := h.statsAs(owner, t.ChatTypeSupergroup); n != 0 {
		tb.Fatalf("owner in a group must get no response, got %d messages", n)
	}

	if n := h.statsAs(owner, t.ChatTypePrivate); n != 1 {
		tb.Fatalf("owner in private must get stats, got %d messages", n)
	}

	reply := h.api.last(tb)
	contains(tb, reply, "Groups: 1")
	contains(tb, reply, "Users: 2")
	contains(tb, reply, "Memberships: 2")
	contains(tb, reply, "Chats with groups: 1")

	// Hidden from /help
	if help := plain(h.run(owner, "/help").Text); strings.Contains(help, "/stats") {
		tb.Fatalf("/stats must not be listed in /help: %s", help)
	}
}

func TestFormatBytes(tb *testing.T) {
	for in, want := range map[int64]string{0: "0 B", 1023: "1023 B", 1024: "1.0 KiB", 5 * 1024 * 1024: "5.0 MiB"} {
		if got := formatBytes(in); got != want {
			tb.Errorf("formatBytes(%d) = %q, want %q", in, got, want)
		}
	}
}
