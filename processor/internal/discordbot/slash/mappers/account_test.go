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
		{"add", []*discordgo.ApplicationCommandInteractionDataOption{subopt("add", sopt("player_id", "User_ABC"), sopt("name", "Main"))}, []string{"add", "Main", "User_ABC"}},
		{"add no name", []*discordgo.ApplicationCommandInteractionDataOption{subopt("add", sopt("player_id", "User_ABC"))}, []string{"add", "User_ABC"}},
		{"remove", []*discordgo.ApplicationCommandInteractionDataOption{subopt("remove", sopt("account", "Main"))}, []string{"remove", "Main"}},
		{"rename", []*discordgo.ApplicationCommandInteractionDataOption{subopt("rename", sopt("account", "User_ABC"), sopt("name", "alt"))}, []string{"rename", "User_ABC", "alt"}},
		{"rename clear", []*discordgo.ApplicationCommandInteractionDataOption{subopt("rename", sopt("account", "User_ABC"))}, []string{"rename", "User_ABC"}},
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
		{subopt("rename")},
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
