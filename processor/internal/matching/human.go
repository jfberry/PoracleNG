package matching

import (
	"math"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/metrics"
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

	haversineCount := 0
	defer func() {
		metrics.MatchingHaversines.WithLabelValues(metrics.TypePokemon).Observe(float64(haversineCount))
	}()

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

		// PVP-specific blocked-alert gate. When the rule has a PVP
		// filter (PVPRankingLeague != 0) and the user has "pvp" in
		// blocked_alerts — typically derived from
		// [discord.command_security] pvp by reconciliation when the
		// user lost the required role — drop the match. Mirrors
		// PoracleJS monster.js:102 which folds the same NOT LIKE
		// '%pvp%' clause into the SQL on the PVP-filtered branch.
		if monster.PVPRankingLeague != 0 && human.BlockedAlertsSet["pvp"] {
			continue
		}

		anchorLat, anchorLon, effectiveAreas := resolveOverride(monster.OverrideLocationLabel, monster.OverrideAreas, human)

		// Lazy haversine: compute once when first needed, cache for reuse.
		var dist int
		distComputed := false
		haversine := func() int {
			if !distComputed {
				dist = HaversineDistance(anchorLat, anchorLon, monsterLat, monsterLon)
				distComputed = true
				haversineCount++
			}
			return dist
		}

		// Distance/area check
		if monster.Distance > 0 {
			if haversine() > monster.Distance {
				continue
			}
		} else {
			if !areaOverlap(effectiveAreas, areas) {
				continue
			}
		}
		// Strict area restriction
		if strictAreasEnabled && human.AreaRestriction != nil {
			if !areaOverlap(human.AreaRestriction, areas) {
				continue
			}
		}

		// Reuse cached haversine (or compute now for area-based users).
		actualDist := haversine()
		bearing := Bearing(anchorLat, anchorLon, monsterLat, monsterLon)

		result = append(result, webhook.MatchedUser{
			ID:                human.ID,
			Name:              human.Name,
			Type:              human.Type,
			Language:          human.Language,
			Latitude:          anchorLat,
			Longitude:         anchorLon,
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
			RuleUID:           monster.UID,
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

	haversineCount := 0
	defer func() {
		metrics.MatchingHaversines.WithLabelValues(blockedAlertType).Observe(float64(haversineCount))
	}()

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

		anchorLat, anchorLon, effectiveAreas := resolveOverride(td.OverrideLocationLabel, td.OverrideAreas, human)

		// Lazy haversine: compute once when first needed, cache for reuse.
		var dist int
		distComputed := false
		haversine := func() int {
			if !distComputed {
				dist = HaversineDistance(anchorLat, anchorLon, raidLat, raidLon)
				distComputed = true
				haversineCount++
			}
			return dist
		}

		// Specific gym tracking - check specificgym block
		if td.IsSpecificMatch {
			if human.BlockedAlertsSet["specificgym"] {
				continue
			}
		} else {
			// Distance/area check
			if td.Distance > 0 {
				if haversine() > td.Distance {
					continue
				}
			} else {
				if !areaOverlap(effectiveAreas, areas) {
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

		// Reuse cached haversine (or compute now for area-based users).
		actualDist := haversine()
		bearing := Bearing(anchorLat, anchorLon, raidLat, raidLon)

		result = append(result, webhook.MatchedUser{
			ID:                human.ID,
			Name:              human.Name,
			Type:              human.Type,
			Language:          human.Language,
			Latitude:          anchorLat,
			Longitude:         anchorLon,
			Template:          td.Template,
			Distance:          actualDist,
			Clean:             td.Clean,
			Ping:              td.Ping,
			Bearing:           int(math.Round(bearing)),
			CardinalDirection: CardinalDirection(bearing),
			RSVPChanges:       td.RSVPChanges,
			TrackDistance:     td.Distance,
			RuleUID:           td.UID,
		})
	}
	return result
}

type raidUserData struct {
	HumanID               string
	ProfileNo             int
	Distance              int
	Template              string
	Clean                 int
	Ping                  string
	RSVPChanges           int
	UID                   int64 // database UID of the matched raid/egg rule — surfaced on MatchedUser.RuleUID
	IsSpecificMatch       bool
	OverrideLocationLabel string
	OverrideAreas         []string
}

func areaOverlap(humanAreas []string, matchedAreas map[string]bool) bool {
	for _, ha := range humanAreas {
		if matchedAreas[ha] {
			return true
		}
	}
	return false
}
