package mappers

import (
	"reflect"
	"testing"

	"github.com/bwmarrin/discordgo"
)

// helper: build a string option
func sopt(name, val string) *discordgo.ApplicationCommandInteractionDataOption {
	return &discordgo.ApplicationCommandInteractionDataOption{
		Name:  name,
		Type:  discordgo.ApplicationCommandOptionString,
		Value: val,
	}
}

// helper: build an int option (Value must be float64 for IntValue cast).
func iopt(name string, val int) *discordgo.ApplicationCommandInteractionDataOption {
	return &discordgo.ApplicationCommandInteractionDataOption{
		Name:  name,
		Type:  discordgo.ApplicationCommandOptionInteger,
		Value: float64(val),
	}
}

// helper: build a bool option
func bopt(name string, val bool) *discordgo.ApplicationCommandInteractionDataOption {
	return &discordgo.ApplicationCommandInteractionDataOption{
		Name:  name,
		Type:  discordgo.ApplicationCommandOptionBoolean,
		Value: val,
	}
}

func TestTrackMapperMissingPokemon(t *testing.T) {
	_, err := Track(nil)
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected *MapperError, got %T (%v)", err, err)
	}
	if me.Key != "error.slash.track.no_pokemon" {
		t.Errorf("key=%q", me.Key)
	}
}

func TestTrackMapperEmptyPokemonString(t *testing.T) {
	_, err := Track([]*discordgo.ApplicationCommandInteractionDataOption{sopt("pokemon", "")})
	if _, ok := err.(*MapperError); !ok {
		t.Fatalf("expected MapperError for empty pokemon, got %v", err)
	}
}

func TestTrackMapperPokemonOnly(t *testing.T) {
	tokens, err := Track([]*discordgo.ApplicationCommandInteractionDataOption{sopt("pokemon", "25")})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"25"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v, want %v", tokens, want)
	}
}

func TestTrackMapperPokemonLowercased(t *testing.T) {
	// "Everything" picker value gets lowercased to "everything" token.
	tokens, err := Track([]*discordgo.ApplicationCommandInteractionDataOption{sopt("pokemon", "Everything")})
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 1 || tokens[0] != "everything" {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestTrackMapperIV(t *testing.T) {
	tokens, _ := Track([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "25"),
		sopt("iv", "100"),
	})
	want := []string{"25", "iv100"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestTrackMapperIVEmptyOmitted(t *testing.T) {
	// Empty iv option drops the token.
	tokens, _ := Track([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "25"),
		sopt("iv", ""),
	})
	if !reflect.DeepEqual(tokens, []string{"25"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestTrackMapperPVPRanks(t *testing.T) {
	tokens, _ := Track([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "25"),
		iopt("great_rank", 5),
		iopt("ultra_rank", 10),
		iopt("little_rank", 3),
	})
	want := []string{"25", "great5", "ultra10", "little3"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestTrackMapperPVPRankZeroOmitted(t *testing.T) {
	// Zero rank is "not set" — produce no token.
	tokens, _ := Track([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "25"),
		iopt("great_rank", 0),
	})
	if !reflect.DeepEqual(tokens, []string{"25"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestTrackMapperDistance(t *testing.T) {
	tokens, _ := Track([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "25"),
		iopt("distance", 500),
	})
	want := []string{"25", "d500"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestTrackMapperDistanceZeroOmitted(t *testing.T) {
	tokens, _ := Track([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "25"),
		iopt("distance", 0),
	})
	if !reflect.DeepEqual(tokens, []string{"25"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestTrackMapperClean(t *testing.T) {
	tokens, _ := Track([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "25"),
		bopt("clean", true),
	})
	want := []string{"25", "clean"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestTrackMapperCleanFalseOmitted(t *testing.T) {
	tokens, _ := Track([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "25"),
		bopt("clean", false),
	})
	if !reflect.DeepEqual(tokens, []string{"25"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestTrackMapperTemplate(t *testing.T) {
	tokens, _ := Track([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "25"),
		sopt("template", "fancy"),
	})
	want := []string{"25", "template:fancy"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestTrackMapperForm(t *testing.T) {
	tokens, _ := Track([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "25"),
		sopt("form", "alola"),
	})
	want := []string{"25", "form:alola"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestTrackMapperSize(t *testing.T) {
	tokens, _ := Track([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "25"),
		sopt("size", "xxl"),
	})
	want := []string{"25", "xxl"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestTrackMapperSizeAllOmitted(t *testing.T) {
	// "all" is the catch-all and must not emit a size token.
	tokens, _ := Track([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "25"),
		sopt("size", "all"),
	})
	if !reflect.DeepEqual(tokens, []string{"25"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestTrackMapperAllOptions(t *testing.T) {
	tokens, err := Track([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "25"),
		sopt("iv", "95"),
		iopt("great_rank", 5),
		iopt("ultra_rank", 10),
		iopt("little_rank", 7),
		iopt("distance", 250),
		bopt("clean", true),
		sopt("template", "pvp"),
		sopt("form", "alola"),
		sopt("size", "xl"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"25", "iv95", "great5", "ultra10", "little7", "d250", "clean", "template:pvp", "form:alola", "xl"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestTrackMapperLocationAreas(t *testing.T) {
	tokens, err := Track([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "25"),
		iopt("distance", 500),
		sopt("location", "Home"),
		sopt("areas", "berlin,munich"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"25", "d500", "location:Home", "area:berlin,munich"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestTrackMapperLocationEmpty(t *testing.T) {
	// Empty location option produces no token.
	tokens, _ := Track([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "25"),
		sopt("location", ""),
	})
	if !reflect.DeepEqual(tokens, []string{"25"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestLookupTrack(t *testing.T) {
	if Lookup("track") == nil {
		t.Fatal("nil mapper for /track")
	}
}
