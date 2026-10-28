package commands

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/mute"
)

// RouteToMuteFromType is the helper each per-type command file (raid,
// egg, quest, etc.) calls at the top of its Run when it sees `mute` or
// `unmute` as the first positional arg. Returns the routed reply when
// the reroute fires; returns nil to indicate the command should fall
// through to its normal handling.
//
// Mirrors what UntrackCommand does for the `!raid remove` ≡
// `!untrack raid` duality — the per-type form is a thin alias that
// prepends the type and delegates to the unified !mute / !unmute
// command.
func RouteToMuteFromType(ctx *bot.CommandContext, typeName string, args []string) []bot.Reply {
	if len(args) == 0 {
		return nil
	}
	switch strings.ToLower(args[0]) {
	case "mute":
		return routeMuteOrUnmute(ctx, "cmd.mute", typeName, args[1:])
	case "unmute":
		return routeMuteOrUnmute(ctx, "cmd.unmute", typeName, args[1:])
	}
	return nil
}

func routeMuteOrUnmute(ctx *bot.CommandContext, cmdKey, typeName string, rest []string) []bot.Reply {
	if ctx.Registry == nil {
		return []bot.Reply{{React: "🙅"}}
	}
	target := ctx.Registry.Lookup(cmdKey)
	if target == nil {
		return []bot.Reply{{React: "🙅"}}
	}
	return target.Run(ctx, append([]string{typeName}, rest...))
}

// MuteCommand implements !mute. The unified parser handles every form
// described in #109: entity scopes (gym/pokemon/area/pokestop/station),
// the self-mute special `!mute everything`, and the positional
// `!mute <pokemon-name>` shorthand. The `:` notation is reserved for
// parameters (`duration:1h`) — scope nouns are positional, matching the
// rest of the command vocabulary (`!raid gym:"X"` style is for filters
// inside a tracking command, not the scope of a !mute call).
//
// Tracking-UID mutes (`!mute id:N`, `!mute raid id:N`) mute a single
// rule; the matcher compares them against mute.Event.MatchedRuleUID.
type MuteCommand struct{}

func (c *MuteCommand) Name() string      { return "cmd.mute" }
func (c *MuteCommand) Aliases() []string { return nil }

// scope nouns recognised by the unified parser. Defined in lowercase;
// the parser compares lowercased input. Mirrors mute.Scope* constants.
var muteScopeNouns = map[string]string{
	"gym":        mute.ScopeGym,
	"pokemon":    mute.ScopePokemon,
	"area":       mute.ScopeArea,
	"pokestop":   mute.ScopePokestop,
	"station":    mute.ScopeStation,
	"everything": mute.ScopeEverything,
}

// defaultMuteDuration is the duration applied when the user omits a
// `duration:` argument. 1h is the documented default in
// docs/buttons-and-snapshots/IMPLEMENTATION.md.
const defaultMuteDuration = time.Hour

func (c *MuteCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	if ctx.MuteStore == nil {
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.mute.unavailable")}}
	}
	if help := helpArgReply(ctx, args, "msg.mute.usage"); help != nil {
		return []bot.Reply{*help}
	}
	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: inlineUsage(ctx, "msg.mute.usage")}}
	}

	// Pull out duration:X anywhere in the args; what's left is positional.
	positional, duration, err := extractMuteDuration(args)
	if err != nil {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.mute.bad_duration", err.Error())}}
	}
	if duration == 0 {
		duration = defaultMuteDuration
	}
	if len(positional) == 0 {
		return []bot.Reply{{React: "🙅", Text: inlineUsage(ctx, "msg.mute.usage")}}
	}

	typeHint := ruleTypeHint(positional)
	positional = stripMuteTypePrefix(positional)
	scopeNoun := strings.ToLower(positional[0])

	// `!mute id:N` (and `!mute <type> id:N`, whose type prefix was just
	// stripped) route through the tracking-UID scope. uids are per table
	// (#323), so the mute is typed: the type comes from the alias that was
	// used (`!raid mute id:N`) or, for a bare `!mute id:N`, from the one
	// table of the target's current profile holding that uid.
	if strings.HasPrefix(scopeNoun, "id:") {
		return runMuteTrackingUID(ctx, scopeNoun[len("id:"):], typeHint, duration)
	}

	scopeType, knownScope := muteScopeNouns[scopeNoun]
	if !knownScope {
		// Positional shorthand: !mute <pokemon-name> ≡ !mute pokemon <name>.
		scopeType = mute.ScopePokemon
		positional = append([]string{"pokemon"}, positional...)
	}

	switch scopeType {
	case mute.ScopeEverything:
		return runMuteEverything(ctx, duration)
	case mute.ScopePokemon:
		return runMutePokemon(ctx, positional[1:], duration)
	case mute.ScopeGym:
		return runMuteGym(ctx, positional[1:], duration)
	case mute.ScopeArea:
		return runMuteArea(ctx, positional[1:], duration)
	case mute.ScopePokestop:
		return runMuteIDScope(ctx, mute.ScopePokestop, "pokestop", positional[1:], duration)
	case mute.ScopeStation:
		return runMuteIDScope(ctx, mute.ScopeStation, "station", positional[1:], duration)
	default:
		return []bot.Reply{{React: "🙅", Text: inlineUsage(ctx, "msg.mute.usage")}}
	}
}

