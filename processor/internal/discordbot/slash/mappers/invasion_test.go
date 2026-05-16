package mappers

import (
	"reflect"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestInvasionMapperMissingGrunt(t *testing.T) {
	_, err := Invasion(nil)
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected MapperError, got %T", err)
	}
	if me.Key != "error.slash.invasion.no_grunt" {
		t.Errorf("key=%q", me.Key)
	}
}

func TestInvasionMapperEmptyGrunt(t *testing.T) {
	_, err := Invasion([]*discordgo.ApplicationCommandInteractionDataOption{sopt("grunt_type", "")})
	if _, ok := err.(*MapperError); !ok {
		t.Fatalf("expected MapperError, got %v", err)
	}
}

func TestInvasionMapperGruntOnly(t *testing.T) {
	tokens, err := Invasion([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("grunt_type", "Dragon"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tokens, []string{"dragon"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestInvasionMapperAllOptions(t *testing.T) {
	tokens, err := Invasion([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("grunt_type", "kecleon"),
		iopt("distance", 500),
		bopt("clean", true),
		sopt("template", "fancy"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"kecleon", "d500", "clean", "template:fancy"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestLookupInvasion(t *testing.T) {
	if Lookup("invasion") == nil {
		t.Fatal("nil mapper for /invasion")
	}
}
