package matching

import (
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

// Match returns all matched users for a lure.
func (m *LureMatcher) Match(data *LureData, st *state.State) []webhook.MatchedUser {
	if st == nil {
		return nil
	}

	matchedAreaNames := st.Geofence.MatchedAreaNames(data.Latitude, data.Longitude)
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
			Clean:     boolToInt(l.Clean),
			Ping:      l.Ping,
		})
	}

	return ValidateHumansGeneric(
		trackings,
		data.Latitude, data.Longitude,
		matchedAreaNames,
		m.AreaSecurityEnabled && m.StrictLocations,
		st.Humans,
		"lure",
	)
}
