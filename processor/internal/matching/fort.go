package matching

import (
	"strings"

	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// FortData holds processed fort update data for matching.
type FortData struct {
	ID          string
	FortType    string // "pokestop" or "gym"
	IsEmpty     bool
	ChangeTypes []string // e.g. ["new", "name", "location"]
	Latitude    float64
	Longitude   float64
}

// FortMatcher performs in-memory fort update matching.
type FortMatcher struct {
	StrictLocations     bool
	AreaSecurityEnabled bool
}

// Match returns all matched users for a fort update.
func (m *FortMatcher) Match(data *FortData, st *state.State) []webhook.MatchedUser {
	if st == nil {
		return nil
	}

	matchedAreaNames := st.Geofence.MatchedAreaNames(data.Latitude, data.Longitude)
	var trackings []trackingUserData

	for _, f := range st.Forts {
		// fort_type match OR 'everything'
		ft := strings.ToLower(f.FortType)
		if !(ft == strings.ToLower(data.FortType) || ft == "everything") {
			continue
		}

		// include_empty check
		if data.IsEmpty && !f.IncludeEmpty {
			continue
		}

		// change_types match
		if f.ChangeTypes != "[]" && f.ChangeTypes != "" {
			if !changeTypesMatch(f.ChangeTypes, data.ChangeTypes) {
				continue
			}
		}

		trackings = append(trackings, trackingUserData{
			HumanID:   f.ID,
			ProfileNo: f.ProfileNo,
			Distance:  f.Distance,
			Template:  f.Template,
			Clean:     false, // forts table doesn't have clean
			Ping:      f.Ping,
		})
	}

	return ValidateHumansGeneric(
		trackings,
		data.Latitude, data.Longitude,
		matchedAreaNames,
		m.AreaSecurityEnabled && m.StrictLocations,
		st.Humans,
		"forts",
	)
}

// changeTypesMatch checks if any of the actual change types match
// the tracked change types (stored as JSON array string like '["new","name"]').
func changeTypesMatch(trackedJSON string, actualChanges []string) bool {
	tracked := strings.ToLower(trackedJSON)
	for _, c := range actualChanges {
		if strings.Contains(tracked, `"`+strings.ToLower(c)+`"`) {
			return true
		}
	}
	return false
}
