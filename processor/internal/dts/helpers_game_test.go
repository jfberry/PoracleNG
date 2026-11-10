package dts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	raymond "github.com/mailgun/raymond/v2"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// testBundle creates a Bundle with en and de translations for testing.
func testBundle() *i18n.Bundle {
	b := i18n.NewBundle()
	// English translations
	enT := i18n.NewTranslator("en", map[string]string{
		"poke_1":       "Bulbasaur",
		"poke_25":      "Pikachu",
		"poke_150":     "Mewtwo",
		"form_0":       "Normal",
		"form_46":      "Alolan",
		"form_65":      "Galarian",
		"move_14":      "Hyper Beam",
		"move_281":     "Razor Leaf",
		"poke_type_1":  "Normal",
		"poke_type_4":  "Poison",
		"poke_type_12": "Grass",
		"poke_type_14": "Psychic",
		"greeting":     "Hello",
	})
	// German translations
	deT := i18n.NewTranslator("de", map[string]string{
		"poke_1":       "Bisasam",
		"poke_25":      "Pikachu",
		"form_46":      "Alola",
		"form_65":      "Galar",
		"move_14":      "Hyperstrahl",
		"poke_type_12": "Pflanze",
		"poke_type_14": "Psycho",
		"greeting":     "Hallo",
	})
	b.AddTranslator(enT)
	b.AddTranslator(deT)
	return b
}

// testGameData creates minimal GameData for testing.
func testGameData() *gamedata.GameData {
	return &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 1, Form: 0}: {
				PokemonID: 1, FormID: 0,
				Types: []int{12, 4}, Attack: 118, Defense: 111, Stamina: 128,
				Evolutions: []gamedata.Evolution{{PokemonID: 2, FormID: 0}},
			},
			{ID: 25, Form: 0}: {
				PokemonID: 25, FormID: 0,
				Types: []int{13}, Attack: 112, Defense: 96, Stamina: 111,
			},
			{ID: 150, Form: 0}: {
				PokemonID: 150, FormID: 0,
				Types: []int{14}, Attack: 300, Defense: 182, Stamina: 214,
			},
		},
		Moves: map[int]*gamedata.Move{
			14:  {MoveID: 14, TypeID: 1, Fast: false},
			281: {MoveID: 281, TypeID: 12, Fast: true},
		},
		Types: map[int]*gamedata.TypeInfo{
			1:  {TypeID: 1, Emoji: "type_normal"},
			4:  {TypeID: 4, Emoji: "type_poison"},
			12: {TypeID: 12, Emoji: "type_grass"},
			13: {TypeID: 13, Emoji: "type_electric"},
			14: {TypeID: 14, Emoji: "type_psychic"},
		},
		Items:   make(map[int]*gamedata.Item),
		Grunts:  make(map[int]*gamedata.Grunt),
		Weather: make(map[int]*gamedata.WeatherData),
		Util: &gamedata.UtilData{
			PowerUpCost: map[string]gamedata.PowerUpCostEntry{
				"1":    {Stardust: 200, Candy: 1},
				"1.5":  {Stardust: 200, Candy: 1},
				"2":    {Stardust: 200, Candy: 1},
				"39":   {Stardust: 10000, Candy: 15},
				"39.5": {Stardust: 10000, Candy: 15},
				"40":   {Stardust: 10000, XLCandy: 10},
				"40.5": {Stardust: 10000, XLCandy: 10},
			},
			CpMultipliers: map[string]float64{
				"1": 0.0939999967813492, "1.5": 0.135137432089339,
				"20": 0.597400009632111, "40": 0.790300011634827,
				"51": 0.845300018787384,
			},
		},
	}
}

// testEmoji creates an EmojiLookup for testing.
func testEmoji() *EmojiLookup {
	return &EmojiLookup{
		defaults: map[string]string{
			"type_grass":    "🌿",
			"type_poison":   "☠️",
			"type_normal":   "⚪",
			"type_psychic":  "🔮",
			"type_electric": "⚡",
		},
		custom: map[string]map[string]string{
			"telegram": {
				"type_grass": "🍃",
			},
		},
	}
}

func init() {
	// Register generic helpers (already done in helpers_test.go init)
	// Register game helpers with test data — sync.Once ensures single registration.
	RegisterGameHelpers(testGameData(), testBundle(), testEmoji(), "")
}

