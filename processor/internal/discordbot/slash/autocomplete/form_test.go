package autocomplete

import (
	"context"
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// formTestDeps wires a minimal BotDeps with Exeggutor (103) in two forms:
// Normal (form 0) and Alolan (form 47, name "Alolan"). Pokemon name
// resolution is what cascading callers look up by name to find the ID.
func formTestDeps(t *testing.T) *bot.BotDeps {
	t.Helper()
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"poke_103": "Exeggutor",
		"poke_25":  "Pikachu",
		"form_47":  "Alolan",
	}))
	bundle.LinkFallbacks()
	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 103, Form: 0}:  {PokemonID: 103},
			{ID: 103, Form: 47}: {PokemonID: 103, FormID: 47},
			{ID: 25, Form: 0}:   {PokemonID: 25},
		},
	}
	return &bot.BotDeps{Translations: bundle, GameData: gd, Cfg: &config.Config{}}
}

func TestForm_NoPokemonSelectedReturnsNil(t *testing.T) {
	deps := formTestDeps(t)
	out := Form(context.Background(), deps, "", "", "en")
	if out != nil {
		t.Errorf("expected nil for missing pokemon, got %+v", out)
	}
}

func TestForm_ResolvesByCanonicalName(t *testing.T) {
	deps := formTestDeps(t)
	out := Form(context.Background(), deps, "exeggutor", "", "en")
	if len(out) != 2 {
		t.Fatalf("expected 2 form choices for Exeggutor, got %d (%+v)", len(out), out)
	}
	names := map[string]bool{}
	for _, c := range out {
		names[c.Name] = true
	}
	if !names["Normal"] {
		t.Errorf("expected 'Normal' form, got %+v", out)
	}
	if !names["Alolan"] {
		t.Errorf("expected 'Alolan' form, got %+v", out)
	}
}

func TestForm_ResolvesByNumericID(t *testing.T) {
	deps := formTestDeps(t)
	out := Form(context.Background(), deps, "103", "", "en")
	if len(out) != 2 {
		t.Errorf("expected 2 forms for ID 103, got %d", len(out))
	}
}

func TestForm_FiltersBySubstring(t *testing.T) {
	deps := formTestDeps(t)
	out := Form(context.Background(), deps, "exeggutor", "alol", "en")
	if len(out) != 1 || out[0].Name != "Alolan" {
		t.Errorf("expected only 'Alolan' for 'alol' filter, got %+v", out)
	}
}

func TestForm_UnknownPokemonReturnsNil(t *testing.T) {
	deps := formTestDeps(t)
	if out := Form(context.Background(), deps, "not-a-pokemon", "", "en"); out != nil {
		t.Errorf("expected nil for unknown pokemon, got %+v", out)
	}
}

func TestForm_NormalValueIsLowercase(t *testing.T) {
	// Form 0 emits "Normal" as label and "normal" as value so the bot's
	// existing form prefix parser sees the same token shape it would
	// from the text bot. (The bot accepts form:normal as well as numeric
	// form IDs.)
	deps := formTestDeps(t)
	out := Form(context.Background(), deps, "exeggutor", "", "en")
	for _, c := range out {
		if c.Name == "Normal" {
			if v, _ := c.Value.(string); !strings.EqualFold(v, "normal") {
				t.Errorf("Normal value=%q, want 'normal'", v)
			}
			return
		}
	}
	t.Error("Normal entry not found")
}
