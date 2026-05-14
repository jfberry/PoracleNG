package rowtext

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// testGenerator creates a Generator with minimal GameData and embedded English translations.
func testGenerator(t *testing.T) *Generator {
	t.Helper()

	bundle := i18n.Load("")

	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 1, Form: 0}:   {PokemonID: 1, FormID: 0, Types: []int{12}},
			{ID: 6, Form: 0}:   {PokemonID: 6, FormID: 0, Types: []int{10, 3}},
			{ID: 6, Form: 65}:  {PokemonID: 6, FormID: 65, Types: []int{10, 17}},
			{ID: 25, Form: 0}:  {PokemonID: 25, FormID: 0, Types: []int{13}},
			{ID: 150, Form: 0}: {PokemonID: 150, FormID: 0, Types: []int{14}},
		},
		Moves: map[int]*gamedata.Move{
			214: {MoveID: 214, TypeID: 12},
			90:  {MoveID: 90, TypeID: 5},
		},
		Items: map[int]*gamedata.Item{
			1301: {},
		},
		Util: &gamedata.UtilData{
			Genders: map[int]gamedata.GenderInfo{
				1: {Emoji: "♂"},
				2: {Emoji: "♀"},
			},
			Lures: map[int]gamedata.LureInfo{
				501: {},
				502: {},
			},
		},
	}

	return &Generator{
		GD:                  gd,
		Translations:        bundle,
		DefaultTemplateName: "1",
	}
}

func TestMonsterRowText_Everything(t *testing.T) {
	g := testGenerator(t)
	tr := g.Translations.For("en")

	m := &db.MonsterTracking{
		PokemonID: 0, Form: 0, Template: "1",
		MinIV: -1, MaxIV: 100, MinCP: 0, MaxCP: 9000,
		MinLevel: 0, MaxLevel: 40, MaxSize: 5, MaxRarity: 6,
		MaxATK: 15, MaxDEF: 15, MaxSTA: 15,
	}

	result := g.MonsterRowText(tr, m)
	if !strings.Contains(result, "**Everything**") {
		t.Errorf("expected Everything, got: %s", result)
	}
	if !strings.Contains(result, "iv: ?%-100%") {
		t.Errorf("expected iv: ?%%-100%%, got: %s", result)
	}
}

func TestMonsterRowText_WithPVP(t *testing.T) {
	g := testGenerator(t)
	tr := g.Translations.For("en")

	m := &db.MonsterTracking{
		PokemonID: 25, Form: 0, Template: "1",
		MinIV: 0, MaxIV: 100, MinCP: 0, MaxCP: 9000,
		MinLevel: 0, MaxLevel: 40, MaxSize: 5, MaxRarity: 6,
		MaxATK: 15, MaxDEF: 15, MaxSTA: 15,
		PVPRankingLeague: 1500, PVPRankingBest: 1, PVPRankingWorst: 10,
		PVPRankingMinCP: 1400, PVPRankingCap: 50,
	}

	result := g.MonsterRowText(tr, m)
	if !strings.Contains(result, "pvp ranking:") {
		t.Errorf("expected pvp ranking, got: %s", result)
	}
	if !strings.Contains(result, "greatpvp") {
		t.Errorf("expected greatpvp league, got: %s", result)
	}
	if !strings.Contains(result, "top10") {
		t.Errorf("expected top10, got: %s", result)
	}
	if !strings.Contains(result, "level cap: 50") {
		t.Errorf("expected level cap: 50, got: %s", result)
	}
}

func TestMonsterRowText_WithSizeAndRarity(t *testing.T) {
	g := testGenerator(t)
	tr := g.Translations.For("en")

	m := &db.MonsterTracking{
		PokemonID: 1, Form: 0, Template: "1",
		MinIV: 90, MaxIV: 100, MinCP: 0, MaxCP: 9000,
		MinLevel: 0, MaxLevel: 40,
		MaxATK: 15, MaxDEF: 15, MaxSTA: 15,
		Size: 4, MaxSize: 5, Rarity: 3, MaxRarity: 5,
	}

	result := g.MonsterRowText(tr, m)
	if !strings.Contains(result, "size: XL-XXL") {
		t.Errorf("expected size: XL-XXL, got: %s", result)
	}
	if !strings.Contains(result, "rarity: Rare-Ultra-Rare") {
		t.Errorf("expected rarity: Rare-Ultra-Rare, got: %s", result)
	}
}

