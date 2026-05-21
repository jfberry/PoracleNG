package buttonactions

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/pokemon/poracleng/processor/internal/buttons"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/mute"
	"github.com/pokemon/poracleng/processor/internal/snapshots"
)

// muteDurationDefault matches the !mute command default (1h) so a button
// without an explicit duration_min produces the same mute as a bare
// `!mute gym X`. Operators can override per button via Params.
const muteDurationDefault = time.Hour

// HandleMute is the action handler for `action = "mute"`. Reads the
// snapshot's view to find the scope value (gym_id, pokemon_id, etc.) and
// writes a mute entry to the in-memory store. Returns an ephemeral
// confirmation describing what was muted and for how long.
//
// Errors when:
//   - deps.MuteStore is nil (snapshots enabled but mute store wasn't wired)
//   - def.Scope is unset or unrecognised
//   - the relevant scope value can't be found in the snapshot view
//
// All errors return an ephemeral message and a nil go-error, except for
// the "feature unavailable" case which surfaces the nil-store via the
// returned error so the dispatcher can log a louder warning. Operators
// shouldn't see a "feature unavailable" string unless their config is
// genuinely broken.
func HandleMute(_ context.Context, snap *snapshots.Snapshot, def buttons.Def, _ string, deps Deps) (Response, error) {
	tr := deps.Tr
	if deps.MuteStore == nil {
		return Response{
			Text:     tr.T("msg.mute.unavailable"),
			Reaction: "🙅",
		}, errors.New("buttonactions/mute: nil MuteStore in deps")
	}
	if snap == nil {
		return Response{Text: tr.T("msg.button.expired"), Reaction: "🙅"}, nil
	}

	scopeType := def.Scope
	if scopeType == "" {
		return Response{Text: tr.T("msg.button.missing_scope"), Reaction: "🙅"}, nil
	}

	scopeValue, ok := muteScopeValue(scopeType, snap)
	if !ok {
		return Response{
			Text:     tr.Tf("msg.button.missing_target", scopeNounLabel(tr, scopeType)),
			Reaction: "🙅",
		}, nil
	}

	duration := muteDurationFromParams(def.Params)
	entry := mute.Entry{
		HumanID:    snap.Target,
		ScopeType:  scopeType,
		ScopeValue: scopeValue,
		ExpiresAt:  time.Now().Add(duration).Unix(),
	}
	deps.MuteStore.Add(entry)

	return Response{
		Text:     tr.Tf("msg.mute.added", muteScopeLabel(tr, scopeType, scopeValue, snap), durationLabel(duration)),
		Reaction: "🔇",
	}, nil
}

// scopeNounLabel translates the scope type to its user-facing noun
// ("gym", "pokemon", "area", ...). Relies on the standard per-key
// English fallback in Translator.T — missing localisations cascade
// to the en.json string.
func scopeNounLabel(tr *i18n.Translator, scope string) string {
	return tr.T("msg.mute.scope_" + scope)
}

// muteScopeValue extracts the scope identifier from the snapshot view
// based on the button's scope. Returns ("", false) when the field is
// missing or empty — the dispatcher emits a "couldn't find" message.
//
// Mirrors the scope dispatch table in docs/buttons-and-snapshots/DESIGN.md
// and #109. The view keys (gym_id, pokestop_id, etc.) are the
// underscore-cased webhook fields the renderer surfaces in the view map.
func muteScopeValue(scope string, snap *snapshots.Snapshot) (string, bool) {
	switch scope {
	case buttons.ScopeEverything:
		return "", true // ScopeEverything is keyless; mute.Add accepts empty ScopeValue
	case buttons.ScopeGym:
		return viewString(snap, "gym_id"), viewString(snap, "gym_id") != ""
	case buttons.ScopePokestop:
		return viewString(snap, "pokestop_id"), viewString(snap, "pokestop_id") != ""
	case buttons.ScopeStation:
		return viewString(snap, "station_id"), viewString(snap, "station_id") != ""
	case buttons.ScopePokemon:
		if id := viewInt(snap, "pokemon_id"); id > 0 {
			return strconv.Itoa(id), true
		}
		return "", false
	case buttons.ScopeArea:
		if len(snap.MatchedAreas) > 0 {
			return snap.MatchedAreas[0], true
		}
		return "", false
	case buttons.ScopeTracking:
		// Tracking-UID mute (Phase 2.5). Not enforceable until MatchedUser
		// carries the UID; the store accepts ScopeTracking writes but the
		// matcher ignores them. Return a clear error rather than a silent
		// success.
		return "", false
	default:
		return "", false
	}
}

