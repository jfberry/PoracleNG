package commands

import (
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// Every tracking command's remove path follows the same rules, enforced by
// checkRemoveArgs and removeRules:
//
//   - Unrecognised words are refused before anything is removed.
//   - `id:N` stands alone: it names exact rules, so any other argument beside
//     it is refused.
//   - An argument the remove doesn't use is refused rather than ignored, so
//     `!raid remove level5 mystic` can't silently remove the level-5 rules
//     of every team.
//   - A remove has to name what it removes. A bare `remove` is refused;
//     `everything` removes every rule of the type, whatever values it holds.

// removeSpec declares which of a command's params its remove honours.
// arg.remove, arg.everything and id:N are always honoured.
type removeSpec struct {
	// selectors name rules to remove; a remove needs at least one of
	// these (or everything, or id:N).
	selectors []bot.ParamDef
	// filters narrow the selected rules but can't select on their own.
	filters []bot.ParamDef
}

func containsParam(params []bot.ParamDef, p bot.ParamDef) bool {
	for _, q := range params {
		if q == p {
			return true
		}
	}
	return false
}

// checkRemoveArgs is the gate every remove path goes through. Commands that
// read selectors out of the unrecognised words (quest item names, invasion
// grunt types) must consume them first and pass the words they consumed as
// leftoverSelectors. Returns nil when the remove may go ahead.
func checkRemoveArgs(ctx *bot.CommandContext, parsed *bot.ParsedArgs, spec removeSpec, leftoverSelectors []string) *bot.Reply {
	tr := ctx.Tr()
	if warn := bot.ReportUnrecognized(parsed, tr); warn != nil {
		return warn
	}

	remove := bot.ParamDef{Type: bot.ParamKeyword, Key: "arg.remove"}
	everything := bot.ParamDef{Type: bot.ParamKeyword, Key: "arg.everything"}
	byUID := len(parsed.RemoveUIDs) > 0

	named := len(leftoverSelectors) > 0
	var unused []string
	if byUID {
		unused = append(unused, leftoverSelectors...)
	}
	for _, m := range parsed.Matched {
		switch {
		case m.Param == remove || m.Param.Type == bot.ParamRemoveUID:
		case byUID:
			unused = append(unused, m.Token)
		case m.Param == everything:
			named = true
		case containsParam(spec.selectors, m.Param):
			named = true
		case containsParam(spec.filters, m.Param):
		default:
			unused = append(unused, m.Token)
		}
	}
	if len(unused) > 0 {
		return &bot.Reply{React: "🙅", Text: tr.Tf("msg.remove.not_used", strings.Join(unused, ", "))}
	}
	if !byUID && !named {
		return &bot.Reply{React: "🙅", Text: tr.Tf("msg.remove.name_what", tr.T("arg.everything"))}
	}
	return nil
}

// ruleRemover removes one tracking type's rules for the command's target
// and profile.
type ruleRemover[T any] struct {
	store    store.TrackingStore[T]
	getUID   func(*T) int64
	describe func(*T) string
	logName  string
}

// run removes the rules picked by parsed: the id:N rules when given,
// otherwise every rule (with everything) or those selects reports, then
// narrowed by keep when non-nil. Call checkRemoveArgs first.
func (r ruleRemover[T]) run(ctx *bot.CommandContext, parsed *bot.ParsedArgs, selects, keep func(*T) bool) []bot.Reply {
	if len(parsed.RemoveUIDs) > 0 {
		return removeByUIDs(ctx, r.store, parsed.RemoveUIDs, r.getUID, r.describe)
	}
	tr := ctx.Tr()
	everything := parsed.HasKeyword("arg.everything")

	tracked, err := r.store.SelectByIDProfile(ctx.TargetID, ctx.ProfileNo)
	if err != nil {
		log.Errorf("%s command: select for remove: %s", r.logName, err)
		return []bot.Reply{{React: "🙅"}}
	}

	var uids []int64
	var descriptions []string
	for i := range tracked {
		row := &tracked[i]
		if !everything && !selects(row) {
			continue
		}
		if keep != nil && !keep(row) {
			continue
		}
		uids = append(uids, r.getUID(row))
		descriptions = append(descriptions, r.describe(row))
	}
	if len(uids) == 0 {
		return []bot.Reply{{React: "👌", Text: tr.T("msg.nothing_to_remove")}}
	}
	if err := r.store.DeleteByUIDs(ctx.TargetID, uids); err != nil {
		log.Errorf("%s command: delete: %s", r.logName, err)
		return []bot.Reply{{React: "🙅"}}
	}
	ctx.TriggerReload()
	return []bot.Reply{{React: "✅", Text: formatRemovedRows(tr, descriptions)}}
}

// Param shorthands for removeSpec declarations.
func kw(key string) bot.ParamDef { return bot.ParamDef{Type: bot.ParamKeyword, Key: key} }

var (
	levelParam     = bot.ParamDef{Type: bot.ParamPrefixRange, Key: "arg.prefix.level"}
	pokemonParam   = bot.ParamDef{Type: bot.ParamPokemonName}
	typeNameParam  = bot.ParamDef{Type: bot.ParamTypeName}
	raidLevelParam = bot.ParamDef{Type: bot.ParamRaidLevelName}
	teamParam      = bot.ParamDef{Type: bot.ParamTeam}
	genderParam    = bot.ParamDef{Type: bot.ParamGender}
	lureTypeParam  = bot.ParamDef{Type: bot.ParamLureType}
	genParam       = bot.ParamDef{Type: bot.ParamPrefixSingle, Key: "arg.prefix.gen"}
	costumeParam   = bot.ParamDef{Type: bot.ParamPrefixString, Key: "arg.prefix.costume"}
)

// rangeMatches reports whether v is in the level range parsed from
// level<N> or level<N>-<M>.
func rangeMatches(r bot.Range, v int) bool {
	if r.HasMax {
		return v >= r.Min && v <= r.Max
	}
	return v == r.Min
}

// genTypeKeep narrows a removal to rules whose pokemon is in the gen: and
// type given, or returns nil when neither was. A wildcard (pokemon 0) rule
// is in no generation or type, so it is kept.
func genTypeKeep(ctx *bot.CommandContext, parsed *bot.ParsedArgs) func(pokemonID, form int) bool {
	if !hasGenOrTypeFilter(parsed) {
		return nil
	}
	return func(pokemonID, form int) bool {
		return len(filterByGenAndType(ctx, []bot.ResolvedPokemon{{PokemonID: pokemonID, Form: form}}, parsed)) > 0
	}
}
