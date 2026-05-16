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

// formTestDeps wires a minimal BotDeps with Exeggutor (103) in two
// gamedata entries — form 0 (the generic "any form" placeholder, which
// the picker intentionally omits) and form 47 (named "Alolan"). The
// picker exposes only the named form because picking "form: <name>" is
// always intended to specify a concrete variant; leaving the option
// unset is how the user expresses "any form".
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
	// Form 0 is filtered out — picker exposes only the named form.
	if len(out) != 1 {
		t.Fatalf("expected 1 named form choice for Exeggutor, got %d (%+v)", len(out), out)
	}
	if out[0].Name != "Alolan" {
		t.Errorf("expected 'Alolan' form, got %+v", out)
	}
}

func TestForm_ResolvesByNumericID(t *testing.T) {
	deps := formTestDeps(t)
	out := Form(context.Background(), deps, "103", "", "en")
	// Form 0 is filtered out — picker exposes only the named form.
	if len(out) != 1 {
		t.Errorf("expected 1 named form for ID 103, got %d", len(out))
	}
}

func TestForm_OmitsFormZero(t *testing.T) {
	// A species with ONLY form 0 (no named forms) yields an empty
	// picker — and the user expresses "any form" by not picking one.
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"poke_25": "Pikachu",
	}))
	bundle.LinkFallbacks()
	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 25, Form: 0}: {PokemonID: 25},
		},
	}
	deps := &bot.BotDeps{Translations: bundle, GameData: gd, Cfg: &config.Config{}}
	out := Form(context.Background(), deps, "pikachu", "", "en")
	if len(out) != 0 {
		t.Errorf("expected empty picker for species with only form 0, got %+v", out)
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

// Regression for /track form: showing "Normal" twice for Exeggutor.
// Gamedata can carry duplicate-label entries across costume/variant
// forms (rare but does happen). The picker dedupes by lowercase label
// keeping the lower formID. Form 0 is filtered out entirely so the
// "any form" pseudo-entry is never a source of duplicates.
func TestForm_DedupesDuplicateLabels(t *testing.T) {
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"poke_103": "Exeggutor",
		"form_47":  "Alolan",
		"form_103": "Normal",
		"form_500": "Normal", // hypothetical costume sharing the same label
	}))
	bundle.LinkFallbacks()
	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 103, Form: 0}:   {PokemonID: 103},
			{ID: 103, Form: 103}: {PokemonID: 103, FormID: 103},
			{ID: 103, Form: 500}: {PokemonID: 103, FormID: 500},
			{ID: 103, Form: 47}:  {PokemonID: 103, FormID: 47},
		},
	}
	deps := &bot.BotDeps{Translations: bundle, GameData: gd, Cfg: &config.Config{}}

	out := Form(context.Background(), deps, "exeggutor", "", "en")
	normalCount := 0
	for _, c := range out {
		if strings.EqualFold(c.Name, "normal") {
			normalCount++
		}
	}
	if normalCount != 1 {
		t.Errorf("expected exactly one 'Normal' entry, got %d (%+v)", normalCount, out)
	}
}

func TestForm_NamedValueIsLowercase(t *testing.T) {
	// Form values are lowercased so the bot's form: prefix parser sees
	// the same token shape it would from the text bot.
	deps := formTestDeps(t)
	out := Form(context.Background(), deps, "exeggutor", "", "en")
	for _, c := range out {
		if c.Name == "Alolan" {
			if v, _ := c.Value.(string); !strings.EqualFold(v, "alolan") {
				t.Errorf("Alolan value=%q, want 'alolan'", v)
			}
			return
		}
	}
	t.Error("Alolan entry not found")
}
