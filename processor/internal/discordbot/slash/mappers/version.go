package mappers

import "github.com/bwmarrin/discordgo"

// Version maps /version options to text tokens.
// /version has no options; always returns an empty token slice.
func Version(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
	return nil, nil
}
