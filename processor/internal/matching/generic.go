package matching

import (
	"strings"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// trackingUserData holds common tracking fields for human validation.
type trackingUserData struct {
	HumanID   string
	ProfileNo int
	Distance  int
	Template  string
	Clean     bool
	Ping      string
}

// ValidateHumansGeneric filters matched trackings against human criteria.
// blockedAlertType is checked against humans.blocked_alerts (e.g. "invasion", "lure").
func ValidateHumansGeneric(
	trackings []trackingUserData,
	lat, lon float64,
	matchedAreaNames []string,
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
		if strings.Contains(human.BlockedAlerts, blockedAlertType) {
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

		result = append(result, webhook.MatchedUser{
			ID:        human.ID,
			Name:      human.Name,
			Type:      human.Type,
			Language:  human.Language,
			Latitude:  human.Latitude,
			Longitude: human.Longitude,
			Template:  td.Template,
			Distance:  td.Distance,
			Clean:     td.Clean,
			Ping:      td.Ping,
		})
	}
	return result
}
