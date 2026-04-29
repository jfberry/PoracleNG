package enrichment

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// newQuestEnricher builds a minimal Enricher suitable for quest translation tests.
func newQuestEnricher(t *testing.T, gd *gamedata.GameData, translations map[string]map[string]string) *Enricher {
	t.Helper()
	bundle := newInvasionBundle(t, translations)
	return &Enricher{
		TimeLayout:      "15:04:05",
		WeatherProvider: &mockWeather{},
		GameData:        gd,
		Translations:    bundle,
	}
}

// --- Test 1: Quest title in English ---

func TestQuestTranslateTitle(t *testing.T) {
	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{},
	}

	e := newQuestEnricher(t, gd, map[string]map[string]string{
		"en": {
			"quest_title_quest_catch_pokemon_plural": "Catch %{amount_0} Pokémon",
		},
	})

	quest := &webhook.QuestWebhook{
		Title:  "quest_catch_pokemon_plural",
		Target: 10,
	}

	base := map[string]any{}

	m := e.QuestTranslate(base, quest, nil, "en")

	if m["questString"] != "Catch 10 Pokémon" {
		t.Errorf("questString = %q, want %q", m["questString"], "Catch 10 Pokémon")
	}
	if m["questStringEng"] != "Catch 10 Pokémon" {
		t.Errorf("questStringEng = %q, want %q", m["questStringEng"], "Catch 10 Pokémon")
	}
}

// --- Test 2: Quest title in German with English fallback ---

func TestQuestTranslateTitleGerman(t *testing.T) {
	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{},
	}

	e := newQuestEnricher(t, gd, map[string]map[string]string{
		"en": {
			"quest_title_quest_catch_pokemon_plural": "Catch %{amount_0} Pokémon",
		},
		"de": {
			"quest_title_quest_catch_pokemon_plural": "Fange %{amount_0} Pokémon.",
		},
	})

	quest := &webhook.QuestWebhook{
		Title:  "quest_catch_pokemon_plural",
		Target: 10,
	}

	base := map[string]any{}

	m := e.QuestTranslate(base, quest, nil, "de")

	if m["questString"] != "Fange 10 Pokémon." {
		t.Errorf("questString = %q, want %q", m["questString"], "Fange 10 Pokémon.")
	}
	if m["questStringEng"] != "Catch 10 Pokémon" {
		t.Errorf("questStringEng = %q, want %q", m["questStringEng"], "Catch 10 Pokémon")
	}
}

// --- Test 3: Pokemon reward ---

func TestQuestTranslatePokemonReward(t *testing.T) {
	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 25, Form: 0}: {PokemonID: 25, FormID: 0},
		},
		Util: &gamedata.UtilData{
			MegaName: map[int]string{},
		},
	}

	e := newQuestEnricher(t, gd, map[string]map[string]string{
		"en": {
			"poke_25": "Pikachu",
			"form_0":  "Normal",
		},
	})

	quest := &webhook.QuestWebhook{
		Title:  "some_quest",
		Target: 1,
	}

	rewards := []matching.QuestRewardData{{Type: 7, PokemonID: 25, FormID: 0}}
	base := map[string]any{}

	m := e.QuestTranslate(base, quest, rewards, "en")

	monsterNames, _ := m["monsterNames"].(string)
	if !strings.Contains(monsterNames, "Pikachu") {
		t.Errorf("monsterNames = %q, want to contain %q", monsterNames, "Pikachu")
	}

	monsterList, ok := m["monsterList"].([]map[string]any)
	if !ok || len(monsterList) == 0 {
		t.Fatal("monsterList not set or empty")
	}
	if monsterList[0]["name"] != "Pikachu" {
		t.Errorf("monsterList[0][name] = %q, want %q", monsterList[0]["name"], "Pikachu")
	}
	if monsterList[0]["fullName"] != "Pikachu" {
		t.Errorf("monsterList[0][fullName] = %q, want %q", monsterList[0]["fullName"], "Pikachu")
	}
}

// --- Test 4: Item reward ---

func TestQuestTranslateItemReward(t *testing.T) {
	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{},
	}

	e := newQuestEnricher(t, gd, map[string]map[string]string{
		"en": {
			"item_701": "Razz Berry",
		},
	})

	quest := &webhook.QuestWebhook{
		Title:  "some_quest",
		Target: 1,
	}

	rewards := []matching.QuestRewardData{{Type: 2, ItemID: 701, Amount: 6}}
	base := map[string]any{}

	m := e.QuestTranslate(base, quest, rewards, "en")

	itemNames, _ := m["itemNames"].(string)
	if itemNames != "6 Razz Berry" {
		t.Errorf("itemNames = %q, want %q", itemNames, "6 Razz Berry")
	}
}

// --- Test 5: Stardust reward ---

