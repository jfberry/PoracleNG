package bot

import (
	"strings"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// PermittedAreas returns the fence names this human may hold, lowercased.
//
// It is THE definition, called by every grant path (API set-areas, per-rule
// override_areas validation) and every prune path (reconciliation), so a
// fence cannot be granted by one rule and removed by another (#243).
//
// A name is only ever returned if it is a fence. On top of that:
//   - admin and trusted lift the userSelectable filter;
//   - the community filter is lifted only by admin, or by the fence declaring
//     itself exemptFromAreaSecurity.
//
// trusted deliberately does not lift the community filter (#228/#230).
func PermittedAreas(fences []geofence.Fence, cfg *config.Config,
	human *store.Human, admin, trusted bool) []string {
	filtered := subjectToCommunityFilter(cfg, human, admin)
	var allowed map[string]bool
	if filtered {
		allowed = communityAllowedSet(cfg.Area.Communities, human.CommunityMembership)
	}
	out := make([]string, 0, len(fences))
	for _, f := range fences {
		if !admin && !trusted && !f.UserSelectable {
			continue
		}
		if filtered && !communityPermits(f, allowed) {
			continue
		}
		out = append(out, strings.ToLower(f.Name))
	}
	return out
}

// PermittedAreaKeys turns PermittedAreas output into a lookup set holding
// both storage spellings: humans.area keeps underscores, override_areas
// stores them as spaces (geofence.NormalizeAreaKey).
func PermittedAreaKeys(names []string) map[string]bool {
	keys := make(map[string]bool, 2*len(names))
	for _, n := range names {
		keys[strings.ToLower(n)] = true
		keys[geofence.NormalizeAreaKey(n)] = true
	}
	return keys
}

// subjectToCommunityFilter reports whether area_security narrows this
// human's areas. A human with neither a community nor an area_restriction —
// created through the API without one, or predating area_security — is
// unrestricted, as the API has always treated them. A community member is
// filtered even when area_restriction is nil, which it is whenever their
// communities configure no location_fence.
func subjectToCommunityFilter(cfg *config.Config, human *store.Human, admin bool) bool {
	if admin || cfg == nil || !cfg.Area.Enabled || human == nil {
		return false
	}
	return human.AreaRestriction != nil || len(human.CommunityMembership) > 0
}

// communityAllowedSet is the lowercase union of allowed_areas across the
// communities memberOf names (case-insensitive).
func communityAllowedSet(communities []config.CommunityConfig, memberOf []string) map[string]bool {
	allowed := make(map[string]bool)
	for _, comm := range communities {
		for _, m := range memberOf {
			if strings.EqualFold(comm.Name, m) {
				for _, a := range comm.AllowedAreas {
					allowed[strings.ToLower(a)] = true
				}
			}
		}
	}
	return allowed
}

// communityPermits reports whether the community filter lets f through: it
// is in one of the human's communities, or it declares itself outside the
// community system.
func communityPermits(f geofence.Fence, allowed map[string]bool) bool {
	return f.ExemptFromAreaSecurity || allowed[strings.ToLower(f.Name)]
}
