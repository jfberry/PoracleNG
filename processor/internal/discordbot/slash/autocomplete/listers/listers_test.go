package listers

import (
	"context"
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/discordbot/slash/autocomplete"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/rowtext"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// testDeps builds a *bot.BotDeps with mock stores and a working RowText
// Generator. The bundle, GameData entries, and registered tracking rows
// are the minimum needed by the listers under test.
func testDeps(t *testing.T) (*bot.BotDeps, *store.MockHumanStore, *store.MockTrackingStore[db.RaidTrackingAPI], *store.MockTrackingStore[db.MonsterTrackingAPI]) {
	t.Helper()

	bundle := i18n.Load("")

	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 25, Form: 0}:  {PokemonID: 25, FormID: 0, Types: []int{13}},
			{ID: 150, Form: 0}: {PokemonID: 150, FormID: 0, Types: []int{14}},
		},
		Moves: map[int]*gamedata.Move{},
		Items: map[int]*gamedata.Item{},
		Util: &gamedata.UtilData{
			Genders: map[int]gamedata.GenderInfo{},
			Lures:   map[int]gamedata.LureInfo{},
		},
	}

	rt := &rowtext.Generator{
		GD:                  gd,
		Translations:        bundle,
		DefaultTemplateName: "1",
	}

	raidStore := store.NewMockTrackingStore[db.RaidTrackingAPI](store.RaidGetUID, store.RaidSetUID)
	monsterStore := store.NewMockTrackingStore[db.MonsterTrackingAPI](store.MonsterGetUID, store.MonsterSetUID)
	humans := store.NewMockHumanStore()

	deps := &bot.BotDeps{
		Humans:       humans,
		Translations: bundle,
		RowText:      rt,
		Tracking: &store.TrackingStores{
			Monsters:   monsterStore,
			Raids:      raidStore,
			Eggs:       store.NewMockTrackingStore[db.EggTrackingAPI](store.EggGetUID, store.EggSetUID),
			Quests:     store.NewMockTrackingStore[db.QuestTrackingAPI](store.QuestGetUID, store.QuestSetUID),
			Invasions:  store.NewMockTrackingStore[db.InvasionTrackingAPI](store.InvasionGetUID, store.InvasionSetUID),
			Lures:      store.NewMockTrackingStore[db.LureTrackingAPI](store.LureGetUID, store.LureSetUID),
			Nests:      store.NewMockTrackingStore[db.NestTrackingAPI](store.NestGetUID, store.NestSetUID),
			Gyms:       store.NewMockTrackingStore[db.GymTrackingAPI](store.GymGetUID, store.GymSetUID),
			Forts:      store.NewMockTrackingStore[db.FortTrackingAPI](store.FortGetUID, store.FortSetUID),
			Maxbattles: store.NewMockTrackingStore[db.MaxbattleTrackingAPI](store.MaxbattleGetUID, store.MaxbattleSetUID),
		},
	}

	return deps, humans, raidStore, monsterStore
}

func TestListTracking_Raid(t *testing.T) {
	deps, humans, raidStore, _ := testDeps(t)
	humans.AddHuman(&store.Human{ID: "discord:user:42", CurrentProfileNo: 0})

	row := db.RaidTrackingAPI{
		ID:        "discord:user:42",
		ProfileNo: 0,
		PokemonID: 150, // Mewtwo
		Team:      4,
		Level:     5,
	}
	if _, err := raidStore.Insert(&row); err != nil {
		t.Fatalf("seed raid: %v", err)
	}
	want := raidStore.AllRows()[0]

	out, err := ListTracking(context.Background(), deps, "discord:user:42", autocomplete.UserStateHint{Subtype: "raid"})
	if err != nil {
		t.Fatalf("ListTracking: %v", err)
	}
	// Two choices: the "Remove ALL" sentinel at the head, then the
	// actual rule. The sentinel is prepended when the list is
	// non-empty so the operator can clear everything in one click.
	if len(out) != 2 {
		t.Fatalf("got %d choices, want 2 (remove-all sentinel + one rule)", len(out))
	}
	if out[0].Value != RemoveAllSentinel {
		t.Errorf("first choice should be remove-all sentinel; got value=%q", out[0].Value)
	}
	rule := out[1]
	// The rowtext output contains the pokemon-name key (poke_150) when the
	// gamelocale bundle isn't available in the unit-test environment; with
	// it loaded it would be "Mewtwo". Both forms are acceptable here — we
	// only need to prove a description was prepended to the "[id:N]" suffix.
	if !strings.Contains(rule.Label, "poke_150") && !strings.Contains(rule.Label, "Mewtwo") {
		t.Errorf("label missing pokemon name/key: %q", rule.Label)
	}
	suffix := "[id:" + itoa(want.UID) + "]"
	if !strings.HasSuffix(rule.Label, suffix) {
		t.Errorf("label suffix %q missing from %q", suffix, rule.Label)
	}
	if rule.Value != itoa(want.UID) {
		t.Errorf("value=%q want %q", rule.Value, itoa(want.UID))
	}
}

