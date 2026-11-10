package enrichment

import (
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// {{ex}} was aliased to is_ex_raid_eligible, but Golbat sends
// ex_raid_eligible, so it was always empty (#272). Either flag sets it, as
// in matching.
func TestRaidExFromEitherFlag(t *testing.T) {
	e := newTestEnricher()
	for name, raid := range map[string]*webhook.RaidWebhook{
		"ex_raid_eligible":    {ExRaidEligible: true},
		"is_ex_raid_eligible": {IsExRaidEligible: true},
		"neither":             {},
	} {
		raid.Level = 5
		raid.Latitude, raid.Longitude = 52.5, 13.4
		raid.End = time.Now().Add(30 * time.Minute).Unix()
		m, _ := e.Raid(raid, true, TileModeURL)
		want := name != "neither"
		if m["ex"] != want {
			t.Errorf("%s: ex = %v, want %v", name, m["ex"], want)
		}
	}
}

// oldTeamEmoji was never set and previousControlTeamEmoji carried the old
// team's emoji (#272). Each now comes from its own team.
func TestGymTeamEmojiKeys(t *testing.T) {
	gd := &gamedata.GameData{Util: &gamedata.UtilData{Teams: map[int]gamedata.TeamInfo{
		1: {Emoji: "team-mystic"},
		2: {Emoji: "team-valor"},
		3: {Emoji: "team-instinct"},
	}}}
	e := newInvasionEnricher(t, gd, newInvasionBundle(t, map[string]map[string]string{"en": {}}))

	// Mystic → Valor, last owner Instinct.
	m := e.GymTranslate(map[string]any{}, 0, 0, 2, 1, 3, "en")
	if m["oldTeamEmojiKey"] != "team-mystic" {
		t.Errorf("oldTeamEmojiKey = %v, want team-mystic", m["oldTeamEmojiKey"])
	}
	if m["previousControlTeamEmojiKey"] != "team-instinct" {
		t.Errorf("previousControlTeamEmojiKey = %v, want team-instinct", m["previousControlTeamEmojiKey"])
	}
}

// showcaseFocusEmoji was a raw string from a util.json block the embedded
// copy didn't have, so it was always empty and couldn't be themed (#272).
// Enrichment now emits an emoji key for the emoji layer to resolve.
func TestShowcaseFocusEmojiKey(t *testing.T) {
	gd := &gamedata.GameData{
		Types: map[int]*gamedata.TypeInfo{},
		Util: &gamedata.UtilData{ShowcaseFocus: map[string]gamedata.ShowcaseFocusInfo{
			"type": {Emoji: "showcase-focus-type"},
		}},
	}
	e := newInvasionEnricher(t, gd, newInvasionBundle(t, map[string]map[string]string{"en": {"showcase_focus_type": "Type"}}))
	m := e.ShowcaseFocusTranslate([]byte(`{"type":"type","pokemon_type_1":9}`), "en")
	if m["showcaseFocusEmojiKey"] != "showcase-focus-type" {
		t.Errorf("showcaseFocusEmojiKey = %v, want showcase-focus-type", m["showcaseFocusEmojiKey"])
	}
}
