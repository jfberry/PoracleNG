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
		Clean:     0,
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

	matched, _ := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for correct pokemon, got %d", len(matched))
	}

	// Wrong pokemon
	data.PokemonID = 26
	matched, _ = matcher.Match(data, st)
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
	matched, _ := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for correct level, got %d", len(matched))
	}

	// Wrong level
	data.Level = 3
	matched, _ = matcher.Match(data, st)
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
		matched, _ := matcher.Match(data, st)
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
	matched, _ := matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for non-gmax when tracking gmax, got %d", len(matched))
	}

	// Gmax should match
	data.Gmax = 1
	matched, _ = matcher.Match(data, st)
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
	matched, _ := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for correct form, got %d", len(matched))
	}

	data.Form = 6 // wrong form
	matched, _ = matcher.Match(data, st)
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
	matched, _ := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for move1, got %d", len(matched))
	}

	// Match on move2
	data.Move1 = 99
	data.Move2 = 14
	matched, _ = matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for move2, got %d", len(matched))
	}

	// Neither move matches
	data.Move1 = 88
	data.Move2 = 99
	matched, _ = matcher.Match(data, st)
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
	matched, _ := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for correct station, got %d", len(matched))
	}

	// Wrong station
	data.StationID = "station-xyz"
	matched, _ = matcher.Match(data, st)
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
	matched, _ := matcher.Match(data, st)
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
	matched, _ := matcher.Match(data, st)
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
	matched, _ := matcher.Match(data, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for correct evolution, got %d", len(matched))
	}

	data.Evolution = 1
	matched, _ = matcher.Match(data, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for wrong evolution, got %d", len(matched))
	}
}

func TestMaxbattleMatch_RecordsMatchingDuration(t *testing.T) {
	metrics.MatchingDuration.Reset()
	matcher := &MaxbattleMatcher{}
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
		Humans:     map[string]*db.Human{},
		Maxbattles: nil,
		Geofence:   si,
	}

	data := &MaxbattleData{
		StationID: "station1", PokemonID: 143, Form: 0, Level: 3,
		Gmax: 1, Evolution: 0, Move1: 100, Move2: 200,
		Latitude: 51.0, Longitude: 0.0,
	}
	matcher.Match(data, st)

	h, err := metrics.MatchingDuration.GetMetricWithLabelValues("maxbattle")
	if err != nil {
		t.Fatalf("get metric: %v", err)
	}
	var out dto.Metric
	if err := h.(prometheus.Histogram).Write(&out); err != nil {
		t.Fatalf("write metric: %v", err)
	}
	if got := out.GetHistogram().GetSampleCount(); got != 1 {
		t.Errorf("MatchingDuration{type=maxbattle} sample count = %d, want 1", got)
	}
}

func TestMaxbattleMatch_RecordsCandidateCount(t *testing.T) {
	metrics.MatchingCandidates.Reset()
	human := makeHuman("u1")
	mb1 := makeMaxbattle("u1", 143, 3)
	mb2 := makeMaxbattle("u2", 9000, 3) // level-based: any pokemon at level 3
	humans := map[string]*db.Human{
		"u1": human,
		"u2": makeHuman("u2"),
	}
	st := makeMaxbattleState([]*db.MaxbattleTracking{mb1, mb2}, humans)
	matcher := &MaxbattleMatcher{}

	data := &MaxbattleData{
		StationID: "station1", PokemonID: 143, Form: 0, Level: 3,
		Gmax: 1, Evolution: 0, Move1: 100, Move2: 200,
		Latitude: 51.0, Longitude: 0.0,
	}
	matcher.Match(data, st)

	h, _ := metrics.MatchingCandidates.GetMetricWithLabelValues("maxbattle")
	var out dto.Metric
	_ = h.(prometheus.Histogram).Write(&out)
	if got := out.GetHistogram().GetSampleSum(); got != 2 {
		t.Errorf("MatchingCandidates sample sum = %v, want 2", got)
	}
}

// TestMaxbattleMatch_GeoPrefilterParity asserts flag-on and flag-off produce identical results.
func TestMaxbattleMatch_GeoPrefilterParity(t *testing.T) {
	humans := map[string]*db.Human{
		"u1": {ID: "u1", Enabled: true, Area: []string{"belgium"}, Latitude: 50.5, Longitude: 4.5, CurrentProfileNo: 1},
	}
	rules := []db.MaxbattleTracking{
		{ID: "u1", ProfileNo: 1, PokemonID: 143, Level: 3, Form: 0, Move: 9000, Evolution: 9000, Gmax: 0},
	}
	rulesPointers := make([]*db.MaxbattleTracking, len(rules))
	for i := range rules {
		rulesPointers[i] = &rules[i]
	}
	perHuman := db.PartitionByHuman(rulesPointers, db.MaxbattleHumanID)

	spatial := geofence.NewSpatialIndex([]geofence.Fence{
		{Name: "Belgium", DisplayInMatches: true, Path: [][2]float64{{50, 3}, {50, 6}, {51, 6}, {51, 3}, {50, 3}}},
	})
	event := &MaxbattleData{
		StationID: "station1", PokemonID: 143, Form: 0, Level: 3,
		Gmax: 1, Evolution: 0, Move1: 100, Move2: 200,
		Latitude: 50.5, Longitude: 4.5,
	}

	var off, on []webhook.MatchedUser
	for _, flag := range []bool{false, true} {
		st := &state.State{
			Humans:            humans,
			Maxbattles:        rulesPointers,
			MaxbattlesByHuman: perHuman,
			Geofence:          spatial,
			GeoIndex:          state.BuildHumanGeoIndex(humans, nil),
		}
		matcher := &MaxbattleMatcher{GeographicPrefilter: flag}
		users, _ := matcher.Match(event, st)
		if flag {
			on = users
		} else {
			off = users
		}
	}
	if len(off) != len(on) {
		t.Fatalf("parity violation: flag-off matched %d users, flag-on matched %d", len(off), len(on))
	}
	seenOff := map[string]int{}
	for _, u := range off {
		seenOff[u.ID]++
	}
	seenOn := map[string]int{}
	for _, u := range on {
		seenOn[u.ID]++
	}
	for id, n := range seenOff {
		if seenOn[id] != n {
			t.Errorf("parity violation: user %q matched %d times flag-off, %d times flag-on", id, n, seenOn[id])
		}
	}
}
