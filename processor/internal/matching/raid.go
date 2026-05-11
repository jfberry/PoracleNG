package matching

import (
	"time"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/metrics"
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
	GeographicPrefilter bool
}

// matchRaids filters the given raid rule slice and returns the surviving
// raidUserData entries applying the per-rule raid filter logic.
func (m *RaidMatcher) matchRaids(raid *RaidData, rules []*db.RaidTracking) []raidUserData {
	exVal := 0
	if raid.Ex {
		exVal = 1
	}

	var out []raidUserData
	for _, r := range rules {
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

		out = append(out, raidUserData{
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
	return out
}

// MatchRaid returns all matched users for a raid along with the geofence
// areas that contain the gym.
func (m *RaidMatcher) MatchRaid(raid *RaidData, st *state.State) ([]webhook.MatchedUser, []webhook.MatchedArea) {
	start := time.Now()
	defer func() {
		metrics.MatchingDuration.WithLabelValues(metrics.TypeRaid).Observe(time.Since(start).Seconds())
	}()

	if st == nil {
		return nil, nil
	}

	areas, matchedAreaNames := st.Geofence.PointAreasAndNames(raid.Latitude, raid.Longitude)

	var trackingData []raidUserData
	if m.GeographicPrefilter && st.GeoIndex != nil {
		applicable := st.GeoIndex.ApplicableHumans(
			raid.Latitude, raid.Longitude,
			matchedAreaNames,
			m.AreaSecurityEnabled && m.StrictLocations,
		)
		metrics.MatchingApplicable.WithLabelValues(metrics.TypeRaid).Observe(float64(len(applicable)))
		rules := make([]*db.RaidTracking, 0, 4*len(applicable))
		for humanID := range applicable {
			rules = append(rules, st.RaidsByHuman[humanID]...)
		}
		trackingData = m.matchRaids(raid, rules)
	} else {
		trackingData = m.matchRaids(raid, st.Raids)
	}

	metrics.MatchingCandidates.WithLabelValues(metrics.TypeRaid).Observe(float64(len(trackingData)))

	users := ValidateHumansForRaid(
		trackingData,
		raid.Latitude, raid.Longitude,
		matchedAreaNames,
		m.AreaSecurityEnabled && m.StrictLocations,
		st.Humans,
		"raid",
	)
	return users, ConvertAreas(areas)
}

// matchEggs filters the given egg rule slice and returns the surviving
// raidUserData entries applying the per-rule egg filter logic.
func (m *RaidMatcher) matchEggs(egg *EggData, rules []*db.EggTracking) []raidUserData {
	exVal := 0
	if egg.Ex {
		exVal = 1
	}

	var out []raidUserData
	for _, e := range rules {
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

		out = append(out, raidUserData{
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
	return out
}

// MatchEgg returns all matched users for an egg along with the geofence
// areas that contain the gym.
// Port of raid.js:71-131 (eggWhoCares).
func (m *RaidMatcher) MatchEgg(egg *EggData, st *state.State) ([]webhook.MatchedUser, []webhook.MatchedArea) {
	start := time.Now()
	defer func() {
		metrics.MatchingDuration.WithLabelValues(metrics.TypeEgg).Observe(time.Since(start).Seconds())
	}()

	if st == nil {
		return nil, nil
	}

	areas, matchedAreaNames := st.Geofence.PointAreasAndNames(egg.Latitude, egg.Longitude)

	var trackingData []raidUserData
	if m.GeographicPrefilter && st.GeoIndex != nil {
		applicable := st.GeoIndex.ApplicableHumans(
			egg.Latitude, egg.Longitude,
			matchedAreaNames,
			m.AreaSecurityEnabled && m.StrictLocations,
		)
		metrics.MatchingApplicable.WithLabelValues(metrics.TypeEgg).Observe(float64(len(applicable)))
		rules := make([]*db.EggTracking, 0, 4*len(applicable))
		for humanID := range applicable {
			rules = append(rules, st.EggsByHuman[humanID]...)
		}
		trackingData = m.matchEggs(egg, rules)
	} else {
		trackingData = m.matchEggs(egg, st.Eggs)
	}

	metrics.MatchingCandidates.WithLabelValues(metrics.TypeEgg).Observe(float64(len(trackingData)))

	users := ValidateHumansForRaid(
		trackingData,
		egg.Latitude, egg.Longitude,
		matchedAreaNames,
		m.AreaSecurityEnabled && m.StrictLocations,
		st.Humans,
		"egg",
	)
	return users, ConvertAreas(areas)
}
