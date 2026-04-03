package commands

import (
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
	{Type: bot.ParamKeyword, Key: "arg.remove"},
	{Type: bot.ParamKeyword, Key: "arg.everything"},
	{Type: bot.ParamKeyword, Key: "arg.clean"},
	{Type: bot.ParamGender},
}

func (c *InvasionCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	// Extract @mention pings before parsing
	pings, args := extractPings(args)

	if usage := usageReply(ctx, args, "msg.invasion.usage"); usage != nil {
		return []bot.Reply{*usage}
	}
	if help := helpArgReply(ctx, args, "msg.invasion.usage"); help != nil {
		return []bot.Reply{*help}
	}

	parsed := ctx.ArgMatcher.Match(args, invasionParams, ctx.Language)

	common, block := parseCommonTrackFields(ctx, parsed, "invasion")
	if block != nil {
		return []bot.Reply{*block}
	}
	gender := parsed.Gender

	// Build valid type name set from multiple sources:
	// 1. Grunt template-derived names (dragon, giovanni, arlo, mixed, etc.)
	// 2. Translated pokemon type names via poke_type_{id} (user's language + English)
	// 3. Pokestop event names (kecleon, showcase, gold-stop)
	validTypes := make(map[string]string) // input name → canonical DB value

	if ctx.GameData != nil {
		tr := ctx.Tr()
		enTr := ctx.Translations.For("en")

		// Grunt types from template strings
		for _, grunt := range ctx.GameData.Grunts {
			canonical := strings.ToLower(gamedata.TypeNameFromTemplate(grunt.Template))
			if canonical == "" {
				continue
			}
			validTypes[canonical] = canonical

			// Simplified leader/executive names
			tmpl := strings.ToLower(grunt.Template)
			if strings.HasPrefix(tmpl, "character_executive_") {
				short := strings.TrimPrefix(tmpl, "character_executive_")
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

		// Pokestop events (kecleon, showcase, gold-stop)
		if ctx.GameData.Util != nil {
			for _, event := range ctx.GameData.Util.PokestopEvent {
				name := strings.ToLower(event.Name)
				if name != "" {
					validTypes[name] = name
				}
			}
		}
	}

	// Match unrecognized args against valid type/event names
	var gruntTypes []string
	if parsed.HasKeyword("arg.everything") {
		gruntTypes = append(gruntTypes, "everything")
	} else {
		for _, arg := range parsed.Unrecognized {
			lower := strings.ToLower(arg)
			if canonical, ok := validTypes[lower]; ok {
				gruntTypes = append(gruntTypes, canonical)
			}
		}
	}

	// If nothing matched, report unrecognized or default to everything
	if len(gruntTypes) == 0 && !parsed.HasKeyword("arg.remove") {
		if len(parsed.Unrecognized) > 0 {
			return []bot.Reply{{React: "🙅", Text: tr.T("msg.no_invasion_type")}}
		}
		gruntTypes = append(gruntTypes, "everything")
	}
	if len(gruntTypes) == 0 {
		gruntTypes = append(gruntTypes, "everything")
	}

	if parsed.HasKeyword("arg.remove") {
		return c.removeInvasions(ctx, parsed, gruntTypes)
	}

	insert := make([]db.InvasionTrackingAPI, 0, len(gruntTypes))
	for _, gt := range gruntTypes {
		insert = append(insert, db.InvasionTrackingAPI{
			ID:        ctx.TargetID,
			ProfileNo: ctx.ProfileNo,
			Ping:      pings,
			Template:  common.Template,
			Distance:  common.Distance,
			Clean:     db.IntBool(common.Clean),
			Gender:    gender,
			GruntType: gt,
		})
	}

	tracked, err := ctx.Tracking.Invasions.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("invasion command: select existing: %s", err)
		return []bot.Reply{{React: "🙅"}}
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

func (c *InvasionCommand) removeInvasions(ctx *bot.CommandContext, parsed *bot.ParsedArgs, gruntTypes []string) []bot.Reply {
	if len(parsed.RemoveUIDs) > 0 {
		tr := ctx.Tr()
		return removeByUIDs(ctx, ctx.Tracking.Invasions, parsed.RemoveUIDs,
			store.InvasionGetUID,
			func(r *db.InvasionTrackingAPI) string { return ctx.RowText.InvasionRowText(tr, invasionAPIToTracking(r)) },
		)
	}

	tracked, err := ctx.Tracking.Invasions.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("invasion command: select for remove: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	gtSet := make(map[string]bool)
	for _, gt := range gruntTypes {
		gtSet[gt] = true
	}

	var uids []int64
	var removed []db.InvasionTrackingAPI
	for _, existing := range tracked {
		// "everything" means remove all
		if gtSet["everything"] || gtSet[existing.GruntType] {
			uids = append(uids, existing.UID)
			removed = append(removed, existing)
		}
	}
	if len(uids) == 0 {
		return []bot.Reply{{React: "👌"}}
	}
	if err := ctx.Tracking.Invasions.DeleteByUIDs(ctx.TargetID, uids); err != nil {
		log.Errorf("invasion command: delete: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}
	ctx.TriggerReload()
	tr := ctx.Tr()
	var descriptions []string
	for i := range removed {
		descriptions = append(descriptions, ctx.RowText.InvasionRowText(tr, invasionAPIToTracking(&removed[i])))
	}
	return []bot.Reply{{React: "✅", Text: formatRemovedRows(tr, descriptions)}}
}
