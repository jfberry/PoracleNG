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

func TestUntrackMapperEmitsIDToken(t *testing.T) {
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

// Sub-command names other than pokemon/raid/etc. still produce the right
// token shape — the mapper does not validate sub-command names, that's
// purely a Discord-side concern.
func TestUntrackMapperAnySubcommandUID(t *testing.T) {
	tokens, err := Untrack([]*discordgo.ApplicationCommandInteractionDataOption{
		subopt("gym", sopt("tracking", "123")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tokens, []string{"id:123"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestLookupUntrack(t *testing.T) {
	if Lookup("untrack") == nil {
		t.Fatal("nil mapper for /untrack")
	}
}