// extractMuteDuration removes the first `duration:X` token from args and
// returns the remaining positional tokens plus the parsed duration. Zero
// duration means "no flag present" — the caller substitutes the default.
//
// Accepts plain Go duration strings: "1h", "30m", "2d" (the "d" suffix
// expands to 24h since Go's time.ParseDuration doesn't know about days).
func extractMuteDuration(args []string) ([]string, time.Duration, error) {
	var positional []string
	var d time.Duration
	for _, a := range args {
		if !strings.HasPrefix(strings.ToLower(a), "duration:") {
			positional = append(positional, a)
			continue
		}
		raw := strings.TrimSpace(a[len("duration:"):])
		if raw == "" {
			return nil, 0, fmt.Errorf("duration: requires a value (e.g. duration:30m)")
		}
		parsed, err := parseDurationFlex(raw)
		if err != nil {
			return nil, 0, err
		}
		d = parsed
	}
	return positional, d, nil
}

// parseDurationFlex accepts Go's time.ParseDuration forms ("1h", "30m")
// plus a trailing "d" suffix that expands to 24h multiples ("7d" → 168h).
func parseDurationFlex(raw string) (time.Duration, error) {
	if strings.HasSuffix(raw, "d") {
		n, err := strconv.Atoi(raw[:len(raw)-1])
		if err != nil || n <= 0 {
			return 0, fmt.Errorf("invalid duration %q", raw)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	return time.ParseDuration(raw)
}

// isTrackingTypeToken reports whether s is a tracking type a per-type
// alias can prepend: validUntrackTypes in untrack.go, plus "pokemon",
// which !track routes as.
func isTrackingTypeToken(s string) bool {
	switch s {
	case "pokemon", "raid", "egg", "quest", "invasion", "incident",
		"lure", "nest", "gym", "fort", "maxbattle", "tappable":
		return true
	}
	return false
}

// stripMuteTypePrefix drops the tracking-type token that the per-type
// aliases prepend (`!raid mute gym X` runs `!mute raid gym X`), so the
// rest parses exactly like a direct !mute / !unmute. The type is kept
// when it is itself the scope: `!mute gym X` and `!mute pokemon pikachu`
// (also what `!gym mute X` and `!track mute pikachu` become).
func stripMuteTypePrefix(positional []string) []string {
	if len(positional) < 2 || !isTrackingTypeToken(strings.ToLower(positional[0])) {
		return positional
	}
	second := strings.ToLower(positional[1])
	if strings.HasPrefix(second, "id:") {
		return positional[1:]
	}
	if _, ok := muteScopeNouns[second]; ok {
		return positional[1:]
	}
	if _, ok := muteScopeNouns[strings.ToLower(positional[0])]; ok {
		return positional
	}
	// `!raid mute pikachu` — positional pokemon shorthand after the type.
	return positional[1:]
}

// ruleTypeHint returns the canonical rule table named by a leading
// tracking-type token directly before an id: argument (`raid id:5`, as left
// by the per-type `!raid mute id:5` alias), or "" when there is none.
func ruleTypeHint(positional []string) string {
	if len(positional) < 2 || !strings.HasPrefix(strings.ToLower(positional[1]), "id:") {
		return ""
	}
	table, _ := mute.CanonicalRuleTable(strings.ToLower(positional[0]))
	return table
}

// ruleCommandName is the command that addresses a rule table (`!track` for
// pokemon rules, `!<table>` otherwise), for the "use e.g. ..." hint.
func ruleCommandName(table string) string {
	if table == "pokemon" {
		return "track"
	}
	return table
}

// runMuteTrackingUID: !mute id:N. Mutes a single tracking rule by UID.
// The filterMuted step (with mute.Event.MatchedRuleUID/Table populated)
// suppresses alerts whose matched rule is exactly this (table, uid).
//
// table is the alias-supplied rule table, or "" for a bare `!mute id:N`, in
// which case the uid is looked up in the target's CURRENT profile (like
// `!untrack id:`) and refused when it is missing or matches several tables.
func runMuteTrackingUID(ctx *bot.CommandContext, raw, table string, duration time.Duration) []bot.Reply {
	tr := ctx.Tr()
	raw = strings.TrimSpace(raw)
	uid, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || uid <= 0 {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.mute.bad_uid", raw)}}
	}
	var tables []string
	if ctx.Tracking != nil {
		tables = ctx.Tracking.TablesWithUID(ctx.TargetID, ctx.ProfileNo, uid)
	}
	if table != "" {
		// An alias named the table: the rule must be there. (Without
		// tracking stores wired the claim cannot be checked; trust it.)
		if ctx.Tracking != nil && !containsString(tables, table) {
			return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.mute.uid_not_found", uid)}}
		}
	} else {
		switch len(tables) {
		case 0:
			return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.mute.uid_not_found", uid)}}
		case 1:
			table = tables[0]
		default:
			return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.mute.uid_ambiguous", uid, strings.Join(tables, ", "), ruleCommandName(tables[0]))}}
		}
	}
	entry := mute.Entry{
		HumanID:    ctx.TargetID,
		ScopeType:  mute.ScopeTracking,
		RuleTable:  table,
		ScopeValue: strconv.FormatInt(uid, 10),
		ExpiresAt:  time.Now().Add(duration).Unix(),
	}
	ctx.MuteStore.Add(entry)
	return []bot.Reply{{
		React: "🔇",
		Text:  tr.Tf("msg.mute.added", tr.Tf("msg.mute.rule_label", table, uid), formatDuration(duration)),
	}}
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// runMuteEverything: self-mute all alerts.
func runMuteEverything(ctx *bot.CommandContext, duration time.Duration) []bot.Reply {
	tr := ctx.Tr()
	entry := mute.Entry{
		HumanID:   ctx.TargetID,
		ScopeType: mute.ScopeEverything,
		ExpiresAt: time.Now().Add(duration).Unix(),
	}
	ctx.MuteStore.Add(entry)
	return []bot.Reply{{
		React: "🔇",
		Text:  tr.Tf("msg.mute.added", tr.T("msg.mute.everything_label"), formatDuration(duration)),
	}}
}

// runMutePokemon: !mute pokemon <name|id> — uses the existing pokemon
// resolver so aliases and translations Just Work.
func runMutePokemon(ctx *bot.CommandContext, args []string, duration time.Duration) []bot.Reply {
	tr := ctx.Tr()
	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.mute.usage_pokemon", bot.CommandPrefix(ctx))}}
	}
	raw := strings.Join(args, " ")
	pokemonID, name := resolveMutePokemon(ctx, raw)
	if pokemonID == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.mute.no_pokemon", raw)}}
	}
	entry := mute.Entry{
		HumanID:    ctx.TargetID,
		ScopeType:  mute.ScopePokemon,
		ScopeValue: strconv.Itoa(pokemonID),
		ExpiresAt:  time.Now().Add(duration).Unix(),
	}
	ctx.MuteStore.Add(entry)
	return []bot.Reply{{
		React: "🔇",
		Text:  tr.Tf("msg.mute.added", name, formatDuration(duration)),
	}}
}

// runMuteGym: !mute gym <name|id> — reuses the existing resolver from raid.
func runMuteGym(ctx *bot.CommandContext, args []string, duration time.Duration) []bot.Reply {
	tr := ctx.Tr()
	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.mute.usage_gym", bot.CommandPrefix(ctx))}}
	}
	raw := strings.Join(args, " ")
	id, abort := resolveGymRef(ctx, raw)
	if abort != nil {
		return []bot.Reply{*abort}
	}
	entry := mute.Entry{
		HumanID:    ctx.TargetID,
		ScopeType:  mute.ScopeGym,
		ScopeValue: id,
		ExpiresAt:  time.Now().Add(duration).Unix(),
	}
	ctx.MuteStore.Add(entry)
	return []bot.Reply{{
		React: "🔇",
		Text:  tr.Tf("msg.mute.added", muteGymLabel(raw, id), formatDuration(duration)),
	}}
}

