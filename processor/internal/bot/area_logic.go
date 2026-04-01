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

// NewAreaLogic creates a new AreaLogic from fence data and config.
func NewAreaLogic(fences []geofence.Fence, cfg *config.Config) *AreaLogic {
	return &AreaLogic{fences: fences, cfg: cfg}
}

// GetAvailableAreas returns the list of areas available to a user.
// When area security is disabled, all user-selectable fences are returned.
// When enabled, only fences whose names appear in the community's allowed areas are returned.
func (a *AreaLogic) GetAvailableAreas(communities []string) []AreaInfo {
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

	// Area security enabled — build allowed set from community config
	allowedSet := a.buildAllowedSet(communities)

	var areas []AreaInfo
	for _, f := range a.fences {
		if f.UserSelectable && allowedSet[strings.ToLower(f.Name)] {
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
func (a *AreaLogic) GetAvailableAreasMarked(communities []string, currentAreas []string) []AreaInfo {
	areas := a.GetAvailableAreas(communities)

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
func (a *AreaLogic) AddAreas(currentAreas []string, communities []string, toAdd []string) (added []string, notFound []string, newList []string) {
	available := a.GetAvailableAreas(communities)
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
		lower := strings.ToLower(name)
		if displayName, ok := availableMap[lower]; ok {
			if !currentSet[lower] {
				newList = append(newList, lower)
				currentSet[lower] = true
				added = append(added, displayName)
			}
		} else {
			notFound = append(notFound, name)
		}
	}

	return added, notFound, newList
}

// RemoveAreas removes named areas from the current list.
// It returns the display names of areas that were actually removed and the remaining list.
func (a *AreaLogic) RemoveAreas(currentAreas []string, toRemove []string) (removed []string, newList []string) {
	removeSet := make(map[string]bool, len(toRemove))
	for _, name := range toRemove {
		removeSet[strings.ToLower(name)] = true
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

// FindFence looks up a fence by name (case-insensitive).
func (a *AreaLogic) FindFence(name string) *geofence.Fence {
	lower := strings.ToLower(name)
	for i := range a.fences {
		if strings.ToLower(a.fences[i].Name) == lower {
			return &a.fences[i]
		}
	}
	return nil
}

// buildAllowedSet builds a set of lowercase area names allowed by the given communities.
func (a *AreaLogic) buildAllowedSet(communities []string) map[string]bool {
	allowedSet := make(map[string]bool)
	for _, comm := range communities {
		for _, cc := range a.cfg.Area.Communities {
			if strings.EqualFold(cc.Name, comm) {
				for _, area := range cc.AllowedAreas {
					allowedSet[strings.ToLower(area)] = true
				}
			}
		}
	}
	return allowedSet
}
