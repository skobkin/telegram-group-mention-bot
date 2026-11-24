package bot

import (
	"telegram-group-mention-bot/internal/i18n"

	t "github.com/mymmrac/telego"
)

// langOf picks the reply language from the sender's Telegram language_code.
func langOf(user *t.User) i18n.Lang {
	if user == nil {
		return i18n.EN
	}

	return i18n.Pick(user.LanguageCode)
}

// tr is shorthand for i18n.T.
func tr(lang i18n.Lang, key string, args ...any) string {
	return i18n.T(lang, key, args...)
}

// md translates a message and escapes it for MarkdownV2.
func md(lang i18n.Lang, key string, args ...any) string {
	return escapeMarkdownV2(i18n.T(lang, key, args...))
}
