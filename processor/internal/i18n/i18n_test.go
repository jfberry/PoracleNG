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
			"greeting":     "Hallo",
			"msg.count":    "Du hast {0} Nachrichten",
			"pair":         "{0} von {1}",
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
