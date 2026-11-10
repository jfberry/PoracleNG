package commands

import (
	"sort"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// NestCommand implements !nest — track nesting pokemon.
type NestCommand struct{}

func (c *NestCommand) Name() string      { return "cmd.nest" }
func (c *NestCommand) Aliases() []string { return nil }

var nestParams = []bot.ParamDef{
	{Type: bot.ParamRemoveUID},
	{Type: bot.ParamPrefixSingle, Key: "arg.prefix.d"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.template"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.form"},
	{Type: bot.ParamPrefixSingle, Key: "arg.prefix.gen"},
	{Type: bot.ParamPrefixSingle, Key: "arg.prefix.minspawn"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.location"},
	{Type: bot.ParamPrefixStringList, Key: "arg.prefix.area"},
	{Type: bot.ParamKeyword, Key: "arg.remove"},
	{Type: bot.ParamKeyword, Key: "arg.everything"},
	{Type: bot.ParamKeyword, Key: "arg.clean"},
	{Type: bot.ParamTypeName},
	{Type: bot.ParamPokemonName},
}

func (c *NestCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	if reply := RouteToMuteFromType(ctx, "nest", args); reply != nil {
		return reply
	}

	// Extract @mention pings before parsing
	pings, args := extractPings(args)

	if usage := usageReply(ctx, args, "msg.nest.usage"); usage != nil {
		return []bot.Reply{*usage}
	}
	if help := helpArgReply(ctx, args, "msg.nest.usage"); help != nil {
		return []bot.Reply{*help}
	}

	if ctx.Config.General.DisableNest {
		return []bot.Reply{{React: "\U0001f645", Text: "This alert type is disabled"}}
	}

	parsed := ctx.ArgMatcher.Match(args, nestParams, ctx.Language)

	if warn := bot.ReportUnrecognized(parsed, tr); warn != nil {
		return []bot.Reply{*warn}
	}

	if parsed.HasKeyword("arg.remove") {
		return c.removeNests(ctx, parsed)
	}

	common, block := parseCommonTrackFields(ctx, parsed, "nest")
	if block != nil {
		return []bot.Reply{*block}
	}

	override, overrideReply := parseOverride(ctx, parsed.Strings["location"], parsed.StringLists["area"], common.Distance)
	if overrideReply != nil {
		return []bot.Reply{*overrideReply}
	}

	minSpawnAvg := 0
	if ms, ok := parsed.Singles["minspawn"]; ok {
		minSpawnAvg = ms
	}

	// Resolve pokemon list
	monsterList, formReply := c.resolveMonsters(ctx, parsed)
	if formReply != nil {
		return []bot.Reply{*formReply}
	}

	if len(monsterList) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.no_pokemon")}}
	}

	insert := make([]db.NestTrackingAPI, 0, len(monsterList))
	for _, mon := range monsterList {
		insert = append(insert, db.NestTrackingAPI{
			ID:                    ctx.TargetID,
			ProfileNo:             ctx.ProfileNo,
			Ping:                  pings,
			Template:              common.Template,
			Distance:              common.Distance,
			Clean:                 common.Clean,
			PokemonID:             mon.PokemonID,
			Form:                  mon.Form,
			MinSpawnAvg:           minSpawnAvg,
			OverrideLocationLabel: override.LocationLabel,
			OverrideAreas:         override.Areas,
		})
	}

	tracked, err := ctx.Tracking.Nests.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("nest command: select existing: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	if r := overrideLimitReply(ctx, tracked, insert, store.NestGetUID, store.NestSetUID); r != nil {
		return r
	}
	diff, err := store.ApplyDiff(ctx.Tracking.Nests, ctx.TargetID, tracked, insert,
		store.NestGetUID, store.NestSetUID)
	if err != nil {
		log.Errorf("nest command: apply diff: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	message := buildTrackingMessage(tr, ctx, len(diff.AlreadyPresent), len(diff.Updates), len(diff.Inserts),
		func(i int) string { return ctx.RowText.NestRowText(tr, nestAPIToTracking(&diff.AlreadyPresent[i])) },
		func(i int) string { return ctx.RowText.NestRowText(tr, nestAPIToTracking(&diff.Updates[i])) },
		func(i int) string { return ctx.RowText.NestRowText(tr, nestAPIToTracking(&diff.Inserts[i])) },
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

func (c *NestCommand) resolveMonsters(ctx *bot.CommandContext, parsed *bot.ParsedArgs) ([]bot.ResolvedPokemon, *bot.Reply) {
	if parsed.HasKeyword("arg.everything") {
		if !hasGenOrTypeFilter(parsed) || ctx.GameData == nil {
			return []bot.ResolvedPokemon{{PokemonID: 0, Form: 0}}, nil
		}
		// `everything gen1` / `everything fire`: expand to every species and
		// narrow, as !track does, instead of silently ignoring the filter.
		// Sorted so rows are inserted (and listed) in dex order.
		monsters := filterByGenAndType(ctx, allBaseForms(ctx), parsed)
		sort.Slice(monsters, func(i, j int) bool { return monsters[i].PokemonID < monsters[j].PokemonID })
		return monsters, nil
	}

	monsters, reply := applyFormFilter(ctx, parsed.Pokemon, parsed)
	if reply != nil {
		return nil, reply
	}
	return filterByGenAndType(ctx, monsters, parsed), nil
}

// nestRemoveSpec: nest rules are removed by pokemon; gen: and type narrow
// the selection.
var nestRemoveSpec = removeSpec{
	selectors: []bot.ParamDef{pokemonParam},
	filters:   []bot.ParamDef{genParam, typeNameParam},
}

func (c *NestCommand) removeNests(ctx *bot.CommandContext, parsed *bot.ParsedArgs) []bot.Reply {
	if reply := checkRemoveArgs(ctx, parsed, nestRemoveSpec, nil); reply != nil {
		return []bot.Reply{*reply}
	}
	tr := ctx.Tr()

	selects := func(r *db.NestTrackingAPI) bool {
		for _, mon := range parsed.Pokemon {
			if r.PokemonID == mon.PokemonID {
				return true
			}
		}
		return false
	}
	var keep func(*db.NestTrackingAPI) bool
	if inGenType := genTypeKeep(ctx, parsed); inGenType != nil {
		keep = func(r *db.NestTrackingAPI) bool { return inGenType(r.PokemonID, r.Form) }
	}

	return ruleRemover[db.NestTrackingAPI]{
		store:    ctx.Tracking.Nests,
		getUID:   store.NestGetUID,
		describe: func(r *db.NestTrackingAPI) string { return ctx.RowText.NestRowText(tr, nestAPIToTracking(r)) },
		logName:  "nest",
	}.run(ctx, parsed, selects, keep)
}
