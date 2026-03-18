package matching

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/state"
)

func makeLureTestState(lures []*db.LureTracking, humans map[string]*db.Human) *state.State {
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
		Lures:    lures,
		Geofence: si,
		Fences:   fences,
	}
}

func TestLureMatchBasic(t *testing.T) {
	human := makeHuman("user1")
	lure := &db.LureTracking{
		ID: "user1", ProfileNo: 1, LureID: 501,
		Distance: 0, Template: "1",
	}

	st := makeLureTestState([]*db.LureTracking{lure}, map[string]*db.Human{"user1": human})
	matcher := &LureMatcher{}

	data := &LureData{
		PokestopID: "stop1",
		LureID:     501,
		Latitude:   51.0, Longitude: 0.0,
	}

	matched := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match, got %d", len(matched))
	}
}

func TestLureMatchAnyLure(t *testing.T) {
	human := makeHuman("user1")
	lure := &db.LureTracking{
		ID: "user1", ProfileNo: 1, LureID: 0, // any lure
		Distance: 0, Template: "1",
	}

	st := makeLureTestState([]*db.LureTracking{lure}, map[string]*db.Human{"user1": human})
	matcher := &LureMatcher{}

	data := &LureData{
		PokestopID: "stop1",
		LureID:     501,
		Latitude:   51.0, Longitude: 0.0,
	}

	matched := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for lure_id=0 (any), got %d", len(matched))
	}
}

func TestLureMatchWrongLure(t *testing.T) {
	human := makeHuman("user1")
	lure := &db.LureTracking{
		ID: "user1", ProfileNo: 1, LureID: 501,
		Distance: 0, Template: "1",
	}

	st := makeLureTestState([]*db.LureTracking{lure}, map[string]*db.Human{"user1": human})
	matcher := &LureMatcher{}

	data := &LureData{
		PokestopID: "stop1",
		LureID:     502, // different lure type
		Latitude:   51.0, Longitude: 0.0,
	}

	matched := matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for wrong lure, got %d", len(matched))
	}
}

func TestLureMatchDistance(t *testing.T) {
	human := makeHuman("user1")
	human.Latitude = 51.5
	human.Longitude = 0.0

	lure := &db.LureTracking{
		ID: "user1", ProfileNo: 1, LureID: 501,
		Distance: 1000, Template: "1", // 1km
	}

	st := makeLureTestState([]*db.LureTracking{lure}, map[string]*db.Human{"user1": human})
	matcher := &LureMatcher{}

	// Close enough
	data := &LureData{
		PokestopID: "stop1",
		LureID:     501,
		Latitude:   51.5005, Longitude: 0.0,
	}
	matched := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match within distance, got %d", len(matched))
	}

	// Too far
	data.Latitude = 51.6
	matched = matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for distant lure, got %d", len(matched))
	}
}

func TestLureBlockedAlerts(t *testing.T) {
	human := makeHuman("user1")
	human.BlockedAlerts = `["lure"]`
	lure := &db.LureTracking{
		ID: "user1", ProfileNo: 1, LureID: 0,
		Distance: 0, Template: "1",
	}

	st := makeLureTestState([]*db.LureTracking{lure}, map[string]*db.Human{"user1": human})
	matcher := &LureMatcher{}

	data := &LureData{
		PokestopID: "stop1", LureID: 501,
		Latitude: 51.0, Longitude: 0.0,
	}

	matched := matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for blocked alerts, got %d", len(matched))
	}
}

// Based on real pokestop/lure webhook: lure_id=501, latitude=51.293981, longitude=1.063606
// Uses wider fence to encompass real-world coordinates from Kent, UK
func TestLureRealWorldData(t *testing.T) {
	human := makeHuman("user1")
	human.Area = []string{"kent"}
	lure := &db.LureTracking{
		ID: "user1", ProfileNo: 1, LureID: 501,
		Distance: 0, Template: "1",
	}

	fences := []geofence.Fence{
		{
			Name:             "Kent",
			DisplayInMatches: true,
			Path: [][2]float64{
				{50.0, -1.0},
				{52.0, -1.0},
				{52.0, 2.0},
				{50.0, 2.0},
			},
		},
	}
	si := geofence.NewSpatialIndex(fences)

	st := &state.State{
		Humans:   map[string]*db.Human{"user1": human},
		Lures:    []*db.LureTracking{lure},
		Geofence: si,
		Fences:   fences,
	}

	matcher := &LureMatcher{}

	data := &LureData{
		PokestopID: "bcb01ff037763713b5f3a07459913e70.16",
		LureID:     501,
		Latitude:   51.293981,
		Longitude:  1.063606,
	}

	matched := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for real world lure, got %d", len(matched))
	}
}
