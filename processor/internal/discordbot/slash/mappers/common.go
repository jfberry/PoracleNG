package mappers

import "github.com/bwmarrin/discordgo"

// flattenOptions converts a slice of ApplicationCommandInteractionDataOption
// into a map keyed by option Name for O(1) lookup by mappers.
//
// Does not recurse into sub-commands; sub-command-aware mappers (e.g. /area,
// /profile, /untrack) iterate opts directly.
func flattenOptions(opts []*discordgo.ApplicationCommandInteractionDataOption) map[string]*discordgo.ApplicationCommandInteractionDataOption {
	out := make(map[string]*discordgo.ApplicationCommandInteractionDataOption, len(opts))
	for _, o := range opts {
		out[o.Name] = o
	}
	return out
}
