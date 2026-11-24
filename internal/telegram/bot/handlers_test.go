package bot

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"telegram-group-mention-bot/internal/storage"

	t "github.com/mymmrac/telego"
)

const testChatID int64 = -1001

// fakeAPI records what handlers send.
type fakeAPI struct {
	mu      sync.Mutex
	sent    []*t.SendMessageParams
	actions int
}

func (f *fakeAPI) SendMessage(_ context.Context, params *t.SendMessageParams) (*t.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.sent = append(f.sent, params)

	return &t.Message{MessageID: len(f.sent)}, nil
}

func (f *fakeAPI) SendChatAction(context.Context, *t.SendChatActionParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.actions++

	return nil
}

func (f *fakeAPI) last(tb testing.TB) *t.SendMessageParams {
	tb.Helper()

	f.mu.Lock()
	defer f.mu.Unlock()

	if len(f.sent) == 0 {
		tb.Fatal("no message was sent")
	}

	return f.sent[len(f.sent)-1]
}

type harness struct {
	t     *testing.T
	bot   *Bot
	api   *fakeAPI
	store *storage.Storage
}

func newHarness(tb *testing.T) *harness {
	tb.Helper()

	store, err := storage.New(filepath.Join(tb.TempDir(), "test.sqlite"))
	if err != nil {
		tb.Fatal(err)
	}

	tb.Cleanup(func() { _ = store.Close() })

	api := &fakeAPI{}

	return &harness{
		t:     tb,
		api:   api,
		store: store,
		bot:   &Bot{api: api, storage: store, me: &botInfo{Username: "test_bot"}},
	}
}

func (h *harness) user(id int64, username, first string) *t.User {
	h.t.Helper()

	if _, err := h.store.CreateOrUpdateUser(context.Background(), id, username, first, ""); err != nil {
		h.t.Fatal(err)
	}

	return &t.User{ID: id, Username: username, FirstName: first}
}

func (h *harness) group(name string, members ...int64) {
	h.t.Helper()

	ctx := context.Background()
	if err := h.store.CreateGroup(ctx, name, testChatID); err != nil {
		h.t.Fatal(err)
	}

	group, err := h.store.GetGroup(ctx, name, testChatID)
	if err != nil {
		h.t.Fatal(err)
	}

	for _, id := range members {
		if _, err := h.store.AddMember(ctx, group.ID, &storage.User{ID: id}); err != nil {
			h.t.Fatal(err)
		}
	}
}

func (h *harness) msg(from *t.User, text string) t.Message {
	return t.Message{MessageID: 42, Chat: t.Chat{ID: testChatID, Type: t.ChatTypeSupergroup}, From: from, Text: text}
}

// run dispatches a command through the command table, as registration does.
func (h *harness) run(from *t.User, text string) *t.SendMessageParams {
	h.t.Helper()

	name := strings.TrimPrefix(strings.Fields(text)[0], "/")

	for _, cmd := range h.bot.commands() {
		for _, n := range cmd.names {
			if n == name {
				if err := cmd.handler(context.Background(), h.msg(from, text)); err != nil {
					h.t.Logf("handler returned %v", err)
				}

				return h.api.last(h.t)
			}
		}
	}

	h.t.Fatalf("unknown command %q", name)

	return nil
}

