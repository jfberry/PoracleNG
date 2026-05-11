package matching

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/metrics"
	"github.com/pokemon/poracleng/processor/internal/pvp"
	"github.com/pokemon/poracleng/processor/internal/state"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func makeTestState(monsters []*db.MonsterTracking, humans map[string]*db.Human) *state.State {
	idx := &db.MonsterIndex{
		ByPokemonID:   make(map[int][]*db.MonsterTracking),
		PVPSpecific:   make(map[int][]*db.MonsterTracking),
		PVPEverything: make(map[int][]*db.MonsterTracking),
	}
	for _, league := range []int{500, 1500, 2500} {
		idx.PVPSpecific[league] = nil
		idx.PVPEverything[league] = nil
	}

	for _, m := range monsters {
		if m.PVPRankingLeague != 0 {
			if m.PokemonID != 0 {
				idx.PVPSpecific[m.PVPRankingLeague] = append(idx.PVPSpecific[m.PVPRankingLeague], m)
			} else {
				idx.PVPEverything[m.PVPRankingLeague] = append(idx.PVPEverything[m.PVPRankingLeague], m)
			}
		} else {
			idx.ByPokemonID[m.PokemonID] = append(idx.ByPokemonID[m.PokemonID], m)
		}
	}

	// Simple geofence covering a large area
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
		Monsters: idx,
		Geofence: si,
		Fences:   fences,
	}
}

func makeHuman(id string) *db.Human {
	return &db.Human{
		ID:               id,
		Name:             "Test User",
		Type:             "discord:user",
		Enabled:          true,
		AdminDisable:     false,
		Area:             []string{"testarea"},
		Latitude:         51.0,
		Longitude:        0.0,
		CurrentProfileNo: 1,
	}
}

func makeMonster(id string, pokemonID int) *db.MonsterTracking {
	return &db.MonsterTracking{
		ID:              id,
		ProfileNo:       1,
		PokemonID:       pokemonID,
		Form:            0,
		MinIV:           -1,
		MaxIV:           100,
		MinCP:           0,
		MaxCP:           defaultMaxCP,
		MinLevel:        0,
		MaxLevel:        defaultMaxLevel,
		ATK:             0,
		DEF:             0,
		STA:             0,
		MaxATK:          15,
		MaxDEF:          15,
		MaxSTA:          15,
		Gender:          0,
		MinWeight:       0,
		MaxWeight:       defaultMaxWeight,
		MinTime:         0,
		Rarity:          -1,
		MaxRarity:       6,
		Size:            -1,
		MaxSize:         5,
		Template:        "1",
		PVPRankingWorst: 4096,
		PVPRankingBest:  1,
		PVPRankingMinCP: 0,
	}
}

func TestPokemonMatchBasic(t *testing.T) {
	human := makeHuman("user1")
	monster := makeMonster("user1", 25) // Pikachu

	st := makeTestState([]*db.MonsterTracking{monster}, map[string]*db.Human{"user1": human})

	matcher := &PokemonMatcher{PVPQueryMaxRank: 100}

	pokemon := &ProcessedPokemon{
		PokemonID:   25,
		Form:        0,
		IV:          100,
		CP:          1000,
		Level:       20,
		ATK:         15,
		DEF:         15,
		STA:         15,
		Gender:      1,
		Weight:      6.0,
		Size:        1,
		RarityGroup: 1,
		TTHSeconds:  600,
		Latitude:    51.5,
		Longitude:   0.0,
		Encountered: true,
		PVPBestRank: make(map[int][]pvp.LeagueRank),
		PVPEvoData:  make(map[int]map[int][]pvp.LeagueRank),
	}

	matched, _ := matcher.Match(pokemon, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match, got %d", len(matched))
	}
	if len(matched) > 0 && matched[0].ID != "user1" {
		t.Errorf("Expected user1, got %s", matched[0].ID)
	}
}

func TestPokemonMatchCatchAll(t *testing.T) {
	human := makeHuman("user1")
	monster := makeMonster("user1", 0) // Catch-all

	st := makeTestState([]*db.MonsterTracking{monster}, map[string]*db.Human{"user1": human})
	matcher := &PokemonMatcher{PVPQueryMaxRank: 100}

	pokemon := &ProcessedPokemon{
		PokemonID:   999,
		Form:        0,
		IV:          50,
		CP:          500,
		Level:       15,
		ATK:         10,
		DEF:         10,
		STA:         10,
		Gender:      2,
		Weight:      5.0,
		Size:        2,
		RarityGroup: 2,
		TTHSeconds:  300,
		Latitude:    51.0,
		Longitude:   0.0,
		Encountered: true,
		PVPBestRank: make(map[int][]pvp.LeagueRank),
		PVPEvoData:  make(map[int]map[int][]pvp.LeagueRank),
	}

	matched, _ := matcher.Match(pokemon, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match (catch-all), got %d", len(matched))
	}
}

func TestPokemonMatchIVFilter(t *testing.T) {
	human := makeHuman("user1")
	monster := makeMonster("user1", 25)
	monster.MinIV = 90
	monster.MaxIV = 100

	st := makeTestState([]*db.MonsterTracking{monster}, map[string]*db.Human{"user1": human})
	matcher := &PokemonMatcher{PVPQueryMaxRank: 100}

	// Low IV - should not match
	pokemon := &ProcessedPokemon{
		PokemonID:   25,
		Form:        0,
		IV:          50,
		CP:          500,
		Level:       15,
		ATK:         7,
		DEF:         7,
		STA:         7,
		Weight:      5.0,
		Size:        1,
		RarityGroup: 1,
		TTHSeconds:  600,
		Latitude:    51.0,
		Longitude:   0.0,
		Encountered: true,
		PVPBestRank: make(map[int][]pvp.LeagueRank),
		PVPEvoData:  make(map[int]map[int][]pvp.LeagueRank),
	}

	matched, _ := matcher.Match(pokemon, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for low IV, got %d", len(matched))
	}

	// High IV - should match
	pokemon.IV = 95
	pokemon.ATK = 14
	pokemon.DEF = 14
	pokemon.STA = 15
	matched, _ = matcher.Match(pokemon, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for high IV, got %d", len(matched))
	}
}

func TestPokemonMatchFormFilter(t *testing.T) {
	human := makeHuman("user1")
	monster := makeMonster("user1", 25)
	monster.Form = 5 // specific form

	st := makeTestState([]*db.MonsterTracking{monster}, map[string]*db.Human{"user1": human})
	matcher := &PokemonMatcher{PVPQueryMaxRank: 100}

	// Wrong form
	pokemon := &ProcessedPokemon{
		PokemonID: 25, Form: 0, IV: 100, CP: 1000, Level: 20,
		ATK: 15, DEF: 15, STA: 15, Weight: 5.0, Size: 1, RarityGroup: 1,
		TTHSeconds: 600, Latitude: 51.0, Longitude: 0.0, Encountered: true,
		PVPBestRank: make(map[int][]pvp.LeagueRank),
		PVPEvoData:  make(map[int]map[int][]pvp.LeagueRank),
	}

	matched, _ := matcher.Match(pokemon, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for wrong form, got %d", len(matched))
	}

	// Correct form
	pokemon.Form = 5
	matched, _ = matcher.Match(pokemon, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for correct form, got %d", len(matched))
	}
}

