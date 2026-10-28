package mappers

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
)

// Tappable maps /tappable options to text-command tokens.
//
// Options:
//
//	reward    (string, required choice) — candy, xl_candy, mega_energy,
//	                                      item, stardust, pokecoins, everything
//	pokemon   (string, autocomplete)    — the pokemon for candy / xl_candy /
//	                                      mega_energy; omitted = any pokemon
//	item      (string, autocomplete)    — the item for reward=item;
//	                                      omitted = any item (`all items`)
//	amount    (int)                     — minimum reward amount
//	distance, clean, template, location, areas — the shared tracker tail
//
// Tokens follow the !tappable grammar: `candy:<pokemon>` / bare `candy`,
// `xlcandy:<pokemon>` / `xlcandy`, `energy:<pokemon>` / `energy`, a bare
// item name (matchItemName resolves it) or `all items`, `stardust[:N]`,
// `pokecoins[:N]`, `everything`. Stardust and pokecoins keep their minimum
// in the reward column, so amount becomes `stardust:N` / `pokecoins:N`;
// every other kind gets `amount:N`.
func Tappable(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
	o := flattenOptions(opts)

	kind := getString(o["reward"])
	pokemon := getString(o["pokemon"])
	item := getString(o["item"])
	var amount int64
	if v, ok := o["amount"]; ok && v.IntValue() > 0 {
		amount = v.IntValue()
	}

	pokemonKinds := map[string]string{"candy": "candy", "xl_candy": "xlcandy", "mega_energy": "energy"}
	if _, ok := pokemonKinds[kind]; pokemon != "" && !ok {
		return nil, &MapperError{Key: "error.slash.tappable.pokemon_not_applicable"}
	}
	if item != "" && kind != "item" {
		return nil, &MapperError{Key: "error.slash.tappable.item_not_applicable"}
	}

	var tokens []string
	amountToken := amount > 0
	switch kind {
	case "candy", "xl_candy", "mega_energy":
		tok := pokemonKinds[kind]
		if pokemon != "" {
			tok += ":" + pokemon
		}
		tokens = append(tokens, tok)
	case "item":
		if item != "" {
			// Bare name: matchItemName resolves it from the unrecognized
			// args, case-folded.
			tokens = append(tokens, strings.ToLower(item))
		} else {
			tokens = append(tokens, "all items")
		}
	case "stardust", "pokecoins":
		tok := kind
		if amount > 0 {
			tok = fmt.Sprintf("%s:%d", kind, amount)
		}
		tokens = append(tokens, tok)
		amountToken = false
	case "everything":
		tokens = append(tokens, "everything")
	default:
		return nil, &MapperError{Key: "error.slash.tappable.no_reward"}
	}
	if amountToken {
		tokens = append(tokens, fmt.Sprintf("amount:%d", amount))
	}

	appendCommonTail(&tokens, o)
	return tokens, nil
}

func init() { registry["tappable"] = Tappable }
