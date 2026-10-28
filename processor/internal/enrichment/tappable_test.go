package enrichment

import (
	"strings"
	"testing"
	"time"

	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/uicons"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func tappableTestHook(rewardsJSON string) *webhook.TappableWebhook {
	return &webhook.TappableWebhook{
		EncounterID:           "16082073574809335024",
		SpawnpointID:          "476daa23b5b",
		PokestopID:            "None",
		Latitude:              48.16,
		Longitude:             16.41,
		Type:                  "TAPPABLE_TYPE_BOX",
		Rewards:               []byte(rewardsJSON),
		DisappearTime:         time.Now().Add(10 * time.Minute).Unix(),
		DisappearTimeVerified: true,
	}
}

func TestTappableBase_CandyReward(t *testing.T) {
	e := newTestEnricher()
	tw := tappableTestHook(`[{"pokemon_candy":25,"count":6}]`)
	rewards := matching.ParseTappableRewards(tw)
	m, _ := e.Tappable(tw, rewards, TileModeSkip)

	want := map[string]any{
		"encounter_id":            "16082073574809335024",
		"spawnpoint_id":           "476daa23b5b",
		"tappable_type":           "TAPPABLE_TYPE_BOX",
		"rewardType":              "pokemon_candy",
		"rewardTypeId":            matching.TappableRewardCandy,
		"reward":                  25,
		"rewardAmount":            6,
		"rewardPokemonId":         25,
		"rewardItemId":            0,
		"rewardCount":             1,
		"disappear_time_verified": true,
	}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s = %v, want %v", k, m[k], v)
		}
	}
	if m["pokestop_id"] != "" {
		t.Errorf(`pokestop_id = %v, want "" (shadows the raw "None")`, m["pokestop_id"])
	}
	if _, ok := m["tth"]; !ok {
		t.Error("tth missing for a known disappear time")
	}
	if s, _ := m["disappearTime"].(string); s == "" {
		t.Error("disappearTime missing")
	}
	if img, _ := m["imgUrl"].(string); !strings.Contains(img, "/reward/candy/") {
		t.Errorf("imgUrl = %q, want a candy reward icon", img)
	}
	if _, ok := m["reactMapUrl"]; ok {
		t.Error("a spawn tappable has no map entity; reactMapUrl must not be set")
	}
	if m["googleMapUrl"] == nil {
		t.Error("googleMapUrl missing")
	}
	list, _ := m["rewards"].([]map[string]any)
	if len(list) != 1 || list[0]["type"] != "pokemon_candy" || list[0]["pokemonId"] != 25 {
		t.Errorf("rewards = %v", m["rewards"])
	}
}

func TestTappableBase_UnknownExpiryAndPokestop(t *testing.T) {
	e := newTestEnricher()
	tw := tappableTestHook(`[{"item":1301,"count":1}]`)
	tw.DisappearTime = 0
	tw.SpawnpointID = "None"
	tw.PokestopID = "stop1"
	name := "Fountain"
	tw.PokestopName = &name
	m, _ := e.Tappable(tw, matching.ParseTappableRewards(tw), TileModeSkip)

	for _, k := range []string{"disappearTime", "disappear_time", "time"} {
		if _, ok := m[k]; ok {
			t.Errorf("%s set, want absent (unknown expiry)", k)
		}
	}
	// Unknown expiry still gets a default tth (Golbat's 20 minute fallback)
	// so a clean rule's message has a TTL instead of being dropped.
	if tth, ok := m["tth"].(geo.TTH); !ok || tth != (geo.TTH{Minutes: 20}) {
		t.Errorf("tth = %#v, want geo.TTH{Minutes: 20}", m["tth"])
	}
	if m["spawnpoint_id"] != "" {
		t.Errorf(`spawnpoint_id = %v, want "" (shadows the raw "None")`, m["spawnpoint_id"])
	}
	if m["pokestop_id"] != "stop1" || m["pokestop_name"] != "Fountain" {
		t.Errorf("pokestop = %v / %v", m["pokestop_id"], m["pokestop_name"])
	}
	if m["reward"] != 1301 || m["rewardItemId"] != 1301 {
		t.Errorf("reward = %v, rewardItemId = %v, want 1301", m["reward"], m["rewardItemId"])
	}
}

