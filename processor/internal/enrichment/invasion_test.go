package enrichment

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// newInvasionEnricher builds a minimal Enricher suitable for invasion tests.
// Only GameData, Translations, and WeatherProvider are set.
func newInvasionEnricher(t *testing.T, gd *gamedata.GameData, bundle *i18n.Bundle) *Enricher {
	t.Helper()
	return &Enricher{
		TimeLayout:      "15:04:05",
		WeatherProvider: &mockWeather{},
		GameData:        gd,
		Translations:    bundle,
	}
}

// newInvasionBundle creates a Bundle from the given per-language translation maps.
func newInvasionBundle(t *testing.T, translations map[string]map[string]string) *i18n.Bundle {
	t.Helper()
	dir := t.TempDir()

	for lang, kv := range translations {
		// Build JSON manually
		var parts []string
		for k, v := range kv {
			parts = append(parts, `"`+k+`": "`+v+`"`)
		}
		data := []byte("{" + strings.Join(parts, ", ") + "}")
		if err := os.WriteFile(filepath.Join(dir, lang+".json"), data, 0644); err != nil {
			t.Fatal(err)
		}
	}

	b := i18n.NewBundle()
	if err := b.LoadJSONDir(dir); err != nil {
		t.Fatal(err)
	}
	return b
}

// --- Test 1: Base grunt type color and emoji ---

func TestInvasionBaseGruntTypeColorAndEmoji(t *testing.T) {
	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{},
		Grunts: map[int]*gamedata.Grunt{
			44: {
				ID:         44,
				TypeID:     12,
				Gender:     1,
				CategoryID: 2,
				Team:       [3][]gamedata.GruntEncounterEntry{},
			},
		},
		Types: map[int]*gamedata.TypeInfo{
			12: {TypeID: 12, Color: "6AAD38", Emoji: "poke_type_grass"},
		},
		Util: &gamedata.UtilData{
			PokestopEvent: map[int]gamedata.EventInfo{},
		},
	}

	e := newInvasionEnricher(t, gd, newTestBundle())
	m, _ := e.Invasion(52.52, 13.40, 0, "stop1", 44, 0, 0, TileModeURL)

	if m["gruntTypeColor"] != "6AAD38" {
		t.Errorf("gruntTypeColor = %q, want %q", m["gruntTypeColor"], "6AAD38")
	}
	if m["gruntTypeEmojiKey"] != "poke_type_grass" {
		t.Errorf("gruntTypeEmojiKey = %q, want %q", m["gruntTypeEmojiKey"], "poke_type_grass")
	}
	if m["gruntTypeID"] != 12 {
		t.Errorf("gruntTypeID = %v, want 12", m["gruntTypeID"])
	}
	if m["gruntGender"] != 1 {
		t.Errorf("gruntGender = %v, want 1", m["gruntGender"])
	}
}

// --- Test 2: Event invasion uses PokestopEvent data ---

func TestInvasionBaseEventInvasion(t *testing.T) {
	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{},
		Grunts:   map[int]*gamedata.Grunt{},
		Types:    map[int]*gamedata.TypeInfo{},
		Util: &gamedata.UtilData{
			PokestopEvent: map[int]gamedata.EventInfo{
				7: {Name: "Showcase", Color: "AABBCC", Emoji: "pokestop_event_showcase"},
			},
		},
	}

	e := newInvasionEnricher(t, gd, newTestBundle())
	m, _ := e.Invasion(52.52, 13.40, 0, "stop2", 0, 7, 0, TileModeURL)

	if m["gruntTypeColor"] != "AABBCC" {
		t.Errorf("gruntTypeColor = %q, want %q", m["gruntTypeColor"], "AABBCC")
	}
	if m["gruntTypeEmojiKey"] != "pokestop_event_showcase" {
		t.Errorf("gruntTypeEmojiKey = %q, want %q", m["gruntTypeEmojiKey"], "pokestop_event_showcase")
	}
}

// --- Test 3: Translate grunt name (English) ---

