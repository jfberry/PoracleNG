package commands

import (
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
	{Type: bot.ParamKeyword, Key: "arg.remove"},
	{Type: bot.ParamKeyword, Key: "arg.everything"},
	{Type: bot.ParamKeyword, Key: "arg.clean"},
	{Type: bot.ParamTypeName},
	{Type: bot.ParamPokemonName},
}

func (c *NestCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	// Extract @mention pings before parsing
	pings, args := extractPings(args)

	if usage := usageReply(ctx, args, "msg.nest.usage"); usage != nil {
		return []bot.Reply{*usage}
	}
	if help := helpArgReply(ctx, args, "msg.nest.usage"); help != nil {
		return []bot.Reply{*help}
	}

	parsed := ctx.ArgMatcher.Match(args, nestParams, ctx.Language)

	if warn := bot.ReportUnrecognized(parsed, tr); warn != nil {
		return []bot.Reply{*warn}
	}

	common, block := parseCommonTrackFields(ctx, parsed, "nest")
	if block != nil {
		return []bot.Reply{*block}
	}
	minSpawnAvg := 0
	if ms, ok := parsed.Singles["minspawn"]; ok {
		minSpawnAvg = ms
	}

	// Resolve pokemon list
	monsterList := c.resolveMonsters(ctx, parsed)

	if len(monsterList) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.no_pokemon")}}
	}

	if parsed.HasKeyword("arg.remove") {
		if len(parsed.RemoveUIDs) > 0 {
			tr := ctx.Tr()
			return removeByUIDs(ctx, ctx.Tracking.Nests, parsed.RemoveUIDs,
				store.NestGetUID,
				func(r *db.NestTrackingAPI) string { return ctx.RowText.NestRowText(tr, nestAPIToTracking(r)) },
			)
		}
		return c.removeNests(ctx, monsterList)
	}

	insert := make([]db.NestTrackingAPI, 0, len(monsterList))
	for _, mon := range monsterList {
		insert = append(insert, db.NestTrackingAPI{
			ID:          ctx.TargetID,
			ProfileNo:   ctx.ProfileNo,
			Ping:        pings,
			Template:    common.Template,
			Distance:    common.Distance,
			Clean:       db.IntBool(common.Clean),
			PokemonID:   mon.PokemonID,
			Form:        mon.Form,
			MinSpawnAvg: minSpawnAvg,
		})
	}

	tracked, err := ctx.Tracking.Nests.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("nest command: select existing: %s", err)
		return []bot.Reply{{React: "🙅"}}
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

func (c *NestCommand) resolveMonsters(ctx *bot.CommandContext, parsed *bot.ParsedArgs) []bot.ResolvedPokemon {
	if parsed.HasKeyword("arg.everything") {
		return []bot.ResolvedPokemon{{PokemonID: 0, Form: 0}}
	}

	monsters := parsed.Pokemon
	if formName, ok := parsed.Strings["form"]; ok {
		monsters = filterByForm(ctx, monsters, formName)
	}
	return filterByGenAndType(ctx, monsters, parsed)
}

func (c *NestCommand) removeNests(ctx *bot.CommandContext, monsterList []bot.ResolvedPokemon) []bot.Reply {
	tracked, err := ctx.Tracking.Nests.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("nest command: select for remove: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	removeIDs := make(map[int]bool)
	for _, mon := range monsterList {
		removeIDs[mon.PokemonID] = true
	}

	var uids []int64
	var removed []db.NestTrackingAPI
	for _, existing := range tracked {
		// PokemonID 0 means everything — remove all
		if removeIDs[0] || removeIDs[existing.PokemonID] {
			uids = append(uids, existing.UID)
			removed = append(removed, existing)
		}
	}
	if len(uids) == 0 {
		return []bot.Reply{{React: "👌"}}
	}
	if err := ctx.Tracking.Nests.DeleteByUIDs(ctx.TargetID, uids); err != nil {
		log.Errorf("nest command: delete: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}
	ctx.TriggerReload()
	tr := ctx.Tr()
	var descriptions []string
	for i := range removed {
		descriptions = append(descriptions, ctx.RowText.NestRowText(tr, nestAPIToTracking(&removed[i])))
	}
	return []bot.Reply{{React: "✅", Text: formatRemovedRows(tr, descriptions)}}
}
