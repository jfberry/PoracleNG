package matching

import (
	"math"
	"time"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// GymData holds processed gym data for matching.
type GymData struct {
	GymID             string
	TeamID            int
	OldTeamID         int
	SlotsAvailable    int
	OldSlotsAvailable int
	InBattle          bool
	Latitude          float64
	Longitude         float64
}

// GymMatcher performs in-memory gym matching.
type GymMatcher struct {
	StrictLocations     bool
	AreaSecurityEnabled bool
}

// matchGyms filters the given gym rule slice and returns the surviving
// trackingUserData entries applying the per-rule gym filter logic.
// IsSpecificMatch is set on each entry so validateHumansForGym does not
// need to re-scan the gym tracking slice.
func (m *GymMatcher) matchGyms(data *GymData, rules []*db.GymTracking) []trackingUserData {
	teamChanged := data.OldTeamID != data.TeamID
	slotsChanged := data.OldSlotsAvailable != data.SlotsAvailable

	var out []trackingUserData
	for _, g := range rules {
		// team match OR team==4 (any)
		if !(g.Team == data.TeamID || g.Team == 4) {
			continue
		}

		// Change detection logic
		if !teamChanged {
			// No team change — only alert if tracking slot or battle changes
			wantsThis := false
			if slotsChanged && g.SlotChanges {
				wantsThis = true
			}
			if data.InBattle && g.BattleChanges {
				wantsThis = true
			}
			if !wantsThis {
				continue
			}
		}

		// Specific gym or area/distance
		isSpecificMatch := false
		if g.GymID != nil && *g.GymID == data.GymID {
			// Specific gym match — skip area/distance check
			isSpecificMatch = true
		} else if g.GymID != nil {
			// Tracking a specific gym but it's not this one
			continue
		}
		// If g.GymID is nil, area/distance is checked by validateHumansForGym

		out = append(out, trackingUserData{
			HumanID:         g.ID,
			ProfileNo:       g.ProfileNo,
			Distance:        g.Distance,
			Template:        g.Template,
			Clean:           g.Clean,
			Ping:            g.Ping,
			IsSpecificMatch: isSpecificMatch,
		})
	}
	return out
}

// Match returns all matched users for a gym change along with the geofence
// areas that contain the gym.
func (m *GymMatcher) Match(data *GymData, st *state.State) ([]webhook.MatchedUser, []webhook.MatchedArea) {
	start := time.Now()
	defer func() {
		metrics.MatchingDuration.WithLabelValues(metrics.TypeGym).Observe(time.Since(start).Seconds())
	}()

	if st == nil {
		return nil, nil
	}

	areas, matchedAreaNames := st.Geofence.PointAreasAndNames(data.Latitude, data.Longitude)

	trackings := m.matchGyms(data, st.Gyms)

	metrics.MatchingCandidates.WithLabelValues(metrics.TypeGym).Observe(float64(len(trackings)))

	users := validateHumansForGym(
		trackings,
		data.Latitude, data.Longitude,
		matchedAreaNames,
		m.AreaSecurityEnabled && m.StrictLocations,
		st.Humans,
	)
	return users, ConvertAreas(areas)
}

// validateHumansForGym is like ValidateHumansGeneric but handles specific gym tracking.
// The IsSpecificMatch flag on each trackingUserData is set by matchGyms, so no full
// gym-tracking scan is needed here.
func validateHumansForGym(
	trackings []trackingUserData,
	lat, lon float64,
	matchedAreaNames map[string]bool,
	strictAreasEnabled bool,
	humans map[string]*db.Human,
) []webhook.MatchedUser {
	if len(trackings) == 0 {
		return nil
	}

	haversineCount := 0
	defer func() {
		metrics.MatchingHaversines.WithLabelValues(metrics.TypeGym).Observe(float64(haversineCount))
	}()

	seen := make(map[string]bool)
	var result []webhook.MatchedUser

	for _, td := range trackings {
		human, ok := humans[td.HumanID]
		if !ok || !human.Enabled || human.AdminDisable {
			continue
		}
		if human.BlockedAlertsSet["gym"] {
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
				if !areaOverlap(human.Area, matchedAreaNames) {
					continue
				}
			}
		}

		if strictAreasEnabled && human.AreaRestriction != nil {
			if !areaOverlap(human.AreaRestriction, matchedAreaNames) {
				continue
			}
		}

		if seen[human.ID] {
			continue
		}
		seen[human.ID] = true

		// Reuse cached haversine (or compute now for area-based / specific-gym users).
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
		})
	}
	return result
}
