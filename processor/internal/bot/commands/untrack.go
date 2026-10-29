package commands

import (
	"slices"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// UntrackCommand implements !untrack — remove pokemon tracking rules.
// When the first argument is a recognised tracking type (raid, egg, quest,
// invasion, incident, lure, nest, gym, fort, maxbattle) the command is
// rerouted to that type's command with "remove" prepended, matching what
// the slash dispatcher does for /untrack <subtype>.
type UntrackCommand struct{}

func (c *UntrackCommand) Name() string      { return "cmd.untrack" }
func (c *UntrackCommand) Aliases() []string { return nil }

// validUntrackTypes is the set of canonical English tracking-type short names
// that !untrack <type> can reroute to. Pokemon is excluded because it IS the
// default path handled by this command's own logic.
//
// This mirrors the non-pokemon entries in the subs slice inside
// discordbot/slash/definitions.go's untrackOptions(). When adding a type
// here, add it there too (and vice versa).
var validUntrackTypes = map[string]bool{
	"raid":      true,
	"egg":       true,
	"quest":     true,
	"invasion":  true,
	"incident":  true, // text-only: slash uses /incident directly, not /untrack incident
	"lure":      true,
	"nest":      true,
	"gym":       true,
	"fort":      true,
	"maxbattle": true,
}

// validUntrackType reports whether s is a recognised non-pokemon tracking type.
func validUntrackType(s string) bool {
	return validUntrackTypes[s]
}

// untrackRemoveSpec: pokemon rules are removed by species, gen: or type
// (which add to each other); a costume narrows the selection.
var untrackRemoveSpec = removeSpec{
	selectors: []bot.ParamDef{pokemonParam, genParam, typeNameParam},
	filters:   []bot.ParamDef{costumeParam},
}

var untrackParams = []bot.ParamDef{
	{Type: bot.ParamRemoveUID},
	{Type: bot.ParamKeyword, Key: "arg.everything"},
	{Type: bot.ParamPrefixSingle, Key: "arg.prefix.gen"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.costume"},
	{Type: bot.ParamTypeName},
	{Type: bot.ParamPokemonName},
}

func (c *UntrackCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	// Reroute !untrack <type> [...] to the per-type command with "remove"
	// prepended, mirroring the slash dispatcher's /untrack <subtype> handling.
	// This must be first so the pokemon-untrack logic never sees a type token.
	if len(args) >= 1 && validUntrackType(args[0]) {
		typeName := args[0]
		cmdKey := "cmd." + typeName
		if ctx.Registry == nil {
			log.Errorf("untrack command: registry is nil (cannot reroute to %s)", cmdKey)
			return []bot.Reply{{React: "🙅"}}
		}
		target := ctx.Registry.Lookup(cmdKey)
		if target == nil {
			log.Errorf("untrack command: %s not found in registry", cmdKey)
			return []bot.Reply{{React: "🙅"}}
		}
		return target.Run(ctx, append([]string{"remove"}, args[1:]...))
	}

	tr := ctx.Tr()
	parsed := ctx.ArgMatcher.Match(args, untrackParams, ctx.Language)

	if reply := checkRemoveArgs(ctx, parsed, untrackRemoveSpec, nil); reply != nil {
		return []bot.Reply{*reply}
	}

	// Costume filter: when present, narrows removal to rules with this
	// exact costume ID, in addition to whatever pokemon/gen/type/everything
	// selection matched below. Unlike form (which isn't accepted here),
	// costume participates directly in the match so
	// `!untrack pikachu costume:1` removes only the costume-1 rule.
	var keep func(*db.MonsterTrackingAPI) bool
	if costumeArg, ok := parsed.Strings["costume"]; ok {
		id, resolved := ctx.ArgMatcher.ResolveCostume(costumeArg, ctx.Language)
		if !resolved {
			return []bot.Reply{{
				React: "🙅",
				Text: tr.Tf("msg.costume_not_found",
					ctx.EscapeForCode(costumeArg),
					bot.CommandPrefix(ctx)),
			}}
		}
		keep = func(r *db.MonsterTrackingAPI) bool { return r.Costume == id }
	}

	remover := ruleRemover[db.MonsterTrackingAPI]{
		store:    ctx.Tracking.Monsters,
		getUID:   store.MonsterGetUID,
		describe: func(r *db.MonsterTrackingAPI) string { return ctx.RowText.MonsterRowText(tr, monsterAPIToTracking(r)) },
		logName:  "untrack",
	}
	if len(parsed.RemoveUIDs) > 0 || parsed.HasKeyword("arg.everything") {
		return remover.run(ctx, parsed, nil, keep)
	}

	tracked, err := ctx.Tracking.Monsters.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("untrack command: select existing: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	// Species to remove: those named, plus every tracked species in the
	// gen: and every tracked species of a type given. These add to each
	// other (`!untrack pikachu gen1` removes pikachu and all of gen 1).
	removeIDs := make(map[int]bool)
	for _, mon := range parsed.Pokemon {
		removeIDs[mon.PokemonID] = true
	}
	if gen, ok := parsed.Singles["gen"]; ok && ctx.GameData != nil && ctx.GameData.Util != nil {
		genInfo := ctx.GameData.Util.GenData[gen]
		if genInfo.Min > 0 && genInfo.Max > 0 {
			for _, existing := range tracked {
				if existing.PokemonID >= genInfo.Min && existing.PokemonID <= genInfo.Max {
					removeIDs[existing.PokemonID] = true
				}
			}
		}
	}
	if len(parsed.Types) > 0 && ctx.GameData != nil {
		for _, existing := range tracked {
			m := ctx.GameData.GetMonster(existing.PokemonID, existing.Form)
			if m == nil {
				continue
			}
			for _, t := range m.Types {
				if slices.Contains(parsed.Types, t) {
					removeIDs[existing.PokemonID] = true
					break
				}
			}
		}
	}

	return remover.run(ctx, parsed, func(r *db.MonsterTrackingAPI) bool { return removeIDs[r.PokemonID] }, keep)
}
