package mappers

import (
	"reflect"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestAreaMapperNoSubcommand(t *testing.T) {
	_, err := Area(nil)
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected MapperError, got %T", err)
	}
	if me.Key != "error.slash.area.no_subcommand" {
		t.Errorf("key=%q", me.Key)
	}
}

func TestAreaMapperAddRequiresArea(t *testing.T) {
	_, err := Area([]*discordgo.ApplicationCommandInteractionDataOption{
		subopt("add"),
	})
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected MapperError, got %T", err)
	}
	if me.Key != "error.slash.area.no_area" {
		t.Errorf("key=%q", me.Key)
	}
}

func TestAreaMapperAdd(t *testing.T) {
	tokens, err := Area([]*discordgo.ApplicationCommandInteractionDataOption{
		subopt("add", sopt("area", "London")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tokens, []string{"add", "London"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestAreaMapperRemove(t *testing.T) {
	tokens, err := Area([]*discordgo.ApplicationCommandInteractionDataOption{
		subopt("remove", sopt("area", "Paris")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tokens, []string{"remove", "Paris"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestAreaMapperRemoveRequiresArea(t *testing.T) {
	_, err := Area([]*discordgo.ApplicationCommandInteractionDataOption{
		subopt("remove"),
	})
	if _, ok := err.(*MapperError); !ok {
		t.Fatalf("expected MapperError, got %v", err)
	}
}

func TestAreaMapperShowEmitsNoTokens(t *testing.T) {
	tokens, err := Area([]*discordgo.ApplicationCommandInteractionDataOption{
		subopt("show"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 0 {
		t.Errorf("show should emit no tokens, got %v", tokens)
	}
}

func TestAreaMapperUnknownSubcommand(t *testing.T) {
	_, err := Area([]*discordgo.ApplicationCommandInteractionDataOption{
		subopt("bogus"),
	})
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected MapperError, got %T", err)
	}
	if me.Key != "error.slash.area.unknown_subcommand" {
		t.Errorf("key=%q", me.Key)
	}
}

func TestLookupArea(t *testing.T) {
	if Lookup("area") == nil {
		t.Fatal("nil mapper for /area")
	}
}
