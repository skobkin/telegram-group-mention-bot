package bot

import (
	"os"
	"regexp"
	"strings"
	"testing"

	t "github.com/mymmrac/telego"
)

func russian(u *t.User) *t.User {
	u.LanguageCode = "ru"

	return u
}

func TestRussianUsersGetRussianReplies(tb *testing.T) {
	h := newHarness(tb)
	alice := russian(h.user(1, "alice", "Алиса"))
	h.group("devs")

	contains(tb, h.run(alice, "/join devs"), "Вы вступили в группу «devs»")
	contains(tb, h.run(alice, "/join devs"), "Вы уже состоите")
	contains(tb, h.run(alice, "/join nope"), "Группа не найдена")
	contains(tb, h.run(alice, "/m nope"), "Группа «nope» не найдена")
	contains(tb, h.run(alice, "/leave devs"), "Вы вышли из группы")
	contains(tb, h.run(alice, "/new bad_name"), "Недопустимое имя группы")
	contains(tb, h.run(alice, "/new"), "Использование")
	contains(tb, h.run(alice, "/rename"), "Использование")
	contains(tb, h.run(alice, "/my"), "Вы не состоите ни в одной группе")
	contains(tb, h.run(alice, "/m devs"), "Некого упоминать")
	contains(tb, h.run(alice, "/clear"), "Удалены пустые группы")
}

func TestOthersGetEnglish(tb *testing.T) {
	h := newHarness(tb)
	h.group("devs")

	for _, code := range []string{"", "en", "de", "uk", "fr-FR"} {
		u := h.user(1, "alice", "Alice")
		u.LanguageCode = code

		contains(tb, h.run(u, "/join devs"), "You have joined")
		contains(tb, h.run(u, "/leave devs"), "You have left")
	}
}

func TestRussianHelpAndLists(tb *testing.T) {
	h := newHarness(tb)
	alice := russian(h.user(1, "alice", "Алиса"))
	h.group("devs", 1)
	h.group("qa")

	help := h.run(alice, "/help")
	contains(tb, help, "Доступные команды")
	contains(tb, help, "/clear")

	if strings.Contains(plain(help.Text), "Available commands") {
		tb.Fatal("help must be fully translated")
	}

	list := plain(h.run(alice, "/list").Text)
	for _, want := range []string{"Группы в этом чате", "devs (1 участник)", "qa (0 участников)"} {
		if !strings.Contains(list, want) {
			tb.Fatalf("list %q does not contain %q", list, want)
		}
	}

	show := h.run(alice, "/show devs")
	contains(tb, show, "Участники группы «devs» (1)")
}

func TestRussianPickerAndButtons(tb *testing.T) {
	h := newHarness(tb)
	alice := russian(h.user(1, "alice", "Алиса"))
	h.group("devs", 1)

	picker := h.run(alice, "/m")
	contains(tb, picker, "Выберите группу для упоминания")

	post := h.run(alice, "/m devs")
	kb := post.ReplyMarkup.(*t.InlineKeyboardMarkup)

	if got := kb.InlineKeyboard[0][0].Text; got != "Вступить: devs" {
		tb.Fatalf("unexpected button %q", got)
	}

	// callbacks answer in the presser's language
	bob := russian(h.user(2, "bob", "Боб"))
	h.press(bob, kb.InlineKeyboard[0][0].CallbackData, nil)

	if got := h.lastToast(); !strings.Contains(got, "Вы вступили") {
		tb.Fatalf("unexpected toast %q", got)
	}

	h.press(bob, "v1:nuke:1:1", nil)

	if got := h.lastToast(); got != "Недопустимая кнопка." {
		tb.Fatalf("unexpected toast %q", got)
	}
}

func TestRussianThrottleAndForceDelete(tb *testing.T) {
	h, _ := throttledHarness(tb, 30_000_000_000, 0)
	alice := russian(h.user(1, "alice", "Алиса"))
	h.api.statuses = map[int64]string{1: t.MemberStatusAdministrator}
	h.group("devs", 1)

	h.run2(alice, "/m devs")

	contains(tb, h.run(russian(h.user(2, "bob", "Боб")), "/m devs"), "Подождите 30 с")

	ask := h.run(alice, "/del devs force")
	contains(tb, ask, "вместе с участниками (1 участник)")

	kb := ask.ReplyMarkup.(*t.InlineKeyboardMarkup)
	if kb.InlineKeyboard[0][0].Text != "Удалить" || kb.InlineKeyboard[0][1].Text != "Отмена" {
		tb.Fatalf("unexpected buttons %+v", kb.InlineKeyboard)
	}
}

// User-facing texts must never contain raw Go errors; details go to the log.
func TestNoRawErrorsInUserFacingTexts(tb *testing.T) {
	forbidden := regexp.MustCompile(`(?m)(escapeMarkdownV2|outcome\{text:|answerCallback)\([^)]*%v`)

	for _, file := range []string{"commands.go", "group_operations.go", "admin.go", "callbacks.go", "helpers.go", "owner.go"} {
		src, err := readSource(file)
		if err != nil {
			tb.Fatal(err)
		}

		if forbidden.MatchString(src) {
			tb.Errorf("%s formats an error into a user-facing text", file)
		}
	}

}

func readSource(name string) (string, error) {
	data, err := os.ReadFile(name) //nolint:gosec // fixed file names of this package

	return string(data), err
}
