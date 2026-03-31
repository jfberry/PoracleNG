package commands

import (
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/api"
	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
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

	if usage := usageReply(ctx, args, "cmd.fort.usage"); usage != nil {
		return []bot.Reply{*usage}
	}

	parsed := ctx.ArgMatcher.Match(args, fortParams, ctx.Language)

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
		Ping:         "",
		Template:     template,
		Distance:     distance,
		FortType:     fortType,
		IncludeEmpty: db.IntBool(includeEmpty),
		ChangeTypes:  changeTypesStr,
	}}

	tracked, err := db.SelectFortsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("fort command: select existing: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	var updates, alreadyPresent []db.FortTrackingAPI
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
			return ctx.RowText.FortUpdateRowText(tr, fortAPIToTracking(&alreadyPresent[i]))
		},
		func(i int) string {
			return ctx.RowText.FortUpdateRowText(tr, fortAPIToTracking(&updates[i]))
		},
		func(i int) string {
			return ctx.RowText.FortUpdateRowText(tr, fortAPIToTracking(&insert[i]))
		},
	)

	if len(updates) > 0 {
		uids := make([]int64, len(updates))
		for i, u := range updates {
			uids[i] = u.UID
		}
		if err := db.DeleteByUIDs(ctx.DB, "forts", ctx.TargetID, uids); err != nil {
			log.Errorf("fort command: delete updated: %s", err)
			return []bot.Reply{{React: "🙅"}}
		}
	}

	toInsert := append(insert, updates...)
	for i := range toInsert {
		if _, err := db.InsertFort(ctx.DB, &toInsert[i]); err != nil {
			log.Errorf("fort command: insert: %s", err)
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

func (c *FortCommand) removeForts(ctx *bot.CommandContext, fortType string) []bot.Reply {
	tracked, err := db.SelectFortsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
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
	if err := db.DeleteByUIDs(ctx.DB, "forts", ctx.TargetID, uids); err != nil {
		log.Errorf("fort command: delete: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}
	ctx.TriggerReload()
	tr := ctx.Tr()
	return []bot.Reply{{React: "✅", Text: tr.Tf("cmd.removed_n", len(uids))}}
}
