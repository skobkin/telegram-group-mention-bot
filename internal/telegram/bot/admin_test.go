package bot

import (
	"context"
	"strings"
	"testing"
	"time"

	t "github.com/mymmrac/telego"
)

// withReply runs a command as a reply to a message authored by target.
func (h *harness) runReplying(from, target *t.User, text string) *t.SendMessageParams {
	h.t.Helper()

	name := strings.TrimPrefix(strings.Fields(text)[0], "/")
	msg := h.msg(from, text)
	msg.ReplyToMessage = &t.Message{MessageID: 7, From: target, Chat: msg.Chat}

	for _, cmd := range h.bot.commands() {
		if cmd.names[0] == name {
			_ = cmd.handler(context.Background(), msg)

			return h.api.last(h.t)
		}
	}

	h.t.Fatalf("unknown command %q", name)

	return nil
}

func (h *harness) memberCount() int {
	h.t.Helper()

	members, err := h.store.GetGroupMembers(context.Background(), h.devsID())
	if err != nil {
		h.t.Fatal(err)
	}

	return len(members)
}

func TestAdminAddAndRemoveMember(tb *testing.T) {
	h := newHarness(tb)
	admin := h.user(1, "admin", "Admin")
	bob := &t.User{ID: 2, Username: "bob", FirstName: "Bob"} // never seen before
	h.api.statuses = map[int64]string{1: t.MemberStatusAdministrator}
	h.group("devs")

	contains(tb, h.runReplying(admin, bob, "/add devs"), "Bob was added to group 'devs'")

	if h.memberCount() != 1 {
		tb.Fatal("bob should be a member")
	}

	contains(tb, h.runReplying(admin, bob, "/add devs"), "already a member")
	contains(tb, h.runReplying(admin, bob, "/remove devs"), "Bob was removed from group 'devs'")
	contains(tb, h.runReplying(admin, bob, "/remove devs"), "not a member")
	contains(tb, h.runReplying(admin, bob, "/add ghosts"), "Group 'ghosts' not found")
	contains(tb, h.run(admin, "/add devs"), "Reply to the user's message")
	contains(tb, h.run(admin, "/add"), "Usage")
}

func TestAdminCommandsRejectNonAdmins(tb *testing.T) {
	h := newHarness(tb)
	alice := h.user(1, "alice", "Alice")
	bob := h.user(2, "bob", "Bob")
	h.group("devs", 2)

	contains(tb, h.runReplying(alice, bob, "/add devs"), "Only chat admins")
	contains(tb, h.runReplying(alice, bob, "/remove devs"), "Only chat admins")
	contains(tb, h.run(alice, "/del devs force"), "Only chat admins")

	if h.memberCount() != 1 {
		tb.Fatal("non-admin must not change members")
	}
}

func TestAdminTargetsMentionedUser(tb *testing.T) {
	h := newHarness(tb)
	admin := h.user(1, "admin", "Admin")
	h.user(2, "Bob", "Bob")
	h.api.statuses = map[int64]string{1: t.MemberStatusCreator}
	h.group("devs")

	// "/add devs @bob" with a mention entity; the "é" makes UTF-16 offsets differ from bytes
	text := "/add devs @bob"
	msg := h.msg(admin, text)
	msg.Entities = []t.MessageEntity{
		{Type: t.EntityTypeBotCommand, Offset: 0, Length: 4},
		{Type: t.EntityTypeMention, Offset: 10, Length: 4},
	}

	for _, cmd := range h.bot.commands() {
		if cmd.names[0] == "add" {
			_ = cmd.handler(context.Background(), msg)
		}
	}

	contains(tb, h.api.last(tb), "Bob was added")

	// unknown @username gives a hint, not a crash
	msg.Text = "/add devs @nobody"
	msg.Entities[1] = t.MessageEntity{Type: t.EntityTypeMention, Offset: 10, Length: 7}

	for _, cmd := range h.bot.commands() {
		if cmd.names[0] == "add" {
			_ = cmd.handler(context.Background(), msg)
		}
	}

	contains(tb, h.api.last(tb), "don't know that user")
}

func TestEntityTextUsesUTF16Offsets(tb *testing.T) {
	// "😀" is two UTF-16 units but four bytes
	text := "😀 @bob"
	if got := entityText(text, t.MessageEntity{Offset: 3, Length: 4}); got != "@bob" {
		tb.Fatalf("got %q", got)
	}

	if entityText(text, t.MessageEntity{Offset: 3, Length: 40}) != "" {
		tb.Fatal("out-of-range entity must give empty text")
	}
}