func TestMonsterRowText_WithGenderAndTime(t *testing.T) {
	g := testGenerator(t)
	tr := g.Translations.For("en")

	m := &db.MonsterTracking{
		PokemonID: 1, Form: 0, Template: "2",
		MinIV: 0, MaxIV: 100, MinCP: 0, MaxCP: 9000,
		MinLevel: 0, MaxLevel: 40, MaxSize: 5, MaxRarity: 6,
		MaxATK: 15, MaxDEF: 15, MaxSTA: 15,
		Gender: 1, MinTime: 60, Clean: 1,
	}

	result := g.MonsterRowText(tr, m)
	if !strings.Contains(result, "gender: ♂") {
		t.Errorf("expected gender: ♂, got: %s", result)
	}
	if !strings.Contains(result, "minimum time: 60s") {
		t.Errorf("expected minimum time: 60s, got: %s", result)
	}
	if !strings.Contains(result, "template: 2") {
		t.Errorf("expected template: 2, got: %s", result)
	}
	if !strings.Contains(result, "clean") {
		t.Errorf("expected clean, got: %s", result)
	}
}

// TestMonsterRowText_LegacyWeightShown verifies that a tracking row carrying
// a non-default weight range — !track no longer accepts these, but rows from
// older PoracleJS or pre-removal !track invocations may still have them — is
// surfaced in the row text so users can see the legacy filter is still
// suppressing matches and remove the rule via !untrack id:N.
func TestMonsterRowText_LegacyWeightShown(t *testing.T) {
	g := testGenerator(t)
	tr := g.Translations.For("en")

	// Default-shape row with an explicit weight constraint added.
	m := &db.MonsterTracking{
		PokemonID: 1, Form: 0, Template: "1",
		MinIV: -1, MaxIV: 100, MinCP: 0, MaxCP: 9000,
		MinLevel: 0, MaxLevel: 55,
		MaxATK: 15, MaxDEF: 15, MaxSTA: 15,
		MinWeight: 5000, MaxWeight: 9000000,
		MaxSize: 5, MaxRarity: 6,
	}
	result := g.MonsterRowText(tr, m)
	// Stored values render verbatim (DB stores webhook-kg × 1000, i.e. grams,
	// matching what the user typed at !track weight:N-M).
	if !strings.Contains(result, "weight: 5000-9000000g") {
		t.Errorf("expected weight range to be rendered, got: %s", result)
	}
	if !strings.Contains(result, "legacy filter") {
		t.Errorf("expected legacy-filter warning marker, got: %s", result)
	}

	// A row with the no-op range (0..9000000) must not render the weight line.
	m.MinWeight = 0
	m.MaxWeight = 9000000
	result = g.MonsterRowText(tr, m)
	if strings.Contains(result, "weight:") {
		t.Errorf("no-op weight range must not render, got: %s", result)
	}

	// A row left at zero defaults (newly inserted by post-removal !track)
	// also must not render the weight line.
	m.MinWeight = 0
	m.MaxWeight = 0
	result = g.MonsterRowText(tr, m)
	if strings.Contains(result, "weight:") {
		t.Errorf("zero-default weight range must not render, got: %s", result)
	}
}

func TestRaidRowText_GenericLevel(t *testing.T) {
	g := testGenerator(t)
	tr := g.Translations.For("en")

	r := &db.RaidTracking{
		PokemonID: 9000, Level: 5, Team: 4, Template: "1",
	}

	result := g.RaidRowText(tr, r)
	if !strings.Contains(result, "**Level 5 raids**") {
		t.Errorf("expected Level 5 raids, got: %s", result)
	}
	if !strings.Contains(result, "without rsvp updates") {
		t.Errorf("expected rsvp text, got: %s", result)
	}
}

func TestRaidRowText_AllLevel(t *testing.T) {
	g := testGenerator(t)
	tr := g.Translations.For("en")

	r := &db.RaidTracking{
		PokemonID: 9000, Level: 90, Team: 1, Template: "1",
		Distance: 500, Exclusive: true, RSVPChanges: 1,
	}

	result := g.RaidRowText(tr, r)
	if !strings.Contains(result, "**All level raids**") {
		t.Errorf("expected All level raids, got: %s", result)
	}
	if !strings.Contains(result, "distance: 500m") {
		t.Errorf("expected distance: 500m, got: %s", result)
	}
	if !strings.Contains(result, "controlled by Mystic") {
		t.Errorf("expected controlled by Mystic, got: %s", result)
	}
	if !strings.Contains(result, "must be an EX Gym") {
		t.Errorf("expected EX Gym, got: %s", result)
	}
	if !strings.Contains(result, "including rsvp updates") {
		t.Errorf("expected including rsvp, got: %s", result)
	}
}

