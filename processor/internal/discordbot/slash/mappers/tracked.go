package mappers

import "github.com/bwmarrin/discordgo"

// Tracked maps /tracked options to text tokens.
// /tracked has no options; always returns an empty token slice.
func Tracked(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
	return nil, nil
}

func init() { registry["tracked"] = Tracked }
