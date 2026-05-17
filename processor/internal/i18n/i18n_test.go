package i18n

import (
	"strings"
	"testing"
)

func TestFormat(t *testing.T) {
	tests := []struct {
		tmpl string
		args []any
		want string
	}{
		{"Hello", nil, "Hello"},
		{"{0} messages", []any{42}, "42 messages"},
		{"{0} of {1}", []any{"a", "b"}, "a of b"},
		{"no placeholders", []any{"ignored"}, "no placeholders"},
		{"{0} and {0}", []any{"x"}, "x and x"},
		{"{5} out of range", []any{"a"}, "{5} out of range"},
	}
	for _, tt := range tests {
		got := Format(tt.tmpl, tt.args...)
		if got != tt.want {
			t.Errorf("Format(%q, %v) = %q, want %q", tt.tmpl, tt.args, got, tt.want)
		}
	}
}

func TestTranslator(t *testing.T) {
	tr := &Translator{
		lang: "de",
		messages: map[string]string{
			"greeting":  "Hallo",
			"msg.count": "Du hast {0} Nachrichten",
			"pair":      "{0} von {1}",
		},
	}

	if got := tr.T("greeting"); got != "Hallo" {
		t.Errorf("T(greeting) = %q", got)
	}
	if got := tr.T("missing.key"); got != "missing.key" {
		t.Errorf("T(missing) = %q, want key as fallback", got)
	}
	if got := tr.Tf("msg.count", 5); got != "Du hast 5 Nachrichten" {
		t.Errorf("Tf = %q", got)
	}
	if got := tr.Tf("pair", "a", "b"); got != "a von b" {
		t.Errorf("Tf multi = %q", got)
	}
}

func TestNilTranslator(t *testing.T) {
	var tr *Translator
	if got := tr.T("some.key"); got != "some.key" {
		t.Errorf("nil.T = %q, want key fallback", got)
	}
	if got := tr.Lang(); got != "en" {
		t.Errorf("nil.Lang() = %q, want en", got)
	}
}

func TestBundleFallbackToEnglish(t *testing.T) {
	b := NewBundle()
	b.merge("en", map[string]string{"greeting": "Hello"})

	// Unknown locale falls back to English
	tr := b.For("xx")
	if tr.Lang() != "en" {
		t.Errorf("unknown locale should fall back to en, got %s", tr.Lang())
	}
	if got := tr.T("greeting"); got != "Hello" {
		t.Errorf("en fallback = %q, want Hello", got)
	}
}

func TestBundleEmptyLocaleFallsBackToEnglish(t *testing.T) {
	b := NewBundle()
	b.merge("en", map[string]string{"greeting": "Hello"})

	tr := b.For("")
	if got := tr.T("greeting"); got != "Hello" {
		t.Errorf("empty locale should use en, got %q", got)
	}
}

func TestBundleBaseLanguageFallback(t *testing.T) {
	b := NewBundle()
	b.merge("pt", map[string]string{"greeting": "Olá"})
	b.merge("en", map[string]string{"greeting": "Hello"})

	// "pt-br" should fall back to "pt"
	tr := b.For("pt-br")
	if tr.Lang() != "pt" {
		t.Errorf("pt-br should fall back to pt, got %s", tr.Lang())
	}
	if got := tr.T("greeting"); got != "Olá" {
		t.Errorf("pt fallback = %q", got)
	}
}

func TestPerKeyFallbackToEnglish(t *testing.T) {
	b := NewBundle()
	b.merge("en", map[string]string{"poke_25": "Pikachu", "greeting": "Hello"})
	b.merge("sv", map[string]string{"greeting": "Hej"})
	b.LinkFallbacks()

	tr := b.For("sv")
	// Key present in sv → use sv
	if got := tr.T("greeting"); got != "Hej" {
		t.Errorf("sv greeting = %q, want Hej", got)
	}
	// Key missing in sv → fall back to English
	if got := tr.T("poke_25"); got != "Pikachu" {
		t.Errorf("sv poke_25 = %q, want Pikachu (en fallback)", got)
	}
}

