package nlp

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/i18n"
)

func testTranslator() *i18n.Translator {
	return i18n.NewTranslator("en", map[string]string{
		// Pokemon
		"poke_25":  "Pikachu",
		"poke_122": "Mr. Mime",
		"poke_83":  "Farfetch'd",
		"poke_250": "Ho-Oh",
		"poke_785": "Tapu Koko",
		"poke_439": "Mime Jr.",
		"poke_474": "Porygon-Z",
		"poke_233": "Porygon2",
		"poke_480": "Uxie",
		"poke_481": "Mesprit",
		"poke_482": "Azelf",
		"poke_1":   "Bulbasaur",

		// Types
		"poke_type_1":  "Normal",
		"poke_type_2":  "Fighting",
		"poke_type_10": "Fire",
		"poke_type_13": "Electric",
		"poke_type_11": "Water",

		// Forms
		"form_46": "Alolan",
		"form_47": "Galarian",
		"form_99": "Shadow",
		"form_50": "Normal", // not in trackable list

		// Items
		"item_1":   "Poke Ball",
		"item_701": "Razz Berry",
		"item_706": "Golden Razz Berry",
		"item_100": "Potion",

		// Moves
		"move_14":  "Hyper Beam",
		"move_13":  "Wrap",
		"move_100": "Shadow Ball",
		"move_200": "Rock Slide",
	})
}