func TestTappableIconURLs(t *testing.T) {
	e := newTestEnricher()
	for _, tc := range []struct {
		rewards []matching.TappableRewardData
		want    string
	}{
		{[]matching.TappableRewardData{{Type: matching.TappableRewardItem, ItemID: 1, Amount: 1}}, "/reward/item/"},
		{[]matching.TappableRewardData{{Type: matching.TappableRewardStardust, Amount: 500}}, "/reward/stardust/"},
		{[]matching.TappableRewardData{{Type: matching.TappableRewardMegaEnergy, PokemonID: 6, Amount: 20}}, "/reward/mega_resource/"},
	} {
		m := map[string]any{}
		e.addTappableIconURLs(m, tc.rewards)
		if img, _ := m["imgUrl"].(string); !strings.Contains(img, tc.want) {
			t.Errorf("reward %+v: imgUrl = %q, want it to contain %q", tc.rewards[0], img, tc.want)
		}
	}
	xl := &Enricher{ImgUicons: uicons.NewWithIndex("https://icons.test", "png", &uicons.Index{
		RewardXlCandy: map[string]bool{"25.png": true, "0.png": true},
	})}
	m := map[string]any{}
	xl.addTappableIconURLs(m, []matching.TappableRewardData{{Type: matching.TappableRewardXLCandy, PokemonID: 25, Amount: 1}})
	if img, _ := m["imgUrl"].(string); !strings.Contains(img, "/reward/xl_candy/") {
		t.Errorf("xl candy imgUrl = %q", img)
	}
	// No index entry -> no URL: imgUrl stays unset for the fallback image.
	m = map[string]any{}
	(&Enricher{ImgUicons: uicons.NewWithIndex("https://icons.test", "png", nil)}).addTappableIconURLs(m,
		[]matching.TappableRewardData{{Type: matching.TappableRewardXLCandy, PokemonID: 25, Amount: 1}})
	if v, ok := m["imgUrl"]; ok {
		t.Errorf("imgUrl = %v, want unset when the icon set has no URL", v)
	}
	m = map[string]any{}
	e.addTappableIconURLs(m, []matching.TappableRewardData{{Key: "experience", Amount: 100}})
	if _, ok := m["imgUrl"]; ok {
		t.Error("an untrackable reward must not set an icon")
	}
}

func TestTappableTranslate_RewardStringAndTypeName(t *testing.T) {
	bundle := newInvasionBundle(t, map[string]map[string]string{
		"en": {
			"poke_25":           "Pikachu",
			"poke_6":            "Charizard",
			"item_1301":         "Rare Candy",
			"quest_reward_3":    "Stardust",
			"quest_reward_4":    "Candy",
			"quest_reward_12":   "Mega Energy",
			"tappable.type.box": "Box",
		},
		"de": {
			"poke_25":           "Pikachu",
			"quest_reward_4":    "Bonbon",
			"tappable.type.box": "Kiste",
		},
	})
	e := newInvasionEnricher(t, nil, bundle)
	tw := tappableTestHook(`[{"pokemon_candy":25,"count":6},{"item":1301,"count":2},{"stardust":true,"count":250},
		{"xl_candy":25,"count":1},{"mega_energy_pokemon_id":6,"count":20},{"experience":true,"count":100}]`)
	tw.Type = "TAPPABLE_TYPE_ROUTE_PIN"
	rewards := matching.ParseTappableRewards(tw)
	base, _ := e.Tappable(tw, rewards, TileModeSkip)

	m := e.TappableTranslate(base, tw, rewards, "en")
	wantString := "6 Pikachu Candy, 2 Rare Candy, 250 Stardust, 1 Pikachu XL Candy, 20 Charizard Mega Energy, 100 Experience"
	if m["rewardString"] != wantString {
		t.Errorf("rewardString = %q, want %q", m["rewardString"], wantString)
	}
	if m["rewardName"] != "Pikachu" {
		t.Errorf("rewardName = %v, want Pikachu", m["rewardName"])
	}
	// No tappable.type.route_pin key in this bundle: prettified fallback.
	if m["tappableTypeName"] != "Route Pin" {
		t.Errorf("tappableTypeName = %v, want Route Pin", m["tappableTypeName"])
	}
	list, _ := m["rewards"].([]map[string]any)
	if len(list) != 6 || list[1]["text"] != "2 Rare Candy" || list[1]["name"] != "Rare Candy" {
		t.Errorf("rewards = %v", m["rewards"])
	}

	tw.Type = "TAPPABLE_TYPE_BOX"
	de := e.TappableTranslate(base, tw, rewards[:1], "de")
	if de["rewardString"] != "6 Pikachu Bonbon" || de["rewardStringEng"] != "6 Pikachu Candy" {
		t.Errorf("de rewardString = %q / eng %q", de["rewardString"], de["rewardStringEng"])
	}
	if de["tappableTypeName"] != "Kiste" || de["tappableTypeNameEng"] != "Box" {
		t.Errorf("de tappableTypeName = %v / eng %v", de["tappableTypeName"], de["tappableTypeNameEng"])
	}
}

func TestTappableTypeName_Fallbacks(t *testing.T) {
	bundle := newInvasionBundle(t, map[string]map[string]string{"en": {}})
	tr := bundle.For("en")
	for in, want := range map[string]string{
		"TAPPABLE_TYPE_BREAKFAST": "Breakfast",
		"TAPPABLE_TYPE_MAPLE":     "Maple",
		"SOMETHING_NEW":           "Something New",
		"":                        "",
	} {
		if got := TappableTypeName(tr, in); got != want {
			t.Errorf("TappableTypeName(%q) = %q, want %q", in, got, want)
		}
	}
}
