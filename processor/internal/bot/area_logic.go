package bot

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jmoiron/sqlx"
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
// When enabled, only fences whose names appear in the community's allowed
// areas are returned.
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

// ValidateAndPrune removes areas from a user's list that are no longer permitted
// given their current community membership. When area security is disabled, all
// areas are valid. Returns the pruned list and the removed areas.
// Used by reconciliation after community membership changes.
func (a *AreaLogic) ValidateAndPrune(currentAreas []string, communities []string) (valid []string, removed []string) {
	if !a.cfg.Area.Enabled {
		return currentAreas, nil
	}

	allowedSet := a.buildAllowedSet(communities)
	if len(allowedSet) == 0 {
		// No communities → no allowed areas → all removed
		return nil, currentAreas
	}

	for _, area := range currentAreas {
		if allowedSet[strings.ToLower(area)] {
			valid = append(valid, area)
		} else {
			removed = append(removed, area)
		}
	}
	return
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

// filterPermittedAreas returns a new slice containing only the areas whose
// lowercase form appears in permitted. The original case of each area name is
// preserved. Returns nil (not an empty slice) when every area is filtered out,
// so callers can distinguish "nothing left" from "nothing provided".
func filterPermittedAreas(areas []string, permitted map[string]bool) []string {
	if len(areas) == 0 {
		return nil
	}
	var kept []string
	for _, area := range areas {
		if permitted[strings.ToLower(area)] {
			kept = append(kept, area)
		}
	}
	return kept
}

// trackingTablesWithOverrideAreas lists all tables that carry an override_areas column.
// Mirrors humanOwnedTables in db/human_queries.go minus user_locations (no override_areas).
var trackingTablesWithOverrideAreas = []string{
	"monsters", "raid", "egg", "quest", "invasion",
	"lures", "nests", "gym", "forts", "maxbattle",
}

// PruneOverrideAreas walks every tracking row for the given human and drops
// areas no longer in permitted from the override_areas JSON column. When the
// pruned list is empty the column is set to NULL so the rule falls back to the
// human's own area list.
//
// Call this after reconciling community_membership so that per-rule area
// overrides stay consistent with the user's new permitted set.
//
// Wiring note: the natural call site is reconcileAreaSecurity in
// internal/discordbot/reconciliation.go (the "before && after" branch, after
// community_membership is updated). Wiring is deferred because Reconciliation
// does not currently hold a *sqlx.DB; that plumbing is a follow-up task.
func (a *AreaLogic) PruneOverrideAreas(dbx *sqlx.DB, humanID string, permitted map[string]bool) error {
	type row struct {
		UID           int64  `db:"uid"`
		OverrideAreas string `db:"override_areas"`
	}

	for _, table := range trackingTablesWithOverrideAreas {
		var rows []row
		q := fmt.Sprintf(
			"SELECT uid, COALESCE(override_areas, '') AS override_areas FROM `%s` WHERE id = ? AND override_areas IS NOT NULL AND override_areas != ''",
			table,
		)
		if err := dbx.Select(&rows, q, humanID); err != nil {
			return fmt.Errorf("prune %s: %w", table, err)
		}

		for _, r := range rows {
			var areas []string
			if err := json.Unmarshal([]byte(r.OverrideAreas), &areas); err != nil {
				// Malformed JSON — skip rather than corrupt the row.
				continue
			}

			kept := filterPermittedAreas(areas, permitted)
			if len(kept) == len(areas) {
				continue // nothing changed
			}

			var newVal any
			if len(kept) == 0 {
				newVal = nil
			} else {
				b, _ := json.Marshal(kept)
				newVal = string(b)
			}

			updQ := fmt.Sprintf("UPDATE `%s` SET override_areas = ? WHERE uid = ?", table)
			if _, err := dbx.Exec(updQ, newVal, r.UID); err != nil {
				return fmt.Errorf("update %s uid %d: %w", table, r.UID, err)
			}
		}
	}

	return nil
}
