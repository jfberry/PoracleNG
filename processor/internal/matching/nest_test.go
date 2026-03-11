package matching

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/state"
)

func makeNestTestState(nests []*db.NestTracking, humans map[string]*db.Human) *state.State {
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
		Nests:    nests,
		Geofence: si,
		Fences:   fences,
	}
}

func TestNestMatchBasic(t *testing.T) {
	human := makeHuman("user1")
	nest := &db.NestTracking{
		ID: "user1", ProfileNo: 1, PokemonID: 25,
		Distance: 0, Template: "1",
	}

	st := makeNestTestState([]*db.NestTracking{nest}, map[string]*db.Human{"user1": human})
	matcher := &NestMatcher{}

	data := &NestData{
		NestID:     1234,
		PokemonID:  25,
		Form:       0,
		PokemonAvg: 5.0,
		Latitude:   51.0, Longitude: 0.0,
	}

	matched := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match, got %d", len(matched))
	}
}

func TestNestMatchAnyPokemon(t *testing.T) {
	human := makeHuman("user1")
	nest := &db.NestTracking{
		ID: "user1", ProfileNo: 1, PokemonID: 0, // any
		Distance: 0, Template: "1",
	}

	st := makeNestTestState([]*db.NestTracking{nest}, map[string]*db.Human{"user1": human})
	matcher := &NestMatcher{}

	data := &NestData{
		NestID: 1234, PokemonID: 999, Form: 0, PokemonAvg: 5.0,
		Latitude: 51.0, Longitude: 0.0,
	}

	matched := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for pokemon_id=0 (any), got %d", len(matched))
	}
}

func TestNestMatchWrongPokemon(t *testing.T) {
	human := makeHuman("user1")
	nest := &db.NestTracking{
		ID: "user1", ProfileNo: 1, PokemonID: 25,
		Distance: 0, Template: "1",
	}

	st := makeNestTestState([]*db.NestTracking{nest}, map[string]*db.Human{"user1": human})
	matcher := &NestMatcher{}

	data := &NestData{
		NestID: 1234, PokemonID: 26, Form: 0, PokemonAvg: 5.0,
		Latitude: 51.0, Longitude: 0.0,
	}

	matched := matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for wrong pokemon, got %d", len(matched))
	}
}

func TestNestMatchFormFilter(t *testing.T) {
	human := makeHuman("user1")
	nest := &db.NestTracking{
		ID: "user1", ProfileNo: 1, PokemonID: 25, Form: 5,
		Distance: 0, Template: "1",
	}

	st := makeNestTestState([]*db.NestTracking{nest}, map[string]*db.Human{"user1": human})
	matcher := &NestMatcher{}

	// Wrong form
	data := &NestData{
		NestID: 1234, PokemonID: 25, Form: 0, PokemonAvg: 5.0,
		Latitude: 51.0, Longitude: 0.0,
	}
	matched := matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for wrong form, got %d", len(matched))
	}

	// Correct form
	data.Form = 5
	matched = matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for correct form, got %d", len(matched))
	}
}

func TestNestMatchFormAny(t *testing.T) {
	human := makeHuman("user1")
	nest := &db.NestTracking{
		ID: "user1", ProfileNo: 1, PokemonID: 25, Form: 0, // any form
		Distance: 0, Template: "1",
	}

	st := makeNestTestState([]*db.NestTracking{nest}, map[string]*db.Human{"user1": human})
	matcher := &NestMatcher{}

	data := &NestData{
		NestID: 1234, PokemonID: 25, Form: 5, PokemonAvg: 5.0,
		Latitude: 51.0, Longitude: 0.0,
	}

	matched := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for form=0 (any), got %d", len(matched))
	}
}

func TestNestMatchMinSpawnAvg(t *testing.T) {
	human := makeHuman("user1")
	nest := &db.NestTracking{
		ID: "user1", ProfileNo: 1, PokemonID: 25,
		MinSpawnAvg: 10,
		Distance:    0, Template: "1",
	}

	st := makeNestTestState([]*db.NestTracking{nest}, map[string]*db.Human{"user1": human})
	matcher := &NestMatcher{}

	// Below minimum
	data := &NestData{
		NestID: 1234, PokemonID: 25, PokemonAvg: 5.0,
		Latitude: 51.0, Longitude: 0.0,
	}
	matched := matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for low spawn avg, got %d", len(matched))
	}

	// Above minimum
	data.PokemonAvg = 15.0
	matched = matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for high spawn avg, got %d", len(matched))
	}
}

func TestNestBlockedAlerts(t *testing.T) {
	human := makeHuman("user1")
	human.BlockedAlerts = "nest"
	nest := &db.NestTracking{
		ID: "user1", ProfileNo: 1, PokemonID: 0,
		Distance: 0, Template: "1",
	}

	st := makeNestTestState([]*db.NestTracking{nest}, map[string]*db.Human{"user1": human})
	matcher := &NestMatcher{}

	data := &NestData{
		NestID: 1234, PokemonID: 25, PokemonAvg: 5.0,
		Latitude: 51.0, Longitude: 0.0,
	}

	matched := matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for blocked alerts, got %d", len(matched))
	}
}
