package matching

import (
	"math"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// ValidateHumans filters matched monster trackings against human criteria.
// Port of monsterAlarmMatch.js:190-264.
func ValidateHumans(
	monsterList []*db.MonsterTracking,
	monsterLat, monsterLon float64,
	matchedAreaNames map[string]bool,
	strictAreasEnabled bool,
	humans map[string]*db.Human,
) []webhook.MatchedUser {
	if len(monsterList) == 0 {
		return nil
	}

	// Find unique human IDs
	humanIDs := make(map[string]bool)
	for _, m := range monsterList {
		humanIDs[m.ID] = true
	}

	// Filter humans that are valid (enabled, not blocked for monsters)
	validHumans := make(map[string]*db.Human)
	for id := range humanIDs {
		h, ok := humans[id]
		if !ok {
			continue
		}
		if !h.Enabled || h.AdminDisable {
			continue
		}
		if h.BlockedAlertsSet["monster"] {
			continue
		}
		validHumans[id] = h
	}

	// Normalize area names for comparison (already lowercased with _ -> space)
	areas := matchedAreaNames

	var result []webhook.MatchedUser
	for _, monster := range monsterList {
		human, ok := validHumans[monster.ID]
		if !ok {
			continue
		}
		// Profile check
		if monster.ProfileNo != human.CurrentProfileNo {
			continue
		}
		// Distance/area check
		if monster.Distance > 0 {
			dist := HaversineDistance(human.Latitude, human.Longitude, monsterLat, monsterLon)
			if dist > monster.Distance {
				continue
			}
		} else {
			if !areaOverlap(human.Area, areas) {
				continue
			}
		}
		// Strict area restriction
		if strictAreasEnabled && human.AreaRestriction != nil {
			if !areaOverlap(human.AreaRestriction, areas) {
				continue
			}
		}

		// Compute actual distance and bearing from user to event
		actualDist := HaversineDistance(human.Latitude, human.Longitude, monsterLat, monsterLon)
		bearing := Bearing(human.Latitude, human.Longitude, monsterLat, monsterLon)

		result = append(result, webhook.MatchedUser{
			ID:                human.ID,
			Name:              human.Name,
			Type:              human.Type,
			Language:          human.Language,
			Latitude:          human.Latitude,
			Longitude:         human.Longitude,
			Template:          monster.Template,
			Distance:          actualDist,
			Clean:             monster.Clean,
			Ping:              monster.Ping,
			Bearing:           int(math.Round(bearing)),
			CardinalDirection: CardinalDirection(bearing),
			PokemonID:         monster.PokemonID,
			PVPRankingCap:     monster.PVPRankingCap,
			PVPRankingLeague:  monster.PVPRankingLeague,
			PVPRankingWorst:   monster.PVPRankingWorst,
			TrackDistance:     monster.Distance,
		})
	}
	return result
}

// ValidateHumansForRaid filters matched raid/egg trackings against human criteria.
func ValidateHumansForRaid(
	trackingData []raidUserData,
	raidLat, raidLon float64,
	matchedAreaNames map[string]bool,
	strictAreasEnabled bool,
	humans map[string]*db.Human,
	blockedAlertType string,
) []webhook.MatchedUser {
	if len(trackingData) == 0 {
		return nil
	}

	areas := matchedAreaNames

	seen := make(map[string]bool)
	var result []webhook.MatchedUser

	for _, td := range trackingData {
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

		// Specific gym tracking - check specificgym block
		if td.IsSpecificGym {
			if human.BlockedAlertsSet["specificgym"] {
				continue
			}
		} else {
			// Distance/area check
			if td.Distance > 0 {
				dist := HaversineDistance(human.Latitude, human.Longitude, raidLat, raidLon)
				if dist > td.Distance {
					continue
				}
			} else {
				if !areaOverlap(human.Area, areas) {
					continue
				}
			}
		}

		// Strict area restriction
		if strictAreasEnabled && human.AreaRestriction != nil {
			if !areaOverlap(human.AreaRestriction, areas) {
				continue
			}
		}

		// Deduplicate by human ID
		if seen[human.ID] {
			continue
		}
		seen[human.ID] = true

		// Compute actual distance and bearing from user to event
		actualDist := HaversineDistance(human.Latitude, human.Longitude, raidLat, raidLon)
		bearing := Bearing(human.Latitude, human.Longitude, raidLat, raidLon)

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
			RSVPChanges:       td.RSVPChanges,
			TrackDistance:     td.Distance,
		})
	}
	return result
}

type raidUserData struct {
	HumanID       string
	ProfileNo     int
	Distance      int
	Template      string
	Clean         int
	Ping          string
	RSVPChanges   int
	IsSpecificGym bool
}

func areaOverlap(humanAreas []string, matchedAreas map[string]bool) bool {
	for _, ha := range humanAreas {
		if matchedAreas[ha] {
			return true
		}
	}
	return false
}