// renderWithData renders a template with a context and private data frame.
func renderWithData(t *testing.T, source string, ctx any, data map[string]any) string {
	t.Helper()
	tpl, err := raymond.Parse(source)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	df := raymond.NewDataFrame()
	for k, v := range data {
		df.Set(k, v)
	}
	result, err := tpl.ExecWith(ctx, df)
	if err != nil {
		t.Fatalf("exec failed: %v", err)
	}
	return result
}

// ---------------------------------------------------------------------------
// Pokemon name helpers
// ---------------------------------------------------------------------------

func TestPokemonName(t *testing.T) {
	ctx := map[string]any{"id": 1}
	data := map[string]any{"language": "en"}

	got := renderWithData(t, `{{pokemonName id}}`, ctx, data)
	if got != "Bulbasaur" {
		t.Errorf("pokemonName en: got %q, want %q", got, "Bulbasaur")
	}

	// German
	data["language"] = "de"
	got = renderWithData(t, `{{pokemonName id}}`, ctx, data)
	if got != "Bisasam" {
		t.Errorf("pokemonName de: got %q, want %q", got, "Bisasam")
	}
}

func TestPokemonNameUnknown(t *testing.T) {
	ctx := map[string]any{"id": 99999}
	data := map[string]any{"language": "en"}

	got := renderWithData(t, `{{pokemonName id}}`, ctx, data)
	if got != "99999" {
		t.Errorf("pokemonName unknown: got %q, want %q", got, "99999")
	}
}

func TestPokemonNameEng(t *testing.T) {
	ctx := map[string]any{"id": 1}
	got := renderWithData(t, `{{pokemonNameEng id}}`, ctx, nil)
	if got != "Bulbasaur" {
		t.Errorf("pokemonNameEng: got %q, want %q", got, "Bulbasaur")
	}
}

func TestPokemonNameAlt(t *testing.T) {
	ctx := map[string]any{"id": 1}
	data := map[string]any{"altLanguage": "de"}

	got := renderWithData(t, `{{pokemonNameAlt id}}`, ctx, data)
	if got != "Bisasam" {
		t.Errorf("pokemonNameAlt de: got %q, want %q", got, "Bisasam")
	}
}

func TestPokemonNameAltFallback(t *testing.T) {
	ctx := map[string]any{"id": 1}
	// No altLanguage set → should fall back to "en"
	got := renderWithData(t, `{{pokemonNameAlt id}}`, ctx, nil)
	if got != "Bulbasaur" {
		t.Errorf("pokemonNameAlt fallback: got %q, want %q", got, "Bulbasaur")
	}
}

func TestPokemonForm(t *testing.T) {
	ctx := map[string]any{"form": 46}
	data := map[string]any{"language": "de"}

	got := renderWithData(t, `{{pokemonForm form}}`, ctx, data)
	if got != "Alola" {
		t.Errorf("pokemonForm de: got %q, want %q", got, "Alola")
	}
}

func TestPokemonFormEng(t *testing.T) {
	ctx := map[string]any{"form": 65}
	got := renderWithData(t, `{{pokemonFormEng form}}`, ctx, nil)
	if got != "Galarian" {
		t.Errorf("pokemonFormEng: got %q, want %q", got, "Galarian")
	}
}

// ---------------------------------------------------------------------------
// Pokemon block helper
// ---------------------------------------------------------------------------

func TestPokemonBlockHelper(t *testing.T) {
	ctx := map[string]any{"id": 1, "form": 0}
	data := map[string]any{"language": "en", "platform": "discord"}

	got := renderWithData(t, `{{#pokemon id form}}{{name}} ATK:{{baseStats.baseAttack}}{{/pokemon}}`, ctx, data)
	if got != "Bulbasaur ATK:118" {
		t.Errorf("pokemon block: got %q, want %q", got, "Bulbasaur ATK:118")
	}
}

func TestPokemonBlockHelperFullName(t *testing.T) {
	// Bulbasaur form 65 (Galarian) — not "Normal" so should include form
	ctx := map[string]any{"id": 1, "form": 65}
	data := map[string]any{"language": "en", "platform": "discord"}

	got := renderWithData(t, `{{#pokemon id form}}{{fullName}}{{/pokemon}}`, ctx, data)
	if got != "Bulbasaur (Galarian)" {
		t.Errorf("pokemon fullName with form: got %q, want %q", got, "Bulbasaur (Galarian)")
	}
}

