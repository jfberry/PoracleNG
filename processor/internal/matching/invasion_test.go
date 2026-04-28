package matching

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
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

	matched, _ := matcher.Match(data, st)
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

	matched, _ := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for 'everything' grunt type, got %d", len(matched))
	}
}

func TestInvasionMatchBossKeyword(t *testing.T) {
	human := makeHuman("user1")
	inv := &db.InvasionTracking{
		ID: "user1", ProfileNo: 1, GruntType: "boss",
		Distance: 0, Template: "1",
	}
	st := makeInvasionTestState([]*db.InvasionTracking{inv}, map[string]*db.Human{"user1": human})
	matcher := &InvasionMatcher{}

	// Boss webhook (any boss; the GruntType string is what ResolveGruntTypeName
	// produces — for an event Giovanni it's "giovanni") matches the "boss" rule.
	bossData := &InvasionData{
		PokestopID: "stop1",
		GruntType:  "giovanni",
		Boss:       true,
		Latitude:   51.0, Longitude: 0.0,
	}
	if matched, _ := matcher.Match(bossData, st); len(matched) != 1 {
		t.Errorf("boss rule should match a boss invasion (got %d matches)", len(matched))
	}

	// A non-boss invasion (e.g. Electric grunt) must NOT match the boss rule.
	gruntData := &InvasionData{
		PokestopID: "stop1",
		GruntType:  "electric",
		Boss:       false,
		Latitude:   51.0, Longitude: 0.0,
	}
	if matched, _ := matcher.Match(gruntData, st); len(matched) != 0 {
		t.Errorf("boss rule must not match a non-boss grunt (got %d matches)", len(matched))
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

	matched, _ := matcher.Match(data, st)
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
	matched, _ := matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for wrong gender, got %d", len(matched))
	}

	// Correct gender
	data.Gender = 1
	matched, _ = matcher.Match(data, st)
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

	matched, _ := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for gender=0 (any), got %d", len(matched))
	}
}

func TestInvasionBlockedAlerts(t *testing.T) {
	human := makeHuman("user1")
	human.SetBlockedAlerts(`["invasion"]`)
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

	matched, _ := matcher.Match(data, st)
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

	matched, _ := matcher.Match(data, st)
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

	matched, _ := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for real world invasion, got %d", len(matched))
	}
}

func TestResolveGruntTypeName(t *testing.T) {
	gd := &gamedata.GameData{
		Grunts: map[int]*gamedata.Grunt{
			4:  {TypeID: 0, Template: "CHARACTER_GRUNT_MALE"},          // Mixed
			5:  {TypeID: 0, Template: "CHARACTER_GRUNT_FEMALE"},        // Mixed
			8:  {TypeID: 0, Template: "CHARACTER_DARKNESS_GRUNT_FEMALE"}, // Darkness
			23: {TypeID: 12, Template: "CHARACTER_GRASS_GRUNT_MALE"},   // Grass
			28: {TypeID: 0, Template: "CHARACTER_METAL_GRUNT_FEMALE"},  // Metal
			49: {TypeID: 13, Template: "CHARACTER_ELECTRIC_GRUNT_FEMALE"}, // Electric
			50: {TypeID: 13, Template: "CHARACTER_ELECTRIC_GRUNT_MALE"},   // Electric
		},
		Types: map[int]*gamedata.TypeInfo{
			13: {Name: "Electric"},
			12: {Name: "Grass"},
		},
		Util: &gamedata.UtilData{
			PokestopEvent: map[int]gamedata.EventInfo{
				7: {Name: "Gold-Stop"},
				8: {Name: "Kecleon"},
				9: {Name: "Showcase"},
			},
		},
	}

	tests := []struct {
		name        string
		gruntTypeID int
		displayType int
		expected    string
	}{
		{"kecleon event", 0, 8, "kecleon"},
		{"showcase event", 0, 9, "showcase"},
		{"gold-stop event", 0, 7, "gold-stop"},
		{"event overrides grunt", 49, 8, "kecleon"},
		{"electric grunt", 49, 1, "electric"},
		{"grass grunt", 23, 1, "grass"},
		{"metal grunt (TypeID=0) → steel", 28, 1, "steel"},
		{"darkness grunt (TypeID=0)", 8, 1, "darkness"},
		{"mixed grunt (TypeID=0)", 4, 1, "mixed"},
		{"unknown grunt falls back to ID", 999, 1, "999"},
		{"zero grunt", 0, 0, "0"},
		{"nil gamedata falls back to ID", 50, 1, "50"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			testGD := gd
			if tt.name == "nil gamedata falls back to ID" {
				testGD = nil
			}
			got := ResolveGruntTypeName(tt.gruntTypeID, tt.displayType, testGD)
			if got != tt.expected {
				t.Errorf("ResolveGruntTypeName(%d, %d) = %q, want %q",
					tt.gruntTypeID, tt.displayType, got, tt.expected)
			}
		})
	}
}
