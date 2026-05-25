package mappers

import (
	"reflect"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestNestMapperMissingPokemon(t *testing.T) {
	_, err := Nest(nil)
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected MapperError, got %T", err)
	}
	if me.Key != "error.slash.nest.no_pokemon" {
		t.Errorf("key=%q", me.Key)
	}
}

func TestNestMapperPokemonLowercased(t *testing.T) {
	tokens, _ := Nest([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "Pikachu"),
	})
	if !reflect.DeepEqual(tokens, []string{"pikachu"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestNestMapperMinSpawnAvg(t *testing.T) {
	tokens, _ := Nest([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "pikachu"),
		iopt("min_spawn_avg", 5),
	})
	want := []string{"pikachu", "minspawn5"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestNestMapperMinSpawnAvgZeroOmitted(t *testing.T) {
	tokens, _ := Nest([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "pikachu"),
		iopt("min_spawn_avg", 0),
	})
	if !reflect.DeepEqual(tokens, []string{"pikachu"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestNestMapperAllOptions(t *testing.T) {
	tokens, err := Nest([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "pikachu"),
		iopt("min_spawn_avg", 3),
		iopt("distance", 800),
		bopt("clean", true),
		sopt("template", "fancy"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"pikachu", "minspawn3", "d800", "clean", "template:fancy"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestNestMapperLocationAreas(t *testing.T) {
	tokens, err := Nest([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "pikachu"),
		sopt("location", "Home"),
		sopt("areas", "westside"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"pikachu", "location:Home", "area:westside"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestLookupNest(t *testing.T) {
	if Lookup("nest") == nil {
		t.Fatal("nil mapper for /nest")
	}
}
