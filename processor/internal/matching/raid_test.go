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
		Move: 9000, Distance: 0, Template: "1", Clean: false,
	}

	st := makeRaidTestState([]*db.RaidTracking{raid}, nil, map[string]*db.Human{"user1": human})
	matcher := &RaidMatcher{}

	raidData := &RaidData{
		GymID: "gym1", PokemonID: 150, Form: 0, Level: 5,
		TeamID: 1, Ex: false, Evolution: 0, Move1: 100, Move2: 200,
		Latitude: 51.0, Longitude: 0.0,
	}

	matched := matcher.MatchRaid(raidData, st)
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

	matched := matcher.MatchRaid(raidData, st)
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

	matched := matcher.MatchRaid(raidData, st)
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
	matched := matcher.MatchRaid(raidData, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for matching move_1, got %d", len(matched))
	}

	// Move matches move_2
	raidData.Move1 = 300
	raidData.Move2 = 100
	matched = matcher.MatchRaid(raidData, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for matching move_2, got %d", len(matched))
	}

	// No match
	raidData.Move1 = 300
	raidData.Move2 = 400
	matched = matcher.MatchRaid(raidData, st)
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
	matched := matcher.MatchRaid(raidData, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for specific gym, got %d", len(matched))
	}

	// Wrong gym
	raidData.GymID = "gym2"
	matched = matcher.MatchRaid(raidData, st)
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

	matched := matcher.MatchEgg(eggData, st)
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

	matched := matcher.MatchEgg(eggData, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for level=90 (any), got %d", len(matched))
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

	matched := matcher.MatchRaid(raidData, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match (deduped), got %d", len(matched))
	}
}
