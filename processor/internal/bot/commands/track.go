package commands

import (
	"fmt"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/api"
	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
)

// TrackCommand implements !track — pokemon tracking with IV, PVP, type, gen filters.
type TrackCommand struct{}

func (c *TrackCommand) Name() string      { return "cmd.track" }
func (c *TrackCommand) Aliases() []string { return nil }

func (c *TrackCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	if usage := usageReply(ctx, args, "cmd.track.usage"); usage != nil {
		return []bot.Reply{*usage}
	}
	if help := helpArgReply(ctx, args, "cmd.track.usage"); help != nil {
		return []bot.Reply{*help}
	}

	// Build params list — some are conditional on permissions
	params := trackParams(ctx)

	parsed := ctx.ArgMatcher.Match(args, params, ctx.Language)

	// Check for unrecognized args
	if warn := bot.ReportUnrecognized(parsed, tr); warn != nil {
		return []bot.Reply{*warn}
	}

	// Handle remove
	if parsed.HasKeyword("arg.remove") {
		return c.removeTracking(ctx, parsed)
	}

	// Resolve pokemon list
	monsterList := c.resolveMonsters(ctx, parsed)

	// Parse filter values with defaults
	filters := c.parseFilters(ctx, parsed)

	// Resolve PVP
	pvpLeague, pvpBest, pvpWorst, pvpMinCP, pvpCap := c.parsePVP(parsed)

	// Check PVP permission if PVP filters are present
	if pvpLeague > 0 {
		if !bot.CheckFeaturePermission(ctx.Config, ctx.Platform, "pvp", ctx.UserID, nil) {
			return []bot.Reply{{React: "🙅", Text: tr.T("cmd.no_permission")}}
		}
	}

	// If min_iv is still default (-1) but other IV-related filters are set, default to 0
	if filters.minIV == -1 && (filters.minCP > 0 || filters.minLevel > 0 ||
		filters.atk > 0 || filters.def > 0 || filters.sta > 0 ||
		pvpLeague > 0) {
		filters.minIV = 0
	}

	// Build insert structs
	insert := make([]db.MonsterTrackingAPI, 0, len(monsterList))
	for _, mon := range monsterList {
		insert = append(insert, db.MonsterTrackingAPI{
			ID:               ctx.TargetID,
			ProfileNo:        ctx.ProfileNo,
			PokemonID:        mon.PokemonID,
			Form:             mon.Form,
			Ping:             "",
			Distance:         filters.distance,
			MinIV:            filters.minIV,
			MaxIV:            filters.maxIV,
			MinCP:            filters.minCP,
			MaxCP:            filters.maxCP,
			MinLevel:         filters.minLevel,
			MaxLevel:         filters.maxLevel,
			ATK:              filters.atk,
			DEF:              filters.def,
			STA:              filters.sta,
			MaxATK:           filters.maxAtk,
			MaxDEF:           filters.maxDef,
			MaxSTA:           filters.maxSta,
			Gender:           filters.gender,
			MinWeight:        filters.minWeight,
			MaxWeight:        filters.maxWeight,
			MinTime:          filters.minTime,
			Rarity:           filters.rarity,
			MaxRarity:        filters.maxRarity,
			Size:             filters.size,
			MaxSize:          filters.maxSize,
			Template:         filters.template,
			Clean:            db.IntBool(filters.clean),
			PVPRankingLeague: pvpLeague,
			PVPRankingBest:   pvpBest,
			PVPRankingWorst:  pvpWorst,
			PVPRankingMinCP:  pvpMinCP,
			PVPRankingCap:    pvpCap,
		})
	}

	if len(insert) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.no_pokemon")}}
	}

	// Diff against existing
	tracked, err := db.SelectMonstersByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("track command: select existing: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	var updates []db.MonsterTrackingAPI
	var alreadyPresent []db.MonsterTrackingAPI

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

	// Build response
	totalChanges := len(alreadyPresent) + len(updates) + len(insert)
	var message string
	if totalChanges > 20 {
		message = tr.Tf("tracking.bulk_changes",
			ctx.Config.Discord.Prefix, tr.T("tracking.tracked"))
	} else {
		var sb strings.Builder
		for i := range alreadyPresent {
			mt := monsterAPIToTracking(&alreadyPresent[i])
			sb.WriteString(tr.T("tracking.unchanged"))
			sb.WriteString(ctx.RowText.MonsterRowText(tr, mt))
			sb.WriteByte('\n')
		}
		for i := range updates {
			mt := monsterAPIToTracking(&updates[i])
			sb.WriteString(tr.T("tracking.updated"))
			sb.WriteString(ctx.RowText.MonsterRowText(tr, mt))
			sb.WriteByte('\n')
		}
		for i := range insert {
			mt := monsterAPIToTracking(&insert[i])
			sb.WriteString(tr.T("tracking.new"))
			sb.WriteString(ctx.RowText.MonsterRowText(tr, mt))
			sb.WriteByte('\n')
		}
		message = sb.String()
	}

	// Apply changes
	if len(updates) > 0 {
		uids := make([]int64, len(updates))
		for i, u := range updates {
			uids[i] = u.UID
		}
		if err := db.DeleteByUIDs(ctx.DB, "monsters", ctx.TargetID, uids); err != nil {
			log.Errorf("track command: delete updated: %s", err)
			return []bot.Reply{{React: "🙅"}}
		}
	}

	toInsert := make([]db.MonsterTrackingAPI, 0, len(insert)+len(updates))
	toInsert = append(toInsert, insert...)
	toInsert = append(toInsert, updates...)

	for i := range toInsert {
		if _, err := db.InsertMonster(ctx.DB, &toInsert[i]); err != nil {
			log.Errorf("track command: insert: %s", err)
			return []bot.Reply{{React: "🙅"}}
		}
	}

	ctx.TriggerReload()

	if len(insert) == 0 && len(updates) == 0 {
		return []bot.Reply{{React: "👌", Text: message}}
	}
	return []bot.Reply{{React: "✅", Text: message}}
}

