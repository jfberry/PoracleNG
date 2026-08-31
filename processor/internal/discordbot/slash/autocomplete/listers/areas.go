package listers

import (
	"context"
	"sort"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/discordbot/slash/autocomplete"
)

// ListAreas enumerates the geofence areas currently selected on the user's
// human record. Each Choice has Label == Value == the area name. Returns
// (nil, nil) for an unregistered user so callers can surface "no choices"
// without an error path.
//
// This is the /area remove list. For the sets drawn from the geofence
// config rather than the user's selection, see ListAvailableAreas
// (everything they may use) and ListAddableAreas (what they may still add).
//
// HumanLite does not carry the Area slice (it is a JSON column parsed only
// by Get), so this lister calls the full Get. Autocomplete callsites are
// already off the matching hot path, so the extra parse is cheap.
func ListAreas(ctx context.Context, deps *bot.BotDeps, userID string, hint autocomplete.UserStateHint) ([]autocomplete.Choice, error) {
	if deps.Humans == nil {
		return nil, nil
	}
	human, err := deps.Humans.Get(userID)
	if err != nil {
		return nil, err
	}
	if human == nil {
		return nil, nil
	}
	// The human record stores areas lowercased; show the fence's display
	// casing so this list reads like the add list. Names with no matching
	// fence pass through unchanged, and RemoveAreas matches either way.
	names := human.Area
	if deps.StateMgr != nil {
		if st := deps.StateMgr.Get(); st != nil {
			names = bot.NewAreaLogic(st.Fences, deps.Cfg).ResolveDisplayNames(human.Area)
		}
	}
	out := make([]autocomplete.Choice, 0, len(names))
	for _, area := range names {
		out = append(out, autocomplete.Choice{Label: area, Value: area})
	}
	return out, nil
}

// ListAvailableAreas enumerates every geofence area the user is allowed to
// name: user-selectable fences, narrowed to their community's allowed set
// when area security is on, with admins bypassing both filters (hence the
// IsAdmin hint). Areas they have already selected are included — this set
// is independent of their selection.
//
// It backs the tracker commands' `areas:` option, which *overrides* a
// rule's areas rather than narrowing the user's selection; parseOverride
// (bot/commands/override.go) validates that option against this exact set.
func ListAvailableAreas(ctx context.Context, deps *bot.BotDeps, userID string, hint autocomplete.UserStateHint) ([]autocomplete.Choice, error) {
	areas, err := availableAreasFor(deps, userID, hint.IsAdmin)
	if err != nil || areas == nil {
		return nil, err
	}
	return areaChoices(areas, false), nil
}

// ListAddableAreas is ListAvailableAreas minus the areas already on the
// user's human record — the /area add list, where offering an area they
// already have would be a no-op.
func ListAddableAreas(ctx context.Context, deps *bot.BotDeps, userID string, hint autocomplete.UserStateHint) ([]autocomplete.Choice, error) {
	areas, err := availableAreasFor(deps, userID, hint.IsAdmin)
	if err != nil || areas == nil {
		return nil, err
	}
	return areaChoices(areas, true), nil
}

// availableAreasFor resolves the user's available areas with IsActive
// marking their current selection. Returns (nil, nil) when the user is
// unregistered or the pieces AreaLogic needs aren't wired, so callers
// degrade to "no suggestions".
func availableAreasFor(deps *bot.BotDeps, userID string, isAdmin bool) ([]bot.AreaInfo, error) {
	if deps == nil || deps.Humans == nil || deps.StateMgr == nil {
		return nil, nil
	}
	st := deps.StateMgr.Get()
	if st == nil {
		return nil, nil
	}
	human, err := deps.Humans.Get(userID)
	if err != nil {
		return nil, err
	}
	if human == nil {
		return nil, nil
	}

	// Community membership only narrows the set when area security is on;
	// mirrors humanCommunities in bot/commands/area.go.
	var communities []string
	if deps.Cfg != nil && deps.Cfg.Area.Enabled {
		communities = human.CommunityMembership
	}

	logic := bot.NewAreaLogic(st.Fences, deps.Cfg)
	return logic.GetAvailableAreasMarked(communities, human.Area, isAdmin), nil
}

// areaChoices converts marked areas into alphabetically sorted choices,
// optionally dropping the ones already selected. Labels carry the fence's
// display casing ("Gent Centrum"); the commands match area names
// case-insensitively, so label == value.
func areaChoices(areas []bot.AreaInfo, skipActive bool) []autocomplete.Choice {
	out := make([]autocomplete.Choice, 0, len(areas))
	for _, a := range areas {
		if skipActive && a.IsActive {
			continue
		}
		out = append(out, autocomplete.Choice{Label: a.Name, Value: a.Name})
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Label) < strings.ToLower(out[j].Label)
	})
	return out
}