func TestInvasionTranslateGruntName(t *testing.T) {
	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{},
		Grunts: map[int]*gamedata.Grunt{
			44: {
				ID:         44,
				TypeID:     12,
				Gender:     0,
				CategoryID: 2,
				Team:       [3][]gamedata.GruntEncounterEntry{},
			},
		},
		Types: map[int]*gamedata.TypeInfo{},
		Util: &gamedata.UtilData{
			Genders: map[int]gamedata.GenderInfo{},
		},
	}

	bundle := newInvasionBundle(t, map[string]map[string]string{
		"en": {
			"character_category_2": "Grunt",
			"poke_type_12":        "Grass",
		},
	})

	e := newInvasionEnricher(t, gd, bundle)
	base := map[string]any{"gameWeatherId": 0, "gruntGender": 0}
	m := e.InvasionTranslate(base, 44, nil, "en")

	if m["gruntName"] != "Grunt" {
		t.Errorf("gruntName = %q, want %q", m["gruntName"], "Grunt")
	}
	if m["gruntTypeName"] != "Grass" {
		t.Errorf("gruntTypeName = %q, want %q", m["gruntTypeName"], "Grass")
	}
}

// --- Test 4: Translate grunt name (German) ---

func TestInvasionTranslateGruntNameGerman(t *testing.T) {
	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{},
		Grunts: map[int]*gamedata.Grunt{
			44: {
				ID:         44,
				TypeID:     12,
				Gender:     0,
				CategoryID: 2,
				Team:       [3][]gamedata.GruntEncounterEntry{},
			},
		},
		Types: map[int]*gamedata.TypeInfo{},
		Util: &gamedata.UtilData{
			Genders: map[int]gamedata.GenderInfo{},
		},
	}

	bundle := newInvasionBundle(t, map[string]map[string]string{
		"de": {
			"character_category_2": "Ruepel",
			"poke_type_12":        "Pflanze",
		},
	})

	e := newInvasionEnricher(t, gd, bundle)
	base := map[string]any{"gameWeatherId": 0, "gruntGender": 0}
	m := e.InvasionTranslate(base, 44, nil, "de")

	if m["gruntName"] != "Ruepel" {
		t.Errorf("gruntName = %q, want %q", m["gruntName"], "Ruepel")
	}
	if m["gruntTypeName"] != "Pflanze" {
		t.Errorf("gruntTypeName = %q, want %q", m["gruntTypeName"], "Pflanze")
	}
}

// --- Test 5: Translate gender ---

func TestInvasionTranslateGender(t *testing.T) {
	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{},
		Grunts: map[int]*gamedata.Grunt{
			44: {
				ID:         44,
				TypeID:     0,
				Gender:     1,
				CategoryID: 2,
				Team:       [3][]gamedata.GruntEncounterEntry{},
			},
		},
		Types: map[int]*gamedata.TypeInfo{},
		Util: &gamedata.UtilData{
			Genders: map[int]gamedata.GenderInfo{
				1: {Name: "Male", Emoji: "gender_male"},
			},
		},
	}

	bundle := newInvasionBundle(t, map[string]map[string]string{
		"en": {
			"character_category_2": "Grunt",
			"Male":                "Male",
		},
	})

	e := newInvasionEnricher(t, gd, bundle)
	base := map[string]any{"gameWeatherId": 0, "gruntGender": 1}
	m := e.InvasionTranslate(base, 44, nil, "en")

	if m["genderName"] != "Male" {
		t.Errorf("genderName = %q, want %q", m["genderName"], "Male")
	}
	if m["genderEmojiKey"] != "gender_male" {
		t.Errorf("genderEmojiKey = %q, want %q", m["genderEmojiKey"], "gender_male")
	}
}

// --- Test 6: Rewards with two slots (85%/15%) ---