// trackParams builds the parameter list, conditionally including everything/individually.
func trackParams(ctx *bot.CommandContext) []bot.ParamDef {
	params := []bot.ParamDef{
		{Type: bot.ParamPrefixRange, Key: "arg.prefix.iv"},
		{Type: bot.ParamPrefixSingle, Key: "arg.prefix.miniv"},
		{Type: bot.ParamPrefixSingle, Key: "arg.prefix.maxiv"},
		{Type: bot.ParamPrefixRange, Key: "arg.prefix.cp"},
		{Type: bot.ParamPrefixSingle, Key: "arg.prefix.mincp"},
		{Type: bot.ParamPrefixSingle, Key: "arg.prefix.maxcp"},
		{Type: bot.ParamPrefixRange, Key: "arg.prefix.level"},
		{Type: bot.ParamPrefixSingle, Key: "arg.prefix.maxlevel"},
		{Type: bot.ParamPrefixRange, Key: "arg.prefix.atk"},
		{Type: bot.ParamPrefixSingle, Key: "arg.prefix.maxatk"},
		{Type: bot.ParamPrefixRange, Key: "arg.prefix.def"},
		{Type: bot.ParamPrefixSingle, Key: "arg.prefix.maxdef"},
		{Type: bot.ParamPrefixRange, Key: "arg.prefix.sta"},
		{Type: bot.ParamPrefixSingle, Key: "arg.prefix.maxsta"},
		{Type: bot.ParamPrefixRange, Key: "arg.prefix.weight"},
		{Type: bot.ParamPrefixSingle, Key: "arg.prefix.maxweight"},
		{Type: bot.ParamPrefixRange, Key: "arg.prefix.rarity"},
		{Type: bot.ParamPrefixSingle, Key: "arg.prefix.maxrarity"},
		{Type: bot.ParamPrefixRange, Key: "arg.prefix.size"},
		{Type: bot.ParamPrefixSingle, Key: "arg.prefix.maxsize"},
		{Type: bot.ParamPrefixSingle, Key: "arg.prefix.d"},
		{Type: bot.ParamPrefixSingle, Key: "arg.prefix.t"},
		{Type: bot.ParamPrefixSingle, Key: "arg.prefix.gen"},
		{Type: bot.ParamPrefixSingle, Key: "arg.prefix.cap"},
		{Type: bot.ParamPrefixString, Key: "arg.prefix.form"},
		{Type: bot.ParamPrefixString, Key: "arg.prefix.template"},
		{Type: bot.ParamKeyword, Key: "arg.remove"},
		{Type: bot.ParamKeyword, Key: "arg.clean"},
		{Type: bot.ParamKeyword, Key: "arg.shiny"},
		{Type: bot.ParamGender},
		{Type: bot.ParamTypeName},
		{Type: bot.ParamPokemonName},
		// PVP leagues
		{Type: bot.ParamPVPLeague, Key: "arg.prefix.great"},
		{Type: bot.ParamPVPLeague, Key: "arg.prefix.greathigh"},
		{Type: bot.ParamPVPLeague, Key: "arg.prefix.greatcp"},
		{Type: bot.ParamPVPLeague, Key: "arg.prefix.ultra"},
		{Type: bot.ParamPVPLeague, Key: "arg.prefix.ultrahigh"},
		{Type: bot.ParamPVPLeague, Key: "arg.prefix.ultracp"},
		{Type: bot.ParamPVPLeague, Key: "arg.prefix.little"},
		{Type: bot.ParamPVPLeague, Key: "arg.prefix.littlehigh"},
		{Type: bot.ParamPVPLeague, Key: "arg.prefix.littlecp"},
	}

	// Conditional params based on permissions
	everythingPerms := strings.ToLower(ctx.Config.Tracking.EverythingFlagPermissions)
	if everythingPerms != "deny" || ctx.IsAdmin {
		params = append(params, bot.ParamDef{Type: bot.ParamKeyword, Key: "arg.everything"})
	}
	if everythingPerms != "allow-and-ignore-individually" || ctx.IsAdmin {
		params = append(params, bot.ParamDef{Type: bot.ParamKeyword, Key: "arg.individually"})
	}

	return params
}

