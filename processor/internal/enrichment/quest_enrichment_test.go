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