func TestPokemonBlockHelperFormZero(t *testing.T) {
	// form=0 → should still work
	ctx := map[string]any{"id": 25, "form": 0}
	data := map[string]any{"language": "en", "platform": "discord"}

	got := renderWithData(t, `{{#pokemon id form}}{{name}}{{/pokemon}}`, ctx, data)
	if got != "Pikachu" {
		t.Errorf("pokemon block form 0: got %q, want %q", got, "Pikachu")
	}
}

func TestPokemonBlockHasEvolutions(t *testing.T) {
	ctx := map[string]any{"id": 1, "form": 0}
	data := map[string]any{"language": "en", "platform": "discord"}

	got := renderWithData(t, `{{#pokemon id form}}{{#if hasEvolutions}}evolves{{else}}no{{/if}}{{/pokemon}}`, ctx, data)
	if got != "evolves" {
		t.Errorf("hasEvolutions true: got %q, want %q", got, "evolves")
	}

	ctx["id"] = 150 // Mewtwo — no evolutions in test data
	got = renderWithData(t, `{{#pokemon id form}}{{#if hasEvolutions}}evolves{{else}}no{{/if}}{{/pokemon}}`, ctx, data)
	if got != "no" {
		t.Errorf("hasEvolutions false: got %q, want %q", got, "no")
	}
}

// ---------------------------------------------------------------------------
// calculateCp
// ---------------------------------------------------------------------------

func TestCalculateCp(t *testing.T) {
	// Mewtwo level 20, 15/15/15: known CP = 2387
	// attack = (300+15)*0.5974 = 188.181
	// defense = (182+15)*0.5974 = 117.688
	// stamina = (214+15)*0.5974 = 136.804
	// CP = floor(188.181 * sqrt(117.688) * sqrt(136.804) / 10) = floor(188.181 * 10.849 * 11.697 / 10)
	// = floor(188.181 * 126.896 / 10) = floor(23880.8 / 10) ≈ floor(2388.08) = 2387
	ctx := map[string]any{"id": 150, "form": 0}

	got := renderWithData(t, `{{calculateCp id form 20 15 15 15}}`, ctx, nil)
	// Validate it's a reasonable CP for Mewtwo
	if got != "2387" {
		t.Errorf("calculateCp Mewtwo L20 15/15/15: got %q, want %q", got, "2387")
	}
}

func TestCalculateCpMinimum(t *testing.T) {
	// Very low stats → should return at least 10
	ctx := map[string]any{}
	got := renderWithData(t, `{{calculateCp 0 0 1 0 0 0}}`, ctx, nil)
	if got != "10" {
		t.Errorf("calculateCp minimum: got %q, want %q", got, "10")
	}
}

func TestCalculateCpWithBaseStatsMap(t *testing.T) {
	// Same Mewtwo calculation but passing baseStats as map[string]int (enrichment format)
	ctx := map[string]any{
		"baseStats": map[string]int{
			"baseAttack":  300,
			"baseDefense": 182,
			"baseStamina": 214,
		},
	}
	got := renderWithData(t, `{{calculateCp baseStats 20 15 15 15}}`, ctx, nil)
	if got != "2387" {
		t.Errorf("calculateCp with map[string]int baseStats: got %q, want %q", got, "2387")
	}
}

// ---------------------------------------------------------------------------
// Move helpers
// ---------------------------------------------------------------------------

func TestMoveName(t *testing.T) {
	ctx := map[string]any{"move": 14}
	data := map[string]any{"language": "en"}

	got := renderWithData(t, `{{moveName move}}`, ctx, data)
	if got != "Hyper Beam" {
		t.Errorf("moveName en: got %q, want %q", got, "Hyper Beam")
	}

	data["language"] = "de"
	got = renderWithData(t, `{{moveName move}}`, ctx, data)
	if got != "Hyperstrahl" {
		t.Errorf("moveName de: got %q, want %q", got, "Hyperstrahl")
	}
}

func TestMoveNameEng(t *testing.T) {
	ctx := map[string]any{"move": 281}
	got := renderWithData(t, `{{moveNameEng move}}`, ctx, nil)
	if got != "Razor Leaf" {
		t.Errorf("moveNameEng: got %q, want %q", got, "Razor Leaf")
	}
}

