package commands

import (
	"slices"
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// matchItemNames matches the unrecognized args against item names from
// game data (item_{id} translation keys, in the user's language and
// English). Every item named becomes a match, so `!quest razz_berry
// pinap_berry` tracks both; a name may span several tokens ("golden razz
// berry" typed unquoted), longest span first. Matched tokens are removed
// from parsed.Unrecognized (and returned as consumed) and anything left
// over stays there for ReportUnrecognized to flag.
func (c *QuestCommand) matchItemNames(ctx *bot.CommandContext, parsed *bot.ParsedArgs) (ids []int, consumed []string) {
	if ctx.GameData == nil || len(parsed.Unrecognized) == 0 {
		return nil, nil
	}

	tr := ctx.Tr()
	enTr := ctx.Translations.For("en")

	names := make(map[string]int)
	for id := range ctx.GameData.Items {
		key := gamedata.ItemTranslationKey(id)
		for _, name := range []string{strings.ToLower(tr.T(key)), strings.ToLower(enTr.T(key))} {
			// Skip untranslated keys (T returns the key itself)
			if name == "" || name == strings.ToLower(key) {
				continue
			}
			names[name] = id
		}
	}

	tokens := parsed.Unrecognized
	var rest []string
	for i := 0; i < len(tokens); {
		matched := false
		for j := len(tokens); j > i; j-- {
			if id, ok := names[strings.ToLower(strings.Join(tokens[i:j], " "))]; ok {
				if !slices.Contains(ids, id) {
					ids = append(ids, id)
				}
				consumed = append(consumed, tokens[i:j]...)
				i = j
				matched = true
				break
			}
		}
		if !matched {
			rest = append(rest, tokens[i])
			i++
		}
	}
	parsed.Unrecognized = rest
	return ids, consumed
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

	if parsed.HasKeyword("arg.remove") {
		return c.handleRemove(ctx, parsed)
	}

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
				if len(mons) == 0 {
					// An unknown pokemon must not widen to "any pokemon".
					return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.unrecognized", kind.key+":"+val)}}
				}
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

		// Item quest tracking (reward_type = 2), one rule per item named.
		// Matched tokens are consumed; the rest are reported below.
		items, _ := c.matchItemNames(ctx, parsed)
		for _, itemID := range items {
			insert = append(insert, c.makeQuest(ctx, common, override, shiny, pings, 2, itemID, 0, amountVal))
			amountUsed = true
		}

		// Report leftovers before the "no quest type" reply, so a typo
		// (`!quest pikchu`) is named rather than reported as no type.
		if warn := bot.ReportUnrecognized(parsed, tr); warn != nil {
			return []bot.Reply{*warn}
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

// questRemoveSpec: quest rules are removed by reward (item names are read
// from the leftover words); summary narrows the selection to summary-mode
// rules.
var questRemoveSpec = removeSpec{
	selectors: []bot.ParamDef{
		pokemonParam,
		kw("arg.all_pokemon"), kw("arg.all_items"),
		kw("arg.stardust"), kw("arg.pokecoin"), kw("arg.energy"), kw("arg.candy"),
		{Type: bot.ParamPrefixString, Key: "arg.prefix.stardust"},
		{Type: bot.ParamPrefixString, Key: "arg.prefix.pokecoin"},
		{Type: bot.ParamPrefixString, Key: "arg.prefix.energy"},
		{Type: bot.ParamPrefixString, Key: "arg.prefix.candy"},
	},
	filters: []bot.ParamDef{kw("arg.summary")},
}

// handleRemove handles !quest remove. It mirrors the add path: every reward
// kind named in the command has its matching rules removed
// (`!quest remove stardust pikachu` removes both). A reward of 0 matches
// every rule of that reward type.
func (c *QuestCommand) handleRemove(ctx *bot.CommandContext, parsed *bot.ParsedArgs) []bot.Reply {
	tr := ctx.Tr()
	type questTarget struct{ rewardType, reward int }
	var targets []questTarget
	target := func(rewardType, reward int) {
		targets = append(targets, questTarget{rewardType, reward})
	}

	if parsed.HasKeyword("arg.all_pokemon") {
		target(7, 0)
	}
	if parsed.HasKeyword("arg.all_items") {
		target(2, 0)
	}

	if stardustVal, ok := parsed.Strings["stardust"]; ok {
		target(3, questParseInt(stardustVal))
	} else if parsed.HasKeyword("arg.stardust") {
		target(3, 0)
	}

	if pokecoinVal, ok := parsed.Strings["pokecoin"]; ok {
		target(8, questParseInt(pokecoinVal))
	} else if parsed.HasKeyword("arg.pokecoin") {
		target(8, 0)
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
			if len(mons) == 0 {
				// An unknown pokemon must not widen to "any pokemon".
				return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.unrecognized", kind.key+":"+val)}}
			}
		} else if parsed.HasKeyword("arg." + kind.key) {
			mons = parsed.Pokemon
			pokemonConsumed = pokemonConsumed || len(mons) > 0
		} else {
			continue
		}
		if len(mons) == 0 {
			target(kind.rewardType, 0)
		}
		for _, mon := range mons {
			target(kind.rewardType, mon.PokemonID)
		}
	}

	if !pokemonConsumed {
		for _, mon := range parsed.Pokemon {
			target(7, mon.PokemonID)
		}
	}

	// Item rewards (e.g. !quest remove pinap). Matched words are consumed so
	// only genuinely unknown ones are left for checkRemoveArgs to refuse.
	items, itemWords := c.matchItemNames(ctx, parsed)
	for _, itemID := range items {
		target(2, itemID)
	}

	if reply := checkRemoveArgs(ctx, parsed, questRemoveSpec, itemWords); reply != nil {
		return []bot.Reply{*reply}
	}

	// `summary` removes only summary-mode rules, leaving a parallel
	// immediate-mode rule for the same reward in place. Without it, rules
	// are removed whatever their clean / edit / summary bits.
	var keep func(*db.QuestTrackingAPI) bool
	if parsed.HasKeyword("arg.summary") {
		keep = func(r *db.QuestTrackingAPI) bool { return db.IsSummary(r.Clean) }
	}

	return ruleRemover[db.QuestTrackingAPI]{
		store:    ctx.Tracking.Quests,
		getUID:   store.QuestGetUID,
		describe: func(r *db.QuestTrackingAPI) string { return ctx.RowText.QuestRowText(tr, questAPIToTracking(r)) },
		logName:  "quest",
	}.run(ctx, parsed, func(r *db.QuestTrackingAPI) bool {
		for _, t := range targets {
			if r.RewardType == t.rewardType && (t.reward == 0 || r.Reward == t.reward) {
				return true
			}
		}
		return false
	}, keep)
}
