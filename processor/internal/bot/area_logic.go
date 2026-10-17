package bot

import (
	"strings"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/geofence"
)

// AreaInfo describes a geofence area with its display metadata.
type AreaInfo struct {
	Name           string
	Group          string
	UserSelectable bool
	IsActive       bool
}

// AreaLogic provides pure-logic operations on geofence areas without DB access.
// It is created per-request from the current state's fences and config.
type AreaLogic struct {
	fences []geofence.Fence
	cfg    *config.Config
}

// Fences returns the fence list this AreaLogic was built from.
func (a *AreaLogic) Fences() []geofence.Fence { return a.fences }

// NewAreaLogic creates a new AreaLogic from fence data and config.
func NewAreaLogic(fences []geofence.Fence, cfg *config.Config) *AreaLogic {
	return &AreaLogic{fences: fences, cfg: cfg}
}

// GetAvailableAreas returns the list of areas available to a user.
// When area security is disabled, all user-selectable fences are returned.
// When enabled, fences whose names appear in the community's allowed areas
// are returned, plus any fence marked exemptFromAreaSecurity.
//
// Admins bypass both filters (matching PoracleJS area.js semantics):
// they see every fence regardless of UserSelectable, and area_security
// community membership is ignored. This lets an admin who happens to
// belong to no community still administer their own areas.
func (a *AreaLogic) GetAvailableAreas(communities []string, isAdmin bool) []AreaInfo {
	if isAdmin {
		areas := make([]AreaInfo, 0, len(a.fences))
		for _, f := range a.fences {
			areas = append(areas, AreaInfo{
				Name:           f.Name,
				Group:          f.Group,
				UserSelectable: f.UserSelectable,
			})
		}
		return areas
	}

	if !a.cfg.Area.Enabled {
		var areas []AreaInfo
		for _, f := range a.fences {
			if f.UserSelectable {
				areas = append(areas, AreaInfo{
					Name:           f.Name,
					Group:          f.Group,
					UserSelectable: true,
				})
			}
		}
		return areas
	}

	// Area security enabled. Same community predicates as PermittedAreas, so
	// a fence is judged identically by !area and the API. The gate differs on
	// purpose: !area always filters, because it is handed a membership list
	// rather than a human.
	allowedSet := communityAllowedSet(a.cfg.Area.Communities, communities)

	var areas []AreaInfo
	for _, f := range a.fences {
		if f.UserSelectable && communityPermits(f, allowedSet) {
			areas = append(areas, AreaInfo{
				Name:           f.Name,
				Group:          f.Group,
				UserSelectable: true,
			})
		}
	}
	return areas
}

// GetAvailableAreasMarked returns available areas with IsActive set for areas
// present in the user's current area list.
func (a *AreaLogic) GetAvailableAreasMarked(communities []string, currentAreas []string, isAdmin bool) []AreaInfo {
	areas := a.GetAvailableAreas(communities, isAdmin)

	currentSet := make(map[string]bool, len(currentAreas))
	for _, ca := range currentAreas {
		currentSet[strings.ToLower(ca)] = true
	}

	for i := range areas {
		if currentSet[strings.ToLower(areas[i].Name)] {
			areas[i].IsActive = true
		}
	}
	return areas
}

// AddAreas validates and adds areas to the user's current list.
// It returns the display names of areas that were added, names that were not found
// in the available set, and the new full area list (lowercase).
func (a *AreaLogic) AddAreas(currentAreas []string, communities []string, toAdd []string, isAdmin bool) (added []string, notFound []string, newList []string) {
	available := a.GetAvailableAreas(communities, isAdmin)
	availableMap := make(map[string]string, len(available)) // lowercase -> display name
	for _, ai := range available {
		availableMap[strings.ToLower(ai.Name)] = ai.Name
	}

	currentSet := make(map[string]bool, len(currentAreas))
	for _, ca := range currentAreas {
		currentSet[strings.ToLower(ca)] = true
	}

	newList = append(newList, currentAreas...)

	for _, name := range toAdd {
		canonical, displayName, ok := matchAvailableArea(availableMap, name)
		if !ok {
			notFound = append(notFound, name)
			continue
		}
		if !currentSet[canonical] {
			newList = append(newList, canonical)
			currentSet[canonical] = true
			added = append(added, displayName)
		}
	}

	return added, notFound, newList
}

