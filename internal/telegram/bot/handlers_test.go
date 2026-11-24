package bot

import (
	"context"
	"errors"
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
	answers []*t.AnswerCallbackQueryParams
	edits   []*t.EditMessageTextParams
	markups []*t.EditMessageReplyMarkupParams
	deleted []*t.DeleteMessageParams

	reactions   []*t.SetMessageReactionParams
	reactionErr error
	menus       map[string][]t.BotCommand // scope type -> commands registered
	menuSets    int

	// chat member statuses by user ID for GetChatMember; missing means "member"
	statuses  map[int64]string
	adminCall int
}

func (f *fakeAPI) SetMessageReaction(_ context.Context, params *t.SetMessageReactionParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.reactionErr != nil {
		return f.reactionErr
	}

	f.reactions = append(f.reactions, params)

	return nil
}

func (f *fakeAPI) GetMyCommands(_ context.Context, params *t.GetMyCommandsParams) ([]t.BotCommand, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.menus[params.Scope.ScopeType()+"/"+params.LanguageCode], nil
}

func (f *fakeAPI) SetMyCommands(_ context.Context, params *t.SetMyCommandsParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.menus == nil {
		f.menus = make(map[string][]t.BotCommand)
	}

	f.menus[params.Scope.ScopeType()+"/"+params.LanguageCode] = params.Commands
	f.menuSets++

	return nil
}

func (f *fakeAPI) GetChatMember(_ context.Context, params *t.GetChatMemberParams) (t.ChatMember, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.adminCall++

	switch f.statuses[params.UserID] {
	case t.MemberStatusCreator:
		return &t.ChatMemberOwner{Status: t.MemberStatusCreator}, nil
	case t.MemberStatusAdministrator:
		return &t.ChatMemberAdministrator{Status: t.MemberStatusAdministrator}, nil
	default:
		return &t.ChatMemberMember{Status: t.MemberStatusMember}, nil
	}
}

func (f *fakeAPI) AnswerCallbackQuery(_ context.Context, params *t.AnswerCallbackQueryParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.answers = append(f.answers, params)

	return nil
}

func (f *fakeAPI) EditMessageText(_ context.Context, params *t.EditMessageTextParams) (*t.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.edits = append(f.edits, params)

	return &t.Message{MessageID: params.MessageID}, nil
}

func (f *fakeAPI) EditMessageReplyMarkup(_ context.Context, params *t.EditMessageReplyMarkupParams) (*t.Message, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.markups = append(f.markups, params)

	return &t.Message{MessageID: params.MessageID}, nil
}

func (f *fakeAPI) DeleteMessage(_ context.Context, params *t.DeleteMessageParams) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.deleted = append(f.deleted, params)

	return nil
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

	api := &fakeAPI{reactionErr: errors.New("REACTION_INVALID")}

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
		kb, ok := p.ReplyMarkup.(*t.InlineKeyboardMarkup)
		if !ok {
			tb.Fatalf("expected an inline keyboard, got %T", p.ReplyMarkup)
		}

		var texts []string
		for _, row := range kb.InlineKeyboard {
			for _, button := range row {
				texts = append(texts, button.Text)
			}
		}

		return texts
	}

	if got := keyboardTexts(h.run(alice, "/join")); len(got) != 1 || got[0] != "qa" {
		tb.Fatalf("join picker should offer only groups not joined yet, got %v", got)
	}

	if got := keyboardTexts(h.run(alice, "/leave")); len(got) != 1 || got[0] != "devs" {
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
	contains(tb, list, "developers (1 member; aliases: dev)")
	contains(tb, list, "qa (0 members)")

	contains(tb, h.run(alice, "/unalias DEV"), "removed")
	contains(tb, h.run(alice, "/unalias dev"), "not found")
	contains(tb, h.run(alice, "/m dev"), "not found")
}

func ctxBG() context.Context { return context.Background() }

// run2 is run without asserting that a reply exists.
func (h *harness) run2(from *t.User, text string) {
	h.t.Helper()

	name := strings.TrimPrefix(strings.Fields(text)[0], "/")

	for _, cmd := range h.bot.commands() {
		for _, n := range cmd.names {
			if n == name {
				_ = cmd.handler(context.Background(), h.msg(from, text))

				return
			}
		}
	}

	h.t.Fatalf("unknown command %q", name)
}

func TestMentionWithText(tb *testing.T) {
	h := newHarness(tb)
	alice := h.user(1, "alice", "Alice")
	h.group("devs", 1)

	post := h.run(alice, "/m devs please review my_pr (#12)")
	contains(tb, post, "please review my_pr (#12)")
	contains(tb, post, "@alice")

	text := plain(post.Text)
	if strings.Index(text, "please review") > strings.Index(text, "devs: @alice") {
		tb.Fatalf("the note belongs below the header and above the member lines: %q", text)
	}

	// no note, no empty paragraph
	if strings.Contains(plain(h.run(alice, "/m devs").Text), "\n\n\n") {
		tb.Fatal("unexpected blank paragraph")
	}
}

