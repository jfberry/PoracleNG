package commands

import (
	"strings"

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
	{Type: bot.ParamPrefixSingle, Key: "arg.prefix.d"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.template"},
	{Type: bot.ParamKeyword, Key: "arg.remove"},
	{Type: bot.ParamKeyword, Key: "arg.everything"},
	{Type: bot.ParamKeyword, Key: "arg.pokestop"},
	{Type: bot.ParamKeyword, Key: "arg.gym"},
	{Type: bot.ParamKeyword, Key: "arg.location"},
	{Type: bot.ParamKeyword, Key: "arg.new"},
	{Type: bot.ParamKeyword, Key: "arg.removal"},
	{Type: bot.ParamKeyword, Key: "arg.photo"},
	{Type: bot.ParamKeyword, Key: "arg.include_empty"},
}

func (c *FortCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	// Extract @mention pings before parsing
	pings, args := extractPings(args)

	if usage := usageReply(ctx, args, "cmd.fort.usage"); usage != nil {
		return []bot.Reply{*usage}
	}
	if help := helpArgReply(ctx, args, "cmd.fort.usage"); help != nil {
		return []bot.Reply{*help}
	}

	parsed := ctx.ArgMatcher.Match(args, fortParams, ctx.Language)

	if warn := bot.ReportUnrecognized(parsed, tr); warn != nil {
		return []bot.Reply{*warn}
	}

	template := ctx.DefaultTemplate()
	if t, ok := parsed.Strings["template"]; ok {
		template = t
	}

	// Validate template exists
	var templateWarn string
	if block, warn := validateTemplate(ctx, "fort-update", template); block != nil {
		return []bot.Reply{*block}
	} else {
		templateWarn = warn
	}

	distance := 0
	if d, ok := parsed.Singles["d"]; ok {
		distance = d
	}
	distance = enforceDistance(ctx, distance)
	includeEmpty := parsed.HasKeyword("arg.include_empty")

	// Determine fort_type
	fortType := ""
	if parsed.HasKeyword("arg.everything") {
		fortType = "everything"
	} else if parsed.HasKeyword("arg.pokestop") {
		fortType = "pokestop"
	} else if parsed.HasKeyword("arg.gym") {
		fortType = "gym"
	} else {
		fortType = "everything"
	}

	// Collect change types from keywords
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
		changeTypes = append(changeTypes, "photo")
	}

	changeTypesStr := strings.Join(changeTypes, ",")

	if parsed.HasKeyword("arg.remove") {
		return c.removeForts(ctx, fortType)
	}

	insert := []db.FortTrackingAPI{{
		ID:           ctx.TargetID,
		ProfileNo:    ctx.ProfileNo,
		Ping:         pings,
		Template:     template,
		Distance:     distance,
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

	message += trackingWarnings(ctx, distance)

	if templateWarn != "" {
		message += "\n⚠️ " + templateWarn
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
	for _, existing := range tracked {
		if fortType == "everything" || existing.FortType == fortType {
			uids = append(uids, existing.UID)
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
	return []bot.Reply{{React: "✅", Text: tr.Tf("cmd.removed_n", len(uids))}}
}
