package mappers

import (
	"reflect"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestFortMapperMissingType(t *testing.T) {
	// No fort_type → out-of-range → error.
	_, err := Fort(nil)
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected MapperError, got %T", err)
	}
	if me.Key != "error.slash.fort.no_type" {
		t.Errorf("key=%q", me.Key)
	}
}

func TestFortMapperPokestop(t *testing.T) {
	tokens, err := Fort([]*discordgo.ApplicationCommandInteractionDataOption{
		iopt("fort_type", 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tokens, []string{"pokestop"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestFortMapperGym(t *testing.T) {
	tokens, err := Fort([]*discordgo.ApplicationCommandInteractionDataOption{
		iopt("fort_type", 1),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tokens, []string{"gym"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestFortMapperOutOfRange(t *testing.T) {
	// Bogus fort_type value out of choice range → error.
	_, err := Fort([]*discordgo.ApplicationCommandInteractionDataOption{
		iopt("fort_type", 99),
	})
	if _, ok := err.(*MapperError); !ok {
		t.Fatalf("expected MapperError, got %v", err)
	}
}

func TestFortMapperIncludeEmpty(t *testing.T) {
	tokens, _ := Fort([]*discordgo.ApplicationCommandInteractionDataOption{
		iopt("fort_type", 0),
		bopt("include_empty", true),
	})
	want := []string{"pokestop", "include empty"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestFortMapperAllOptions(t *testing.T) {
	tokens, err := Fort([]*discordgo.ApplicationCommandInteractionDataOption{
		iopt("fort_type", 1),
		bopt("include_empty", true),
		iopt("distance", 1500),
		sopt("template", "fancy"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"gym", "include empty", "d1500", "template:fancy"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestLookupFort(t *testing.T) {
	if Lookup("fort") == nil {
		t.Fatal("nil mapper for /fort")
	}
}

func TestFortMapperLocationAreas(t *testing.T) {
	tokens, err := Fort([]*discordgo.ApplicationCommandInteractionDataOption{
		iopt("fort_type", 0),
		sopt("location", "Work"),
		sopt("areas", "midtown"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"pokestop", "location:Work", "area:midtown"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestFortTypeName(t *testing.T) {
	cases := map[int]string{
		0:   "pokestop",
		1:   "gym",
		2:   "station",
		3:   "everything",
		4:   "",
		-1:  "",
		999: "",
	}
	for v, want := range cases {
		if got := fortTypeName(v); got != want {
			t.Errorf("fortTypeName(%d)=%q want %q", v, got, want)
		}
	}
}

// station and everything are fort types !fort accepts; /fort must offer them.
func TestFortMapperStationAndEverything(t *testing.T) {
	for v, want := range map[int]string{2: "station", 3: "everything"} {
		tokens, err := Fort([]*discordgo.ApplicationCommandInteractionDataOption{
			iopt("fort_type", v),
		})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(tokens, []string{want}) {
			t.Errorf("fort_type %d: tokens=%v want [%s]", v, tokens, want)
		}
	}
}