type trackFilters struct {
	distance  int
	minIV     int
	maxIV     int
	minCP     int
	maxCP     int
	minLevel  int
	maxLevel  int
	atk       int
	def       int
	sta       int
	maxAtk    int
	maxDef    int
	maxSta    int
	gender    int
	minWeight int
	maxWeight int
	minTime   int
	rarity    int
	maxRarity int
	size      int
	maxSize   int
	template  string
	clean     bool
}

func (c *TrackCommand) parseFilters(ctx *bot.CommandContext, parsed *bot.ParsedArgs) trackFilters {
	f := trackFilters{
		minIV:     -1,
		maxIV:     100,
		maxCP:     9000,
		maxLevel:  55,
		maxAtk:    15,
		maxDef:    15,
		maxSta:    15,
		maxWeight: 9000000,
		rarity:    -1,
		maxRarity: 6,
		size:      -1,
		maxSize:   5,
		template:  ctx.DefaultTemplate(),
	}

	// Distance
	if d, ok := parsed.Singles["d"]; ok {
		f.distance = d
	}
	f.distance = enforceDistance(ctx, f.distance)

	// Template
	if t, ok := parsed.Strings["template"]; ok {
		f.template = t
	}

	// Time
	if t, ok := parsed.Singles["t"]; ok {
		f.minTime = t
	}

	// Clean
	f.clean = parsed.HasKeyword("arg.clean")

	// Gender
	f.gender = parsed.Gender

	// IV — iv50 means min=50 (max stays 100), iv50-80 means min=50 max=80
	if r, ok := parsed.Ranges["iv"]; ok {
		f.minIV = r.Min
		if r.HasMax {
			f.maxIV = r.Max
		}
	}
	if v, ok := parsed.Singles["miniv"]; ok {
		f.minIV = v
	}
	if v, ok := parsed.Singles["maxiv"]; ok {
		f.maxIV = v
	}

	// CP
	if r, ok := parsed.Ranges["cp"]; ok {
		f.minCP = r.Min
		if r.HasMax {
			f.maxCP = r.Max
		}
	}
	if v, ok := parsed.Singles["mincp"]; ok {
		f.minCP = v
	}
	if v, ok := parsed.Singles["maxcp"]; ok {
		f.maxCP = v
	}

	// Level
	if r, ok := parsed.Ranges["level"]; ok {
		f.minLevel = r.Min
		if r.HasMax {
			f.maxLevel = r.Max
		}
	}
	if v, ok := parsed.Singles["maxlevel"]; ok {
		f.maxLevel = v
	}

	// Stats
	if r, ok := parsed.Ranges["atk"]; ok {
		f.atk = r.Min
		if r.HasMax {
			f.maxAtk = r.Max
		}
	}
	if v, ok := parsed.Singles["maxatk"]; ok {
		f.maxAtk = v
	}
	if r, ok := parsed.Ranges["def"]; ok {
		f.def = r.Min
		if r.HasMax {
			f.maxDef = r.Max
		}
	}
	if v, ok := parsed.Singles["maxdef"]; ok {
		f.maxDef = v
	}
	if r, ok := parsed.Ranges["sta"]; ok {
		f.sta = r.Min
		if r.HasMax {
			f.maxSta = r.Max
		}
	}
	if v, ok := parsed.Singles["maxsta"]; ok {
		f.maxSta = v
	}

	// Weight
	if r, ok := parsed.Ranges["weight"]; ok {
		f.minWeight = r.Min
		if r.HasMax {
			f.maxWeight = r.Max
		}
	}
	if v, ok := parsed.Singles["maxweight"]; ok {
		f.maxWeight = v
	}

	// Rarity
	if r, ok := parsed.Ranges["rarity"]; ok {
		f.rarity = r.Min
		if r.HasMax {
			f.maxRarity = r.Max
		}
	}
	if v, ok := parsed.Singles["maxrarity"]; ok {
		f.maxRarity = v
	}

	// Size
	if r, ok := parsed.Ranges["size"]; ok {
		f.size = r.Min
		if r.HasMax {
			f.maxSize = r.Max
		}
	}
	if v, ok := parsed.Singles["maxsize"]; ok {
		f.maxSize = v
	}

	// Generation filter
	if gen, ok := parsed.Singles["gen"]; ok {
		_ = gen // gen filtering happens in resolveMonsters
	}

	return f
}