func TestListTracking_Monster(t *testing.T) {
	deps, humans, _, monsterStore := testDeps(t)
	humans.AddHuman(&store.Human{ID: "discord:user:42", CurrentProfileNo: 0})

	row := db.MonsterTrackingAPI{
		ID:        "discord:user:42",
		ProfileNo: 0,
		PokemonID: 25, // Pikachu
		MinIV:     -1,
		MaxIV:     100,
		MinCP:     0,
		MaxCP:     9000,
		MinLevel:  0,
		MaxLevel:  40,
		MaxATK:    15,
		MaxDEF:    15,
		MaxSTA:    15,
		MaxSize:   5,
		MaxRarity: 6,
	}
	if _, err := monsterStore.Insert(&row); err != nil {
		t.Fatalf("seed monster: %v", err)
	}
	want := monsterStore.AllRows()[0]

	out, err := ListTracking(context.Background(), deps, "discord:user:42", autocomplete.UserStateHint{Subtype: "pokemon"})
	if err != nil {
		t.Fatalf("ListTracking: %v", err)
	}
	// Two choices: remove-all sentinel + the seeded rule.
	if len(out) != 2 {
		t.Fatalf("got %d choices, want 2 (remove-all sentinel + one rule)", len(out))
	}
	if out[0].Value != RemoveAllSentinel {
		t.Errorf("first choice should be remove-all sentinel; got value=%q", out[0].Value)
	}
	rule := out[1]
	// See TestListTracking_Raid for why both forms are acceptable.
	if !strings.Contains(rule.Label, "poke_25") && !strings.Contains(rule.Label, "Pikachu") {
		t.Errorf("label missing pokemon name/key: %q", rule.Label)
	}
	suffix := "[id:" + itoa(want.UID) + "]"
	if !strings.HasSuffix(rule.Label, suffix) {
		t.Errorf("label suffix %q missing from %q", suffix, rule.Label)
	}
	if rule.Value != itoa(want.UID) {
		t.Errorf("value=%q want %q", rule.Value, itoa(want.UID))
	}
}

// TestListTracking_NoRulesNoRemoveAllSentinel — when the user has no
// rules of the requested subtype, the picker must NOT show the
// "Remove ALL" affordance (there's nothing to remove). Avoids the
// confusing UX of clicking it on an empty list.
func TestListTracking_NoRulesNoRemoveAllSentinel(t *testing.T) {
	deps, humans, _, _ := testDeps(t)
	humans.AddHuman(&store.Human{ID: "discord:user:42"})

	out, err := ListTracking(context.Background(), deps, "discord:user:42", autocomplete.UserStateHint{Subtype: "raid"})
	if err != nil {
		t.Fatalf("ListTracking: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("expected empty list (no remove-all on empty), got %d choices: %v", len(out), out)
	}
}

func TestListTracking_UnknownSubtypeReturnsNil(t *testing.T) {
	deps, humans, _, _ := testDeps(t)
	humans.AddHuman(&store.Human{ID: "discord:user:42"})

	out, err := ListTracking(context.Background(), deps, "discord:user:42", autocomplete.UserStateHint{Subtype: "no-such-type"})
	if err != nil {
		t.Fatalf("ListTracking: %v", err)
	}
	if out != nil {
		t.Errorf("expected nil for unknown subtype, got %v", out)
	}
}

func TestListTracking_UnregisteredUserUsesProfileZero(t *testing.T) {
	// No human seeded; the lister should still proceed with profile 0
	// (the mock store ignores id/profileNo). Confirms the lister is
	// nil-safe against a missing human.
	deps, _, raidStore, _ := testDeps(t)
	if _, err := raidStore.Insert(&db.RaidTrackingAPI{ID: "discord:user:99", PokemonID: 150, Team: 4, Level: 5}); err != nil {
		t.Fatalf("seed raid: %v", err)
	}
	out, err := ListTracking(context.Background(), deps, "discord:user:99", autocomplete.UserStateHint{Subtype: "raid"})
	if err != nil {
		t.Fatalf("ListTracking: %v", err)
	}
	// remove-all sentinel + the seeded rule = 2 choices.
	if len(out) != 2 {
		t.Fatalf("got %d choices, want 2", len(out))
	}
}

func TestListAreas(t *testing.T) {
	deps, humans, _, _ := testDeps(t)
	humans.AddHuman(&store.Human{
		ID:   "discord:user:42",
		Area: []string{"london", "paris"},
	})

	out, err := ListAreas(context.Background(), deps, "discord:user:42", autocomplete.UserStateHint{})
	if err != nil {
		t.Fatalf("ListAreas: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d choices", len(out))
	}
	got := map[string]bool{}
	for _, c := range out {
		got[c.Label] = true
		if c.Label != c.Value {
			t.Errorf("label/value mismatch: %q vs %q", c.Label, c.Value)
		}
	}
	if !got["london"] || !got["paris"] {
		t.Errorf("missing expected areas: %v", out)
	}
}

func TestListAreas_UnregisteredUserNil(t *testing.T) {
	deps, _, _, _ := testDeps(t)
	out, err := ListAreas(context.Background(), deps, "discord:user:nobody", autocomplete.UserStateHint{})
	if err != nil {
		t.Fatalf("ListAreas: %v", err)
	}
	if out != nil {
		t.Errorf("expected nil for unregistered user, got %v", out)
	}
}

func TestListProfiles(t *testing.T) {
	deps, humans, _, _ := testDeps(t)
	humans.AddHuman(&store.Human{ID: "discord:user:42"})
	humans.SeedProfile(store.Profile{ID: "discord:user:42", ProfileNo: 0, Name: "default"})
	humans.SeedProfile(store.Profile{ID: "discord:user:42", ProfileNo: 1, Name: "work"})

	out, err := ListProfiles(context.Background(), deps, "discord:user:42", autocomplete.UserStateHint{})
	if err != nil {
		t.Fatalf("ListProfiles: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d choices", len(out))
	}
	labels := map[string]string{}
	for _, c := range out {
		labels[c.Value] = c.Label
	}
	if labels["0"] != "default [#0]" {
		t.Errorf("profile 0 label=%q", labels["0"])
	}
	if labels["1"] != "work [#1]" {
		t.Errorf("profile 1 label=%q", labels["1"])
	}
}

// itoa avoids pulling strconv into the test for a tiny helper.
func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	buf := [20]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
