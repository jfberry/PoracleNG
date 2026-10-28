package commands

import (
	"strconv"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// TappableCommand implements !tappable — track reward tappables (boxes and
// other map objects that give candy, items, stardust... when tapped).
//
// Grammar mirrors !quest without pokemon-encounter rewards, shiny, form,
// summary and edit: stardust[:N], pokecoins[:N], candy[:pokemon],
// xlcandy[:pokemon], energy[:pokemon], item names, `all items`,
// `everything`, amount:N. A bare pokemon name means candy for that pokemon.
type TappableCommand struct{}

func (c *TappableCommand) Name() string      { return "cmd.tappable" }
func (c *TappableCommand) Aliases() []string { return nil }

// tappableParams deliberately omits edit, summary, shiny and form: tappables
// support none of them, so those tokens surface as unrecognized.
var tappableParams = []bot.ParamDef{
	{Type: bot.ParamRemoveUID},
	{Type: bot.ParamPrefixSingle, Key: "arg.prefix.d"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.template"},
	{Type: bot.ParamPrefixString, Key: "arg.prefix.stardust"}, // stardust:500 (min amount)
	{Type: bot.ParamPrefixString, Key: "arg.prefix.pokecoin"}, // pokecoins:10 (min amount)
	{Type: bot.ParamPrefixString, Key: "arg.prefix.energy"},   // energy:charizard
	{Type: bot.ParamPrefixString, Key: "arg.prefix.xl_candy"}, // xlcandy:pikachu
	{Type: bot.ParamPrefixString, Key: "arg.prefix.candy"},    // candy:pikachu
	{Type: bot.ParamPrefixString, Key: "arg.prefix.amount"},   // amount:N
	{Type: bot.ParamPrefixString, Key: "arg.prefix.location"},
	{Type: bot.ParamPrefixStringList, Key: "arg.prefix.area"},
	{Type: bot.ParamKeyword, Key: "arg.remove"},
	{Type: bot.ParamKeyword, Key: "arg.everything"},
	{Type: bot.ParamKeyword, Key: "arg.all_items"},
	{Type: bot.ParamKeyword, Key: "arg.clean"},
	{Type: bot.ParamKeyword, Key: "arg.stardust"},
	{Type: bot.ParamKeyword, Key: "arg.pokecoin"},
	{Type: bot.ParamKeyword, Key: "arg.energy"},
	{Type: bot.ParamKeyword, Key: "arg.xl_candy"},
	{Type: bot.ParamKeyword, Key: "arg.candy"},
	{Type: bot.ParamPokemonName},
}

// tappableAllRewardTypes is every reward type a tappable rule can track, in
// the order `everything` creates them.
var tappableAllRewardTypes = []int{
	matching.TappableRewardCandy,
	matching.TappableRewardXLCandy,
	matching.TappableRewardMegaEnergy,
	matching.TappableRewardItem,
	matching.TappableRewardStardust,
	matching.TappableRewardPokecoin,
}

// tappablePokemonKinds are the reward types whose reward column is a
// pokemon id, with their `kind:pokemon` / bare keyword name.
var tappablePokemonKinds = []struct {
	rewardType int
	key        string
}{
	{matching.TappableRewardMegaEnergy, "energy"},
	{matching.TappableRewardXLCandy, "xl_candy"},
	{matching.TappableRewardCandy, "candy"},
}

// tappableReward is one reward named in a command: the reward_type and
// reward columns of the rule it becomes.
type tappableReward struct {
	rewardType int
	reward     int
}

// tappableRewards collects every reward named in the command. The returned
// bool is false when the command named only rewards whose minimum is fixed
// in the reward column (explicit stardust:N / pokecoins:N), so amount:N
// would be ignored. Item names are matched against the unrecognized tokens,
// which are consumed on a match.
func tappableRewards(ctx *bot.CommandContext, parsed *bot.ParsedArgs, amount int) ([]tappableReward, bool) {
	var out []tappableReward
	amountUsed := false

	if parsed.HasKeyword("arg.all_items") {
		out = append(out, tappableReward{matching.TappableRewardItem, 0})
		amountUsed = true
	}

	// Stardust and pokecoins keep their minimum in reward (quest quirk): an
	// explicit stardust:N wins, a bare keyword takes amount:N.
	for _, kind := range []struct {
		rewardType int
		key        string
	}{{matching.TappableRewardStardust, "stardust"}, {matching.TappableRewardPokecoin, "pokecoin"}} {
		if val, ok := parsed.Strings[kind.key]; ok {
			n, _ := tappableNumber(val) // validated by invalidTappableNumber
			out = append(out, tappableReward{kind.rewardType, n})
		} else if parsed.HasKeyword("arg." + kind.key) {
			out = append(out, tappableReward{kind.rewardType, amount})
			amountUsed = true
		}
	}

	// energy / xlcandy / candy: kind:pokemon names the pokemon; a bare
	// keyword uses the pokemon named elsewhere in the command, or any
	// pokemon when none is. Pokemon taken this way are not also candy.
	pokemonConsumed := false
	// kind:<name> that resolves to no pokemon is reported as unrecognized
	// rather than widened to "any pokemon" (which, on remove, would delete
	// every rule of that kind). Added after item matching, so the full-phrase
	// item match only ever sees the bare tokens.
	var unresolved []string
	for _, kind := range tappablePokemonKinds {
		var mons []bot.ResolvedPokemon
		if val, ok := parsed.Strings[kind.key]; ok {
			mons = ctx.Resolver.Resolve(val, ctx.Language)
			if len(mons) == 0 {
				unresolved = append(unresolved, kind.key+":"+val)
				continue
			}
		} else if parsed.HasKeyword("arg." + kind.key) {
			mons = parsed.Pokemon
			pokemonConsumed = pokemonConsumed || len(mons) > 0
		} else {
			continue
		}
		amountUsed = true
		if len(mons) == 0 {
			out = append(out, tappableReward{kind.rewardType, 0})
		}
		out = appendTappablePokemon(out, kind.rewardType, mons)
	}

	// A bare pokemon name means candy for that pokemon.
	if len(parsed.Pokemon) > 0 && !pokemonConsumed {
		out = appendTappablePokemon(out, matching.TappableRewardCandy, parsed.Pokemon)
		amountUsed = true
	}

	if itemID, itemTokens := matchItemName(ctx, parsed); itemID > 0 {
		consumeUnrecognized(parsed, itemTokens)
		out = append(out, tappableReward{matching.TappableRewardItem, itemID})
		amountUsed = true
	}
	parsed.Unrecognized = append(parsed.Unrecognized, unresolved...)

	return out, amountUsed
}

// tappableNumberKeys are the prefixes whose value is a minimum amount.
var tappableNumberKeys = []string{"stardust", "pokecoin", "amount"}

// tappableNumber parses a non-negative whole number (digits only).
func tappableNumber(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}

// invalidTappableNumber returns the first stardust: / pokecoins: / amount:
// argument whose value isn't a non-negative whole number ("" when all are).
func invalidTappableNumber(parsed *bot.ParsedArgs) string {
	for _, key := range tappableNumberKeys {
		if val, ok := parsed.Strings[key]; ok {
			if _, valid := tappableNumber(val); !valid {
				return key + ":" + val
			}
		}
	}
	return ""
}

// appendTappablePokemon adds one reward per distinct pokemon id: tappable
// rewards have no form, so the resolver's per-form entries collapse.
func appendTappablePokemon(out []tappableReward, rewardType int, mons []bot.ResolvedPokemon) []tappableReward {
	seen := make(map[int]bool, len(mons))
	for _, mon := range mons {
		if mon.PokemonID <= 0 || seen[mon.PokemonID] {
			continue
		}
		seen[mon.PokemonID] = true
		out = append(out, tappableReward{rewardType, mon.PokemonID})
	}
	return out
}

func (c *TappableCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()

	if reply := RouteToMuteFromType(ctx, "tappable", args); reply != nil {
		return reply
	}

	pings, args := extractPings(args)

	if usage := usageReply(ctx, args, "msg.tappable.usage"); usage != nil {
		return []bot.Reply{*usage}
	}
	if help := helpArgReply(ctx, args, "msg.tappable.usage"); help != nil {
		return []bot.Reply{*help}
	}

	if ctx.Config.General.DisableTappable {
		return []bot.Reply{{React: "\U0001f645", Text: "This alert type is disabled"}}
	}

	parsed := ctx.ArgMatcher.Match(args, tappableParams, ctx.Language)

	common, block := parseCommonTrackFields(ctx, parsed, "tappable")
	if block != nil {
		return []bot.Reply{*block}
	}

	override, overrideReply := parseOverride(ctx, parsed.Strings["location"], parsed.StringLists["area"], common.Distance)
	if overrideReply != nil {
		return []bot.Reply{*overrideReply}
	}

	// Checked before remove too: 0 is the wildcard, so a typo read as 0
	// would widen a rule on add and delete several on remove.
	if bad := invalidTappableNumber(parsed); bad != "" {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.tappable.invalid_number", bad)}}
	}

	if parsed.HasKeyword("arg.remove") {
		return c.handleRemove(ctx, parsed)
	}

	if (parsed.HasKeyword("arg.everything") || parsed.HasKeyword("arg.all_items")) && !questBulkAllowed(ctx) {
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.tappable.bulk_not_permitted")}}
	}

	amountStr, amountSet := parsed.Strings["amount"]
	amount := 0
	if amountSet {
		amount, _ = tappableNumber(amountStr) // validated by invalidTappableNumber
	}

	var rewards []tappableReward
	if parsed.HasKeyword("arg.everything") {
		// Every trackable reward type with any reward; amount:N applies
		// where the type has a minimum (stardust/pokecoin keep it in reward).
		for _, rt := range tappableAllRewardTypes {
			reward := 0
			if rt == matching.TappableRewardStardust || rt == matching.TappableRewardPokecoin {
				reward = amount
			}
			rewards = append(rewards, tappableReward{rt, reward})
		}
	} else {
		var amountUsed bool
		rewards, amountUsed = tappableRewards(ctx, parsed, amount)
		if amountSet && len(rewards) > 0 && !amountUsed {
			return []bot.Reply{{React: "🙅", Text: tr.T("msg.tappable.amount_not_applicable")}}
		}
	}

	if warn := bot.ReportUnrecognized(parsed, tr); warn != nil {
		return []bot.Reply{*warn}
	}
	if len(rewards) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.tappable.no_reward")}}
	}

	insert := make([]db.TappableTrackingAPI, 0, len(rewards))
	for _, r := range rewards {
		row := db.TappableTrackingAPI{
			ID:                    ctx.TargetID,
			ProfileNo:             ctx.ProfileNo,
			Ping:                  pings,
			Template:              common.Template,
			Distance:              common.Distance,
			Clean:                 common.Clean,
			RewardType:            r.rewardType,
			Reward:                r.reward,
			OverrideLocationLabel: override.LocationLabel,
			OverrideAreas:         override.Areas,
		}
		if r.rewardType != matching.TappableRewardStardust && r.rewardType != matching.TappableRewardPokecoin {
			row.Amount = amount
		}
		insert = append(insert, row)
	}

	tracked, err := ctx.Tracking.Tappables.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("tappable command: select existing: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	diff, err := store.ApplyDiff(ctx.Tracking.Tappables, ctx.TargetID, tracked, insert,
		store.TappableGetUID, store.TappableSetUID)
	if err != nil {
		log.Errorf("tappable command: apply diff: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	describe := func(r *db.TappableTrackingAPI) string {
		return ctx.RowText.TappableRowText(tr, tappableAPIToTracking(r))
	}
	message := buildTrackingMessage(tr, ctx, len(diff.AlreadyPresent), len(diff.Updates), len(diff.Inserts),
		func(i int) string { return describe(&diff.AlreadyPresent[i]) },
		func(i int) string { return describe(&diff.Updates[i]) },
		func(i int) string { return describe(&diff.Inserts[i]) },
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

// handleRemove removes the rules for every reward named in the command;
// with no reward named (or `everything`) it removes every tappable rule.
// A reward of 0 in the target removes every rule of that reward type.
func (c *TappableCommand) handleRemove(ctx *bot.CommandContext, parsed *bot.ParsedArgs) []bot.Reply {
	tr := ctx.Tr()

	if len(parsed.RemoveUIDs) > 0 {
		return removeByUIDs(ctx, ctx.Tracking.Tappables, parsed.RemoveUIDs,
			store.TappableGetUID,
			func(r *db.TappableTrackingAPI) string {
				return ctx.RowText.TappableRowText(tr, tappableAPIToTracking(r))
			},
		)
	}

	var targets []tappableReward
	if !parsed.HasKeyword("arg.everything") {
		targets, _ = tappableRewards(ctx, parsed, 0)
	}
	if warn := bot.ReportUnrecognized(parsed, tr); warn != nil {
		return []bot.Reply{*warn}
	}
	if len(targets) == 0 {
		for _, rt := range tappableAllRewardTypes {
			targets = append(targets, tappableReward{rt, 0})
		}
	}

	tracked, err := ctx.Tracking.Tappables.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("tappable command: select for remove: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}

	var uids []int64
	var removed []db.TappableTrackingAPI
	for _, existing := range tracked {
		for _, t := range targets {
			if existing.RewardType == t.rewardType && (t.reward == 0 || existing.Reward == t.reward) {
				uids = append(uids, existing.UID)
				removed = append(removed, existing)
				break
			}
		}
	}

	if len(uids) == 0 {
		return []bot.Reply{{React: "👌"}}
	}
	if err := ctx.Tracking.Tappables.DeleteByUIDs(ctx.TargetID, uids); err != nil {
		log.Errorf("tappable command: delete: %s", err)
		return []bot.Reply{{React: "🙅"}}
	}
	ctx.TriggerReload()

	descriptions := make([]string, 0, len(removed))
	for i := range removed {
		descriptions = append(descriptions, ctx.RowText.TappableRowText(tr, tappableAPIToTracking(&removed[i])))
	}
	return []bot.Reply{{React: "✅", Text: formatRemovedRows(tr, descriptions)}}
}
