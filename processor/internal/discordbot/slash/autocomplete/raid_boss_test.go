package autocomplete

import (
	"context"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

func raidBossTestDeps(t *testing.T, withRecent bool) *bot.BotDeps {
	t.Helper()
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"poke_150": "Mewtwo",
		"poke_25":  "Pikachu",
		"poke_6":   "Charizard",
	}))
	bundle.LinkFallbacks()

	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 25, Form: 0}:  {PokemonID: 25},
			{ID: 6, Form: 0}:   {PokemonID: 6},
			{ID: 150, Form: 0}: {PokemonID: 150},
		},
	}
	deps := &bot.BotDeps{Translations: bundle, GameData: gd}
	if withRecent {
		ra := tracker.NewRecentActivity()
		ra.RecordRaidBoss(150)
		deps.RecentActivity = ra
	}
	return deps
}

func TestRaidBoss_EmptyShowsRecentActivity(t *testing.T) {
	deps := raidBossTestDeps(t, true)
	out := RaidBoss(context.Background(), deps, "", "en")
	found := false
	for _, c := range out {
		if c.Name == "Mewtwo" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Mewtwo from RecentActivity in empty-focused output: %+v", out)
	}
}

func TestRaidBoss_KeywordMatch(t *testing.T) {
	deps := raidBossTestDeps(t, false)
	out := RaidBoss(context.Background(), deps, "me", "en")
	found := false
	for _, c := range out {
		if c.Name == "mega" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'mega' keyword in 'me' results: %+v", out)
	}
}

func TestRaidBoss_EmptyNoRecentShowsKeywordsAndPokemon(t *testing.T) {
	// On empty focused with no RecentActivity, we surface tier keywords
	// AND fall through to the general pokemon autocomplete (which now
	// returns an alphabetical starter list rather than nil), so the user
	// always sees a populated dropdown.
	deps := raidBossTestDeps(t, false)
	out := RaidBoss(context.Background(), deps, "", "en")
	if len(out) == 0 {
		t.Fatalf("expected non-empty choices on empty focused")
	}
	keywords := map[string]bool{}
	for _, kw := range raidLevelKeywords {
		keywords[kw] = true
	}
	gotKeyword, gotPokemon := false, false
	for _, c := range out {
		if keywords[c.Name] {
			gotKeyword = true
		} else if c.Name != "Everything" {
			gotPokemon = true
		}
	}
	if !gotKeyword {
		t.Errorf("no tier/keyword entries surfaced: %+v", out)
	}
	if !gotPokemon {
		t.Errorf("no pokemon entries surfaced: %+v", out)
	}
}

func TestRaidBoss_TypedNameMatchesPokemon(t *testing.T) {
	deps := raidBossTestDeps(t, false)
	out := RaidBoss(context.Background(), deps, "pika", "en")
	found := false
	for _, c := range out {
		if c.Name == "Pikachu" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Pikachu via pokemon fallthrough on 'pika': %+v", out)
	}
}