func TestPokemonMatchGenderFilter(t *testing.T) {
	human := makeHuman("user1")
	monster := makeMonster("user1", 25)
	monster.Gender = 1 // male only

	st := makeTestState([]*db.MonsterTracking{monster}, map[string]*db.Human{"user1": human})
	matcher := &PokemonMatcher{PVPQueryMaxRank: 100}

	pokemon := &ProcessedPokemon{
		PokemonID: 25, Form: 0, IV: 100, CP: 1000, Level: 20,
		ATK: 15, DEF: 15, STA: 15, Gender: 2, Weight: 5.0, Size: 1, RarityGroup: 1,
		TTHSeconds: 600, Latitude: 51.0, Longitude: 0.0, Encountered: true,
		PVPBestRank: make(map[int][]pvp.LeagueRank),
		PVPEvoData:  make(map[int]map[int][]pvp.LeagueRank),
	}

	matched, _ := matcher.Match(pokemon, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for female when tracking male, got %d", len(matched))
	}

	pokemon.Gender = 1
	matched, _ = matcher.Match(pokemon, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for male, got %d", len(matched))
	}
}

func TestPokemonUnencounteredDefaultFilters(t *testing.T) {
	// Unencountered pokemon should match tracking rules with default stat filters
	human := makeHuman("user1")
	monster := makeMonster("user1", 25)

	st := makeTestState([]*db.MonsterTracking{monster}, map[string]*db.Human{"user1": human})
	matcher := &PokemonMatcher{PVPQueryMaxRank: 100}

	pokemon := &ProcessedPokemon{
		PokemonID: 25, Form: 0, IV: -1, Encountered: false,
		Gender: 1, Size: 1, RarityGroup: 1, TTHSeconds: 600,
		Latitude: 51.0, Longitude: 0.0,
		PVPBestRank: make(map[int][]pvp.LeagueRank),
		PVPEvoData:  make(map[int]map[int][]pvp.LeagueRank),
	}

	matched, _ := matcher.Match(pokemon, st)
	if len(matched) != 1 {
		t.Errorf("Unencountered with default filters should match, got %d", len(matched))
	}
}

func TestPokemonUnencounteredStatFilters(t *testing.T) {
	// Unencountered pokemon should NOT match when stat filters are set
	human := makeHuman("user1")

	pokemon := &ProcessedPokemon{
		PokemonID: 25, Form: 0, IV: -1, Encountered: false,
		Gender: 1, Size: 1, RarityGroup: 1, TTHSeconds: 600,
		Latitude: 51.0, Longitude: 0.0,
		PVPBestRank: make(map[int][]pvp.LeagueRank),
		PVPEvoData:  make(map[int]map[int][]pvp.LeagueRank),
	}

	matcher := &PokemonMatcher{PVPQueryMaxRank: 100}

	tests := []struct {
		name   string
		modify func(m *db.MonsterTracking)
	}{
		{"maxlevel10", func(m *db.MonsterTracking) { m.MaxLevel = 10 }},
		{"minlevel20", func(m *db.MonsterTracking) { m.MinLevel = 20 }},
		{"maxcp500", func(m *db.MonsterTracking) { m.MaxCP = 500 }},
		{"mincp100", func(m *db.MonsterTracking) { m.MinCP = 100 }},
		{"maxatk0", func(m *db.MonsterTracking) { m.MaxATK = 0 }},
		{"minatk10", func(m *db.MonsterTracking) { m.ATK = 10 }},
		{"maxweight1000", func(m *db.MonsterTracking) { m.MaxWeight = 1000 }},
		{"minweight500", func(m *db.MonsterTracking) { m.MinWeight = 500 }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			monster := makeMonster("user1", 25)
			tt.modify(monster)
			st := makeTestState([]*db.MonsterTracking{monster}, map[string]*db.Human{"user1": human})

			matched, _ := matcher.Match(pokemon, st)
			if len(matched) != 0 {
				t.Errorf("Unencountered should not match %s filter, got %d", tt.name, len(matched))
			}
		})
	}
}

func TestPokemonMatchDistanceFilter(t *testing.T) {
	human := makeHuman("user1")
	human.Latitude = 51.5
	human.Longitude = 0.0

	monster := makeMonster("user1", 25)
	monster.Distance = 1000 // 1km radius

	st := makeTestState([]*db.MonsterTracking{monster}, map[string]*db.Human{"user1": human})
	matcher := &PokemonMatcher{PVPQueryMaxRank: 100}

	// Far away - should not match
	pokemon := &ProcessedPokemon{
		PokemonID: 25, Form: 0, IV: 100, CP: 1000, Level: 20,
		ATK: 15, DEF: 15, STA: 15, Weight: 5.0, Size: 1, RarityGroup: 1,
		TTHSeconds: 600, Latitude: 51.6, Longitude: 0.0, Encountered: true,
		PVPBestRank: make(map[int][]pvp.LeagueRank),
		PVPEvoData:  make(map[int]map[int][]pvp.LeagueRank),
	}

	matched, _ := matcher.Match(pokemon, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for distant pokemon, got %d", len(matched))
	}

	// Close - should match
	pokemon.Latitude = 51.5005
	matched, _ = matcher.Match(pokemon, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for close pokemon, got %d", len(matched))
	}
}

func TestPokemonMatchWeightFilter(t *testing.T) {
	human := makeHuman("user1")
	monster := makeMonster("user1", 25)
	monster.MinWeight = 5000  // 5kg in weight*1000
	monster.MaxWeight = 10000 // 10kg in weight*1000

	st := makeTestState([]*db.MonsterTracking{monster}, map[string]*db.Human{"user1": human})
	matcher := &PokemonMatcher{PVPQueryMaxRank: 100}

	pokemon := &ProcessedPokemon{
		PokemonID: 25, Form: 0, IV: 100, CP: 1000, Level: 20,
		ATK: 15, DEF: 15, STA: 15, Weight: 7.0, Size: 1, RarityGroup: 1,
		TTHSeconds: 600, Latitude: 51.0, Longitude: 0.0, Encountered: true,
		PVPBestRank: make(map[int][]pvp.LeagueRank),
		PVPEvoData:  make(map[int]map[int][]pvp.LeagueRank),
	}

	matched, _ := matcher.Match(pokemon, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for weight in range, got %d", len(matched))
	}

	pokemon.Weight = 15.0 // Too heavy
	matched, _ = matcher.Match(pokemon, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for heavy pokemon, got %d", len(matched))
	}
}

