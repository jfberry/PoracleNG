package mappers

import (
	"slices"
	"testing"

	"github.com/bwmarrin/discordgo"
)

func TestAccountMapper(t *testing.T) {
	cases := []struct {
		name string
		opts []*discordgo.ApplicationCommandInteractionDataOption
		want []string
	}{
		{"list", []*discordgo.ApplicationCommandInteractionDataOption{subopt("list")}, []string{"list"}},
		{"add", []*discordgo.ApplicationCommandInteractionDataOption{subopt("add", sopt("player_id", "User_ABC"), sopt("label", "Main"))}, []string{"add", "User_ABC", "Main"}},
		{"add no label", []*discordgo.ApplicationCommandInteractionDataOption{subopt("add", sopt("player_id", "User_ABC"))}, []string{"add", "User_ABC"}},
		{"remove", []*discordgo.ApplicationCommandInteractionDataOption{subopt("remove", sopt("account", "Main"))}, []string{"remove", "Main"}},
		{"label", []*discordgo.ApplicationCommandInteractionDataOption{subopt("label", sopt("account", "User_ABC"), sopt("label", "alt"))}, []string{"label", "User_ABC", "alt"}},
		{"label clear", []*discordgo.ApplicationCommandInteractionDataOption{subopt("label", sopt("account", "User_ABC"))}, []string{"label", "User_ABC"}},
	}
	for _, tc := range cases {
		got, err := Account(tc.opts)
		if err != nil || !slices.Equal(got, tc.want) {
			t.Errorf("%s: got %q, %v; want %q", tc.name, got, err, tc.want)
		}
	}
	for _, bad := range [][]*discordgo.ApplicationCommandInteractionDataOption{
		nil,
		{subopt("add")},
		{subopt("remove")},
		{subopt("label")},
	} {
		if _, err := Account(bad); err == nil {
			t.Errorf("expected error for %v", bad)
		}
	}
}

func TestTrackMapperPersonalOptions(t *testing.T) {
	got, err := Track([]*discordgo.ApplicationCommandInteractionDataOption{
		sopt("pokemon", "everything"), sopt("personal_shiny", "yes"), sopt("personal_background", "yes"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(got, "personalshiny") || !slices.Contains(got, "personalbackground") {
		t.Fatalf("personal tokens missing: %q", got)
	}
}
