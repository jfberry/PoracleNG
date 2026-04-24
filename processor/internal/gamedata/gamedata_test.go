package gamedata

import (
	"os"
	"path/filepath"
	"testing"
)

// testBaseDir finds the project root relative to this test file.
func testBaseDir(t *testing.T) string {
	t.Helper()
	// processor/internal/gamedata/ → project root is ../../../
	dir := filepath.Join("..", "..", "..")
	// Verify raw data exists
	if _, err := os.Stat(filepath.Join(dir, "resources", "rawdata", "pokemon.json")); err != nil {
		t.Skipf("raw resource files not found at %s: %v", dir, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "resources", "data", "util.json")); err != nil {
		t.Skipf("util.json not found at %s: %v", dir, err)
	}
	return dir
}

func loadTestGameData(t *testing.T) *GameData {
	t.Helper()
	baseDir := testBaseDir(t)
	gd, err := Load(baseDir)
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	return gd
}

func TestLoadGameData(t *testing.T) {
	gd := loadTestGameData(t)

	if len(gd.Monsters) == 0 {
		t.Fatal("no monsters loaded")
	}
	if len(gd.Moves) == 0 {
		t.Fatal("no moves loaded")
	}
	if len(gd.Types) == 0 {
		t.Fatal("no types loaded")
	}
	if len(gd.Items) == 0 {
		t.Fatal("no items loaded")
	}
	if len(gd.Grunts) == 0 {
		t.Fatal("no grunts loaded")
	}
	if len(gd.Weather) == 0 {
		t.Fatal("no weather loaded")
	}
	if gd.Util == nil {
		t.Fatal("util data not loaded")
	}
	if len(gd.Util.GenData) == 0 {
		t.Fatal("no generation data")
	}

	t.Logf("Loaded: %d monsters, %d moves, %d types, %d items, %d grunts, %d weather",
		len(gd.Monsters), len(gd.Moves), len(gd.Types), len(gd.Items), len(gd.Grunts), len(gd.Weather))
}

func TestGetMonster(t *testing.T) {
	gd := loadTestGameData(t)

	// Bulbasaur should exist at form 0
	bulba := gd.GetMonster(1, 0)
	if bulba == nil {
		t.Fatal("Bulbasaur (1, 0) not found")
	}
	if bulba.PokemonID != 1 {
		t.Errorf("PokemonID = %d, want 1", bulba.PokemonID)
	}
	if len(bulba.Types) != 2 {
		t.Fatalf("Bulbasaur types count = %d, want 2", len(bulba.Types))
	}
	// Bulbasaur is Poison(4)/Grass(12)
	if bulba.Types[0] != 4 && bulba.Types[0] != 12 {
		t.Errorf("Bulbasaur first type = %d, want 4 or 12", bulba.Types[0])
	}
	if bulba.Attack == 0 || bulba.Defense == 0 || bulba.Stamina == 0 {
		t.Errorf("Bulbasaur stats missing: atk=%d def=%d sta=%d", bulba.Attack, bulba.Defense, bulba.Stamina)
	}
}

func TestGetMonsterFallback(t *testing.T) {
	gd := loadTestGameData(t)

	// Unknown form should fall back to form 0
	bulbaFallback := gd.GetMonster(1, 99999)
	if bulbaFallback == nil {
		t.Fatal("GetMonster(1, 99999) returned nil (should fallback to form 0)")
	}
	if bulbaFallback.PokemonID != 1 {
		t.Errorf("fallback PokemonID = %d, want 1", bulbaFallback.PokemonID)
	}

	// Completely unknown pokemon should return nil
	unknown := gd.GetMonster(99999, 0)
	if unknown != nil {
		t.Error("GetMonster(99999, 0) should return nil")
	}
}

