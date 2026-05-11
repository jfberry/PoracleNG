package matching

import (
	"time"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// LureData holds processed lure data for matching.
type LureData struct {
	PokestopID string
	LureID     int
	Latitude   float64
	Longitude  float64
}

// LureMatcher performs in-memory lure matching.
type LureMatcher struct {
	StrictLocations     bool
	AreaSecurityEnabled bool
	GeographicPrefilter bool
}

// matchLures filters the given lure rule slice and returns the surviving
// trackingUserData entries applying the per-rule lure filter logic.
func (m *LureMatcher) matchLures(data *LureData, rules []*db.LureTracking) []trackingUserData {
	var out []trackingUserData
	for _, l := range rules {
		// lure_id match OR lure_id==0 (any)
		if !(l.LureID == data.LureID || l.LureID == 0) {
			continue
		}
		out = append(out, trackingUserData{
			HumanID:   l.ID,
			ProfileNo: l.ProfileNo,
			Distance:  l.Distance,
			Template:  l.Template,
			Clean:     l.Clean,
			Ping:      l.Ping,
		})
	}
	return out
}

// Match returns all matched users for a lure along with the geofence areas
// that contain the pokestop.
func (m *LureMatcher) Match(data *LureData, st *state.State) ([]webhook.MatchedUser, []webhook.MatchedArea) {
	start := time.Now()
	defer func() {
		metrics.MatchingDuration.WithLabelValues("lure").Observe(time.Since(start).Seconds())
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
		metrics.MatchingApplicable.WithLabelValues("lure").Observe(float64(len(applicable)))
		for humanID := range applicable {
			trackings = append(trackings, m.matchLures(data, st.LuresByHuman[humanID])...)
		}
	} else {
		trackings = m.matchLures(data, st.Lures)
	}

	metrics.MatchingCandidates.WithLabelValues("lure").Observe(float64(len(trackings)))

	users := ValidateHumansGeneric(
		trackings,
		data.Latitude, data.Longitude,
		matchedAreaNames,
		m.AreaSecurityEnabled && m.StrictLocations,
		st.Humans,
		"lure",
	)
	return users, ConvertAreas(areas)
}