func TestPokemonMatchPVPFilter(t *testing.T) {
	human := makeHuman("user1")
	monster := &db.MonsterTracking{
		ID: "user1", ProfileNo: 1, PokemonID: 25, Form: 0,
		MinIV: -1, MaxIV: 100, MinCP: 0, MaxCP: 9999,
		MinLevel: 0, MaxLevel: 40, ATK: 0, DEF: 0, STA: 0,
		MaxATK: 15, MaxDEF: 15, MaxSTA: 15, Gender: 0,
		MinWeight: 0, MaxWeight: 99999999, Rarity: -1, MaxRarity: 6,
		Size: -1, MaxSize: 5, Template: "1",
		PVPRankingLeague: 1500, PVPRankingBest: 1, PVPRankingWorst: 50,
		PVPRankingMinCP: 1400, PVPRankingCap: 0,
	}

	st := makeTestState([]*db.MonsterTracking{monster}, map[string]*db.Human{"user1": human})
	matcher := &PokemonMatcher{PVPQueryMaxRank: 100}

	pokemon := &ProcessedPokemon{
		PokemonID: 25, Form: 0, IV: 50, CP: 500, Level: 15,
		ATK: 0, DEF: 15, STA: 15, Weight: 5.0, Size: 1, RarityGroup: 1,
		TTHSeconds: 600, Latitude: 51.0, Longitude: 0.0, Encountered: true,
		PVPBestRank: map[int][]pvp.LeagueRank{
			1500: {{Rank: 10, CP: 1490, Caps: []int{50}}},
		},
		PVPEvoData: make(map[int]map[int][]pvp.LeagueRank),
	}

	matched, _ := matcher.Match(pokemon, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 PVP match, got %d", len(matched))
	}

	// Rank too high
	pokemon.PVPBestRank[1500] = []pvp.LeagueRank{{Rank: 100, CP: 1490, Caps: []int{50}}}
	matched, _ = matcher.Match(pokemon, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for bad PVP rank, got %d", len(matched))
	}
}

func TestPokemonMatchPVPCapFilter(t *testing.T) {
	human := makeHuman("user1")

	// Tracking rule requires cap 50
	monster := &db.MonsterTracking{
		ID: "user1", ProfileNo: 1, PokemonID: 25, Form: 0,
		MinIV: -1, MaxIV: 100, MinCP: 0, MaxCP: defaultMaxCP,
		MinLevel: 0, MaxLevel: defaultMaxLevel, ATK: 0, DEF: 0, STA: 0,
		MaxATK: 15, MaxDEF: 15, MaxSTA: 15, Gender: 0,
		MinWeight: 0, MaxWeight: defaultMaxWeight, Rarity: -1, MaxRarity: 6,
		Size: -1, MaxSize: 5, Template: "1",
		PVPRankingLeague: 1500, PVPRankingBest: 1, PVPRankingWorst: 100,
		PVPRankingMinCP: 0, PVPRankingCap: 50,
	}

	st := makeTestState([]*db.MonsterTracking{monster}, map[string]*db.Human{"user1": human})
	matcher := &PokemonMatcher{PVPQueryMaxRank: 100}

	// Pokemon has cap 50 — should match
	pokemon := &ProcessedPokemon{
		PokemonID: 25, Form: 0, IV: 50, CP: 500, Level: 15,
		ATK: 0, DEF: 15, STA: 15, Weight: 5.0, Size: 1, RarityGroup: 1,
		TTHSeconds: 600, Latitude: 51.0, Longitude: 0.0, Encountered: true,
		PVPBestRank: map[int][]pvp.LeagueRank{
			1500: {{Rank: 10, CP: 1490, Caps: []int{50, 51}}},
		},
		PVPEvoData: make(map[int]map[int][]pvp.LeagueRank),
	}

	matched, _ := matcher.Match(pokemon, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for PVP cap 50, got %d", len(matched))
	}

	// Pokemon only has cap 51 — should not match cap 50 tracking
	pokemon.PVPBestRank[1500] = []pvp.LeagueRank{{Rank: 10, CP: 1490, Caps: []int{51}}}
	matched, _ = matcher.Match(pokemon, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for PVP cap 51 when tracking cap 50, got %d", len(matched))
	}

	// Tracking rule with cap 0 (any) — should match any caps
	monster.PVPRankingCap = 0
	st = makeTestState([]*db.MonsterTracking{monster}, map[string]*db.Human{"user1": human})
	pokemon.PVPBestRank[1500] = []pvp.LeagueRank{{Rank: 10, CP: 1490, Caps: []int{51}}}
	matched, _ = matcher.Match(pokemon, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for PVP cap 0 (any), got %d", len(matched))
	}

	// Pokemon has empty caps with cap filter set — should not filter (caps check skipped when empty)
	monster.PVPRankingCap = 50
	st = makeTestState([]*db.MonsterTracking{monster}, map[string]*db.Human{"user1": human})
	pokemon.PVPBestRank[1500] = []pvp.LeagueRank{{Rank: 10, CP: 1490, Caps: nil}}
	matched, _ = matcher.Match(pokemon, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match when pokemon has no caps (filter skipped), got %d", len(matched))
	}
}

// Test case from real webhook: Venipede (543) spawns with PVP data showing
// Scolipede (545) is rank 1 in ultra league. User tracks pokemon 545 with PVP.
func TestPokemonMatchPVPEvolutionDirect(t *testing.T) {
	human := makeHuman("user1")

	// User tracks Scolipede (545) in ultra league PVP
	monster := &db.MonsterTracking{
		ID: "user1", ProfileNo: 1, PokemonID: 545, Form: 0,
		MinIV: -1, MaxIV: 100, MinCP: 0, MaxCP: 9999,
		MinLevel: 0, MaxLevel: 40, ATK: 0, DEF: 0, STA: 0,
		MaxATK: 15, MaxDEF: 15, MaxSTA: 15, Gender: 0,
		MinWeight: 0, MaxWeight: 99999999, Rarity: -1, MaxRarity: 6,
		Size: -1, MaxSize: 5, Template: "1",
		PVPRankingLeague: 2500, PVPRankingBest: 1, PVPRankingWorst: 100,
		PVPRankingMinCP: 0, PVPRankingCap: 0,
	}

	st := makeTestState([]*db.MonsterTracking{monster}, map[string]*db.Human{"user1": human})
	matcher := &PokemonMatcher{PVPQueryMaxRank: 100, PVPEvolutionDirectTracking: true}

	// Venipede (543) spawns with PVP evo data showing Scolipede (545) rank 1 ultra league
	pokemon := &ProcessedPokemon{
		PokemonID: 543, Form: 2033, IV: 66.7, CP: 461, Level: 27,
		ATK: 0, DEF: 15, STA: 15, Weight: 2.871, Size: 3, RarityGroup: 1,
		TTHSeconds: 600, Latitude: 51.0, Longitude: 0.0, Encountered: true,
		PVPBestRank: map[int][]pvp.LeagueRank{
			2500: {{Rank: 1075, CP: 676, Caps: []int{50}}},
		},
		PVPEvoData: map[int]map[int][]pvp.LeagueRank{
			545: {
				2500: {{Rank: 1, CP: 2500, Caps: []int{50}}},
			},
		},
	}

	matched, _ := matcher.Match(pokemon, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 PVP evolution direct match for Scolipede, got %d", len(matched))
	}
	if len(matched) == 1 && matched[0].PokemonID != 545 {
		t.Errorf("Expected matched PokemonID 545 (Scolipede), got %d", matched[0].PokemonID)
	}

	// With evolution direct tracking disabled, should not match
	matcher2 := &PokemonMatcher{PVPQueryMaxRank: 100, PVPEvolutionDirectTracking: false}
	matched, _ = matcher2.Match(pokemon, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches with evo direct tracking disabled, got %d", len(matched))
	}
}