// plain removes MarkdownV2 escaping so assertions read naturally.
func plain(text string) string {
	return strings.ReplaceAll(text, `\`, "")
}

func contains(tb testing.TB, params *t.SendMessageParams, want string) {
	tb.Helper()

	if !strings.Contains(plain(params.Text), want) {
		tb.Fatalf("reply %q does not contain %q", plain(params.Text), want)
	}
}

func TestNewCommand(tb *testing.T) {
	h := newHarness(tb)
	alice := h.user(1, "alice", "Alice")

	contains(tb, h.run(alice, "/new Devs"), "created successfully")

	if _, err := h.store.GetGroup(context.Background(), "devs", testChatID); err != nil {
		tb.Fatalf("group should be stored lowercased: %v", err)
	}

	contains(tb, h.run(alice, "/new DEVS"), "already used")
	contains(tb, h.run(alice, "/new bad_name"), "Invalid group name")
	contains(tb, h.run(alice, "/new"), "Usage")
}

func TestReplyTargetsOriginalMessage(tb *testing.T) {
	h := newHarness(tb)
	reply := h.run(h.user(1, "alice", "Alice"), "/list")

	if reply.ReplyParameters == nil || reply.ReplyParameters.MessageID != 42 {
		tb.Fatalf("reply must reference the command message, got %+v", reply.ReplyParameters)
	}

	if reply.ParseMode != "MarkdownV2" {
		tb.Fatalf("unexpected parse mode %q", reply.ParseMode)
	}
}

func TestJoinAndLeave(tb *testing.T) {
	h := newHarness(tb)
	alice := h.user(1, "alice", "Alice")
	h.group("devs")

	contains(tb, h.run(alice, "/join devs"), "You have joined")
	contains(tb, h.run(alice, "/join devs"), "already a member")
	contains(tb, h.run(alice, "/join nope"), "Group not found")
	contains(tb, h.run(alice, "/leave devs"), "You have left")
	contains(tb, h.run(alice, "/leave devs"), "not a member")
}

func TestPickersOfferTheRightGroups(tb *testing.T) {
	h := newHarness(tb)
	alice := h.user(1, "alice", "Alice")

	contains(tb, h.run(alice, "/join"), "No groups available to join")
	contains(tb, h.run(alice, "/leave"), "No groups available to leave")
	contains(tb, h.run(alice, "/m"), "No groups available to mention")
	contains(tb, h.run(alice, "/del"), "No groups available to delete")
	contains(tb, h.run(alice, "/show"), "No groups available to show")

	h.group("devs", 1)
	h.group("qa")

	keyboardTexts := func(p *t.SendMessageParams) []string {
		kb, ok := p.ReplyMarkup.(*t.ReplyKeyboardMarkup)
		if !ok {
			tb.Fatalf("expected a reply keyboard, got %T", p.ReplyMarkup)
		}

		var texts []string
		for _, row := range kb.Keyboard {
			for _, button := range row {
				texts = append(texts, button.Text)
			}
		}

		return texts
	}

	if got := keyboardTexts(h.run(alice, "/join")); len(got) != 1 || got[0] != "/join qa" {
		tb.Fatalf("join picker should offer only groups not joined yet, got %v", got)
	}

	if got := keyboardTexts(h.run(alice, "/leave")); len(got) != 1 || got[0] != "/leave devs" {
		tb.Fatalf("leave picker should offer only joined groups, got %v", got)
	}

	if got := keyboardTexts(h.run(alice, "/m")); len(got) != 2 {
		tb.Fatalf("mention picker should offer every group, got %v", got)
	}

	contains(tb, h.run(alice, "/join"), "Select a group to join")
}

func TestMentionCommand(tb *testing.T) {
	h := newHarness(tb)
	alice := h.user(1, "alice", "Alice")
	h.user(2, "", "Bob")
	h.group("devs", 1, 2)
	h.group("empty")

	reply := h.run(alice, "/m devs")
	contains(tb, reply, "@alice")
	contains(tb, reply, "tg://user?id=2")

	for _, alias := range []string{"/mention devs", "/call devs"} {
		contains(tb, h.run(alice, alias), "@alice")
	}

	contains(tb, h.run(alice, "/m empty"), "No members to mention")
	contains(tb, h.run(alice, "/m ghosts"), "Group 'ghosts' not found")
}

func TestShowCommandDoesNotPing(tb *testing.T) {
	h := newHarness(tb)
	alice := h.user(1, "alice", "Alice")
	h.group("devs", 1)
	h.group("empty")

	reply := h.run(alice, "/show devs")
	contains(tb, reply, "Members of group")
	contains(tb, reply, "Alice")

	if strings.Contains(plain(reply.Text), "tg://") {
		tb.Fatal("/show must not contain mention links")
	}

	contains(tb, h.run(alice, "/show empty"), "has no members")
}

func TestDeleteCommand(tb *testing.T) {
	h := newHarness(tb)
	alice := h.user(1, "alice", "Alice")
	h.group("busy", 1)
	h.group("empty")

	contains(tb, h.run(alice, "/del busy"), "it has members")
	contains(tb, h.run(alice, "/del empty"), "has been deleted")

	if _, err := h.store.GetGroup(context.Background(), "empty", testChatID); err == nil {
		tb.Fatal("empty group should be gone")
	}
}

func TestListMyAndClear(tb *testing.T) {
	h := newHarness(tb)
	alice := h.user(1, "alice", "Alice")

	contains(tb, h.run(alice, "/list"), "No groups found")
	contains(tb, h.run(alice, "/my"), "haven't joined")
	contains(tb, h.run(alice, "/clear"), "No empty groups")

	h.group("devs", 1)
	h.group("qa")
	h.group("all")

	list := h.run(alice, "/list")
	contains(tb, list, "devs")
	contains(tb, list, "qa")

	my := h.run(alice, "/my")
	contains(tb, my, "devs")

	if strings.Contains(my.Text, "qa") {
		tb.Fatal("/my must list only joined groups")
	}

	cleared := h.run(alice, "/clear")
	contains(tb, cleared, "Removed empty groups")
	contains(tb, cleared, "qa")

	if strings.Contains(cleared.Text, "all") {
		tb.Fatal("/clear must keep the all group")
	}
}

func TestHelpCommand(tb *testing.T) {
	h := newHarness(tb)
	reply := h.run(h.user(1, "alice", "Alice"), "/help")

	contains(tb, reply, "Available commands")
	contains(tb, reply, "/clear")
}

func TestFreeFormMention(tb *testing.T) {
	h := newHarness(tb)
	alice := h.user(1, "alice", "Alice")
	h.group("devs", 1)

	send := func(text string) {
		if err := h.bot.handleFreeFormMessage(context.Background(), h.msg(alice, text)); err != nil {
			tb.Fatal(err)
		}
	}

	send("hello everyone")
	send("ping @ghosts")

	if len(h.api.sent) != 0 {
		tb.Fatalf("no reply expected, got %d", len(h.api.sent))
	}

	send("hey @devs, look!")
	contains(tb, h.api.last(tb), "@alice")
}

func TestCommandsAreCaseInsensitive(tb *testing.T) {
	h := newHarness(tb)
	alice := h.user(1, "alice", "Alice")
	h.group("devs")

	contains(tb, h.run(alice, "/join Devs"), "You have joined")
	contains(tb, h.run(alice, "/m DEVS"), "@alice")
	contains(tb, h.run(alice, "/show dEvS"), "Members of group")

	if err := h.bot.handleFreeFormMessage(context.Background(), h.msg(alice, "ping @DEVS now")); err != nil {
		tb.Fatal(err)
	}

	contains(tb, h.api.last(tb), "@alice")
}

func TestRenameCommand(tb *testing.T) {
	h := newHarness(tb)
	alice := h.user(1, "alice", "Alice")
	h.group("devs", 1)
	h.group("qa")
	h.group("all")

	contains(tb, h.run(alice, "/rename"), "Usage")
	contains(tb, h.run(alice, "/rename devs qa"), "already used")
	contains(tb, h.run(alice, "/rename ghosts x"), "not found")
	contains(tb, h.run(alice, "/rename devs bad_name"), "Invalid group name")
	contains(tb, h.run(alice, "/rename all everyone"), "cannot be renamed")
	contains(tb, h.run(alice, "/rename devs all"), "cannot be renamed")
	contains(tb, h.run(alice, "/rename Devs Engineers"), "renamed to 'engineers'")

	// members are kept
	contains(tb, h.run(alice, "/m engineers"), "@alice")
	contains(tb, h.run(alice, "/m devs"), "not found")
}

func TestAliasCommands(tb *testing.T) {
	h := newHarness(tb)
	alice := h.user(1, "alice", "Alice")
	h.group("developers", 1)
	h.group("qa")

	contains(tb, h.run(alice, "/alias developers"), "Usage")
	contains(tb, h.run(alice, "/alias ghosts g"), "Group 'ghosts' not found")
	contains(tb, h.run(alice, "/alias developers bad_alias"), "Invalid alias")
	contains(tb, h.run(alice, "/alias developers all"), "reserved")
	contains(tb, h.run(alice, "/alias developers qa"), "already used")
	contains(tb, h.run(alice, "/alias Developers Dev"), "Alias 'dev' added")
	contains(tb, h.run(alice, "/alias developers dev"), "already used")
	contains(tb, h.run(alice, "/new dev"), "already used")

	// the alias mentions the group, both as a command and free-form
	contains(tb, h.run(alice, "/m dev"), "@alice")

	if err := h.bot.handleFreeFormMessage(context.Background(), h.msg(alice, "hey @dev")); err != nil {
		tb.Fatal(err)
	}

	contains(tb, h.api.last(tb), "@alice")

	list := h.run(alice, "/list")
	contains(tb, list, "developers (aliases: dev)")

	contains(tb, h.run(alice, "/unalias DEV"), "removed")
	contains(tb, h.run(alice, "/unalias dev"), "not found")
	contains(tb, h.run(alice, "/m dev"), "not found")
}
