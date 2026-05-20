package matching

import (
	"math"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// ConvertAreas converts the geofence package's MatchedArea slice to the
// webhook package's MatchedArea slice. Used by every matcher so handlers
// receive an already-converted []webhook.MatchedArea and don't have to
// re-walk the geofence rtree.
func ConvertAreas(in []geofence.MatchedArea) []webhook.MatchedArea {
	if len(in) == 0 {
		return nil
	}
	out := make([]webhook.MatchedArea, len(in))
	for i, a := range in {
		out[i] = webhook.MatchedArea{
			Name:             a.Name,
			DisplayInMatches: a.DisplayInMatches,
			Group:            a.Group,
		}
	}
	return out
}

// boolToInt converts a bool to 0/1 int for the Clean field.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// trackingUserData holds common tracking fields for human validation.
type trackingUserData struct {
	HumanID         string
	ProfileNo       int
	Distance        int
	Template        string
	Clean           int
	Ping            string
	UID             int64 // database UID of the matching tracking rule — surfaced on MatchedUser.RuleUID for snapshot/mute use
	IsSpecificMatch bool  // set when the rule is pinned to a specific entity (gym_id, station_id, etc.) — meaning area/distance check is bypassed and a type-specific blocked-alert key is checked instead (e.g. "specificgym" or "specificstation")
}

// ValidateHumansGeneric filters matched trackings against human criteria.
// blockedAlertType is checked against humans.blocked_alerts (e.g. "invasion", "lure").
func ValidateHumansGeneric(
	trackings []trackingUserData,
	lat, lon float64,
	matchedAreaNames map[string]bool,
	strictAreasEnabled bool,
	humans map[string]*db.Human,
	blockedAlertType string,
) []webhook.MatchedUser {
	if len(trackings) == 0 {
		return nil
	}

	haversineCount := 0
	defer func() {
		metrics.MatchingHaversines.WithLabelValues(blockedAlertType).Observe(float64(haversineCount))
	}()

	seen := make(map[string]bool)
	var result []webhook.MatchedUser

	for _, td := range trackings {
		human, ok := humans[td.HumanID]
		if !ok || !human.Enabled || human.AdminDisable {
			continue
		}
		if human.BlockedAlertsSet[blockedAlertType] {
			continue
		}
		if td.ProfileNo != human.CurrentProfileNo {
			continue
		}

		// Lazy haversine: compute once when first needed, cache for reuse.
		var dist int
		distComputed := false
		haversine := func() int {
			if !distComputed {
				dist = HaversineDistance(human.Latitude, human.Longitude, lat, lon)
				distComputed = true
				haversineCount++
			}
			return dist
		}

		// Distance/area check
		if td.Distance > 0 {
			if haversine() > td.Distance {
				continue
			}
		} else {
			if !areaOverlap(human.Area, matchedAreaNames) {
				continue
			}
		}

		// Strict area restriction
		if strictAreasEnabled && human.AreaRestriction != nil {
			if !areaOverlap(human.AreaRestriction, matchedAreaNames) {
				continue
			}
		}

		// Deduplicate by human ID
		if seen[human.ID] {
			continue
		}
		seen[human.ID] = true

		// Reuse cached haversine (or compute now for area-based users).
		actualDist := haversine()
		bearing := Bearing(human.Latitude, human.Longitude, lat, lon)

		result = append(result, webhook.MatchedUser{
			ID:                human.ID,
			Name:              human.Name,
			Type:              human.Type,
			Language:          human.Language,
			Latitude:          human.Latitude,
			Longitude:         human.Longitude,
			Template:          td.Template,
			Distance:          actualDist,
			Clean:             td.Clean,
			Ping:              td.Ping,
			Bearing:           int(math.Round(bearing)),
			CardinalDirection: CardinalDirection(bearing),
			TrackDistance:     td.Distance,
			RuleUID:           td.UID,
		})
	}
	return result
}
