package commands

import (
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// GymCommand implements !gym — track gym team changes, slot changes, and battle changes.
type GymCommand struct{}

func (c *GymCommand) Name() string      { return "cmd.gym" }
func (c *GymCommand) Aliases() []string { return nil }

var gymParams = []bot.ParamDef{
	{Type: bot.ParamRemoveUID},
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

	// Extract @mention pings before parsing
	pings, args := extractPings(args)

	if usage := usageReply(ctx, args, "msg.gym.usage"); usage != nil {
		return []bot.Reply{*usage}
	}
	if help := helpArgReply(ctx, args, "msg.gym.usage"); help != nil {
		return []bot.Reply{*help}
	}

	parsed := ctx.ArgMatcher.Match(args, gymParams, ctx.Language)

	if warn := bot.ReportUnrecognized(parsed, tr); warn != nil {
		return []bot.Reply{*warn}
	}

	template := ctx.DefaultTemplate()
	if t, ok := parsed.Strings["template"]; ok {
		template = t
	}

	// Validate template exists
	var templateWarn string
	if _, explicit := parsed.Strings["template"]; explicit {
		if block, warn := validateTemplate(ctx, "gym", template); block != nil {
			return []bot.Reply{*block}
		} else {
			templateWarn = warn
		}
	}

	distance := 0
	if d, ok := parsed.Singles["d"]; ok {
		distance = d
	}
	distance = enforceDistance(ctx, distance)
	clean := parsed.HasKeyword("arg.clean")
	slotChanges := parsed.HasKeyword("arg.slot_changes")

	battleChanges := false
	if parsed.HasKeyword("arg.battle_changes") {
		if ctx.Config.Tracking.EnableGymBattle {
			battleChanges = true
		} else {
			return []bot.Reply{{React: "🙅", Text: tr.T("msg.gym.battle_disabled")}}
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
		if len(parsed.RemoveUIDs) > 0 {
			return removeByUIDs(ctx, ctx.Tracking.Gyms, parsed.RemoveUIDs)
		}
		return c.removeGyms(ctx, teams)
	}

	insert := make([]db.GymTrackingAPI, 0, len(teams))
	for _, team := range teams {
		insert = append(insert, db.GymTrackingAPI{
			ID:            ctx.TargetID,
			ProfileNo:     ctx.ProfileNo,
			Ping:          pings,
			Template:      template,
			Distance:      distance,
			Team:          team,
			Clean:         db.IntBool(clean),
			SlotChanges:   db.IntBool(slotChanges),
			BattleChanges: db.IntBool(battleChanges),
			GymID:         nil,
		})
	}

	tracked, err := ctx.Tracking.Gyms.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("gym command: select existing: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	// Gym uses a custom diff loop because we need to preserve gym_id from the
	// existing row (gym-specific tracking is set via scanner DB, not user commands).
	diff := store.DiffAndClassify(tracked, insert, store.GymGetUID, store.GymSetUID)

	// Preserve gym_id on updates
	for i := range diff.Updates {
		for _, existing := range tracked {
			if store.GymGetUID(&existing) == store.GymGetUID(&diff.Updates[i]) {
				diff.Updates[i].GymID = existing.GymID
				break
			}
		}
	}

	message := buildTrackingMessage(tr, ctx, len(diff.AlreadyPresent), len(diff.Updates), len(diff.Inserts),
		func(i int) string { return ctx.RowText.GymRowText(tr, gymAPIToTracking(&diff.AlreadyPresent[i])) },
		func(i int) string { return ctx.RowText.GymRowText(tr, gymAPIToTracking(&diff.Updates[i])) },
		func(i int) string { return ctx.RowText.GymRowText(tr, gymAPIToTracking(&diff.Inserts[i])) },
	)

	// Apply: delete updated UIDs, then insert new + updated
	if len(diff.Updates) > 0 {
		uids := make([]int64, len(diff.Updates))
		for i := range diff.Updates {
			uids[i] = store.GymGetUID(&diff.Updates[i])
		}
		if err := ctx.Tracking.Gyms.DeleteByUIDs(ctx.TargetID, uids); err != nil {
			log.Errorf("gym command: delete updated: %s", err)
			return []bot.Reply{{React: "🙅"}}
		}
	}
	for i := range diff.Inserts {
		if _, err := ctx.Tracking.Gyms.Insert(&diff.Inserts[i]); err != nil {
			log.Errorf("gym command: insert: %s", err)
			return []bot.Reply{{React: "🙅"}}
		}
	}
	for i := range diff.Updates {
		if _, err := ctx.Tracking.Gyms.Insert(&diff.Updates[i]); err != nil {
			log.Errorf("gym command: insert update: %s", err)
			return []bot.Reply{{React: "🙅"}}
		}
	}

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

func (c *GymCommand) removeGyms(ctx *bot.CommandContext, teams []int) []bot.Reply {
	tracked, err := ctx.Tracking.Gyms.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
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
	if err := ctx.Tracking.Gyms.DeleteByUIDs(ctx.TargetID, uids); err != nil {
		log.Errorf("gym command: delete: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}
	ctx.TriggerReload()
	tr := ctx.Tr()
	return []bot.Reply{{React: "✅", Text: tr.Tf("msg.removed_n", len(uids))}}
}
