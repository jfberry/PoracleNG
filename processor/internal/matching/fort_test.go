package matching

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/webhook"
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

func TestFortMatch_RecordsMatchingDuration(t *testing.T) {
	metrics.MatchingDuration.Reset()
	matcher := &FortMatcher{}
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
	st := &state.State{
		Humans:   map[string]*db.Human{},
		Forts:    nil,
		Geofence: si,
	}

	data := &FortData{
		ID: "stop1", FortType: "pokestop", IsEmpty: false,
		ChangeTypes: []string{"name"}, Latitude: 51.0, Longitude: 0.0,
	}
	matcher.Match(data, st)

	h, err := metrics.MatchingDuration.GetMetricWithLabelValues("fort_update")
	if err != nil {
		t.Fatalf("get metric: %v", err)
	}
	var out dto.Metric
	if err := h.(prometheus.Histogram).Write(&out); err != nil {
		t.Fatalf("write metric: %v", err)
	}
	if got := out.GetHistogram().GetSampleCount(); got != 1 {
		t.Errorf("MatchingDuration{type=fort_update} sample count = %d, want 1", got)
	}
}

func TestFortMatch_RecordsCandidateCount(t *testing.T) {
	metrics.MatchingCandidates.Reset()
	human := makeHuman("u1")
	fort1 := &db.FortTracking{
		ID: "u1", ProfileNo: 1, FortType: "pokestop",
		IncludeEmpty: true, ChangeTypes: "[]", Distance: 0, Template: "1",
	}
	fort2 := &db.FortTracking{
		ID: "u2", ProfileNo: 1, FortType: "everything",
		IncludeEmpty: true, ChangeTypes: "[]", Distance: 0, Template: "1",
	}
	humans := map[string]*db.Human{
		"u1": human,
		"u2": makeHuman("u2"),
	}
	st := makeFortTestState([]*db.FortTracking{fort1, fort2}, humans)
	matcher := &FortMatcher{}

	data := &FortData{
		ID: "stop1", FortType: "pokestop", IsEmpty: false,
		ChangeTypes: []string{"name"}, Latitude: 51.0, Longitude: 0.0,
	}
	matcher.Match(data, st)

	h, _ := metrics.MatchingCandidates.GetMetricWithLabelValues("fort_update")
	var out dto.Metric
	_ = h.(prometheus.Histogram).Write(&out)
	if got := out.GetHistogram().GetSampleSum(); got != 2 {
		t.Errorf("MatchingCandidates sample sum = %v, want 2", got)
	}
}

// TestFortMatch_GeoPrefilterParity asserts flag-on and flag-off produce identical results.
func TestFortMatch_GeoPrefilterParity(t *testing.T) {
	humans := map[string]*db.Human{
		"u1": {ID: "u1", Enabled: true, Area: []string{"belgium"}, Latitude: 50.5, Longitude: 4.5, CurrentProfileNo: 1},
	}
	rules := []db.FortTracking{
		{ID: "u1", ProfileNo: 1, FortType: "pokestop", IncludeEmpty: false, ChangeTypes: "[]"},
	}
	rulesPointers := make([]*db.FortTracking, len(rules))
	for i := range rules {
		rulesPointers[i] = &rules[i]
	}
	perHuman := db.PartitionByHuman[db.FortTracking](rulesPointers)

	spatial := geofence.NewSpatialIndex([]geofence.Fence{
		{Name: "Belgium", DisplayInMatches: true, Path: [][2]float64{{50, 3}, {50, 6}, {51, 6}, {51, 3}, {50, 3}}},
	})
	event := &FortData{
		ID: "stop1", FortType: "pokestop", IsEmpty: false,
		ChangeTypes: []string{"name"},
		Latitude:    50.5, Longitude: 4.5,
	}

	var off, on []webhook.MatchedUser
	for _, flag := range []bool{false, true} {
		st := &state.State{
			Humans:       humans,
			Forts:        rulesPointers,
			FortsByHuman: perHuman,
			Geofence:     spatial,
			GeoIndex:     state.BuildHumanGeoIndex(humans, nil),
		}
		matcher := &FortMatcher{GeographicPrefilter: flag}
		users, _ := matcher.Match(event, st)
		if flag {
			on = users
		} else {
			off = users
		}
	}
	assertMatchedUserParity(t, off, on)
}
