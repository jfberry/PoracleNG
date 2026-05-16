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

func TestRaidBoss_EmptyNoRecentShowsKeywords(t *testing.T) {
	deps := raidBossTestDeps(t, false)
	out := RaidBoss(context.Background(), deps, "", "en")
	if len(out) == 0 {
		t.Fatalf("expected keyword choices even without RecentActivity")
	}
	// All entries should be keywords (since no recent activity and no
	// general-pokemon fallthrough on empty focused).
	for _, c := range out {
		isKeyword := false
		for _, kw := range raidLevelKeywords {
			if c.Name == kw {
				isKeyword = true
				break
			}
		}
		if !isKeyword {
			t.Errorf("entry %q is not a keyword; should not appear for empty focused without RecentActivity", c.Name)
		}
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
