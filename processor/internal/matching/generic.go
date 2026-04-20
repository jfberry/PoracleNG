package matching

import (
	"math"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// boolToInt converts a bool to 0/1 int for the Clean field.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// trackingUserData holds common tracking fields for human validation.
type trackingUserData struct {
	HumanID           string
	ProfileNo         int
	Distance          int
	Template          string
	Clean             int
	Ping              string
	IsSpecificStation bool // maxbattle: station-specific tracking (for specificstation blocked check)
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

		// Distance/area check
		if td.Distance > 0 {
			dist := HaversineDistance(human.Latitude, human.Longitude, lat, lon)
			if dist > td.Distance {
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

		// Compute actual distance and bearing from user to event
		actualDist := HaversineDistance(human.Latitude, human.Longitude, lat, lon)
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
		})
	}
	return result
}
