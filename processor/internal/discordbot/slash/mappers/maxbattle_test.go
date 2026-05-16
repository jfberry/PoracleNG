package mappers

import (
	"reflect"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestMaxbattleMapperMissingPokemon(t *testing.T) {
	_, err := Maxbattle(nil)
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected MapperError, got %T", err)
	}
	if me.Key != "error.slash.maxbattle.no_pokemon" {
		t.Errorf("key=%q", me.Key)
	}
}

func TestMaxbattleMapperPokemonLowercased(t *testing.T) {
	tokens, _ := Maxbattle([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "Charizard"),
	})
	if !reflect.DeepEqual(tokens, []string{"charizard"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestMaxbattleMapperLevel(t *testing.T) {
	tokens, _ := Maxbattle([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "charizard"),
		iopt("level", 6),
	})
	want := []string{"charizard", "level6"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestMaxbattleMapperLevelZeroOmitted(t *testing.T) {
	tokens, _ := Maxbattle([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "charizard"),
		iopt("level", 0),
	})
	if !reflect.DeepEqual(tokens, []string{"charizard"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestMaxbattleMapperGmax(t *testing.T) {
	tokens, _ := Maxbattle([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "charizard"),
		bopt("gmax", true),
	})
	want := []string{"charizard", "gmax"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestMaxbattleMapperGmaxFalseOmitted(t *testing.T) {
	tokens, _ := Maxbattle([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "charizard"),
		bopt("gmax", false),
	})
	if !reflect.DeepEqual(tokens, []string{"charizard"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestMaxbattleMapperAllOptions(t *testing.T) {
	tokens, err := Maxbattle([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "charizard"),
		iopt("level", 5),
		bopt("gmax", true),
		iopt("distance", 1000),
		bopt("clean", true),
		sopt("template", "fancy"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"charizard", "level5", "gmax", "d1000", "clean", "template:fancy"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestLookupMaxbattle(t *testing.T) {
	if Lookup("maxbattle") == nil {
		t.Fatal("nil mapper for /maxbattle")
	}
}