func TestMoveType(t *testing.T) {
	ctx := map[string]any{"move": 281}
	data := map[string]any{"language": "en"}

	got := renderWithData(t, `{{moveType move}}`, ctx, data)
	if got != "Grass" {
		t.Errorf("moveType en: got %q, want %q", got, "Grass")
	}

	data["language"] = "de"
	got = renderWithData(t, `{{moveType move}}`, ctx, data)
	if got != "Pflanze" {
		t.Errorf("moveType de: got %q, want %q", got, "Pflanze")
	}
}

func TestMoveEmoji(t *testing.T) {
	ctx := map[string]any{"move": 281}

	// Discord platform (default)
	data := map[string]any{"platform": "discord"}
	got := renderWithData(t, `{{moveEmoji move}}`, ctx, data)
	if got != "🌿" {
		t.Errorf("moveEmoji discord: got %q, want %q", got, "🌿")
	}

	// Telegram platform — custom override
	data["platform"] = "telegram"
	got = renderWithData(t, `{{moveEmoji move}}`, ctx, data)
	if got != "🍃" {
		t.Errorf("moveEmoji telegram: got %q, want %q", got, "🍃")
	}
}

func TestMoveTypeUnknownMove(t *testing.T) {
	ctx := map[string]any{"move": 99999}
	data := map[string]any{"language": "en"}

	got := renderWithData(t, `{{moveType move}}`, ctx, data)
	if got != "" {
		t.Errorf("moveType unknown move: got %q, want empty", got)
	}
}

// ---------------------------------------------------------------------------
// getEmoji
// ---------------------------------------------------------------------------

func TestGetEmoji(t *testing.T) {
	ctx := map[string]any{}
	data := map[string]any{"platform": "discord"}

	got := renderWithData(t, `{{getEmoji "type_psychic"}}`, ctx, data)
	if got != "🔮" {
		t.Errorf("getEmoji discord: got %q, want %q", got, "🔮")
	}
}

func TestGetEmojiPerPlatform(t *testing.T) {
	ctx := map[string]any{}

	// Telegram has custom override for type_grass
	data := map[string]any{"platform": "telegram"}
	got := renderWithData(t, `{{getEmoji "type_grass"}}`, ctx, data)
	if got != "🍃" {
		t.Errorf("getEmoji telegram custom: got %q, want %q", got, "🍃")
	}

	// Discord uses default
	data["platform"] = "discord"
	got = renderWithData(t, `{{getEmoji "type_grass"}}`, ctx, data)
	if got != "🌿" {
		t.Errorf("getEmoji discord default: got %q, want %q", got, "🌿")
	}
}

// ---------------------------------------------------------------------------
// translateAlt
// ---------------------------------------------------------------------------

func TestTranslateAlt(t *testing.T) {
	ctx := map[string]any{}
	data := map[string]any{"altLanguage": "de"}

	got := renderWithData(t, `{{translateAlt "greeting"}}`, ctx, data)
	if got != "Hallo" {
		t.Errorf("translateAlt de: got %q, want %q", got, "Hallo")
	}
}

func TestTranslateAltFallbackEn(t *testing.T) {
	ctx := map[string]any{}
	// No altLanguage → falls back to "en"
	got := renderWithData(t, `{{translateAlt "greeting"}}`, ctx, nil)
	if got != "Hello" {
		t.Errorf("translateAlt fallback en: got %q, want %q", got, "Hello")
	}
}

// ---------------------------------------------------------------------------
// getPowerUpCost
// ---------------------------------------------------------------------------

func TestGetPowerUpCostInline(t *testing.T) {
	ctx := map[string]any{}
	// Level 1 to 2 = two power-ups (1.0 and 1.5), each 200 stardust, 1 candy
	got := renderWithData(t, `{{getPowerUpCost 1 2}}`, ctx, nil)
	expected := "400 stardust, 2 candy, 0 XL candy"
	if got != expected {
		t.Errorf("getPowerUpCost 1→2: got %q, want %q", got, expected)
	}
}

func TestGetPowerUpCostBlock(t *testing.T) {
	ctx := map[string]any{}
	got := renderWithData(t, `{{#getPowerUpCost 1 2}}SD:{{stardust}} C:{{candy}} XL:{{xlCandy}}{{/getPowerUpCost}}`, ctx, nil)
	expected := "SD:400 C:2 XL:0"
	if got != expected {
		t.Errorf("getPowerUpCost block: got %q, want %q", got, expected)
	}
}

