package db_test

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/state"
)

// TestLoadedOverridesMatchEitherSpelling: the matcher state loaders key each
// loaded override_areas entry with NormalizeAreaKey, so a stored fence name
// (underscores) and a legacy spaced name both match the fence's spaced key.
func TestLoadedOverridesMatchEitherSpelling(t *testing.T) {
	for _, raw := range []string{`["gent_centrum"]`, `["gent centrum"]`, `["Gent_Centrum"]`} {
		keys := db.LoadOverrideKeys(raw)
		if len(keys) != 1 || keys[0] != "gent centrum" {
			t.Errorf("LoadOverrideKeys(%s) = %v, want [gent centrum]", raw, keys)
		}
	}
	if got := db.LoadOverrideKeys(""); got != nil {
		t.Errorf("LoadOverrideKeys(\"\") = %v, want nil", got)
	}
	if got := db.LoadOverrideKeys("not json"); got != nil {
		t.Errorf("LoadOverrideKeys(bad json) = %v, want nil", got)
	}
}

// overrideState builds a state with fence Gent_Centrum around (51, 0) and one
// human with no areas of their own, so only the rule's override can match.
func overrideState() *state.State {
	fences := []geofence.Fence{{
		Name: "Gent_Centrum", DisplayInMatches: true,
		Path: [][2]float64{{50, -1}, {52, -1}, {52, 1}, {50, 1}},
	}}
	si := geofence.NewSpatialIndex(fences)
	return &state.State{
		Humans: map[string]*db.Human{"u1": {
			ID: "u1", Type: "discord:user", Enabled: true,
			Latitude: 51, Longitude: 0, CurrentProfileNo: 1,
		}},
		Geofence: si,
		Fences:   fences,
	}
}

func underscoredOverride() []string { return db.LoadOverrideKeys(`["gent_centrum"]`) }

func TestLoadedOverride_ValidateHumans(t *testing.T) {
	st := overrideState()
	_, names := st.Geofence.PointAreasAndNames(51, 0)
	rule := &db.MonsterTracking{ID: "u1", ProfileNo: 1, PokemonID: 25, OverrideAreas: underscoredOverride()}
	out := matching.ValidateHumans([]*db.MonsterTracking{rule}, 51, 0, names, false, st.Humans)
	if len(out) != 1 {
		t.Fatalf("pokemon override gent_centrum should match fence Gent_Centrum; got %d", len(out))
	}
}

func TestLoadedOverride_ValidateHumansForRaid(t *testing.T) {
	st := overrideState()
	st.Raids = []*db.RaidTracking{{
		ID: "u1", ProfileNo: 1, PokemonID: 150, Level: 5, Team: 4,
		Evolution: 9000, Move: 9000, OverrideAreas: underscoredOverride(),
	}}
	out, _ := (&matching.RaidMatcher{}).MatchRaid(&matching.RaidData{
		GymID: "gym1", PokemonID: 150, Level: 5, TeamID: 1, Latitude: 51, Longitude: 0,
	}, st)
	if len(out) != 1 {
		t.Fatalf("raid override gent_centrum should match fence Gent_Centrum; got %d", len(out))
	}
}

func TestLoadedOverride_ValidateHumansGeneric(t *testing.T) {
	st := overrideState()
	st.Lures = []*db.LureTracking{{ID: "u1", ProfileNo: 1, LureID: 501, OverrideAreas: underscoredOverride()}}
	out, _ := (&matching.LureMatcher{}).Match(&matching.LureData{
		PokestopID: "stop1", LureID: 501, Latitude: 51, Longitude: 0,
	}, st)
	if len(out) != 1 {
		t.Fatalf("lure override gent_centrum should match fence Gent_Centrum; got %d", len(out))
	}
}

func TestLoadedOverride_Gym(t *testing.T) {
	st := overrideState()
	st.Gyms = []*db.GymTracking{{ID: "u1", ProfileNo: 1, Team: 2, OverrideAreas: underscoredOverride()}}
	out, _ := (&matching.GymMatcher{}).Match(&matching.GymData{
		GymID: "gym1", TeamID: 2, OldTeamID: 1, Latitude: 51, Longitude: 0,
	}, st)
	if len(out) != 1 {
		t.Fatalf("gym override gent_centrum should match fence Gent_Centrum; got %d", len(out))
	}
}
