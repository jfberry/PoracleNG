package mappers

import (
	"reflect"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestProfileMapperNoSubcommand(t *testing.T) {
	_, err := Profile(nil)
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected MapperError, got %T", err)
	}
	if me.Key != "error.slash.profile.no_subcommand" {
		t.Errorf("key=%q", me.Key)
	}
}

func TestProfileMapperList(t *testing.T) {
	tokens, err := Profile([]*discordgo.ApplicationCommandInteractionDataOption{
		subopt("list"),
	})
	if err != nil {
		t.Fatal(err)
	}
	// "list" maps to arg.list which the text bot's ProfileCommand handles
	// as listProfiles.
	if !reflect.DeepEqual(tokens, []string{"list"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestProfileMapperChange(t *testing.T) {
	tokens, err := Profile([]*discordgo.ApplicationCommandInteractionDataOption{
		subopt("change", sopt("name", "weekend")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tokens, []string{"switch", "weekend"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestProfileMapperChangeRequiresName(t *testing.T) {
	_, err := Profile([]*discordgo.ApplicationCommandInteractionDataOption{
		subopt("change"),
	})
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected MapperError, got %T", err)
	}
	if me.Key != "error.slash.profile.no_name" {
		t.Errorf("key=%q", me.Key)
	}
}

func TestProfileMapperCreate(t *testing.T) {
	tokens, err := Profile([]*discordgo.ApplicationCommandInteractionDataOption{
		subopt("create", sopt("name", "holiday")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tokens, []string{"add", "holiday"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestProfileMapperCreateRequiresName(t *testing.T) {
	_, err := Profile([]*discordgo.ApplicationCommandInteractionDataOption{
		subopt("create"),
	})
	if _, ok := err.(*MapperError); !ok {
		t.Fatalf("expected MapperError, got %v", err)
	}
}

func TestProfileMapperDelete(t *testing.T) {
	tokens, err := Profile([]*discordgo.ApplicationCommandInteractionDataOption{
		subopt("delete", sopt("name", "spring")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tokens, []string{"remove", "spring"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestProfileMapperDeleteRequiresName(t *testing.T) {
	_, err := Profile([]*discordgo.ApplicationCommandInteractionDataOption{
		subopt("delete"),
	})
	if _, ok := err.(*MapperError); !ok {
		t.Fatalf("expected MapperError, got %v", err)
	}
}

func TestProfileMapperUnknownSubcommand(t *testing.T) {
	_, err := Profile([]*discordgo.ApplicationCommandInteractionDataOption{
		subopt("bogus", sopt("name", "x")),
	})
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected MapperError, got %T", err)
	}
	if me.Key != "error.slash.profile.unknown_subcommand" {
		t.Errorf("key=%q", me.Key)
	}
}

func TestLookupProfile(t *testing.T) {
	if Lookup("profile") == nil {
		t.Fatal("nil mapper for /profile")
	}
}