func TestQuestTranslateStardustReward(t *testing.T) {
	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{},
	}

	e := newQuestEnricher(t, gd, map[string]map[string]string{
		"en": {
			"quest_reward_3": "Stardust",
		},
	})

	quest := &webhook.QuestWebhook{
		Title:  "some_quest",
		Target: 1,
	}

	rewards := []matching.QuestRewardData{{Type: 3, Amount: 200}}
	base := map[string]any{"dustAmount": 200}

	m := e.QuestTranslate(base, quest, rewards, "en")

	rewardString, _ := m["rewardString"].(string)
	if !strings.Contains(rewardString, "200 Stardust") {
		t.Errorf("rewardString = %q, want to contain %q", rewardString, "200 Stardust")
	}

	dustText, _ := m["dustText"].(string)
	if dustText != "200 Stardust" {
		t.Errorf("dustText = %q, want %q", dustText, "200 Stardust")
	}
}

// --- Test 6: Mega energy reward ---

func TestQuestTranslateMegaEnergyReward(t *testing.T) {
	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{},
	}

	e := newQuestEnricher(t, gd, map[string]map[string]string{
		"en": {
			"poke_9":          "Blastoise",
			"quest_reward_12": "Mega Energy",
		},
	})

	quest := &webhook.QuestWebhook{
		Title:  "some_quest",
		Target: 1,
	}

	rewards := []matching.QuestRewardData{{Type: 12, PokemonID: 9, Amount: 10}}
	base := map[string]any{}

	m := e.QuestTranslate(base, quest, rewards, "en")

	energyNames, _ := m["energyMonstersNames"].(string)
	if energyNames != "10 Blastoise Mega Energy" {
		t.Errorf("energyMonstersNames = %q, want %q", energyNames, "10 Blastoise Mega Energy")
	}
}

// --- Test 7: Combined reward string (pokemon + stardust) ---

func TestQuestTranslateRewardString(t *testing.T) {
	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 25, Form: 0}: {PokemonID: 25, FormID: 0},
		},
		Util: &gamedata.UtilData{
			MegaName: map[int]string{},
		},
	}

	e := newQuestEnricher(t, gd, map[string]map[string]string{
		"en": {
			"poke_25":         "Pikachu",
			"form_0":          "Normal",
			"quest_reward_3":  "Stardust",
		},
	})

	quest := &webhook.QuestWebhook{
		Title:  "some_quest",
		Target: 1,
	}

	rewards := []matching.QuestRewardData{
		{Type: 7, PokemonID: 25, FormID: 0},
		{Type: 3, Amount: 500},
	}
	base := map[string]any{"dustAmount": 500}

	m := e.QuestTranslate(base, quest, rewards, "en")

	rewardString, _ := m["rewardString"].(string)
	if !strings.Contains(rewardString, "Pikachu") {
		t.Errorf("rewardString = %q, want to contain %q", rewardString, "Pikachu")
	}
	if !strings.Contains(rewardString, "500 Stardust") {
		t.Errorf("rewardString = %q, want to contain %q", rewardString, "500 Stardust")
	}
	// Should be joined with ", "
	if !strings.Contains(rewardString, ", ") {
		t.Errorf("rewardString = %q, want parts joined with %q", rewardString, ", ")
	}
}

// --- Condition translation tests ---

// Real captured webhook payload — type 8 (Throw Type) with throw_type_id 12 (Excellent)
// expected to produce "Excellent Throw" via quest_condition_8_formatted.
func TestQuestTranslateConditionThrowType(t *testing.T) {
	gd := &gamedata.GameData{Monsters: map[gamedata.MonsterKey]*gamedata.Monster{}}
	e := newQuestEnricher(t, gd, map[string]map[string]string{
		"en": {
			"quest_condition_8":           "Throw Type",
			"quest_condition_8_formatted": "%{throw_type} Throw",
			"throw_type_12":               "Excellent",
		},
	})
	quest := &webhook.QuestWebhook{
		Conditions: []webhook.QuestCondition{
			{Type: 8, Info: map[string]any{"throw_type_id": float64(12), "hit": false}},
		},
	}
	m := e.QuestTranslate(map[string]any{}, quest, nil, "en")
	if got := m["conditionString"]; got != "Excellent Throw" {
		t.Errorf("conditionString = %q, want %q", got, "Excellent Throw")
	}
	list, _ := m["conditionList"].([]map[string]any)
	if len(list) != 1 || list[0]["formatted"] != "Excellent Throw" || list[0]["name"] != "Throw Type" {
		t.Errorf("conditionList[0] = %+v", list)
	}
}

