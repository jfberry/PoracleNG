package commands

import (
	"github.com/guregu/null/v6"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// EggCommand implements !egg — track raid eggs by level.
type EggCommand struct{}

func (c *EggCommand) Name() string      { return "cmd.egg" }
func (c *EggCommand) Aliases() []string { return nil }

// eggParams declares the parameter types !egg accepts.
var eggParams = []bot.ParamDef{
	{Type: bot.ParamRemoveUID},
	{Type: bot.ParamPrefixRange, Key: "arg.prefix.level"},
	{Type: bot.ParamPrefixSingle, Key: "arg.prefix.d"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.template"},
	{Type: bot.ParamKeyword, Key: "arg.remove"},
	{Type: bot.ParamKeyword, Key: "arg.everything"},
	{Type: bot.ParamKeyword, Key: "arg.clean"},
	{Type: bot.ParamKeyword, Key: "arg.edit"},
	{Type: bot.ParamKeyword, Key: "arg.ex"},
	{Type: bot.ParamKeyword, Key: "arg.rsvp"},
	{Type: bot.ParamKeyword, Key: "arg.no_rsvp"},
	{Type: bot.ParamKeyword, Key: "arg.rsvp_only"},
	{Type: bot.ParamTeam},
	{Type: bot.ParamRaidLevelName},
}

func (c *EggCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	// Extract @mention pings before parsing
	pings, args := extractPings(args)

	if usage := usageReply(ctx, args, "msg.egg.usage"); usage != nil {
		return []bot.Reply{*usage}
	}
	if help := helpArgReply(ctx, args, "msg.egg.usage"); help != nil {
		return []bot.Reply{*help}
	}

	if ctx.Config.General.DisableRaid {
		return []bot.Reply{{React: "\U0001f645", Text: "This alert type is disabled"}}
	}

	parsed := ctx.ArgMatcher.Match(args, eggParams, ctx.Language)

	// Collect levels from multiple sources
	levelSet := make(map[int]bool)

	// From level<N> or level<N>-<M> prefix
	if r, ok := parsed.Ranges["level"]; ok {
		if r.HasMax {
			for lvl := r.Min; lvl <= r.Max; lvl++ {
				levelSet[lvl] = true
			}
		} else {
			levelSet[r.Min] = true
		}
	}

	// From raid level names (legendary, mega, shadow, etc.)
	for _, lvl := range parsed.RaidLevels {
		levelSet[lvl] = true
	}

	// "everything" → all levels from game data
	if parsed.HasKeyword("arg.everything") {
		if ctx.GameData != nil && ctx.GameData.Util != nil {
			for lvl := range ctx.GameData.Util.RaidLevels {
				levelSet[lvl] = true
			}
		}
	}

	if len(levelSet) == 0 {
		if warn := bot.ReportUnrecognized(parsed, tr); warn != nil {
			return []bot.Reply{*warn}
		}
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.no_egg_levels")}}
	}

	common, block := parseCommonTrackFields(ctx, parsed, "egg")
	if block != nil {
		return []bot.Reply{*block}
	}
	exclusive := parsed.HasKeyword("arg.ex")
	team := parsed.Team

	rsvpChanges := 0
	if parsed.HasKeyword("arg.rsvp") {
		rsvpChanges = 1
	}
	if parsed.HasKeyword("arg.rsvp_only") {
		rsvpChanges = 2
	}
	if parsed.HasKeyword("arg.no_rsvp") {
		rsvpChanges = 0
	}

	// Check for unrecognized args
	if warn := bot.ReportUnrecognized(parsed, tr); warn != nil {
		return []bot.Reply{*warn}
	}

	// Handle remove
	if parsed.HasKeyword("arg.remove") {
		if len(parsed.RemoveUIDs) > 0 {
			tr := ctx.Tr()
			return removeByUIDs(ctx, ctx.Tracking.Eggs, parsed.RemoveUIDs,
				store.EggGetUID,
				func(r *db.EggTrackingAPI) string { return ctx.RowText.EggRowText(tr, eggAPIToTracking(r)) },
			)
		}
		return c.removeEggs(ctx, levelSet)
	}

	// Build insert structs
	levels := make([]int, 0, len(levelSet))
	for lvl := range levelSet {
		levels = append(levels, lvl)
	}

	insert := make([]db.EggTrackingAPI, 0, len(levels))
	for _, lvl := range levels {
		insert = append(insert, db.EggTrackingAPI{
			ID:          ctx.TargetID,
			ProfileNo:   ctx.ProfileNo,
			Ping:        pings,
			Template:    common.Template,
			Distance:    common.Distance,
			Team:        team,
			Clean:       common.Clean,
			Exclusive:   db.IntBool(exclusive),
			GymID:       null.String{},
			RSVPChanges: rsvpChanges,
			Level:       lvl,
		})
	}

	// Diff against existing and apply
	tracked, err := ctx.Tracking.Eggs.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("egg command: select existing: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	diff, err := store.ApplyDiff(ctx.Tracking.Eggs, ctx.TargetID, tracked, insert,
		store.EggGetUID, store.EggSetUID)
	if err != nil {
		log.Errorf("egg command: apply diff: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	// Build response message
	message := buildTrackingMessage(tr, ctx, len(diff.AlreadyPresent), len(diff.Updates), len(diff.Inserts),
		func(i int) string { return ctx.RowText.EggRowText(tr, eggAPIToTracking(&diff.AlreadyPresent[i])) },
		func(i int) string { return ctx.RowText.EggRowText(tr, eggAPIToTracking(&diff.Updates[i])) },
		func(i int) string { return ctx.RowText.EggRowText(tr, eggAPIToTracking(&diff.Inserts[i])) },
	)

	ctx.TriggerReload()

	message += trackingWarnings(ctx, common.Distance)

	if common.TemplateWarn != "" {
		message += "\n⚠️ " + common.TemplateWarn
	}

	if len(diff.Inserts) == 0 && len(diff.Updates) == 0 {
		return []bot.Reply{{React: "👌", Text: message}}
	}
	return []bot.Reply{{React: "✅", Text: message}}
}

func (c *EggCommand) removeEggs(ctx *bot.CommandContext, levelSet map[int]bool) []bot.Reply {
	tr := ctx.Tr()
	tracked, err := ctx.Tracking.Eggs.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("egg command: select for remove: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	var uidsToDelete []int64
	var removed []db.EggTrackingAPI
	for _, existing := range tracked {
		if levelSet[existing.Level] {
			uidsToDelete = append(uidsToDelete, existing.UID)
			removed = append(removed, existing)
		}
	}

	if len(uidsToDelete) == 0 {
		return []bot.Reply{{React: "👌"}}
	}

	if err := ctx.Tracking.Eggs.DeleteByUIDs(ctx.TargetID, uidsToDelete); err != nil {
		log.Errorf("egg command: delete: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	ctx.TriggerReload()

	var descriptions []string
	for i := range removed {
		descriptions = append(descriptions, ctx.RowText.EggRowText(tr, eggAPIToTracking(&removed[i])))
	}
	return []bot.Reply{{React: "✅", Text: formatRemovedRows(tr, descriptions)}}
}

