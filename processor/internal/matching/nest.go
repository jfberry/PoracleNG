package matching

import (
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
}

// Match returns all matched users for a nest along with the geofence areas
// that contain the nest centre.
func (m *NestMatcher) Match(data *NestData, st *state.State) ([]webhook.MatchedUser, []webhook.MatchedArea) {
	if st == nil {
		return nil, nil
	}

	areas, matchedAreaNames := st.Geofence.PointAreasAndNames(data.Latitude, data.Longitude)
	var trackings []trackingUserData

	for _, n := range st.Nests {
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

		trackings = append(trackings, trackingUserData{
			HumanID:   n.ID,
			ProfileNo: n.ProfileNo,
			Distance:  n.Distance,
			Template:  n.Template,
			Clean:     n.Clean,
			Ping:      n.Ping,
		})
	}

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
