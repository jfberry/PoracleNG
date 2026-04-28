package matching

import (
	"database/sql"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/state"
)

func makeRaidTestState(raids []*db.RaidTracking, eggs []*db.EggTracking, humans map[string]*db.Human) *state.State {
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
		Raids:    raids,
		Eggs:     eggs,
		Geofence: si,
		Fences:   fences,
	}
}

func TestRaidMatchBasic(t *testing.T) {
	human := makeHuman("user1")
	raid := &db.RaidTracking{
		ID: "user1", ProfileNo: 1, PokemonID: 150, Level: 5,
		Team: 4, Exclusive: false, Form: 0, Evolution: 9000,
		Move: 9000, Distance: 0, Template: "1", Clean: 0,
	}

	st := makeRaidTestState([]*db.RaidTracking{raid}, nil, map[string]*db.Human{"user1": human})
	matcher := &RaidMatcher{}

	raidData := &RaidData{
		GymID: "gym1", PokemonID: 150, Form: 0, Level: 5,
		TeamID: 1, Ex: false, Evolution: 0, Move1: 100, Move2: 200,
		Latitude: 51.0, Longitude: 0.0,
	}

	matched, _ := matcher.MatchRaid(raidData, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 raid match, got %d", len(matched))
	}
}

func TestRaidMatchLevel9000(t *testing.T) {
	human := makeHuman("user1")
	raid := &db.RaidTracking{
		ID: "user1", ProfileNo: 1, PokemonID: 9000, Level: 5,
		Team: 4, Exclusive: false, Form: 0, Evolution: 9000,
		Move: 9000, Distance: 0, Template: "1",
	}

	st := makeRaidTestState([]*db.RaidTracking{raid}, nil, map[string]*db.Human{"user1": human})
	matcher := &RaidMatcher{}

	raidData := &RaidData{
		GymID: "gym1", PokemonID: 999, Form: 0, Level: 5,
		TeamID: 1, Ex: false, Evolution: 0, Move1: 100, Move2: 200,
		Latitude: 51.0, Longitude: 0.0,
	}

	matched, _ := matcher.MatchRaid(raidData, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 raid match for pokemon_id=9000, got %d", len(matched))
	}
}

func TestRaidMatchTeamFilter(t *testing.T) {
	human := makeHuman("user1")
	raid := &db.RaidTracking{
		ID: "user1", ProfileNo: 1, PokemonID: 150, Level: 5,
		Team: 1, Exclusive: false, Form: 0, Evolution: 9000,
		Move: 9000, Distance: 0, Template: "1",
	}

	st := makeRaidTestState([]*db.RaidTracking{raid}, nil, map[string]*db.Human{"user1": human})
	matcher := &RaidMatcher{}

	// Wrong team
	raidData := &RaidData{
		GymID: "gym1", PokemonID: 150, Form: 0, Level: 5,
		TeamID: 2, Ex: false, Evolution: 0, Move1: 100, Move2: 200,
		Latitude: 51.0, Longitude: 0.0,
	}

	matched, _ := matcher.MatchRaid(raidData, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for wrong team, got %d", len(matched))
	}
}

func TestRaidMatchMoveFilter(t *testing.T) {
	human := makeHuman("user1")
	raid := &db.RaidTracking{
		ID: "user1", ProfileNo: 1, PokemonID: 150, Level: 5,
		Team: 4, Exclusive: false, Form: 0, Evolution: 9000,
		Move: 100, Distance: 0, Template: "1",
	}

	st := makeRaidTestState([]*db.RaidTracking{raid}, nil, map[string]*db.Human{"user1": human})
	matcher := &RaidMatcher{}

	// Move matches move_1
	raidData := &RaidData{
		GymID: "gym1", PokemonID: 150, Form: 0, Level: 5,
		TeamID: 1, Ex: false, Evolution: 0, Move1: 100, Move2: 200,
		Latitude: 51.0, Longitude: 0.0,
	}
	matched, _ := matcher.MatchRaid(raidData, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for matching move_1, got %d", len(matched))
	}

	// Move matches move_2
	raidData.Move1 = 300
	raidData.Move2 = 100
	matched, _ = matcher.MatchRaid(raidData, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for matching move_2, got %d", len(matched))
	}

	// No match
	raidData.Move1 = 300
	raidData.Move2 = 400
	matched, _ = matcher.MatchRaid(raidData, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for non-matching moves, got %d", len(matched))
	}
}