func TestInvasionTranslateRewardsTwoSlots(t *testing.T) {
	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 650, Form: 0}:    {PokemonID: 650, FormID: 0},
			{ID: 598, Form: 2186}: {PokemonID: 598, FormID: 2186},
			{ID: 652, Form: 0}:    {PokemonID: 652, FormID: 0},
		},
		Grunts: map[int]*gamedata.Grunt{
			44: {
				ID:         44,
				TypeID:     12,
				Gender:     1,
				CategoryID: 2,
				Rewards:    []int{0, 1, 2},
				Team: [3][]gamedata.GruntEncounterEntry{
					{{ID: 650, FormID: 0}},
					{{ID: 598, FormID: 2186}},
					{{ID: 652, FormID: 0}},
				},
			},
		},
		Types: map[int]*gamedata.TypeInfo{},
		Util: &gamedata.UtilData{
			Genders:  map[int]gamedata.GenderInfo{},
			MegaName: map[int]string{},
		},
	}

	bundle := newInvasionBundle(t, map[string]map[string]string{
		"en": {
			"character_category_2": "Grunt",
			"poke_type_12":        "Grass",
			"poke_650":            "Chespin",
			"poke_598":            "Ferrothorn",
			"poke_652":            "Chesnaught",
			"form_0":              "Normal",
			"form_2186":           "Normal",
		},
	})

	e := newInvasionEnricher(t, gd, bundle)
	base := map[string]any{"gameWeatherId": 0, "gruntGender": 1}
	m := e.InvasionTranslate(base, 44, nil, "en")

	rewardsList, ok := m["gruntRewardsList"].(map[string]any)
	if !ok {
		t.Fatal("gruntRewardsList not set or wrong type")
	}

	// First slot: 85%
	firstSlot, ok := rewardsList["first"].(map[string]any)
	if !ok {
		t.Fatal("gruntRewardsList.first not set")
	}
	if firstSlot["chance"] != 85 {
		t.Errorf("first chance = %v, want 85", firstSlot["chance"])
	}
	monsters0, ok := firstSlot["monsters"].([]map[string]any)
	if !ok || len(monsters0) == 0 {
		t.Fatal("first monsters not set")
	}
	if monsters0[0]["id"] != 650 {
		t.Errorf("first monster id = %v, want 650", monsters0[0]["id"])
	}
	if monsters0[0]["fullName"] != "Chespin" {
		t.Errorf("first monster fullName = %q, want %q", monsters0[0]["fullName"], "Chespin")
	}

	// Second slot: 15%
	secondSlot, ok := rewardsList["second"].(map[string]any)
	if !ok {
		t.Fatal("gruntRewardsList.second not set")
	}
	if secondSlot["chance"] != 15 {
		t.Errorf("second chance = %v, want 15", secondSlot["chance"])
	}
	monsters1, ok := secondSlot["monsters"].([]map[string]any)
	if !ok || len(monsters1) == 0 {
		t.Fatal("second monsters not set")
	}
	if monsters1[0]["id"] != 598 {
		t.Errorf("slot 1 monster id = %v, want 598", monsters1[0]["id"])
	}
	if monsters1[0]["fullName"] != "Ferrothorn" {
		t.Errorf("slot 1 monster fullName = %q, want %q", monsters1[0]["fullName"], "Ferrothorn")
	}

	// Rewards text should contain percentages
	rewardsText, _ := m["gruntRewards"].(string)
	if !strings.Contains(rewardsText, "85%") {
		t.Errorf("gruntRewards should contain 85%%, got %q", rewardsText)
	}
	if !strings.Contains(rewardsText, "15%") {
		t.Errorf("gruntRewards should contain 15%%, got %q", rewardsText)
	}
}

// --- Test 7: Single slot rewards (100%, no percentage shown) ---

func TestInvasionTranslateRewardsSingleSlot(t *testing.T) {
	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 25, Form: 0}: {PokemonID: 25, FormID: 0},
		},
		Grunts: map[int]*gamedata.Grunt{
			44: {
				ID:         44,
				TypeID:     0,
				Gender:     0,
				CategoryID: 2,
				Rewards:    []int{0},
				Team: [3][]gamedata.GruntEncounterEntry{
					{{ID: 25, FormID: 0}},
				},
			},
		},
		Types: map[int]*gamedata.TypeInfo{},
		Util: &gamedata.UtilData{
			Genders:  map[int]gamedata.GenderInfo{},
			MegaName: map[int]string{},
		},
	}

	bundle := newInvasionBundle(t, map[string]map[string]string{
		"en": {
			"character_category_2": "Grunt",
			"poke_25":             "Pikachu",
			"form_0":              "Normal",
		},
	})

	e := newInvasionEnricher(t, gd, bundle)
	base := map[string]any{"gameWeatherId": 0, "gruntGender": 0}
	m := e.InvasionTranslate(base, 44, nil, "en")

	rewardsList, ok := m["gruntRewardsList"].(map[string]any)
	if !ok {
		t.Fatal("gruntRewardsList not set or wrong type")
	}
	firstSlot, ok := rewardsList["first"].(map[string]any)
	if !ok {
		t.Fatal("gruntRewardsList.first not set")
	}
	_ = firstSlot
	if firstSlot["chance"] != 100 {
		t.Errorf("slot 0 chance = %v, want 100", firstSlot["chance"])
	}

	// Single slot should NOT contain "%" in the text
	rewardsText, _ := m["gruntRewards"].(string)
	if strings.Contains(rewardsText, "%") {
		t.Errorf("gruntRewards for single slot should not contain %%, got %q", rewardsText)
	}
	if !strings.Contains(rewardsText, "Pikachu") {
		t.Errorf("gruntRewards should contain Pikachu, got %q", rewardsText)
	}
}

