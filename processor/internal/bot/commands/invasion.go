package commands

import (
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// InvasionCommand implements !invasion / !incident — track Team Rocket invasions.
type InvasionCommand struct{}

func (c *InvasionCommand) Name() string      { return "cmd.invasion" }
func (c *InvasionCommand) Aliases() []string { return []string{"cmd.incident"} }

var invasionParams = []bot.ParamDef{
	{Type: bot.ParamRemoveUID},
	{Type: bot.ParamPrefixSingle, Key: "arg.prefix.d"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.template"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.location"},
	{Type: bot.ParamPrefixStringList, Key: "arg.prefix.area"},
	{Type: bot.ParamKeyword, Key: "arg.remove"},
	{Type: bot.ParamKeyword, Key: "arg.everything"},
	{Type: bot.ParamKeyword, Key: "arg.clean"},
	{Type: bot.ParamGender},
}

func (c *InvasionCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	if reply := RouteToMuteFromType(ctx, "invasion", args); reply != nil {
		return reply
	}

	// Extract @mention pings before parsing
	pings, args := extractPings(args)

	if usage := usageReply(ctx, args, "msg.invasion.usage"); usage != nil {
		return []bot.Reply{*usage}
	}
	if help := helpArgReply(ctx, args, "msg.invasion.usage"); help != nil {
		return []bot.Reply{*help}
	}

	if ctx.Config.General.DisableInvasion {
		return []bot.Reply{{React: "\U0001f645", Text: "This alert type is disabled"}}
	}

	parsed := ctx.ArgMatcher.Match(args, invasionParams, ctx.Language)

	if parsed.HasKeyword("arg.remove") {
		return c.removeInvasions(ctx, parsed)
	}

	common, block := parseCommonTrackFields(ctx, parsed, "invasion")
	if block != nil {
		return []bot.Reply{*block}
	}

	override, overrideReply := parseOverride(ctx, parsed.Strings["location"], parsed.StringLists["area"], common.Distance)
	if overrideReply != nil {
		return []bot.Reply{*overrideReply}
	}

	gender := parsed.Gender

	validTypes := invasionTypeNames(ctx)

	// Match unrecognized args against valid type/event names
	var gruntTypes, unknown []string
	for _, arg := range parsed.Unrecognized {
		lower := strings.ToLower(arg)
		if canonical, ok := validTypes[lower]; ok {
			gruntTypes = append(gruntTypes, canonical)
		} else {
			unknown = append(unknown, arg)
		}
	}

	// Any token that isn't a known type is an error — for add and remove
	// alike. Remove in particular must never fall through to "everything"
	// because of a typo: that would delete every invasion rule.
	if len(unknown) > 0 {
		if len(gruntTypes) == 0 {
			return []bot.Reply{{React: "🙅", Text: tr.T("msg.no_invasion_type")}}
		}
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.unrecognized", strings.Join(unknown, ", "))}}
	}

	// No type given (or the everything keyword) means every type.
	if parsed.HasKeyword("arg.everything") || len(gruntTypes) == 0 {
		gruntTypes = []string{"everything"}
	}

	insert := make([]db.InvasionTrackingAPI, 0, len(gruntTypes))
	for _, gt := range gruntTypes {
		insert = append(insert, db.InvasionTrackingAPI{
			ID:                    ctx.TargetID,
			ProfileNo:             ctx.ProfileNo,
			Ping:                  pings,
			Template:              common.Template,
			Distance:              common.Distance,
			Clean:                 common.Clean,
			Gender:                gender,
			GruntType:             gt,
			OverrideLocationLabel: override.LocationLabel,
			OverrideAreas:         override.Areas,
		})
	}

	tracked, err := ctx.Tracking.Invasions.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("invasion command: select existing: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	if r := overrideLimitReply(ctx, tracked, insert, store.InvasionGetUID, store.InvasionSetUID); r != nil {
		return r
	}
	diff, err := store.ApplyDiff(ctx.Tracking.Invasions, ctx.TargetID, tracked, insert,
		store.InvasionGetUID, store.InvasionSetUID)
	if err != nil {
		log.Errorf("invasion command: apply diff: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	message := buildTrackingMessage(tr, ctx, len(diff.AlreadyPresent), len(diff.Updates), len(diff.Inserts),
		func(i int) string {
			return ctx.RowText.InvasionRowText(tr, invasionAPIToTracking(&diff.AlreadyPresent[i]))
		},
		func(i int) string {
			return ctx.RowText.InvasionRowText(tr, invasionAPIToTracking(&diff.Updates[i]))
		},
		func(i int) string {
			return ctx.RowText.InvasionRowText(tr, invasionAPIToTracking(&diff.Inserts[i]))
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

// invasionRemoveSpec: invasion rules are removed by grunt type (read from
// the leftover words); a gender narrows the selection.
var invasionRemoveSpec = removeSpec{
	filters: []bot.ParamDef{genderParam},
}

func (c *InvasionCommand) removeInvasions(ctx *bot.CommandContext, parsed *bot.ParsedArgs) []bot.Reply {
	validTypes := invasionTypeNames(ctx)
	gruntTypes := make(map[string]bool)
	var named, unknown []string
	for _, arg := range parsed.Unrecognized {
		if canonical, ok := validTypes[strings.ToLower(arg)]; ok {
			gruntTypes[canonical] = true
			named = append(named, arg)
		} else {
			unknown = append(unknown, arg)
		}
	}
	parsed.Unrecognized = unknown
	if reply := checkRemoveArgs(ctx, parsed, invasionRemoveSpec, named); reply != nil {
		return []bot.Reply{*reply}
	}

	tr := ctx.Tr()
	var keep func(*db.InvasionTrackingAPI) bool
	if parsed.Gender != 0 {
		keep = func(r *db.InvasionTrackingAPI) bool { return r.Gender == parsed.Gender }
	}
	return ruleRemover[db.InvasionTrackingAPI]{
		store:  ctx.Tracking.Invasions,
		getUID: store.InvasionGetUID,
		describe: func(r *db.InvasionTrackingAPI) string {
			return ctx.RowText.InvasionRowText(tr, invasionAPIToTracking(r))
		},
		logName: "invasion",
	}.run(ctx, parsed, func(r *db.InvasionTrackingAPI) bool { return gruntTypes[r.GruntType] }, keep)
}

// invasionTypeNames maps every name !invasion accepts for a grunt type or
// pokestop event to its canonical DB value. Sources:
// 1. Grunt template-derived names (dragon, giovanni, arlo, mixed, etc.)
// 2. Translated pokemon type names via poke_type_{id} (user's language + English)
// 3. Pokestop event names (kecleon, showcase, gold-stop)
func invasionTypeNames(ctx *bot.CommandContext) map[string]string {
	validTypes := make(map[string]string) // input name → canonical DB value

	if ctx.GameData != nil {
		tr := ctx.Tr()
		enTr := ctx.Translations.For("en")

		// Group keyword: !invasion boss matches any grunt with Boss=true
		// (Giovanni / Arlo / Cliff / Sierra, regular and event variants).
		validTypes["boss"] = "boss"

		// Grunt types from template strings
		for _, grunt := range ctx.GameData.Grunts {
			canonical := strings.ToLower(gamedata.TypeNameFromTemplate(grunt.Template))
			if canonical == "" {
				continue
			}
			validTypes[canonical] = canonical
			// The parser replaces underscores with spaces in unquoted tokens,
			// so `!invasion npc_0` arrives here as "npc 0". Without this alias
			// every underscore-named grunt (the 24 npc_* and
			// player_team_leader) is unreachable unless the user quotes it.
			if spaced := strings.ReplaceAll(canonical, "_", " "); spaced != canonical {
				validTypes[spaced] = canonical
			}

			// Simplified leader/executive names
			tmpl := strings.ToLower(grunt.Template)
			if after, ok := strings.CutPrefix(tmpl, "character_executive_"); ok {
				short := after
				short = strings.TrimSuffix(short, "_male")
				short = strings.TrimSuffix(short, "_female")
				if short != "" {
					validTypes[short] = canonical
				}
			} else if strings.HasPrefix(tmpl, "character_") && !strings.Contains(tmpl, "_grunt") {
				short := strings.TrimPrefix(tmpl, "character_")
				short = strings.TrimSuffix(short, "_male")
				short = strings.TrimSuffix(short, "_female")
				if short != "" && short != canonical {
					validTypes[short] = canonical
				}
			}

			// Translated type names for typed grunts (e.g. German "Drache" → "dragon")
			if grunt.TypeID > 0 {
				typeKey := gamedata.TypeTranslationKey(grunt.TypeID)
				translated := strings.ToLower(tr.T(typeKey))
				if translated != typeKey {
					validTypes[translated] = canonical
				}
				enTranslated := strings.ToLower(enTr.T(typeKey))
				if enTranslated != typeKey && enTranslated != translated {
					validTypes[enTranslated] = canonical
				}
			}
		}

		// Pokestop events (kecleon, showcase, gold-stop). The canonical DB
		// value is the lowercased English util.json name (see
		// matching/invasion.go ResolveGruntTypeName). Accept both the
		// English util.json name and the translated display_type_N name
		// (from resources/gamelocale/) in the user's language and English.
		if ctx.GameData.Util != nil {
			for id, event := range ctx.GameData.Util.PokestopEvent {
				canonical := strings.ToLower(event.Name)
				if canonical == "" {
					continue
				}
				validTypes[canonical] = canonical

				key := fmt.Sprintf("display_type_%d", id)
				translated := strings.ToLower(tr.T(key))
				if translated != "" && translated != key {
					validTypes[translated] = canonical
				}
				enTranslated := strings.ToLower(enTr.T(key))
				if enTranslated != "" && enTranslated != key && enTranslated != translated {
					validTypes[enTranslated] = canonical
				}
			}
		}
	}
	return validTypes
}
