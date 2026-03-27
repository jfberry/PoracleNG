package matching

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/state"
)

func makeMaxbattle(id string, pokemonID, level int) *db.MaxbattleTracking {
	return &db.MaxbattleTracking{
		ID:        id,
		ProfileNo: 1,
		PokemonID: pokemonID,
		Level:     level,
		Form:      0,
		Move:      9000, // any move
		Evolution: 9000, // any evolution
		Gmax:      0,    // any
		Distance:  0,
		Template:  "1",
		Clean:     false,
	}
}

func makeMaxbattleState(maxbattles []*db.MaxbattleTracking, humans map[string]*db.Human) *state.State {
	fences := []geofence.Fence{
		{
			Name:             "TestArea",
			DisplayInMatches: true,
			Path: [][2]float64{
				{50.0, -1.0}, {52.0, -1.0}, {52.0, 1.0}, {50.0, 1.0},
			},
		},
	}
	return &state.State{
		Humans:     humans,
		Maxbattles: maxbattles,
		Geofence:   geofence.NewSpatialIndex(fences),
		Fences:     fences,
	}
}

func TestMaxbattleMatchPokemon(t *testing.T) {
	human := makeHuman("user1")
	mb := makeMaxbattle("user1", 25, 3) // track specific pokemon

	st := makeMaxbattleState([]*db.MaxbattleTracking{mb}, map[string]*db.Human{"user1": human})
	matcher := &MaxbattleMatcher{}

	data := &MaxbattleData{
		StationID: "station1", PokemonID: 25, Level: 3,
		Latitude: 51.0, Longitude: 0.0,
	}

	matched := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for correct pokemon, got %d", len(matched))
	}

	// Wrong pokemon
	data.PokemonID = 26
	matched = matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for wrong pokemon, got %d", len(matched))
	}
}

func TestMaxbattleMatchLevelBased(t *testing.T) {
	human := makeHuman("user1")
	mb := makeMaxbattle("user1", 9000, 5) // level-based: pokemon_id=9000, specific level

	st := makeMaxbattleState([]*db.MaxbattleTracking{mb}, map[string]*db.Human{"user1": human})
	matcher := &MaxbattleMatcher{}

	// Correct level
	data := &MaxbattleData{
		StationID: "station1", PokemonID: 150, Level: 5,
		Latitude: 51.0, Longitude: 0.0,
	}
	matched := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for correct level, got %d", len(matched))
	}

	// Wrong level
	data.Level = 3
	matched = matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for wrong level, got %d", len(matched))
	}
}

func TestMaxbattleMatchAllLevels(t *testing.T) {
	human := makeHuman("user1")
	mb := makeMaxbattle("user1", 9000, 90) // level 90 = all levels

	st := makeMaxbattleState([]*db.MaxbattleTracking{mb}, map[string]*db.Human{"user1": human})
	matcher := &MaxbattleMatcher{}

	for _, lvl := range []int{1, 3, 5, 6, 7} {
		data := &MaxbattleData{
			StationID: "station1", PokemonID: 150, Level: lvl,
			Latitude: 51.0, Longitude: 0.0,
		}
		matched := matcher.Match(data, st)
		if len(matched) != 1 {
			t.Errorf("Expected 1 match for level %d with level=90, got %d", lvl, len(matched))
		}
	}
}

func TestMaxbattleMatchGmax(t *testing.T) {
	human := makeHuman("user1")
	mb := makeMaxbattle("user1", 9000, 90)
	mb.Gmax = 1 // require gmax

	st := makeMaxbattleState([]*db.MaxbattleTracking{mb}, map[string]*db.Human{"user1": human})
	matcher := &MaxbattleMatcher{}

	// Non-gmax should not match
	data := &MaxbattleData{
		StationID: "station1", PokemonID: 150, Level: 7, Gmax: 0,
		Latitude: 51.0, Longitude: 0.0,
	}
	matched := matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for non-gmax when tracking gmax, got %d", len(matched))
	}

	// Gmax should match
	data.Gmax = 1
	matched = matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for gmax, got %d", len(matched))
	}
}

