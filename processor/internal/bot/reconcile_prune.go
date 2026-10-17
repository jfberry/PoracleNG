package bot

import (
	"errors"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// PruneOptions selects which prunes PruneForMembership runs.
type PruneOptions struct {
	// Overrides prunes per-rule override_areas. Low-value derived data whose
	// loss is visible and self-correcting, so callers run it unconditionally.
	Overrides bool
	// UserAreas prunes humans.area and every profile. Irreversible, so it is
	// gated behind [reconciliation.*] prune_user_areas (default false).
	UserAreas bool
}

// PruneForMembership strips, after a community-membership change, every
// area the human may no longer hold, judged by PermittedAreas so the prune
// can never undo a grant. human must carry the NEW membership and
// area_restriction.
//
// trusted is passed as true: prunes act only on the community dimension, so
// a non-userSelectable fence granted via trusted survives. No fences => skip
// entirely: an empty list means the load failed, not that nothing is allowed.
//
// It works from a DENIED set, not a permitted set: a name is stripped only if
// it is a currently loaded fence that PermittedAreas does not return. A fence
// missing from a partial load (one Koji project down, a skipped GeoJSON
// feature) is therefore never stripped; unknown names match nothing, so
// keeping them is harmless.
func PruneForMembership(hs store.HumanStore, fences []geofence.Fence, cfg *config.Config,
	human *store.Human, admin bool, opts PruneOptions) ([]string, error) {
	if len(fences) == 0 || human == nil {
		return nil, nil
	}
	allowed := PermittedAreaKeys(PermittedAreas(fences, cfg, human, admin, true))
	denied := make(map[string]bool)
	for _, f := range fences {
		for _, k := range []string{strings.ToLower(f.Name), geofence.NormalizeAreaKey(f.Name)} {
			if !allowed[k] {
				denied[k] = true
			}
		}
	}

	var errs []error
	if opts.Overrides {
		if err := hs.StripOverrideAreas(human.ID, denied); err != nil {
			errs = append(errs, err)
		}
	}
	var removed []string
	if opts.UserAreas {
		r, err := hs.StripUserAreas(human.ID, denied)
		if err != nil {
			errs = append(errs, err)
		}
		removed = r
	}
	return removed, errors.Join(errs...)
}
