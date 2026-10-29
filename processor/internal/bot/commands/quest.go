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

// QuestCommand implements !quest — track quest rewards (pokemon, stardust, pokecoins, items, candy, energy).
type QuestCommand struct{}

func (c *QuestCommand) Name() string      { return "cmd.quest" }
func (c *QuestCommand) Aliases() []string { return nil }

var questParams = []bot.ParamDef{
	{Type: bot.ParamRemoveUID},
	{Type: bot.ParamPrefixSingle, Key: "arg.prefix.d"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.template"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.form"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.stardust"}, // stardust:1000 (min amount)
	{Type: bot.ParamPrefixString, Key: "arg.prefix.pokecoin"}, // pokecoins:10 (min amount)
	{Type: bot.ParamPrefixString, Key: "arg.prefix.energy"},   // energy:charizard (pokemon)
	{Type: bot.ParamPrefixString, Key: "arg.prefix.candy"},    // candy:pikachu (pokemon)
	{Type: bot.ParamPrefixString, Key: "arg.prefix.amount"},   // amount:N (min amount for item/candy/mega_energy quests)
	{Type: bot.ParamPrefixString, Key: "arg.prefix.location"},
	{Type: bot.ParamPrefixStringList, Key: "arg.prefix.area"},
	{Type: bot.ParamKeyword, Key: "arg.remove"},
	{Type: bot.ParamKeyword, Key: "arg.everything"},
	{Type: bot.ParamKeyword, Key: "arg.all_pokemon"}, // all pokemon (reward_type 7, wildcard reward=0)
	{Type: bot.ParamKeyword, Key: "arg.all_items"},   // all items (reward_type 2, wildcard reward=0)
	{Type: bot.ParamKeyword, Key: "arg.clean"},
	{Type: bot.ParamKeyword, Key: "arg.summary"},
	{Type: bot.ParamKeyword, Key: "arg.shiny"},
	{Type: bot.ParamKeyword, Key: "arg.stardust"}, // bare "stardust" keyword (any amount)
	{Type: bot.ParamKeyword, Key: "arg.pokecoin"}, // bare "pokecoins" keyword (any amount)
	{Type: bot.ParamKeyword, Key: "arg.energy"},   // bare "energy" keyword (any pokemon)
	{Type: bot.ParamKeyword, Key: "arg.candy"},    // bare "candy" keyword (any pokemon)
	{Type: bot.ParamPokemonName},
}

func (c *QuestCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	if reply := RouteToMuteFromType(ctx, "quest", args); reply != nil {
		return reply
	}

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

	override, overrideReply := parseOverride(ctx, parsed.Strings["location"], parsed.StringLists["area"], common.Distance)
	if overrideReply != nil {
		return []bot.Reply{*overrideReply}
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

	// Bulk wildcard keywords (everything / all pokemon / all items) are
	// gated behind everything_flag_permissions for non-admins, mirroring
	// PoracleJS. "all pokemon" → reward_type 7 wildcard (reward=0);
	// "all items" → reward_type 2 wildcard (reward=0). Both combine with
	// each other and with the reward-specific keywords below.
	wantsAllPokemon := parsed.HasKeyword("arg.all_pokemon")
	wantsAllItems := parsed.HasKeyword("arg.all_items")
	if (wantsAllPokemon || wantsAllItems || parsed.HasKeyword("arg.everything")) && !questBulkAllowed(ctx) {
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.quest.bulk_not_permitted")}}
	}
	if wantsAllPokemon {
		insert = append(insert, c.makeQuest(ctx, common, override, shiny, pings, 7, 0, 0, 0))
	}
	if wantsAllItems {
		insert = append(insert, c.makeQuest(ctx, common, override, shiny, pings, 2, 0, 0, amountVal))
	}

	// Every reward named in the command becomes a rule, as in PoracleJS
	// (`!quest stardust pikachu` tracks both). `everything` already covers
	// every reward type, so it stands alone.
	if parsed.HasKeyword("arg.everything") {
		// When amount:N is paired with everything, apply it per-row where
		// supported:
		//
		//	pokemon (7)        — no amount semantics; always 0
		//	stardust (3)       — min lives in Reward (matcher quirk)
		//	candy / mega / item — q.Amount > 0 filter (the natural slot)
		insert = append(insert, c.makeQuest(ctx, common, override, shiny, pings, 7, 0, 0, 0))
		insert = append(insert, c.makeQuest(ctx, common, override, shiny, pings, 3, amountVal, 0, 0))
		insert = append(insert, c.makeQuest(ctx, common, override, shiny, pings, 8, amountVal, 0, 0))
		insert = append(insert, c.makeQuest(ctx, common, override, shiny, pings, 12, 0, 0, amountVal))
		insert = append(insert, c.makeQuest(ctx, common, override, shiny, pings, 4, 0, 0, amountVal))
		insert = append(insert, c.makeQuest(ctx, common, override, shiny, pings, 2, 0, 0, amountVal))
	} else {
		// amountUsed records whether a reward that honours amount:N was
		// named, so amount:N alongside only pokemon rewards can be refused.
		amountUsed := false
		// A bare energy/candy keyword takes the named pokemon as its
		// target; those pokemon are then not pokemon rewards themselves.
		pokemonConsumed := false

		// Stardust and pokecoins hold their minimum in Reward, so amount:N
		// counts as used by either form.
		if stardustVal, ok := parsed.Strings["stardust"]; ok {
			// Explicit stardust:N wins over a peer amount:N — the explicit
			// form is unambiguous about which column to fill.
			insert = append(insert, c.makeQuest(ctx, common, override, shiny, pings, 3, questParseInt(stardustVal), 0, 0))
			amountUsed = true
		} else if parsed.HasKeyword("arg.stardust") {
			// bare "stardust" + amount:N — route amount into the Reward
			// column (the stardust grammar's own min-amount slot). bare
			// "stardust" alone means "any amount".
			insert = append(insert, c.makeQuest(ctx, common, override, shiny, pings, 3, amountVal, 0, 0))
			amountUsed = true
		}

		if pokecoinVal, ok := parsed.Strings["pokecoin"]; ok {
			// pokecoins:N — min amount lives in Reward, mirroring stardust.
			insert = append(insert, c.makeQuest(ctx, common, override, shiny, pings, 8, questParseInt(pokecoinVal), 0, 0))
			amountUsed = true
		} else if parsed.HasKeyword("arg.pokecoin") {
			// bare "pokecoins" + amount:N — route amount into Reward.
			insert = append(insert, c.makeQuest(ctx, common, override, shiny, pings, 8, amountVal, 0, 0))
			amountUsed = true
		}

		// energy (12) and candy (4) share a grammar: kind:pokemon names the
		// pokemon; a bare keyword uses the pokemon named elsewhere in the
		// command, or any pokemon when none is.
		for _, kind := range []struct {
			rewardType int
			key        string
		}{{12, "energy"}, {4, "candy"}} {
			var mons []bot.ResolvedPokemon
			if val, ok := parsed.Strings[kind.key]; ok {
				mons = ctx.Resolver.Resolve(val, ctx.Language)
			} else if parsed.HasKeyword("arg." + kind.key) {
				mons = parsed.Pokemon
				pokemonConsumed = pokemonConsumed || len(mons) > 0
			} else {
				continue
			}
			amountUsed = true
			if len(mons) == 0 {
				insert = append(insert, c.makeQuest(ctx, common, override, shiny, pings, kind.rewardType, 0, 0, amountVal))
			}
			for _, mon := range mons {
				insert = append(insert, c.makeQuest(ctx, common, override, shiny, pings, kind.rewardType, mon.PokemonID, mon.Form, amountVal))
			}
		}

		if len(parsed.Pokemon) > 0 && !pokemonConsumed {
			// Pokemon quest tracking (reward_type = 7)
			monsterList, formReply := c.resolveMonsters(ctx, parsed)
			if formReply != nil {
				return []bot.Reply{*formReply}
			}
			for _, mon := range monsterList {
				insert = append(insert, db.QuestTrackingAPI{
					ID:                    ctx.TargetID,
					ProfileNo:             ctx.ProfileNo,
					Ping:                  pings,
					Template:              common.Template,
					Distance:              common.Distance,
					Clean:                 common.Clean,
					Shiny:                 db.IntBool(shiny),
					RewardType:            7,
					Reward:                mon.PokemonID,
					Form:                  mon.Form,
					Amount:                0,
					OverrideLocationLabel: override.LocationLabel,
					OverrideAreas:         override.Areas,
				})
			}
		}

		if itemID := c.matchItemName(ctx, parsed); itemID > 0 {
			// Item quest tracking (reward_type = 2)
			// Consume matched item tokens from Unrecognized
			parsed.Unrecognized = nil
			insert = append(insert, c.makeQuest(ctx, common, override, shiny, pings, 2, itemID, 0, amountVal))
			amountUsed = true
		}

		if amountSet && !amountUsed && !wantsAllItems && len(insert) > 0 {
			// amount:N with only pokemon rewards (which have no amount
			// semantics) would be a silent no-op.
			return []bot.Reply{{React: "🙅", Text: tr.T("msg.quest.amount_not_applicable")}}
		}
		if len(insert) == 0 {
			return []bot.Reply{{React: "🙅", Text: tr.T("msg.no_quest_type")}}
		}
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

	if r := overrideLimitReply(ctx, tracked, insert, store.QuestGetUID, store.QuestSetUID); r != nil {
		return r
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

func (c *QuestCommand) makeQuest(ctx *bot.CommandContext, common *commonTrackFields, override Override, shiny bool, ping string, rewardType, reward, form, amount int) db.QuestTrackingAPI {
	return db.QuestTrackingAPI{
		ID:                    ctx.TargetID,
		ProfileNo:             ctx.ProfileNo,
		Ping:                  ping,
		Template:              common.Template,
		Distance:              common.Distance,
		Clean:                 common.Clean,
		Shiny:                 db.IntBool(shiny),
		RewardType:            rewardType,
		Reward:                reward,
		Form:                  form,
		Amount:                amount,
		OverrideLocationLabel: override.LocationLabel,
		OverrideAreas:         override.Areas,
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

// questBulkAllowed reports whether the user may use bulk wildcard keywords
// (everything / all pokemon / all items). Mirrors PoracleJS: gated behind
// everything_flag_permissions for non-admins, always allowed for admins.
func questBulkAllowed(ctx *bot.CommandContext) bool {
	if ctx.IsAdmin {
		return true
	}
	return strings.ToLower(ctx.Config.Tracking.EverythingFlagPermissions) != "deny"
}

// handleRemove handles !quest remove variants. Must be called before reward type detection.
func (c *QuestCommand) handleRemove(ctx *bot.CommandContext, parsed *bot.ParsedArgs, common *commonTrackFields, shiny bool, pings string) []bot.Reply {
	if reply := rejectFormOnRemove(ctx, parsed); reply != nil {
		return []bot.Reply{*reply}
	}

	if len(parsed.RemoveUIDs) > 0 {
		tr := ctx.Tr()
		return removeByUIDs(ctx, ctx.Tracking.Quests, parsed.RemoveUIDs,
			store.QuestGetUID,
			func(r *db.QuestTrackingAPI) string { return ctx.RowText.QuestRowText(tr, questAPIToTracking(r)) },
		)
	}

	var targets []db.QuestTrackingAPI

	noOverride := Override{}
	target := func(rewardType, reward, form int) {
		targets = append(targets, c.makeQuest(ctx, common, noOverride, shiny, pings, rewardType, reward, form, 0))
	}
	allTypes := func() {
		for _, rt := range []int{7, 3, 8, 12, 4, 2} {
			target(rt, 0, 0)
		}
	}

	// Mirrors the add path: every reward kind named in the command has its
	// matching rules removed (`!quest remove stardust pikachu` removes
	// both). `everything` already covers every reward type, so it stands
	// alone.
	if parsed.HasKeyword("arg.everything") {
		allTypes()
		return c.removeQuests(ctx, targets)
	}

	// "all pokemon" / "all items" — wildcard rows (reward=0), which match
	// every rule of that reward type.
	if parsed.HasKeyword("arg.all_pokemon") {
		target(7, 0, 0)
	}
	if parsed.HasKeyword("arg.all_items") {
		target(2, 0, 0)
	}

	if stardustVal, ok := parsed.Strings["stardust"]; ok {
		target(3, questParseInt(stardustVal), 0)
	} else if parsed.HasKeyword("arg.stardust") {
		target(3, 0, 0)
	}

	if pokecoinVal, ok := parsed.Strings["pokecoin"]; ok {
		target(8, questParseInt(pokecoinVal), 0)
	} else if parsed.HasKeyword("arg.pokecoin") {
		target(8, 0, 0)
	}

	// energy (12) and candy (4): kind:pokemon names the pokemon; a bare
	// keyword uses the pokemon named elsewhere in the command (which are
	// then not pokemon rewards themselves), or any pokemon when none is.
	pokemonConsumed := false
	for _, kind := range []struct {
		rewardType int
		key        string
	}{{12, "energy"}, {4, "candy"}} {
		var mons []bot.ResolvedPokemon
		if val, ok := parsed.Strings[kind.key]; ok {
			mons = ctx.Resolver.Resolve(val, ctx.Language)
		} else if parsed.HasKeyword("arg." + kind.key) {
			mons = parsed.Pokemon
			pokemonConsumed = pokemonConsumed || len(mons) > 0
		} else {
			continue
		}
		if len(mons) == 0 {
			target(kind.rewardType, 0, 0)
		}
		for _, mon := range mons {
			target(kind.rewardType, mon.PokemonID, mon.Form)
		}
	}

	if len(parsed.Pokemon) > 0 && !pokemonConsumed {
		monsterList, formReply := c.resolveMonsters(ctx, parsed)
		if formReply != nil {
			return []bot.Reply{*formReply}
		}
		for _, mon := range monsterList {
			target(7, mon.PokemonID, mon.Form)
		}
	}

	if itemID := c.matchItemName(ctx, parsed); itemID > 0 {
		// Item reward (e.g. !quest remove pinap). Consume matched tokens
		// so the unrecognized-arg checker doesn't also flag them.
		parsed.Unrecognized = nil
		target(2, itemID, 0)
	}

	if len(targets) == 0 {
		// No specific type — remove everything
		allTypes()
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
	requireSummary := len(targets) > 0 && db.IsSummary(targets[0].Clean)

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
