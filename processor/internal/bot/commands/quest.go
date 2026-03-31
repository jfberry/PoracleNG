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

// QuestCommand implements !quest — track quest rewards (pokemon, stardust, items, candy, energy).
type QuestCommand struct{}

func (c *QuestCommand) Name() string      { return "cmd.quest" }
func (c *QuestCommand) Aliases() []string { return nil }

var questParams = []bot.ParamDef{
	{Type: bot.ParamPrefixSingle, Key: "arg.prefix.d"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.template"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.form"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.stardust"}, // stardust:1000 (min amount)
	{Type: bot.ParamPrefixString, Key: "arg.prefix.energy"},   // energy:charizard (pokemon)
	{Type: bot.ParamPrefixString, Key: "arg.prefix.candy"},    // candy:pikachu (pokemon)
	{Type: bot.ParamKeyword, Key: "arg.remove"},
	{Type: bot.ParamKeyword, Key: "arg.everything"},
	{Type: bot.ParamKeyword, Key: "arg.clean"},
	{Type: bot.ParamKeyword, Key: "arg.shiny"},
	{Type: bot.ParamKeyword, Key: "arg.stardust"},  // bare "stardust" keyword (any amount)
	{Type: bot.ParamKeyword, Key: "arg.energy"},     // bare "energy" keyword (any pokemon)
	{Type: bot.ParamKeyword, Key: "arg.candy"},      // bare "candy" keyword (any pokemon)
	{Type: bot.ParamPokemonName},
}

func (c *QuestCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	parsed := ctx.ArgMatcher.Match(args, questParams, ctx.Language)

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
	shiny := parsed.HasKeyword("arg.shiny")

	// Determine reward mode.
	// Supports colon syntax (preferred): energy:charizard, candy:pikachu, stardust:1000
	// Also supports bare keywords: energy, candy, stardust (+ separate pokemon name)
	var insert []db.QuestTrackingAPI

	if stardustVal, ok := parsed.Strings["stardust"]; ok {
		// stardust:1000 — minimum amount
		amount := questParseInt(stardustVal)
		insert = append(insert, c.makeQuest(ctx, template, distance, clean, shiny, 3, 0, 0, amount))
	} else if parsed.HasKeyword("arg.stardust") {
		// bare "stardust" — any amount
		insert = append(insert, c.makeQuest(ctx, template, distance, clean, shiny, 3, 0, 0, 0))
	} else if energyVal, ok := parsed.Strings["energy"]; ok {
		// energy:charizard — resolve pokemon name
		resolved := ctx.Resolver.Resolve(energyVal, ctx.Language)
		if len(resolved) > 0 {
			for _, mon := range resolved {
				insert = append(insert, c.makeQuest(ctx, template, distance, clean, shiny, 12, mon.PokemonID, mon.Form, 0))
			}
		} else {
			insert = append(insert, c.makeQuest(ctx, template, distance, clean, shiny, 12, 0, 0, 0))
		}
	} else if parsed.HasKeyword("arg.energy") {
		// bare "energy" — check for pokemon in args
		if len(parsed.Pokemon) > 0 {
			for _, mon := range parsed.Pokemon {
				insert = append(insert, c.makeQuest(ctx, template, distance, clean, shiny, 12, mon.PokemonID, mon.Form, 0))
			}
		} else {
			insert = append(insert, c.makeQuest(ctx, template, distance, clean, shiny, 12, 0, 0, 0))
		}
	} else if candyVal, ok := parsed.Strings["candy"]; ok {
		// candy:pikachu — resolve pokemon name
		resolved := ctx.Resolver.Resolve(candyVal, ctx.Language)
		if len(resolved) > 0 {
			for _, mon := range resolved {
				insert = append(insert, c.makeQuest(ctx, template, distance, clean, shiny, 4, mon.PokemonID, mon.Form, 0))
			}
		} else {
			insert = append(insert, c.makeQuest(ctx, template, distance, clean, shiny, 4, 0, 0, 0))
		}
	} else if parsed.HasKeyword("arg.candy") {
		// bare "candy" — check for pokemon in args
		if len(parsed.Pokemon) > 0 {
			for _, mon := range parsed.Pokemon {
				insert = append(insert, c.makeQuest(ctx, template, distance, clean, shiny, 4, mon.PokemonID, mon.Form, 0))
			}
		} else {
			insert = append(insert, c.makeQuest(ctx, template, distance, clean, shiny, 4, 0, 0, 0))
		}
	} else if parsed.HasKeyword("arg.everything") {
		// Everything: track all quest types (pokemon reward_type=7, reward=0)
		insert = append(insert, db.QuestTrackingAPI{
			ID:         ctx.TargetID,
			ProfileNo:  ctx.ProfileNo,
			Ping:       "",
			Template:   template,
			Distance:   distance,
			Clean:      db.IntBool(clean),
			Shiny:      db.IntBool(shiny),
			RewardType: 7,
			Reward:     0,
			Form:       0,
			Amount:     0,
		})
	} else if len(parsed.Pokemon) > 0 {
		// Pokemon quest tracking (reward_type = 7)
		monsterList := c.resolveMonsters(ctx, parsed)
		for _, mon := range monsterList {
			insert = append(insert, db.QuestTrackingAPI{
				ID:         ctx.TargetID,
				ProfileNo:  ctx.ProfileNo,
				Ping:       "",
				Template:   template,
				Distance:   distance,
				Clean:      db.IntBool(clean),
				Shiny:      db.IntBool(shiny),
				RewardType: 7,
				Reward:     mon.PokemonID,
				Form:       mon.Form,
				Amount:     0,
			})
		}
	} else {
		return []bot.Reply{{React: "🙅", Text: "No quest type specified"}}
	}

	if parsed.HasKeyword("arg.remove") {
		return c.removeQuests(ctx, insert)
	}

	if len(insert) == 0 {
		return []bot.Reply{{React: "🙅", Text: "No quest type specified"}}
	}

	// Diff against existing
	tracked, err := db.SelectQuestsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("quest command: select existing: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	var updates, alreadyPresent []db.QuestTrackingAPI
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
			return ctx.RowText.QuestRowText(tr, questAPIToTracking(&alreadyPresent[i]))
		},
		func(i int) string {
			return ctx.RowText.QuestRowText(tr, questAPIToTracking(&updates[i]))
		},
		func(i int) string {
			return ctx.RowText.QuestRowText(tr, questAPIToTracking(&insert[i]))
		},
	)

	if len(updates) > 0 {
		uids := make([]int64, len(updates))
		for i, u := range updates {
			uids[i] = u.UID
		}
		if err := db.DeleteByUIDs(ctx.DB, "quest", ctx.TargetID, uids); err != nil {
			log.Errorf("quest command: delete updated: %s", err)
			return []bot.Reply{{React: "🙅"}}
		}
	}

	toInsert := append(insert, updates...)
	for i := range toInsert {
		if _, err := db.InsertQuest(ctx.DB, &toInsert[i]); err != nil {
			log.Errorf("quest command: insert: %s", err)
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

func (c *QuestCommand) makeQuest(ctx *bot.CommandContext, template string, distance int, clean, shiny bool, rewardType, reward, form, amount int) db.QuestTrackingAPI {
	return db.QuestTrackingAPI{
		ID:         ctx.TargetID,
		ProfileNo:  ctx.ProfileNo,
		Ping:       "",
		Template:   template,
		Distance:   distance,
		Clean:      db.IntBool(clean),
		Shiny:      db.IntBool(shiny),
		RewardType: rewardType,
		Reward:     reward,
		Form:       form,
		Amount:     amount,
	}
}

func questParseInt(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

func (c *QuestCommand) resolveMonsters(ctx *bot.CommandContext, parsed *bot.ParsedArgs) []bot.ResolvedPokemon {
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

	return monsters
}

func (c *QuestCommand) removeQuests(ctx *bot.CommandContext, targets []db.QuestTrackingAPI) []bot.Reply {
	tracked, err := db.SelectQuestsByIDProfile(ctx.DB, ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("quest command: select for remove: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	var uids []int64
	for _, existing := range tracked {
		for _, target := range targets {
			if existing.RewardType == target.RewardType &&
				(target.Reward == 0 || existing.Reward == target.Reward) {
				uids = append(uids, existing.UID)
				break
			}
		}
	}

	if len(uids) == 0 {
		return []bot.Reply{{React: "👌"}}
	}
	if err := db.DeleteByUIDs(ctx.DB, "quest", ctx.TargetID, uids); err != nil {
		log.Errorf("quest command: delete: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}
	ctx.TriggerReload()
	return []bot.Reply{{React: "✅", Text: fmt.Sprintf("Removed %d quest tracking rules", len(uids))}}
}
