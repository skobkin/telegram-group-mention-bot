package bot

import (
	"context"
	"fmt"
	"strings"
	"telegram-group-mention-bot/internal/i18n"
	"testing"

	"telegram-group-mention-bot/internal/storage"

	t "github.com/mymmrac/telego"
)

func TestParseCallback(tb *testing.T) {
	good := map[string]callbackData{
		"v1:join:5:7":    {action: "join", groupID: 5, requester: 7},
		"v1:join:5:0":    {action: "join", groupID: 5},
		"v1:leave:5:0":   {action: "leave", groupID: 5},
		"v1:m:9:7":       {action: "m", groupID: 9, requester: 7},
		"v1:pg:show:2:7": {paging: true, action: "show", page: 2, requester: 7},
	}

	for raw, want := range good {
		got, err := parseCallback(raw)
		if err != nil || got != want {
			tb.Errorf("parseCallback(%q) = %+v, %v; want %+v", raw, got, err, want)
		}

		if got.String() != raw {
			tb.Errorf("round trip of %q gave %q", raw, got.String())
		}
	}

	for _, raw := range []string{
		"", "v2:join:5:7", "v1:nuke:5:7", "v1:join:x:7", "v1:join:5:x", "v1:join:5", "v1:join:5:7:9",
		"v1:m:9:0", "v1:del:9:0", "v1:pg:join:1:0", "v1:pg:nuke:1:7", "v1:pg:join:-1:7", "v1:join:-1:7",
	} {
		if _, err := parseCallback(raw); err == nil {
			tb.Errorf("parseCallback(%q) must fail", raw)
		}
	}
}

func TestCallbackDataFitsTelegramLimit(tb *testing.T) {
	longest := callbackData{paging: true, action: "leave", page: 999999, requester: 9999999999999}.String()
	if len(longest) > 64 {
		tb.Fatalf("callback data %q exceeds 64 bytes", longest)
	}
}

func (h *harness) press(from *t.User, data string, origin *t.Message) {
	h.t.Helper()

	picker := &t.Message{MessageID: 100, Chat: t.Chat{ID: testChatID, Type: t.ChatTypeSupergroup}, ReplyToMessage: origin}

	err := h.bot.handleCallback(context.Background(), t.CallbackQuery{ID: "q", From: *from, Message: picker, Data: data})
	if err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) lastToast() string {
	h.t.Helper()

	if len(h.api.answers) == 0 {
		h.t.Fatal("callback was not answered")
	}

	return h.api.answers[len(h.api.answers)-1].Text
}

func (h *harness) devsID() uint {
	h.t.Helper()

	g, err := h.store.GetGroup(context.Background(), "devs", testChatID)
	if err != nil {
		h.t.Fatal(err)
	}

	return g.ID
}

func TestPickerJoinEditsMessage(tb *testing.T) {
	h := newHarness(tb)
	alice := h.user(1, "alice", "Alice")
	h.group("devs")

	h.press(alice, fmt.Sprintf("v1:join:%d:1", h.devsID()), nil)

	if got := h.lastToast(); !strings.Contains(got, "You have joined") {
		tb.Fatalf("unexpected toast %q", got)
	}

	if len(h.api.edits) != 1 || h.api.edits[0].MessageID != 100 || !strings.Contains(plain(h.api.edits[0].Text), "You have joined") {
		tb.Fatalf("picker must be edited in place, got %+v", h.api.edits)
	}

	if len(h.api.sent) != 0 {
		tb.Fatal("no new message expected")
	}

	// the keyboard is removed by editing without reply markup
	if h.api.edits[0].ReplyMarkup != nil {
		tb.Fatal("edited picker must not keep a keyboard")
	}
}

func TestPickerRejectsOtherUsers(tb *testing.T) {
	h := newHarness(tb)
	h.user(1, "alice", "Alice")
	bob := h.user(2, "bob", "Bob")
	h.group("devs")

	h.press(bob, fmt.Sprintf("v1:join:%d:1", h.devsID()), nil)

	if got := h.lastToast(); got != "This menu is not for you." {
		tb.Fatalf("unexpected toast %q", got)
	}

	members, _ := h.store.GetGroupMembers(context.Background(), h.devsID())
	if len(members) != 0 || len(h.api.edits) != 0 {
		tb.Fatal("another user's picker must not do anything")
	}
}

func TestCallbackRejectsInvalidDataAndForeignChat(tb *testing.T) {
	h := newHarness(tb)
	alice := h.user(1, "alice", "Alice")
	h.group("devs")

	h.press(alice, "v1:nuke:1:1", nil)

	if got := h.lastToast(); got != "Invalid button." {
		tb.Fatalf("unexpected toast %q", got)
	}

	// a group that belongs to another chat
	ctx := context.Background()
	_ = h.store.CreateGroup(ctx, "other", -2002)
	other, _ := h.store.GetGroup(ctx, "other", -2002)

	h.press(alice, fmt.Sprintf("v1:show:%d:1", other.ID), nil)

	if got := h.lastToast(); got != "This group no longer exists." {
		tb.Fatalf("unexpected toast %q", got)
	}

	if len(h.api.edits) != 1 || !strings.Contains(plain(h.api.edits[0].Text), "no longer exists") {
		tb.Fatalf("stale picker must be edited, got %+v", h.api.edits)
	}
}

