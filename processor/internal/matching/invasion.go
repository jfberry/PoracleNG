package matching

import (
	"fmt"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// InvasionData holds processed invasion data for matching.
type InvasionData struct {
	PokestopID string
	GruntType  string // lowercased grunt type string
	Gender     int
	Latitude   float64
	Longitude  float64
}

// InvasionMatcher performs in-memory invasion matching.
type InvasionMatcher struct {
	StrictLocations     bool
	AreaSecurityEnabled bool
}

// Match returns all matched users for an invasion.
func (m *InvasionMatcher) Match(data *InvasionData, st *state.State) []webhook.MatchedUser {
	if st == nil {
		return nil
	}

	matchedAreaNames := st.Geofence.MatchedAreaNames(data.Latitude, data.Longitude)
	var trackings []trackingUserData

	gruntType := strings.ToLower(data.GruntType)

	for _, inv := range st.Invasions {
		// grunt_type match OR 'everything'
		invGrunt := strings.ToLower(inv.GruntType)
		if !(invGrunt == gruntType || invGrunt == "everything") {
			continue
		}
		// gender match OR 0 (any)
		if !(inv.Gender == data.Gender || inv.Gender == 0) {
			continue
		}

		trackings = append(trackings, trackingUserData{
			HumanID:   inv.ID,
			ProfileNo: inv.ProfileNo,
			Distance:  inv.Distance,
			Template:  inv.Template,
			Clean:     inv.Clean,
			Ping:      inv.Ping,
		})
	}

	return ValidateHumansGeneric(
		trackings,
		data.Latitude, data.Longitude,
		matchedAreaNames,
		m.AreaSecurityEnabled && m.StrictLocations,
		st.Humans,
		"invasion",
	)
}

// ResolveGruntType returns the grunt type string from the webhook fields.
func ResolveGruntType(incidentGruntType, gruntType, displayType int) string {
	if displayType >= 7 {
		return fmt.Sprintf("e%d", displayType)
	}
	if incidentGruntType > 0 && incidentGruntType != 352 {
		return fmt.Sprintf("%d", incidentGruntType)
	}
	if gruntType > 0 {
		return fmt.Sprintf("%d", gruntType)
	}
	return "0"
}
