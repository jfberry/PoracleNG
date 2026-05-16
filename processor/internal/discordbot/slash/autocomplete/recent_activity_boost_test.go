package autocomplete

import (
	"context"
	"testing"

	"github.com/bwmarrin/discordgo"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

func boostPokemonDeps(t *testing.T) *bot.BotDeps {
	t.Helper()
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"poke_25":  "Pikachu",
		"poke_6":   "Charizard",
		"poke_150": "Mewtwo",
	}))
	bundle.LinkFallbacks()
	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 25, Form: 0}:  {PokemonID: 25},
			{ID: 6, Form: 0}:   {PokemonID: 6},
			{ID: 150, Form: 0}: {PokemonID: 150},
		},
	}
	return &bot.BotDeps{Translations: bundle, GameData: gd, Cfg: &config.Config{}}
}

// Boost must surface the active pokemon ID first on empty focused, even
// when the base list already contains that pokemon (dedupes by Value).
func TestPrependActivePokemon_BoostsFirst(t *testing.T) {
	deps := boostPokemonDeps(t)
	base := Pokemon(context.Background(), deps, "", "en")
	out := PrependActivePokemon(base, deps, []int{150}, "en")
	if len(out) == 0 {
		t.Fatal("expected non-empty output")
	}
	if out[0].Name != "Mewtwo" {
		t.Errorf("first choice = %q, want Mewtwo (active ID 150 prepended)", out[0].Name)
	}
	// Dedup: Mewtwo must only appear once even though base also contains it.
	count := 0
	for _, c := range out {
		if c.Name == "Mewtwo" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Mewtwo appears %d times, want 1 (dedup against base)", count)
	}
}

// Empty active list = pass-through; base is unchanged.
func TestPrependActivePokemon_EmptyActiveFallsThrough(t *testing.T) {
	deps := boostPokemonDeps(t)
	base := Pokemon(context.Background(), deps, "", "en")
	out := PrependActivePokemon(base, deps, nil, "en")
	// Returning base directly is the intended optimisation.
	if len(out) != len(base) {
		t.Errorf("len(out)=%d, len(base)=%d — pass-through should preserve length", len(out), len(base))
	}
}

// IDs that don't resolve to a registered pokemon are silently skipped.
func TestPrependActivePokemon_UnknownIDSkipped(t *testing.T) {
	deps := boostPokemonDeps(t)
	base := Pokemon(context.Background(), deps, "", "en")
	out := PrependActivePokemon(base, deps, []int{99999}, "en")
	for _, c := range out {
		if c.Name == "" {
			t.Errorf("empty name leaked from skip path: %+v", out)
		}
	}
}

func boostItemDeps(t *testing.T) *bot.BotDeps {
	t.Helper()
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"item_1":   "Poke Ball",
		"item_701": "Razz Berry",
		"item_706": "Golden Razz Berry",
	}))
	bundle.LinkFallbacks()
	gd := &gamedata.GameData{
		Items: map[int]*gamedata.Item{
			1:   {ItemID: 1},
			701: {ItemID: 701},
			706: {ItemID: 706},
		},
	}
	return &bot.BotDeps{Translations: bundle, GameData: gd, Cfg: &config.Config{}}
}

func TestPrependActiveItems_BoostsFirst(t *testing.T) {
	deps := boostItemDeps(t)
	base := Item(context.Background(), deps, "", "en")
	out := PrependActiveItems(base, deps, []int{706}, "en")
	if len(out) == 0 {
		t.Fatal("expected non-empty output")
	}
	if out[0].Name != "Golden Razz Berry" {
		t.Errorf("first = %q, want Golden Razz Berry", out[0].Name)
	}
	// Value contract for Item autocomplete: same as Label.
	if v, _ := out[0].Value.(string); v != "Golden Razz Berry" {
		t.Errorf("Value=%v, want 'Golden Razz Berry' (same as Label)", out[0].Value)
	}
	// Dedup: must only appear once.
	count := 0
	for _, c := range out {
		if c.Name == "Golden Razz Berry" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Golden Razz Berry appears %d times, want 1", count)
	}
}

