package nlp

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/i18n"
)

func testParserTranslator() *i18n.Translator {
	return i18n.NewTranslator("en", map[string]string{
		// Pokemon
		"poke_25":  "Pikachu",
		"poke_149": "Dragonite",
		"poke_37":  "Vulpix",
		"poke_133": "Eevee",
		"poke_1":   "Bulbasaur",
		"poke_150": "Mewtwo",
		"poke_143": "Snorlax",
		"poke_184": "Azumarill",
		"poke_122": "Mr. Mime",
		"poke_83":  "Farfetch'd",
		"poke_250": "Ho-Oh",
		"poke_785": "Tapu Koko",

		// Types
		"poke_type_1":  "Normal",
		"poke_type_2":  "Fighting",
		"poke_type_3":  "Flying",
		"poke_type_4":  "Poison",
		"poke_type_5":  "Ground",
		"poke_type_6":  "Rock",
		"poke_type_7":  "Bug",
		"poke_type_8":  "Ghost",
		"poke_type_9":  "Steel",
		"poke_type_10": "Fire",
		"poke_type_11": "Water",
		"poke_type_12": "Grass",
		"poke_type_13": "Electric",
		"poke_type_14": "Psychic",
		"poke_type_15": "Ice",
		"poke_type_16": "Dragon",
		"poke_type_17": "Dark",
		"poke_type_18": "Fairy",

		// Forms
		"form_46": "Alolan",
		"form_47": "Galarian",
		"form_99": "Shadow",

		// Items
		"item_1":    "Poke Ball",
		"item_701":  "Razz Berry",
		"item_706":  "Golden Razz Berry",
		"item_1301": "Rare Candy",
		"item_100":  "Potion",

		// Moves
		"move_14":  "Hyper Beam",
		"move_13":  "Wrap",
		"move_100": "Shadow Ball",
		"move_200": "Rock Slide",
		"move_201": "Psystrike",
	})
}

func newTestParser() *Parser {
	tr := testParserTranslator()
	invasionEvents := map[string]bool{
		"kecleon":  true,
		"showcase": true,
	}
	return NewParser(tr, "/nonexistent", invasionEvents)
}

func TestParserEndToEnd(t *testing.T) {
	p := newTestParser()

	tests := []struct {
		input string
		want  string
	}{
		// Basic tracking
		{"track shiny pikachu", "!track pikachu"},
		{"perfect dragonite", "!track dragonite iv100"},
		{"nundo pokemon within 1km", "!track everything iv0 maxiv0 d1000"},
		{"100% pokemon", "!track everything iv100"},
		{"pikachu", "!track pikachu"},
		{"nearby 100%", "!track everything iv100 d1000"},
		{"XXL pokemon", "!track everything size:xxl"},
		{"alolan vulpix", "!track vulpix form:alolan"},
		{"shadow pokemon with good IVs", "!track everything form:shadow iv80"},

		// PVP
		{"good PVP pikachu for great league", "!track pikachu great5"},
		{"rank 1 great league anything", "!track everything great1"},
		{"ultra league rank 1 azumarill", "!track azumarill ultra1"},

		// Raids
		{"level 5 raids", "!raid level5"},
		{"mewtwo raids within 2km", "!raid mewtwo d2000"},
		{"mega raids", "!raid level6"},
		{"mewtwo raids with psystrike", "!raid mewtwo move:psystrike"},

		// Eggs
		{"all raid eggs", "!egg everything"},

		// Invasions
		{"team rocket water female", "!invasion water female"},
		{"kecleon pokestop", "!invasion kecleon"},

		// Quests
		{"stardust quests", "!quest stardust"},
		{"golden razz berry quests", `!quest "golden razz berry"`},
		{"rare candy quests", `!quest "rare candy"`},

		// Lures
		{"mossy lure nearby", "!lure mossy d1000"},

		// Gyms
		{"gym taken by valor", "!gym valor"},

		// Nests
		{"all fire type nests", "!nest fire"},

		// Removal
		{"stop tracking dragonite", "!untrack dragonite"},
		{"remove all raid tracking", "!raid remove everything"},

		// Maxbattle
		{"gigantamax battles", "!maxbattle level7"},

		// Fort
		{"new pokestops added", "!fort new"},

		// Between pattern
		{"pokemon between 95 and 99 IV", "!track everything iv95-99"},

		// Multiple pokemon
		{"track hundos for pikachu eevee and dragonite", "!track pikachu iv100\n!track eevee iv100\n!track dragonite iv100"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := p.Parse(tt.input)
			if result.Status != "ok" {
				t.Fatalf("Parse(%q) status=%q error=%q, want ok", tt.input, result.Status, result.Error)
			}
			if result.Command != tt.want {
				t.Errorf("Parse(%q)\n  got:  %q\n  want: %q", tt.input, result.Command, tt.want)
			}
		})
	}
}

func TestParserEmptyInput(t *testing.T) {
	p := newTestParser()
	result := p.Parse("")
	if result.Status != "error" {
		t.Errorf("expected error for empty input, got %q", result.Status)
	}
}

func TestParserPokemonCount(t *testing.T) {
	p := newTestParser()
	count := p.PokemonCount()
	if count < 12 {
		t.Errorf("PokemonCount() = %d, want >= 12", count)
	}
}