func TestRaidRowText_SpecificPokemon(t *testing.T) {
	g := testGenerator(t)
	tr := g.Translations.For("en")

	r := &db.RaidTracking{
		PokemonID: 150, Form: 0, Team: 4, Template: "1",
	}

	result := g.RaidRowText(tr, r)
	if !strings.Contains(result, "**poke_150**") || strings.Contains(result, "Level") {
		// poke_150 because we don't have game locale loaded, just embedded
		t.Logf("raid specific pokemon result: %s", result)
	}
}

func TestEggRowText(t *testing.T) {
	g := testGenerator(t)
	tr := g.Translations.For("en")

	e := &db.EggTracking{
		Level: 5, Team: 4, Template: "1", RSVPChanges: 2,
	}

	result := g.EggRowText(tr, e)
	if !strings.Contains(result, "Level 5 eggs") {
		t.Errorf("expected Level 5 eggs, got: %s", result)
	}
	if !strings.Contains(result, "rsvp only") {
		t.Errorf("expected rsvp only, got: %s", result)
	}
}

func TestQuestRowText_Stardust(t *testing.T) {
	g := testGenerator(t)
	tr := g.Translations.For("en")

	q := &db.QuestTracking{
		RewardType: 3, Reward: 1000, Template: "1",
	}

	result := g.QuestRowText(tr, q)
	if !strings.Contains(result, "Reward:") {
		t.Errorf("expected Reward:, got: %s", result)
	}
	if !strings.Contains(result, "1000 or more stardust") {
		t.Errorf("expected 1000 or more stardust, got: %s", result)
	}
}

func TestQuestRowText_StardustAny(t *testing.T) {
	g := testGenerator(t)
	tr := g.Translations.For("en")

	q := &db.QuestTracking{
		RewardType: 3, Reward: 0, Template: "1",
	}

	result := g.QuestRowText(tr, q)
	if !strings.Contains(result, "stardust") {
		t.Errorf("expected stardust, got: %s", result)
	}
}

func TestQuestRowText_MegaEnergy(t *testing.T) {
	g := testGenerator(t)
	tr := g.Translations.For("en")

	q := &db.QuestTracking{
		RewardType: 12, Reward: 1, Template: "1",
	}

	result := g.QuestRowText(tr, q)
	if !strings.Contains(result, "mega energy") {
		t.Errorf("expected mega energy, got: %s", result)
	}
}

func TestInvasionRowText(t *testing.T) {
	g := testGenerator(t)
	tr := g.Translations.For("en")

	inv := &db.InvasionTracking{
		GruntType: "Water", Gender: 2, Distance: 300, Template: "1",
	}

	result := g.InvasionRowText(tr, inv)
	if !strings.Contains(result, "Grunt type:") {
		t.Errorf("expected Grunt type:, got: %s", result)
	}
	if !strings.Contains(result, "distance: 300m") {
		t.Errorf("expected distance: 300m, got: %s", result)
	}
	if !strings.Contains(result, "gender: female") {
		t.Errorf("expected gender: female, got: %s", result)
	}
}

func TestInvasionRowText_Any(t *testing.T) {
	g := testGenerator(t)
	tr := g.Translations.For("en")

	inv := &db.InvasionTracking{
		GruntType: "", Gender: 0, Template: "1",
	}

	result := g.InvasionRowText(tr, inv)
	if !strings.Contains(result, "**any**") {
		t.Errorf("expected **any**, got: %s", result)
	}
}

func TestNestRowText(t *testing.T) {
	g := testGenerator(t)
	tr := g.Translations.For("en")

	n := &db.NestTracking{
		PokemonID: 0, Template: "1", MinSpawnAvg: 5,
	}

	result := g.NestRowText(tr, n)
	if !strings.Contains(result, "**Everything**") {
		t.Errorf("expected Everything, got: %s", result)
	}
	if !strings.Contains(result, "Min avg. spawn 5/hour") {
		t.Errorf("expected spawn avg, got: %s", result)
	}
}

func TestLureRowText_Any(t *testing.T) {
	g := testGenerator(t)
	tr := g.Translations.For("en")

	l := &db.LureTracking{
		LureID: 0, Template: "1",
	}

	result := g.LureRowText(tr, l)
	if !strings.Contains(result, "Lure type: **any**") {
		t.Errorf("expected Lure type: **any**, got: %s", result)
	}
}

func TestLureRowText_Specific(t *testing.T) {
	g := testGenerator(t)
	tr := g.Translations.For("en")

	l := &db.LureTracking{
		LureID: 501, Distance: 200, Template: "1",
	}

	result := g.LureRowText(tr, l)
	if !strings.Contains(result, "Normal Lure") {
		t.Errorf("expected Normal Lure, got: %s", result)
	}
	if !strings.Contains(result, "distance: 200m") {
		t.Errorf("expected distance: 200m, got: %s", result)
	}
}

