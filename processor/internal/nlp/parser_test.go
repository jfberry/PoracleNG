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
		{"mossy lure nearby", "!lure type:mossy d1000"},
		{"glacial lures", "!lure type:glacial"},
		{"ice lure", "!lure type:glacial"},
		{"rainy lure nearby", "!lure type:rainy d1000"},

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

		// Multi-word pokemon names (fuzzy matching)
		{"track mr mime", `!track "mr. mime"`},
		{"track mr. mime", `!track "mr. mime"`},
		{"track tapu koko", `!track "tapu koko"`},
		{"track ho-oh", "!track ho-oh"},
		{"perfect farfetchd", `!track "farfetch'd" iv100`},
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

func TestParserShortcuts(t *testing.T) {
	p := newTestParser()
	tests := []struct {
		input string
		want  string
	}{
		{"stop", "!stop"},
		{"Stop", "!stop"},
		{"STOP", "!stop"},
		{"stop alerts", "!stop"},
		{"pause", "!stop"},
		{"show me what i'm tracking", "!tracked"},
		{"what am i tracking", "!tracked"},
		{"tracked", "!tracked"},
		{"help", "!help"},
		{"start", "!poracle"},
		{"register", "!poracle"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := p.Parse(tt.input)
			if result.Status != "ok" {
				t.Fatalf("Parse(%q) status=%q error=%q", tt.input, result.Status, result.Error)
			}
			if result.Command != tt.want {
				t.Errorf("Parse(%q) = %q, want %q", tt.input, result.Command, tt.want)
			}
		})
	}
}

// TestParserMultiLeaguePVP — matchPVP should emit one filter per league
// mentioned, matching the bot's own !track pikachu great5 ultra10 support.
// Rank (top N) applies to every league.
func TestParserMultiLeaguePVP(t *testing.T) {
	p := newTestParser()
	tests := []struct {
		input string
		want  string
	}{
		{"pikachu great and ultra league top 10", "!track pikachu great10 ultra10"},
		{"pikachu great league ultra league top 15", "!track pikachu great15 ultra15"},
		{"pikachu gl ul top 20", "!track pikachu great20 ultra20"},
		{"pikachu great ultra little top 25", "!track pikachu great25 ultra25 little25"},
		// single-league behaviour unchanged
		{"pikachu great league top 10", "!track pikachu great10"},
		{"pikachu pvp", "!track pikachu great5"},
		// league without rank defaults to 5
		{"pikachu great and ultra", "!track pikachu great5 ultra5"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := p.Parse(tt.input)
			if result.Status != "ok" {
				t.Fatalf("Parse(%q) status=%q error=%q", tt.input, result.Status, result.Error)
			}
			if result.Command != tt.want {
				t.Errorf("got %q, want %q", result.Command, tt.want)
			}
		})
	}
}

// TestParserShortcutPrefixes — prefix-based shortcuts route natural phrases
// with arguments (locations, area names) into the right Poracle command.
func TestParserShortcutPrefixes(t *testing.T) {
	p := newTestParser()
	tests := []struct {
		input string
		want  string
	}{
		{"set my location to 51.5,-0.1", "!location 51.5,-0.1"},
		{"set location to London", "!location London"},
		{"set my location 52,13", "!location 52,13"},
		{"my location is Berlin", "!location Berlin"},
		{"i am at Hackney", "!location Hackney"},
		{"i'm at Shoreditch", "!location Shoreditch"},
		{"add area london", "!area add london"},
		{"add areas north south", "!area add north south"},
		{"remove area london", "!area remove london"},
		{"delete area manchester", "!area remove manchester"},
		// exact-match area shortcuts
		{"list areas", "!area list"},
		{"my areas", "!area list"},
		{"clear location", "!location"},
		{"remove my location", "!location"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := p.Parse(tt.input)
			if result.Status != "ok" {
				t.Fatalf("Parse(%q) status=%q error=%q", tt.input, result.Status, result.Error)
			}
			if result.Command != tt.want {
				t.Errorf("got %q, want %q", result.Command, tt.want)
			}
		})
	}
}

// TestParserCleanEditPing — quality-of-life flag synonyms survive through
// assemble as raw Poracle filter tokens.
func TestParserCleanEditPing(t *testing.T) {
	p := newTestParser()
	tests := []struct {
		input string
		want  string
	}{
		{"pikachu perfect autodelete", "!track pikachu iv100 clean"},
		{"pikachu perfect auto-delete", "!track pikachu iv100 clean"},
		{"pikachu perfect autoclean", "!track pikachu iv100 clean"},
		{"pikachu perfect editable", "!track pikachu iv100 edit"},
		{"pikachu perfect notify", "!track pikachu iv100 ping"},
		{"pikachu perfect with ping", "!track pikachu iv100 ping"},
		// raw passthrough still works
		{"pikachu iv100 clean edit ping", "!track pikachu iv100 clean edit ping"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := p.Parse(tt.input)
			if result.Status != "ok" {
				t.Fatalf("Parse(%q) status=%q error=%q", tt.input, result.Status, result.Error)
			}
			if result.Command != tt.want {
				t.Errorf("got %q, want %q", result.Command, tt.want)
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
