package geofence

import "strings"

// NormalizeAreaKey is the comparison key for an area name: lowercased,
// underscores as spaces (the form of Fence.NormalizedName). It is used for
// lookups, comparisons and matching only; stored names use the fence's own
// spelling (see CanonicalAreaName), so compare a stored name by keying both
// sides.
func NormalizeAreaKey(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "_", " "))
}

// CanonicalAreaName returns the stored spelling of an area name: the
// lowercased name of the loaded fence whose key matches name (underscores and
// spaces exactly as the fence has them). When no loaded fence matches it
// returns strings.ToLower(name). NormalizeAreaKey is the comparison key only;
// this is what writes store.
func CanonicalAreaName(fences []Fence, name string) string {
	key := NormalizeAreaKey(name)
	for i := range fences {
		if NormalizeAreaKey(fences[i].Name) == key {
			return strings.ToLower(fences[i].Name)
		}
	}
	return strings.ToLower(name)
}