func TestMentionRepliesToTargetMessage(tb *testing.T) {
	h := newHarness(tb)
	alice := h.user(1, "alice", "Alice")
	h.group("devs", 1)

	msg := h.msg(alice, "/m devs")
	msg.ReplyToMessage = &t.Message{MessageID: 7, Chat: msg.Chat}

	for _, cmd := range h.bot.commands() {
		if cmd.names[0] == "mention" {
			_ = cmd.handler(context.Background(), msg)
		}
	}

	if got := h.api.last(tb).ReplyParameters; got == nil || got.MessageID != 7 {
		tb.Fatalf("mention must reply to the replied-to message, got %+v", got)
	}

	// without a reply target it answers the command itself
	if got := h.run(alice, "/m devs").ReplyParameters; got == nil || got.MessageID != 42 {
		tb.Fatalf("expected reply to the command, got %+v", got)
	}

	// free-form mentions answer the user's message
	if err := h.bot.handleFreeFormMessage(context.Background(), h.msg(alice, "ping @devs")); err != nil {
		tb.Fatal(err)
	}

	if got := h.api.last(tb).ReplyParameters; got == nil || got.MessageID != 42 {
		tb.Fatalf("free-form mention must reply to the message, got %+v", got)
	}
}

func TestTopicCreationReplyIsNotAReplyTarget(tb *testing.T) {
	h := newHarness(tb)
	alice := h.user(1, "alice", "Alice")
	h.group("devs", 1)

	msg := h.msg(alice, "/m devs")
	msg.IsTopicMessage = true
	msg.MessageThreadID = 555
	msg.ReplyToMessage = &t.Message{MessageID: 555, ForumTopicCreated: &t.ForumTopicCreated{Name: "x"}}

	for _, cmd := range h.bot.commands() {
		if cmd.names[0] == "mention" {
			_ = cmd.handler(context.Background(), msg)
		}
	}

	post := h.api.last(tb)
	if post.ReplyParameters == nil || post.ReplyParameters.MessageID != 42 {
		tb.Fatalf("must reply to the command, got %+v", post.ReplyParameters)
	}

	if post.MessageThreadID != 555 {
		tb.Fatalf("must stay in topic 555, got %d", post.MessageThreadID)
	}
}

func TestThreadIDOnlyInRealTopics(tb *testing.T) {
	h := newHarness(tb)
	alice := h.user(1, "alice", "Alice")

	// a reply chain in a normal supergroup has a thread ID but is no forum topic
	msg := h.msg(alice, "/list")
	msg.MessageThreadID = 99

	for _, cmd := range h.bot.commands() {
		if cmd.names[0] == "list" {
			_ = cmd.handler(context.Background(), msg)
		}
	}

	if got := h.api.last(tb).MessageThreadID; got != 0 {
		tb.Fatalf("non-topic messages must not get message_thread_id, got %d", got)
	}

	// in a forum topic, every kind of answer carries the thread
	msg.IsTopicMessage = true

	for _, text := range []string{"/list", "/my", "/join", "/help", "/new bad_name"} {
		msg.Text = text
		name := strings.TrimPrefix(strings.Fields(text)[0], "/")

		for _, cmd := range h.bot.commands() {
			if cmd.names[0] == name {
				_ = cmd.handler(context.Background(), msg)
			}
		}

		if got := h.api.last(tb).MessageThreadID; got != 99 {
			tb.Fatalf("%s: expected thread 99, got %d", text, got)
		}
	}
}

func TestLinkPreviewsAreDisabled(tb *testing.T) {
	h := newHarness(tb)
	alice := h.user(1, "alice", "Alice")
	h.group("devs", 1)

	for _, text := range []string{"/list", "/m devs https://example.com"} {
		if lp := h.run(alice, text).LinkPreviewOptions; lp == nil || !lp.IsDisabled {
			tb.Fatalf("%s: link preview must be disabled", text)
		}
	}
}

func TestShowUsesExpandableBlockquoteForLongLists(tb *testing.T) {
	h := newHarness(tb)
	alice := h.user(1, "alice", "Alice")

	ids := []int64{1}
	for i := int64(2); i <= 12; i++ {
		h.user(i, "", "User"+string(rune('A'+i)))
		ids = append(ids, i)
	}

	h.group("big", ids...)
	h.group("small", 1)

	big := h.run(alice, "/show big").Text
	if !strings.Contains(big, "**>") || !strings.HasSuffix(big, "||") || !strings.Contains(plain(big), "(12)") {
		tb.Fatalf("long list must be an expandable blockquote: %q", big)
	}

	if strings.Contains(h.run(alice, "/show small").Text, "**>") {
		tb.Fatal("short lists are plain")
	}
}
