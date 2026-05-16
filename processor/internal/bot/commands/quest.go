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
	{Type: bot.ParamRemoveUID},
	{Type: bot.ParamPrefixSingle, Key: "arg.prefix.d"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.template"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.form"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.stardust"}, // stardust:1000 (min amount)
	{Type: bot.ParamPrefixString, Key: "arg.prefix.energy"},   // energy:charizard (pokemon)
	{Type: bot.ParamPrefixString, Key: "arg.prefix.candy"},    // candy:pikachu (pokemon)
	{Type: bot.ParamPrefixString, Key: "arg.prefix.amount"},   // amount:N (min amount for item/candy/mega_energy quests)
	{Type: bot.ParamKeyword, Key: "arg.remove"},
	{Type: bot.ParamKeyword, Key: "arg.everything"},
	{Type: bot.ParamKeyword, Key: "arg.clean"},
	{Type: bot.ParamKeyword, Key: "arg.summary"},
	{Type: bot.ParamKeyword, Key: "arg.shiny"},
	{Type: bot.ParamKeyword, Key: "arg.stardust"}, // bare "stardust" keyword (any amount)
	{Type: bot.ParamKeyword, Key: "arg.energy"},   // bare "energy" keyword (any pokemon)
	{Type: bot.ParamKeyword, Key: "arg.candy"},    // bare "candy" keyword (any pokemon)
	{Type: bot.ParamPokemonName},
}

