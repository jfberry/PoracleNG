package commands

import (
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// NestCommand implements !nest — track nesting pokemon.
type NestCommand struct{}

func (c *NestCommand) Name() string      { return "cmd.nest" }
func (c *NestCommand) Aliases() []string { return nil }

var nestParams = []bot.ParamDef{
	{Type: bot.ParamPrefixSingle, Key: "arg.prefix.d"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.template"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.form"},
	{Type: bot.ParamPrefixSingle, Key: "arg.prefix.gen"},
	{Type: bot.ParamPrefixSingle, Key: "arg.prefix.minspawn"},
	{Type: bot.ParamKeyword, Key: "arg.remove"},
	{Type: bot.ParamKeyword, Key: "arg.everything"},
	{Type: bot.ParamKeyword, Key: "arg.clean"},
	{Type: bot.ParamTypeName},
	{Type: bot.ParamPokemonName},
}

func (c *NestCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	// Extract @mention pings before parsing
	pings, args := extractPings(args)

	if usage := usageReply(ctx, args, "cmd.nest.usage"); usage != nil {
		return []bot.Reply{*usage}
	}
	if help := helpArgReply(ctx, args, "cmd.nest.usage"); help != nil {
		return []bot.Reply{*help}
	}

	parsed := ctx.ArgMatcher.Match(args, nestParams, ctx.Language)

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
		if block, warn := validateTemplate(ctx, "nest", template); block != nil {
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
	minSpawnAvg := 0
	if ms, ok := parsed.Singles["minspawn"]; ok {
		minSpawnAvg = ms
	}

	// Resolve pokemon list
	monsterList := c.resolveMonsters(ctx, parsed)

	if len(monsterList) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.no_pokemon")}}
	}

	if parsed.HasKeyword("arg.remove") {
		return c.removeNests(ctx, monsterList)
	}

	insert := make([]db.NestTrackingAPI, 0, len(monsterList))
	for _, mon := range monsterList {
		insert = append(insert, db.NestTrackingAPI{
			ID:          ctx.TargetID,
			ProfileNo:   ctx.ProfileNo,
			Ping:        pings,
			Template:    template,
			Distance:    distance,
			Clean:       db.IntBool(clean),
			PokemonID:   mon.PokemonID,
			Form:        mon.Form,
			MinSpawnAvg: minSpawnAvg,
		})
	}

	tracked, err := ctx.Tracking.Nests.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("nest command: select existing: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	diff, err := store.ApplyDiff(ctx.Tracking.Nests, ctx.TargetID, tracked, insert,
		store.NestGetUID, store.NestSetUID)
	if err != nil {
		log.Errorf("nest command: apply diff: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	message := buildTrackingMessage(tr, ctx, len(diff.AlreadyPresent), len(diff.Updates), len(diff.Inserts),
		func(i int) string { return ctx.RowText.NestRowText(tr, nestAPIToTracking(&diff.AlreadyPresent[i])) },
		func(i int) string { return ctx.RowText.NestRowText(tr, nestAPIToTracking(&diff.Updates[i])) },
		func(i int) string { return ctx.RowText.NestRowText(tr, nestAPIToTracking(&diff.Inserts[i])) },
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

func (c *NestCommand) resolveMonsters(ctx *bot.CommandContext, parsed *bot.ParsedArgs) []bot.ResolvedPokemon {
	if parsed.HasKeyword("arg.everything") {
		return []bot.ResolvedPokemon{{PokemonID: 0, Form: 0}}
	}

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

func (c *NestCommand) removeNests(ctx *bot.CommandContext, monsterList []bot.ResolvedPokemon) []bot.Reply {
	tracked, err := ctx.Tracking.Nests.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("nest command: select for remove: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	removeIDs := make(map[int]bool)
	for _, mon := range monsterList {
		removeIDs[mon.PokemonID] = true
	}

	var uids []int64
	for _, existing := range tracked {
		// PokemonID 0 means everything — remove all
		if removeIDs[0] || removeIDs[existing.PokemonID] {
			uids = append(uids, existing.UID)
		}
	}
	if len(uids) == 0 {
		return []bot.Reply{{React: "👌"}}
	}
	if err := ctx.Tracking.Nests.DeleteByUIDs(ctx.TargetID, uids); err != nil {
		log.Errorf("nest command: delete: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}
	ctx.TriggerReload()
	tr := ctx.Tr()
	return []bot.Reply{{React: "✅", Text: tr.Tf("cmd.removed_n", len(uids))}}
}
