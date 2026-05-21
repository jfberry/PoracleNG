package slash

import (
	"testing"

	"github.com/bwmarrin/discordgo"
)

// TestFormatSlashInvocation pins the wire format used in DM log entries.
// Three shapes cover the full slash vocabulary:
//
//   - flat options (/track pokemon:pikachu iv:90)
//   - sub-command with options (/info pokemon name:pikachu)
//   - bare sub-command (/info rarity)
//
// If this format ever changes, an admin reading historical log entries
// will see a discontinuity; pinning it makes that intentional.
func TestFormatSlashInvocation(t *testing.T) {
	cases := []struct {
		name string
		ic   *discordgo.InteractionCreate
		want string
	}{
		{
			name: "flat options",
			ic: &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
				Type: discordgo.InteractionApplicationCommand,
				Data: discordgo.ApplicationCommandInteractionData{
					Name: "track",
					Options: []*discordgo.ApplicationCommandInteractionDataOption{
						{Name: "pokemon", Type: discordgo.ApplicationCommandOptionString, Value: "pikachu"},
						{Name: "iv", Type: discordgo.ApplicationCommandOptionInteger, Value: float64(90)},
					},
				},
			}},
			want: "/track pokemon:pikachu iv:90",
		},
		{
			name: "sub-command with options",
			ic: &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
				Type: discordgo.InteractionApplicationCommand,
				Data: discordgo.ApplicationCommandInteractionData{
					Name: "info",
					Options: []*discordgo.ApplicationCommandInteractionDataOption{
						{Name: "pokemon", Type: discordgo.ApplicationCommandOptionSubCommand,
							Options: []*discordgo.ApplicationCommandInteractionDataOption{
								{Name: "name", Type: discordgo.ApplicationCommandOptionString, Value: "pikachu"},
							},
						},
					},
				},
			}},
			want: "/info pokemon name:pikachu",
		},
		{
			name: "bare sub-command",
			ic: &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
				Type: discordgo.InteractionApplicationCommand,
				Data: discordgo.ApplicationCommandInteractionData{
					Name: "info",
					Options: []*discordgo.ApplicationCommandInteractionDataOption{
						{Name: "rarity", Type: discordgo.ApplicationCommandOptionSubCommand},
					},
				},
			}},
			want: "/info rarity",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := formatSlashInvocation(tc.ic)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