func TestPrependActiveItems_EmptyActiveFallsThrough(t *testing.T) {
	deps := boostItemDeps(t)
	base := Item(context.Background(), deps, "", "en")
	out := PrependActiveItems(base, deps, nil, "en")
	if len(out) != len(base) {
		t.Errorf("len(out)=%d, len(base)=%d", len(out), len(base))
	}
}

func boostGruntDeps(t *testing.T) *bot.BotDeps {
	t.Helper()
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"poke_type_10": "Fire",
		"poke_type_11": "Water",
	}))
	bundle.LinkFallbacks()
	gd := &gamedata.GameData{
		Grunts: map[int]*gamedata.Grunt{
			1: {Template: "CHARACTER_FIRE_GRUNT_MALE", TypeID: 10},
			2: {Template: "CHARACTER_WATER_GRUNT_FEMALE", TypeID: 11},
			3: {Template: "CHARACTER_GIOVANNI", Boss: true},
		},
	}
	return &bot.BotDeps{Translations: bundle, GameData: gd}
}

// Active grunt TypeIDs come from invasion webhooks. We expect the
// matching Grunt entry's translated type to be surfaced first as
// "<Type> Grunt".
func TestPrependActiveGrunts_BoostsFirst(t *testing.T) {
	deps := boostGruntDeps(t)
	base := Grunt(context.Background(), deps, "", "en")
	out := PrependActiveGrunts(base, deps, []int{11}, "en")
	if len(out) == 0 {
		t.Fatal("expected non-empty output")
	}
	if out[0].Name != "Water Grunt" {
		t.Errorf("first = %q, want 'Water Grunt' (TypeID 11)", out[0].Name)
	}
	if v, _ := out[0].Value.(string); v != "water" {
		t.Errorf("Value=%v, want 'water' (canonical English)", out[0].Value)
	}
	// Dedup: only one Water Grunt even though base contains it too.
	count := 0
	for _, c := range out {
		if c.Name == "Water Grunt" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("Water Grunt appears %d times, want 1", count)
	}
}

// Unknown TypeIDs are silently skipped — no panic, no empty entries.
func TestPrependActiveGrunts_UnknownTypeIDSkipped(t *testing.T) {
	deps := boostGruntDeps(t)
	base := Grunt(context.Background(), deps, "", "en")
	out := PrependActiveGrunts(base, deps, []int{9999}, "en")
	for _, c := range out {
		if c.Name == "" {
			t.Errorf("empty name from skip path: %+v", out)
		}
	}
}

func TestPrependActiveGrunts_EmptyActiveFallsThrough(t *testing.T) {
	deps := boostGruntDeps(t)
	base := Grunt(context.Background(), deps, "", "en")
	out := PrependActiveGrunts(base, deps, nil, "en")
	if len(out) != len(base) {
		t.Errorf("len(out)=%d, len(base)=%d", len(out), len(base))
	}
}

// End-to-end: RecentActivity → routeAutocomplete via the dispatcher's
// helper functions. The dispatcher itself is exercised at
// internal/discordbot/slash, but the autocomplete-package boost wrappers
// rely on the same RecentActivity contract.
func TestRecentActivity_BoostIntegration(t *testing.T) {
	deps := boostPokemonDeps(t)
	ra := tracker.NewRecentActivity()
	ra.RecordMaxBattleBoss(150)
	deps.RecentActivity = ra

	base := Pokemon(context.Background(), deps, "", "en")
	out := PrependActivePokemon(base, deps, deps.RecentActivity.ActiveMaxBattleBosses(), "en")
	if len(out) == 0 || out[0].Name != "Mewtwo" {
		t.Errorf("expected Mewtwo first via RecentActivity, got %+v", choiceNames(out))
	}
}

func choiceNames(c []*discordgo.ApplicationCommandOptionChoice) []string {
	out := make([]string, 0, len(c))
	for _, x := range c {
		out = append(out, x.Name)
	}
	return out
}
