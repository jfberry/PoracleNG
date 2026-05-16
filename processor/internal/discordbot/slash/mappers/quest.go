package mappers

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// Quest maps /quest options to text-command tokens.
//
// Options (all reward types are mutually exclusive — exactly one must be set):
//
//	pokemon     (string, autocomplete) — pokemon name/ID for pokemon-reward quests
//	item        (string, autocomplete) — item name (golden razz, etc.)
//	stardust    (int)                  — minimum stardust amount
//	candy       (string, autocomplete) — candy reward pokemon
//	mega_energy (string, autocomplete) — mega energy reward pokemon
//	min_amount  (int)                  — minimum reward amount; only valid for
//	                                     item / candy / mega_energy rewards
//	                                     (pokemon has no amount, stardust uses
//	                                     its dedicated option)
//	distance    (int)                  — alert radius in metres
//	clean       (bool)                  — auto-delete on expiry
//	template    (string, autocomplete) — DTS template name
//
// The text bot's /quest grammar uses prefix tokens (`stardust:1000`,
// `candy:pikachu`, `energy:charizard`) plus a bare pokemon name for
// reward_type=7. Items match against translated item names via
// matchItemName on Unrecognized args — so item picks emit the bare name,
// not an `item:` prefix that the bot has no matcher for. Mutual exclusion
// is enforced here because the text grammar can't distinguish "track only
// pokemon" from "track pokemon AND stardust" — the bot would silently
// pick the first matching branch.
//
// XL-candy is not surfaced: matching/quest.go and enrichment/quest.go
// handle reward types 2/3/4/7/12 only — XL candy has no reward type and
// no matcher entry, so a slash option would silently never fire.
func Quest(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
	o := flattenOptions(opts)

	rewardOpts := []string{"pokemon", "item", "stardust", "candy", "mega_energy"}
	var set []string
	for _, name := range rewardOpts {
		if hasNonZeroValue(o[name]) {
			set = append(set, name)
		}
	}
	if len(set) == 0 {
		return nil, &MapperError{Key: "error.slash.quest.no_reward"}
	}
	if len(set) > 1 {
		return nil, &MapperError{Key: "error.slash.quest.exactly_one_reward", Args: []any{strings.Join(set, ", ")}}
	}

	// min_amount only applies to amount-bearing reward types. Pokemon (7)
	// has no amount and stardust (3) uses its dedicated option (which
	// also stores its threshold in Reward, not Amount). Reject the
	// invalid combination here so the user sees the error in the
	// slash-response edit rather than as an opaque text-bot rejection.
	var minAmount int64
	if v, ok := o["min_amount"]; ok && v.IntValue() > 0 {
		switch set[0] {
		case "pokemon", "stardust":
			return nil, &MapperError{Key: "error.slash.quest.amount_not_applicable"}
		}
		minAmount = v.IntValue()
	}

	tokens := []string{}
	switch set[0] {
	case "pokemon":
		tokens = append(tokens, strings.ToLower(o["pokemon"].StringValue()))
	case "item":
		// Bare name, no prefix — matchItemName resolves against translated
		// item names from Unrecognized args. Lowercased so the matcher's
		// case-folded comparison sees what it expects.
		tokens = append(tokens, strings.ToLower(o["item"].StringValue()))
	case "stardust":
		tokens = append(tokens, fmt.Sprintf("stardust:%d", o["stardust"].IntValue()))
	case "candy":
		tokens = append(tokens, "candy:"+o["candy"].StringValue())
	case "mega_energy":
		tokens = append(tokens, "energy:"+o["mega_energy"].StringValue())
	}

	if minAmount > 0 {
		tokens = append(tokens, fmt.Sprintf("amount:%d", minAmount))
	}

	appendDistance(&tokens, o["distance"])
	if tok := emitFlag(o["clean"], "clean"); tok != "" {
		tokens = append(tokens, tok)
	}
	// `summary` is the text-bot's arg.summary keyword: a quest matched
	// while this flag is set is buffered for scheduled batch delivery
	// instead of firing an immediate alert. The slash option is a
	// single-choice "Yes" string (no second click to toggle), and
	// emitFlag returns the bare keyword for any non-empty value.
	if tok := emitFlag(o["summary"], "summary"); tok != "" {
		tokens = append(tokens, tok)
	}
	if v, ok := o["template"]; ok && v.StringValue() != "" {
		tokens = append(tokens, "template:"+v.StringValue())
	}
	return tokens, nil
}

func init() { registry["quest"] = Quest }
