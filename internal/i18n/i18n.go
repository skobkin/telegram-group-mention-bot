// Package i18n is a small message catalogue (English and Russian) for the bot's replies.
//
// The language is picked per reply from the sender's Telegram language_code. Message
// templates use fmt verbs; plural forms live under "<key>.one", ".few", ".many" and ".other".
package i18n

import (
	"fmt"
	"sort"
	"strings"
)

// Lang is a supported reply language.
type Lang string

const (
	EN Lang = "en"
	RU Lang = "ru"
)

// Languages lists the supported languages; the first one is the fallback.
var Languages = []Lang{EN, RU}

// Pick chooses the reply language for a Telegram language_code ("ru", "ru-RU", ... → Russian).
func Pick(languageCode string) Lang {
	code := strings.ToLower(strings.TrimSpace(languageCode))
	if code == "ru" || strings.HasPrefix(code, "ru-") || strings.HasPrefix(code, "ru_") {
		return RU
	}

	return EN
}

var catalogues = map[Lang]map[string]string{EN: en, RU: ru}

// T formats the message for key in lang. A key missing in lang falls back to English;
// a key missing everywhere is returned as is, so a gap is visible rather than empty.
func T(lang Lang, key string, args ...any) string {
	template, ok := catalogues[lang][key]
	if !ok {
		template, ok = en[key]
	}

	if !ok {
		return key
	}

	if len(args) == 0 {
		return template
	}

	return fmt.Sprintf(template, args...)
}

// Plural formats the plural form of key for n; the template receives n as its only argument.
func Plural(lang Lang, key string, n int) string {
	category := pluralCategory(lang, n)

	if template, ok := catalogues[lang][key+"."+category]; ok {
		return fmt.Sprintf(template, n)
	}

	return fmt.Sprintf(T(lang, key+".other"), n)
}

// pluralCategory implements the CLDR rules for the supported languages.
func pluralCategory(lang Lang, n int) string {
	if n < 0 {
		n = -n
	}

	switch lang {
	case RU:
		mod10, mod100 := n%10, n%100

		switch {
		case mod10 == 1 && mod100 != 11:
			return "one"
		case mod10 >= 2 && mod10 <= 4 && (mod100 < 12 || mod100 > 14):
			return "few"
		default:
			return "many"
		}
	default:
		if n == 1 {
			return "one"
		}

		return "other"
	}
}

// Keys returns the sorted message keys of a language (used by completeness tests).
func Keys(lang Lang) []string {
	keys := make([]string, 0, len(catalogues[lang]))
	for key := range catalogues[lang] {
		keys = append(keys, key)
	}

	sort.Strings(keys)

	return keys
}

// Template returns the raw template of key in lang.
func Template(lang Lang, key string) (string, bool) {
	template, ok := catalogues[lang][key]

	return template, ok
}