func TestGetPowerUpCostBlockWithAddCommas(t *testing.T) {
	ctx := map[string]any{}
	got := renderWithData(t, `{{#getPowerUpCost 1 40}}{{addCommas stardust}} stardust & {{candy}} candy{{/getPowerUpCost}}`, ctx, nil)
	if got == "" {
		t.Error("getPowerUpCost block with addCommas: got empty")
	}
	if !strings.Contains(got, "stardust") || !strings.Contains(got, "candy") {
		t.Errorf("getPowerUpCost block: expected stardust and candy, got %q", got)
	}
}

// Regression test: getPowerUpCost used inline inside {{#each}} must not
// cause infinite recursion. This reproduces the exact DTS template pattern:
// {{#each pvp_rankings}}...{{getPowerUpCost ../level this.level}}...{{/each}}
func TestGetPowerUpCostInlineInsideEach(t *testing.T) {
	ctx := map[string]any{
		"baseLevel": 25,
		"rankings": []map[string]any{
			{"targetLevel": 40, "name": "First"},
			{"targetLevel": 50, "name": "Second"},
		},
	}
	tpl := `{{#each rankings}}{{getPowerUpCost ../baseLevel this.targetLevel}} | {{/each}}`
	got := renderWithData(t, tpl, ctx, nil)
	if got == "" {
		t.Fatal("getPowerUpCost inline inside each: got empty result")
	}
	if !strings.Contains(got, "stardust") {
		t.Errorf("getPowerUpCost inline inside each: expected stardust in output, got %q", got)
	}
	// Should render twice (two rankings)
	if strings.Count(got, "|") != 2 {
		t.Errorf("getPowerUpCost inline inside each: expected 2 items, got %q", got)
	}
}

// Same test but as a subexpression: (getPowerUpCost ...) inside {{#each}}
// This matches the user's DTS pattern: {{{replace (getPowerUpCost ../level this.level) ...}}}
func TestGetPowerUpCostSubexpressionInsideEach(t *testing.T) {
	ctx := map[string]any{
		"baseLevel": 25,
		"rankings": []map[string]any{
			{"targetLevel": 40},
		},
	}
	tpl := `{{#each rankings}}{{replace (getPowerUpCost ../baseLevel this.targetLevel) "stardust" "SD"}}{{/each}}`
	got := renderWithData(t, tpl, ctx, nil)
	if got == "" {
		t.Fatal("getPowerUpCost subexpression inside each: got empty result")
	}
	if !strings.Contains(got, "SD") {
		t.Errorf("expected 'stardust' replaced with 'SD', got %q", got)
	}
}

// ---------------------------------------------------------------------------
// buildFullName (unit)
// ---------------------------------------------------------------------------

