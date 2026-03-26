package matching

import (
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// RaidData holds the processed raid data for matching.
type RaidData struct {
	GymID     string
	PokemonID int
	Form      int
	Level     int
	TeamID    int
	Ex        bool
	Evolution int
	Move1     int
	Move2     int
	Latitude  float64
	Longitude float64
}

// EggData holds the processed egg data for matching.
type EggData struct {
	GymID     string
	Level     int
	TeamID    int
	Ex        bool
	Latitude  float64
	Longitude float64
}

// RaidMatcher performs in-memory raid matching.
// Port of raid.js:8-69 (raidWhoCares).
type RaidMatcher struct {
	StrictLocations     bool
	AreaSecurityEnabled bool
}

// MatchRaid returns all matched users for a raid.
func (m *RaidMatcher) MatchRaid(raid *RaidData, st *state.State) []webhook.MatchedUser {
	if st == nil {
		return nil
	}

	matchedAreaNames := st.Geofence.MatchedAreaNames(raid.Latitude, raid.Longitude)
	var trackingData []raidUserData

	exVal := 0
	if raid.Ex {
		exVal = 1
	}

	for _, r := range st.Raids {
		// pokemon_id match OR (pokemon_id==9000 AND (level matches OR level==90))
		if !(r.PokemonID == raid.PokemonID || (r.PokemonID == 9000 && (r.Level == raid.Level || r.Level == 90))) {
			continue
		}
		// team match OR team==4 (any)
		if !(r.Team == raid.TeamID || r.Team == 4) {
			continue
		}
		// exclusive match OR exclusive==0 (any)
		rExVal := 0
		if r.Exclusive {
			rExVal = 1
		}
		if !(rExVal == exVal || rExVal == 0) {
			continue
		}
		// form match OR form==0 (any)
		if !(r.Form == raid.Form || r.Form == 0) {
			continue
		}
		// evolution match
		if !(r.Evolution == 9000 || r.Evolution == raid.Evolution) {
			continue
		}
		// move match
		if !(r.Move == 9000 || r.Move == raid.Move1 || r.Move == raid.Move2) {
			continue
		}

		isSpecificGym := false
		if r.GymID.Valid && r.GymID.String == raid.GymID {
			isSpecificGym = true
		} else if r.GymID.Valid {
			// Specific gym tracking but doesn't match this gym
			continue
		}

		trackingData = append(trackingData, raidUserData{
			HumanID:       r.ID,
			ProfileNo:     r.ProfileNo,
			Distance:      r.Distance,
			Template:      r.Template,
			Clean:         r.Clean,
			Ping:          r.Ping,
			RSVPChanges:   r.RSVPChanges,
			IsSpecificGym: isSpecificGym,
		})
	}

	return ValidateHumansForRaid(
		trackingData,
		raid.Latitude, raid.Longitude,
		matchedAreaNames,
		m.AreaSecurityEnabled && m.StrictLocations,
		st.Humans,
		"raid",
	)
}

// MatchEgg returns all matched users for an egg.
// Port of raid.js:71-131 (eggWhoCares).
func (m *RaidMatcher) MatchEgg(egg *EggData, st *state.State) []webhook.MatchedUser {
	if st == nil {
		return nil
	}

	matchedAreaNames := st.Geofence.MatchedAreaNames(egg.Latitude, egg.Longitude)
	var trackingData []raidUserData

	exVal := 0
	if egg.Ex {
		exVal = 1
	}

	for _, e := range st.Eggs {
		// level match OR level==90 (any)
		if !(e.Level == egg.Level || e.Level == 90) {
			continue
		}
		// team match OR team==4 (any)
		if !(e.Team == egg.TeamID || e.Team == 4) {
			continue
		}
		// exclusive match OR exclusive==0 (any)
		eExVal := 0
		if e.Exclusive {
			eExVal = 1
		}
		if !(eExVal == exVal || eExVal == 0) {
			continue
		}

		isSpecificGym := false
		if e.GymID.Valid && e.GymID.String == egg.GymID {
			isSpecificGym = true
		} else if e.GymID.Valid {
			continue
		}

		trackingData = append(trackingData, raidUserData{
			HumanID:       e.ID,
			ProfileNo:     e.ProfileNo,
			Distance:      e.Distance,
			Template:      e.Template,
			Clean:         e.Clean,
			Ping:          e.Ping,
			RSVPChanges:   e.RSVPChanges,
			IsSpecificGym: isSpecificGym,
		})
	}

	return ValidateHumansForRaid(
		trackingData,
		egg.Latitude, egg.Longitude,
		matchedAreaNames,
		m.AreaSecurityEnabled && m.StrictLocations,
		st.Humans,
		"egg",
	)
}