func (c *TrackCommand) parsePVP(parsed *bot.ParsedArgs) (league, best, worst, minCP, cap int) {
	best = 1
	worst = 4096

	// Check each league in priority order
	for _, l := range []struct {
		name string
		cp   int
	}{
		{"great", 1500},
		{"ultra", 2500},
		{"little", 500},
	} {
		if f, ok := parsed.PVP[l.name]; ok {
			league = l.cp
			best = f.Best
			worst = f.Worst
			minCP = f.MinCP
			break
		}
	}

	if v, ok := parsed.Singles["cap"]; ok && league > 0 {
		cap = v
	}

	return
}

func (c *TrackCommand) resolveMonsters(ctx *bot.CommandContext, parsed *bot.ParsedArgs) []bot.ResolvedPokemon {
	// "everything" keyword
	if parsed.HasKeyword("arg.everything") {
		forceIndividual := parsed.HasKeyword("arg.individually") ||
			len(parsed.Types) > 0 ||
			parsed.Singles["gen"] > 0 ||
			strings.ToLower(ctx.Config.Tracking.EverythingFlagPermissions) == "allow-and-always-individually"

		if forceIndividual && ctx.GameData != nil {
			// Expand to all base forms
			var monsters []bot.ResolvedPokemon
			for key := range ctx.GameData.Monsters {
				if key.Form == 0 {
					monsters = append(monsters, bot.ResolvedPokemon{PokemonID: key.ID, Form: 0})
				}
			}
			return c.filterByGenAndType(ctx, monsters, parsed)
		}
		// Single catch-all entry
		return []bot.ResolvedPokemon{{PokemonID: 0, Form: 0}}
	}

	monsters := parsed.Pokemon

	// Form filtering — match form name via translation keys (form_{formId})
	if formName, ok := parsed.Strings["form"]; ok && ctx.GameData != nil {
		tr := ctx.Tr()
		enTr := ctx.Translations.For("en")
		var filtered []bot.ResolvedPokemon
		for _, mon := range monsters {
			for key := range ctx.GameData.Monsters {
				if key.ID != mon.PokemonID || key.Form == 0 {
					continue
				}
				formKey := gamedata.FormTranslationKey(key.Form)
				translatedForm := strings.ToLower(tr.T(formKey))
				enForm := strings.ToLower(enTr.T(formKey))
				if translatedForm == formName || enForm == formName {
					filtered = append(filtered, bot.ResolvedPokemon{PokemonID: key.ID, Form: key.Form})
				}
			}
		}
		if len(filtered) > 0 {
			monsters = filtered
		}
	}

	return c.filterByGenAndType(ctx, monsters, parsed)
}

