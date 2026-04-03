package commands

import (
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// matchItemName tries to match unrecognized args against item names from game data
// using item_{id} translation keys. Returns the item ID or 0 if no match.
func (c *QuestCommand) matchItemName(ctx *bot.CommandContext, parsed *bot.ParsedArgs) int {
	if ctx.GameData == nil || len(parsed.Unrecognized) == 0 {
		return 0
	}

	tr := ctx.Tr()
	enTr := ctx.Translations.For("en")

	// Join all unrecognized args into a single string for multi-word item names
	// e.g. "golden razz berry" from quoted input or separate tokens
	fullPhrase := strings.ToLower(strings.Join(parsed.Unrecognized, " "))

	for id := range ctx.GameData.Items {
		key := gamedata.ItemTranslationKey(id)
		translatedName := strings.ToLower(tr.T(key))
		enName := strings.ToLower(enTr.T(key))

		// Skip untranslated keys (returns the key itself)
		if translatedName == strings.ToLower(key) && enName == strings.ToLower(key) {
			continue
		}

		if fullPhrase == translatedName || fullPhrase == enName {
			return id
		}
	}

	// Also try matching individual unrecognized args (single-word items)
	for _, arg := range parsed.Unrecognized {
		lower := strings.ToLower(arg)
		for id := range ctx.GameData.Items {
			key := gamedata.ItemTranslationKey(id)
			translatedName := strings.ToLower(tr.T(key))
			enName := strings.ToLower(enTr.T(key))
			if translatedName == strings.ToLower(key) && enName == strings.ToLower(key) {
				continue
			}
			if lower == translatedName || lower == enName {
				return id
			}
		}
	}

	return 0
}

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

	// Extract @mention pings before parsing
	pings, args := extractPings(args)

	if usage := usageReply(ctx, args, "cmd.quest.usage"); usage != nil {
		return []bot.Reply{*usage}
	}
	if help := helpArgReply(ctx, args, "cmd.quest.usage"); help != nil {
		return []bot.Reply{*help}
	}

	parsed := ctx.ArgMatcher.Match(args, questParams, ctx.Language)

	template := ctx.DefaultTemplate()
	if t, ok := parsed.Strings["template"]; ok {
		template = t
	}

	// Validate template exists
	var templateWarn string
	if _, explicit := parsed.Strings["template"]; explicit {
		if block, warn := validateTemplate(ctx, "quest", template); block != nil {
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
	shiny := parsed.HasKeyword("arg.shiny")

	// Handle remove first — before reward type detection
	if parsed.HasKeyword("arg.remove") {
		return c.handleRemove(ctx, parsed, template, distance, clean, shiny, pings)
	}

	// Determine reward mode.
	// Supports colon syntax (preferred): energy:charizard, candy:pikachu, stardust:1000
	// Also supports bare keywords: energy, candy, stardust (+ separate pokemon name)
	var insert []db.QuestTrackingAPI

	if stardustVal, ok := parsed.Strings["stardust"]; ok {
		// stardust:1000 — minimum amount stored in Reward field (matching alerter)
		amount := questParseInt(stardustVal)
		insert = append(insert, c.makeQuest(ctx, template, distance, clean, shiny, pings, 3, amount, 0, 0))
	} else if parsed.HasKeyword("arg.stardust") {
		// bare "stardust" — any amount
		insert = append(insert, c.makeQuest(ctx, template, distance, clean, shiny, pings, 3, 0, 0, 0))
	} else if energyVal, ok := parsed.Strings["energy"]; ok {
		// energy:charizard — resolve pokemon name
		resolved := ctx.Resolver.Resolve(energyVal, ctx.Language)
		if len(resolved) > 0 {
			for _, mon := range resolved {
				insert = append(insert, c.makeQuest(ctx, template, distance, clean, shiny, pings, 12, mon.PokemonID, mon.Form, 0))
			}
		} else {
			insert = append(insert, c.makeQuest(ctx, template, distance, clean, shiny, pings, 12, 0, 0, 0))
		}
	} else if parsed.HasKeyword("arg.energy") {
		// bare "energy" — check for pokemon in args
		if len(parsed.Pokemon) > 0 {
			for _, mon := range parsed.Pokemon {
				insert = append(insert, c.makeQuest(ctx, template, distance, clean, shiny, pings, 12, mon.PokemonID, mon.Form, 0))
			}
		} else {
			insert = append(insert, c.makeQuest(ctx, template, distance, clean, shiny, pings, 12, 0, 0, 0))
		}
	} else if candyVal, ok := parsed.Strings["candy"]; ok {
		// candy:pikachu — resolve pokemon name
		resolved := ctx.Resolver.Resolve(candyVal, ctx.Language)
		if len(resolved) > 0 {
			for _, mon := range resolved {
				insert = append(insert, c.makeQuest(ctx, template, distance, clean, shiny, pings, 4, mon.PokemonID, mon.Form, 0))
			}
		} else {
			insert = append(insert, c.makeQuest(ctx, template, distance, clean, shiny, pings, 4, 0, 0, 0))
		}
	} else if parsed.HasKeyword("arg.candy") {
		// bare "candy" — check for pokemon in args
		if len(parsed.Pokemon) > 0 {
			for _, mon := range parsed.Pokemon {
				insert = append(insert, c.makeQuest(ctx, template, distance, clean, shiny, pings, 4, mon.PokemonID, mon.Form, 0))
			}
		} else {
			insert = append(insert, c.makeQuest(ctx, template, distance, clean, shiny, pings, 4, 0, 0, 0))
		}
	} else if parsed.HasKeyword("arg.everything") {
		// Everything: track all quest reward types (matching alerter behavior)
		// Pokemon quests
		insert = append(insert, c.makeQuest(ctx, template, distance, clean, shiny, pings, 7, 0, 0, 0))
		// Stardust quests (reward=0 means any amount)
		insert = append(insert, c.makeQuest(ctx, template, distance, clean, shiny, pings, 3, 0, 0, 0))
		// Mega energy quests
		insert = append(insert, c.makeQuest(ctx, template, distance, clean, shiny, pings, 12, 0, 0, 0))
		// Candy quests
		insert = append(insert, c.makeQuest(ctx, template, distance, clean, shiny, pings, 4, 0, 0, 0))
		// Item quests
		insert = append(insert, c.makeQuest(ctx, template, distance, clean, shiny, pings, 2, 0, 0, 0))
	} else if len(parsed.Pokemon) > 0 {
		// Pokemon quest tracking (reward_type = 7)
		monsterList := c.resolveMonsters(ctx, parsed)
		for _, mon := range monsterList {
			insert = append(insert, db.QuestTrackingAPI{
				ID:         ctx.TargetID,
				ProfileNo:  ctx.ProfileNo,
				Ping:       pings,
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
	} else if itemID := c.matchItemName(ctx, parsed); itemID > 0 {
		// Item quest tracking (reward_type = 2)
		// Consume matched item tokens from Unrecognized
		parsed.Unrecognized = nil
		insert = append(insert, c.makeQuest(ctx, template, distance, clean, shiny, pings, 2, itemID, 0, 0))
	} else {
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.no_quest_type")}}
	}

	// Check for remaining unrecognized args (after item matching had a chance)
	if warn := bot.ReportUnrecognized(parsed, tr); warn != nil {
		return []bot.Reply{*warn}
	}

	if len(insert) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.T("cmd.no_quest_type")}}
	}

	// Diff against existing and apply
	tracked, err := ctx.Tracking.Quests.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("quest command: select existing: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	diff, err := store.ApplyDiff(ctx.Tracking.Quests, ctx.TargetID, tracked, insert,
		store.QuestGetUID, store.QuestSetUID)
	if err != nil {
		log.Errorf("quest command: apply diff: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	message := buildTrackingMessage(tr, ctx, len(diff.AlreadyPresent), len(diff.Updates), len(diff.Inserts),
		func(i int) string {
			return ctx.RowText.QuestRowText(tr, questAPIToTracking(&diff.AlreadyPresent[i]))
		},
		func(i int) string {
			return ctx.RowText.QuestRowText(tr, questAPIToTracking(&diff.Updates[i]))
		},
		func(i int) string {
			return ctx.RowText.QuestRowText(tr, questAPIToTracking(&diff.Inserts[i]))
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

func (c *QuestCommand) makeQuest(ctx *bot.CommandContext, template string, distance int, clean, shiny bool, ping string, rewardType, reward, form, amount int) db.QuestTrackingAPI {
	return db.QuestTrackingAPI{
		ID:         ctx.TargetID,
		ProfileNo:  ctx.ProfileNo,
		Ping:       ping,
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

// handleRemove handles !quest remove variants. Must be called before reward type detection.
func (c *QuestCommand) handleRemove(ctx *bot.CommandContext, parsed *bot.ParsedArgs, template string, distance int, clean, shiny bool, pings string) []bot.Reply {
	var targets []db.QuestTrackingAPI

	if parsed.HasKeyword("arg.everything") {
		// remove everything — match all reward types
		for _, rt := range []int{7, 3, 12, 4, 2} {
			targets = append(targets, c.makeQuest(ctx, template, distance, clean, shiny, pings, rt, 0, 0, 0))
		}
	} else if stardustVal, ok := parsed.Strings["stardust"]; ok {
		amount := questParseInt(stardustVal)
		targets = append(targets, c.makeQuest(ctx, template, distance, clean, shiny, pings, 3, amount, 0, 0))
	} else if parsed.HasKeyword("arg.stardust") {
		targets = append(targets, c.makeQuest(ctx, template, distance, clean, shiny, pings, 3, 0, 0, 0))
	} else if energyVal, ok := parsed.Strings["energy"]; ok {
		resolved := ctx.Resolver.Resolve(energyVal, ctx.Language)
		if len(resolved) > 0 {
			for _, mon := range resolved {
				targets = append(targets, c.makeQuest(ctx, template, distance, clean, shiny, pings, 12, mon.PokemonID, mon.Form, 0))
			}
		} else {
			targets = append(targets, c.makeQuest(ctx, template, distance, clean, shiny, pings, 12, 0, 0, 0))
		}
	} else if parsed.HasKeyword("arg.energy") {
		if len(parsed.Pokemon) > 0 {
			for _, mon := range parsed.Pokemon {
				targets = append(targets, c.makeQuest(ctx, template, distance, clean, shiny, pings, 12, mon.PokemonID, mon.Form, 0))
			}
		} else {
			targets = append(targets, c.makeQuest(ctx, template, distance, clean, shiny, pings, 12, 0, 0, 0))
		}
	} else if candyVal, ok := parsed.Strings["candy"]; ok {
		resolved := ctx.Resolver.Resolve(candyVal, ctx.Language)
		if len(resolved) > 0 {
			for _, mon := range resolved {
				targets = append(targets, c.makeQuest(ctx, template, distance, clean, shiny, pings, 4, mon.PokemonID, mon.Form, 0))
			}
		} else {
			targets = append(targets, c.makeQuest(ctx, template, distance, clean, shiny, pings, 4, 0, 0, 0))
		}
	} else if parsed.HasKeyword("arg.candy") {
		if len(parsed.Pokemon) > 0 {
			for _, mon := range parsed.Pokemon {
				targets = append(targets, c.makeQuest(ctx, template, distance, clean, shiny, pings, 4, mon.PokemonID, mon.Form, 0))
			}
		} else {
			targets = append(targets, c.makeQuest(ctx, template, distance, clean, shiny, pings, 4, 0, 0, 0))
		}
	} else if len(parsed.Pokemon) > 0 {
		monsterList := c.resolveMonsters(ctx, parsed)
		for _, mon := range monsterList {
			targets = append(targets, c.makeQuest(ctx, template, distance, clean, shiny, pings, 7, mon.PokemonID, mon.Form, 0))
		}
	} else {
		// No specific type — remove everything
		for _, rt := range []int{7, 3, 12, 4, 2} {
			targets = append(targets, c.makeQuest(ctx, template, distance, clean, shiny, pings, rt, 0, 0, 0))
		}
	}

	return c.removeQuests(ctx, targets)
}

func (c *QuestCommand) removeQuests(ctx *bot.CommandContext, targets []db.QuestTrackingAPI) []bot.Reply {
	tracked, err := ctx.Tracking.Quests.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
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
	if err := ctx.Tracking.Quests.DeleteByUIDs(ctx.TargetID, uids); err != nil {
		log.Errorf("quest command: delete: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}
	ctx.TriggerReload()
	tr := ctx.Tr()
	return []bot.Reply{{React: "✅", Text: tr.Tf("cmd.removed_n", len(uids))}}
}
