package matching

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/state"
)

func makeGymTestState(gyms []*db.GymTracking, humans map[string]*db.Human) *state.State {
	fences := []geofence.Fence{
		{
			Name:             "TestArea",
			DisplayInMatches: true,
			Path: [][2]float64{
				{50.0, -1.0},
				{52.0, -1.0},
				{52.0, 1.0},
				{50.0, 1.0},
			},
		},
	}
	si := geofence.NewSpatialIndex(fences)

	return &state.State{
		Humans:   humans,
		Gyms:     gyms,
		Geofence: si,
		Fences:   fences,
	}
}

func TestGymMatchTeamChange(t *testing.T) {
	human := makeHuman("user1")
	gym := &db.GymTracking{
		ID: "user1", ProfileNo: 1, Team: 2,
		Distance: 0, Template: "1",
	}

	st := makeGymTestState([]*db.GymTracking{gym}, map[string]*db.Human{"user1": human})
	matcher := &GymMatcher{}

	data := &GymData{
		GymID:     "gym1",
		TeamID:    2,
		OldTeamID: 1, // team changed from 1 to 2
		Latitude:  51.0, Longitude: 0.0,
	}

	matched, _ := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for team change, got %d", len(matched))
	}
}

func TestGymMatchTeamAny(t *testing.T) {
	human := makeHuman("user1")
	gym := &db.GymTracking{
		ID: "user1", ProfileNo: 1, Team: 4, // any team
		Distance: 0, Template: "1",
	}

	st := makeGymTestState([]*db.GymTracking{gym}, map[string]*db.Human{"user1": human})
	matcher := &GymMatcher{}

	data := &GymData{
		GymID:     "gym1",
		TeamID:    3,
		OldTeamID: 1,
		Latitude:  51.0, Longitude: 0.0,
	}

	matched, _ := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for team=4 (any), got %d", len(matched))
	}
}

func TestGymMatchWrongTeam(t *testing.T) {
	human := makeHuman("user1")
	gym := &db.GymTracking{
		ID: "user1", ProfileNo: 1, Team: 2,
		Distance: 0, Template: "1",
	}

	st := makeGymTestState([]*db.GymTracking{gym}, map[string]*db.Human{"user1": human})
	matcher := &GymMatcher{}

	data := &GymData{
		GymID:     "gym1",
		TeamID:    3, // user wants team 2, got team 3
		OldTeamID: 1,
		Latitude:  51.0, Longitude: 0.0,
	}

	matched, _ := matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for wrong team, got %d", len(matched))
	}
}

func TestGymNoTeamChangeWithoutSlotOrBattle(t *testing.T) {
	human := makeHuman("user1")
	gym := &db.GymTracking{
		ID: "user1", ProfileNo: 1, Team: 2,
		Distance: 0, Template: "1",
	}

	st := makeGymTestState([]*db.GymTracking{gym}, map[string]*db.Human{"user1": human})
	matcher := &GymMatcher{}

	// Same team, no slot or battle change
	data := &GymData{
		GymID:             "gym1",
		TeamID:            2,
		OldTeamID:         2,
		SlotsAvailable:    3,
		OldSlotsAvailable: 3,
		InBattle:          false,
		OldInBattle:       false,
		Latitude:          51.0, Longitude: 0.0,
	}

	matched, _ := matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches when nothing changed, got %d", len(matched))
	}
}

func TestGymSlotChange(t *testing.T) {
	human := makeHuman("user1")
	gym := &db.GymTracking{
		ID: "user1", ProfileNo: 1, Team: 2,
		SlotChanges: true,
		Distance:    0, Template: "1",
	}

	st := makeGymTestState([]*db.GymTracking{gym}, map[string]*db.Human{"user1": human})
	matcher := &GymMatcher{}

	data := &GymData{
		GymID:             "gym1",
		TeamID:            2,
		OldTeamID:         2, // no team change
		SlotsAvailable:    4,
		OldSlotsAvailable: 3, // slot changed
		Latitude:          51.0, Longitude: 0.0,
	}

	matched, _ := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for slot change, got %d", len(matched))
	}
}

