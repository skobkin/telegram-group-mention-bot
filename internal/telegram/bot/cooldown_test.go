package bot

import (
	"strings"
	"testing"
	"time"

	t "github.com/mymmrac/telego"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time          { return c.now }
func (c *fakeClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

func throttledHarness(tb *testing.T, group, user time.Duration) (*harness, *fakeClock) {
	tb.Helper()

	h := newHarness(tb)
	clock := &fakeClock{now: time.Unix(10_000, 0)}
	h.bot.now = clock.Now
	h.bot.groupCooldown = group
	h.bot.userCooldown = user

	return h, clock
}

func TestQuickRepeatedMentionIsThrottledOnce(tb *testing.T) {
	h, clock := throttledHarness(tb, 30*time.Second, 0)
	alice := h.user(1, "alice", "Alice")
	bob := h.user(2, "bob", "Bob")
	h.group("devs", 1, 2)

	contains(tb, h.run(alice, "/m devs"), "@bob")
	sent := len(h.api.sent)

	// a second quick mention (by someone else) produces one notice, not a ping
	notice := h.run(bob, "/m devs")
	contains(tb, notice, "Please wait 30 s")

	if len(h.api.sent) != sent+1 {
		tb.Fatalf("expected exactly one notice, sent %d new messages", len(h.api.sent)-sent)
	}

	// further throttled attempts in the same window stay silent
	for range 3 {
		if err := h.bot.handleFreeFormMessage(ctxBG(), h.msg(bob, "@devs now!")); err != nil {
			tb.Fatal(err)
		}
	}

	if len(h.api.sent) != sent+1 {
		tb.Fatalf("throttle notice must not repeat, got %d messages", len(h.api.sent)-sent)
	}

	// after the window the group can be mentioned again
	clock.Advance(31 * time.Second)
	contains(tb, h.run(alice, "/m devs"), "@bob")
}

func TestUserCooldownAppliesAcrossGroups(tb *testing.T) {
	h, clock := throttledHarness(tb, 0, 10*time.Second)
	alice := h.user(1, "alice", "Alice")
	h.group("devs", 1)
	h.group("qa", 1)

	contains(tb, h.run(alice, "/m devs"), "@alice")
	contains(tb, h.run(alice, "/m qa"), "Please wait 10 s")

	clock.Advance(4 * time.Second)

	// the notice is not repeated, and the reported wait shrinks only for the first notice
	before := len(h.api.sent)
	h.run2(alice, "/m qa")

	if len(h.api.sent) != before {
		tb.Fatal("no second notice expected")
	}
}

func TestAdminsBypassCooldown(tb *testing.T) {
	h, _ := throttledHarness(tb, 30*time.Second, 30*time.Second)
	admin := h.user(1, "admin", "Admin")
	h.user(2, "bob", "Bob")
	h.api.statuses = map[int64]string{1: t.MemberStatusAdministrator}
	h.group("devs", 1, 2)

	for range 3 {
		contains(tb, h.run(admin, "/m devs"), "@bob")
	}
}

func TestExemptCallerLeavesNoCooldownBehind(tb *testing.T) {
	h, _ := throttledHarness(tb, 30*time.Second, 0)
	admin := h.user(1, "admin", "Admin")
	member := h.user(2, "bob", "Bob")
	h.api.statuses = map[int64]string{1: t.MemberStatusAdministrator}
	h.group("devs", 1, 2)

	// the very first mention (by anyone) starts the window, even an admin's
	contains(tb, h.run(admin, "/m devs"), "@bob")
	contains(tb, h.run(member, "/m devs"), "Please wait")
}

func TestCooldownsDisabledWithZero(tb *testing.T) {
	h, _ := throttledHarness(tb, 0, 0)
	alice := h.user(1, "alice", "Alice")
	h.group("devs", 1)

	for range 5 {
		contains(tb, h.run(alice, "/m devs"), "@alice")
	}
}

func TestEmptyGroupDoesNotStartCooldown(tb *testing.T) {
	h, _ := throttledHarness(tb, 30*time.Second, 30*time.Second)
	alice := h.user(1, "alice", "Alice")
	h.group("empty")
	h.group("devs", 1)

	contains(tb, h.run(alice, "/m empty"), "No members to mention")
	contains(tb, h.run(alice, "/m devs"), "@alice")
}

func TestMemberCap(tb *testing.T) {
	h := newHarness(tb)
	h.bot.maxMentions = 2
	alice := h.user(1, "alice", "Alice")
	h.user(2, "bob", "Bob")
	h.user(3, "carol", "Carol")
	h.user(4, "dave", "Dave")
	h.group("devs", 1, 2, 3, 4)

	post := plain(h.run(alice, "/m devs").Text)

	if got := strings.Count(post, "@"); got != 2 {
		tb.Fatalf("expected 2 pinged members, got %d in %q", got, post)
	}

	if !strings.Contains(post, "2 more not pinged (limit: 2 per post)") {
		tb.Fatalf("missing skipped note in %q", post)
	}
}

func TestCooldownTrackerBoundsMemory(tb *testing.T) {
	var c cooldownTracker

	now := time.Unix(1000, 0)

	for i := range 2000 {
		c.reserve(now, []cooldownRule{{key: string(rune('a' + i%26)), window: time.Second}}, nil)
	}

	later := now.Add(time.Hour)
	for i := range 1100 {
		c.reserve(later, []cooldownRule{{key: "k" + time.Duration(i).String(), window: time.Hour}}, nil)
	}

	c.reserve(later.Add(2*time.Hour), []cooldownRule{{key: "x", window: time.Second}}, nil)

	if len(c.entries) > 3 {
		tb.Fatalf("expired entries must be pruned, got %d", len(c.entries))
	}
}