func TestMerge(t *testing.T) {
	b := NewBundle()
	b.merge("de", map[string]string{"a": "1", "b": "2"})
	b.merge("de", map[string]string{"b": "override", "c": "3"})

	tr := b.For("de")
	if tr.T("a") != "1" {
		t.Error("a should be preserved")
	}
	if tr.T("b") != "override" {
		t.Error("b should be overridden")
	}
	if tr.T("c") != "3" {
		t.Error("c should be added")
	}
}

func TestEmbeddedLocales(t *testing.T) {
	b := Load("")

	// English should be loaded from en.json
	en := b.For("en")
	got := en.Tf("rate_limit.reached", 20, 240)
	if got != "You have reached the limit of 20 messages over 240 seconds" {
		t.Errorf("en rate_limit.reached = %q", got)
	}

	// German should be translated
	de := b.For("de")
	if de.Lang() != "de" {
		t.Fatalf("expected de translator, got %s", de.Lang())
	}
	got = de.Tf("rate_limit.reached", 20, 240)
	if !strings.Contains(got, "20") || !strings.Contains(got, "240") {
		t.Errorf("de rate_limit.reached = %q, expected formatted German string", got)
	}
	if strings.Contains(got, "You have") {
		t.Errorf("de message should be translated, got English: %q", got)
	}
}

func TestEmbeddedAllLanguages(t *testing.T) {
	b := Load("")
	enText := b.For("en").T("rate_limit.reached")

	for _, lang := range []string{"de", "fr", "it", "nb-no", "ru"} {
		tr := b.For(lang)
		if tr.Lang() != lang {
			t.Errorf("expected %s translator, got %s", lang, tr.Lang())
		}
		got := tr.T("rate_limit.reached")
		if got == "rate_limit.reached" {
			t.Errorf("%s: rate_limit.reached returned raw key", lang)
		}
		if got == enText {
			t.Errorf("%s: rate_limit.reached returned English text, should be translated", lang)
		}
	}
}

// TestZhCNPokemonNames verifies that the Simplified Chinese Pokemon names
// seeded into processor/internal/i18n/locale/zh-cn.json are reachable via
// the normal Bundle lookup path — the same path enrichment uses to resolve
// {{name}} from the poke_{id} identifier. Catches regressions if the file
// gets corrupted, overwritten by a Crowdin sync that drops keys, or if the
// embed directive ever stops picking up zh-cn.json.
func TestZhCNPokemonNames(t *testing.T) {
	b := Load("")
	tr := b.For("zh-cn")
	if tr.Lang() != "zh-cn" {
		t.Fatalf("expected zh-cn translator, got %s", tr.Lang())
	}

	// Sample a handful across the dex and verify Simplified Chinese
	// characters (not Traditional, not English, not raw key).
	cases := map[string]string{
		"poke_1":   "妙蛙种子", // Bulbasaur — differs from zh-tw's 妙蛙種子
		"poke_6":   "喷火龙",  // Charizard — differs from zh-tw's 噴火龍
		"poke_25":  "皮卡丘",  // Pikachu — same in Simplified and Traditional
		"poke_150": "超梦",   // Mewtwo — differs from zh-tw's 超夢
		"poke_445": "烈咬陆鲨", // Garchomp — differs from zh-tw's 烈咬陸鯊
	}
	for key, want := range cases {
		got := tr.T(key)
		if got != want {
			t.Errorf("zh-cn %s = %q, want %q", key, got, want)
		}
	}

	// Nidoran was split into ♀/♂ in modern masterfiles; both IDs should
	// resolve to the user's single original translation.
	for _, id := range []string{"poke_29", "poke_32"} {
		if got := tr.T(id); got != "尼多兰" {
			t.Errorf("zh-cn %s = %q, want 尼多兰", id, got)
		}
	}

	// A UI key should still work alongside the new Pokemon names.
	if got := tr.T("weather.unknown"); got != "未知" {
		t.Errorf("zh-cn weather.unknown = %q, want 未知", got)
	}
}

