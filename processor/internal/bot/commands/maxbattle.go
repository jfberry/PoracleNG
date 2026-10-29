package commands

import (
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// MaxbattleCommand implements !maxbattle — track max battles (Dynamax).
type MaxbattleCommand struct{}

func (c *MaxbattleCommand) Name() string      { return "cmd.maxbattle" }
func (c *MaxbattleCommand) Aliases() []string { return nil }

var maxbattleParams = []bot.ParamDef{
	{Type: bot.ParamRemoveUID},
	{Type: bot.ParamPrefixRange, Key: "arg.prefix.level"},
	{Type: bot.ParamPrefixSingle, Key: "arg.prefix.d"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.template"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.form"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.move"},
	{Type: bot.ParamPrefixSingle, Key: "arg.prefix.gen"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.location"},
	{Type: bot.ParamPrefixStringList, Key: "arg.prefix.area"},
	{Type: bot.ParamKeyword, Key: "arg.remove"},
	{Type: bot.ParamKeyword, Key: "arg.everything"},
	{Type: bot.ParamKeyword, Key: "arg.clean"},
	{Type: bot.ParamKeyword, Key: "arg.gmax"},
	{Type: bot.ParamTypeName},
	{Type: bot.ParamPokemonName},
}

func (c *MaxbattleCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	if reply := RouteToMuteFromType(ctx, "maxbattle", args); reply != nil {
		return reply
	}

	// Extract @mention pings before parsing
	pings, args := extractPings(args)

	if usage := usageReply(ctx, args, "msg.maxbattle.usage"); usage != nil {
		return []bot.Reply{*usage}
	}
	if help := helpArgReply(ctx, args, "msg.maxbattle.usage"); help != nil {
		return []bot.Reply{*help}
	}

	if ctx.Config.General.DisableMaxBattle {
		return []bot.Reply{{React: "\U0001f645", Text: "This alert type is disabled"}}
	}

	parsed := ctx.ArgMatcher.Match(args, maxbattleParams, ctx.Language)

	if parsed.HasKeyword("arg.remove") {
		return c.removeMaxbattles(ctx, parsed)
	}

	if warn := bot.ReportUnrecognized(parsed, tr); warn != nil {
		return []bot.Reply{*warn}
	}

	common, block := parseCommonTrackFields(ctx, parsed, "maxbattle")
	if block != nil {
		return []bot.Reply{*block}
	}

	override, overrideReply := parseOverride(ctx, parsed.Strings["location"], parsed.StringLists["area"], common.Distance)
	if overrideReply != nil {
		return []bot.Reply{*overrideReply}
	}

	gmax := 0
	if parsed.HasKeyword("arg.gmax") {
		gmax = 1
	}

	// Move filter — match by translated name
	move := bot.WildcardID
	if moveName, ok := parsed.Strings["move"]; ok {
		id, found := resolveMoveByName(ctx, moveName)
		if !found {
			return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.move.unknown", moveName)}}
		}
		move = id
	}

	var insert []db.MaxbattleTrackingAPI

	if len(parsed.Pokemon) > 0 {
		// Track specific pokemon
		monsterList, formReply := c.resolveMonsters(ctx, parsed)
		if formReply != nil {
			return []bot.Reply{*formReply}
		}
		for _, mon := range monsterList {
			insert = append(insert, db.MaxbattleTrackingAPI{
				ID:                    ctx.TargetID,
				ProfileNo:             ctx.ProfileNo,
				Ping:                  pings,
				Template:              common.Template,
				Distance:              common.Distance,
				Clean:                 common.Clean,
				PokemonID:             mon.PokemonID,
				Form:                  mon.Form,
				Level:                 90, // 90 = all levels for specific pokemon
				Move:                  move,
				Gmax:                  gmax,
				Evolution:             bot.WildcardID,
				StationID:             nil,
				OverrideLocationLabel: override.LocationLabel,
				OverrideAreas:         override.Areas,
			})
		}
	} else {
		// gen/type filters narrow a list of named pokemon; by-level rules
		// have none to narrow, so `everything gen1` / `fire level5` would
		// silently track every boss. Reject instead.
		if hasGenOrTypeFilter(parsed) {
			return []bot.Reply{{React: "🙅", Text: tr.T("msg.maxbattle.gen_type_needs_pokemon")}}
		}

		// Track by level
		levelSet := make(map[int]bool)

		if r, ok := parsed.Ranges["level"]; ok {
			if r.HasMax {
				for lvl := r.Min; lvl <= r.Max; lvl++ {
					levelSet[lvl] = true
				}
			} else {
				levelSet[r.Min] = true
			}
		}

		if parsed.HasKeyword("arg.everything") {
			levelSet[90] = true // 90 = all levels
		}

		if len(levelSet) == 0 {
			return []bot.Reply{{React: "🙅", Text: tr.T("msg.no_levels")}}
		}

		for lvl := range levelSet {
			insert = append(insert, db.MaxbattleTrackingAPI{
				ID:                    ctx.TargetID,
				ProfileNo:             ctx.ProfileNo,
				Ping:                  pings,
				Template:              common.Template,
				Distance:              common.Distance,
				Clean:                 common.Clean,
				PokemonID:             bot.WildcardID, // 9000 = by level
				Level:                 lvl,
				Move:                  move,
				Gmax:                  gmax,
				Evolution:             bot.WildcardID,
				StationID:             nil,
				OverrideLocationLabel: override.LocationLabel,
				OverrideAreas:         override.Areas,
			})
		}
	}

	if len(insert) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.no_levels")}}
	}

	// Diff against existing and apply
	tracked, err := ctx.Tracking.Maxbattles.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("maxbattle command: select existing: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	if r := overrideLimitReply(ctx, tracked, insert, store.MaxbattleGetUID, store.MaxbattleSetUID); r != nil {
		return r
	}
	diff, err := store.ApplyDiff(ctx.Tracking.Maxbattles, ctx.TargetID, tracked, insert,
		store.MaxbattleGetUID, store.MaxbattleSetUID)
	if err != nil {
		log.Errorf("maxbattle command: apply diff: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	message := buildTrackingMessage(tr, ctx, len(diff.AlreadyPresent), len(diff.Updates), len(diff.Inserts),
		func(i int) string {
			return ctx.RowText.MaxbattleRowText(tr, maxbattleAPIToTracking(&diff.AlreadyPresent[i]))
		},
		func(i int) string {
			return ctx.RowText.MaxbattleRowText(tr, maxbattleAPIToTracking(&diff.Updates[i]))
		},
		func(i int) string {
			return ctx.RowText.MaxbattleRowText(tr, maxbattleAPIToTracking(&diff.Inserts[i]))
		},
	)

	ctx.TriggerReload()

	message += trackingWarnings(ctx, common.Distance)

	if common.TemplateWarn != "" {
		message += "\n⚠️ " + common.TemplateWarn
	}

	react := "✅"
	if len(diff.Inserts) == 0 && len(diff.Updates) == 0 {
		react = "👌"
	}
	return []bot.Reply{{React: react, Text: message}}
}

func (c *MaxbattleCommand) resolveMonsters(ctx *bot.CommandContext, parsed *bot.ParsedArgs) ([]bot.ResolvedPokemon, *bot.Reply) {
	monsters, reply := applyFormFilter(ctx, parsed.Pokemon, parsed)
	if reply != nil {
		return nil, reply
	}
	return filterByGenAndType(ctx, monsters, parsed), nil
}

// maxbattleRemoveSpec: max battle rules are removed by pokemon or by
// level; gen: and type narrow the selection.
var maxbattleRemoveSpec = removeSpec{
	selectors: []bot.ParamDef{pokemonParam, levelParam},
	filters:   []bot.ParamDef{genParam, typeNameParam},
}

func (c *MaxbattleCommand) removeMaxbattles(ctx *bot.CommandContext, parsed *bot.ParsedArgs) []bot.Reply {
	if reply := checkRemoveArgs(ctx, parsed, maxbattleRemoveSpec, nil); reply != nil {
		return []bot.Reply{*reply}
	}
	tr := ctx.Tr()

	levelRange, hasLevel := parsed.Ranges["level"]
	selects := func(r *db.MaxbattleTrackingAPI) bool {
		for _, mon := range parsed.Pokemon {
			if r.PokemonID == mon.PokemonID {
				return true
			}
		}
		// Level rules are the by-level rows (wildcard pokemon); a
		// pokemon-specific rule is removed by naming the pokemon.
		return hasLevel && r.PokemonID == bot.WildcardID && rangeMatches(levelRange, r.Level)
	}
	var keep func(*db.MaxbattleTrackingAPI) bool
	if inGenType := genTypeKeep(ctx, parsed); inGenType != nil {
		keep = func(r *db.MaxbattleTrackingAPI) bool { return inGenType(r.PokemonID, r.Form) }
	}

	return ruleRemover[db.MaxbattleTrackingAPI]{
		store:  ctx.Tracking.Maxbattles,
		getUID: store.MaxbattleGetUID,
		describe: func(r *db.MaxbattleTrackingAPI) string {
			return ctx.RowText.MaxbattleRowText(tr, maxbattleAPIToTracking(r))
		},
		logName: "maxbattle",
	}.run(ctx, parsed, selects, keep)
}
