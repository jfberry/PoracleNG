package matching

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/state"
)

func makeFortTestState(forts []*db.FortTracking, humans map[string]*db.Human) *state.State {
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
		Forts:    forts,
		Geofence: si,
		Fences:   fences,
	}
}

func TestFortMatchNewPokestop(t *testing.T) {
	human := makeHuman("user1")
	fort := &db.FortTracking{
		ID: "user1", ProfileNo: 1, FortType: "pokestop",
		ChangeTypes: `["new"]`,
		Distance:    0, Template: "1",
	}

	st := makeFortTestState([]*db.FortTracking{fort}, map[string]*db.Human{"user1": human})
	matcher := &FortMatcher{}

	data := &FortData{
		ID:          "fort1",
		FortType:    "pokestop",
		ChangeTypes: []string{"new"},
		Latitude:    51.0, Longitude: 0.0,
	}

	matched, _ := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for new pokestop, got %d", len(matched))
	}
}

func TestFortMatchEverything(t *testing.T) {
	human := makeHuman("user1")
	fort := &db.FortTracking{
		ID: "user1", ProfileNo: 1, FortType: "everything",
		ChangeTypes: "[]",
		Distance:    0, Template: "1",
	}

	st := makeFortTestState([]*db.FortTracking{fort}, map[string]*db.Human{"user1": human})
	matcher := &FortMatcher{}

	data := &FortData{
		ID:          "fort1",
		FortType:    "gym",
		ChangeTypes: []string{"new"},
		Latitude:    51.0, Longitude: 0.0,
	}

	matched, _ := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for fort_type=everything, got %d", len(matched))
	}
}

func TestFortMatchWrongType(t *testing.T) {
	human := makeHuman("user1")
	fort := &db.FortTracking{
		ID: "user1", ProfileNo: 1, FortType: "pokestop",
		ChangeTypes: "[]",
		Distance:    0, Template: "1",
	}

	st := makeFortTestState([]*db.FortTracking{fort}, map[string]*db.Human{"user1": human})
	matcher := &FortMatcher{}

	data := &FortData{
		ID:          "fort1",
		FortType:    "gym", // wrong type
		ChangeTypes: []string{"new"},
		Latitude:    51.0, Longitude: 0.0,
	}

	matched, _ := matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for wrong fort type, got %d", len(matched))
	}
}

func TestFortMatchChangeTypes(t *testing.T) {
	human := makeHuman("user1")
	fort := &db.FortTracking{
		ID: "user1", ProfileNo: 1, FortType: "pokestop",
		ChangeTypes: `["name","location"]`,
		Distance:    0, Template: "1",
	}

	st := makeFortTestState([]*db.FortTracking{fort}, map[string]*db.Human{"user1": human})
	matcher := &FortMatcher{}

	// Matching change type
	data := &FortData{
		ID:          "fort1",
		FortType:    "pokestop",
		ChangeTypes: []string{"edit", "name"},
		Latitude:    51.0, Longitude: 0.0,
	}
	matched, _ := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for matching change type, got %d", len(matched))
	}

	// Non-matching change type
	data.ChangeTypes = []string{"edit", "description"}
	matched, _ = matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for non-matching change type, got %d", len(matched))
	}
}

func TestFortMatchEmptyChangeTypesFilter(t *testing.T) {
	human := makeHuman("user1")
	fort := &db.FortTracking{
		ID: "user1", ProfileNo: 1, FortType: "pokestop",
		ChangeTypes: "[]", // any change type
		Distance:    0, Template: "1",
	}

	st := makeFortTestState([]*db.FortTracking{fort}, map[string]*db.Human{"user1": human})
	matcher := &FortMatcher{}

	data := &FortData{
		ID:          "fort1",
		FortType:    "pokestop",
		ChangeTypes: []string{"removal"},
		Latitude:    51.0, Longitude: 0.0,
	}

	matched, _ := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for empty change types filter, got %d", len(matched))
	}
}

func TestFortMatchEmpty(t *testing.T) {
	human := makeHuman("user1")
	fort := &db.FortTracking{
		ID: "user1", ProfileNo: 1, FortType: "pokestop",
		ChangeTypes:  "[]",
		IncludeEmpty: false, // exclude empty forts
		Distance:     0, Template: "1",
	}

	st := makeFortTestState([]*db.FortTracking{fort}, map[string]*db.Human{"user1": human})
	matcher := &FortMatcher{}

	data := &FortData{
		ID:          "fort1",
		FortType:    "pokestop",
		IsEmpty:     true,
		ChangeTypes: []string{"new"},
		Latitude:    51.0, Longitude: 0.0,
	}

	matched, _ := matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for empty fort when include_empty=false, got %d", len(matched))
	}

	// With include_empty=true
	fort.IncludeEmpty = true
	st = makeFortTestState([]*db.FortTracking{fort}, map[string]*db.Human{"user1": human})
	matched, _ = matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for empty fort when include_empty=true, got %d", len(matched))
	}
}

func TestFortBlockedAlerts(t *testing.T) {
	human := makeHuman("user1")
	human.SetBlockedAlerts(`["forts"]`)
	fort := &db.FortTracking{
		ID: "user1", ProfileNo: 1, FortType: "everything",
		ChangeTypes: "[]",
		Distance:    0, Template: "1",
	}

	st := makeFortTestState([]*db.FortTracking{fort}, map[string]*db.Human{"user1": human})
	matcher := &FortMatcher{}

	data := &FortData{
		ID:          "fort1",
		FortType:    "pokestop",
		ChangeTypes: []string{"new"},
		Latitude:    51.0, Longitude: 0.0,
	}

	matched, _ := matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for blocked alerts, got %d", len(matched))
	}
}

// Based on real fort_update webhook: change_type=new, type=pokestop, lat=51.160499, lon=0.900055
func TestFortRealWorldData(t *testing.T) {
	human := makeHuman("user1")
	fort := &db.FortTracking{
		ID: "user1", ProfileNo: 1, FortType: "pokestop",
		ChangeTypes:  `["new"]`,
		IncludeEmpty: true,
		Distance:     0, Template: "1",
	}

	st := makeFortTestState([]*db.FortTracking{fort}, map[string]*db.Human{"user1": human})
	matcher := &FortMatcher{}

	data := &FortData{
		ID:          "b120279e3c053ff0ac2dac7973f7fb43.16",
		FortType:    "pokestop",
		IsEmpty:     true, // name=null, description=null
		ChangeTypes: []string{"new"},
		Latitude:    51.160499,
		Longitude:   0.900055,
	}

	matched, _ := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for real world fort update, got %d", len(matched))
	}
}

func TestChangeTypesMatch(t *testing.T) {
	tests := []struct {
		name          string
		trackedJSON   string
		actualChanges []string
		expected      bool
	}{
		{"single match", `["name"]`, []string{"name"}, true},
		{"partial match", `["name","location"]`, []string{"edit", "name"}, true},
		{"no match", `["name","location"]`, []string{"edit", "description"}, false},
		{"case insensitive", `["Name"]`, []string{"name"}, true},
		{"empty tracked", `[]`, []string{"name"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := changeTypesMatch(tt.trackedJSON, tt.actualChanges)
			if got != tt.expected {
				t.Errorf("changeTypesMatch(%q, %v) = %v, want %v",
					tt.trackedJSON, tt.actualChanges, got, tt.expected)
			}
		})
	}
}
