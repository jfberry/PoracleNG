package mappers

import (
	"github.com/bwmarrin/discordgo"
)

// Gym maps /gym options to text-command tokens.
//
// Options:
//
//	team            (int, choices)        — 0..3 (harmony/mystic/valor/instinct)
//	slot_changes    (bool)                 — alert on team slot composition changes
//	battle_changes  (bool)                 — alert on battle state changes
//	distance        (int)                 — alert radius in metres
//	clean           (bool)                 — auto-delete on expiry
//	template        (string, autocomplete) — DTS template name
//
// `slot changes` and `battle changes` are multi-word keywords in the text
// grammar (see arg.slot_changes / arg.battle_changes in en.json). We emit
// each as a single space-containing token; the dispatcher passes the
// joined token through the multi-line splitter intact (the tokenizer
// preserves quoted/space-bearing tokens once they're produced by a
// mapper, since they bypass shell-style tokenization).
func Gym(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
	o := flattenOptions(opts)
	tokens := []string{}

	if v, ok := o["team"]; ok {
		// Team is an int choice 0..3; teamNameForValue returns "" for
		// out-of-range values which we omit (omitting team token = any).
		if name := teamNameForValue(int(v.IntValue())); name != "" {
			tokens = append(tokens, name)
		}
	}
	if v, ok := o["slot_changes"]; ok && v.BoolValue() {
		tokens = append(tokens, "slot changes")
	}
	if v, ok := o["battle_changes"]; ok && v.BoolValue() {
		tokens = append(tokens, "battle changes")
	}
	appendDistance(&tokens, o["distance"])
	if v, ok := o["clean"]; ok && v.BoolValue() {
		tokens = append(tokens, "clean")
	}
	if v, ok := o["template"]; ok && v.StringValue() != "" {
		tokens = append(tokens, "template:"+v.StringValue())
	}
	return tokens, nil
}

func init() { registry["gym"] = Gym }