// ResolveAvailableArea validates a single area name against the user's
// available set. Returns the display-cased name when the area is
// available, ("", false) when the area is unknown or not selectable for
// this user. Used by `!mute area` and similar one-name validators that
// don't want the full AddAreas/RemoveAreas flow.
func (a *AreaLogic) ResolveAvailableArea(name string, communities []string, isAdmin bool) (string, bool) {
	available := a.GetAvailableAreas(communities, isAdmin)
	availableMap := make(map[string]string, len(available))
	for _, ai := range available {
		availableMap[strings.ToLower(ai.Name)] = ai.Name
	}
	_, displayName, ok := matchAvailableArea(availableMap, name)
	return displayName, ok
}

// matchAvailableArea looks up a user-supplied area name against the available
// set, trying the underscore-restored form as a fallback. The bot parser
// converts unquoted underscores to spaces, so an area genuinely named
// "gent_centrum" arrives here as "gent centrum"; we try both so the user
// doesn't have to know to wrap the name in quotes.
//
// Returns the canonical lowercase key (matches availableMap) and the
// proper display name. ok=false when neither form matches.
func matchAvailableArea(availableMap map[string]string, name string) (canonical, displayName string, ok bool) {
	lower := strings.ToLower(name)
	if dn, found := availableMap[lower]; found {
		return lower, dn, true
	}
	underForm := strings.ReplaceAll(lower, " ", "_")
	if underForm != lower {
		if dn, found := availableMap[underForm]; found {
			return underForm, dn, true
		}
	}
	return "", "", false
}

// RemoveAreas removes named areas from the current list.
// It returns the display names of areas that were actually removed and the remaining list.
//
// User input is matched against stored names tolerantly: an area stored as
// "gent_centrum" matches when the user typed "gent_centrum" (parser strips
// the underscore to a space) or quoted "\"gent_centrum\"" (preserved as-is).
func (a *AreaLogic) RemoveAreas(currentAreas []string, toRemove []string) (removed []string, newList []string) {
	removeSet := make(map[string]bool, len(toRemove))
	for _, name := range toRemove {
		lower := strings.ToLower(name)
		removeSet[lower] = true
		// Add the underscore-restored form so a stored "gent_centrum"
		// matches when the user typed it unquoted (parser → "gent centrum").
		removeSet[strings.ReplaceAll(lower, " ", "_")] = true
	}

	for _, ca := range currentAreas {
		if removeSet[strings.ToLower(ca)] {
			removed = append(removed, ca)
		} else {
			newList = append(newList, ca)
		}
	}

	return removed, newList
}

// ResolveDisplayNames maps area names (typically lowercase) to their proper
// display names from fence data. If a fence is not found, the original name is kept.
func (a *AreaLogic) ResolveDisplayNames(areas []string) []string {
	displayNames := make([]string, 0, len(areas))
	for _, name := range areas {
		found := false
		for _, f := range a.fences {
			if strings.EqualFold(f.Name, name) {
				displayNames = append(displayNames, f.Name)
				found = true
				break
			}
		}
		if !found {
			displayNames = append(displayNames, name)
		}
	}
	return displayNames
}

// FindFence looks up a fence by name (case-insensitive). Tries the
// underscore-restored form as a fallback so a fence named "gent_centrum"
// matches when the user typed it unquoted (the parser strips the
// underscore to a space).
func (a *AreaLogic) FindFence(name string) *geofence.Fence {
	lower := strings.ToLower(name)
	underForm := strings.ReplaceAll(lower, " ", "_")
	for i := range a.fences {
		fenceLower := strings.ToLower(a.fences[i].Name)
		if fenceLower == lower || fenceLower == underForm {
			return &a.fences[i]
		}
	}
	return nil
}