// Test from real webhook: Eevee (133) spawns with PVP evolution data showing
// Sylveon (700) rank 512 in great league. User tracks Sylveon (700) with PVP great league.
// This should match via PVP evolution direct tracking.
func TestPokemonMatchPVPEvolutionDirectEevee(t *testing.T) {
	human := makeHuman("user1")

	// User tracks Sylveon (700) in great league PVP
	monster := &db.MonsterTracking{
		ID: "user1", ProfileNo: 1, PokemonID: 700, Form: 0,
		MinIV: -1, MaxIV: 100, MinCP: 0, MaxCP: 9999,
		MinLevel: 0, MaxLevel: 55, ATK: 0, DEF: 0, STA: 0,
		MaxATK: 15, MaxDEF: 15, MaxSTA: 15, Gender: 0,
		MinWeight: 0, MaxWeight: 99999999, Rarity: -1, MaxRarity: 6,
		Size: -1, MaxSize: 5, Template: "1",
		PVPRankingLeague: 1500, PVPRankingBest: 1, PVPRankingWorst: 1000,
		PVPRankingMinCP: 0, PVPRankingCap: 0,
	}

	st := makeTestState([]*db.MonsterTracking{monster}, map[string]*db.Human{"user1": human})
	matcher := &PokemonMatcher{PVPQueryMaxRank: 4096, PVPEvolutionDirectTracking: true}

	// Real Eevee webhook: pokemon_id=133, form=1092
	// PVP great league has evolution entry: pokemon=700 (Sylveon), form=3062, rank=512, cp=1496
	pokemon := &ProcessedPokemon{
		PokemonID: 133, Form: 1092, IV: 44.4, CP: 125, Level: 5,
		ATK: 5, DEF: 9, STA: 6, Weight: 9.308, Size: 3, RarityGroup: 1,
		TTHSeconds: 1580, Latitude: 51.0, Longitude: 0.0, Encountered: true,
		PVPBestRank: map[int][]pvp.LeagueRank{
			1500: {{Rank: 2538, CP: 1052, Caps: []int{50}}},
		},
		PVPEvoData: map[int]map[int][]pvp.LeagueRank{
			134: {1500: {{Rank: 1498, CP: 1479, Caps: []int{50}}}},
			135: {1500: {{Rank: 1780, CP: 1481, Caps: []int{50}}}},
			136: {1500: {{Rank: 1602, CP: 1477, Caps: []int{50}}}},
			196: {1500: {{Rank: 2891, CP: 1464, Caps: []int{50}}}},
			197: {1500: {{Rank: 1474, CP: 1484, Caps: []int{50}}}},
			470: {1500: {{Rank: 2447, CP: 1470, Caps: []int{50}}}},
			471: {1500: {{Rank: 1541, CP: 1484, Caps: []int{50}}}},
			700: {1500: {{Rank: 512, CP: 1496, Caps: []int{50}}}},
		},
	}

	matched, _ := matcher.Match(pokemon, st)
	if len(matched) != 1 {
		t.Fatalf("Expected 1 PVP evolution match for Sylveon via Eevee, got %d", len(matched))
	}
	if matched[0].PokemonID != 700 {
		t.Errorf("Expected matched PokemonID 700 (Sylveon), got %d", matched[0].PokemonID)
	}

	// With evolution direct tracking disabled, should not match
	matcher2 := &PokemonMatcher{PVPQueryMaxRank: 4096, PVPEvolutionDirectTracking: false}
	matched2, _ := matcher2.Match(pokemon, st)
	if len(matched2) != 0 {
		t.Errorf("Expected 0 matches with evo direct tracking disabled, got %d", len(matched2))
	}

	// Track Umbreon (197) ultra league — Eevee also has ultra evo data for it
	monsterUmbreon := &db.MonsterTracking{
		ID: "user1", ProfileNo: 1, PokemonID: 197, Form: 0,
		MinIV: -1, MaxIV: 100, MinCP: 0, MaxCP: 9999,
		MinLevel: 0, MaxLevel: 55, ATK: 0, DEF: 0, STA: 0,
		MaxATK: 15, MaxDEF: 15, MaxSTA: 15, Gender: 0,
		MinWeight: 0, MaxWeight: 99999999, Rarity: -1, MaxRarity: 6,
		Size: -1, MaxSize: 5, Template: "1",
		PVPRankingLeague: 2500, PVPRankingBest: 1, PVPRankingWorst: 4096,
		PVPRankingMinCP: 0, PVPRankingCap: 0,
	}

	st2 := makeTestState([]*db.MonsterTracking{monsterUmbreon}, map[string]*db.Human{"user1": human})

	// Add ultra league evo data for Umbreon
	pokemon.PVPEvoData[197][2500] = []pvp.LeagueRank{{Rank: 2688, CP: 2174, Caps: nil}}

	matched3, _ := matcher.Match(pokemon, st2)
	if len(matched3) != 1 {
		t.Fatalf("Expected 1 PVP evolution match for Umbreon via Eevee in ultra, got %d", len(matched3))
	}
	if matched3[0].PokemonID != 197 {
		t.Errorf("Expected matched PokemonID 197 (Umbreon), got %d", matched3[0].PokemonID)
	}
}

// Test that PVP evolution matching uses the evolution's form (from PVP data)
// not the spawned pokemon's form. A user tracking Sylveon form:3062 should
// match an Eevee spawn that has PVP data showing Sylveon form 3062.
func TestPokemonMatchPVPEvolutionFormCheck(t *testing.T) {
	human := makeHuman("user1")

	// Track Sylveon (700) with specific form 3062
	monster := &db.MonsterTracking{
		ID: "user1", ProfileNo: 1, PokemonID: 700, Form: 3062,
		MinIV: -1, MaxIV: 100, MinCP: 0, MaxCP: 9999,
		MinLevel: 0, MaxLevel: 55, ATK: 0, DEF: 0, STA: 0,
		MaxATK: 15, MaxDEF: 15, MaxSTA: 15, Gender: 0,
		MinWeight: 0, MaxWeight: 99999999, Rarity: -1, MaxRarity: 6,
		Size: -1, MaxSize: 5, Template: "1",
		PVPRankingLeague: 1500, PVPRankingBest: 1, PVPRankingWorst: 1000,
		PVPRankingMinCP: 0, PVPRankingCap: 0,
	}

	st := makeTestState([]*db.MonsterTracking{monster}, map[string]*db.Human{"user1": human})
	matcher := &PokemonMatcher{PVPQueryMaxRank: 4096, PVPEvolutionDirectTracking: true}

	// Eevee (133, form 1092) with PVP evo data: Sylveon form 3062 rank 512
	pokemon := &ProcessedPokemon{
		PokemonID: 133, Form: 1092, IV: 44.4, CP: 125, Level: 5,
		ATK: 5, DEF: 9, STA: 6, Weight: 9.308, Size: 3, RarityGroup: 1,
		TTHSeconds: 1580, Latitude: 51.0, Longitude: 0.0, Encountered: true,
		PVPBestRank: map[int][]pvp.LeagueRank{
			1500: {{Rank: 2538, CP: 1052, Caps: []int{50}}},
		},
		PVPEvoData: map[int]map[int][]pvp.LeagueRank{
			700: {1500: {{Rank: 512, CP: 1496, Caps: []int{50}, Form: 3062}}},
		},
	}

	// Should match: PVP evo form 3062 matches tracking form 3062
	matched, _ := matcher.Match(pokemon, st)
	if len(matched) != 1 {
		t.Fatalf("Expected 1 match for Sylveon form:3062 via Eevee PVP evo, got %d", len(matched))
	}

	// Track Sylveon with wrong form — should NOT match
	monsterWrongForm := &db.MonsterTracking{
		ID: "user1", ProfileNo: 1, PokemonID: 700, Form: 9999,
		MinIV: -1, MaxIV: 100, MinCP: 0, MaxCP: 9999,
		MinLevel: 0, MaxLevel: 55, ATK: 0, DEF: 0, STA: 0,
		MaxATK: 15, MaxDEF: 15, MaxSTA: 15, Gender: 0,
		MinWeight: 0, MaxWeight: 99999999, Rarity: -1, MaxRarity: 6,
		Size: -1, MaxSize: 5, Template: "1",
		PVPRankingLeague: 1500, PVPRankingBest: 1, PVPRankingWorst: 1000,
		PVPRankingMinCP: 0, PVPRankingCap: 0,
	}
	st2 := makeTestState([]*db.MonsterTracking{monsterWrongForm}, map[string]*db.Human{"user1": human})
	matched2, _ := matcher.Match(pokemon, st2)
	if len(matched2) != 0 {
		t.Errorf("Expected 0 matches for Sylveon form:9999 (wrong form), got %d", len(matched2))
	}
}