func TestForceDeleteNeedsConfirmation(tb *testing.T) {
	h := newHarness(tb)
	admin := h.user(1, "admin", "Admin")
	h.user(2, "bob", "Bob")
	h.api.statuses = map[int64]string{1: t.MemberStatusAdministrator}
	h.group("devs", 2)

	ask := h.run(admin, "/del devs force")
	contains(tb, ask, "1 member")

	if h.memberCount() != 1 {
		tb.Fatal("nothing may be deleted before confirmation")
	}

	kb := ask.ReplyMarkup.(*t.InlineKeyboardMarkup)
	confirm, cancel := kb.InlineKeyboard[0][0].CallbackData, kb.InlineKeyboard[0][1].CallbackData

	// somebody else cannot press the buttons
	other := h.user(3, "eve", "Eve")
	h.press(other, confirm, nil)

	if h.lastToast() != "This menu is not for you." || h.memberCount() != 1 {
		tb.Fatal("only the requesting admin may confirm")
	}

	h.press(admin, cancel, nil)

	if !strings.Contains(plain(h.api.edits[len(h.api.edits)-1].Text), "cancelled") || h.memberCount() != 1 {
		tb.Fatal("cancel must keep the group")
	}

	h.press(admin, confirm, nil)

	if _, err := h.store.GetGroup(context.Background(), "devs", testChatID); err == nil {
		tb.Fatal("group should be deleted after confirmation")
	}

	if !strings.Contains(plain(h.api.edits[len(h.api.edits)-1].Text), "has been deleted") && !strings.Contains(plain(h.api.edits[len(h.api.edits)-1].Text), "have been deleted") {
		tb.Fatalf("unexpected edit %q", h.api.edits[len(h.api.edits)-1].Text)
	}

	// the cascade removed the member rows
	if members, _ := h.store.GetGroupMembers(context.Background(), 1); len(members) != 0 {
		tb.Fatal("members must be deleted with the group")
	}
}

func TestForceDeleteRechecksAdminOnConfirm(tb *testing.T) {
	h := newHarness(tb)
	admin := h.user(1, "admin", "Admin")
	h.api.statuses = map[int64]string{1: t.MemberStatusAdministrator}
	h.group("devs", 1)

	ask := h.run(admin, "/del devs force")
	confirm := ask.ReplyMarkup.(*t.InlineKeyboardMarkup).InlineKeyboard[0][0].CallbackData

	// demoted in the meantime, after the cache expired
	h.api.statuses = map[int64]string{}
	h.bot.now = func() time.Time { return time.Now().Add(2 * adminCacheTTL) }

	h.press(admin, confirm, nil)

	if h.lastToast() != "Only chat admins can do this." {
		tb.Fatalf("unexpected toast %q", h.lastToast())
	}

	if _, err := h.store.GetGroup(context.Background(), "devs", testChatID); err != nil {
		tb.Fatal("group must survive")
	}
}

func TestAdminCacheExpiry(tb *testing.T) {
	h := newHarness(tb)
	h.api.statuses = map[int64]string{1: t.MemberStatusAdministrator}

	now := time.Unix(1000, 0)
	h.bot.now = func() time.Time { return now }

	ctx := context.Background()

	if !h.bot.isChatAdmin(ctx, testChatID, 1) {
		tb.Fatal("user 1 is an admin")
	}

	if !h.bot.isChatAdmin(ctx, testChatID, 1) {
		tb.Fatal("user 1 is still an admin (cached)")
	}

	if h.api.adminCall != 1 {
		tb.Fatalf("second check must come from the cache, got %d API calls", h.api.adminCall)
	}

	now = now.Add(adminCacheTTL - time.Second)
	h.bot.isChatAdmin(ctx, testChatID, 1)

	if h.api.adminCall != 1 {
		tb.Fatal("entry must still be cached before the TTL")
	}

	h.api.statuses = map[int64]string{}
	now = now.Add(2 * time.Second)

	if h.bot.isChatAdmin(ctx, testChatID, 1) {
		tb.Fatal("expired entry must be re-fetched (user is no longer an admin)")
	}

	if h.api.adminCall != 2 {
		tb.Fatalf("expected a re-fetch after expiry, got %d API calls", h.api.adminCall)
	}
}

func TestAnonymousAdminCountsAsAdmin(tb *testing.T) {
	h := newHarness(tb)
	msg := h.msg(&t.User{ID: 1087968824, IsBot: true}, "/clear")
	msg.SenderChat = &t.Chat{ID: testChatID}

	if !h.bot.isAdminMessage(context.Background(), &msg) {
		tb.Fatal("anonymous admin must count as admin")
	}

}