func TestCallbackForDeletedGroup(tb *testing.T) {
	h := newHarness(tb)
	alice := h.user(1, "alice", "Alice")
	h.group("devs")
	id := h.devsID()

	if err := h.store.DeleteGroup(context.Background(), id); err != nil {
		tb.Fatal(err)
	}

	h.press(alice, fmt.Sprintf("v1:join:%d:0", id), nil)

	if got := h.lastToast(); got != "This group no longer exists." {
		tb.Fatalf("unexpected toast %q", got)
	}

	if len(h.api.edits) != 0 {
		tb.Fatal("post buttons must not edit the mention post")
	}
}

func TestJoinLeaveButtonsUnderMentionPost(tb *testing.T) {
	h := newHarness(tb)
	alice := h.user(1, "alice", "Alice")
	bob := h.user(2, "bob", "Bob")
	h.group("devs", 1)

	post := h.run(alice, "/m devs")

	kb, ok := post.ReplyMarkup.(*t.InlineKeyboardMarkup)
	if !ok || len(kb.InlineKeyboard) != 1 || len(kb.InlineKeyboard[0]) != 2 {
		tb.Fatalf("mention post needs Join/Leave buttons, got %+v", post.ReplyMarkup)
	}

	join := kb.InlineKeyboard[0][0].CallbackData
	leave := kb.InlineKeyboard[0][1].CallbackData

	h.press(bob, join, nil)
	if !strings.Contains(h.lastToast(), "You have joined") {
		tb.Fatalf("unexpected toast %q", h.lastToast())
	}

	// repeated clicks are idempotent
	h.press(bob, join, nil)
	if !strings.Contains(h.lastToast(), "already a member") {
		tb.Fatalf("unexpected toast %q", h.lastToast())
	}

	h.press(bob, leave, nil)
	if !strings.Contains(h.lastToast(), "You have left") {
		tb.Fatalf("unexpected toast %q", h.lastToast())
	}

	if len(h.api.edits) != 0 {
		tb.Fatal("the mention post must not be edited")
	}
}

func TestPickerMentionPostsAndDeletesPicker(tb *testing.T) {
	h := newHarness(tb)
	alice := h.user(1, "alice", "Alice")
	h.group("devs", 1)

	origin := &t.Message{MessageID: 77, Chat: t.Chat{ID: testChatID}}
	h.press(alice, fmt.Sprintf("v1:m:%d:1", h.devsID()), origin)

	post := h.api.last(tb)
	contains(tb, post, "@alice")

	if post.ReplyParameters == nil || post.ReplyParameters.MessageID != 77 {
		tb.Fatalf("mention should reply to the original command, got %+v", post.ReplyParameters)
	}

	if len(h.api.deleted) != 1 || h.api.deleted[0].MessageID != 100 {
		tb.Fatalf("picker should be deleted, got %+v", h.api.deleted)
	}
}

func TestPickerPaging(tb *testing.T) {
	groups := make([]storage.MentionGroup, 45)
	for i := range groups {
		groups[i] = storage.MentionGroup{ID: uint(i + 1), Name: fmt.Sprintf("g%d", i)}
	}

	count := func(kb *t.InlineKeyboardMarkup) (buttons int, nav []string) {
		for _, row := range kb.InlineKeyboard {
			for _, b := range row {
				if strings.HasPrefix(b.CallbackData, "v1:pg:") {
					nav = append(nav, b.Text)
				} else {
					buttons++
				}
			}
		}

		return buttons, nav
	}

	if n, nav := count(groupPickerKeyboard(i18n.EN, "join", groups, 0, 1)); n != 20 || len(nav) != 1 || nav[0] != "Next ›" {
		tb.Fatalf("first page: %d buttons, nav %v", n, nav)
	}

	if n, nav := count(groupPickerKeyboard(i18n.EN, "join", groups, 1, 1)); n != 20 || len(nav) != 2 {
		tb.Fatalf("middle page: %d buttons, nav %v", n, nav)
	}

	if n, nav := count(groupPickerKeyboard(i18n.EN, "join", groups, 2, 1)); n != 5 || len(nav) != 1 || nav[0] != "‹ Prev" {
		tb.Fatalf("last page: %d buttons, nav %v", n, nav)
	}

	if groupPickerKeyboard(i18n.EN, "join", nil, 0, 1) != nil {
		tb.Fatal("no groups must give no keyboard")
	}
}

func TestPagingCallbackSwitchesPage(tb *testing.T) {
	h := newHarness(tb)
	alice := h.user(1, "alice", "Alice")

	for i := range 25 {
		h.group(fmt.Sprintf("g%02d", i))
	}

	h.press(alice, "v1:pg:m:1:1", nil)

	if len(h.api.markups) != 1 || h.api.markups[0].MessageID != 100 {
		tb.Fatalf("expected one keyboard edit, got %+v", h.api.markups)
	}

	if kb := h.api.markups[0].ReplyMarkup; kb == nil || len(kb.InlineKeyboard) == 0 {
		tb.Fatal("expected a keyboard for page 2")
	}
}