func TestPokemonMatchBlockedAlerts(t *testing.T) {
	human := makeHuman("user1")
	human.SetBlockedAlerts(`["monster","pvp"]`)

	monster := makeMonster("user1", 25)

	st := makeTestState([]*db.MonsterTracking{monster}, map[string]*db.Human{"user1": human})
	matcher := &PokemonMatcher{PVPQueryMaxRank: 100}

	pokemon := &ProcessedPokemon{
		PokemonID: 25, Form: 0, IV: 100, CP: 1000, Level: 20,
		ATK: 15, DEF: 15, STA: 15, Weight: 5.0, Size: 1, RarityGroup: 1,
		TTHSeconds: 600, Latitude: 51.0, Longitude: 0.0, Encountered: true,
		PVPBestRank: make(map[int][]pvp.LeagueRank),
		PVPEvoData:  make(map[int]map[int][]pvp.LeagueRank),
	}

	matched, _ := matcher.Match(pokemon, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for blocked alerts, got %d", len(matched))
	}
}

func TestPokemonMatchMinTime(t *testing.T) {
	human := makeHuman("user1")
	monster := makeMonster("user1", 25)
	monster.MinTime = 300 // at least 5 minutes remaining

	st := makeTestState([]*db.MonsterTracking{monster}, map[string]*db.Human{"user1": human})
	matcher := &PokemonMatcher{PVPQueryMaxRank: 100}

	// Not enough time
	pokemon := &ProcessedPokemon{
		PokemonID: 25, Form: 0, IV: 100, CP: 1000, Level: 20,
		ATK: 15, DEF: 15, STA: 15, Weight: 5.0, Size: 1, RarityGroup: 1,
		TTHSeconds: 100, Latitude: 51.0, Longitude: 0.0, Encountered: true,
		PVPBestRank: make(map[int][]pvp.LeagueRank),
		PVPEvoData:  make(map[int]map[int][]pvp.LeagueRank),
	}

	matched, _ := matcher.Match(pokemon, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for low TTH, got %d", len(matched))
	}

	pokemon.TTHSeconds = 600
	matched, _ = matcher.Match(pokemon, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for sufficient TTH, got %d", len(matched))
	}
}

func TestPokemonMatchProfileFilter(t *testing.T) {
	human := makeHuman("user1")
	human.CurrentProfileNo = 2

	monster := makeMonster("user1", 25)
	monster.ProfileNo = 1 // different profile

	st := makeTestState([]*db.MonsterTracking{monster}, map[string]*db.Human{"user1": human})
	matcher := &PokemonMatcher{PVPQueryMaxRank: 100}

	pokemon := &ProcessedPokemon{
		PokemonID: 25, Form: 0, IV: 100, CP: 1000, Level: 20,
		ATK: 15, DEF: 15, STA: 15, Weight: 5.0, Size: 1, RarityGroup: 1,
		TTHSeconds: 600, Latitude: 51.0, Longitude: 0.0, Encountered: true,
		PVPBestRank: make(map[int][]pvp.LeagueRank),
		PVPEvoData:  make(map[int]map[int][]pvp.LeagueRank),
	}

	matched, _ := matcher.Match(pokemon, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for wrong profile, got %d", len(matched))
	}
}

func TestPokemonMatch_RecordsMatchingDuration(t *testing.T) {
	// Reset metric for deterministic read
	metrics.MatchingDuration.Reset()
	matcher := &PokemonMatcher{}
	pokemon := &ProcessedPokemon{PokemonID: 25, IV: 100, Latitude: 51.0, Longitude: 0.0, Encountered: true}

	// Create state with empty geofence to avoid panic
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
		Monsters: &db.MonsterIndex{ByPokemonID: map[int][]*db.MonsterTracking{}, PVPSpecific: make(map[int][]*db.MonsterTracking), PVPEverything: make(map[int][]*db.MonsterTracking)},
		Geofence: si,
		Fences:   fences,
	}

	matcher.Match(pokemon, st)

	h, err := metrics.MatchingDuration.GetMetricWithLabelValues("pokemon")
	if err != nil {
		t.Fatalf("get metric: %v", err)
	}
	var out dto.Metric
	if err := h.(prometheus.Histogram).Write(&out); err != nil {
		t.Fatalf("write metric: %v", err)
	}
	if got := out.GetHistogram().GetSampleCount(); got != 1 {
		t.Errorf("MatchingDuration{type=pokemon} sample count = %d, want 1", got)
	}
}

func TestPokemonMatch_RecordsCandidateCount(t *testing.T) {
	metrics.MatchingCandidates.Reset()
	matcher := &PokemonMatcher{PVPQueryMaxRank: 100}
	// Pre-seed two rules under pokemon_id=25 so candidate count > 0
	human := makeHuman("u1")
	rules := []*db.MonsterTracking{
		makeMonster("u1", 25),
		makeMonster("u2", 25),
	}
	// Add human for second user
	humans := map[string]*db.Human{
		"u1": human,
		"u2": makeHuman("u2"),
	}
	st := makeTestState(rules, humans)

	pokemon := &ProcessedPokemon{
		PokemonID:   25,
		Form:        0,
		IV:          100,
		CP:          1000,
		Level:       20,
		ATK:         15,
		DEF:         15,
		STA:         15,
		Gender:      1,
		Weight:      6.0,
		Size:        1,
		RarityGroup: 1,
		TTHSeconds:  600,
		Latitude:    51.0,
		Longitude:   0.0,
		Encountered: true,
		PVPBestRank: make(map[int][]pvp.LeagueRank),
		PVPEvoData:  make(map[int]map[int][]pvp.LeagueRank),
	}
	matcher.Match(pokemon, st)

	h, err := metrics.MatchingCandidates.GetMetricWithLabelValues("pokemon")
	if err != nil {
		t.Fatalf("get metric: %v", err)
	}
	var out dto.Metric
	if err := h.(prometheus.Histogram).Write(&out); err != nil {
		t.Fatalf("write metric: %v", err)
	}
	if got := out.GetHistogram().GetSampleSum(); got != 2 {
		t.Errorf("MatchingCandidates sample sum = %v, want 2", got)
	}
}