func TestAllKeysPresent(t *testing.T) {
	b := Load("")
	keys := []string{
		"rate_limit.reached",
		"rate_limit.banned_soft",
		"rate_limit.banned_hard",
		"rate_limit.shame",
	}
	for _, lang := range []string{"en", "de", "fr", "it", "nb-no", "ru"} {
		tr := b.For(lang)
		for _, key := range keys {
			got := tr.T(key)
			if got == key {
				t.Errorf("%s: missing translation for %q", lang, key)
			}
		}
	}
}

// TestPoracleAdminI18nParity verifies that every cmd.poracle_admin.* and
// cmd.pa key present in the English bundle is also present in the German
// bundle. German is checked as the reference non-English locale for the
// skeleton. Other locales may lag until later tasks add them.
func TestPoracleAdminI18nParity(t *testing.T) {
	b := Load("")
	en := b.For("en")
	de := b.For("de")

	// Collect all cmd.poracle_admin.* and cmd.pa keys from English.
	// We test by attempting to resolve them in de — if de returns the raw key
	// it means the translation is missing (de falls back to en for missing keys,
	// so a raw-key result means both en AND de are missing, which shouldn't
	// happen for keys we've deliberately added).
	//
	// The reliable test is: does de have its OWN value (not delegated to en)?
	// We compare de.T(key) with en.T(key): if they're the same AND the key
	// isn't one of the English-only name keys (cmd.poracle_admin, cmd.pa),
	// that's a missing German translation.
	englishOnlyKeys := map[string]bool{
		"cmd.poracle_admin": true, // command name — intentionally English in all locales
		"cmd.pa":            true, // alias — intentionally English in all locales
	}

	keysToCheck := []string{
		"cmd.poracle_admin",
		"cmd.pa",
		"cmd.poracle_admin.desc",
		"cmd.poracle_admin.not_admin",
		"cmd.poracle_admin.unknown_group",
		"cmd.poracle_admin.unknown_sub",
		"cmd.poracle_admin.help.admin_only",
		"cmd.poracle_admin.help.groups",
		"cmd.poracle_admin.help_intro",
		"cmd.poracle_admin.stub",
		"cmd.poracle_admin.group.slash.desc",
		"cmd.poracle_admin.group.reload.desc",
		"cmd.poracle_admin.group.emoji.desc",
		"cmd.poracle_admin.group.reconcile.desc",
		"cmd.poracle_admin.group.cache.desc",
		"cmd.poracle_admin.group.ratelimit.desc",
		"cmd.poracle_admin.group.summary.desc",
		"cmd.poracle_admin.group.status.desc",
		"cmd.poracle_admin.group.maintenance.desc",
		"cmd.poracle_admin.slash.help_stub",
		"cmd.poracle_admin.reload.help_stub",
		"cmd.poracle_admin.emoji.help_stub",
		"cmd.poracle_admin.reconcile.help_stub",
		"cmd.poracle_admin.cache.help_stub",
		"cmd.poracle_admin.ratelimit.help_stub",
		"cmd.poracle_admin.summary.help_stub",
		"cmd.poracle_admin.status.help_stub",
		"cmd.poracle_admin.maintenance.help_stub",
	}

	for _, key := range keysToCheck {
		enVal := en.T(key)
		if enVal == key {
			t.Errorf("en: key %q not found in English bundle — add it to en.json", key)
			continue
		}

		deVal := de.T(key)
		if deVal == key {
			t.Errorf("de: key %q not found (returned raw key)", key)
			continue
		}

		if !englishOnlyKeys[key] && deVal == enVal {
			t.Errorf("de: key %q appears to be missing a German translation (de returned same value as en: %q)", key, enVal)
		}
	}
}