// runMuteArea: !mute area <name>. Validates the name against the user's
// available areas (same set !area uses) so a typo doesn't silently
// produce a never-matching mute.
func runMuteArea(ctx *bot.CommandContext, args []string, duration time.Duration) []bot.Reply {
	tr := ctx.Tr()
	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.mute.usage_area", bot.CommandPrefix(ctx))}}
	}
	raw := strings.Join(args, " ")

	display := raw
	if ctx.AreaLogic != nil {
		var isAdmin bool
		var communities []string
		if h := getUserHuman(ctx); h != nil {
			communities = h.CommunityMembership
			isAdmin = ctx.IsAdmin
		}
		resolved, ok := ctx.AreaLogic.ResolveAvailableArea(raw, communities, isAdmin)
		if !ok {
			return []bot.Reply{{
				React: "🙅",
				Text:  tr.Tf("msg.mute.unknown_area", raw),
			}}
		}
		display = resolved
	}

	entry := mute.Entry{
		HumanID:    ctx.TargetID,
		ScopeType:  mute.ScopeArea,
		ScopeValue: display, // store the display-cased name for clean !tracked output
		ExpiresAt:  time.Now().Add(duration).Unix(),
	}
	ctx.MuteStore.Add(entry)
	return []bot.Reply{{
		React: "🔇",
		Text:  tr.Tf("msg.mute.added", display, formatDuration(duration)),
	}}
}