// TestPokemonMatch_GeoPrefilterParity is the safety net for the
// flagged code path. Same inputs, both flag values, identical outputs.
// If this test ever fails, the flag-on path is dropping (or producing)
// rules that the flag-off path didn't.
func TestPokemonMatch_GeoPrefilterParity(t *testing.T) {
	humans := map[string]*db.Human{
		"u1": {ID: "u1", Enabled: true, Area: []string{"belgium"}, Latitude: 50.5, Longitude: 4.5, CurrentProfileNo: 1},
		"u2": {ID: "u2", Enabled: true, Area: []string{"belgium"}, Latitude: 50.5, Longitude: 4.5, CurrentProfileNo: 1},
	}
	monsterRules := []db.MonsterTracking{
		{ID: "u1", ProfileNo: 1, PokemonID: 25, MinIV: -1, MaxIV: 100, MaxCP: defaultMaxCP, MaxLevel: defaultMaxLevel,
			MaxATK: 15, MaxDEF: 15, MaxSTA: 15, MaxWeight: defaultMaxWeight, Rarity: -1, MaxRarity: 6, Size: -1, MaxSize: 5,
			PVPRankingWorst: 4096},
		{ID: "u1", ProfileNo: 1, PokemonID: 0, MinIV: -1, MaxIV: 100, MaxCP: defaultMaxCP, MaxLevel: defaultMaxLevel,
			MaxATK: 15, MaxDEF: 15, MaxSTA: 15, MaxWeight: defaultMaxWeight, Rarity: -1, MaxRarity: 6, Size: -1, MaxSize: 5,
			PVPRankingWorst: 4096}, // everything
		{ID: "u2", ProfileNo: 1, PokemonID: 25, MinIV: -1, MaxIV: 100, MaxCP: defaultMaxCP, MaxLevel: defaultMaxLevel,
			MaxATK: 15, MaxDEF: 15, MaxSTA: 15, MaxWeight: defaultMaxWeight, Rarity: -1, MaxRarity: 6, Size: -1, MaxSize: 5,
			PVPRankingWorst: 4096},
	}
	// BuildMonsterIndexFromRules populates both ByPokemonID (fallback path) and
	// ByHumanAndLeague (per-human path) from the same backing slice.
	monsters := db.BuildMonsterIndexFromRules(monsterRules)

	spatial := geofence.NewSpatialIndex([]geofence.Fence{
		{Name: "Belgium", DisplayInMatches: true, Path: [][2]float64{{50, 3}, {50, 6}, {51, 6}, {51, 3}, {50, 3}}},
	})

	pokemon := &ProcessedPokemon{
		PokemonID:   25,
		IV:          100,
		CP:          1000,
		Level:       20,
		ATK:         15,
		DEF:         15,
		STA:         15,
		Weight:      6.0,
		Size:        1,
		RarityGroup: 1,
		TTHSeconds:  600,
		Latitude:    50.5,
		Longitude:   4.5,
		Encountered: true,
		PVPBestRank: make(map[int][]pvp.LeagueRank),
		PVPEvoData:  make(map[int]map[int][]pvp.LeagueRank),
	}

	var off, on []webhook.MatchedUser
	for _, flag := range []bool{false, true} {
		st := &state.State{
			Humans:   humans,
			Monsters: monsters,
			Geofence: spatial,
			GeoIndex: state.BuildHumanGeoIndex(humans, nil),
		}
		matcher := &PokemonMatcher{GeographicPrefilter: flag, PVPQueryMaxRank: 100}
		users, _ := matcher.Match(pokemon, st)
		if flag {
			on = users
		} else {
			off = users
		}
	}

	if len(off) != len(on) {
		t.Fatalf("parity violation: flag-off matched %d users, flag-on matched %d", len(off), len(on))
	}
	// Compare by user ID (order may differ).
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

// TestPokemonMatch_GeoPrefilterParityWithPVPEvo extends the basic parity
// check to the PVP-evolution sub-path inside assembleCandidatesPerHuman.
// An Eevee spawn carries PVP evo data for Sylveon (700) in great league;
// the user tracks Sylveon 700 with PVP great league rules. Both the
// per-bucket path (flag off) and the per-human path (flag on) must produce
// the same matched user set.
func TestPokemonMatch_GeoPrefilterParityWithPVPEvo(t *testing.T) {
	humans := map[string]*db.Human{
		"u1": {ID: "u1", Enabled: true, Area: []string{"belgium"}, Latitude: 50.5, Longitude: 4.5, CurrentProfileNo: 1},
		"u2": {ID: "u2", Enabled: true, Area: []string{"belgium"}, Latitude: 50.5, Longitude: 4.5, CurrentProfileNo: 1},
	}

	// u1 tracks Sylveon (700) via great-league PVP — this exercises the
	// PVP evolution branch because the spawn is Eevee (133), not Sylveon.
	// u2 tracks Eevee (133) directly (non-PVP) as a sanity control.
	monsterRules := []db.MonsterTracking{
		{
			ID: "u1", ProfileNo: 1, PokemonID: 700, Form: 0,
			MinIV: -1, MaxIV: 100, MinCP: 0, MaxCP: 9999,
			MinLevel: 0, MaxLevel: 55, ATK: 0, DEF: 0, STA: 0,
			MaxATK: 15, MaxDEF: 15, MaxSTA: 15, Gender: 0,
			MinWeight: 0, MaxWeight: 99999999, Rarity: -1, MaxRarity: 6,
			Size: -1, MaxSize: 5, Template: "1",
			PVPRankingLeague: 1500, PVPRankingBest: 1, PVPRankingWorst: 1000,
			PVPRankingMinCP: 0, PVPRankingCap: 0,
		},
		{
			ID: "u2", ProfileNo: 1, PokemonID: 133, Form: 0,
			MinIV: -1, MaxIV: 100, MaxCP: defaultMaxCP, MaxLevel: defaultMaxLevel,
			MaxATK: 15, MaxDEF: 15, MaxSTA: 15, MaxWeight: defaultMaxWeight,
			Rarity: -1, MaxRarity: 6, Size: -1, MaxSize: 5, Template: "1",
			PVPRankingWorst: 4096,
		},
	}
	monsters := db.BuildMonsterIndexFromRules(monsterRules)

	spatial := geofence.NewSpatialIndex([]geofence.Fence{
		{Name: "Belgium", DisplayInMatches: true, Path: [][2]float64{{50, 3}, {50, 6}, {51, 6}, {51, 3}, {50, 3}}},
	})

	// Eevee (133, form 1092) with PVP evo data for Sylveon (700) rank 512
	// great league — mirrors the real webhook from TestPokemonMatchPVPEvolutionDirectEevee.
	pokemon := &ProcessedPokemon{
		PokemonID: 133, Form: 1092, IV: 44.4, CP: 125, Level: 5,
		ATK: 5, DEF: 9, STA: 6, Weight: 9.308, Size: 3, RarityGroup: 1,
		TTHSeconds: 1580, Latitude: 50.5, Longitude: 4.5, Encountered: true,
		PVPBestRank: map[int][]pvp.LeagueRank{
			1500: {{Rank: 2538, CP: 1052, Caps: []int{50}}},
		},
		PVPEvoData: map[int]map[int][]pvp.LeagueRank{
			134: {1500: {{Rank: 1498, CP: 1479, Caps: []int{50}}}},
			135: {1500: {{Rank: 1780, CP: 1481, Caps: []int{50}}}},
			136: {1500: {{Rank: 1602, CP: 1477, Caps: []int{50}}}},
			196: {1500: {{Rank: 2891, CP: 1464, Caps: []int{50}}}},
			197: {1500: {{Rank: 1474, CP: 1484, Caps: []int{50}}}},
			470: {1500: {{Rank: 2447, CP: 1470, Caps: []int{50}}}},
			471: {1500: {{Rank: 1541, CP: 1484, Caps: []int{50}}}},
			700: {1500: {{Rank: 512, CP: 1496, Caps: []int{50}}}},
		},
	}

	var off, on []webhook.MatchedUser
	for _, flag := range []bool{false, true} {
		st := &state.State{
			Humans:   humans,
			Monsters: monsters,
			Geofence: spatial,
			GeoIndex: state.BuildHumanGeoIndex(humans, nil),
		}
		matcher := &PokemonMatcher{
			GeographicPrefilter:        flag,
			PVPEvolutionDirectTracking: true,
			PVPQueryMaxRank:            4096,
		}
		users, _ := matcher.Match(pokemon, st)
		if flag {
			on = users
		} else {
			off = users
		}
	}

	if len(off) != len(on) {
		t.Fatalf("PVP-evo parity violation: flag-off matched %d users, flag-on matched %d", len(off), len(on))
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
			t.Errorf("PVP-evo parity violation: user %q matched %d times flag-off, %d times flag-on", id, n, seenOn[id])
		}
	}
	// Sanity: both u1 (PVP evo match for Sylveon) and u2 (direct Eevee match)
	// must appear exactly once.
	for _, id := range []string{"u1", "u2"} {
		if seenOff[id] != 1 {
			t.Errorf("expected user %q matched once (flag-off), got %d", id, seenOff[id])
		}
	}
}

