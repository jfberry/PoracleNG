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

// RaidCommand implements !raid — track raid bosses or raid levels.
type RaidCommand struct{}

func (c *RaidCommand) Name() string      { return "cmd.raid" }
func (c *RaidCommand) Aliases() []string { return nil }

var raidParams = []bot.ParamDef{
	{Type: bot.ParamRemoveUID},
	{Type: bot.ParamPrefixRange, Key: "arg.prefix.level"},
	{Type: bot.ParamPrefixSingle, Key: "arg.prefix.d"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.template"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.move"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.form"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.costume"},
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
	// No gen/type params: !raid has no gen or type filter, so accepting
	// them would silently track more than asked. (PoracleJS expanded a
	// type into every species of it — not ported yet.)
	{Type: bot.ParamPokemonName},
}

func (c *RaidCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	// Per-type mute/unmute alias: `!raid mute id:12` ≡ `!mute raid id:12`.
	if reply := RouteToMuteFromType(ctx, "raid", args); reply != nil {
		return reply
	}

	// Extract @mention pings before parsing
	pings, args := extractPings(args)

	if usage := usageReply(ctx, args, "msg.raid.usage"); usage != nil {
		return []bot.Reply{*usage}
	}
	if help := helpArgReply(ctx, args, "msg.raid.usage"); help != nil {
		return []bot.Reply{*help}
	}

	if ctx.Config.General.DisableRaid {
		return []bot.Reply{{React: "\U0001f645", Text: "This alert type is disabled"}}
	}

	parsed := ctx.ArgMatcher.Match(args, raidParams, ctx.Language)

	if parsed.HasKeyword("arg.remove") {
		return c.removeRaids(ctx, parsed)
	}

	if warn := bot.ReportUnrecognized(parsed, tr); warn != nil {
		return []bot.Reply{*warn}
	}

	common, block := parseCommonTrackFields(ctx, parsed, "raid")
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

	// Move filter — match by translated name
	move := bot.WildcardID
	if moveName, ok := parsed.Strings["move"]; ok {
		id, found := resolveMoveByName(ctx, moveName)
		if !found {
			return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.move.unknown", moveName)}}
		}
		move = id
	}

	// Costume filter — 9000 = any (default), 0 = no costume, N = specific.
	costume := 9000
	if costumeArg, ok := parsed.Strings["costume"]; ok {
		id, resolved := ctx.ArgMatcher.ResolveCostume(costumeArg, ctx.Language)
		if !resolved {
			return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.costume_not_found", ctx.EscapeForCode(costumeArg), bot.CommandPrefix(ctx))}}
		}
		costume = id
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

	// Build insert list — either by pokemon name or by level
	var insert []db.RaidTrackingAPI

	if len(parsed.Pokemon) > 0 {
		monsters, formReply := applyFormFilter(ctx, parsed.Pokemon, parsed)
		if formReply != nil {
			return []bot.Reply{*formReply}
		}
		// Track specific pokemon
		for _, mon := range monsters {
			insert = append(insert, db.RaidTrackingAPI{
				ID:                    ctx.TargetID,
				ProfileNo:             ctx.ProfileNo,
				Ping:                  pings,
				PokemonID:             mon.PokemonID,
				Form:                  mon.Form,
				Costume:               costume,
				Level:                 bot.WildcardID,
				Team:                  team,
				Exclusive:             db.IntBool(exclusive),
				Move:                  move,
				Evolution:             bot.WildcardID,
				GymID:                 gymID,
				Distance:              common.Distance,
				Template:              common.Template,
				Clean:                 common.Clean,
				RSVPChanges:           rsvpChanges,
				OverrideLocationLabel: override.LocationLabel,
				OverrideAreas:         override.Areas,
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

		for _, lvl := range parsed.RaidLevels {
			levelSet[lvl] = true
		}

		if parsed.HasKeyword("arg.everything") {
			if ctx.GameData != nil && ctx.GameData.Util != nil {
				for lvl := range ctx.GameData.Util.RaidLevels {
					levelSet[lvl] = true
				}
			}
		}

		if len(levelSet) == 0 {
			return []bot.Reply{{React: "🙅", Text: tr.T("msg.no_raid_target")}}
		}

		for lvl := range levelSet {
			insert = append(insert, db.RaidTrackingAPI{
				ID:                    ctx.TargetID,
				ProfileNo:             ctx.ProfileNo,
				Ping:                  pings,
				PokemonID:             bot.WildcardID,
				Costume:               costume,
				Level:                 lvl,
				Team:                  team,
				Exclusive:             db.IntBool(exclusive),
				Move:                  move,
				Evolution:             bot.WildcardID,
				GymID:                 gymID,
				Distance:              common.Distance,
				Template:              common.Template,
				Clean:                 common.Clean,
				RSVPChanges:           rsvpChanges,
				OverrideLocationLabel: override.LocationLabel,
				OverrideAreas:         override.Areas,
			})
		}
	}

	// Diff against existing and apply
	tracked, err := ctx.Tracking.Raids.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("raid command: select existing: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	if r := overrideLimitReply(ctx, tracked, insert, store.RaidGetUID, store.RaidSetUID); r != nil {
		return r
	}
	diff, err := store.ApplyDiff(ctx.Tracking.Raids, ctx.TargetID, tracked, insert,
		store.RaidGetUID, store.RaidSetUID)
	if err != nil {
		log.Errorf("raid command: apply diff: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	message := buildTrackingMessage(tr, ctx, len(diff.AlreadyPresent), len(diff.Updates), len(diff.Inserts),
		func(i int) string { return ctx.RowText.RaidRowText(tr, raidAPIToTracking(&diff.AlreadyPresent[i])) },
		func(i int) string { return ctx.RowText.RaidRowText(tr, raidAPIToTracking(&diff.Updates[i])) },
		func(i int) string { return ctx.RowText.RaidRowText(tr, raidAPIToTracking(&diff.Inserts[i])) },
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

// raidRemoveSpec: raid rules are removed by boss or level; a costume
// narrows the selection.
var raidRemoveSpec = removeSpec{
	selectors: []bot.ParamDef{pokemonParam, levelParam, raidLevelParam},
	filters:   []bot.ParamDef{costumeParam},
}

func (c *RaidCommand) removeRaids(ctx *bot.CommandContext, parsed *bot.ParsedArgs) []bot.Reply {
	if reply := checkRemoveArgs(ctx, parsed, raidRemoveSpec, nil); reply != nil {
		return []bot.Reply{*reply}
	}
	tr := ctx.Tr()

	var keep func(*db.RaidTrackingAPI) bool
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
		keep = func(r *db.RaidTrackingAPI) bool { return r.Costume == id }
	}

	levelRange, hasLevel := parsed.Ranges["level"]
	selects := func(r *db.RaidTrackingAPI) bool {
		for _, mon := range parsed.Pokemon {
			if r.PokemonID == mon.PokemonID {
				return true
			}
		}
		return slices.Contains(parsed.RaidLevels, r.Level) ||
			(hasLevel && rangeMatches(levelRange, r.Level))
	}

	return ruleRemover[db.RaidTrackingAPI]{
		store:    ctx.Tracking.Raids,
		getUID:   store.RaidGetUID,
		describe: func(r *db.RaidTrackingAPI) string { return ctx.RowText.RaidRowText(tr, raidAPIToTracking(r)) },
		logName:  "raid",
	}.run(ctx, parsed, selects, keep)
}
