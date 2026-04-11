package matching

import (
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
}

// Match returns all matched users for a maxbattle.
func (m *MaxbattleMatcher) Match(data *MaxbattleData, st *state.State) []webhook.MatchedUser {
	if st == nil {
		return nil
	}

	matchedAreaNames := st.Geofence.MatchedAreaNames(data.Latitude, data.Longitude)
	var trackings []trackingUserData

	for _, mb := range st.Maxbattles {
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

		trackings = append(trackings, trackingUserData{
			HumanID:          mb.ID,
			ProfileNo:        mb.ProfileNo,
			Distance:         mb.Distance,
			Template:         mb.Template,
			Clean:            boolToInt(mb.Clean),
			Ping:             mb.Ping,
			IsSpecificStation: isSpecificStation,
		})
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

	return ValidateHumansGeneric(
		filtered,
		data.Latitude, data.Longitude,
		matchedAreaNames,
		m.AreaSecurityEnabled && m.StrictLocations,
		st.Humans,
		"maxbattle",
	)
}
