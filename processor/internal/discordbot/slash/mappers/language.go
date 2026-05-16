package mappers

import (
	"strings"

	"github.com/bwmarrin/discordgo"
)

// Language maps /language options to text tokens.
// Optional "code" string option becomes a single positional token,
// lower-cased so locale codes match regardless of how the user typed
// them (Discord choices already lower-case the value, but a manually
// typed value from a renamed option must be normalised).
func Language(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
	o := flattenOptions(opts)
	if v, ok := o["code"]; ok && v.StringValue() != "" {
		return []string{strings.ToLower(v.StringValue())}, nil
	}
	return nil, nil
}

func init() { registry["language"] = Language }
