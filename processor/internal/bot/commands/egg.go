package commands

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/guregu/null/v6"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/api"
	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
)

// EggCommand implements !egg — track raid eggs by level.
type EggCommand struct{}

func (c *EggCommand) Name() string      { return "cmd.egg" }
func (c *EggCommand) Aliases() []string { return nil }

// eggParams declares the parameter types !egg accepts.
var eggParams = []bot.ParamDef{
	{Type: bot.ParamPrefixRange, Key: "arg.prefix.level"},
	{Type: bot.ParamPrefixSingle, Key: "arg.prefix.d"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.template"},
	{Type: bot.ParamKeyword, Key: "arg.remove"},
	{Type: bot.ParamKeyword, Key: "arg.everything"},
	{Type: bot.ParamKeyword, Key: "arg.clean"},
	{Type: bot.ParamKeyword, Key: "arg.ex"},
	{Type: bot.ParamKeyword, Key: "arg.rsvp"},
	{Type: bot.ParamKeyword, Key: "arg.no_rsvp"},
	{Type: bot.ParamKeyword, Key: "arg.rsvp_only"},
	{Type: bot.ParamTeam},
	{Type: bot.ParamRaidLevelName},
}

func (c *EggCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	parsed := ctx.ArgMatcher.Match(args, eggParams, ctx.Language)

	// Collect levels from multiple sources
	levelSet := make(map[int]bool)

	// From level<N> or level<N>-<M> prefix
	if r, ok := parsed.Ranges["level"]; ok {
		if r.HasMax {
			for lvl := r.Min; lvl <= r.Max; lvl++ {
				levelSet[lvl] = true
			}
		} else {
			levelSet[r.Min] = true
		}
	}

	// From raid level names (legendary, mega, shadow, etc.)
	for _, lvl := range parsed.RaidLevels {
		levelSet[lvl] = true
	}

	// "everything" → all levels from game data
	if parsed.HasKeyword("arg.everything") {
		if ctx.GameData != nil && ctx.GameData.Util != nil {
			for lvl := range ctx.GameData.Util.RaidLevels {
				levelSet[lvl] = true
			}
		}
	}

	if len(levelSet) == 0 {
		if warn := bot.ReportUnrecognized(parsed, tr); warn != nil {
			return []bot.Reply{*warn}
		}
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.no_egg_levels")}}
	}

	// Parse common fields
	distance := 0
	if d, ok := parsed.Singles["d"]; ok {
		distance = d
	}

	template := ctx.DefaultTemplate()
	if t, ok := parsed.Strings["template"]; ok {
		template = t
	}

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

	// Check for unrecognized args
	if warn := bot.ReportUnrecognized(parsed, tr); warn != nil {
		return []bot.Reply{*warn}
	}

	// Handle remove
	if parsed.HasKeyword("arg.remove") {
		return c.removeEggs(ctx, levelSet)
	}

	// Build insert structs
	levels := make([]int, 0, len(levelSet))
	for lvl := range levelSet {
		levels = append(levels, lvl)
	}

	insert := make([]db.EggTrackingAPI, 0, len(levels))
	for _, lvl := range levels {
		insert = append(insert, db.EggTrackingAPI{
			ID:          ctx.TargetID,
			ProfileNo:   ctx.ProfileNo,
			Ping:        "",
			Template:    template,
			Distance:    distance,
			Team:        team,
			Clean:       db.IntBool(clean),
			Exclusive:   db.IntBool(exclusive),
			GymID:       null.String{},
			RSVPChanges: rsvpChanges,
			Level:       lvl,
		})
	}

	// Diff against existing
	tracked, err := db.SelectEggsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("egg command: select existing: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	var updates []db.EggTrackingAPI
	var alreadyPresent []db.EggTrackingAPI

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

	// Build response message
	totalChanges := len(alreadyPresent) + len(updates) + len(insert)
	var message string
	if totalChanges > 20 {
		message = tr.Tf("tracking.bulk_changes",
			ctx.Config.Discord.Prefix, tr.T("tracking.tracked"))
	} else {
		var sb strings.Builder
		for i := range alreadyPresent {
			et := eggAPIToTracking(&alreadyPresent[i])
			sb.WriteString(tr.T("tracking.unchanged"))
			sb.WriteString(ctx.RowText.EggRowText(tr, et))
			sb.WriteByte('\n')
		}
		for i := range updates {
			et := eggAPIToTracking(&updates[i])
			sb.WriteString(tr.T("tracking.updated"))
			sb.WriteString(ctx.RowText.EggRowText(tr, et))
			sb.WriteByte('\n')
		}
		for i := range insert {
			et := eggAPIToTracking(&insert[i])
			sb.WriteString(tr.T("tracking.new"))
			sb.WriteString(ctx.RowText.EggRowText(tr, et))
			sb.WriteByte('\n')
		}
		message = sb.String()
	}

	// Apply changes to DB
	if len(updates) > 0 {
		uids := make([]int64, len(updates))
		for i, u := range updates {
			uids[i] = u.UID
		}
		if err := db.DeleteByUIDs(ctx.DB, "egg", ctx.TargetID, uids); err != nil {
			log.Errorf("egg command: delete updated: %s", err)
			return []bot.Reply{{React: "🙅"}}
		}
	}

	toInsert := make([]db.EggTrackingAPI, 0, len(insert)+len(updates))
	toInsert = append(toInsert, insert...)
	toInsert = append(toInsert, updates...)

	for i := range toInsert {
		if _, err := db.InsertEgg(ctx.DB, &toInsert[i]); err != nil {
			log.Errorf("egg command: insert: %s", err)
			return []bot.Reply{{React: "🙅"}}
		}
	}

	ctx.TriggerReload()

	if len(insert) == 0 && len(updates) == 0 {
		return []bot.Reply{{React: "👌", Text: message}}
	}
	return []bot.Reply{{React: "✅", Text: message}}
}

func (c *EggCommand) removeEggs(ctx *bot.CommandContext, levelSet map[int]bool) []bot.Reply {
	tr := ctx.Tr()
	tracked, err := db.SelectEggsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("egg command: select for remove: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	var uidsToDelete []int64
	var removed []db.EggTrackingAPI
	for _, existing := range tracked {
		if levelSet[existing.Level] {
			uidsToDelete = append(uidsToDelete, existing.UID)
			removed = append(removed, existing)
		}
	}

	if len(uidsToDelete) == 0 {
		return []bot.Reply{{React: "👌"}}
	}

	if err := db.DeleteByUIDs(ctx.DB, "egg", ctx.TargetID, uidsToDelete); err != nil {
		log.Errorf("egg command: delete: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	ctx.TriggerReload()

	var sb strings.Builder
	for i := range removed {
		et := eggAPIToTracking(&removed[i])
		fmt.Fprintf(&sb, "Removed: %s\n", ctx.RowText.EggRowText(tr, et))
	}
	return []bot.Reply{{React: "✅", Text: sb.String()}}
}

func eggAPIToTracking(api *db.EggTrackingAPI) *db.EggTracking {
	return &db.EggTracking{
		ID:          api.ID,
		ProfileNo:   api.ProfileNo,
		Ping:        api.Ping,
		Clean:       bool(api.Clean),
		Distance:    api.Distance,
		Template:    api.Template,
		Team:        api.Team,
		Level:       api.Level,
		Exclusive:   bool(api.Exclusive),
		GymID:       sql.NullString{String: api.GymID.String, Valid: api.GymID.Valid},
		RSVPChanges: api.RSVPChanges,
	}
}
