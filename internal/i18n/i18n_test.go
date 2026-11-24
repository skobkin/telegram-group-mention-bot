package i18n

import (
	"regexp"
	"strings"
	"testing"
)

func TestPick(t *testing.T) {
	for code, want := range map[string]Lang{"": EN, "en": EN, "en-US": EN, "ru": RU, "RU": RU, "ru-RU": RU, "ru_UA": RU, "uk": EN, "rus": EN} {
		if got := Pick(code); got != want {
			t.Errorf("Pick(%q) = %q, want %q", code, got, want)
		}
	}
}

// Every English key must exist in Russian, and vice versa, so a new message cannot ship untranslated.
func TestCataloguesAreComplete(t *testing.T) {
	// plural keys: Russian has one/few/many, English has one/other; compare by base key
	base := func(key string) string {
		for _, suffix := range []string{".one", ".few", ".many", ".other"} {
			if strings.HasSuffix(key, suffix) {
				return strings.TrimSuffix(key, suffix)
			}
		}

		return key
	}

	set := func(lang Lang) map[string]bool {
		keys := map[string]bool{}
		for _, key := range Keys(lang) {
			keys[base(key)] = true
		}

		return keys
	}

	enKeys, ruKeys := set(EN), set(RU)

	for key := range enKeys {
		if !ruKeys[key] {
			t.Errorf("key %q is missing in ru", key)
		}
	}

	for key := range ruKeys {
		if !enKeys[key] {
			t.Errorf("key %q is missing in en", key)
		}
	}

	// Russian needs all three plural forms of every English plural key
	for _, key := range Keys(EN) {
		if strings.HasSuffix(key, ".one") {
			for _, suffix := range []string{".one", ".few", ".many"} {
				if _, ok := Template(RU, strings.TrimSuffix(key, ".one")+suffix); !ok {
					t.Errorf("ru lacks %s%s", strings.TrimSuffix(key, ".one"), suffix)
				}
			}
		}
	}
}

// Translations must use the same fmt verbs as the English template, or formatting would break.
func TestTemplatesUseSameVerbs(t *testing.T) {
	verbs := regexp.MustCompile(`%[sdv]`)

	for _, key := range Keys(EN) {
		enTemplate, _ := Template(EN, key)
		ruTemplate, ok := Template(RU, key)

		if !ok {
			continue
		}

		if got, want := len(verbs.FindAllString(ruTemplate, -1)), len(verbs.FindAllString(enTemplate, -1)); got != want {
			t.Errorf("%s: ru has %d verbs, en has %d", key, got, want)
		}
	}
}

func TestFallbackAndMissingKey(t *testing.T) {
	if got := T(Lang("de"), "cb.invalid"); got != "Invalid button." {
		t.Errorf("unknown language must fall back to English, got %q", got)
	}

	if got := T(EN, "no.such.key"); got != "no.such.key" {
		t.Errorf("missing keys are returned as is, got %q", got)
	}
}

func TestPluralRussian(t *testing.T) {
	want := map[int]string{
		0: "0 участников", 1: "1 участник", 2: "2 участника", 4: "4 участника", 5: "5 участников",
		11: "11 участников", 12: "12 участников", 14: "14 участников", 21: "21 участник", 22: "22 участника",
		25: "25 участников", 101: "101 участник", 111: "111 участников",
	}

	for n, text := range want {
		if got := Plural(RU, "members", n); got != text {
			t.Errorf("Plural(ru, %d) = %q, want %q", n, got, text)
		}
	}
}

func TestPluralEnglish(t *testing.T) {
	for n, want := range map[int]string{0: "0 members", 1: "1 member", 2: "2 members", 11: "11 members"} {
		if got := Plural(EN, "members", n); got != want {
			t.Errorf("Plural(en, %d) = %q, want %q", n, got, want)
		}
	}
}

func TestFormatting(t *testing.T) {
	if got := T(RU, "join.done", "devs"); got != "Вы вступили в группу «devs»!" {
		t.Errorf("got %q", got)
	}

}
