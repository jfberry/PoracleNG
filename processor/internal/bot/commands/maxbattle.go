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
	{Type: bot.ParamKeyword, Key: "arg.remove"},
	{Type: bot.ParamKeyword, Key: "arg.everything"},
	{Type: bot.ParamKeyword, Key: "arg.clean"},
	{Type: bot.ParamKeyword, Key: "arg.gmax"},
	{Type: bot.ParamTypeName},
	{Type: bot.ParamPokemonName},
}

func (c *MaxbattleCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

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

	if warn := bot.ReportUnrecognized(parsed, tr); warn != nil {
		return []bot.Reply{*warn}
	}

	common, block := parseCommonTrackFields(ctx, parsed, "maxbattle")
	if block != nil {
		return []bot.Reply{*block}
	}

	gmax := 0
	if parsed.HasKeyword("arg.gmax") {
		gmax = 1
	}

	// Move filter — match by translated name
	move := bot.WildcardID
	if moveName, ok := parsed.Strings["move"]; ok {
		move = resolveMoveByName(ctx, moveName)
	}

	// Handle remove
	if parsed.HasKeyword("arg.remove") {
		return c.removeMaxbattles(ctx, parsed)
	}

	var insert []db.MaxbattleTrackingAPI

	if len(parsed.Pokemon) > 0 {
		// Track specific pokemon
		monsterList := c.resolveMonsters(ctx, parsed)
		for _, mon := range monsterList {
			insert = append(insert, db.MaxbattleTrackingAPI{
				ID:        ctx.TargetID,
				ProfileNo: ctx.ProfileNo,
				Ping:      pings,
				Template:  common.Template,
				Distance:  common.Distance,
				Clean:     db.IntBool(common.Clean),
				PokemonID: mon.PokemonID,
				Form:      mon.Form,
				Level:     90, // 90 = all levels for specific pokemon
				Move:      move,
				Gmax:      gmax,
				Evolution: bot.WildcardID,
				StationID: nil,
			})
		}
	} else {
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
				ID:        ctx.TargetID,
				ProfileNo: ctx.ProfileNo,
				Ping:      pings,
				Template:  common.Template,
				Distance:  common.Distance,
				Clean:     db.IntBool(common.Clean),
				PokemonID: bot.WildcardID, // 9000 = by level
				Level:     lvl,
				Move:      move,
				Gmax:      gmax,
				Evolution: bot.WildcardID,
				StationID: nil,
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

func (c *MaxbattleCommand) resolveMonsters(ctx *bot.CommandContext, parsed *bot.ParsedArgs) []bot.ResolvedPokemon {
	monsters := parsed.Pokemon
	if formName, ok := parsed.Strings["form"]; ok {
		monsters = filterByForm(ctx, monsters, formName)
	}
	return filterByGenAndType(ctx, monsters, parsed)
}

func (c *MaxbattleCommand) removeMaxbattles(ctx *bot.CommandContext, parsed *bot.ParsedArgs) []bot.Reply {
	if len(parsed.RemoveUIDs) > 0 {
		tr := ctx.Tr()
		return removeByUIDs(ctx, ctx.Tracking.Maxbattles, parsed.RemoveUIDs,
			store.MaxbattleGetUID,
			func(r *db.MaxbattleTrackingAPI) string { return ctx.RowText.MaxbattleRowText(tr, maxbattleAPIToTracking(r)) },
		)
	}

	tracked, err := ctx.Tracking.Maxbattles.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("maxbattle command: select for remove: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	var uids []int64
	var removed []db.MaxbattleTrackingAPI
	if parsed.HasKeyword("arg.everything") {
		for _, existing := range tracked {
			uids = append(uids, existing.UID)
			removed = append(removed, existing)
		}
	} else {
		removeIDs := make(map[int]bool)
		for _, mon := range parsed.Pokemon {
			removeIDs[mon.PokemonID] = true
		}
		for _, existing := range tracked {
			if removeIDs[existing.PokemonID] {
				uids = append(uids, existing.UID)
				removed = append(removed, existing)
			}
		}
	}

	if len(uids) == 0 {
		return []bot.Reply{{React: "👌"}}
	}
	if err := ctx.Tracking.Maxbattles.DeleteByUIDs(ctx.TargetID, uids); err != nil {
		log.Errorf("maxbattle command: delete: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}
	ctx.TriggerReload()
	tr := ctx.Tr()
	var descriptions []string
	for i := range removed {
		descriptions = append(descriptions, ctx.RowText.MaxbattleRowText(tr, maxbattleAPIToTracking(&removed[i])))
	}
	return []bot.Reply{{React: "✅", Text: formatRemovedRows(tr, descriptions)}}
}
