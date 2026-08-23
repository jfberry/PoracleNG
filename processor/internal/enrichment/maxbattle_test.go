package enrichment

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// maxbattleTestEnricher builds an Enricher with generation data + a translator
// so we can assert the pokemonId / generation / generationName fields that DTS
// maxbattle templates reference (parity with raid/pokemon).
func maxbattleTestEnricher() *Enricher {
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"poke_6":       "Charizard",
		"generation_1": "Kanto",
		"max_battle_6": "Tier 6",
		"poke_type_4":  "Fire",
		"poke_type_12": "Grass",
		"weather_1":    "Clear",
		"form_65":      "Alolan",
		"costume_3":    "Winter 2023",
	}))
	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 6, Form: 0}:  {PokemonID: 6, FormID: 0, Types: []int{4, 12}, GenID: 1, Attack: 1, Defense: 1, Stamina: 1},
			{ID: 6, Form: 65}: {PokemonID: 6, FormID: 65, Types: []int{4, 12}, GenID: 1, Attack: 1, Defense: 1, Stamina: 1},
		},
		Moves: map[int]*gamedata.Move{},
		Types: map[int]*gamedata.TypeInfo{},
		Util: &gamedata.UtilData{
			GenData: map[int]gamedata.GenInfo{1: {Roman: "I"}},
		},
	}
	return &Enricher{
		WeatherProvider: &mockWeather{},
		TimeLayout:      "15:04:05",
		DateLayout:      "2006-01-02",
		GameData:        gd,
		Translations:    bundle,
		DefaultLocale:   "en",
	}
}

func TestMaxbattle_SetsPokemonIdAndGeneration(t *testing.T) {
	e := maxbattleTestEnricher()
	mb := &webhook.MaxbattleWebhook{
		ID: "station1", Name: "Power Spot", Latitude: 52.5, Longitude: 13.4,
		BattleLevel: 6, BattlePokemonID: 6, BattlePokemonForm: 65,
		BattlePokemonGender: 2, BattlePokemonCostume: 3, BattlePokemonAlignment: 1,
		BattlePokemonBreadMode: 2,
	}
	m, _ := e.Maxbattle(52.5, 13.4, 0, mb, TileModeSkip)

	if got := m["pokemonId"]; got != 6 {
		t.Errorf("pokemonId = %v, want 6", got)
	}
	if got := m["generation"]; got != 1 {
		t.Errorf("generation = %v, want 1", got)
	}
	// Identity passthroughs (PoracleJS parity).
	for field, want := range map[string]int{
		"form": 65, "formId": 65, "gender": 2, "costume": 3,
		"alignment": 1, "level": 6, "bread": 2,
	} {
		if got := m[field]; got != want {
			t.Errorf("%s = %v, want %d", field, got, want)
		}
	}
	if _, ok := m["boostingWeatherIds"]; !ok {
		t.Errorf("boostingWeatherIds should be set")
	}
}

func TestMaxbattleTranslate_SetsGenerationName(t *testing.T) {
	e := maxbattleTestEnricher()
	mb := &webhook.MaxbattleWebhook{
		ID: "station1", Latitude: 52.5, Longitude: 13.4,
		BattleLevel: 6, BattlePokemonID: 6, BattlePokemonForm: 0,
	}
	base, _ := e.Maxbattle(52.5, 13.4, 0, mb, TileModeSkip)
	base["gameWeatherId"] = 1 // mockWeather returns 0; inject to exercise the Eng variant
	m := e.MaxbattleTranslate(base, mb, "en")

	if got := m["generation"]; got != 1 {
		t.Errorf("generation = %v, want 1", got)
	}
	if got, _ := m["generationName"].(string); got != "Kanto" {
		t.Errorf("generationName = %q, want %q", got, "Kanto")
	}
	// Shared-with-raid *Eng fields.
	if got, _ := m["gameWeatherNameEng"].(string); got != "Clear" {
		t.Errorf("gameWeatherNameEng = %q, want %q", got, "Clear")
	}
	if _, ok := m["boostWeatherNameEng"]; !ok {
		t.Errorf("boostWeatherNameEng should be emitted")
	}
}

// TestMaxbattleTranslate_BossIncludesFormAndCostume locks the fix: the
// max-battle boss's webhook form + costume must reach fullName and megaName
// (which now equals the full display name), plus a standalone costumeName.
func TestMaxbattleTranslate_BossIncludesFormAndCostume(t *testing.T) {
	e := maxbattleTestEnricher()
	mb := &webhook.MaxbattleWebhook{
		ID: "station1", Latitude: 52.5, Longitude: 13.4,
		BattleLevel: 6, BattlePokemonID: 6, BattlePokemonForm: 65, BattlePokemonCostume: 3,
	}
	base, _ := e.Maxbattle(52.5, 13.4, 0, mb, TileModeSkip)
	m := e.MaxbattleTranslate(base, mb, "en")

	fullName, _ := m["fullName"].(string)
	if !strings.Contains(fullName, "Alolan") || !strings.Contains(fullName, "Winter 2023") {
		t.Errorf("fullName = %q, want it to contain the form (Alolan) and costume (Winter 2023)", fullName)
	}
	if mega, _ := m["megaName"].(string); mega != fullName {
		t.Errorf("megaName = %q, want it to equal fullName %q", mega, fullName)
	}
	if got := m["costumeName"]; got != "Winter 2023" {
		t.Errorf("costumeName = %v, want \"Winter 2023\"", got)
	}
}
