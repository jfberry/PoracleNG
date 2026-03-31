package commands

import (
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/api"
	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
)

// InvasionCommand implements !invasion / !incident — track Team Rocket invasions.
type InvasionCommand struct{}

func (c *InvasionCommand) Name() string      { return "cmd.invasion" }
func (c *InvasionCommand) Aliases() []string { return []string{"cmd.incident"} }

var invasionParams = []bot.ParamDef{
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

	if usage := usageReply(ctx, args, "cmd.invasion.usage"); usage != nil {
		return []bot.Reply{*usage}
	}
	if help := helpArgReply(ctx, args, "cmd.invasion.usage"); help != nil {
		return []bot.Reply{*help}
	}

	parsed := ctx.ArgMatcher.Match(args, invasionParams, ctx.Language)

	template := ctx.DefaultTemplate()
	if t, ok := parsed.Strings["template"]; ok {
		template = t
	}
	distance := 0
	if d, ok := parsed.Singles["d"]; ok {
		distance = d
	}
	distance = enforceDistance(ctx, distance)
	clean := parsed.HasKeyword("arg.clean")
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
			return []bot.Reply{{React: "🙅", Text: tr.T("cmd.no_invasion_type")}}
		}
		gruntTypes = append(gruntTypes, "everything")
	}
	if len(gruntTypes) == 0 {
		gruntTypes = append(gruntTypes, "everything")
	}

	if parsed.HasKeyword("arg.remove") {
		return c.removeInvasions(ctx, gruntTypes)
	}

	insert := make([]db.InvasionTrackingAPI, 0, len(gruntTypes))
	for _, gt := range gruntTypes {
		insert = append(insert, db.InvasionTrackingAPI{
			ID:        ctx.TargetID,
			ProfileNo: ctx.ProfileNo,
			Ping:      pings,
			Template:  template,
			Distance:  distance,
			Clean:     db.IntBool(clean),
			Gender:    gender,
			GruntType: gt,
		})
	}

	tracked, err := db.SelectInvasionsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("invasion command: select existing: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	var updates, alreadyPresent []db.InvasionTrackingAPI
	for i := len(insert) - 1; i >= 0; i-- {
		for _, existing := range tracked {
			noMatch, isDup, uid, isUpd := api.DiffTracking(&existing, &insert[i])
			if noMatch {
				continue
			}
			if isDup {
				alreadyPresent = append(alreadyPresent, insert[i])
				insert = append(insert[:i], insert[i+1:]...)
				break
			}
			if isUpd {
				u := insert[i]
				u.UID = uid
				updates = append(updates, u)
				insert = append(insert[:i], insert[i+1:]...)
				break
			}
		}
	}

	message := buildTrackingMessage(tr, ctx, len(alreadyPresent), len(updates), len(insert),
		func(i int) string {
			return ctx.RowText.InvasionRowText(tr, invasionAPIToTracking(&alreadyPresent[i]))
		},
		func(i int) string {
			return ctx.RowText.InvasionRowText(tr, invasionAPIToTracking(&updates[i]))
		},
		func(i int) string {
			return ctx.RowText.InvasionRowText(tr, invasionAPIToTracking(&insert[i]))
		},
	)

	if len(updates) > 0 {
		uids := make([]int64, len(updates))
		for i, u := range updates {
			uids[i] = u.UID
		}
		if err := db.DeleteByUIDs(ctx.DB, "invasion", ctx.TargetID, uids); err != nil {
			log.Errorf("invasion command: delete updated: %s", err)
			return []bot.Reply{{React: "🙅"}}
		}
	}

	toInsert := append(insert, updates...)
	for i := range toInsert {
		if _, err := db.InsertInvasion(ctx.DB, &toInsert[i]); err != nil {
			log.Errorf("invasion command: insert: %s", err)
			return []bot.Reply{{React: "🙅"}}
		}
	}

	ctx.TriggerReload()
	react := "✅"
	if len(insert) == 0 && len(updates) == 0 {
		react = "👌"
	}
	return []bot.Reply{{React: react, Text: message}}
}

func (c *InvasionCommand) removeInvasions(ctx *bot.CommandContext, gruntTypes []string) []bot.Reply {
	tracked, err := db.SelectInvasionsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("invasion command: select for remove: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	gtSet := make(map[string]bool)
	for _, gt := range gruntTypes {
		gtSet[gt] = true
	}

	var uids []int64
	for _, existing := range tracked {
		// "everything" means remove all
		if gtSet["everything"] || gtSet[existing.GruntType] {
			uids = append(uids, existing.UID)
		}
	}
	if len(uids) == 0 {
		return []bot.Reply{{React: "👌"}}
	}
	if err := db.DeleteByUIDs(ctx.DB, "invasion", ctx.TargetID, uids); err != nil {
		log.Errorf("invasion command: delete: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}
	ctx.TriggerReload()
	tr := ctx.Tr()
	return []bot.Reply{{React: "✅", Text: tr.Tf("cmd.removed_n", len(uids))}}
}
