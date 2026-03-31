package commands

import (
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
)

// UntrackCommand implements !untrack — remove pokemon tracking rules.
type UntrackCommand struct{}

func (c *UntrackCommand) Name() string      { return "cmd.untrack" }
func (c *UntrackCommand) Aliases() []string { return nil }

var untrackParams = []bot.ParamDef{
	{Type: bot.ParamKeyword, Key: "arg.everything"},
	{Type: bot.ParamPrefixSingle, Key: "arg.prefix.gen"},
	{Type: bot.ParamTypeName},
	{Type: bot.ParamPokemonName},
}

func (c *UntrackCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	parsed := ctx.ArgMatcher.Match(args, untrackParams, ctx.Language)

	if warn := bot.ReportUnrecognized(parsed, tr); warn != nil {
		return []bot.Reply{*warn}
	}

	tracked, err := db.SelectMonstersByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("untrack command: select existing: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	// Build set of pokemon IDs to remove
	removeIDs := make(map[int]bool)

	if parsed.HasKeyword("arg.everything") {
		for _, existing := range tracked {
			removeIDs[existing.PokemonID] = true
		}
	} else {
		for _, mon := range parsed.Pokemon {
			removeIDs[mon.PokemonID] = true
		}
	}

	// Generation filter — if gen is specified without pokemon, remove all pokemon in that gen
	if gen, ok := parsed.Singles["gen"]; ok && ctx.GameData != nil {
		genInfo := ctx.GameData.Util.GenData[gen]
		if genInfo.Min > 0 && genInfo.Max > 0 {
			for _, existing := range tracked {
				if existing.PokemonID >= genInfo.Min && existing.PokemonID <= genInfo.Max {
					removeIDs[existing.PokemonID] = true
				}
			}
		}
	}

	// Type filter — remove all pokemon of the given type(s)
	if len(parsed.Types) > 0 && ctx.GameData != nil {
		typeSet := make(map[int]bool)
		for _, t := range parsed.Types {
			typeSet[t] = true
		}
		for _, existing := range tracked {
			m := ctx.GameData.GetMonster(existing.PokemonID, existing.Form)
			if m == nil {
				continue
			}
			for _, t := range m.Types {
				if typeSet[t] {
					removeIDs[existing.PokemonID] = true
					break
				}
			}
		}
	}

	if len(removeIDs) == 0 {
		return []bot.Reply{{React: "👌", Text: "No matching tracking rules found"}}
	}

	var uidsToDelete []int64
	var removed []db.MonsterTrackingAPI
	for _, existing := range tracked {
		if removeIDs[existing.PokemonID] {
			uidsToDelete = append(uidsToDelete, existing.UID)
			removed = append(removed, existing)
		}
	}

	if len(uidsToDelete) == 0 {
		return []bot.Reply{{React: "👌"}}
	}

	if err := db.DeleteByUIDs(ctx.DB, "monsters", ctx.TargetID, uidsToDelete); err != nil {
		log.Errorf("untrack command: delete: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	ctx.TriggerReload()

	var sb strings.Builder
	if len(removed) > 20 {
		fmt.Fprintf(&sb, "Removed %d tracking rules", len(removed))
	} else {
		for i := range removed {
			mt := monsterAPIToTracking(&removed[i])
			fmt.Fprintf(&sb, "Removed: %s\n", ctx.RowText.MonsterRowText(tr, mt))
		}
	}
	return []bot.Reply{{React: "✅", Text: sb.String()}}
}
