package commands

import (
	"slices"
	"strings"

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
	{Type: bot.ParamPrefixString, Key: "arg.prefix.gym"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.location"},
	{Type: bot.ParamPrefixStringList, Key: "arg.prefix.area"},
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

	if reply := RouteToMuteFromType(ctx, "egg", args); reply != nil {
		return reply
	}

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

	if parsed.HasKeyword("arg.remove") {
		return c.removeEggs(ctx, parsed)
	}

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

	override, overrideReply := parseOverride(ctx, parsed.Strings["location"], parsed.StringLists["area"], common.Distance)
	if overrideReply != nil {
		return []bot.Reply{*overrideReply}
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

	// Optional `gym:<id-or-name>` argument pins the rule to a specific
	// gym. Multi-word names need outer quotes (e.g. `"gym:Town Hall"`)
	// so the bot parser keeps it as one token.
	gymID := null.String{}
	if raw := strings.TrimSpace(parsed.Strings["gym"]); raw != "" {
		id, abort := resolveGymRef(ctx, raw)
		if abort != nil {
			return []bot.Reply{*abort}
		}
		gymID = null.StringFrom(id)
	}

	// Build insert structs
	levels := make([]int, 0, len(levelSet))
	for lvl := range levelSet {
		levels = append(levels, lvl)
	}

	insert := make([]db.EggTrackingAPI, 0, len(levels))
	for _, lvl := range levels {
		insert = append(insert, db.EggTrackingAPI{
			ID:                    ctx.TargetID,
			ProfileNo:             ctx.ProfileNo,
			Ping:                  pings,
			Template:              common.Template,
			Distance:              common.Distance,
			Team:                  team,
			Clean:                 common.Clean,
			Exclusive:             db.IntBool(exclusive),
			GymID:                 gymID,
			RSVPChanges:           rsvpChanges,
			Level:                 lvl,
			OverrideLocationLabel: override.LocationLabel,
			OverrideAreas:         override.Areas,
		})
	}

	// Diff against existing and apply
	tracked, err := ctx.Tracking.Eggs.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("egg command: select existing: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	if r := overrideLimitReply(ctx, tracked, insert, store.EggGetUID, store.EggSetUID); r != nil {
		return r
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

// eggRemoveSpec: egg rules are removed by level.
var eggRemoveSpec = removeSpec{
	selectors: []bot.ParamDef{levelParam, raidLevelParam},
}

func (c *EggCommand) removeEggs(ctx *bot.CommandContext, parsed *bot.ParsedArgs) []bot.Reply {
	if reply := checkRemoveArgs(ctx, parsed, eggRemoveSpec, nil); reply != nil {
		return []bot.Reply{*reply}
	}
	tr := ctx.Tr()
	levelRange, hasLevel := parsed.Ranges["level"]
	return ruleRemover[db.EggTrackingAPI]{
		store:    ctx.Tracking.Eggs,
		getUID:   store.EggGetUID,
		describe: func(r *db.EggTrackingAPI) string { return ctx.RowText.EggRowText(tr, eggAPIToTracking(r)) },
		logName:  "egg",
	}.run(ctx, parsed, func(r *db.EggTrackingAPI) bool {
		return slices.Contains(parsed.RaidLevels, r.Level) || (hasLevel && rangeMatches(levelRange, r.Level))
	}, nil)
}
