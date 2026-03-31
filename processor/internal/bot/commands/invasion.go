package commands

import (
	"fmt"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/api"
	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
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
	{Type: bot.ParamTypeName},
}

func (c *InvasionCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	parsed := ctx.ArgMatcher.Match(args, invasionParams, ctx.Language)

	if warn := bot.ReportUnrecognized(parsed, tr); warn != nil {
		return []bot.Reply{*warn}
	}

	template := ctx.DefaultTemplate()
	if t, ok := parsed.Strings["template"]; ok {
		template = t
	}
	distance := 0
	if d, ok := parsed.Singles["d"]; ok {
		distance = d
	}
	clean := parsed.HasKeyword("arg.clean")
	gender := parsed.Gender

	// Resolve grunt types from type names
	// In the DB, grunt_type is stored as a string like "mixed" or a type name.
	// Type IDs from ParamTypeName are used to build the grunt_type string.
	var gruntTypes []string
	if parsed.HasKeyword("arg.everything") {
		gruntTypes = append(gruntTypes, "") // empty = everything
	} else if len(parsed.Types) > 0 {
		for _, typeID := range parsed.Types {
			gruntTypes = append(gruntTypes, fmt.Sprintf("%d", typeID))
		}
	} else {
		// Default: empty string (any grunt)
		gruntTypes = append(gruntTypes, "")
	}

	if parsed.HasKeyword("arg.remove") {
		return c.removeInvasions(ctx, gruntTypes)
	}

	insert := make([]db.InvasionTrackingAPI, 0, len(gruntTypes))
	for _, gt := range gruntTypes {
		insert = append(insert, db.InvasionTrackingAPI{
			ID:        ctx.TargetID,
			ProfileNo: ctx.ProfileNo,
			Ping:      "",
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
		// Empty string in gtSet means "everything" — remove all
		if gtSet[""] || gtSet[existing.GruntType] {
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
