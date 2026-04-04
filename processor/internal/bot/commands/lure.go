package commands

import (
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/store"
)

type LureCommand struct{}

func (c *LureCommand) Name() string      { return "cmd.lure" }
func (c *LureCommand) Aliases() []string { return nil }

var lureParams = []bot.ParamDef{
	{Type: bot.ParamRemoveUID},
	{Type: bot.ParamPrefixSingle, Key: "arg.prefix.d"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.template"},
	{Type: bot.ParamKeyword, Key: "arg.remove"},
	{Type: bot.ParamKeyword, Key: "arg.everything"},
	{Type: bot.ParamKeyword, Key: "arg.clean"},
	{Type: bot.ParamLureType},
}

func (c *LureCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	// Extract @mention pings before parsing
	pings, args := extractPings(args)

	if usage := usageReply(ctx, args, "msg.lure.usage"); usage != nil {
		return []bot.Reply{*usage}
	}
	if help := helpArgReply(ctx, args, "msg.lure.usage"); help != nil {
		return []bot.Reply{*help}
	}

	if ctx.Config.General.DisableLure {
		return []bot.Reply{{React: "\U0001f645", Text: "This alert type is disabled"}}
	}

	parsed := ctx.ArgMatcher.Match(args, lureParams, ctx.Language)

	if warn := bot.ReportUnrecognized(parsed, tr); warn != nil {
		return []bot.Reply{*warn}
	}

	common, block := parseCommonTrackFields(ctx, parsed, "lure")
	if block != nil {
		return []bot.Reply{*block}
	}

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
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.no_lure_type")}}
	}

	if parsed.HasKeyword("arg.remove") {
		if len(parsed.RemoveUIDs) > 0 {
			tr := ctx.Tr()
			return removeByUIDs(ctx, ctx.Tracking.Lures, parsed.RemoveUIDs,
				store.LureGetUID,
				func(r *db.LureTrackingAPI) string { return ctx.RowText.LureRowText(tr, lureAPIToTracking(r)) },
			)
		}
		return removeLures(ctx, lureIDs)
	}

	insert := make([]db.LureTrackingAPI, 0, len(lureIDs))
	for _, id := range lureIDs {
		insert = append(insert, db.LureTrackingAPI{
			ID:        ctx.TargetID,
			ProfileNo: ctx.ProfileNo,
			Ping:      pings,
			LureID:    id,
			Distance:  common.Distance,
			Template:  common.Template,
			Clean:     db.IntBool(common.Clean),
		})
	}

	tracked, err := ctx.Tracking.Lures.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("lure command: select existing: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	diff, err := store.ApplyDiff(ctx.Tracking.Lures, ctx.TargetID, tracked, insert,
		store.LureGetUID, store.LureSetUID)
	if err != nil {
		log.Errorf("lure command: apply diff: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	message := buildTrackingMessage(tr, ctx, len(diff.AlreadyPresent), len(diff.Updates), len(diff.Inserts),
		func(i int) string { return ctx.RowText.LureRowText(tr, lureAPIToTracking(&diff.AlreadyPresent[i])) },
		func(i int) string { return ctx.RowText.LureRowText(tr, lureAPIToTracking(&diff.Updates[i])) },
		func(i int) string { return ctx.RowText.LureRowText(tr, lureAPIToTracking(&diff.Inserts[i])) },
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

func removeLures(ctx *bot.CommandContext, lureIDs []int) []bot.Reply {
	tracked, err := ctx.Tracking.Lures.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		return []bot.Reply{{React: "🙅"}}
	}
	idSet := make(map[int]bool)
	for _, id := range lureIDs {
		idSet[id] = true
	}
	var uids []int64
	var removed []db.LureTrackingAPI
	for _, existing := range tracked {
		if idSet[existing.LureID] || idSet[0] {
			uids = append(uids, existing.UID)
			removed = append(removed, existing)
		}
	}
	if len(uids) == 0 {
		return []bot.Reply{{React: "👌"}}
	}
	if err := ctx.Tracking.Lures.DeleteByUIDs(ctx.TargetID, uids); err != nil {
		log.Errorf("lure command: delete: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}
	ctx.TriggerReload()
	tr := ctx.Tr()
	var descriptions []string
	for i := range removed {
		descriptions = append(descriptions, ctx.RowText.LureRowText(tr, lureAPIToTracking(&removed[i])))
	}
	return []bot.Reply{{React: "✅", Text: formatRemovedRows(tr, descriptions)}}
}