func (c *QuestCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	// Extract @mention pings before parsing
	pings, args := extractPings(args)

	if usage := usageReply(ctx, args, "msg.quest.usage"); usage != nil {
		return []bot.Reply{*usage}
	}
	if help := helpArgReply(ctx, args, "msg.quest.usage"); help != nil {
		return []bot.Reply{*help}
	}

	if ctx.Config.General.DisableQuest {
		return []bot.Reply{{React: "\U0001f645", Text: "This alert type is disabled"}}
	}

	parsed := ctx.ArgMatcher.Match(args, questParams, ctx.Language)

	common, block := parseCommonTrackFields(ctx, parsed, "quest")
	if block != nil {
		return []bot.Reply{*block}
	}
	if parsed.HasKeyword("arg.summary") {
		// edit and summary are mutually exclusive: edit means update one
		// in-place message; summary buffers and groups. Reject up-front
		// so users get a clear error rather than surprising behaviour.
		if parsed.HasKeyword("arg.edit") {
			return []bot.Reply{{React: "🙅", Text: tr.T("msg.quest.edit_summary_conflict")}}
		}
		common.Clean |= 4
	}
	shiny := parsed.HasKeyword("arg.shiny")

	// Handle remove first — before reward type detection
	if parsed.HasKeyword("arg.remove") {
		return c.handleRemove(ctx, parsed, common, shiny, pings)
	}

	// Determine reward mode.
	// Supports colon syntax (preferred): energy:charizard, candy:pikachu, stardust:1000
	// Also supports bare keywords: energy, candy, stardust (+ separate pokemon name)
	var insert []db.QuestTrackingAPI

	// amount:N — minimum-amount filter for reward types that support it.
	// internal/matching/quest.go honours q.Amount for item (2), candy (4),
	// and mega_energy (12). Stardust (3) stores its minimum in Reward,
	// not Amount — when paired with a stardust reward we route the
	// amount into Reward instead of rejecting, so users don't have to
	// remember which keyword goes where. Pokemon (7) has no amount
	// semantics; rejecting amount:N with pokemon-reward prevents a
	// silent no-op the user wouldn't notice.
	amountStr, amountSet := parsed.Strings["amount"]
	amountVal := 0
	if amountSet {
		amountVal = questParseInt(amountStr)
	}

	if stardustVal, ok := parsed.Strings["stardust"]; ok {
		// Explicit stardust:N wins over a peer amount:N — the explicit
		// form is unambiguous about which column to fill.
		_ = amountSet
		amount := questParseInt(stardustVal)
		insert = append(insert, c.makeQuest(ctx, common, shiny, pings, 3, amount, 0, 0))
	} else if parsed.HasKeyword("arg.stardust") {
		// bare "stardust" + amount:N — route amount into the Reward
		// column (the stardust grammar's own min-amount slot). bare
		// "stardust" alone means "any amount".
		insert = append(insert, c.makeQuest(ctx, common, shiny, pings, 3, amountVal, 0, 0))
	} else if energyVal, ok := parsed.Strings["energy"]; ok {
		// energy:charizard — resolve pokemon name
		resolved := ctx.Resolver.Resolve(energyVal, ctx.Language)
		if len(resolved) > 0 {
			for _, mon := range resolved {
				insert = append(insert, c.makeQuest(ctx, common, shiny, pings, 12, mon.PokemonID, mon.Form, amountVal))
			}
		} else {
			insert = append(insert, c.makeQuest(ctx, common, shiny, pings, 12, 0, 0, amountVal))
		}
	} else if parsed.HasKeyword("arg.energy") {
		// bare "energy" — check for pokemon in args
		if len(parsed.Pokemon) > 0 {
			for _, mon := range parsed.Pokemon {
				insert = append(insert, c.makeQuest(ctx, common, shiny, pings, 12, mon.PokemonID, mon.Form, amountVal))
			}
		} else {
			insert = append(insert, c.makeQuest(ctx, common, shiny, pings, 12, 0, 0, amountVal))
		}
	} else if candyVal, ok := parsed.Strings["candy"]; ok {
		// candy:pikachu — resolve pokemon name
		resolved := ctx.Resolver.Resolve(candyVal, ctx.Language)
		if len(resolved) > 0 {
			for _, mon := range resolved {
				insert = append(insert, c.makeQuest(ctx, common, shiny, pings, 4, mon.PokemonID, mon.Form, amountVal))
			}
		} else {
			insert = append(insert, c.makeQuest(ctx, common, shiny, pings, 4, 0, 0, amountVal))
		}
	} else if parsed.HasKeyword("arg.candy") {
		// bare "candy" — check for pokemon in args
		if len(parsed.Pokemon) > 0 {
			for _, mon := range parsed.Pokemon {
				insert = append(insert, c.makeQuest(ctx, common, shiny, pings, 4, mon.PokemonID, mon.Form, amountVal))
			}
		} else {
			insert = append(insert, c.makeQuest(ctx, common, shiny, pings, 4, 0, 0, amountVal))
		}
	} else if parsed.HasKeyword("arg.everything") {
		// Everything: track all quest reward types. When amount:N is
		// paired with everything, apply it per-row where supported:
		//
		//	pokemon (7)        — no amount semantics; always 0
		//	stardust (3)       — min lives in Reward (matcher quirk)
		//	candy / mega / item — q.Amount > 0 filter (the natural slot)
		insert = append(insert, c.makeQuest(ctx, common, shiny, pings, 7, 0, 0, 0))
		insert = append(insert, c.makeQuest(ctx, common, shiny, pings, 3, amountVal, 0, 0))
		insert = append(insert, c.makeQuest(ctx, common, shiny, pings, 12, 0, 0, amountVal))
		insert = append(insert, c.makeQuest(ctx, common, shiny, pings, 4, 0, 0, amountVal))
		insert = append(insert, c.makeQuest(ctx, common, shiny, pings, 2, 0, 0, amountVal))
	} else if len(parsed.Pokemon) > 0 {
		if amountSet {
			return []bot.Reply{{React: "🙅", Text: tr.T("msg.quest.amount_not_applicable")}}
		}
		// Pokemon quest tracking (reward_type = 7)
		monsterList, formReply := c.resolveMonsters(ctx, parsed)
		if formReply != nil {
			return []bot.Reply{*formReply}
		}
		for _, mon := range monsterList {
			insert = append(insert, db.QuestTrackingAPI{
				ID:         ctx.TargetID,
				ProfileNo:  ctx.ProfileNo,
				Ping:       pings,
				Template:   common.Template,
				Distance:   common.Distance,
				Clean:      common.Clean,
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
		insert = append(insert, c.makeQuest(ctx, common, shiny, pings, 2, itemID, 0, amountVal))
	} else {
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.no_quest_type")}}
	}

	// Check for remaining unrecognized args (after item matching had a chance)
	if warn := bot.ReportUnrecognized(parsed, tr); warn != nil {
		return []bot.Reply{*warn}
	}

	if len(insert) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.no_quest_type")}}
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

	message += trackingWarnings(ctx, common.Distance)

	if common.TemplateWarn != "" {
		message += "\n⚠️ " + common.TemplateWarn
	}

	react := "✅"
	if len(diff.Inserts) == 0 && len(diff.Updates) == 0 {
		react = "👌"
	}
	return []bot.Reply{{React: react, Text: message}}
}

func (c *QuestCommand) makeQuest(ctx *bot.CommandContext, common *commonTrackFields, shiny bool, ping string, rewardType, reward, form, amount int) db.QuestTrackingAPI {
	return db.QuestTrackingAPI{
		ID:         ctx.TargetID,
		ProfileNo:  ctx.ProfileNo,
		Ping:       ping,
		Template:   common.Template,
		Distance:   common.Distance,
		Clean:      common.Clean,
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

func (c *QuestCommand) resolveMonsters(ctx *bot.CommandContext, parsed *bot.ParsedArgs) ([]bot.ResolvedPokemon, *bot.Reply) {
	return applyFormFilter(ctx, parsed.Pokemon, parsed)
}

// handleRemove handles !quest remove variants. Must be called before reward type detection.
func (c *QuestCommand) handleRemove(ctx *bot.CommandContext, parsed *bot.ParsedArgs, common *commonTrackFields, shiny bool, pings string) []bot.Reply {
	if len(parsed.RemoveUIDs) > 0 {
		tr := ctx.Tr()
		return removeByUIDs(ctx, ctx.Tracking.Quests, parsed.RemoveUIDs,
			store.QuestGetUID,
			func(r *db.QuestTrackingAPI) string { return ctx.RowText.QuestRowText(tr, questAPIToTracking(r)) },
		)
	}

	var targets []db.QuestTrackingAPI

	if parsed.HasKeyword("arg.everything") {
		// remove everything — match all reward types
		for _, rt := range []int{7, 3, 12, 4, 2} {
			targets = append(targets, c.makeQuest(ctx, common, shiny, pings, rt, 0, 0, 0))
		}
	} else if stardustVal, ok := parsed.Strings["stardust"]; ok {
		amount := questParseInt(stardustVal)
		targets = append(targets, c.makeQuest(ctx, common, shiny, pings, 3, amount, 0, 0))
	} else if parsed.HasKeyword("arg.stardust") {
		targets = append(targets, c.makeQuest(ctx, common, shiny, pings, 3, 0, 0, 0))
	} else if energyVal, ok := parsed.Strings["energy"]; ok {
		resolved := ctx.Resolver.Resolve(energyVal, ctx.Language)
		if len(resolved) > 0 {
			for _, mon := range resolved {
				targets = append(targets, c.makeQuest(ctx, common, shiny, pings, 12, mon.PokemonID, mon.Form, 0))
			}
		} else {
			targets = append(targets, c.makeQuest(ctx, common, shiny, pings, 12, 0, 0, 0))
		}
	} else if parsed.HasKeyword("arg.energy") {
		if len(parsed.Pokemon) > 0 {
			for _, mon := range parsed.Pokemon {
				targets = append(targets, c.makeQuest(ctx, common, shiny, pings, 12, mon.PokemonID, mon.Form, 0))
			}
		} else {
			targets = append(targets, c.makeQuest(ctx, common, shiny, pings, 12, 0, 0, 0))
		}
	} else if candyVal, ok := parsed.Strings["candy"]; ok {
		resolved := ctx.Resolver.Resolve(candyVal, ctx.Language)
		if len(resolved) > 0 {
			for _, mon := range resolved {
				targets = append(targets, c.makeQuest(ctx, common, shiny, pings, 4, mon.PokemonID, mon.Form, 0))
			}
		} else {
			targets = append(targets, c.makeQuest(ctx, common, shiny, pings, 4, 0, 0, 0))
		}
	} else if parsed.HasKeyword("arg.candy") {
		if len(parsed.Pokemon) > 0 {
			for _, mon := range parsed.Pokemon {
				targets = append(targets, c.makeQuest(ctx, common, shiny, pings, 4, mon.PokemonID, mon.Form, 0))
			}
		} else {
			targets = append(targets, c.makeQuest(ctx, common, shiny, pings, 4, 0, 0, 0))
		}
	} else if len(parsed.Pokemon) > 0 {
		monsterList, formReply := c.resolveMonsters(ctx, parsed)
		if formReply != nil {
			return []bot.Reply{*formReply}
		}
		for _, mon := range monsterList {
			targets = append(targets, c.makeQuest(ctx, common, shiny, pings, 7, mon.PokemonID, mon.Form, 0))
		}
	} else if itemID := c.matchItemName(ctx, parsed); itemID > 0 {
		// Item reward (e.g. !quest remove pinap) — match a single item.
		// Consume matched tokens so the unrecognized-arg checker doesn't
		// also flag them downstream.
		parsed.Unrecognized = nil
		targets = append(targets, c.makeQuest(ctx, common, shiny, pings, 2, itemID, 0, 0))
	} else {
		// No specific type — remove everything
		for _, rt := range []int{7, 3, 12, 4, 2} {
			targets = append(targets, c.makeQuest(ctx, common, shiny, pings, rt, 0, 0, 0))
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

	// Summary-bit filtering: if the user typed `summary` on the remove
	// command, the target's Clean carries bit 4 (db.IsSummary). Only
	// remove rules whose existing Clean also has bit 4 set, so
	// `!quest remove pikachu summary` removes ONLY the summary-mode
	// Pikachu rule and leaves a parallel immediate-mode Pikachu rule
	// untouched. When the user did NOT type `summary` we don't filter
	// on Clean (the historic behaviour — removes regardless of clean /
	// edit / ping bits).
	requireSummary := false
	if len(targets) > 0 && db.IsSummary(targets[0].Clean) {
		requireSummary = true
	}

	var uids []int64
	var removed []db.QuestTrackingAPI
	for _, existing := range tracked {
		if requireSummary && !db.IsSummary(existing.Clean) {
			continue
		}
		for _, target := range targets {
			if existing.RewardType == target.RewardType &&
				(target.Reward == 0 || existing.Reward == target.Reward) {
				uids = append(uids, existing.UID)
				removed = append(removed, existing)
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
	var descriptions []string
	for i := range removed {
		descriptions = append(descriptions, ctx.RowText.QuestRowText(tr, questAPIToTracking(&removed[i])))
	}
	return []bot.Reply{{React: "✅", Text: formatRemovedRows(tr, descriptions)}}
}