// muteScopeLabel produces the display label for a mute scope.
// Pokemon scope translates the dex id to the species name; gym /
// pokestop / station prefer the webhook's name field over the raw id;
// area uses the matched area name verbatim; everything uses the
// localised everything-label.
func muteScopeLabel(tr *i18n.Translator, scope, value string, snap *snapshots.Snapshot) string {
	switch scope {
	case buttons.ScopeEverything:
		return tr.T("msg.mute.everything_label")
	case buttons.ScopePokemon:
		if id, err := strconv.Atoi(value); err == nil {
			return gamedata.PokemonName(tr, id)
		}
		return value
	case buttons.ScopeGym:
		if name := viewString(snap, "gym_name"); name != "" {
			return name
		}
		return value
	case buttons.ScopePokestop:
		if name := viewString(snap, "pokestop_name"); name != "" {
			return name
		}
		return value
	case buttons.ScopeStation:
		if name := viewString(snap, "station_name"); name != "" {
			return name
		}
		return value
	default:
		return value
	}
}

// muteDurationFromParams reads duration_min from the button's params bag
// and converts to a time.Duration. Returns muteDurationDefault when the
// key is missing or the value is non-numeric/non-positive.
//
// Accepts JSON numbers (float64), Go ints, and string numbers — TOML
// might surface durations as ints, and JSON unmarshal hands us float64s.
func muteDurationFromParams(params map[string]any) time.Duration {
	if len(params) == 0 {
		return muteDurationDefault
	}
	raw, ok := params["duration_min"]
	if !ok {
		return muteDurationDefault
	}
	var mins int
	switch v := raw.(type) {
	case int:
		mins = v
	case int64:
		mins = int(v)
	case float64:
		mins = int(v)
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			mins = n
		}
	}
	if mins <= 0 {
		return muteDurationDefault
	}
	return time.Duration(mins) * time.Minute
}

// durationLabel produces an operator-friendly duration string — "30m",
// "1h", "2h 30m", "1d". Mirrors the bot/commands formatDuration logic
// but reimplemented here to keep this package free of bot imports.
func durationLabel(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	days := int(d / (24 * time.Hour))
	d -= time.Duration(days) * 24 * time.Hour
	hours := int(d / time.Hour)
	d -= time.Duration(hours) * time.Hour
	mins := int(d / time.Minute)

	parts := []string{}
	if days > 0 {
		parts = append(parts, fmt.Sprintf("%dd", days))
	}
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if mins > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%dm", mins))
	}
	return strings.Join(parts, " ")
}

// viewString reads a string field from the snapshot's layers via the
// LayeredView-priority walk in Snapshot.Lookup. Tolerates the raw
// map[string]any shape JSON-unmarshal produces.
func viewString(snap *snapshots.Snapshot, key string) string {
	v, ok := snap.Lookup(key)
	if !ok {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// viewInt reads an integer-ish field from the snapshot's layers.
// JSON unmarshal gives float64 for numbers; we coerce.
func viewInt(snap *snapshots.Snapshot, key string) int {
	v, ok := snap.Lookup(key)
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case string:
		if i, err := strconv.Atoi(n); err == nil {
			return i
		}
	}
	return 0
}