// runMuteIDScope: pokestop / station — the value is taken as-is. These
// are opaque hex IDs from webhooks, not user-friendly names.
func runMuteIDScope(ctx *bot.CommandContext, scope, label string, args []string, duration time.Duration) []bot.Reply {
	tr := ctx.Tr()
	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.mute.usage_id_scope", bot.CommandPrefix(ctx), label)}}
	}
	id := strings.TrimSpace(strings.Join(args, " "))
	if id == "" {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.mute.usage_id_scope", bot.CommandPrefix(ctx), label)}}
	}
	entry := mute.Entry{
		HumanID:    ctx.TargetID,
		ScopeType:  scope,
		ScopeValue: id,
		ExpiresAt:  time.Now().Add(duration).Unix(),
	}
	ctx.MuteStore.Add(entry)
	return []bot.Reply{{
		React: "🔇",
		Text:  tr.Tf("msg.mute.added", fmt.Sprintf("%s %s", label, id), formatDuration(duration)),
	}}
}

// resolveMutePokemon attempts to resolve a name to a single pokemon ID.
// Returns (id, displayName) or (0, "") on miss. Numeric inputs bypass the
// resolver — `!mute pokemon 25` should always work even if "25" isn't a
// valid name in any language.
func resolveMutePokemon(ctx *bot.CommandContext, raw string) (int, string) {
	raw = strings.TrimSpace(raw)
	if n, err := strconv.Atoi(raw); err == nil && n > 0 {
		return n, raw
	}
	if ctx.Resolver == nil {
		return 0, ""
	}
	results := ctx.Resolver.Resolve(raw, ctx.Language)
	if len(results) == 0 {
		return 0, ""
	}
	// Picking the first result mirrors what !track does for ambiguous
	// names (alphabetically-first match wins).
	return results[0].PokemonID, raw
}