func TestMaxbattleMatchForm(t *testing.T) {
	human := makeHuman("user1")
	mb := makeMaxbattle("user1", 25, 3)
	mb.Form = 5 // specific form

	st := makeMaxbattleState([]*db.MaxbattleTracking{mb}, map[string]*db.Human{"user1": human})
	matcher := &MaxbattleMatcher{}

	data := &MaxbattleData{
		StationID: "station1", PokemonID: 25, Level: 3, Form: 5,
		Latitude: 51.0, Longitude: 0.0,
	}
	matched := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for correct form, got %d", len(matched))
	}

	data.Form = 6 // wrong form
	matched = matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for wrong form, got %d", len(matched))
	}
}

func TestMaxbattleMatchMove(t *testing.T) {
	human := makeHuman("user1")
	mb := makeMaxbattle("user1", 25, 3)
	mb.Move = 14 // specific move

	st := makeMaxbattleState([]*db.MaxbattleTracking{mb}, map[string]*db.Human{"user1": human})
	matcher := &MaxbattleMatcher{}

	// Match on move1
	data := &MaxbattleData{
		StationID: "station1", PokemonID: 25, Level: 3,
		Move1: 14, Move2: 99,
		Latitude: 51.0, Longitude: 0.0,
	}
	matched := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for move1, got %d", len(matched))
	}

	// Match on move2
	data.Move1 = 99
	data.Move2 = 14
	matched = matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for move2, got %d", len(matched))
	}

	// Neither move matches
	data.Move1 = 88
	data.Move2 = 99
	matched = matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for wrong moves, got %d", len(matched))
	}
}

func TestMaxbattleMatchSpecificStation(t *testing.T) {
	human := makeHuman("user1")
	mb := makeMaxbattle("user1", 9000, 90)
	stationID := "station-abc"
	mb.StationID = &stationID

	st := makeMaxbattleState([]*db.MaxbattleTracking{mb}, map[string]*db.Human{"user1": human})
	matcher := &MaxbattleMatcher{}

	// Correct station
	data := &MaxbattleData{
		StationID: "station-abc", PokemonID: 150, Level: 5,
		Latitude: 51.0, Longitude: 0.0,
	}
	matched := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for correct station, got %d", len(matched))
	}

	// Wrong station
	data.StationID = "station-xyz"
	matched = matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for wrong station, got %d", len(matched))
	}
}

func TestMaxbattleBlockedAlerts(t *testing.T) {
	human := makeHuman("user1")
	human.SetBlockedAlerts(`["maxbattle"]`)
	mb := makeMaxbattle("user1", 25, 3)

	st := makeMaxbattleState([]*db.MaxbattleTracking{mb}, map[string]*db.Human{"user1": human})
	matcher := &MaxbattleMatcher{}

	data := &MaxbattleData{
		StationID: "station1", PokemonID: 25, Level: 3,
		Latitude: 51.0, Longitude: 0.0,
	}
	matched := matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for blocked maxbattle, got %d", len(matched))
	}
}

func TestMaxbattleBlockedSpecificStation(t *testing.T) {
	human := makeHuman("user1")
	human.SetBlockedAlerts(`["specificstation"]`)
	mb := makeMaxbattle("user1", 9000, 90)
	stationID := "station-abc"
	mb.StationID = &stationID

	st := makeMaxbattleState([]*db.MaxbattleTracking{mb}, map[string]*db.Human{"user1": human})
	matcher := &MaxbattleMatcher{}

	data := &MaxbattleData{
		StationID: "station-abc", PokemonID: 150, Level: 5,
		Latitude: 51.0, Longitude: 0.0,
	}
	matched := matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for blocked specificstation, got %d", len(matched))
	}
}

func TestMaxbattleEvolution(t *testing.T) {
	human := makeHuman("user1")
	mb := makeMaxbattle("user1", 25, 3)
	mb.Evolution = 2 // specific evolution

	st := makeMaxbattleState([]*db.MaxbattleTracking{mb}, map[string]*db.Human{"user1": human})
	matcher := &MaxbattleMatcher{}

	data := &MaxbattleData{
		StationID: "station1", PokemonID: 25, Level: 3, Evolution: 2,
		Latitude: 51.0, Longitude: 0.0,
	}
	matched := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for correct evolution, got %d", len(matched))
	}

	data.Evolution = 1
	matched = matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for wrong evolution, got %d", len(matched))
	}
}
