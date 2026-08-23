package enrichment

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// raidTestEnricher builds a minimal Enricher for exercising RaidTranslate's
// boss-name fields (fullName / megaName / costumeName). lang == DefaultLocale
// ("en") so the deferred localized-geo lookup is a no-op (no geocoder needed).
func raidTestEnricher() *Enricher {
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"poke_6":    "Charizard",
		"form_65":   "Alolan",
		"costume_3": "Winter 2023",
		"poke_6_e1": "Mega Charizard", // combo key → localised mega name
	}))
	bundle.LinkFallbacks()
	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 6, Form: 0}:  {PokemonID: 6, FormID: 0, Types: []int{}},
			{ID: 6, Form: 65}: {PokemonID: 6, FormID: 65, Types: []int{}},
		},
		Moves: map[int]*gamedata.Move{},
		Types: map[int]*gamedata.TypeInfo{},
		Util:  &gamedata.UtilData{GenData: map[int]gamedata.GenInfo{1: {Roman: "I"}}},
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

// TestRaidTranslate_NonMegaBossIncludesFormAndCostume locks the fix for a
// non-evolved raid boss: the webhook costume must reach fullName AND megaName
// (which now equals the full display name: base + form + costume).
func TestRaidTranslate_NonMegaBossIncludesFormAndCostume(t *testing.T) {
	e := raidTestEnricher()
	raid := &webhook.RaidWebhook{PokemonID: 6, Form: 65, Costume: 3, Evolution: 0}
	m := e.RaidTranslate(map[string]any{"gameWeatherId": 0}, raid, "en")

	fullName, _ := m["fullName"].(string)
	if !strings.Contains(fullName, "Alolan") || !strings.Contains(fullName, "Winter 2023") {
		t.Errorf("fullName = %q, want it to contain the form (Alolan) and costume (Winter 2023)", fullName)
	}
	mega, _ := m["megaName"].(string)
	if mega != fullName {
		t.Errorf("megaName = %q, want it to equal fullName %q (full display name)", mega, fullName)
	}
	if got := m["costumeName"]; got != "Winter 2023" {
		t.Errorf("costumeName = %v, want \"Winter 2023\"", got)
	}
}

// TestRaidTranslate_MegaBossIncludesCostume locks the fix for an evolved boss:
// the mega name plus the costume.
func TestRaidTranslate_MegaBossIncludesCostume(t *testing.T) {
	e := raidTestEnricher()
	raid := &webhook.RaidWebhook{PokemonID: 6, Form: 0, Costume: 3, Evolution: 1}
	m := e.RaidTranslate(map[string]any{"gameWeatherId": 0}, raid, "en")

	fullName, _ := m["fullName"].(string)
	if !strings.Contains(fullName, "Mega Charizard") || !strings.Contains(fullName, "Winter 2023") {
		t.Errorf("fullName = %q, want it to contain the mega name and costume", fullName)
	}
	if mega, _ := m["megaName"].(string); mega != fullName {
		t.Errorf("megaName = %q, want it to equal fullName %q", mega, fullName)
	}
	if got := m["costumeName"]; got != "Winter 2023" {
		t.Errorf("costumeName = %v, want \"Winter 2023\"", got)
	}
}

// TestRaidTranslate_NoCostumeUnchanged verifies a plain (no-costume) boss keeps
// a clean name with no trailing parenthesised costume and an empty costumeName.
func TestRaidTranslate_NoCostumeUnchanged(t *testing.T) {
	e := raidTestEnricher()
	raid := &webhook.RaidWebhook{PokemonID: 6, Form: 0, Costume: 0, Evolution: 0}
	m := e.RaidTranslate(map[string]any{"gameWeatherId": 0}, raid, "en")

	if fullName, _ := m["fullName"].(string); strings.Contains(fullName, "(") {
		t.Errorf("fullName = %q, want no parenthesised costume for a plain boss", fullName)
	}
	if got := m["costumeName"]; got != "" {
		t.Errorf("costumeName = %v, want empty for a no-costume boss", got)
	}
}
