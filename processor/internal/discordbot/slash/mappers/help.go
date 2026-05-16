package mappers

import (
	"strings"

	"github.com/bwmarrin/discordgo"
)

// Help maps /help options to text tokens.
// Optional "topic" string option becomes a single positional token,
// lower-cased for consistent matching by the text-command parser.
func Help(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
	o := flattenOptions(opts)
	if v, ok := o["topic"]; ok && v.StringValue() != "" {
		return []string{strings.ToLower(v.StringValue())}, nil
	}
	return nil, nil
}

func init() { registry["help"] = Help }