func TestFormJoining(t *testing.T) {
	gd := loadTestGameData(t)

	// Bulbasaur should have its default form (163) entry
	bulbaDefault := gd.GetMonster(1, 163)
	if bulbaDefault == nil {
		t.Skip("Bulbasaur form 163 not found (may not be in raw data)")
	}
	if bulbaDefault.PokemonID != 1 {
		t.Errorf("PokemonID = %d, want 1", bulbaDefault.PokemonID)
	}
	if bulbaDefault.FormID != 163 {
		t.Errorf("FormID = %d, want 163", bulbaDefault.FormID)
	}
}

func TestEvolutions(t *testing.T) {
	gd := loadTestGameData(t)

	bulba := gd.GetMonster(1, 0)
	if bulba == nil {
		t.Fatal("Bulbasaur not found")
	}
	if len(bulba.Evolutions) == 0 {
		t.Fatal("Bulbasaur should have evolutions")
	}
	// Should evolve to Ivysaur (2)
	found := false
	for _, evo := range bulba.Evolutions {
		if evo.PokemonID == 2 {
			found = true
			break
		}
	}
	if !found {
		t.Error("Bulbasaur should evolve to Ivysaur (ID 2)")
	}
}

func TestTempEvolutions(t *testing.T) {
	gd := loadTestGameData(t)

	// Venusaur (3) should have mega evolution
	venusaur := gd.GetMonster(3, 0)
	if venusaur == nil {
		t.Fatal("Venusaur not found")
	}
	if len(venusaur.TempEvolutions) == 0 {
		t.Fatal("Venusaur should have temp evolutions (mega)")
	}
	found := false
	for _, te := range venusaur.TempEvolutions {
		if te.TempEvoID == 1 { // Mega
			found = true
			if te.Attack == 0 {
				t.Error("Mega Venusaur should have non-zero attack")
			}
			break
		}
	}
	if !found {
		t.Error("Venusaur should have mega evolution (TempEvoID 1)")
	}
}

func TestGetGeneration(t *testing.T) {
	gd := loadTestGameData(t)

	tests := []struct {
		name      string
		pokemonID int
		form      int
		wantGen   int
	}{
		{"Bulbasaur", 1, 0, 1},
		{"Pikachu", 25, 0, 1},
		{"Chikorita", 152, 0, 2},
		{"Treecko", 252, 0, 3},
		{"Turtwig", 387, 0, 4},
		{"Snivy", 494, 0, 5},
		{"Chespin", 650, 0, 6},
		{"Rowlet", 722, 0, 7},
		{"Grookey", 810, 0, 8},
		{"Sprigatito", 906, 0, 9},
		// Gen exceptions: Alolan Rattata (19_46) should be gen 7
		{"Alolan Rattata", 19, 46, 7},
		// Galarian Meowth (52_2335) should be gen 8
		{"Galarian Meowth", 52, 2335, 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gen := gd.GetGeneration(tt.pokemonID, tt.form)
			if gen != tt.wantGen {
				t.Errorf("GetGeneration(%d, %d) = %d, want %d", tt.pokemonID, tt.form, gen, tt.wantGen)
			}
		})
	}
}

func TestGetGenerationInfo(t *testing.T) {
	gd := loadTestGameData(t)

	info := gd.GetGenerationInfo(1)
	if info == nil {
		t.Fatal("Gen 1 info is nil")
	}
	if info.Roman != "I" {
		t.Errorf("Gen 1 roman = %q, want %q", info.Roman, "I")
	}
	if info.Min != 1 || info.Max != 151 {
		t.Errorf("Gen 1 range = [%d, %d], want [1, 151]", info.Min, info.Max)
	}
}

