package bot

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	t "github.com/mymmrac/telego"
)

func TestJoinLeaveNewDelAreAcknowledgedWithReaction(tb *testing.T) {
	h := newHarness(tb)
	h.api.reactionErr = nil
	alice := h.user(1, "alice", "Alice")

	h.run2(alice, "/new devs")
	h.run2(alice, "/join devs")
	h.run2(alice, "/leave devs")
	h.run2(alice, "/del devs")

	if len(h.api.reactions) != 4 {
		tb.Fatalf("expected 4 reactions, got %d", len(h.api.reactions))
	}

	for _, r := range h.api.reactions {
		if r.MessageID != 42 || len(r.Reaction) != 1 {
			tb.Fatalf("reaction must target the command message: %+v", r)
		}
	}

	if len(h.api.sent) != 0 {
		tb.Fatalf("no text replies expected on success, got %d", len(h.api.sent))
	}
}

func TestErrorsAndInfoStayText(tb *testing.T) {
	h := newHarness(tb)
	h.api.reactionErr = nil
	alice := h.user(1, "alice", "Alice")
	h.group("devs", 1)

	contains(tb, h.run(alice, "/join devs"), "already a member")
	contains(tb, h.run(alice, "/join nope"), "Group not found")
	contains(tb, h.run(alice, "/new bad_name"), "Invalid group name")
	contains(tb, h.run(alice, "/list"), "devs")

	if len(h.api.reactions) != 0 {
		tb.Fatal("only successful changes are acknowledged with reactions")
	}
}

func TestReactionFailureFallsBackToText(tb *testing.T) {
	h := newHarness(tb) // reactions fail by default in the harness
	alice := h.user(1, "alice", "Alice")
	h.group("devs")

	contains(tb, h.run(alice, "/join devs"), "You have joined")
}

func TestAutoDeleteScheduling(tb *testing.T) {
	h := newHarness(tb)
	alice := h.user(1, "alice", "Alice")
	h.group("devs", 1)

	var scheduled []func()

	var delay time.Duration

	h.bot.autoDelete = 30 * time.Second
	h.bot.afterFunc = func(d time.Duration, f func()) { delay = d; scheduled = append(scheduled, f) }

	h.run2(alice, "/list")

	if len(scheduled) != 1 || delay != 30*time.Second {
		tb.Fatalf("service reply must be scheduled for deletion, got %d at %v", len(scheduled), delay)
	}

	scheduled[0]()

	// the reply (fake ID 1) and the command (42) are deleted
	var ids []int
	for _, d := range h.api.deleted {
		ids = append(ids, d.MessageID)
	}

	slices.Sort(ids)

	if !slices.Equal(ids, []int{1, 42}) {
		tb.Fatalf("expected reply and command to be deleted, got %v", ids)
	}

	// mention posts are never deleted
	scheduled = nil

	h.run2(alice, "/m devs")

	if len(scheduled) != 0 {
		tb.Fatal("mention posts must not be auto-deleted")
	}
}

func TestAutoDeleteOffByDefault(tb *testing.T) {
	h := newHarness(tb)
	alice := h.user(1, "alice", "Alice")

	h.bot.afterFunc = func(time.Duration, func()) { tb.Fatal("nothing may be scheduled when AUTO_DELETE_SECONDS=0") }

	h.run2(alice, "/list")
	h.run2(alice, "/new devs")
}

func TestAutoDeleteFailureIsIgnored(tb *testing.T) {
	h := newHarness(tb)
	h.bot.autoDelete = time.Second

	var scheduled []func()

	h.bot.afterFunc = func(_ time.Duration, f func()) { scheduled = append(scheduled, f) }

	h.run2(h.user(1, "alice", "Alice"), "/list")
	scheduled[0]() // DeleteMessage in the fake never fails; the point is that nothing panics or blocks
}

func TestCommandMenu(tb *testing.T) {
	h := newHarness(tb)
	h.bot.owners = map[int64]struct{}{1: {}}

	h.bot.syncCommandMenu(ctxBG())

	for _, scope := range []string{t.ScopeTypeAllPrivateChats, t.ScopeTypeAllGroupChats} {
		var names []string
		for _, c := range h.api.menus[scope+"/"] {
			names = append(names, c.Command)

			if c.Description == "" || len(c.Description) > 256 {
				tb.Fatalf("bad description for /%s", c.Command)
			}
		}

		for _, want := range []string{"new", "join", "leave", "mention", "call", "show", "del", "list", "my", "clear", "help"} {
			if !slices.Contains(names, want) {
				tb.Fatalf("scope %s misses /%s: %v", scope, want, names)
			}
		}

		if slices.Contains(names, "stats") || slices.Contains(names, "m") {
			tb.Fatalf("hidden or alias commands must not be in the menu: %v", names)
		}
	}

	// Russian descriptions are registered with their own language code
	ruMenu := h.api.menus[t.ScopeTypeAllGroupChats+"/ru"]
	if len(ruMenu) == 0 || ruMenu[0].Description == h.api.menus[t.ScopeTypeAllGroupChats+"/"][0].Description {
		tb.Fatalf("expected a translated menu for ru, got %v", ruMenu)
	}

	// unchanged list: nothing is set again
	sets := h.api.menuSets
	h.bot.syncCommandMenu(ctxBG())

	if h.api.menuSets != sets {
		tb.Fatal("menu must only be re-registered when the list changed")
	}
}

func TestMenuFailureDoesNotPanic(tb *testing.T) {
	h := newHarness(tb)
	h.bot.api = failingMenuAPI{h.api}

	h.bot.syncCommandMenu(ctxBG())
}

type failingMenuAPI struct{ *fakeAPI }

func (failingMenuAPI) SetMyCommands(_ context.Context, _ *t.SetMyCommandsParams) error {
	return errors.New("boom")
}

func TestTruncate(tb *testing.T) {
	if got := truncate(strings.Repeat("я", 300), 256); len([]rune(got)) != 256 {
		tb.Fatalf("got %d runes", len([]rune(got)))
	}
}
