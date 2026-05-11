package matching

import (
	"time"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// NestData holds processed nest data for matching.
type NestData struct {
	NestID     int64
	PokemonID  int
	Form       int
	PokemonAvg float64
	Latitude   float64
	Longitude  float64
}

// NestMatcher performs in-memory nest matching.
type NestMatcher struct {
	StrictLocations     bool
	AreaSecurityEnabled bool
	GeographicPrefilter bool
}

// matchNests filters the given nest rule slice and returns the surviving
// trackingUserData entries applying the per-rule nest filter logic.
func (m *NestMatcher) matchNests(data *NestData, rules []*db.NestTracking) []trackingUserData {
	var out []trackingUserData
	for _, n := range rules {
		// pokemon_id match OR pokemon_id==0 (any)
		if !(n.PokemonID == data.PokemonID || n.PokemonID == 0) {
			continue
		}
		// form match OR form==0 (any)
		if !(n.Form == data.Form || n.Form == 0) {
			continue
		}
		// min_spawn_avg check
		if float64(n.MinSpawnAvg) > data.PokemonAvg {
			continue
		}
		out = append(out, trackingUserData{
			HumanID:   n.ID,
			ProfileNo: n.ProfileNo,
			Distance:  n.Distance,
			Template:  n.Template,
			Clean:     n.Clean,
			Ping:      n.Ping,
		})
	}
	return out
}

// Match returns all matched users for a nest along with the geofence areas
// that contain the nest centre.
func (m *NestMatcher) Match(data *NestData, st *state.State) ([]webhook.MatchedUser, []webhook.MatchedArea) {
	start := time.Now()
	defer func() {
		metrics.MatchingDuration.WithLabelValues("nest").Observe(time.Since(start).Seconds())
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
		metrics.MatchingApplicable.WithLabelValues("nest").Observe(float64(len(applicable)))
		for humanID := range applicable {
			trackings = append(trackings, m.matchNests(data, st.NestsByHuman[humanID])...)
		}
	} else {
		trackings = m.matchNests(data, st.Nests)
	}

	metrics.MatchingCandidates.WithLabelValues("nest").Observe(float64(len(trackings)))

	users := ValidateHumansGeneric(
		trackings,
		data.Latitude, data.Longitude,
		matchedAreaNames,
		m.AreaSecurityEnabled && m.StrictLocations,
		st.Humans,
		"nest",
	)
	return users, ConvertAreas(areas)
}