func TestGymBattleChange(t *testing.T) {
	human := makeHuman("user1")
	gym := &db.GymTracking{
		ID: "user1", ProfileNo: 1, Team: 2,
		BattleChanges: true,
		Distance:      0, Template: "1",
	}

	st := makeGymTestState([]*db.GymTracking{gym}, map[string]*db.Human{"user1": human})
	matcher := &GymMatcher{}

	data := &GymData{
		GymID:       "gym1",
		TeamID:      2,
		OldTeamID:   2,
		InBattle:    true,
		OldInBattle: false, // battle started
		Latitude:    51.0, Longitude: 0.0,
	}

	matched, _ := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for battle change, got %d", len(matched))
	}
}

func TestGymSpecificGym(t *testing.T) {
	human := makeHuman("user1")
	gymID := "gym1"
	gym := &db.GymTracking{
		ID: "user1", ProfileNo: 1, Team: 4,
		GymID:    &gymID,
		Distance: 0, Template: "1",
	}

	st := makeGymTestState([]*db.GymTracking{gym}, map[string]*db.Human{"user1": human})
	matcher := &GymMatcher{}

	// Correct gym
	data := &GymData{
		GymID: "gym1", TeamID: 2, OldTeamID: 1,
		Latitude: 51.0, Longitude: 0.0,
	}
	matched, _ := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for specific gym, got %d", len(matched))
	}

	// Wrong gym
	data.GymID = "gym2"
	matched, _ = matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for wrong gym, got %d", len(matched))
	}
}

func TestGymBlockedAlerts(t *testing.T) {
	human := makeHuman("user1")
	human.SetBlockedAlerts(`["gym","raid"]`)
	gym := &db.GymTracking{
		ID: "user1", ProfileNo: 1, Team: 4,
		Distance: 0, Template: "1",
	}

	st := makeGymTestState([]*db.GymTracking{gym}, map[string]*db.Human{"user1": human})
	matcher := &GymMatcher{}

	data := &GymData{
		GymID: "gym1", TeamID: 2, OldTeamID: 1,
		Latitude: 51.0, Longitude: 0.0,
	}

	matched, _ := matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for blocked alerts, got %d", len(matched))
	}
}

// Verify that blocking "specificgym" does NOT block regular gym alerts
func TestGymBlockedSpecificGymNotGym(t *testing.T) {
	human := makeHuman("user1")
	human.SetBlockedAlerts(`["specificgym"]`)
	gym := &db.GymTracking{
		ID: "user1", ProfileNo: 1, Team: 4,
		Distance: 0, Template: "1",
	}

	st := makeGymTestState([]*db.GymTracking{gym}, map[string]*db.Human{"user1": human})
	matcher := &GymMatcher{}

	data := &GymData{
		GymID: "gym1", TeamID: 2, OldTeamID: 1,
		Latitude: 51.0, Longitude: 0.0,
	}

	matched, _ := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match (specificgym blocked should not block gym alerts), got %d", len(matched))
	}
}

// Based on real gym_details webhook: team=2, slots_available=2, latitude=51.310916, longitude=0.87744
func TestGymRealWorldData(t *testing.T) {
	human := makeHuman("user1")
	gym := &db.GymTracking{
		ID: "user1", ProfileNo: 1, Team: 4,
		Distance: 0, Template: "1",
	}

	st := makeGymTestState([]*db.GymTracking{gym}, map[string]*db.Human{"user1": human})
	matcher := &GymMatcher{}

	data := &GymData{
		GymID:          "77aea786b4ae42239646365c2b8c8b2f.16",
		TeamID:         2,
		OldTeamID:      1,
		SlotsAvailable: 2,
		Latitude:       51.310916,
		Longitude:      0.87744,
	}

	matched, _ := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for real world gym data, got %d", len(matched))
	}
}
