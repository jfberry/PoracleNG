package commands

import (
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/api"
	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
)

type LureCommand struct{}

func (c *LureCommand) Name() string      { return "cmd.lure" }
func (c *LureCommand) Aliases() []string { return nil }

var lureParams = []bot.ParamDef{
	{Type: bot.ParamPrefixSingle, Key: "arg.prefix.d"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.template"},
	{Type: bot.ParamKeyword, Key: "arg.remove"},
	{Type: bot.ParamKeyword, Key: "arg.everything"},
	{Type: bot.ParamKeyword, Key: "arg.clean"},
	{Type: bot.ParamLureType},
}

func (c *LureCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	if usage := usageReply(ctx, args, "cmd.lure.usage"); usage != nil {
		return []bot.Reply{*usage}
	}
	if help := helpArgReply(ctx, args, "cmd.lure.usage"); help != nil {
		return []bot.Reply{*help}
	}

	parsed := ctx.ArgMatcher.Match(args, lureParams, ctx.Language)

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
	distance = enforceDistance(ctx, distance)
	clean := parsed.HasKeyword("arg.clean")

	// Collect lure IDs
	lureIDs := []int{}
	if parsed.LureType != 0 || parsed.HasKeyword("arg.everything") {
		if parsed.HasKeyword("arg.everything") {
			lureIDs = append(lureIDs, 0) // 0 = any lure
		} else {
			lureIDs = append(lureIDs, parsed.LureType)
		}
	} else if parsed.LureType == 0 && !parsed.HasKeyword("arg.everything") {
		// Check if a lure type name matched with ID 0 (normal)
		// If no lure type at all, show error
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.no_lure_type")}}
	}

	if parsed.HasKeyword("arg.remove") {
		return removeLures(ctx, lureIDs)
	}

	insert := make([]db.LureTrackingAPI, 0, len(lureIDs))
	for _, id := range lureIDs {
		insert = append(insert, db.LureTrackingAPI{
			ID:        ctx.TargetID,
			ProfileNo: ctx.ProfileNo,
			LureID:    id,
			Distance:  distance,
			Template:  template,
			Clean:     db.IntBool(clean),
		})
	}

	tracked, err := db.SelectLuresByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("lure command: select existing: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	var updates, alreadyPresent []db.LureTrackingAPI
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
		func(i int) string { return ctx.RowText.LureRowText(tr, lureAPIToTracking(&alreadyPresent[i])) },
		func(i int) string { return ctx.RowText.LureRowText(tr, lureAPIToTracking(&updates[i])) },
		func(i int) string { return ctx.RowText.LureRowText(tr, lureAPIToTracking(&insert[i])) },
	)

	if len(updates) > 0 {
		uids := make([]int64, len(updates))
		for i, u := range updates {
			uids[i] = u.UID
		}
		db.DeleteByUIDs(ctx.DB, "lures", ctx.TargetID, uids)
	}
	toInsert := append(insert, updates...)
	for i := range toInsert {
		if _, err := db.InsertLure(ctx.DB, &toInsert[i]); err != nil {
			log.Errorf("lure command: insert: %s", err)
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

func removeLures(ctx *bot.CommandContext, lureIDs []int) []bot.Reply {
	tracked, err := db.SelectLuresByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		return []bot.Reply{{React: "🙅"}}
	}
	idSet := make(map[int]bool)
	for _, id := range lureIDs {
		idSet[id] = true
	}
	var uids []int64
	for _, existing := range tracked {
		if idSet[existing.LureID] || idSet[0] {
			uids = append(uids, existing.UID)
		}
	}
	if len(uids) == 0 {
		return []bot.Reply{{React: "👌"}}
	}
	db.DeleteByUIDs(ctx.DB, "lures", ctx.TargetID, uids)
	ctx.TriggerReload()
	tr := ctx.Tr()
	return []bot.Reply{{React: "✅", Text: tr.Tf("cmd.removed_n", len(uids))}}
}