func TestBuildFullName(t *testing.T) {
	tests := []struct {
		name, formName, formNameEng, want string
	}{
		{"Pikachu", "", "", "Pikachu"},
		{"Pikachu", "Normal", "Normal", "Pikachu"},
		{"Pikachu", "Alolan", "Alolan", "Pikachu (Alolan)"},
		{"Bisasam", "Alola", "Alolan", "Bisasam (Alola)"},
	}
	for _, tt := range tests {
		got := buildFullName(tt.name, tt.formName, tt.formNameEng)
		if got != tt.want {
			t.Errorf("buildFullName(%q, %q, %q) = %q, want %q",
				tt.name, tt.formName, tt.formNameEng, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// getCPMultiplier (unit)
// ---------------------------------------------------------------------------

func TestGetCPMultiplier(t *testing.T) {
	gd := testGameData()
	// Add CP multipliers to test data
	gd.Util.CpMultipliers = map[string]float64{
		"20": 0.597400009632111,
		"40": 0.790300011634827,
	}

	// Level 20
	cpm := getCPMultiplier(gd, 20)
	if cpm < 0.597 || cpm > 0.598 {
		t.Errorf("getCPMultiplier(20) = %f, want ~0.5974", cpm)
	}

	// Level 40
	cpm = getCPMultiplier(gd, 40)
	if cpm < 0.790 || cpm > 0.791 {
		t.Errorf("getCPMultiplier(40) = %f, want ~0.7903", cpm)
	}

	// Missing level → fallback
	cpm = getCPMultiplier(gd, 100)
	if cpm != 0.7903 {
		t.Errorf("getCPMultiplier(100) = %f, want 0.7903 (fallback)", cpm)
	}

	// Nil GameData → fallback
	cpm = getCPMultiplier(nil, 20)
	if cpm != 0.7903 {
		t.Errorf("getCPMultiplier(nil, 20) = %f, want 0.7903 (fallback)", cpm)
	}
}

// ---------------------------------------------------------------------------
// calculatePowerUpCost (unit)
// ---------------------------------------------------------------------------

func TestCalculatePowerUpCost(t *testing.T) {
	gd := testGameData()

	// Level 1 → 1.5: one power-up, 200 stardust, 1 candy
	sd, c, xl := calculatePowerUpCost(gd, 1, 1.5)
	if sd != 200 || c != 1 || xl != 0 {
		t.Errorf("1→1.5: got (%d, %d, %d), want (200, 1, 0)", sd, c, xl)
	}

	// Level 1 → 2: two power-ups, 400 stardust, 2 candy
	sd, c, xl = calculatePowerUpCost(gd, 1, 2)
	if sd != 400 || c != 2 || xl != 0 {
		t.Errorf("1→2: got (%d, %d, %d), want (400, 2, 0)", sd, c, xl)
	}

	// Level 40 → 40.5: XL candy level
	sd, c, xl = calculatePowerUpCost(gd, 40, 40.5)
	if sd != 10000 || c != 0 || xl != 10 {
		t.Errorf("40→40.5: got (%d, %d, %d), want (10000, 0, 10)", sd, c, xl)
	}

	// Nil GameData returns zeros
	sd, c, xl = calculatePowerUpCost(nil, 1, 2)
	if sd != 0 || c != 0 || xl != 0 {
		t.Errorf("nil gd: got (%d, %d, %d), want (0, 0, 0)", sd, c, xl)
	}
}

// ---------------------------------------------------------------------------
// Custom maps
// ---------------------------------------------------------------------------

func TestLookupCustomMap(t *testing.T) {
	store := &customMapStore{
		maps: map[string]map[string]any{
			"colors": {
				"red":   "#ff0000",
				"green": "#00ff00",
			},
			"colors.de": {
				"red": "#rot",
			},
		},
	}

	check := func(name string, got any, found bool, want any, wantFound bool) {
		t.Helper()
		if got != want || found != wantFound {
			t.Errorf("%s: got (%v, %v), want (%v, %v)", name, got, found, want, wantFound)
		}
	}

	got, ok := lookupCustomMap(store, "colors", "red", "", "en")
	check("basic lookup", got, ok, "#ff0000", true)

	got, ok = lookupCustomMap(store, "colors", "red", "", "de")
	check("lang lookup", got, ok, "#rot", true)

	got, ok = lookupCustomMap(store, "colors", "green", "", "de")
	check("lang fallback to base", got, ok, "#00ff00", true)

	// Missing key → not found (PoracleJS returned undefined, rendering empty)
	got, ok = lookupCustomMap(store, "colors", "blue", "", "en")
	check("missing key", got, ok, nil, false)

	got, ok = lookupCustomMap(store, "colors", "blue", "green", "en")
	check("fallback value", got, ok, "#00ff00", true)

	got, ok = lookupCustomMap(store, "nosuchmap", "red", "", "en")
	check("missing map", got, ok, nil, false)
}

func TestLookupCustomMapNilStore(t *testing.T) {
	got, ok := lookupCustomMap(nil, "colors", "red", "", "en")
	if got != nil || ok {
		t.Errorf("nil store: got (%v, %v), want (nil, false)", got, ok)
	}
}

// writeCustomMaps writes files into <dir>/customMaps and loads them.
func writeCustomMaps(t *testing.T, files map[string]string) *customMapStore {
	t.Helper()
	dir := t.TempDir()
	mapsDir := filepath.Join(dir, "customMaps")
	if err := os.MkdirAll(mapsDir, 0755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(mapsDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return loadCustomMaps(dir)
}

func TestLoadCustomMapsFormats(t *testing.T) {
	store := writeCustomMaps(t, map[string]string{
		// PoracleJS format with object values; lookup uses "name", not the file name
		"gen-file.json": `{"name": "generation", "map": {"1": {"genName": "Kanto", "genNumber": "1"}}}`,
		// PoracleJS array of definitions, one per language
		"weather.json": `[
			{"name": "weatherBoost", "map": {"Windy": "Dragon, Flying, Psychic"}},
			{"name": "weatherBoost", "language": "de", "map": {"Windy": "Drache"}}
		]`,
		// Flat format, named after the file, with a language suffix
		"ivSymbol.json":    `{"15": "⓯"}`,
		"ivSymbol.de.json": `{"15": "XV"}`,
		// Numeric values are allowed
		"trainers.json": `{"name": "trainers", "map": {"144": 2}}`,
	})

	gen, ok := lookupCustomMap(store, "generation", "1", "", "en")
	if obj, isObj := gen.(map[string]any); !ok || !isObj || obj["genName"] != "Kanto" {
		t.Errorf("generation object value: got (%v, %v)", gen, ok)
	}
	if _, ok := lookupCustomMap(store, "gen-file", "1", "", "en"); ok {
		t.Error("map should be registered under its name field, not the file name")
	}
	if v, _ := lookupCustomMap(store, "weatherBoost", "Windy", "", "en"); v != "Dragon, Flying, Psychic" {
		t.Errorf("array file base map: got %v", v)
	}
	if v, _ := lookupCustomMap(store, "weatherBoost", "Windy", "", "de"); v != "Drache" {
		t.Errorf("array file language map: got %v", v)
	}
	if v, _ := lookupCustomMap(store, "ivSymbol", "15", "", "en"); v != "⓯" {
		t.Errorf("flat map: got %v", v)
	}
	if v, _ := lookupCustomMap(store, "ivSymbol", "15", "", "de"); v != "XV" {
		t.Errorf("flat language map: got %v", v)
	}
	if v, _ := lookupCustomMap(store, "trainers", "144", "", "en"); v != float64(2) {
		t.Errorf("numeric value: got %v (%T)", v, v)
	}
}

func TestCustomMapHelperModes(t *testing.T) {
	store := writeCustomMaps(t, map[string]string{
		"generation.json": `{"name": "generation", "map": {"1": {"genName": "Kanto", "genNumber": "1"}, "26_61": {"genName": "Alola", "genNumber": "7"}}}`,
		"shiny.json":      `{"name": "shinyPossible", "map": {"25": "✨"}}`,
		"cells.json":      `{"name": "cells", "map": {"5101693287038812000": "cellA"}}`,
	})
	raymond.RegisterHelper("testmap", func(mapName, value any, options *raymond.Options) any {
		v, ok := lookupCustomMap(store, toString(mapName), toString(value), "", getLang(options))
		return customMapResult(v, ok, options)
	})
	raymond.RegisterHelper("testmap2", func(mapName, value, value2 any, options *raymond.Options) any {
		v, ok := lookupCustomMap(store, toString(mapName), toString(value), toString(value2), getLang(options))
		return customMapResult(v, ok, options)
	})
	defer raymond.RemoveHelper("testmap")
	defer raymond.RemoveHelper("testmap2")

	tests := []struct {
		name string
		tmpl string
		ctx  map[string]any
		want string
	}{
		{"inline hit", `{{testmap 'shinyPossible' pokemonId}}`, map[string]any{"pokemonId": 25}, "✨"},
		{"inline miss is empty", `[{{testmap 'shinyPossible' pokemonId}}]`, map[string]any{"pokemonId": 1}, "[]"},
		{"inline object is empty", `[{{testmap 'generation' pokemonId}}]`, map[string]any{"pokemonId": 1}, "[]"},
		{"block exposes object fields", `{{#testmap 'generation' pokemonId}}Gen {{genNumber}} {{genName}}{{/testmap}}`, map[string]any{"pokemonId": 1}, "Gen 1 Kanto"},
		{"block miss renders else", `{{#testmap 'generation' pokemonId}}{{genName}}{{else}}none{{/testmap}}`, map[string]any{"pokemonId": 999}, "none"},
		{"subexpression hit is truthy", `{{#if (testmap 'generation' pokemonId)}}yes{{else}}no{{/if}}`, map[string]any{"pokemonId": 1}, "yes"},
		{"subexpression miss is falsy", `{{#if (testmap 'generation' pokemonId)}}yes{{else}}no{{/if}}`, map[string]any{"pokemonId": 999}, "no"},
		{"map2 prefers first key", `{{#testmap2 'generation' (concat pokemonId '_' formId) pokemonId}}{{genName}}{{/testmap2}}`, map[string]any{"pokemonId": 26, "formId": 61}, "Alola"},
		{"map2 falls back to second key", `{{#testmap2 'generation' (concat pokemonId '_' formId) pokemonId}}{{genName}}{{/testmap2}}`, map[string]any{"pokemonId": 1, "formId": 0}, "Kanto"},
		{"missing field in concat key", `[{{testmap 'shinyPossible' (concat pokemonId '_' formId)}}]`, map[string]any{"pokemonId": 25}, "[]"},
		// Inline lookups inside another block must still render inline (#ivSymbol report).
		{"inline inside if block", `{{#if pokemonId}}{{testmap 'shinyPossible' pokemonId}}{{/if}}`, map[string]any{"pokemonId": 25}, "✨"},
		{"inline inside else block", `{{#if nope}}x{{else}}{{testmap 'shinyPossible' pokemonId}}{{/if}}`, map[string]any{"pokemonId": 25}, "✨"},
		{"inline inside each", `{{#each ids}}{{testmap 'shinyPossible' this}}{{/each}}`, map[string]any{"ids": []int{25, 1, 25}}, "✨✨"},
		{"inline inside comparison block", `{{#isnt iv '100.00'}}{{testmap 'shinyPossible' pokemonId}}{{/isnt}}`, map[string]any{"iv": "48.89", "pokemonId": 25}, "✨"},
		{"block inside if block", `{{#if pokemonId}}{{#testmap 'generation' pokemonId}}{{genName}}{{/testmap}}{{/if}}`, map[string]any{"pokemonId": 1}, "Kanto"},
		{"large float key", `{{testmap 'cells' cell}}`, map[string]any{"cell": float64(5101693287038812160)}, "cellA"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := renderWithData(t, tc.tmpl, tc.ctx, nil); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCustomMapStoreSnapshot(t *testing.T) {
	store := writeCustomMaps(t, map[string]string{
		"weather.json": `[{"name": "weatherBoost", "map": {"Windy": "Flying"}}, {"name": "weatherBoost", "language": "de", "map": {"Windy": "Flug"}}]`,
	})
	snap := store.snapshot()
	if snap["weatherBoost"]["Windy"] != "Flying" || snap["weatherBoost.de"]["Windy"] != "Flug" {
		t.Errorf("snapshot keyed by lookup key: got %v", snap)
	}
	// Replacing a map in the snapshot must not affect the store.
	snap["weatherBoost"] = nil
	if v, ok := lookupCustomMap(store, "weatherBoost", "Windy", "", "en"); !ok || v != "Flying" {
		t.Errorf("store changed through snapshot: got (%v, %v)", v, ok)
	}
	if got := (*customMapStore)(nil).snapshot(); got == nil || len(got) != 0 {
		t.Errorf("nil store snapshot: got %v, want empty map", got)
	}
}

// {{#pokemon id}} without a form is the documented short form, but the
// helper's signature required both, and raymond panicked with "too few
// input arguments" (#269).
func TestPokemonBlockHelperWithoutForm(t *testing.T) {
	ctx := map[string]any{"id": 25}
	data := map[string]any{"language": "en", "platform": "discord"}

	got := renderWithData(t, `{{#pokemon id}}{{name}}{{/pokemon}}`, ctx, data)
	if got != "Pikachu" {
		t.Errorf("pokemon without form: got %q, want %q", got, "Pikachu")
	}
}

// A helper used as a block helper's argument is its own invocation and must
// not see the enclosing block's IsBlock flag (it used to render the block
// body instead of returning its value).
func TestHelperAsBlockArgument(t *testing.T) {
	ctx := map[string]any{"id": 25}
	data := map[string]any{"language": "en"}
	tests := []struct{ tmpl, want string }{
		{`{{#eq (pokemon id 0) 'Pikachu'}}Y{{else}}N{{/eq}}`, "Y"},
		{`{{#isnt (pokemon id 0) 'Pikachu'}}N{{else}}Y{{/isnt}}`, "Y"},
		{`{{#if (eq (pokemon id 0) 'Pikachu')}}Y{{else}}N{{/if}}`, "Y"},
		{`{{#with (pokemon id 0)}}[{{this}}]{{/with}}`, "[Pikachu]"},
		{`{{#if id}}{{#eq (pokemon id 0) 'Pikachu'}}Y{{/eq}}{{/if}}`, "Y"},
		{`{{#pokemon id 0}}{{fullName}}{{/pokemon}}`, "Pikachu"},
	}
	for _, tc := range tests {
		if got := renderWithData(t, tc.tmpl, ctx, data); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.tmpl, got, tc.want)
		}
	}
}
