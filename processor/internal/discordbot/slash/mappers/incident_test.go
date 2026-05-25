package mappers

import (
	"reflect"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestIncidentMapperTypeRequired(t *testing.T) {
	_, err := Incident(nil)
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected *MapperError, got %T", err)
	}
	if me.Key != "error.slash.incident.no_type" {
		t.Errorf("key=%q", me.Key)
	}
}

func TestIncidentMapperTypeOnly(t *testing.T) {
	tokens, err := Incident([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("type", "kecleon"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tokens, []string{"kecleon"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestIncidentMapperLocationAreas(t *testing.T) {
	tokens, err := Incident([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("type", "kecleon"),
		sopt("location", "Home"),
		sopt("areas", "city"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"kecleon", "location:Home", "area:city"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestLookupIncident(t *testing.T) {
	if Lookup("incident") == nil {
		t.Fatal("nil mapper for /incident")
	}
}
