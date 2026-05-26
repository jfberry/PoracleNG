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
// Tracking-UID mutes (`!mute id:N`, `!mute raid id:N`) are documented in
// the design but not accepted by this command in v1 — they require
// MatchedUser to carry the rule UID for the matcher to enforce them,
// which is deferred to a follow-up (Phase 2.5). Accepting commands we
// can't enforce would be a worse UX than the current "not yet supported"
// message.
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
	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.mute.usage", bot.CommandPrefix(ctx))}}
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
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.mute.usage", bot.CommandPrefix(ctx))}}
	}

	scopeNoun := strings.ToLower(positional[0])

	// `!mute id:N` and `!mute <type> id:N` route through the tracking-UID
	// scope. Both forms come through here: `id:N` directly, or with a
	// tracking-type prefix that we strip before recursing into the UID
	// parse. The tracking type itself is informational — the UID is
	// unique across the user's tracking, so we don't need to scope to a
	// type at the mute level. (The render-side filter still gates by
	// MatchedRuleUID, so muting UID 12 only suppresses alerts from that
	// specific rule.)
	if strings.HasPrefix(scopeNoun, "id:") {
		return runMuteTrackingUID(ctx, scopeNoun[len("id:"):], duration)
	}
	if isTrackingTypeToken(scopeNoun) && len(positional) >= 2 && strings.HasPrefix(strings.ToLower(positional[1]), "id:") {
		return runMuteTrackingUID(ctx, positional[1][len("id:"):], duration)
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
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.mute.usage", bot.CommandPrefix(ctx))}}
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

// isTrackingTypeToken — set used to reject !mute <type> id:N (deferred).
// Mirror of validUntrackTypes in untrack.go.
func isTrackingTypeToken(s string) bool {
	switch s {
	case "raid", "egg", "quest", "invasion", "incident",
		"lure", "nest", "gym", "fort", "maxbattle":
		return true
	}
	return false
}

// runMuteTrackingUID: !mute id:N. Mutes a single tracking rule by UID.
// The filterMuted step (with mute.Event.MatchedRuleUID populated)
// suppresses alerts whose matched rule UID equals this value.
func runMuteTrackingUID(ctx *bot.CommandContext, raw string, duration time.Duration) []bot.Reply {
	tr := ctx.Tr()
	raw = strings.TrimSpace(raw)
	uid, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || uid <= 0 {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.mute.bad_uid", raw)}}
	}
	entry := mute.Entry{
		HumanID:    ctx.TargetID,
		ScopeType:  mute.ScopeTracking,
		ScopeValue: strconv.FormatInt(uid, 10),
		ExpiresAt:  time.Now().Add(duration).Unix(),
	}
	ctx.MuteStore.Add(entry)
	return []bot.Reply{{
		React: "🔇",
		Text:  tr.Tf("msg.mute.added", fmt.Sprintf("rule id:%d", uid), formatDuration(duration)),
	}}
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
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
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
	if len(args) == 0 {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.unmute.usage", bot.CommandPrefix(ctx))}}
	}

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
		return runUnmuteTrackingUID(ctx, scopeNoun[len("id:"):])
	}
	if isTrackingTypeToken(scopeNoun) && len(args) >= 2 && strings.HasPrefix(strings.ToLower(args[1]), "id:") {
		return runUnmuteTrackingUID(ctx, args[1][len("id:"):])
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

	removed := ctx.MuteStore.Remove(ctx.TargetID, scopeType, value)
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
func runUnmuteTrackingUID(ctx *bot.CommandContext, raw string) []bot.Reply {
	tr := ctx.Tr()
	raw = strings.TrimSpace(raw)
	uid, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || uid <= 0 {
		return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.mute.bad_uid", raw)}}
	}
	label := fmt.Sprintf("rule id:%d", uid)
	if ok := ctx.MuteStore.Remove(ctx.TargetID, mute.ScopeTracking, strconv.FormatInt(uid, 10)); !ok {
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