func TestVocabularyPokemonLookup(t *testing.T) {
	tr := testTranslator()
	// Use empty baseDir so alias files won't be found; we test canonical + fuzzy only
	v := BuildVocabularies(tr, "/nonexistent")

	tests := []struct {
		input string
		want  string
	}{
		{"pikachu", "pikachu"},
		{"Pikachu", "pikachu"},
		{"PIKACHU", "pikachu"},
		{"mr. mime", "mr. mime"},
		{"mr mime", "mr. mime"},
		{"mrmime", "mr. mime"},
		{"farfetch'd", "farfetch'd"},
		{"farfetchd", "farfetch'd"},
		{"ho-oh", "ho-oh"},
		{"hooh", "ho-oh"},      // stripped: ho oh → spaces removed: hooh
		{"ho oh", "ho-oh"},     // stripped variant
		{"tapu koko", "tapu koko"},
		{"tapukoko", "tapu koko"},
		{"mime jr.", "mime jr."},
		{"mime jr", "mime jr."},
		{"mimejr", "mime jr."},
		{"porygon-z", "porygon-z"},
		{"porygonz", "porygon-z"},
		{"porygon2", "porygon2"},
		{"bulbasaur", "bulbasaur"},
		{"notapokemon", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := v.Pokemon.Lookup(tt.input)
			if got != tt.want {
				t.Errorf("Pokemon.Lookup(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestVocabularyPokemonAliases(t *testing.T) {
	tr := testTranslator()
	// Use real baseDir to load fallbacks/pokemonAlias.json
	v := BuildVocabularies(tr, "/Users/james/GolandProjects/PoracleNG")

	// Single-ID aliases from pokemonAlias.json
	tests := []struct {
		input string
		want  string
	}{
		{"mrmime", "mr. mime"},
		{"mr mime", "mr. mime"},
		{"hooh", "ho-oh"},
		{"ho oh", "ho-oh"},
		{"mimejr", "mime jr."},
		{"farfetchd", "farfetch'd"},
		{"porygonz", "porygon-z"},
		{"porygon2", "porygon2"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := v.Pokemon.Lookup(tt.input)
			if got != tt.want {
				t.Errorf("Pokemon.Lookup(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}

	// Group alias
	group := v.Pokemon.LookupGroup("laketrio")
	if group == nil {
		t.Fatal("LookupGroup(laketrio) returned nil")
	}
	if len(group) != 3 {
		t.Fatalf("LookupGroup(laketrio) returned %d names, want 3", len(group))
	}
	// Should contain uxie, mesprit, azelf
	nameSet := map[string]bool{}
	for _, n := range group {
		nameSet[n] = true
	}
	for _, want := range []string{"uxie", "mesprit", "azelf"} {
		if !nameSet[want] {
			t.Errorf("LookupGroup(laketrio) missing %q, got %v", want, group)
		}
	}
}

func TestVocabularyTypeLookup(t *testing.T) {
	tr := testTranslator()
	v := BuildVocabularies(tr, "/nonexistent")

	tests := []struct {
		input string
		want  string
	}{
		{"fire", "fire"},
		{"Fire", "fire"},
		{"electric", "electric"},
		{"water", "water"},
		{"normal", "normal"},
		{"fighting", "fighting"},
		{"psychic", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := v.Types.Lookup(tt.input)
			if got != tt.want {
				t.Errorf("Types.Lookup(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestVocabularyFormLookup(t *testing.T) {
	tr := testTranslator()
	v := BuildVocabularies(tr, "/nonexistent")

	tests := []struct {
		input string
		want  string
	}{
		{"alolan", "form:alolan"},
		{"Alolan", "form:alolan"},
		{"galarian", "form:galarian"},
		{"shadow", "form:shadow"},
		{"normal", ""},  // "Normal" form is not in trackableForms
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := v.Forms.Lookup(tt.input)
			if got != tt.want {
				t.Errorf("Forms.Lookup(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestVocabularyItemLookup(t *testing.T) {
	tr := testTranslator()
	v := BuildVocabularies(tr, "/nonexistent")

	tests := []struct {
		input string
		want  string
	}{
		{"potion", "potion"},
		{"razz berry", "razz berry"},
		{"golden razz berry", "golden razz berry"},
		{"Poke Ball", "poke ball"},
		{"unknown item", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := v.Items.Lookup(tt.input)
			if got != tt.want {
				t.Errorf("Items.Lookup(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}

	// Multi-word items sorted longest first
	mw := v.Items.MultiWordNames()
	if len(mw) < 2 {
		t.Fatalf("expected at least 2 multi-word items, got %d", len(mw))
	}
	// "golden razz berry" (17 chars) should come before "razz berry" (10 chars)
	if mw[0] != "golden razz berry" {
		t.Errorf("first multi-word item = %q, want %q", mw[0], "golden razz berry")
	}
}

func TestVocabularyMoveLookup(t *testing.T) {
	tr := testTranslator()
	v := BuildVocabularies(tr, "/nonexistent")

	tests := []struct {
		input string
		want  string
	}{
		{"wrap", "wrap"},
		{"Hyper Beam", "hyper beam"},
		{"shadow ball", "shadow ball"},
		{"rock slide", "rock slide"},
		{"unknown move", ""},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := v.Moves.Lookup(tt.input)
			if got != tt.want {
				t.Errorf("Moves.Lookup(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestVocabularyMultiWordPokemon(t *testing.T) {
	tr := testTranslator()
	v := BuildVocabularies(tr, "/nonexistent")

	mw := v.Pokemon.MultiWordNames()
	if len(mw) == 0 {
		t.Fatal("MultiWordNames() returned empty")
	}

	// Verify sorted longest-first
	for i := 1; i < len(mw); i++ {
		if len(mw[i]) > len(mw[i-1]) {
			t.Errorf("not sorted longest-first: %q (%d) after %q (%d)",
				mw[i], len(mw[i]), mw[i-1], len(mw[i-1]))
		}
	}

	// mr. mime and tapu koko should be in the list (multi-word with space or hyphen)
	nameSet := map[string]bool{}
	for _, n := range mw {
		nameSet[n] = true
	}
	for _, want := range []string{"mr. mime", "tapu koko", "mime jr.", "ho-oh", "porygon-z"} {
		if !nameSet[want] {
			t.Errorf("MultiWordNames() missing %q", want)
		}
	}
}

func TestVocabularyNilTranslator(t *testing.T) {
	// Should not panic with nil translator
	var tr *i18n.Translator
	v := BuildVocabularies(tr, "/nonexistent")
	if got := v.Pokemon.Lookup("pikachu"); got != "" {
		t.Errorf("expected empty from nil translator, got %q", got)
	}
}