func (c *TrackCommand) filterByGenAndType(ctx *bot.CommandContext, monsters []bot.ResolvedPokemon, parsed *bot.ParsedArgs) []bot.ResolvedPokemon {
	if ctx.GameData == nil {
		return monsters
	}

	// Generation filter
	if gen, ok := parsed.Singles["gen"]; ok {
		genInfo := ctx.GameData.Util.GenData[gen]
		if genInfo.Min > 0 && genInfo.Max > 0 {
			var filtered []bot.ResolvedPokemon
			for _, mon := range monsters {
				if mon.PokemonID >= genInfo.Min && mon.PokemonID <= genInfo.Max {
					filtered = append(filtered, mon)
				}
			}
			monsters = filtered
		}
	}

	// Type filter
	if len(parsed.Types) > 0 {
		typeSet := make(map[int]bool)
		for _, t := range parsed.Types {
			typeSet[t] = true
		}
		var filtered []bot.ResolvedPokemon
		for _, mon := range monsters {
			m := ctx.GameData.Monsters[gamedata.MonsterKey{ID: mon.PokemonID, Form: mon.Form}]
			if m == nil {
				m = ctx.GameData.Monsters[gamedata.MonsterKey{ID: mon.PokemonID, Form: 0}]
			}
			if m == nil {
				continue
			}
			for _, t := range m.Types {
				if typeSet[t] {
					filtered = append(filtered, mon)
					break
				}
			}
		}
		monsters = filtered
	}

	return monsters
}

func (c *TrackCommand) removeTracking(ctx *bot.CommandContext, parsed *bot.ParsedArgs) []bot.Reply {
	tr := ctx.Tr()
	tracked, err := db.SelectMonstersByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("track command: select for remove: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	// Build set of pokemon IDs to remove
	removeIDs := make(map[int]bool)
	for _, mon := range parsed.Pokemon {
		removeIDs[mon.PokemonID] = true
	}

	// If everything keyword, remove all
	if parsed.HasKeyword("arg.everything") {
		for _, existing := range tracked {
			removeIDs[existing.PokemonID] = true
		}
	}

	var uidsToDelete []int64
	var removed []db.MonsterTrackingAPI
	for _, existing := range tracked {
		if removeIDs[existing.PokemonID] {
			uidsToDelete = append(uidsToDelete, existing.UID)
			removed = append(removed, existing)
		}
	}

	if len(uidsToDelete) == 0 {
		return []bot.Reply{{React: "👌"}}
	}

	if err := db.DeleteByUIDs(ctx.DB, "monsters", ctx.TargetID, uidsToDelete); err != nil {
		log.Errorf("track command: delete: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	ctx.TriggerReload()

	var sb strings.Builder
	if len(removed) > 20 {
		sb.WriteString(tr.Tf("cmd.removed_n", len(removed)))
	} else {
		for i := range removed {
			mt := monsterAPIToTracking(&removed[i])
			fmt.Fprintf(&sb, "Removed: %s\n", ctx.RowText.MonsterRowText(tr, mt))
		}
	}
	return []bot.Reply{{React: "✅", Text: sb.String()}}
}

func monsterAPIToTracking(a *db.MonsterTrackingAPI) *db.MonsterTracking {
	return &db.MonsterTracking{
		ID:               a.ID,
		ProfileNo:        a.ProfileNo,
		PokemonID:        a.PokemonID,
		Form:             a.Form,
		Distance:         a.Distance,
		MinIV:            a.MinIV,
		MaxIV:            a.MaxIV,
		MinCP:            a.MinCP,
		MaxCP:            a.MaxCP,
		MinLevel:         a.MinLevel,
		MaxLevel:         a.MaxLevel,
		ATK:              a.ATK,
		DEF:              a.DEF,
		STA:              a.STA,
		MaxATK:           a.MaxATK,
		MaxDEF:           a.MaxDEF,
		MaxSTA:           a.MaxSTA,
		Gender:           a.Gender,
		MinWeight:        a.MinWeight,
		MaxWeight:        a.MaxWeight,
		MinTime:          a.MinTime,
		Rarity:           a.Rarity,
		MaxRarity:        a.MaxRarity,
		Size:             a.Size,
		MaxSize:          a.MaxSize,
		Template:         a.Template,
		Clean:            bool(a.Clean),
		Ping:             a.Ping,
		PVPRankingLeague: a.PVPRankingLeague,
		PVPRankingBest:   a.PVPRankingBest,
		PVPRankingWorst:  a.PVPRankingWorst,
		PVPRankingMinCP:  a.PVPRankingMinCP,
		PVPRankingCap:    a.PVPRankingCap,
	}
}
