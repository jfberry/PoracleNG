package commands

import (
	"encoding/json"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// FortCommand implements !fort — track fort (pokestop/gym) updates.
type FortCommand struct{}

func (c *FortCommand) Name() string      { return "cmd.fort" }
func (c *FortCommand) Aliases() []string { return nil }

var fortParams = []bot.ParamDef{
	{Type: bot.ParamRemoveUID},
	{Type: bot.ParamPrefixSingle, Key: "arg.prefix.d"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.template"},
	{Type: bot.ParamKeyword, Key: "arg.remove"},
	{Type: bot.ParamKeyword, Key: "arg.everything"},
	{Type: bot.ParamKeyword, Key: "arg.pokestop"},
	{Type: bot.ParamKeyword, Key: "arg.gym"},
	{Type: bot.ParamKeyword, Key: "arg.station"},
	{Type: bot.ParamKeyword, Key: "arg.location"},
	{Type: bot.ParamKeyword, Key: "arg.new"},
	{Type: bot.ParamKeyword, Key: "arg.removal"},
	{Type: bot.ParamKeyword, Key: "arg.photo"},
	{Type: bot.ParamKeyword, Key: "arg.name"},
	{Type: bot.ParamKeyword, Key: "arg.description"},
	{Type: bot.ParamKeyword, Key: "arg.include_empty"},
}

func (c *FortCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	// Extract @mention pings before parsing
	pings, args := extractPings(args)

	if usage := usageReply(ctx, args, "msg.fort.usage"); usage != nil {
		return []bot.Reply{*usage}
	}
	if help := helpArgReply(ctx, args, "msg.fort.usage"); help != nil {
		return []bot.Reply{*help}
	}

	if ctx.Config.General.DisableFortUpdate {
		return []bot.Reply{{React: "\U0001f645", Text: "This alert type is disabled"}}
	}

	parsed := ctx.ArgMatcher.Match(args, fortParams, ctx.Language)

	if warn := bot.ReportUnrecognized(parsed, tr); warn != nil {
		return []bot.Reply{*warn}
	}

	common, block := parseCommonTrackFields(ctx, parsed, "fort-update")
	if block != nil {
		return []bot.Reply{*block}
	}
	includeEmpty := parsed.HasKeyword("arg.include_empty")

	// Determine fort_type. Per Golbat webhooks-reference, fort_update
	// can carry "pokestop", "gym", or "station". The matcher compares
	// case-insensitively, so storing the lowercase string here matches
	// whatever Golbat emits.
	fortType := ""
	if parsed.HasKeyword("arg.everything") {
		fortType = "everything"
	} else if parsed.HasKeyword("arg.pokestop") {
		fortType = "pokestop"
	} else if parsed.HasKeyword("arg.gym") {
		fortType = "gym"
	} else if parsed.HasKeyword("arg.station") {
		fortType = "station"
	} else {
		fortType = "everything"
	}

	// Collect change types from keywords. Stored values must match what
	// Golbat emits in `change_type` and `edit_types[]` so the matcher's
	// changeTypesMatch lookup succeeds — most keywords map 1:1, but
	// `photo` maps to `image_url` (the Golbat field name) since that's
	// what arrives in edit_types when a Niantic photo URL changes.
	var changeTypes []string
	if parsed.HasKeyword("arg.location") {
		changeTypes = append(changeTypes, "location")
	}
	if parsed.HasKeyword("arg.new") {
		changeTypes = append(changeTypes, "new")
	}
	if parsed.HasKeyword("arg.removal") {
		changeTypes = append(changeTypes, "removal")
	}
	if parsed.HasKeyword("arg.photo") {
		changeTypes = append(changeTypes, "image_url")
	}
	if parsed.HasKeyword("arg.name") {
		changeTypes = append(changeTypes, "name")
	}
	if parsed.HasKeyword("arg.description") {
		changeTypes = append(changeTypes, "description")
	}

	// JSON-encode for the DB column. The matcher (matching/fort.go
	// changeTypesMatch) parses this as JSON; storing a comma-separated
	// string here would silently fail to match. The API endpoint and
	// PoracleJS both use JSON — this brings !fort into line with both.
	changeTypesStr := "[]"
	if len(changeTypes) > 0 {
		b, _ := json.Marshal(changeTypes)
		changeTypesStr = string(b)
	}

	if parsed.HasKeyword("arg.remove") {
		if len(parsed.RemoveUIDs) > 0 {
			tr := ctx.Tr()
			return removeByUIDs(ctx, ctx.Tracking.Forts, parsed.RemoveUIDs,
				store.FortGetUID,
				func(r *db.FortTrackingAPI) string { return ctx.RowText.FortUpdateRowText(tr, fortAPIToTracking(r)) },
			)
		}
		return c.removeForts(ctx, fortType)
	}

	insert := []db.FortTrackingAPI{{
		ID:           ctx.TargetID,
		ProfileNo:    ctx.ProfileNo,
		Ping:         pings,
		Template:     common.Template,
		Distance:     common.Distance,
		FortType:     fortType,
		IncludeEmpty: db.IntBool(includeEmpty),
		ChangeTypes:  changeTypesStr,
	}}

	tracked, err := ctx.Tracking.Forts.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("fort command: select existing: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	diff, err := store.ApplyDiff(ctx.Tracking.Forts, ctx.TargetID, tracked, insert,
		store.FortGetUID, store.FortSetUID)
	if err != nil {
		log.Errorf("fort command: apply diff: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	message := buildTrackingMessage(tr, ctx, len(diff.AlreadyPresent), len(diff.Updates), len(diff.Inserts),
		func(i int) string {
			return ctx.RowText.FortUpdateRowText(tr, fortAPIToTracking(&diff.AlreadyPresent[i]))
		},
		func(i int) string {
			return ctx.RowText.FortUpdateRowText(tr, fortAPIToTracking(&diff.Updates[i]))
		},
		func(i int) string {
			return ctx.RowText.FortUpdateRowText(tr, fortAPIToTracking(&diff.Inserts[i]))
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

func (c *FortCommand) removeForts(ctx *bot.CommandContext, fortType string) []bot.Reply {
	tracked, err := ctx.Tracking.Forts.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("fort command: select for remove: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	var uids []int64
	var removed []db.FortTrackingAPI
	for _, existing := range tracked {
		if fortType == "everything" || existing.FortType == fortType {
			uids = append(uids, existing.UID)
			removed = append(removed, existing)
		}
	}
	if len(uids) == 0 {
		return []bot.Reply{{React: "👌"}}
	}
	if err := ctx.Tracking.Forts.DeleteByUIDs(ctx.TargetID, uids); err != nil {
		log.Errorf("fort command: delete: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}
	ctx.TriggerReload()
	tr := ctx.Tr()
	var descriptions []string
	for i := range removed {
		descriptions = append(descriptions, ctx.RowText.FortUpdateRowText(tr, fortAPIToTracking(&removed[i])))
	}
	return []bot.Reply{{React: "✅", Text: formatRemovedRows(tr, descriptions)}}
}