func TestRaidMatchSpecificGym(t *testing.T) {
	human := makeHuman("user1")
	raid := &db.RaidTracking{
		ID: "user1", ProfileNo: 1, PokemonID: 150, Level: 5,
		Team: 4, Exclusive: false, Form: 0, Evolution: 9000,
		Move: 9000, Distance: 0, Template: "1",
		GymID: sql.NullString{String: "gym1", Valid: true},
	}

	st := makeRaidTestState([]*db.RaidTracking{raid}, nil, map[string]*db.Human{"user1": human})
	matcher := &RaidMatcher{}

	// Correct gym
	raidData := &RaidData{
		GymID: "gym1", PokemonID: 150, Level: 5,
		TeamID: 1, Latitude: 51.0, Longitude: 0.0,
		Evolution: 0, Move1: 100, Move2: 200,
	}
	matched, _ := matcher.MatchRaid(raidData, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for specific gym, got %d", len(matched))
	}

	// Wrong gym
	raidData.GymID = "gym2"
	matched, _ = matcher.MatchRaid(raidData, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for wrong gym, got %d", len(matched))
	}
}

func TestEggMatchBasic(t *testing.T) {
	human := makeHuman("user1")
	egg := &db.EggTracking{
		ID: "user1", ProfileNo: 1, Level: 5,
		Team: 4, Exclusive: false,
		Distance: 0, Template: "1",
	}

	st := makeRaidTestState(nil, []*db.EggTracking{egg}, map[string]*db.Human{"user1": human})
	matcher := &RaidMatcher{}

	eggData := &EggData{
		GymID: "gym1", Level: 5, TeamID: 1, Ex: false,
		Latitude: 51.0, Longitude: 0.0,
	}

	matched, _ := matcher.MatchEgg(eggData, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 egg match, got %d", len(matched))
	}
}

func TestEggMatchLevel90(t *testing.T) {
	human := makeHuman("user1")
	egg := &db.EggTracking{
		ID: "user1", ProfileNo: 1, Level: 90, // any level
		Team: 4, Exclusive: false,
		Distance: 0, Template: "1",
	}

	st := makeRaidTestState(nil, []*db.EggTracking{egg}, map[string]*db.Human{"user1": human})
	matcher := &RaidMatcher{}

	eggData := &EggData{
		GymID: "gym1", Level: 3, TeamID: 1, Ex: false,
		Latitude: 51.0, Longitude: 0.0,
	}

	matched, _ := matcher.MatchEgg(eggData, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for level=90 (any), got %d", len(matched))
	}
}

func TestRaidBlockedAlerts(t *testing.T) {
	human := makeHuman("user1")
	human.SetBlockedAlerts(`["raid"]`)
	raid := &db.RaidTracking{
		ID: "user1", ProfileNo: 1, PokemonID: 150, Level: 5,
		Team: 4, Exclusive: false, Form: 0, Evolution: 9000,
		Move: 9000, Distance: 0, Template: "1",
	}

	st := makeRaidTestState([]*db.RaidTracking{raid}, nil, map[string]*db.Human{"user1": human})
	matcher := &RaidMatcher{}

	raidData := &RaidData{
		GymID: "gym1", PokemonID: 150, Form: 0, Level: 5,
		TeamID: 1, Ex: false, Evolution: 0, Move1: 100, Move2: 200,
		Latitude: 51.0, Longitude: 0.0,
	}

	matched, _ := matcher.MatchRaid(raidData, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for blocked raid alerts, got %d", len(matched))
	}
}

func TestRaidMatchExclusive(t *testing.T) {
	human := makeHuman("user1")
	raid := &db.RaidTracking{
		ID: "user1", ProfileNo: 1, PokemonID: 150, Level: 5,
		Team: 4, Exclusive: true, Form: 0, Evolution: 9000,
		Move: 9000, Distance: 0, Template: "1",
	}

	st := makeRaidTestState([]*db.RaidTracking{raid}, nil, map[string]*db.Human{"user1": human})
	matcher := &RaidMatcher{}

	// EX raid — should match
	raidData := &RaidData{
		GymID: "gym1", PokemonID: 150, Form: 0, Level: 5,
		TeamID: 1, Ex: true, Evolution: 0, Move1: 100, Move2: 200,
		Latitude: 51.0, Longitude: 0.0,
	}
	matched, _ := matcher.MatchRaid(raidData, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for EX raid, got %d", len(matched))
	}

	// Non-EX raid — should not match
	raidData.Ex = false
	matched, _ = matcher.MatchRaid(raidData, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for non-EX raid with exclusive tracking, got %d", len(matched))
	}
}

