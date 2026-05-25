package mappers

import (
	"reflect"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestEggMapperMissingLevel(t *testing.T) {
	_, err := Egg(nil)
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected *MapperError, got %T", err)
	}
	if me.Key != "error.slash.egg.no_level" {
		t.Errorf("key=%q", me.Key)
	}
}

func TestEggMapperEmptyLevel(t *testing.T) {
	_, err := Egg([]*discordgo.ApplicationCommandInteractionDataOption{sopt("level", "")})
	if _, ok := err.(*MapperError); !ok {
		t.Fatalf("expected *MapperError, got %v", err)
	}
}

func TestEggMapperLevelOnly(t *testing.T) {
	tokens, err := Egg([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("level", "5"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tokens, []string{"5"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestEggMapperLevelLowercased(t *testing.T) {
	tokens, _ := Egg([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("level", "Legendary"),
	})
	if !reflect.DeepEqual(tokens, []string{"legendary"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestEggMapperTeam(t *testing.T) {
	cases := []struct {
		teamVal int
		want    string
	}{
		{0, "harmony"},
		{1, "mystic"},
		{2, "valor"},
		{3, "instinct"},
	}
	for _, c := range cases {
		tokens, err := Egg([]*discordgo.ApplicationCommandInteractionDataOption{
			sopt("level", "5"),
			iopt("team", c.teamVal),
		})
		if err != nil {
			t.Fatalf("team=%d: %v", c.teamVal, err)
		}
		want := []string{"5", c.want}
		if !reflect.DeepEqual(tokens, want) {
			t.Errorf("team=%d: tokens=%v want %v", c.teamVal, tokens, want)
		}
	}
}

func TestEggMapperDistance(t *testing.T) {
	tokens, _ := Egg([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("level", "5"),
		iopt("distance", 200),
	})
	want := []string{"5", "d200"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestEggMapperClean(t *testing.T) {
	tokens, _ := Egg([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("level", "5"),
		bopt("clean", true),
	})
	if !reflect.DeepEqual(tokens, []string{"5", "clean"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestEggMapperTemplate(t *testing.T) {
	tokens, _ := Egg([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("level", "5"),
		sopt("template", "fancy"),
	})
	want := []string{"5", "template:fancy"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestEggMapperAllOptions(t *testing.T) {
	tokens, err := Egg([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("level", "mega"),
		iopt("team", 3),
		iopt("distance", 1000),
		bopt("clean", true),
		sopt("template", "rsvp"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"mega", "instinct", "d1000", "clean", "template:rsvp"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestEggMapperLocationAreas(t *testing.T) {
	tokens, err := Egg([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("level", "5"),
		sopt("location", "Home"),
		sopt("areas", "paris,berlin"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"5", "location:Home", "area:paris,berlin"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestLookupEgg(t *testing.T) {
	if Lookup("egg") == nil {
		t.Fatal("nil mapper for /egg")
	}
}

func TestTeamNameForValue(t *testing.T) {
	cases := map[int]string{
		0: "harmony",
		1: "mystic",
		2: "valor",
		3: "instinct",
		4: "",
	}
	for v, want := range cases {
		if got := teamNameForValue(v); got != want {
			t.Errorf("teamNameForValue(%d)=%q want %q", v, got, want)
		}
	}
}
