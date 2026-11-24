package bot

import (
	"reflect"
	"strings"
	"testing"

	"telegram-group-mention-bot/internal/storage"
)

func TestIsValidGroupName(t *testing.T) {
	valid := []string{"devs", "qa-team", "a1", "all", "0"}
	invalid := []string{"", "Devs", "with space", "under_score", "dot.name", "юникод", "a/b", "@devs"}

	for _, name := range valid {
		if !isValidGroupName(name) {
			t.Errorf("%q should be valid", name)
		}
	}

	for _, name := range invalid {
		if isValidGroupName(name) {
			t.Errorf("%q should be invalid", name)
		}
	}
}

func TestEscapeMarkdownV2(t *testing.T) {
	cases := map[string]string{
		"plain":           "plain",
		"a_b*c":           `a\_b\*c`,
		"1.5 (x) - [y]!":  `1\.5\ \(x\)\ \-\ \[y\]\!`,
		"unicode ок ✓":    `unicode\ ок\ ✓`,
		"tg://user?id=1":  `tg\:\/\/user\?id\=1`,
		"back\\slash":     `back\\slash`,
		"":                "",
		"Group 'x' done.": `Group\ \'x\'\ done\.`,
	}

	for in, want := range cases {
		if got := escapeMarkdownV2(in); got != want {
			t.Errorf("escapeMarkdownV2(%q) = %q, want %q", in, got, want)
		}
	}
}

func members() []storage.GroupMember {
	return []storage.GroupMember{
		{UserID: 1, User: storage.User{ID: 1, Username: "alice", FirstName: "Alice", LastName: "A"}},
		{UserID: 2, User: storage.User{ID: 2, FirstName: "Bob_the", LastName: "Builder"}},
	}
}

func TestFormatMentions(t *testing.T) {
	b := &Bot{}

	got := b.formatMentions(members())
	want := []string{`@alice`, `[Bob\_the Builder](tg://user?id=2)`}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("formatMentions = %v, want %v", got, want)
	}
}

func TestFormatMentionsEscapesUsername(t *testing.T) {
	b := &Bot{}

	got := b.formatMentions([]storage.GroupMember{{UserID: 3, User: storage.User{ID: 3, Username: "snake_name"}}})
	if got[0] != `@snake\_name` {
		t.Fatalf("underscore in username must be escaped, got %q", got[0])
	}
}

func TestFormatMemberList(t *testing.T) {
	b := &Bot{}

	got := b.formatMemberList(members())
	want := []string{"Alice A (alice)", "Bob_the Builder"}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("formatMemberList = %v, want %v", got, want)
	}
}

func TestGroupNamesFromText(t *testing.T) {
	cases := []struct {
		text string
		want []string
	}{
		{"hello world", nil},
		{"ping @devs please", []string{"devs"}},
		{"@devs, @qa!", []string{"devs", "qa"}},
		{"(@devs)", []string{"devs"}},
		{"mail me at user@example.com", nil},
		{"@Devs", nil},
		{"@", nil},
		{"@qa-team.", []string{"qa-team"}},
		{"text @user_name here", nil},
	}

	for _, tc := range cases {
		if got := groupNamesFromText(tc.text); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("groupNamesFromText(%q) = %v, want %v", tc.text, got, tc.want)
		}
	}
}

func TestHelpListsEveryCommand(t *testing.T) {
	b := &Bot{}
	text := helpText(b.commands())
	seen := map[string]bool{}

	for _, cmd := range b.commands() {
		for _, name := range cmd.names {
			if seen[name] {
				t.Errorf("command name %q registered twice", name)
			}

			seen[name] = true

			if !strings.Contains(text, "/"+name) {
				t.Errorf("/help does not mention /%s", name)
			}
		}

		if cmd.handler == nil {
			t.Errorf("command %v has no handler", cmd.names)
		}
	}
}
