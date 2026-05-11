package matching

import (
	"time"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// MaxbattleData holds processed maxbattle data for matching.
type MaxbattleData struct {
	StationID string
	PokemonID int
	Form      int
	Level     int
	Gmax      int
	Evolution int
	Move1     int
	Move2     int
	Latitude  float64
	Longitude float64
}

// MaxbattleMatcher performs in-memory maxbattle matching.
type MaxbattleMatcher struct {
	StrictLocations     bool
	AreaSecurityEnabled bool
	GeographicPrefilter bool
}

// matchMaxbattles filters the given maxbattle rule slice and returns the surviving
// trackingUserData entries applying the per-rule maxbattle filter logic.
func (m *MaxbattleMatcher) matchMaxbattles(data *MaxbattleData, rules []*db.MaxbattleTracking) []trackingUserData {
	var out []trackingUserData
	for _, mb := range rules {
		// Pokemon match: exact pokemon_id, or 9000 (level-based) with level match
		if mb.PokemonID == 9000 {
			// Level-based tracking: level must match or be 90 (all levels)
			if !(mb.Level == data.Level || mb.Level == 90) {
				continue
			}
		} else {
			// Pokemon-specific tracking
			if mb.PokemonID != data.PokemonID {
				continue
			}
		}

		// Gmax filter: 0 matches anything, 1 requires gmax
		if mb.Gmax == 1 && data.Gmax != 1 {
			continue
		}

		// Form filter: 0 matches any
		if mb.Form != 0 && mb.Form != data.Form {
			continue
		}

		// Evolution filter: 9000 matches any
		if mb.Evolution != 9000 && mb.Evolution != data.Evolution {
			continue
		}

		// Move filter: 9000 matches any, otherwise must match either move
		if mb.Move != 9000 && mb.Move != data.Move1 && mb.Move != data.Move2 {
			continue
		}

		// Station ID filter: nil matches any
		isSpecificStation := mb.StationID != nil
		if isSpecificStation && *mb.StationID != data.StationID {
			continue
		}

		out = append(out, trackingUserData{
			HumanID:           mb.ID,
			ProfileNo:         mb.ProfileNo,
			Distance:          mb.Distance,
			Template:          mb.Template,
			Clean:             mb.Clean,
			Ping:              mb.Ping,
			IsSpecificStation: isSpecificStation,
		})
	}
	return out
}

// Match returns all matched users for a maxbattle along with the geofence
// areas that contain the station.
func (m *MaxbattleMatcher) Match(data *MaxbattleData, st *state.State) ([]webhook.MatchedUser, []webhook.MatchedArea) {
	start := time.Now()
	defer func() {
		metrics.MatchingDuration.WithLabelValues("maxbattle").Observe(time.Since(start).Seconds())
	}()

	if st == nil {
		return nil, nil
	}

	areas, matchedAreaNames := st.Geofence.PointAreasAndNames(data.Latitude, data.Longitude)

	var trackings []trackingUserData
	if m.GeographicPrefilter && st.GeoIndex != nil {
		applicable := st.GeoIndex.ApplicableHumans(
			data.Latitude, data.Longitude,
			matchedAreaNames,
			m.AreaSecurityEnabled && m.StrictLocations,
		)
		metrics.MatchingApplicable.WithLabelValues("maxbattle").Observe(float64(len(applicable)))
		for humanID := range applicable {
			trackings = append(trackings, m.matchMaxbattles(data, st.MaxbattlesByHuman[humanID])...)
		}
	} else {
		trackings = m.matchMaxbattles(data, st.Maxbattles)
	}

	// Filter out users with blocked "specificstation" alerts for station-specific trackings
	var filtered []trackingUserData
	for _, td := range trackings {
		if td.IsSpecificStation {
			human := st.Humans[td.HumanID]
			if human != nil && human.BlockedAlertsSet["specificstation"] {
				continue
			}
		}
		filtered = append(filtered, td)
	}

	metrics.MatchingCandidates.WithLabelValues("maxbattle").Observe(float64(len(filtered)))

	users := ValidateHumansGeneric(
		filtered,
		data.Latitude, data.Longitude,
		matchedAreaNames,
		m.AreaSecurityEnabled && m.StrictLocations,
		st.Humans,
		"maxbattle",
	)
	return users, ConvertAreas(areas)
}
