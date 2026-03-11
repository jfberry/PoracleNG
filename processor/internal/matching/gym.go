package matching

import (
	"strings"

	"github.com/pokemon/poracleng/processor/internal/db"
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
	OldInBattle       bool
	Latitude          float64
	Longitude         float64
}

// GymMatcher performs in-memory gym matching.
type GymMatcher struct {
	StrictLocations     bool
	AreaSecurityEnabled bool
}

// Match returns all matched users for a gym change.
func (m *GymMatcher) Match(data *GymData, st *state.State) []webhook.MatchedUser {
	if st == nil {
		return nil
	}

	teamChanged := data.OldTeamID != data.TeamID
	slotsChanged := data.OldSlotsAvailable != data.SlotsAvailable
	battleChanged := data.InBattle && !data.OldInBattle

	matchedAreaNames := st.Geofence.MatchedAreaNames(data.Latitude, data.Longitude)
	var trackings []trackingUserData

	for _, g := range st.Gyms {
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
			if battleChanged && g.BattleChanges {
				wantsThis = true
			}
			if !wantsThis {
				continue
			}
		}

		// Specific gym or area/distance
		if g.GymID != nil && *g.GymID == data.GymID {
			// Specific gym match — skip area/distance check
		} else if g.GymID != nil {
			// Tracking a specific gym but it's not this one
			continue
		}
		// If g.GymID is nil, area/distance is checked by ValidateHumansGeneric

		trackings = append(trackings, trackingUserData{
			HumanID:   g.ID,
			ProfileNo: g.ProfileNo,
			Distance:  g.Distance,
			Template:  g.Template,
			Clean:     g.Clean,
			Ping:      g.Ping,
		})
	}

	return validateHumansForGym(
		trackings,
		data.Latitude, data.Longitude,
		matchedAreaNames,
		m.AreaSecurityEnabled && m.StrictLocations,
		st.Humans,
		st.Gyms,
		data.GymID,
	)
}

// validateHumansForGym is like ValidateHumansGeneric but handles specific gym tracking.
func validateHumansForGym(
	trackings []trackingUserData,
	lat, lon float64,
	matchedAreaNames []string,
	strictAreasEnabled bool,
	humans map[string]*db.Human,
	gymTrackings []*db.GymTracking,
	gymID string,
) []webhook.MatchedUser {
	if len(trackings) == 0 {
		return nil
	}

	// Build a quick lookup for specific gym trackings
	specificGymUsers := make(map[string]bool)
	for _, g := range gymTrackings {
		if g.GymID != nil && *g.GymID == gymID {
			specificGymUsers[g.ID] = true
		}
	}

	seen := make(map[string]bool)
	var result []webhook.MatchedUser

	for _, td := range trackings {
		human, ok := humans[td.HumanID]
		if !ok || !human.Enabled || human.AdminDisable {
			continue
		}
		if strings.Contains(human.BlockedAlerts, "gym") {
			continue
		}
		if td.ProfileNo != human.CurrentProfileNo {
			continue
		}

		isSpecificGym := specificGymUsers[td.HumanID]
		if isSpecificGym {
			if strings.Contains(human.BlockedAlerts, "specificgym") {
				continue
			}
		} else {
			// Distance/area check
			if td.Distance > 0 {
				dist := HaversineDistance(human.Latitude, human.Longitude, lat, lon)
				if dist > td.Distance {
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

		result = append(result, webhook.MatchedUser{
			ID:        human.ID,
			Name:      human.Name,
			Type:      human.Type,
			Language:  human.Language,
			Latitude:  human.Latitude,
			Longitude: human.Longitude,
			Template:  td.Template,
			Distance:  td.Distance,
			Clean:     td.Clean,
			Ping:      td.Ping,
		})
	}
	return result
}