func TestRaidMatchFormFilter(t *testing.T) {
	human := makeHuman("user1")
	raid := &db.RaidTracking{
		ID: "user1", ProfileNo: 1, PokemonID: 150, Level: 5,
		Team: 4, Exclusive: false, Form: 598, Evolution: 9000,
		Move: 9000, Distance: 0, Template: "1",
	}

	st := makeRaidTestState([]*db.RaidTracking{raid}, nil, map[string]*db.Human{"user1": human})
	matcher := &RaidMatcher{}

	// Correct form
	raidData := &RaidData{
		GymID: "gym1", PokemonID: 150, Form: 598, Level: 5,
		TeamID: 1, Ex: false, Evolution: 0, Move1: 100, Move2: 200,
		Latitude: 51.0, Longitude: 0.0,
	}
	matched, _ := matcher.MatchRaid(raidData, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for correct form, got %d", len(matched))
	}

	// Wrong form
	raidData.Form = 181
	matched, _ = matcher.MatchRaid(raidData, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for wrong form, got %d", len(matched))
	}
}

func TestRaidMatchEvolution(t *testing.T) {
	human := makeHuman("user1")
	raid := &db.RaidTracking{
		ID: "user1", ProfileNo: 1, PokemonID: 150, Level: 5,
		Team: 4, Exclusive: false, Form: 0, Evolution: 1,
		Move: 9000, Distance: 0, Template: "1",
	}

	st := makeRaidTestState([]*db.RaidTracking{raid}, nil, map[string]*db.Human{"user1": human})
	matcher := &RaidMatcher{}

	// Correct evolution
	raidData := &RaidData{
		GymID: "gym1", PokemonID: 150, Form: 0, Level: 5,
		TeamID: 1, Ex: false, Evolution: 1, Move1: 100, Move2: 200,
		Latitude: 51.0, Longitude: 0.0,
	}
	matched, _ := matcher.MatchRaid(raidData, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for correct evolution, got %d", len(matched))
	}

	// Wrong evolution
	raidData.Evolution = 2
	matched, _ = matcher.MatchRaid(raidData, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for wrong evolution, got %d", len(matched))
	}
}

func TestRaidMatchDistance(t *testing.T) {
	human := makeHuman("user1")
	human.Latitude = 51.0
	human.Longitude = 0.0
	raid := &db.RaidTracking{
		ID: "user1", ProfileNo: 1, PokemonID: 150, Level: 5,
		Team: 4, Exclusive: false, Form: 0, Evolution: 9000,
		Move: 9000, Distance: 1000, Template: "1",
	}

	st := makeRaidTestState([]*db.RaidTracking{raid}, nil, map[string]*db.Human{"user1": human})
	matcher := &RaidMatcher{}

	// Within distance
	raidData := &RaidData{
		GymID: "gym1", PokemonID: 150, Form: 0, Level: 5,
		TeamID: 1, Ex: false, Evolution: 0, Move1: 100, Move2: 200,
		Latitude: 51.005, Longitude: 0.005,
	}
	matched, _ := matcher.MatchRaid(raidData, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match within distance, got %d", len(matched))
	}

	// Outside distance
	raidData.Latitude = 51.5
	matched, _ = matcher.MatchRaid(raidData, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches outside distance, got %d", len(matched))
	}
}

func TestRaidMatchWrongLevel(t *testing.T) {
	human := makeHuman("user1")
	raid := &db.RaidTracking{
		ID: "user1", ProfileNo: 1, PokemonID: 9000, Level: 5,
		Team: 4, Exclusive: false, Form: 0, Evolution: 9000,
		Move: 9000, Distance: 0, Template: "1",
	}

	st := makeRaidTestState([]*db.RaidTracking{raid}, nil, map[string]*db.Human{"user1": human})
	matcher := &RaidMatcher{}

	raidData := &RaidData{
		GymID: "gym1", PokemonID: 999, Form: 0, Level: 3,
		TeamID: 1, Ex: false, Evolution: 0, Move1: 100, Move2: 200,
		Latitude: 51.0, Longitude: 0.0,
	}

	matched, _ := matcher.MatchRaid(raidData, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for wrong level, got %d", len(matched))
	}
}

func TestRaidSpecificGymBlockedAlerts(t *testing.T) {
	human := makeHuman("user1")
	human.SetBlockedAlerts(`["specificgym"]`)
	raid := &db.RaidTracking{
		ID: "user1", ProfileNo: 1, PokemonID: 150, Level: 5,
		Team: 4, Exclusive: false, Form: 0, Evolution: 9000,
		Move: 9000, Distance: 0, Template: "1",
		GymID: sql.NullString{String: "gym1", Valid: true},
	}

	st := makeRaidTestState([]*db.RaidTracking{raid}, nil, map[string]*db.Human{"user1": human})
	matcher := &RaidMatcher{}

	raidData := &RaidData{
		GymID: "gym1", PokemonID: 150, Form: 0, Level: 5,
		TeamID: 1, Ex: false, Evolution: 0, Move1: 100, Move2: 200,
		Latitude: 51.0, Longitude: 0.0,
	}

	matched, _ := matcher.MatchRaid(raidData, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for specificgym blocked, got %d", len(matched))
	}
}

