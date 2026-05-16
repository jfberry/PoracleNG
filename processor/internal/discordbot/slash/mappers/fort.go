package mappers

import (
	"github.com/bwmarrin/discordgo"
)

// Fort maps /fort options to text-command tokens.
//
// Options:
//
//	fort_type      (int, required, choices) — 0=pokestop, 1=gym
//	include_empty  (bool)                    — alert on empty/unowned forts
//	distance       (int)                    — alert radius in metres
//	clean          (bool)                    — auto-delete on expiry
//	template       (string, autocomplete)   — DTS template name
//
// fort_type is required by Discord and resolved to a lowercase keyword
// (`pokestop` / `gym`) consumed by the bot's invasion arg matcher in
// processor/internal/bot/commands/fort.go. Out-of-range choice values
// produce an empty token which we skip — the matcher will then fall
// through to its `everything` default.
func Fort(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
	o := flattenOptions(opts)

	rawType, present := o["fort_type"]
	if !present {
		return nil, &MapperError{Key: "error.slash.fort.no_type"}
	}
	typ := fortTypeName(int(rawType.IntValue()))
	if typ == "" {
		return nil, &MapperError{Key: "error.slash.fort.no_type"}
	}

	tokens := []string{typ}
	if v, ok := o["include_empty"]; ok && v.BoolValue() {
		tokens = append(tokens, "include empty")
	}
	appendCommonTail(&tokens, o)
	return tokens, nil
}

func init() { registry["fort"] = Fort }