func TestGymRowText(t *testing.T) {
	g := testGenerator(t)
	tr := g.Translations.For("en")

	gym := &db.GymTracking{
		Team: 2, SlotChanges: true, BattleChanges: true,
		Distance: 1000, Template: "1",
	}

	result := g.GymRowText(tr, gym)
	if !strings.Contains(result, "Valor gyms") {
		t.Errorf("expected Valor gyms, got: %s", result)
	}
	if !strings.Contains(result, "distance: 1000m") {
		t.Errorf("expected distance, got: %s", result)
	}
	if !strings.Contains(result, "including slot changes") {
		t.Errorf("expected slot changes, got: %s", result)
	}
	if !strings.Contains(result, "including battle changes") {
		t.Errorf("expected battle changes, got: %s", result)
	}
}

func TestGymRowText_AllTeams(t *testing.T) {
	g := testGenerator(t)
	tr := g.Translations.For("en")

	gym := &db.GymTracking{
		Team: 4, Template: "1",
	}

	result := g.GymRowText(tr, gym)
	if !strings.Contains(result, "All team's gyms") {
		t.Errorf("expected All team's gyms, got: %s", result)
	}
}

func TestGymRowText_WithGymID(t *testing.T) {
	g := testGenerator(t)
	tr := g.Translations.For("en")

	gymID := "abc123"
	gym := &db.GymTracking{
		Team: 1, Template: "1", GymID: &gymID,
	}

	// No scanner, should fallback to raw ID
	result := g.GymRowText(tr, gym)
	if !strings.Contains(result, "at gym abc123") {
		t.Errorf("expected at gym abc123, got: %s", result)
	}
}

func TestMaxbattleRowText_GenericLevel(t *testing.T) {
	g := testGenerator(t)
	tr := g.Translations.For("en")

	mb := &db.MaxbattleTracking{
		PokemonID: 9000, Level: 3, Template: "1",
	}

	result := g.MaxbattleRowText(tr, mb)
	if !strings.Contains(result, "Level 3 maxbattles") {
		t.Errorf("expected Level 3 maxbattles, got: %s", result)
	}
}

func TestFortUpdateRowText(t *testing.T) {
	g := testGenerator(t)
	tr := g.Translations.For("en")

	f := &db.FortTracking{
		FortType: "pokestop", Distance: 500, Template: "1",
		ChangeTypes: `["name","location"]`, IncludeEmpty: true,
	}

	result := g.FortUpdateRowText(tr, f)
	if !strings.Contains(result, "Fort updates:") {
		t.Errorf("expected Fort updates:, got: %s", result)
	}
	if !strings.Contains(result, "distance: 500m") {
		t.Errorf("expected distance, got: %s", result)
	}
	if !strings.Contains(result, "including empty changes") {
		t.Errorf("expected including empty, got: %s", result)
	}
}

func TestStandardText_DefaultTemplate(t *testing.T) {
	g := testGenerator(t)
	tr := g.Translations.For("en")

	result := standardText(tr, "1", g.DefaultTemplateName, 0)
	if result != "" {
		t.Errorf("expected empty for default template, got: %q", result)
	}
}

func TestStandardText_CustomTemplateAndClean(t *testing.T) {
	g := testGenerator(t)
	tr := g.Translations.For("en")

	result := standardText(tr, "3", g.DefaultTemplateName, 1)
	if !strings.Contains(result, "template: 3") {
		t.Errorf("expected template: 3, got: %q", result)
	}
	if !strings.Contains(result, "clean") {
		t.Errorf("expected clean, got: %q", result)
	}
}

func TestRaidRowText_WithGymID(t *testing.T) {
	g := testGenerator(t)
	tr := g.Translations.For("en")

	r := &db.RaidTracking{
		PokemonID: 9000, Level: 5, Team: 4, Template: "1",
		GymID: sql.NullString{String: "gym_xyz", Valid: true},
	}

	result := g.RaidRowText(tr, r)
	if !strings.Contains(result, "at gym gym_xyz") {
		t.Errorf("expected at gym gym_xyz (fallback to raw ID), got: %s", result)
	}
}

func TestUcFirst(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"hello", "Hello"},
		{"", ""},
		{"A", "A"},
		{"über", "Über"},
	}
	for _, tt := range tests {
		got := ucFirst(tt.input)
		if got != tt.want {
			t.Errorf("ucFirst(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
