package mappers

import (
	"reflect"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestLureMapperMissingType(t *testing.T) {
	_, err := Lure(nil)
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected MapperError, got %T", err)
	}
	if me.Key != "error.slash.lure.no_type" {
		t.Errorf("key=%q", me.Key)
	}
}

func TestLureMapperEmptyType(t *testing.T) {
	_, err := Lure([]*discordgo.ApplicationCommandInteractionDataOption{sopt("lure_type", "")})
	if _, ok := err.(*MapperError); !ok {
		t.Fatalf("expected MapperError, got %v", err)
	}
}

func TestLureMapperTypeLowercased(t *testing.T) {
	tokens, _ := Lure([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("lure_type", "Glacial"),
	})
	if !reflect.DeepEqual(tokens, []string{"glacial"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestLureMapperAllTypes(t *testing.T) {
	for _, lure := range []string{"normal", "glacial", "mossy", "magnetic", "rainy", "sparkly"} {
		tokens, err := Lure([]*discordgo.ApplicationCommandInteractionDataOption{
			sopt("lure_type", lure),
		})
		if err != nil {
			t.Fatalf("%s: %v", lure, err)
		}
		if !reflect.DeepEqual(tokens, []string{lure}) {
			t.Errorf("%s: tokens=%v", lure, tokens)
		}
	}
}

func TestLureMapperAllOptions(t *testing.T) {
	tokens, err := Lure([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("lure_type", "mossy"),
		iopt("distance", 250),
		bopt("clean", true),
		sopt("template", "fancy"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"mossy", "d250", "clean", "template:fancy"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestLureMapperLocationAreas(t *testing.T) {
	tokens, err := Lure([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("lure_type", "glacial"),
		sopt("location", "Park"),
		sopt("areas", "zone1,zone2"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"glacial", "location:Park", "area:zone1,zone2"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestLookupLure(t *testing.T) {
	if Lookup("lure") == nil {
		t.Fatal("nil mapper for /lure")
	}
}