func TestEggBlockedAlerts(t *testing.T) {
	human := makeHuman("user1")
	human.SetBlockedAlerts(`["egg"]`)
	egg := &db.EggTracking{
		ID: "user1", ProfileNo: 1, Level: 5,
		Team: 4, Exclusive: false,
		Distance: 0, Template: "1",
	}

	st := makeRaidTestState(nil, []*db.EggTracking{egg}, map[string]*db.Human{"user1": human})
	matcher := &RaidMatcher{}

	eggData := &EggData{
		GymID: "gym1", Level: 5, TeamID: 1, Ex: false,
		Latitude: 51.0, Longitude: 0.0,
	}

	matched, _ := matcher.MatchEgg(eggData, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for blocked egg alerts, got %d", len(matched))
	}
}

func TestEggMatchSpecificGym(t *testing.T) {
	human := makeHuman("user1")
	egg := &db.EggTracking{
		ID: "user1", ProfileNo: 1, Level: 5,
		Team: 4, Exclusive: false,
		Distance: 0, Template: "1",
		GymID: sql.NullString{String: "gym1", Valid: true},
	}

	st := makeRaidTestState(nil, []*db.EggTracking{egg}, map[string]*db.Human{"user1": human})
	matcher := &RaidMatcher{}

	// Correct gym
	eggData := &EggData{
		GymID: "gym1", Level: 5, TeamID: 1, Ex: false,
		Latitude: 51.0, Longitude: 0.0,
	}
	matched, _ := matcher.MatchEgg(eggData, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for specific gym, got %d", len(matched))
	}

	// Wrong gym
	eggData.GymID = "gym2"
	matched, _ = matcher.MatchEgg(eggData, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for wrong gym, got %d", len(matched))
	}
}

func TestEggMatchExclusive(t *testing.T) {
	human := makeHuman("user1")
	egg := &db.EggTracking{
		ID: "user1", ProfileNo: 1, Level: 5,
		Team: 4, Exclusive: true,
		Distance: 0, Template: "1",
	}

	st := makeRaidTestState(nil, []*db.EggTracking{egg}, map[string]*db.Human{"user1": human})
	matcher := &RaidMatcher{}

	// EX — match
	eggData := &EggData{
		GymID: "gym1", Level: 5, TeamID: 1, Ex: true,
		Latitude: 51.0, Longitude: 0.0,
	}
	matched, _ := matcher.MatchEgg(eggData, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for EX egg, got %d", len(matched))
	}

	// Non-EX — no match
	eggData.Ex = false
	matched, _ = matcher.MatchEgg(eggData, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for non-EX egg with exclusive tracking, got %d", len(matched))
	}
}

func TestEggMatchDistance(t *testing.T) {
	human := makeHuman("user1")
	human.Latitude = 51.0
	human.Longitude = 0.0
	egg := &db.EggTracking{
		ID: "user1", ProfileNo: 1, Level: 5,
		Team: 4, Exclusive: false,
		Distance: 1000, Template: "1",
	}

	st := makeRaidTestState(nil, []*db.EggTracking{egg}, map[string]*db.Human{"user1": human})
	matcher := &RaidMatcher{}

	// Within distance
	eggData := &EggData{
		GymID: "gym1", Level: 5, TeamID: 1, Ex: false,
		Latitude: 51.005, Longitude: 0.005,
	}
	matched, _ := matcher.MatchEgg(eggData, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match within distance, got %d", len(matched))
	}

	// Outside distance
	eggData.Latitude = 51.5
	matched, _ = matcher.MatchEgg(eggData, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches outside distance, got %d", len(matched))
	}
}

func TestRaidMatchDedup(t *testing.T) {
	human := makeHuman("user1")
	// Two raid trackings for same human
	raid1 := &db.RaidTracking{
		ID: "user1", ProfileNo: 1, PokemonID: 9000, Level: 5,
		Team: 4, Exclusive: false, Form: 0, Evolution: 9000,
		Move: 9000, Distance: 0, Template: "1",
	}
	raid2 := &db.RaidTracking{
		ID: "user1", ProfileNo: 1, PokemonID: 150, Level: 5,
		Team: 4, Exclusive: false, Form: 0, Evolution: 9000,
		Move: 9000, Distance: 0, Template: "1",
	}

	st := makeRaidTestState([]*db.RaidTracking{raid1, raid2}, nil, map[string]*db.Human{"user1": human})
	matcher := &RaidMatcher{}

	raidData := &RaidData{
		GymID: "gym1", PokemonID: 150, Form: 0, Level: 5,
		TeamID: 1, Ex: false, Evolution: 0, Move1: 100, Move2: 200,
		Latitude: 51.0, Longitude: 0.0,
	}

	matched, _ := matcher.MatchRaid(raidData, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match (deduped), got %d", len(matched))
	}
}
