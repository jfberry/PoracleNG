package mappers

import (
	"reflect"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestLocationMapperNoSubcommand(t *testing.T) {
	_, err := Location(nil)
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected MapperError, got %T", err)
	}
	if me.Key != "error.slash.location.no_subcommand" {
		t.Errorf("key=%q", me.Key)
	}
}

func TestLocationMapperAdd(t *testing.T) {
	tokens, err := Location([]*discordgo.ApplicationCommandInteractionDataOption{
		subopt("add", sopt("name", "Home"), sopt("place", "51.5,-0.1")),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"add", "Home", "51.5,-0.1"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("got %v want %v", tokens, want)
	}
}

func TestLocationMapperAddPlaceName(t *testing.T) {
	// Place names are passed through unchanged; geocoding is the text command's job.
	tokens, err := Location([]*discordgo.ApplicationCommandInteractionDataOption{
		subopt("add", sopt("name", "Work"), sopt("place", "London")),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"add", "Work", "London"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("got %v want %v", tokens, want)
	}
}

func TestLocationMapperAddMissingName(t *testing.T) {
	_, err := Location([]*discordgo.ApplicationCommandInteractionDataOption{
		subopt("add", sopt("place", "51.5,-0.1")),
	})
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected MapperError, got %T", err)
	}
	if me.Key != "error.slash.location.no_name" {
		t.Errorf("key=%q", me.Key)
	}
}

func TestLocationMapperAddMissingPlace(t *testing.T) {
	_, err := Location([]*discordgo.ApplicationCommandInteractionDataOption{
		subopt("add", sopt("name", "Home")),
	})
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected MapperError, got %T", err)
	}
	if me.Key != "error.slash.location.empty" {
		t.Errorf("key=%q", me.Key)
	}
}

func TestLocationMapperList(t *testing.T) {
	tokens, err := Location([]*discordgo.ApplicationCommandInteractionDataOption{
		subopt("list"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"list"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("got %v want %v", tokens, want)
	}
}

func TestLocationMapperShow(t *testing.T) {
	tokens, err := Location([]*discordgo.ApplicationCommandInteractionDataOption{
		subopt("show", sopt("name", "Home")),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"show", "Home"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("got %v want %v", tokens, want)
	}
}

func TestLocationMapperRemove(t *testing.T) {
	tokens, err := Location([]*discordgo.ApplicationCommandInteractionDataOption{
		subopt("remove", sopt("name", "Home")),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"remove", "Home"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("got %v want %v", tokens, want)
	}
}

func TestLocationMapperSetDefault(t *testing.T) {
	tokens, err := Location([]*discordgo.ApplicationCommandInteractionDataOption{
		subopt("set-default", sopt("place", "51.5,-0.1")),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"51.5,-0.1"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("got %v want %v", tokens, want)
	}
}

func TestLocationMapperSetDefaultPlaceName(t *testing.T) {
	tokens, err := Location([]*discordgo.ApplicationCommandInteractionDataOption{
		subopt("set-default", sopt("place", "London")),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"London"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("got %v want %v", tokens, want)
	}
}

func TestLocationMapperSetDefaultMissingPlace(t *testing.T) {
	_, err := Location([]*discordgo.ApplicationCommandInteractionDataOption{
		subopt("set-default"),
	})
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected MapperError, got %T", err)
	}
	if me.Key != "error.slash.location.empty" {
		t.Errorf("key=%q", me.Key)
	}
}

func TestLocationMapperRemoveDefault(t *testing.T) {
	tokens, err := Location([]*discordgo.ApplicationCommandInteractionDataOption{
		subopt("remove-default"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"remove", "default"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("got %v want %v", tokens, want)
	}
}

func TestLocationInRegistry(t *testing.T) {
	if Lookup("location") == nil {
		t.Fatal("/location must be in the shared registry")
	}
}
