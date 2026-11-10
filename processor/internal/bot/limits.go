package bot

import (
	"reflect"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/store"
)

// [tracking] max_distance and max_areas apply to rules and areas for users.
// LimitExempt reports who they don't apply to: admins, and destinations that
// only admins and delegated admins can configure (channels, webhooks, groups,
// threads — every human type that is not "<platform>:user").
func LimitExempt(admin bool, targetType string) bool {
	return admin || !strings.HasSuffix(targetType, ":user")
}

// MaxDistanceFor is the [tracking] max_distance cap for a write: 0 means no
// cap. exempt comes from LimitExempt (or a trusted API request).
func MaxDistanceFor(cfg *config.Config, exempt bool) int {
	if exempt || cfg == nil {
		return 0
	}
	return cfg.Tracking.MaxDistance
}

// AreaLimitBlocks reports whether storing areas would break [tracking]
// max_areas (0 = no limit). A list over the limit is still allowed when it
// only keeps names from previous — the list it replaces — so a list stored
// before the limit was lowered can be resubmitted or shrunk, but not grown.
//
// Names compare by geofence.NormalizeAreaKey; duplicates count once.
func AreaLimitBlocks(cfg *config.Config, exempt bool, areas, previous []string) bool {
	if !overAreaLimit(cfg, exempt, areas) {
		return false
	}
	prevKeys := areaKeySet(previous)
	for k := range areaKeySet(areas) {
		if !prevKeys[k] {
			return true
		}
	}
	return false
}

// OverrideAreaLimitBreached applies max_areas to the override_areas of a
// tracking write before it happens: candidates are classified against
// existing exactly as store.ApplyDiff will, then a new rule must be within the
// limit and an updated rule may only keep a subset of the list on the row it
// replaces. Rows that are already present are unchanged and always pass.
//
// The diff only runs when some candidate is over the limit.
func OverrideAreaLimitBreached[T any](cfg *config.Config, exempt bool, existing, candidates []T,
	getUID func(*T) int64, setUID func(*T, int64)) bool {
	over := false
	for i := range candidates {
		if overAreaLimit(cfg, exempt, OverrideAreasOf(&candidates[i])) {
			over = true
			break
		}
	}
	if !over {
		return false
	}
	diff := store.DiffAndClassify(existing, candidates, getUID, setUID)
	for i := range diff.Inserts {
		if AreaLimitBlocks(cfg, exempt, OverrideAreasOf(&diff.Inserts[i]), nil) {
			return true
		}
	}
	for i := range diff.Updates {
		if AreaLimitBlocks(cfg, exempt, OverrideAreasOf(&diff.Updates[i]), OverrideAreasOf(&diff.Replaced[i])) {
			return true
		}
	}
	return false
}

// OverrideAreasOf returns a tracking row's OverrideAreas field (every
// db.*TrackingAPI type has one); nil for anything else.
func OverrideAreasOf(row any) []string {
	v := reflect.ValueOf(row)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	f := v.FieldByName("OverrideAreas")
	if !f.IsValid() {
		return nil
	}
	areas, _ := f.Interface().([]string)
	return areas
}

func overAreaLimit(cfg *config.Config, exempt bool, areas []string) bool {
	if exempt || cfg == nil || cfg.Tracking.MaxAreas <= 0 || len(areas) <= cfg.Tracking.MaxAreas {
		return false
	}
	return len(areaKeySet(areas)) > cfg.Tracking.MaxAreas
}

func areaKeySet(areas []string) map[string]bool {
	set := make(map[string]bool, len(areas))
	for _, a := range areas {
		set[geofence.NormalizeAreaKey(a)] = true
	}
	return set
}
