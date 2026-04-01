package commands

import (
	"github.com/guregu/null/v6"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/api"
	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
)

// RaidCommand implements !raid — track raid bosses or raid levels.
type RaidCommand struct{}

func (c *RaidCommand) Name() string      { return "cmd.raid" }
func (c *RaidCommand) Aliases() []string { return nil }

var raidParams = []bot.ParamDef{
	{Type: bot.ParamPrefixRange, Key: "arg.prefix.level"},
	{Type: bot.ParamPrefixSingle, Key: "arg.prefix.d"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.template"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.move"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.form"},
	{Type: bot.ParamPrefixSingle, Key: "arg.prefix.gen"},
	{Type: bot.ParamKeyword, Key: "arg.remove"},
	{Type: bot.ParamKeyword, Key: "arg.everything"},
	{Type: bot.ParamKeyword, Key: "arg.clean"},
	{Type: bot.ParamKeyword, Key: "arg.ex"},
	{Type: bot.ParamKeyword, Key: "arg.rsvp"},
	{Type: bot.ParamKeyword, Key: "arg.no_rsvp"},
	{Type: bot.ParamKeyword, Key: "arg.rsvp_only"},
	{Type: bot.ParamTeam},
	{Type: bot.ParamRaidLevelName},
	{Type: bot.ParamTypeName},
	{Type: bot.ParamPokemonName},
}

func (c *RaidCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	// Extract @mention pings before parsing
	pings, args := extractPings(args)

	if usage := usageReply(ctx, args, "cmd.raid.usage"); usage != nil {
		return []bot.Reply{*usage}
	}
	if help := helpArgReply(ctx, args, "cmd.raid.usage"); help != nil {
		return []bot.Reply{*help}
	}

	parsed := ctx.ArgMatcher.Match(args, raidParams, ctx.Language)

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
	exclusive := parsed.HasKeyword("arg.ex")
	team := parsed.Team

	rsvpChanges := 0
	if parsed.HasKeyword("arg.rsvp") {
		rsvpChanges = 1
	}
	if parsed.HasKeyword("arg.rsvp_only") {
		rsvpChanges = 2
	}
	if parsed.HasKeyword("arg.no_rsvp") {
		rsvpChanges = 0
	}

	// Move filter — match by translated name
	move := bot.WildcardID
	if moveName, ok := parsed.Strings["move"]; ok {
		move = resolveMoveByName(ctx, moveName)
	}

	// Handle remove
	if parsed.HasKeyword("arg.remove") {
		return c.removeRaids(ctx, parsed)
	}

	// Build insert list — either by pokemon name or by level
	var insert []db.RaidTrackingAPI

	if len(parsed.Pokemon) > 0 {
		// Track specific pokemon
		for _, mon := range parsed.Pokemon {
			insert = append(insert, db.RaidTrackingAPI{
				ID:          ctx.TargetID,
				ProfileNo:   ctx.ProfileNo,
				Ping:        pings,
				PokemonID:   mon.PokemonID,
				Form:        mon.Form,
				Level:       bot.WildcardID,
				Team:        team,
				Exclusive:   db.IntBool(exclusive),
				Move:        move,
				Evolution:   bot.WildcardID,
				GymID:       null.String{},
				Distance:    distance,
				Template:    template,
				Clean:       db.IntBool(clean),
				RSVPChanges: rsvpChanges,
			})
		}
	} else {
		// Track by level
		levelSet := make(map[int]bool)

		if r, ok := parsed.Ranges["level"]; ok {
			if r.HasMax {
				for lvl := r.Min; lvl <= r.Max; lvl++ {
					levelSet[lvl] = true
				}
			} else {
				levelSet[r.Min] = true
			}
		}

		for _, lvl := range parsed.RaidLevels {
			levelSet[lvl] = true
		}

		if parsed.HasKeyword("arg.everything") {
			if ctx.GameData != nil && ctx.GameData.Util != nil {
				for lvl := range ctx.GameData.Util.RaidLevels {
					levelSet[lvl] = true
				}
			}
		}

		if len(levelSet) == 0 {
			return []bot.Reply{{React: "🙅", Text: tr.T("cmd.no_raid_target")}}
		}

		for lvl := range levelSet {
			insert = append(insert, db.RaidTrackingAPI{
				ID:          ctx.TargetID,
				ProfileNo:   ctx.ProfileNo,
				Ping:        pings,
				PokemonID:   bot.WildcardID,
				Level:       lvl,
				Team:        team,
				Exclusive:   db.IntBool(exclusive),
				Move:        move,
				Evolution:   bot.WildcardID,
				GymID:       null.String{},
				Distance:    distance,
				Template:    template,
				Clean:       db.IntBool(clean),
				RSVPChanges: rsvpChanges,
			})
		}
	}

	// Diff against existing
	tracked, err := db.SelectRaidsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("raid command: select existing: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	var updates []db.RaidTrackingAPI
	var alreadyPresent []db.RaidTrackingAPI

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
				update := insert[i]
				update.UID = uid
				updates = append(updates, update)
				insert = append(insert[:i], insert[i+1:]...)
				break
			}
		}
	}

	message := buildTrackingMessage(tr, ctx, len(alreadyPresent), len(updates), len(insert),
		func(i int) string { return ctx.RowText.RaidRowText(tr, raidAPIToTracking(&alreadyPresent[i])) },
		func(i int) string { return ctx.RowText.RaidRowText(tr, raidAPIToTracking(&updates[i])) },
		func(i int) string { return ctx.RowText.RaidRowText(tr, raidAPIToTracking(&insert[i])) },
	)

	// Apply
	if len(updates) > 0 {
		uids := make([]int64, len(updates))
		for i, u := range updates {
			uids[i] = u.UID
		}
		if err := db.DeleteByUIDs(ctx.DB, "raid", ctx.TargetID, uids); err != nil {
			log.Errorf("raid command: delete updated: %s", err)
			return []bot.Reply{{React: "🙅"}}
		}
	}

	toInsert := append(insert, updates...)
	for i := range toInsert {
		if _, err := db.InsertRaid(ctx.DB, &toInsert[i]); err != nil {
			log.Errorf("raid command: insert: %s", err)
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

func (c *RaidCommand) removeRaids(ctx *bot.CommandContext, parsed *bot.ParsedArgs) []bot.Reply {
	tracked, err := db.SelectRaidsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("raid command: select for remove: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	var uidsToDelete []int64
	for _, existing := range tracked {
		shouldRemove := false
		// Remove by pokemon
		for _, mon := range parsed.Pokemon {
			if existing.PokemonID == mon.PokemonID {
				shouldRemove = true
				break
			}
		}
		// Remove by level
		for _, lvl := range parsed.RaidLevels {
			if existing.Level == lvl {
				shouldRemove = true
				break
			}
		}
		if r, ok := parsed.Ranges["level"]; ok {
			if r.HasMax {
				if existing.Level >= r.Min && existing.Level <= r.Max {
					shouldRemove = true
				}
			} else if existing.Level == r.Min {
				shouldRemove = true
			}
		}
		if parsed.HasKeyword("arg.everything") {
			shouldRemove = true
		}
		if shouldRemove {
			uidsToDelete = append(uidsToDelete, existing.UID)
		}
	}

	if len(uidsToDelete) == 0 {
		return []bot.Reply{{React: "👌"}}
	}

	if err := db.DeleteByUIDs(ctx.DB, "raid", ctx.TargetID, uidsToDelete); err != nil {
		log.Errorf("raid command: delete: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	ctx.TriggerReload()
	tr := ctx.Tr()
	return []bot.Reply{{React: "✅", Text: tr.Tf("cmd.removed_n", len(uidsToDelete))}}
}
