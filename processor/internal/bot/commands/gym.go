package commands

import (
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/api"
	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
)

// GymCommand implements !gym — track gym team changes, slot changes, and battle changes.
type GymCommand struct{}

func (c *GymCommand) Name() string      { return "cmd.gym" }
func (c *GymCommand) Aliases() []string { return nil }

var gymParams = []bot.ParamDef{
	{Type: bot.ParamPrefixSingle, Key: "arg.prefix.d"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.template"},
	{Type: bot.ParamKeyword, Key: "arg.remove"},
	{Type: bot.ParamKeyword, Key: "arg.everything"},
	{Type: bot.ParamKeyword, Key: "arg.clean"},
	{Type: bot.ParamKeyword, Key: "arg.slot_changes"},
	{Type: bot.ParamKeyword, Key: "arg.battle_changes"},
	{Type: bot.ParamTeam},
}

func (c *GymCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	if usage := usageReply(ctx, args, "cmd.gym.usage"); usage != nil {
		return []bot.Reply{*usage}
	}

	parsed := ctx.ArgMatcher.Match(args, gymParams, ctx.Language)

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
	slotChanges := parsed.HasKeyword("arg.slot_changes")

	battleChanges := false
	if parsed.HasKeyword("arg.battle_changes") {
		if ctx.Config.Tracking.EnableGymBattle {
			battleChanges = true
		} else {
			return []bot.Reply{{React: "🙅", Text: tr.T("cmd.gym.battle_disabled")}}
		}
	}

	// Team: 4 = unset default, "everything" sets all teams
	teams := []int{}
	if parsed.HasKeyword("arg.everything") {
		// Everything: track all teams (0=harmony, 1=mystic, 2=valor, 3=instinct)
		teams = append(teams, 0, 1, 2, 3)
	} else if parsed.Team != 4 {
		teams = append(teams, parsed.Team)
	} else {
		// Default: team 4 (any team)
		teams = append(teams, 4)
	}

	if parsed.HasKeyword("arg.remove") {
		return c.removeGyms(ctx, teams)
	}

	insert := make([]db.GymTrackingAPI, 0, len(teams))
	for _, team := range teams {
		insert = append(insert, db.GymTrackingAPI{
			ID:            ctx.TargetID,
			ProfileNo:     ctx.ProfileNo,
			Ping:          "",
			Template:      template,
			Distance:      distance,
			Team:          team,
			Clean:         db.IntBool(clean),
			SlotChanges:   db.IntBool(slotChanges),
			BattleChanges: db.IntBool(battleChanges),
			GymID:         nil,
		})
	}

	tracked, err := db.SelectGymsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("gym command: select existing: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	var updates, alreadyPresent []db.GymTrackingAPI
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
		func(i int) string { return ctx.RowText.GymRowText(tr, gymAPIToTracking(&alreadyPresent[i])) },
		func(i int) string { return ctx.RowText.GymRowText(tr, gymAPIToTracking(&updates[i])) },
		func(i int) string { return ctx.RowText.GymRowText(tr, gymAPIToTracking(&insert[i])) },
	)

	if len(updates) > 0 {
		uids := make([]int64, len(updates))
		for i, u := range updates {
			uids[i] = u.UID
		}
		if err := db.DeleteByUIDs(ctx.DB, "gym", ctx.TargetID, uids); err != nil {
			log.Errorf("gym command: delete updated: %s", err)
			return []bot.Reply{{React: "🙅"}}
		}
	}

	toInsert := append(insert, updates...)
	for i := range toInsert {
		if _, err := db.InsertGym(ctx.DB, &toInsert[i]); err != nil {
			log.Errorf("gym command: insert: %s", err)
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

func (c *GymCommand) removeGyms(ctx *bot.CommandContext, teams []int) []bot.Reply {
	tracked, err := db.SelectGymsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("gym command: select for remove: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	teamSet := make(map[int]bool)
	for _, t := range teams {
		teamSet[t] = true
	}

	var uids []int64
	for _, existing := range tracked {
		if teamSet[existing.Team] || teamSet[4] {
			uids = append(uids, existing.UID)
		}
	}
	if len(uids) == 0 {
		return []bot.Reply{{React: "👌"}}
	}
	if err := db.DeleteByUIDs(ctx.DB, "gym", ctx.TargetID, uids); err != nil {
		log.Errorf("gym command: delete: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}
	ctx.TriggerReload()
	tr := ctx.Tr()
	return []bot.Reply{{React: "✅", Text: tr.Tf("cmd.removed_n", len(uids))}}
}
