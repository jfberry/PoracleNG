package mappers

import "github.com/bwmarrin/discordgo"

// Info maps /info options to text tokens.
// /info has no options; always returns an empty token slice.
func Info(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
	return nil, nil
}

func init() { registry["info"] = Info }
