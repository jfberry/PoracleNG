package mappers

import (
	"strings"

	"github.com/bwmarrin/discordgo"
)

// Raid maps /raid options to text-command tokens.
//
// Options:
//
//	boss      (string, autocomplete) — pokemon name/ID (RecentActivity-boosted)
//	level     (string, choices)      — raid level keyword (5, mega, legendary, ...)
//	team      (int,    choices)      — 0..3 (harmony/mystic/valor/instinct)
//	distance  (int)                  — alert radius in metres
//	clean     (bool)                  — auto-delete on expiry
//	template  (string, autocomplete) — DTS template name
//	costume   (string, autocomplete) — raid boss costume (RecentActivity-boosted)
//	form      (string, autocomplete) — raid boss form (RecentActivity-boosted)
//
// Validation: exactly one of boss or level must be set. The text bot
// distinguishes these in the same argument position, but slash users would
// be confused if both were accepted at once — and the text bot does not
// allow that combination either (gym mode aside).
func Raid(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
	o := flattenOptions(opts)

	boss := getString(o["boss"])
	level := getString(o["level"])
	if boss == "" && level == "" {
		return nil, &MapperError{Key: "error.slash.raid.need_boss_or_level"}
	}
	if boss != "" && level != "" {
		return nil, &MapperError{Key: "error.slash.raid.boss_and_level"}
	}

	tokens := []string{}
	if boss != "" {
		tokens = append(tokens, strings.ToLower(boss))
	}
	if level != "" {
		tokens = append(tokens, strings.ToLower(level))
	}

	if v, ok := o["team"]; ok {
		if name := teamNameForValue(int(v.IntValue())); name != "" {
			tokens = append(tokens, name)
		}
	}

	if costume := getString(o["costume"]); costume != "" {
		tokens = append(tokens, "costume:"+costume)
	}

	if form := getString(o["form"]); form != "" {
		tokens = append(tokens, "form:"+form)
	}

	appendCommonTail(&tokens, o)

	return tokens, nil
}

func init() { registry["raid"] = Raid }