// muteGymLabel: when the user typed a name and the resolver returned an
// id, show the name in the confirmation. When they typed an id directly,
// show that id. Avoids inflicting hex IDs on users who used names.
func muteGymLabel(input, resolved string) string {
	if input == resolved {
		return input
	}
	if looksLikeGymID(input) {
		return input
	}
	return fmt.Sprintf("%s (%s)", input, resolved)
}

// looksLikeGymID — gym IDs are 32-char lowercase hex with a `.16` suffix.
// Used by muteGymLabel to decide whether to show the user-typed value as
// a hex id or a friendly name.
func looksLikeGymID(s string) bool {
	if !strings.HasSuffix(s, ".16") {
		return false
	}
	body := s[:len(s)-3]
	if len(body) != 32 {
		return false
	}
	for _, r := range body {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// formatDuration: defined in poracle_admin_util.go ("2d 3h 14m" /
// "3h 14m 12s" / "12s"). Reused here to keep the operator-facing
// formatting consistent across commands.

// UnmuteCommand implements !unmute. Symmetric to !mute: same scope parsing
// + value matching, but writes a remove instead of an add. Special cases
// `!unmute all` and `!unmute everything` to drop every entry for the
// caller.
type UnmuteCommand struct{}

func (c *UnmuteCommand) Name() string      { return "cmd.unmute" }
func (c *UnmuteCommand) Aliases() []string { return nil }

func (c *UnmuteCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
	tr := ctx.Tr()
	if ctx.MuteStore == nil {
		return []bot.Reply{{React: "🙅", Text: tr.T("msg.mute.unavailable")}}
	}
	if help := helpArgReply(ctx, args, "msg.unmute.usage"); help != nil {
		return []bot.Reply{*help}
	}
	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: inlineUsage(ctx, "msg.unmute.usage")}}
	}

	typeHint := ruleTypeHint(args)
	args = stripMuteTypePrefix(args)

	// Special tokens that drop everything for this user.
	first := strings.ToLower(args[0])
	if first == "all" {
		n := ctx.MuteStore.RemoveAll(ctx.TargetID)
		return []bot.Reply{{
			React: "✅",
			Text:  tr.Tf("msg.mute.cleared_all", n),
		}}
	}
	if first == "everything" && len(args) == 1 {
		// `!unmute everything` is an alias for `!unmute all`. The
		// alias only triggers when there's no further value — `!unmute
		// everything` removes the self-mute (ScopeEverything entry),
		// which is consistent (clearing the everything mute also
		// clears everything else).
		n := ctx.MuteStore.RemoveAll(ctx.TargetID)
		return []bot.Reply{{
			React: "✅",
			Text:  tr.Tf("msg.mute.cleared_all", n),
		}}
	}

	scopeNoun := first

	// `!unmute id:N` / `!unmute <type> id:N`: remove the tracking-UID mute.
	if strings.HasPrefix(scopeNoun, "id:") {
		return runUnmuteTrackingUID(ctx, scopeNoun[len("id:"):], typeHint)
	}

	scopeType, knownScope := muteScopeNouns[scopeNoun]
	if !knownScope {
		// Positional shorthand: `!unmute pikachu` ≡ `!unmute pokemon pikachu`.
		scopeType = mute.ScopePokemon
		args = append([]string{"pokemon"}, args...)
	}

	value, label, abort := resolveUnmuteValue(ctx, scopeType, args[1:])
	if abort != nil {
		return []bot.Reply{*abort}
	}

	if scopeType == mute.ScopeEverything {
		// `!unmute everything <something>` — invalid; the everything
		// scope has no value. Treat as clear-all so the user gets the
		// expected outcome.
		n := ctx.MuteStore.RemoveAll(ctx.TargetID)
		return []bot.Reply{{
			React: "✅",
			Text:  tr.Tf("msg.mute.cleared_all", n),
		}}
	}

	removed := ctx.MuteStore.Remove(ctx.TargetID, scopeType, "", value)
	if !removed {
		return []bot.Reply{{
			React: "👌",
			Text:  tr.Tf("msg.mute.none_found", label),
		}}
	}
	return []bot.Reply{{
		React: "✅",
		Text:  tr.Tf("msg.mute.removed", label),
	}}
}

