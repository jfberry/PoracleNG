package main

import (
	"strings"

	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/config"
	"github.com/pokemon/poracleng/processor/internal/geofence"
)

// validateCommunityAreas cross-checks each community's allowed_areas and
// location_fence against the loaded geofence set. A typo in config silently
// contributes nothing at match time — this surfaces it at startup instead.
//
// allowed_areas resolve by geofence.NormalizeAreaKey and location_fence by
// plain lowercase, matching what the runtime compares (community.FilterAreas
// and bot.PermittedAreas key allowed_areas; location restrictions lowercase).
func validateCommunityAreas(fences []geofence.Fence, communities []config.CommunityConfig) {
	fenceSet := make(map[string]bool, len(fences))
	fenceKeys := make(map[string]bool, len(fences))
	for _, f := range fences {
		fenceSet[strings.ToLower(f.Name)] = true
		fenceKeys[geofence.NormalizeAreaKey(f.Name)] = true
	}

	for _, comm := range communities {
		// allowed_areas can be large (dozens of entries per community). Log
		// misses individually and a summary count; skip the per-area ✓ noise.
		validAllowed := 0
		for _, a := range comm.AllowedAreas {
			if fenceKeys[geofence.NormalizeAreaKey(a)] {
				validAllowed++
			} else {
				log.Warnf("config: community %s allowed_areas %q — NOT FOUND in loaded geofences", comm.Name, a)
			}
		}
		if len(comm.AllowedAreas) > 0 {
			log.Infof("config: community %s allowed_areas: %d/%d resolved", comm.Name, validAllowed, len(comm.AllowedAreas))
		}

		// location_fence is typically 1 entry; log each.
		for _, fence := range comm.LocationFence {
			if fenceSet[strings.ToLower(fence)] {
				log.Infof("config: community %s location_fence %q ✓", comm.Name, fence)
			} else {
				log.Warnf("config: community %s location_fence %q — NOT FOUND in loaded geofences", comm.Name, fence)
			}
		}
	}
}
