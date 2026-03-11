package matching

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/state"
)

func makeInvasionTestState(invasions []*db.InvasionTracking, humans map[string]*db.Human) *state.State {
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
		Humans:    humans,
		Invasions: invasions,
		Geofence:  si,
		Fences:    fences,
	}
}

func TestInvasionMatchBasic(t *testing.T) {
	human := makeHuman("user1")
	inv := &db.InvasionTracking{
		ID: "user1", ProfileNo: 1, GruntType: "49",
		Distance: 0, Template: "1",
	}

	st := makeInvasionTestState([]*db.InvasionTracking{inv}, map[string]*db.Human{"user1": human})
	matcher := &InvasionMatcher{}

	data := &InvasionData{
		PokestopID: "stop1",
		GruntType:  "49",
		Latitude:   51.0, Longitude: 0.0,
	}

	matched := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match, got %d", len(matched))
	}
}

func TestInvasionMatchEverything(t *testing.T) {
	human := makeHuman("user1")
	inv := &db.InvasionTracking{
		ID: "user1", ProfileNo: 1, GruntType: "everything",
		Distance: 0, Template: "1",
	}

	st := makeInvasionTestState([]*db.InvasionTracking{inv}, map[string]*db.Human{"user1": human})
	matcher := &InvasionMatcher{}

	data := &InvasionData{
		PokestopID: "stop1",
		GruntType:  "29",
		Latitude:   51.0, Longitude: 0.0,
	}

	matched := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for 'everything' grunt type, got %d", len(matched))
	}
}

func TestInvasionMatchWrongGrunt(t *testing.T) {
	human := makeHuman("user1")
	inv := &db.InvasionTracking{
		ID: "user1", ProfileNo: 1, GruntType: "49",
		Distance: 0, Template: "1",
	}

	st := makeInvasionTestState([]*db.InvasionTracking{inv}, map[string]*db.Human{"user1": human})
	matcher := &InvasionMatcher{}

	data := &InvasionData{
		PokestopID: "stop1",
		GruntType:  "18",
		Latitude:   51.0, Longitude: 0.0,
	}

	matched := matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for wrong grunt, got %d", len(matched))
	}
}

func TestInvasionMatchGender(t *testing.T) {
	human := makeHuman("user1")
	inv := &db.InvasionTracking{
		ID: "user1", ProfileNo: 1, GruntType: "49",
		Gender: 1, Distance: 0, Template: "1",
	}

	st := makeInvasionTestState([]*db.InvasionTracking{inv}, map[string]*db.Human{"user1": human})
	matcher := &InvasionMatcher{}

	// Wrong gender
	data := &InvasionData{
		PokestopID: "stop1",
		GruntType:  "49",
		Gender:     2,
		Latitude:   51.0, Longitude: 0.0,
	}
	matched := matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for wrong gender, got %d", len(matched))
	}

	// Correct gender
	data.Gender = 1
	matched = matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for correct gender, got %d", len(matched))
	}
}

func TestInvasionMatchGenderAny(t *testing.T) {
	human := makeHuman("user1")
	inv := &db.InvasionTracking{
		ID: "user1", ProfileNo: 1, GruntType: "49",
		Gender: 0, Distance: 0, Template: "1", // 0 = any
	}

	st := makeInvasionTestState([]*db.InvasionTracking{inv}, map[string]*db.Human{"user1": human})
	matcher := &InvasionMatcher{}

	data := &InvasionData{
		PokestopID: "stop1",
		GruntType:  "49",
		Gender:     2,
		Latitude:   51.0, Longitude: 0.0,
	}

	matched := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for gender=0 (any), got %d", len(matched))
	}
}

func TestInvasionBlockedAlerts(t *testing.T) {
	human := makeHuman("user1")
	human.BlockedAlerts = "invasion"
	inv := &db.InvasionTracking{
		ID: "user1", ProfileNo: 1, GruntType: "everything",
		Distance: 0, Template: "1",
	}

	st := makeInvasionTestState([]*db.InvasionTracking{inv}, map[string]*db.Human{"user1": human})
	matcher := &InvasionMatcher{}

	data := &InvasionData{
		PokestopID: "stop1",
		GruntType:  "49",
		Latitude:   51.0, Longitude: 0.0,
	}

	matched := matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for blocked alerts, got %d", len(matched))
	}
}

func TestInvasionOutsideArea(t *testing.T) {
	human := makeHuman("user1")
	inv := &db.InvasionTracking{
		ID: "user1", ProfileNo: 1, GruntType: "everything",
		Distance: 0, Template: "1",
	}

	st := makeInvasionTestState([]*db.InvasionTracking{inv}, map[string]*db.Human{"user1": human})
	matcher := &InvasionMatcher{}

	data := &InvasionData{
		PokestopID: "stop1",
		GruntType:  "49",
		Latitude:   40.0, Longitude: 0.0, // outside TestArea fence
	}

	matched := matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches outside area, got %d", len(matched))
	}
}

// Based on real invasion webhook: grunt_type=49, latitude=51.120882, longitude=0.863187
func TestInvasionRealWorldData(t *testing.T) {
	human := makeHuman("user1")
	inv := &db.InvasionTracking{
		ID: "user1", ProfileNo: 1, GruntType: "49",
		Distance: 0, Template: "1",
	}

	st := makeInvasionTestState([]*db.InvasionTracking{inv}, map[string]*db.Human{"user1": human})
	matcher := &InvasionMatcher{}

	data := &InvasionData{
		PokestopID: "949b3ba5eacb4643bc4e76f818fd78df.16",
		GruntType:  "49",
		Latitude:   51.120882,
		Longitude:  0.863187,
	}

	matched := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for real world invasion, got %d", len(matched))
	}
}

func TestResolveGruntType(t *testing.T) {
	tests := []struct {
		name          string
		incidentGrunt int
		gruntType     int
		displayType   int
		expected      string
	}{
		{"display type >= 7", 0, 0, 7, "e7"},
		{"display type >= 7 overrides grunt", 49, 0, 8, "e8"},
		{"incident grunt type used", 49, 0, 1, "49"},
		{"incident grunt 352 ignored", 352, 29, 1, "29"},
		{"fallback to gruntType", 0, 29, 1, "29"},
		{"all zero", 0, 0, 0, "0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ResolveGruntType(tt.incidentGrunt, tt.gruntType, tt.displayType)
			if got != tt.expected {
				t.Errorf("ResolveGruntType(%d, %d, %d) = %q, want %q",
					tt.incidentGrunt, tt.gruntType, tt.displayType, got, tt.expected)
			}
		})
	}
}