// runUnmuteTrackingUID: !unmute id:N. Symmetric with runMuteTrackingUID.
// With a type (`!raid unmute id:N`) it removes that table's mute. Bare, it
// resolves the table from the target's existing tracking mutes for that uid:
// exactly one is removed, none is "no active mute", several are refused (name
// the type). This reads the mute store rather than the rule tables so a mute
// whose rule has since been deleted can still be lifted.
func runUnmuteTrackingUID(ctx *bot.CommandContext, raw, table string) []bot.Reply {
	tr := ctx.Tr()
	raw = strings.TrimSpace(raw)
	uid, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || uid <= 0 {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.mute.bad_uid", raw)}}
	}
	value := strconv.FormatInt(uid, 10)
	if table == "" {
		var muted []string
		for _, e := range ctx.MuteStore.List(ctx.TargetID) {
			if e.ScopeType == mute.ScopeTracking && e.ScopeValue == value && e.RuleTable != "" {
				muted = append(muted, e.RuleTable)
			}
		}
		switch len(muted) {
		case 0:
			return []bot.Reply{{React: "👌", Text: tr.Tf("msg.mute.none_found", fmt.Sprintf("rule id:%d", uid))}}
		case 1:
			table = muted[0]
		default:
			return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.unmute.uid_ambiguous", uid, strings.Join(muted, ", "), ruleCommandName(muted[0]))}}
		}
	}
	label := tr.Tf("msg.mute.rule_label", table, uid)
	if ok := ctx.MuteStore.Remove(ctx.TargetID, mute.ScopeTracking, table, value); !ok {
		return []bot.Reply{{React: "👌", Text: tr.Tf("msg.mute.none_found", label)}}
	}
	return []bot.Reply{{React: "✅", Text: tr.Tf("msg.mute.removed", label)}}
}

// resolveUnmuteValue maps the (scope, args) pair to the ScopeValue stored
// in the mute store. Symmetric with the mute side — same resolvers, same
// label conventions for the confirmation message.
func resolveUnmuteValue(ctx *bot.CommandContext, scopeType string, args []string) (value, label string, abort *bot.Reply) {
	tr := ctx.Tr()
	switch scopeType {
	case mute.ScopePokemon:
		if len(args) == 0 {
			r := bot.Reply{React: "🙅", Text: tr.Tf("msg.unmute.usage_pokemon", bot.CommandPrefix(ctx))}
			return "", "", &r
		}
		raw := strings.Join(args, " ")
		id, name := resolveMutePokemon(ctx, raw)
		if id == 0 {
			r := bot.Reply{React: "🙅", Text: tr.Tf("msg.mute.no_pokemon", raw)}
			return "", "", &r
		}
		return strconv.Itoa(id), name, nil
	case mute.ScopeGym:
		if len(args) == 0 {
			r := bot.Reply{React: "🙅", Text: tr.Tf("msg.unmute.usage_gym", bot.CommandPrefix(ctx))}
			return "", "", &r
		}
		raw := strings.Join(args, " ")
		id, abort := resolveGymRef(ctx, raw)
		if abort != nil {
			return "", "", abort
		}
		return id, muteGymLabel(raw, id), nil
	case mute.ScopeArea:
		if len(args) == 0 {
			r := bot.Reply{React: "🙅", Text: tr.Tf("msg.unmute.usage_area", bot.CommandPrefix(ctx))}
			return "", "", &r
		}
		raw := strings.Join(args, " ")
		return raw, raw, nil
	case mute.ScopePokestop, mute.ScopeStation:
		if len(args) == 0 {
			r := bot.Reply{React: "🙅", Text: tr.Tf("msg.mute.usage_id_scope", bot.CommandPrefix(ctx), scopeType)}
			return "", "", &r
		}
		raw := strings.TrimSpace(strings.Join(args, " "))
		return raw, raw, nil
	case mute.ScopeEverything:
		return "", "everything", nil
	default:
		log.Warnf("unmute: unknown scope %q", scopeType)
		r := bot.Reply{React: "🙅"}
		return "", "", &r
	}
}