// --- Test 8: Third slot fallback ---

func TestInvasionTranslateRewardsThirdSlotFallback(t *testing.T) {
	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 1, Form: 0}: {PokemonID: 1, FormID: 0},
			{ID: 3, Form: 0}: {PokemonID: 3, FormID: 0},
		},
		Grunts: map[int]*gamedata.Grunt{
			44: {
				ID:         44,
				TypeID:     0,
				Gender:     0,
				CategoryID: 2,
				Rewards:    []int{0, 2}, // no slot 1, has slot 2
				Team: [3][]gamedata.GruntEncounterEntry{
					{{ID: 1, FormID: 0}},
					{}, // empty second slot
					{{ID: 3, FormID: 0}},
				},
			},
		},
		Types: map[int]*gamedata.TypeInfo{},
		Util: &gamedata.UtilData{
			Genders:  map[int]gamedata.GenderInfo{},
			MegaName: map[int]string{},
		},
	}

	bundle := newInvasionBundle(t, map[string]map[string]string{
		"en": {
			"character_category_2": "Grunt",
			"poke_1":              "Bulbasaur",
			"poke_3":              "Venusaur",
			"form_0":              "Normal",
		},
	})

	e := newInvasionEnricher(t, gd, bundle)
	base := map[string]any{"gameWeatherId": 0, "gruntGender": 0}
	m := e.InvasionTranslate(base, 44, nil, "en")

	rewardsList, ok := m["gruntRewardsList"].(map[string]any)
	if !ok {
		t.Fatal("gruntRewardsList not set or wrong type")
	}
	firstSlot, ok := rewardsList["first"].(map[string]any)
	if !ok {
		t.Fatal("gruntRewardsList.first not set")
	}
	_ = firstSlot
	if firstSlot["chance"] != 100 {
		t.Errorf("slot 0 chance = %v, want 100", firstSlot["chance"])
	}

	// Should use third slot pokemon (ID: 3), not first slot
	monsters, ok := firstSlot["monsters"].([]map[string]any)
	if !ok || len(monsters) == 0 {
		t.Fatal("monsters not set")
	}
	if monsters[0]["id"] != 3 {
		t.Errorf("monster id = %v, want 3 (third slot)", monsters[0]["id"])
	}
	if monsters[0]["fullName"] != "Venusaur" {
		t.Errorf("monster fullName = %q, want %q", monsters[0]["fullName"], "Venusaur")
	}
}

// --- Test 9: No grunt (gruntTypeID=0) ---

func TestInvasionTranslateNoGrunt(t *testing.T) {
	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{},
		Grunts:   map[int]*gamedata.Grunt{},
		Types:    map[int]*gamedata.TypeInfo{},
		Util: &gamedata.UtilData{
			Genders: map[int]gamedata.GenderInfo{},
		},
	}

	bundle := newInvasionBundle(t, map[string]map[string]string{
		"en": {},
	})

	e := newInvasionEnricher(t, gd, bundle)
	base := map[string]any{"gameWeatherId": 0, "gruntGender": 0}
	m := e.InvasionTranslate(base, 0, nil, "en")

	if _, ok := m["gruntRewardsList"]; ok {
		t.Error("gruntRewardsList should not be set when no grunt found")
	}
	if _, ok := m["gruntName"]; ok {
		t.Error("gruntName should not be set when no grunt found")
	}
}