// Captured shape — type 14 (In a Row) with a "hit:true" payload but no
// throw_type_id. Should fall back to the bare name rather than emit the
// raw "%{throw_type}" placeholder.
func TestQuestTranslateConditionInARowFallback(t *testing.T) {
	gd := &gamedata.GameData{Monsters: map[gamedata.MonsterKey]*gamedata.Monster{}}
	e := newQuestEnricher(t, gd, map[string]map[string]string{
		"en": {
			"quest_condition_14":           "In a Row",
			"quest_condition_14_formatted": "%{throw_type} Throw In a Row",
		},
	})
	quest := &webhook.QuestWebhook{
		Conditions: []webhook.QuestCondition{
			{Type: 14, Info: map[string]any{"hit": true}},
		},
	}
	m := e.QuestTranslate(map[string]any{}, quest, nil, "en")
	if got := m["conditionString"]; got != "In a Row" {
		t.Errorf("conditionString = %q, want bare-name fallback %q", got, "In a Row")
	}
}

// Captured shape — type 1 (Pokemon Type) with pokemon_type_ids list.
// Verifies array placeholders translate via poke_type_{id} and join with
// ", ".
func TestQuestTranslateConditionPokemonType(t *testing.T) {
	gd := &gamedata.GameData{Monsters: map[gamedata.MonsterKey]*gamedata.Monster{}}
	e := newQuestEnricher(t, gd, map[string]map[string]string{
		"en": {
			"quest_condition_1":           "Pokemon Type",
			"quest_condition_1_formatted": "Type(s): %{types}",
			"poke_type_10":                "Fire",
			"poke_type_11":                "Water",
		},
	})
	quest := &webhook.QuestWebhook{
		Conditions: []webhook.QuestCondition{
			{Type: 1, Info: map[string]any{"pokemon_type_ids": []any{float64(10), float64(11)}}},
		},
	}
	m := e.QuestTranslate(map[string]any{}, quest, nil, "en")
	if got := m["conditionString"]; got != "Type(s): Fire, Water" {
		t.Errorf("conditionString = %q, want %q", got, "Type(s): Fire, Water")
	}
}

// Captured shape — type 27 (Invasion Category) with character_category_ids.
// "Catch a Pokemon during a Team Rocket Invasion" — category 2 = Grunt.
func TestQuestTranslateConditionInvasion(t *testing.T) {
	gd := &gamedata.GameData{Monsters: map[gamedata.MonsterKey]*gamedata.Monster{}}
	e := newQuestEnricher(t, gd, map[string]map[string]string{
		"en": {
			"quest_condition_27":           "Invasion Category",
			"quest_condition_27_formatted": "Invasion Category(s): %{categories}",
			"character_category_2":         "Grunt",
		},
	})
	quest := &webhook.QuestWebhook{
		Conditions: []webhook.QuestCondition{
			{Type: 27, Info: map[string]any{"character_category_ids": []any{float64(2)}}},
		},
	}
	m := e.QuestTranslate(map[string]any{}, quest, nil, "en")
	if got := m["conditionString"]; got != "Invasion Category(s): Grunt" {
		t.Errorf("conditionString = %q, want %q", got, "Invasion Category(s): Grunt")
	}
}

// Multiple conditions on a single quest — verify they join in order with ", ".
func TestQuestTranslateConditionMultiple(t *testing.T) {
	gd := &gamedata.GameData{Monsters: map[gamedata.MonsterKey]*gamedata.Monster{}}
	e := newQuestEnricher(t, gd, map[string]map[string]string{
		"en": {
			"quest_condition_8":            "Throw Type",
			"quest_condition_8_formatted":  "%{throw_type} Throw",
			"quest_condition_15":           "Curve Ball",
			"throw_type_11":                "Great",
		},
	})
	quest := &webhook.QuestWebhook{
		Conditions: []webhook.QuestCondition{
			{Type: 8, Info: map[string]any{"throw_type_id": float64(11), "hit": false}},
			{Type: 15, Info: map[string]any{}},
		},
	}
	m := e.QuestTranslate(map[string]any{}, quest, nil, "en")
	if got := m["conditionString"]; got != "Great Throw, Curve Ball" {
		t.Errorf("conditionString = %q, want %q", got, "Great Throw, Curve Ball")
	}
}

// Quests without conditions should not surface conditionString / conditionList.
func TestQuestTranslateConditionEmpty(t *testing.T) {
	gd := &gamedata.GameData{Monsters: map[gamedata.MonsterKey]*gamedata.Monster{}}
	e := newQuestEnricher(t, gd, map[string]map[string]string{"en": {}})
	quest := &webhook.QuestWebhook{}
	m := e.QuestTranslate(map[string]any{}, quest, nil, "en")
	if _, ok := m["conditionString"]; ok {
		t.Errorf("conditionString should be absent when no conditions")
	}
	if _, ok := m["conditionList"]; ok {
		t.Errorf("conditionList should be absent when no conditions")
	}
}
