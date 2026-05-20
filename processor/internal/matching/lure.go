package matching

import (
	"time"

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
}

// Match returns all matched users for a lure along with the geofence areas
// that contain the pokestop.
func (m *LureMatcher) Match(data *LureData, st *state.State) ([]webhook.MatchedUser, []webhook.MatchedArea) {
	start := time.Now()
	defer func() {
		metrics.MatchingDuration.WithLabelValues(metrics.TypeLure).Observe(time.Since(start).Seconds())
	}()

	if st == nil {
		return nil, nil
	}

	areas, matchedAreaNames := st.Geofence.PointAreasAndNames(data.Latitude, data.Longitude)
	var trackings []trackingUserData

	for _, l := range st.Lures {
		// lure_id match OR lure_id==0 (any)
		if !(l.LureID == data.LureID || l.LureID == 0) {
			continue
		}

		trackings = append(trackings, trackingUserData{
			HumanID:   l.ID,
			ProfileNo: l.ProfileNo,
			Distance:  l.Distance,
			Template:  l.Template,
			Clean:     l.Clean,
			Ping:      l.Ping,
			UID:       l.UID,
		})
	}

	metrics.MatchingCandidates.WithLabelValues(metrics.TypeLure).Observe(float64(len(trackings)))

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
