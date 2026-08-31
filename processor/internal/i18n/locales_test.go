package i18n

import "testing"

// The set of languages the server actually has translations for is known
// regardless of whether an operator has narrowed it with available_languages.
// Exposing it lets the language endpoint validate consistently in both
// configurations (#216).
func TestBundleLocalesListsLoadedLanguagesSorted(t *testing.T) {
	b := NewBundle()
	b.AddTranslator(NewTranslator("fr", map[string]string{"a": "b"}))
	b.AddTranslator(NewTranslator("de", map[string]string{"a": "b"}))
	b.AddTranslator(NewTranslator("en", map[string]string{"a": "b"}))

	got := b.Locales()
	want := []string{"de", "en", "fr"}
	if len(got) != len(want) {
		t.Fatalf("Locales() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Locales() = %v, want sorted %v", got, want)
		}
	}
}

func TestBundleLocalesEmptyBundle(t *testing.T) {
	if got := NewBundle().Locales(); len(got) != 0 {
		t.Errorf("Locales() on an empty bundle = %v, want empty", got)
	}
}
