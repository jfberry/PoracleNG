package commands

import (
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/api"
	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
)

// MaxbattleCommand implements !maxbattle — track max battles (Dynamax).
type MaxbattleCommand struct{}

func (c *MaxbattleCommand) Name() string      { return "cmd.maxbattle" }
func (c *MaxbattleCommand) Aliases() []string { return nil }

var maxbattleParams = []bot.ParamDef{
	{Type: bot.ParamPrefixRange, Key: "arg.prefix.level"},
	{Type: bot.ParamPrefixSingle, Key: "arg.prefix.d"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.template"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.form"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.move"},
	{Type: bot.ParamPrefixSingle, Key: "arg.prefix.gen"},
	{Type: bot.ParamKeyword, Key: "arg.remove"},
	{Type: bot.ParamKeyword, Key: "arg.everything"},
	{Type: bot.ParamKeyword, Key: "arg.clean"},
	{Type: bot.ParamKeyword, Key: "arg.gmax"},
	{Type: bot.ParamTypeName},
	{Type: bot.ParamPokemonName},
}

func (c *MaxbattleCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	// Extract @mention pings before parsing
	pings, args := extractPings(args)

	if usage := usageReply(ctx, args, "cmd.maxbattle.usage"); usage != nil {
		return []bot.Reply{*usage}
	}
	if help := helpArgReply(ctx, args, "cmd.maxbattle.usage"); help != nil {
		return []bot.Reply{*help}
	}

	parsed := ctx.ArgMatcher.Match(args, maxbattleParams, ctx.Language)

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

	gmax := 0
	if parsed.HasKeyword("arg.gmax") {
		gmax = 1
	}

	// Move filter — match by translated name
	move := 9000
	if moveName, ok := parsed.Strings["move"]; ok {
		if ctx.GameData != nil {
			tr := ctx.Tr()
			enTr := ctx.Translations.For("en")
			for id := range ctx.GameData.Moves {
				key := gamedata.MoveTranslationKey(id)
				if strings.EqualFold(tr.T(key), moveName) || strings.EqualFold(enTr.T(key), moveName) {
					move = id
					break
				}
			}
		}
	}

	// Handle remove
	if parsed.HasKeyword("arg.remove") {
		return c.removeMaxbattles(ctx, parsed)
	}

	var insert []db.MaxbattleTrackingAPI

	if len(parsed.Pokemon) > 0 {
		// Track specific pokemon
		monsterList := c.resolveMonsters(ctx, parsed)
		for _, mon := range monsterList {
			insert = append(insert, db.MaxbattleTrackingAPI{
				ID:        ctx.TargetID,
				ProfileNo: ctx.ProfileNo,
				Ping:      pings,
				Template:  template,
				Distance:  distance,
				Clean:     db.IntBool(clean),
				PokemonID: mon.PokemonID,
				Form:      mon.Form,
				Level:     90, // 90 = all levels for specific pokemon
				Move:      move,
				Gmax:      gmax,
				Evolution: 9000,
				StationID: nil,
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

		if parsed.HasKeyword("arg.everything") {
			levelSet[90] = true // 90 = all levels
		}

		if len(levelSet) == 0 {
			return []bot.Reply{{React: "🙅", Text: tr.T("cmd.no_levels")}}
		}

		for lvl := range levelSet {
			insert = append(insert, db.MaxbattleTrackingAPI{
				ID:        ctx.TargetID,
				ProfileNo: ctx.ProfileNo,
				Ping:      pings,
				Template:  template,
				Distance:  distance,
				Clean:     db.IntBool(clean),
				PokemonID: 9000, // 9000 = by level
				Level:     lvl,
				Move:      move,
				Gmax:      gmax,
				Evolution: 9000,
				StationID: nil,
			})
		}
	}

	if len(insert) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.no_levels")}}
	}

	// Diff against existing
	tracked, err := db.SelectMaxbattlesByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("maxbattle command: select existing: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	var updates, alreadyPresent []db.MaxbattleTrackingAPI
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
			return ctx.RowText.MaxbattleRowText(tr, maxbattleAPIToTracking(&alreadyPresent[i]))
		},
		func(i int) string {
			return ctx.RowText.MaxbattleRowText(tr, maxbattleAPIToTracking(&updates[i]))
		},
		func(i int) string {
			return ctx.RowText.MaxbattleRowText(tr, maxbattleAPIToTracking(&insert[i]))
		},
	)

	if len(updates) > 0 {
		uids := make([]int64, len(updates))
		for i, u := range updates {
			uids[i] = u.UID
		}
		if err := db.DeleteByUIDs(ctx.DB, "maxbattle", ctx.TargetID, uids); err != nil {
			log.Errorf("maxbattle command: delete updated: %s", err)
			return []bot.Reply{{React: "🙅"}}
		}
	}

	toInsert := append(insert, updates...)
	for i := range toInsert {
		if _, err := db.InsertMaxbattle(ctx.DB, &toInsert[i]); err != nil {
			log.Errorf("maxbattle command: insert: %s", err)
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

func (c *MaxbattleCommand) resolveMonsters(ctx *bot.CommandContext, parsed *bot.ParsedArgs) []bot.ResolvedPokemon {
	monsters := parsed.Pokemon

	// Form filtering
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

	// Generation filter
	if gen, ok := parsed.Singles["gen"]; ok && ctx.GameData != nil {
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
	if len(parsed.Types) > 0 && ctx.GameData != nil {
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

func (c *MaxbattleCommand) removeMaxbattles(ctx *bot.CommandContext, parsed *bot.ParsedArgs) []bot.Reply {
	tracked, err := db.SelectMaxbattlesByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("maxbattle command: select for remove: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	var uids []int64
	if parsed.HasKeyword("arg.everything") {
		for _, existing := range tracked {
			uids = append(uids, existing.UID)
		}
	} else {
		removeIDs := make(map[int]bool)
		for _, mon := range parsed.Pokemon {
			removeIDs[mon.PokemonID] = true
		}
		for _, existing := range tracked {
			if removeIDs[existing.PokemonID] {
				uids = append(uids, existing.UID)
			}
		}
	}

	if len(uids) == 0 {
		return []bot.Reply{{React: "👌"}}
	}
	if err := db.DeleteByUIDs(ctx.DB, "maxbattle", ctx.TargetID, uids); err != nil {
		log.Errorf("maxbattle command: delete: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}
	ctx.TriggerReload()
	tr := ctx.Tr()
	return []bot.Reply{{React: "✅", Text: tr.Tf("cmd.removed_n", len(uids))}}
}
