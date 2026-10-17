package geofence

import "strings"

// NormalizeAreaKey keys an area name the way override_areas and
// Fence.NormalizedName store it: lowercased, underscores as spaces.
// humans.area stores names lowercased with underscores intact, so code
// comparing against both must key both ways.
func NormalizeAreaKey(s string) string {
	return strings.ToLower(strings.ReplaceAll(s, "_", " "))
}
