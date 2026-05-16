package autocomplete

import (
	"context"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
)

// pokemonTestDeps builds a minimal *bot.BotDeps for the pokemon-name
// autocomplete tests. The Monsters map holds species 1 (Bulbasaur), 6
// (Charizard), and 25 (Pikachu); English names come from a hand-built
// Translator. A German translator is added so we can verify userLang
// localisation without depending on shipped locale files.
func pokemonTestDeps(t *testing.T) *bot.BotDeps {
	t.Helper()
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"poke_1":  "Bulbasaur",
		"poke_6":  "Charizard",
		"poke_25": "Pikachu",
	}))
	bundle.AddTranslator(i18n.NewTranslator("de", map[string]string{
		"poke_25": "Pikachu",
		"poke_6":  "Glurak",
	}))
	bundle.LinkFallbacks()

	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 1, Form: 0}:  {PokemonID: 1},
			{ID: 6, Form: 0}:  {PokemonID: 6},
			{ID: 25, Form: 0}: {PokemonID: 25},
		},
	}
	return &bot.BotDeps{Translations: bundle, GameData: gd}
}

func TestPokemon_ExactMatchFirst(t *testing.T) {
	deps := pokemonTestDeps(t)
	out := Pokemon(context.Background(), deps, "pikachu", "en")
	if len(out) == 0 {
		t.Fatalf("expected at least 1 choice")
	}
	if out[0].Name != "Pikachu" {
		t.Errorf("first choice = %q, want Pikachu", out[0].Name)
	}
	if out[0].Value != "pikachu" {
		t.Errorf("Value = %v, want canonical lowercase 'pikachu'", out[0].Value)
	}
}

func TestPokemon_PrefixMatch(t *testing.T) {
	deps := pokemonTestDeps(t)
	out := Pokemon(context.Background(), deps, "char", "en")
	found := false
	for _, c := range out {
		if c.Name == "Charizard" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Charizard missing from prefix 'char' results: %+v", out)
	}
}

func TestPokemon_EmptyFocusedReturnsNil(t *testing.T) {
	deps := pokemonTestDeps(t)
	out := Pokemon(context.Background(), deps, "", "en")
	if out != nil {
		t.Errorf("expected nil for empty focused, got %d entries", len(out))
	}
}

func TestPokemon_ValueIsCanonicalEnglishLowercase(t *testing.T) {
	// User typing in German still gets the canonical English lowercase
	// in Value so the dispatcher can resolve the same id-string later.
	deps := pokemonTestDeps(t)
	out := Pokemon(context.Background(), deps, "glurak", "de")
	if len(out) == 0 {
		t.Fatalf("expected match for 'glurak'")
	}
	if out[0].Name != "Glurak" {
		t.Errorf("Name = %q, want localized 'Glurak'", out[0].Name)
	}
	if out[0].Value != "charizard" {
		t.Errorf("Value = %v, want canonical 'charizard'", out[0].Value)
	}
}

func TestPokemon_NumericIDMatch(t *testing.T) {
	deps := pokemonTestDeps(t)
	out := Pokemon(context.Background(), deps, "25", "en")
	found := false
	for _, c := range out {
		if c.Value == "pikachu" {
			found = true
		}
	}
	if !found {
		t.Errorf("numeric ID '25' didn't surface Pikachu: %+v", out)
	}
}