// TestPokemonMatch_GeoPrefilterDropsOutOfAreaHuman confirms the expected
// speedup property: when no humans cover the spawn's geography, the
// flag-on path doesn't even enter matchMonsters on the per-pokemon
// bucket — the applicable set is empty.
func TestPokemonMatch_GeoPrefilterDropsOutOfAreaHuman(t *testing.T) {
	humans := map[string]*db.Human{
		"u1": {ID: "u1", Enabled: true, Area: []string{"belgium"}, Latitude: 50.5, Longitude: 4.5, CurrentProfileNo: 1},
	}
	monsterRules := []db.MonsterTracking{
		{ID: "u1", ProfileNo: 1, PokemonID: 25, MinIV: -1, MaxIV: 100, MaxCP: defaultMaxCP, MaxLevel: defaultMaxLevel,
			MaxATK: 15, MaxDEF: 15, MaxSTA: 15, MaxWeight: defaultMaxWeight, Rarity: -1, MaxRarity: 6, Size: -1, MaxSize: 5,
			PVPRankingWorst: 4096},
		{ID: "u1", ProfileNo: 1, PokemonID: 0, MinIV: -1, MaxIV: 100, MaxCP: defaultMaxCP, MaxLevel: defaultMaxLevel,
			MaxATK: 15, MaxDEF: 15, MaxSTA: 15, MaxWeight: defaultMaxWeight, Rarity: -1, MaxRarity: 6, Size: -1, MaxSize: 5,
			PVPRankingWorst: 4096}, // everything
	}
	monsters := db.BuildMonsterIndexFromRules(monsterRules)

	st := &state.State{
		Humans:   humans,
		Monsters: monsters,
		Geofence: geofence.NewSpatialIndex([]geofence.Fence{
			{Name: "Japan", DisplayInMatches: true, Path: [][2]float64{{35, 139}, {35, 140}, {36, 140}, {36, 139}, {35, 139}}},
		}),
		GeoIndex: state.BuildHumanGeoIndex(humans, nil),
	}
	matcher := &PokemonMatcher{GeographicPrefilter: true, PVPQueryMaxRank: 100}
	pokemon := &ProcessedPokemon{
		PokemonID:   25,
		IV:          100,
		CP:          1000,
		Level:       20,
		ATK:         15,
		DEF:         15,
		STA:         15,
		Weight:      6.0,
		Size:        1,
		RarityGroup: 1,
		TTHSeconds:  600,
		Latitude:    35.5,
		Longitude:   139.5,
		Encountered: true,
		PVPBestRank: make(map[int][]pvp.LeagueRank),
		PVPEvoData:  make(map[int]map[int][]pvp.LeagueRank),
	}
	users, _ := matcher.Match(pokemon, st)
	if len(users) != 0 {
		t.Errorf("japan spawn, belgium-only human: expected 0 matches, got %d", len(users))
	}
}

// Combinatorial check: a user with rules across multiple profiles, an
// area selection AND a strict AreaRestriction, all interacting at once.
// This is exactly the combination a geographic pre-filter could silently
// drop rules from if it's implemented wrong; locking in expected
// behaviour here means Phase 2 changes that misbehave fail loudly.
//
// The test runs for both GeographicPrefilter=false and =true to confirm
// parity across the flag. db.BuildMonsterIndexFromRules populates both
// ByPokemonID (flag-off path) and ByHumanAndLeague (flag-on path).
func TestPokemonMatch_MultiProfileWithStrictArea(t *testing.T) {
	// Set up human with multiple areas and a strict restriction
	human := &db.Human{
		ID:               "u1",
		Enabled:          true,
		Area:             []string{"belgium", "antwerp"},
		AreaRestriction:  []string{"belgium"},
		Latitude:         0.5,
		Longitude:        0.5,
		CurrentProfileNo: 2,
	}
	humans := map[string]*db.Human{"u1": human}

	// Create two rules: one for profile 1, one for profile 2.
	// makeMonster returns *MonsterTracking; we need values for BuildMonsterIndexFromRules.
	r1 := *makeMonster("u1", 25)
	r1.ProfileNo = 1 // Profile 1 — should NOT match (wrong profile)

	r2 := *makeMonster("u1", 25)
	r2.ProfileNo = 2 // Profile 2 — should match (correct profile)

	monsterRules := []db.MonsterTracking{r1, r2}
	// BuildMonsterIndexFromRules populates both ByPokemonID (flag-off path) and
	// ByHumanAndLeague (flag-on path) from the same backing slice.
	monsters := db.BuildMonsterIndexFromRules(monsterRules)

	// Create geofence with two areas: "belgium" and "antwerp"
	// Both contain (0, 0) where the pokemon spawns
	fences := []geofence.Fence{
		{
			Name:             "Belgium",
			DisplayInMatches: true,
			Path:             [][2]float64{{-1, -1}, {-1, 1}, {1, 1}, {1, -1}, {-1, -1}},
		},
		{
			Name:             "Antwerp",
			DisplayInMatches: true,
			Path:             [][2]float64{{-0.5, -0.5}, {-0.5, 0.5}, {0.5, 0.5}, {0.5, -0.5}, {-0.5, -0.5}},
		},
	}
	geoIndex := geofence.NewSpatialIndex(fences)

	// Pokemon at (0, 0) — in both Belgium and Antwerp geofences
	pokemon := &ProcessedPokemon{
		PokemonID:   25,
		Form:        0,
		IV:          100,
		CP:          1000,
		Level:       20,
		ATK:         15,
		DEF:         15,
		STA:         15,
		Weight:      6.0,
		Size:        1,
		RarityGroup: 1,
		TTHSeconds:  600,
		Latitude:    0,
		Longitude:   0,
		Encountered: true,
		PVPBestRank: make(map[int][]pvp.LeagueRank),
		PVPEvoData:  make(map[int]map[int][]pvp.LeagueRank),
	}

	for _, flag := range []bool{false, true} {
		st := &state.State{
			Humans:   humans,
			Monsters: monsters,
			Geofence: geoIndex,
			Fences:   fences,
			GeoIndex: state.BuildHumanGeoIndex(humans, nil),
		}

		matcher := &PokemonMatcher{
			StrictLocations:     true,
			AreaSecurityEnabled: true,
			GeographicPrefilter: flag,
		}
		users, _ := matcher.Match(pokemon, st)

		// Expected: exactly 1 matched user (profile 2 only)
		// Profile 1 rule should be filtered by CurrentProfileNo check
		if len(users) != 1 {
			t.Errorf("GeographicPrefilter=%v: expected exactly 1 matched user (profile 2 only), got %d", flag, len(users))
			continue
		}
		if users[0].ID != "u1" {
			t.Errorf("GeographicPrefilter=%v: matched ID = %q, want u1", flag, users[0].ID)
		}
	}
}

