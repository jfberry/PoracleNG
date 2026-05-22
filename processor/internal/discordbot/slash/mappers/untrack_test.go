package mappers

import (
	"reflect"
	"testing"

	"github.com/bwmarrin/discordgo"
)

// subopt builds a sub-command option for the slash-mapper tests.
func subopt(name string, children ...*discordgo.ApplicationCommandInteractionDataOption) *discordgo.ApplicationCommandInteractionDataOption {
	return &discordgo.ApplicationCommandInteractionDataOption{
		Name:    name,
		Type:    discordgo.ApplicationCommandOptionSubCommand,
		Options: children,
	}
}

func TestUntrackMapperNoSubcommand(t *testing.T) {
	_, err := Untrack(nil)
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected MapperError, got %T", err)
	}
	if me.Key != "error.slash.untrack.no_subcommand" {
		t.Errorf("key=%q", me.Key)
	}
}

func TestUntrackMapperMissingTracking(t *testing.T) {
	// Sub-command "raid" with no tracking option → no UID, error.
	_, err := Untrack([]*discordgo.ApplicationCommandInteractionDataOption{
		subopt("raid"),
	})
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected MapperError, got %T", err)
	}
	if me.Key != "error.slash.untrack.no_tracking" {
		t.Errorf("key=%q", me.Key)
	}
}

func TestUntrackMapperEmptyTracking(t *testing.T) {
	// Sub-command "pokemon" with empty tracking string → still missing UID.
	_, err := Untrack([]*discordgo.ApplicationCommandInteractionDataOption{
		subopt("pokemon", sopt("tracking", "")),
	})
	if _, ok := err.(*MapperError); !ok {
		t.Fatalf("expected MapperError, got %v", err)
	}
}

// /untrack pokemon → cmd.untrack accepts a bare "id:N" and routes through
// ParamRemoveUID to removeByUIDs(); no "remove" keyword is required.
func TestUntrackMapperEmitsIDTokenForPokemon(t *testing.T) {
	tokens, err := Untrack([]*discordgo.ApplicationCommandInteractionDataOption{
		subopt("pokemon", sopt("tracking", "45")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tokens, []string{"id:45"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

// /untrack <non-pokemon> → cmd.<subtype> requires the explicit "remove"
// keyword alongside "id:N"; without it the typed command treats the UID as
// just a filter and silently no-ops, leaving the rule in place.
func TestUntrackMapperEmitsRemoveAndIDForNonPokemon(t *testing.T) {
	for _, sub := range []string{"raid", "egg", "quest", "invasion", "lure", "nest", "gym", "fort", "maxbattle"} {
		tokens, err := Untrack([]*discordgo.ApplicationCommandInteractionDataOption{
			subopt(sub, sopt("tracking", "123")),
		})
		if err != nil {
			t.Fatalf("%s: %v", sub, err)
		}
		want := []string{"remove", "id:123"}
		if !reflect.DeepEqual(tokens, want) {
			t.Errorf("%s: tokens=%v, want %v", sub, tokens, want)
		}
	}
}

// /untrack pokemon tracking:everything → ["everything"] — the bare
// !untrack everything path that clears every pokemon rule.
func TestUntrackMapperPokemonRemoveAll(t *testing.T) {
	tokens, err := Untrack([]*discordgo.ApplicationCommandInteractionDataOption{
		subopt("pokemon", sopt("tracking", "everything")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tokens, []string{"everything"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

// /untrack raid tracking:everything → ["remove", "everything"] —
// rerouted to !raid remove everything which the raid command's
// HasKeyword("arg.everything") branch handles.
func TestUntrackMapperNonPokemonRemoveAll(t *testing.T) {
	for _, sub := range []string{"raid", "egg", "quest", "invasion", "lure", "nest", "gym", "fort", "maxbattle"} {
		tokens, err := Untrack([]*discordgo.ApplicationCommandInteractionDataOption{
			subopt(sub, sopt("tracking", "everything")),
		})
		if err != nil {
			t.Fatalf("%s: %v", sub, err)
		}
		want := []string{"remove", "everything"}
		if !reflect.DeepEqual(tokens, want) {
			t.Errorf("%s: tokens=%v, want %v", sub, tokens, want)
		}
	}
}

func TestLookupUntrack(t *testing.T) {
	if Lookup("untrack") == nil {
		t.Fatal("nil mapper for /untrack")
	}
}
