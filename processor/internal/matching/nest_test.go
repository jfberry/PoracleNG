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

	matched, _ := matcher.Match(data, st)
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

	matched, _ := matcher.Match(data, st)
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

	matched, _ := matcher.Match(data, st)
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
	matched, _ := matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for wrong form, got %d", len(matched))
	}

	// Correct form
	data.Form = 5
	matched, _ = matcher.Match(data, st)
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

	matched, _ := matcher.Match(data, st)
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
	matched, _ := matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for low spawn avg, got %d", len(matched))
	}

	// Above minimum
	data.PokemonAvg = 15.0
	matched, _ = matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for high spawn avg, got %d", len(matched))
	}
}

func TestNestBlockedAlerts(t *testing.T) {
	human := makeHuman("user1")
	human.SetBlockedAlerts(`["nest"]`)
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

	matched, _ := matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for blocked alerts, got %d", len(matched))
	}
}

func TestNestMatch_RecordsMatchingDuration(t *testing.T) {
	metrics.MatchingDuration.Reset()
	matcher := &NestMatcher{}
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
		Nests:    nil,
		Geofence: si,
	}

	data := &NestData{
		NestID: 1, PokemonID: 25, Form: 0, PokemonAvg: 5.0,
		Latitude: 51.0, Longitude: 0.0,
	}
	matcher.Match(data, st)

	h, err := metrics.MatchingDuration.GetMetricWithLabelValues("nest")
	if err != nil {
		t.Fatalf("get metric: %v", err)
	}
	var out dto.Metric
	if err := h.(prometheus.Histogram).Write(&out); err != nil {
		t.Fatalf("write metric: %v", err)
	}
	if got := out.GetHistogram().GetSampleCount(); got != 1 {
		t.Errorf("MatchingDuration{type=nest} sample count = %d, want 1", got)
	}
}

func TestNestMatch_RecordsCandidateCount(t *testing.T) {
	metrics.MatchingCandidates.Reset()
	human := makeHuman("u1")
	nest1 := &db.NestTracking{
		ID: "u1", ProfileNo: 1, PokemonID: 25, Form: 0,
		MinSpawnAvg: 0, Distance: 0, Template: "1",
	}
	nest2 := &db.NestTracking{
		ID: "u2", ProfileNo: 1, PokemonID: 0, Form: 0, // any pokemon
		MinSpawnAvg: 0, Distance: 0, Template: "1",
	}
	humans := map[string]*db.Human{
		"u1": human,
		"u2": makeHuman("u2"),
	}
	st := makeNestTestState([]*db.NestTracking{nest1, nest2}, humans)
	matcher := &NestMatcher{}

	data := &NestData{
		NestID: 1, PokemonID: 25, Form: 0, PokemonAvg: 5.0,
		Latitude: 51.0, Longitude: 0.0,
	}
	matcher.Match(data, st)

	h, _ := metrics.MatchingCandidates.GetMetricWithLabelValues("nest")
	var out dto.Metric
	_ = h.(prometheus.Histogram).Write(&out)
	if got := out.GetHistogram().GetSampleSum(); got != 2 {
		t.Errorf("MatchingCandidates sample sum = %v, want 2", got)
	}
}

// TestNestMatch_GeoPrefilterParity asserts flag-on and flag-off produce identical results.
func TestNestMatch_GeoPrefilterParity(t *testing.T) {
	humans := map[string]*db.Human{
		"u1": {ID: "u1", Enabled: true, Area: []string{"belgium"}, Latitude: 50.5, Longitude: 4.5, CurrentProfileNo: 1},
	}
	rules := []db.NestTracking{
		{ID: "u1", ProfileNo: 1, PokemonID: 25, Form: 0, MinSpawnAvg: 0},
	}
	rulesPointers := make([]*db.NestTracking, len(rules))
	for i := range rules {
		rulesPointers[i] = &rules[i]
	}
	perHuman := db.PartitionByHuman[db.NestTracking](rulesPointers)

	spatial := geofence.NewSpatialIndex([]geofence.Fence{
		{Name: "Belgium", DisplayInMatches: true, Path: [][2]float64{{50, 3}, {50, 6}, {51, 6}, {51, 3}, {50, 3}}},
	})
	event := &NestData{
		NestID: 1, PokemonID: 25, Form: 0, PokemonAvg: 5.0,
		Latitude: 50.5, Longitude: 4.5,
	}

	var off, on []webhook.MatchedUser
	for _, flag := range []bool{false, true} {
		st := &state.State{
			Humans:       humans,
			Nests:        rulesPointers,
			NestsByHuman: perHuman,
			Geofence:     spatial,
			GeoIndex:     state.BuildHumanGeoIndex(humans, nil),
		}
		matcher := &NestMatcher{GeographicPrefilter: flag}
		users, _ := matcher.Match(event, st)
		if flag {
			on = users
		} else {
			off = users
		}
	}
	assertMatchedUserParity(t, off, on)
}