// TestPokemonMatch_GeoPrefilterParityWithDistanceOnly exercises the rtree
// distance path in HumanGeoIndex.ApplicableHumans. The existing parity
// tests all seed humans with non-empty Area lists, so they hit the
// byArea bucket path and never reach the rtree. This test sets a human
// with NO Area selection but a non-zero rule Distance, forcing
// applicability to be resolved by the rtree-based distance check.
//
// Flag-off and flag-on must produce the same matched users.
func TestPokemonMatch_GeoPrefilterParityWithDistanceOnly(t *testing.T) {
	// Human lives at (50.5, 4.5), tracks NO areas (Area is empty),
	// but has a distance-based rule with d=10000m.
	humans := map[string]*db.Human{
		"u1": {
			ID:               "u1",
			Enabled:          true,
			Area:             nil,
			Latitude:         50.5,
			Longitude:        4.5,
			CurrentProfileNo: 1,
		},
	}
	monsterRules := []db.MonsterTracking{
		{ID: "u1", ProfileNo: 1, PokemonID: 25, MinIV: -1, MaxIV: 100, MaxCP: defaultMaxCP, MaxLevel: defaultMaxLevel,
			MaxATK: 15, MaxDEF: 15, MaxSTA: 15, MaxWeight: defaultMaxWeight, Rarity: -1, MaxRarity: 6, Size: -1, MaxSize: 5,
			PVPRankingWorst: 4096, Distance: 10000},
	}
	monsters := db.BuildMonsterIndexFromRules(monsterRules)

	spatial := geofence.NewSpatialIndex([]geofence.Fence{
		// Geofence is unused by this human (no Area selection), but
		// ValidateHumans's distance check uses the spawn coords directly.
		{Name: "Belgium", Path: [][2]float64{{3, 50}, {3, 51}, {6, 51}, {6, 50}, {3, 50}}},
	})

	// PerHumanMaxDistance returns u1→10000, so the geo index builds
	// an rtree entry for u1 at (50.5, 4.5) with a ~10km bbox.
	perHumanMaxDist := map[string]int{"u1": 10000}

	// Spawn at (50.5005, 4.5005) — ~70m from the human, well inside d=10km.
	pokemon := &ProcessedPokemon{
		PokemonID:   25,
		IV:          100,
		CP:          1000,
		Level:       20,
		ATK:         15,
		DEF:         15,
		STA:         15,
		Weight:      6.0,
		Size:        1,
		RarityGroup: 1,
		TTHSeconds:  600,
		Latitude:    50.5005,
		Longitude:   4.5005,
		Encountered: true,
		PVPBestRank: make(map[int][]pvp.LeagueRank),
		PVPEvoData:  make(map[int]map[int][]pvp.LeagueRank),
	}

	var off, on []webhook.MatchedUser
	for _, flag := range []bool{false, true} {
		st := &state.State{
			Humans:   humans,
			Monsters: monsters,
			Geofence: spatial,
			GeoIndex: state.BuildHumanGeoIndex(humans, perHumanMaxDist),
		}
		matcher := &PokemonMatcher{GeographicPrefilter: flag, PVPQueryMaxRank: 100}
		users, _ := matcher.Match(pokemon, st)
		if flag {
			on = users
		} else {
			off = users
		}
	}

	if len(off) != len(on) {
		t.Fatalf("parity violation: flag-off matched %d users, flag-on matched %d", len(off), len(on))
	}
	if len(off) == 0 {
		t.Fatalf("expected at least 1 matched user (distance-only spawn within 10km), got 0 on both paths — test setup is wrong")
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

// TestPokemonMatch_GeoPrefilterDistanceOutOfRange confirms the rtree
// correctly excludes a distance-tracking human whose bbox does not
// contain the spawn. With flag on, the empty applicable-set means
// matchMonsters is never called for the rule, and no users match.
func TestPokemonMatch_GeoPrefilterDistanceOutOfRange(t *testing.T) {
	humans := map[string]*db.Human{
		"u1": {
			ID:               "u1",
			Enabled:          true,
			Area:             nil,
			Latitude:         50.5,
			Longitude:        4.5,
			CurrentProfileNo: 1,
		},
	}
	monsterRules := []db.MonsterTracking{
		{ID: "u1", ProfileNo: 1, PokemonID: 25, MinIV: -1, MaxIV: 100, MaxCP: defaultMaxCP, MaxLevel: defaultMaxLevel,
			MaxATK: 15, MaxDEF: 15, MaxSTA: 15, MaxWeight: defaultMaxWeight, Rarity: -1, MaxRarity: 6, Size: -1, MaxSize: 5,
			PVPRankingWorst: 4096, Distance: 1000},
	}
	monsters := db.BuildMonsterIndexFromRules(monsterRules)
	perHumanMaxDist := map[string]int{"u1": 1000}

	// Spawn at (10, 10) — thousands of km from the human. Way outside
	// the rtree bbox.
	pokemon := &ProcessedPokemon{
		PokemonID:   25,
		IV:          100,
		CP:          1000,
		Level:       20,
		ATK:         15,
		DEF:         15,
		STA:         15,
		Weight:      6.0,
		Size:        1,
		RarityGroup: 1,
		TTHSeconds:  600,
		Latitude:    10,
		Longitude:   10,
		Encountered: true,
		PVPBestRank: make(map[int][]pvp.LeagueRank),
		PVPEvoData:  make(map[int]map[int][]pvp.LeagueRank),
	}

	st := &state.State{
		Humans:   humans,
		Monsters: monsters,
		Geofence: geofence.NewSpatialIndex([]geofence.Fence{}),
		GeoIndex: state.BuildHumanGeoIndex(humans, perHumanMaxDist),
	}
	matcher := &PokemonMatcher{GeographicPrefilter: true, PVPQueryMaxRank: 100}
	users, _ := matcher.Match(pokemon, st)
	if len(users) != 0 {
		t.Errorf("expected 0 matches (spawn far outside u1's 1km circle), got %d", len(users))
	}
}
