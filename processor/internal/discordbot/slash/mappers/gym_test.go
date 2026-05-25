package mappers

import (
	"reflect"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestGymMapperNoOptions(t *testing.T) {
	// All options optional — no error, empty token list (matches !gym
	// behaviour where bare !gym tracks any team with default settings).
	tokens, err := Gym(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 0 {
		t.Errorf("tokens=%v, want empty", tokens)
	}
}

func TestGymMapperTeam(t *testing.T) {
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
		tokens, err := Gym([]*discordgo.ApplicationCommandInteractionDataOption{
			iopt("team", c.teamVal),
		})
		if err != nil {
			t.Fatalf("team=%d: %v", c.teamVal, err)
		}
		if !reflect.DeepEqual(tokens, []string{c.want}) {
			t.Errorf("team=%d: tokens=%v want %v", c.teamVal, tokens, []string{c.want})
		}
	}
}

func TestGymMapperSlotChanges(t *testing.T) {
	tokens, _ := Gym([]*discordgo.ApplicationCommandInteractionDataOption{
		bopt("slot_changes", true),
	})
	// Multi-word token preserved as-is.
	if !reflect.DeepEqual(tokens, []string{"slot changes"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestGymMapperBattleChanges(t *testing.T) {
	tokens, _ := Gym([]*discordgo.ApplicationCommandInteractionDataOption{
		bopt("battle_changes", true),
	})
	if !reflect.DeepEqual(tokens, []string{"battle changes"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestGymMapperAllOptions(t *testing.T) {
	tokens, err := Gym([]*discordgo.ApplicationCommandInteractionDataOption{
		iopt("team", 1),
		bopt("slot_changes", true),
		bopt("battle_changes", true),
		iopt("distance", 800),
		bopt("clean", true),
		sopt("template", "fancy"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"mystic", "slot changes", "battle changes", "d800", "clean", "template:fancy"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestGymMapperLocationAreas(t *testing.T) {
	tokens, err := Gym([]*discordgo.ApplicationCommandInteractionDataOption{
		iopt("team", 1),
		sopt("location", "Home"),
		sopt("areas", "north,south"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"mystic", "location:Home", "area:north,south"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestLookupGym(t *testing.T) {
	if Lookup("gym") == nil {
		t.Fatal("nil mapper for /gym")
	}
}
