package mappers

import (
	"reflect"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestRaidMapperNeitherBossNorLevel(t *testing.T) {
	_, err := Raid(nil)
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected *MapperError, got %T", err)
	}
	if me.Key != "error.slash.raid.need_boss_or_level" {
		t.Errorf("key=%q", me.Key)
	}
}

func TestRaidMapperBossAndLevel(t *testing.T) {
	_, err := Raid([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("boss", "Mewtwo"),
		sopt("level", "5"),
	})
	me, ok := err.(*MapperError)
	if !ok {
		t.Fatalf("expected *MapperError, got %T", err)
	}
	if me.Key != "error.slash.raid.boss_and_level" {
		t.Errorf("key=%q", me.Key)
	}
}

func TestRaidMapperBossOnly(t *testing.T) {
	tokens, err := Raid([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("boss", "Mewtwo"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tokens, []string{"mewtwo"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestRaidMapperLevelOnly(t *testing.T) {
	tokens, err := Raid([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("level", "5"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(tokens, []string{"5"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestRaidMapperLevelLowercased(t *testing.T) {
	tokens, _ := Raid([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("level", "Legendary"),
	})
	if !reflect.DeepEqual(tokens, []string{"legendary"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestRaidMapperTeam(t *testing.T) {
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
		tokens, err := Raid([]*discordgo.ApplicationCommandInteractionDataOption{
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

func TestRaidMapperDistance(t *testing.T) {
	tokens, _ := Raid([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("level", "5"),
		iopt("distance", 750),
	})
	want := []string{"5", "d750"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestRaidMapperClean(t *testing.T) {
	tokens, _ := Raid([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("level", "5"),
		bopt("clean", true),
	})
	if !reflect.DeepEqual(tokens, []string{"5", "clean"}) {
		t.Errorf("tokens=%v", tokens)
	}
}

func TestRaidMapperTemplate(t *testing.T) {
	tokens, _ := Raid([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("boss", "mewtwo"),
		sopt("template", "rsvp"),
	})
	want := []string{"mewtwo", "template:rsvp"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestRaidMapperAllOptions(t *testing.T) {
	tokens, err := Raid([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("level", "mega"),
		iopt("team", 2),
		iopt("distance", 1000),
		bopt("clean", true),
		sopt("template", "fancy"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"mega", "valor", "d1000", "clean", "template:fancy"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestRaidMapperLocationAreas(t *testing.T) {
	tokens, err := Raid([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("boss", "mewtwo"),
		sopt("location", "Work"),
		sopt("areas", "london"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"mewtwo", "location:Work", "area:london"}
	if !reflect.DeepEqual(tokens, want) {
		t.Errorf("tokens=%v want %v", tokens, want)
	}
}

func TestLookupRaid(t *testing.T) {
	if Lookup("raid") == nil {
		t.Fatal("nil mapper for /raid")
	}
}