func TestMonsterNameKeys(t *testing.T) {
	gd := loadTestGameData(t)

	tests := []struct {
		name       string
		pokemonID  int
		form       int
		evolution  int
		wantPokeKey string
		wantFormKey string
		wantMega    string
	}{
		{
			name: "Bulbasaur default form",
			pokemonID: 1, form: 0, evolution: 0,
			wantPokeKey: "poke_1",
			wantFormKey: "",
			wantMega:    "{0}",
		},
		{
			name: "Bulbasaur specific form",
			pokemonID: 1, form: 163, evolution: 0,
			wantPokeKey: "poke_1",
			wantFormKey: "form_163",
			wantMega:    "{0}",
		},
		{
			name: "Venusaur mega",
			pokemonID: 3, form: 0, evolution: 1,
			wantPokeKey: "poke_3",
			wantFormKey: "",
			wantMega:    "Mega {0}",
		},
		{
			name: "Mega X evolution",
			pokemonID: 6, form: 0, evolution: 2,
			wantPokeKey: "poke_6",
			wantFormKey: "",
			wantMega:    "Mega {0} X",
		},
		{
			name: "Primal evolution",
			pokemonID: 382, form: 0, evolution: 4,
			wantPokeKey: "poke_382",
			wantFormKey: "",
			wantMega:    "Primal {0}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := gd.MonsterNameKeys(tt.pokemonID, tt.form, tt.evolution)
			if info.PokemonKey != tt.wantPokeKey {
				t.Errorf("PokemonKey = %q, want %q", info.PokemonKey, tt.wantPokeKey)
			}
			if info.FormKey != tt.wantFormKey {
				t.Errorf("FormKey = %q, want %q", info.FormKey, tt.wantFormKey)
			}
			if info.MegaNamePattern != tt.wantMega {
				t.Errorf("MegaNamePattern = %q, want %q", info.MegaNamePattern, tt.wantMega)
			}
		})
	}
}

func TestTranslationKeys(t *testing.T) {
	tests := []struct {
		name string
		fn   func() string
		want string
	}{
		{"PokemonTranslationKey(1)", func() string { return PokemonTranslationKey(1) }, "poke_1"},
		{"PokemonTranslationKey(25)", func() string { return PokemonTranslationKey(25) }, "poke_25"},
		{"FormTranslationKey(163)", func() string { return FormTranslationKey(163) }, "form_163"},
		{"MoveTranslationKey(14)", func() string { return MoveTranslationKey(14) }, "move_14"},
		{"TypeTranslationKey(1)", func() string { return TypeTranslationKey(1) }, "poke_type_1"},
		{"ItemTranslationKey(1)", func() string { return ItemTranslationKey(1) }, "item_1"},
		{"WeatherTranslationKey(1)", func() string { return WeatherTranslationKey(1) }, "weather_1"},
		{"GruntTranslationKey(4)", func() string { return GruntTranslationKey(4) }, "grunt_4"},
		{"MegaEvoTranslationKey(3,1)", func() string { return MegaEvoTranslationKey(3, 1) }, "poke_3_e1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.fn()
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetMoveType(t *testing.T) {
	gd := loadTestGameData(t)

	// Wrap (move 13) is Normal type (1) in the raw masterfile
	move := gd.GetMove(13)
	if move == nil {
		t.Fatal("Move 13 (Wrap) not found")
	}
	if move.TypeID != 1 {
		t.Errorf("Move 13 (Wrap) TypeID = %d, want 1 (Normal)", move.TypeID)
	}

	// Move 1 (Thunder Shock) exists but has no type in raw data (minimal entry)
	move1 := gd.GetMove(1)
	if move1 == nil {
		t.Fatal("Move 1 not found")
	}
}

func TestGetTypeColor(t *testing.T) {
	gd := loadTestGameData(t)

	// Grass type (12) should have color
	color := gd.GetTypeColor([]int{12})
	if color == "" {
		t.Error("Grass type should have a color")
	}

	// Empty types should return empty
	empty := gd.GetTypeColor(nil)
	if empty != "" {
		t.Errorf("empty types should return empty color, got %q", empty)
	}
}

func TestGetTypeEmojiKeys(t *testing.T) {
	gd := loadTestGameData(t)

	keys := gd.GetTypeEmojiKeys([]int{12, 4}) // Grass, Poison
	if len(keys) != 2 {
		t.Fatalf("expected 2 emoji keys, got %d", len(keys))
	}
	for _, k := range keys {
		if k == "" {
			t.Error("emoji key should not be empty")
		}
	}
}
