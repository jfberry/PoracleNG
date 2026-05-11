package dts

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/i18n"
)

func TestBuildQuestSummaryView_BasicShape(t *testing.T) {
	bundle := i18n.NewBundle()
	tr := bundle.For("en")

	pokestops := []map[string]any{
		{
			"pokestopName": "Statue One",
			"latitude":     50.0,
			"longitude":    14.0,
			"imgUrl":       "https://example.com/icon.png",
		},
		{
			"pokestopName": "Statue Two",
			"latitude":     50.1,
			"longitude":    14.1,
			"imgUrl":       "https://example.com/icon.png",
		},
	}

	view := BuildQuestSummaryView(QuestSummaryGroup{RewardType: 7, RewardID: 327, Quests: pokestops}, nil, tr)

	if got := view["rewardType"]; got != 7 {
		t.Errorf("rewardType = %v, want 7", got)
	}
	if got := view["reward"]; got != 327 {
		t.Errorf("reward = %v, want 327", got)
	}
	if got := view["count"]; got != 2 {
		t.Errorf("count = %v, want 2", got)
	}
	if got, ok := view["imgUrl"].(string); !ok || got != "https://example.com/icon.png" {
		t.Errorf("imgUrl = %v, want shared icon URL", got)
	}
	quests, ok := view["quests"].([]map[string]any)
	if !ok {
		t.Fatalf("quests is not []map[string]any: %T", view["quests"])
	}
	if len(quests) != 2 {
		t.Errorf("len(quests) = %d, want 2", len(quests))
	}
	// staticMap is empty without a resolver — that's fine for this case.
	if _, ok := view["staticMap"]; !ok {
		t.Errorf("staticMap key missing")
	}
}

func TestBuildQuestSummaryView_EmptyInputDoesNotPanic(t *testing.T) {
	bundle := i18n.NewBundle()
	tr := bundle.For("en")

	view := BuildQuestSummaryView(QuestSummaryGroup{RewardType: 3, RewardID: 100, Quests: nil}, nil, tr)
	if got := view["count"]; got != 0 {
		t.Errorf("count = %v, want 0", got)
	}
	if got := view["imgUrl"]; got != "" {
		t.Errorf("imgUrl = %v, want empty", got)
	}
	if got := view["staticMap"]; got != "" {
		t.Errorf("staticMap = %v, want empty (nil resolver)", got)
	}
	if got, _ := view["quests"].([]map[string]any); len(got) != 0 {
		t.Errorf("quests = %v, want empty", got)
	}
}

func TestBuildQuestSummaryView_RewardName(t *testing.T) {
	bundle := i18n.NewBundle()
	en := bundle.For("en")

	// Stardust: reward field is the dust amount; quest_reward_3 is the
	// label. Our empty bundle returns the key verbatim, so we check that
	// the amount is interpolated.
	view := BuildQuestSummaryView(QuestSummaryGroup{RewardType: 3, RewardID: 1500, Quests: nil}, nil, en)
	name, _ := view["rewardName"].(string)
	if name == "" || name == "quest_reward_3" {
		t.Errorf("expected stardust reward name to include amount, got %q", name)
	}

	// Item: rewardName comes from item_701 lookup. Empty bundle returns
	// "item_701" verbatim; we just want it to be the translation key, not
	// blank.
	view = BuildQuestSummaryView(QuestSummaryGroup{RewardType: 2, RewardID: 701, Quests: nil}, nil, en)
	if got, _ := view["rewardName"].(string); got != "item_701" {
		t.Errorf("item rewardName = %q, want \"item_701\"", got)
	}

	// Pokemon: PokemonTranslationKey(327) → poke_327.
	view = BuildQuestSummaryView(QuestSummaryGroup{RewardType: 7, RewardID: 327, Quests: nil}, nil, en)
	if got, _ := view["rewardName"].(string); got != "poke_327" {
		t.Errorf("pokemon rewardName = %q, want \"poke_327\"", got)
	}

	// rewardForm round-trips into the view so a template can branch on it.
	view = BuildQuestSummaryView(QuestSummaryGroup{RewardType: 7, RewardID: 327, RewardForm: 1, Quests: nil}, nil, en)
	if got := view["rewardForm"]; got != 1 {
		t.Errorf("rewardForm = %v, want 1", got)
	}

	// Pokemon with form, translations present: enrichment-style concat —
	// "<pokemon> <form>" with no parens, matching the per-row fullName.
	// Uses real gamelocale shape: Spinda form 37 translates to bare "01"
	// (pogo-translations doesn't embed the species name in form values),
	// so the joined result is "Spinda 01", not "Spinda Spinda 01".
	bundle2 := i18n.NewBundle()
	bundle2.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"poke_327":        "Spinda",
		"form_37":         "01",
		"quest_reward_4":  "Candy",
		"quest_reward_12": "Mega Energy",
	}))
	tr2 := bundle2.For("en")
	view = BuildQuestSummaryView(QuestSummaryGroup{RewardType: 7, RewardID: 327, RewardForm: 37, Quests: nil}, nil, tr2)
	if got, _ := view["rewardName"].(string); got != "Spinda 01" {
		t.Errorf("pokemon rewardName with form = %q, want %q", got, "Spinda 01")
	}

	// Pokemon with "Normal" form name: the normal-form filter strips it so
	// we don't end up with "Spinda Normal".
	bundle3 := i18n.NewBundle()
	bundle3.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"poke_327": "Spinda",
		"form_0":   "Normal",
	}))
	view = BuildQuestSummaryView(QuestSummaryGroup{RewardType: 7, RewardID: 327, RewardForm: 0, Quests: nil}, nil, bundle3.For("en"))
	if got, _ := view["rewardName"].(string); got != "Spinda" {
		t.Errorf("pokemon rewardName with form 0 = %q, want \"Spinda\"", got)
	}

	// Candy (type 4) appends the translated Candy label and ignores form
	// even when supplied — candy is per-species.
	view = BuildQuestSummaryView(QuestSummaryGroup{RewardType: 4, RewardID: 327, RewardForm: 37, Quests: nil}, nil, tr2)
	if got, _ := view["rewardName"].(string); got != "Spinda Candy" {
		t.Errorf("candy rewardName = %q, want \"Spinda Candy\"", got)
	}

	// Mega energy (type 12) appends the translated Mega Energy label.
	view = BuildQuestSummaryView(QuestSummaryGroup{RewardType: 12, RewardID: 327, Quests: nil}, nil, tr2)
	if got, _ := view["rewardName"].(string); got != "Spinda Mega Energy" {
		t.Errorf("mega energy rewardName = %q, want \"Spinda Mega Energy\"", got)
	}
}

func TestBuildQuestSummaryView_ZeroCoordSkippedInStaticMap(t *testing.T) {
	bundle := i18n.NewBundle()
	tr := bundle.For("en")

	// All-zero coords: even with a resolver we never call the autoposition
	// helper because no markers survive filtering. We don't assert against
	// an actual resolver here (would require fixtures); instead we ensure
	// no panic and empty staticMap when resolver is nil.
	pokestops := []map[string]any{
		{"pokestopName": "Lost Stop", "latitude": 0.0, "longitude": 0.0},
	}
	view := BuildQuestSummaryView(QuestSummaryGroup{RewardType: 7, RewardID: 25, Quests: pokestops}, nil, tr)
	if got := view["staticMap"]; got != "" {
		t.Errorf("staticMap = %v, want empty when only zero coords", got)
	}
}
