package matching

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/pvp"
	"github.com/pokemon/poracleng/processor/internal/state"
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

	matched := matcher.Match(pokemon, st)
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

	matched := matcher.Match(pokemon, st)
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

	matched := matcher.Match(pokemon, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for low IV, got %d", len(matched))
	}

	// High IV - should match
	pokemon.IV = 95
	pokemon.ATK = 14
	pokemon.DEF = 14
	pokemon.STA = 15
	matched = matcher.Match(pokemon, st)
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

	matched := matcher.Match(pokemon, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for wrong form, got %d", len(matched))
	}

	// Correct form
	pokemon.Form = 5
	matched = matcher.Match(pokemon, st)
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

	matched := matcher.Match(pokemon, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for female when tracking male, got %d", len(matched))
	}

	pokemon.Gender = 1
	matched = matcher.Match(pokemon, st)
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

	matched := matcher.Match(pokemon, st)
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
		name    string
		modify  func(m *db.MonsterTracking)
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

			matched := matcher.Match(pokemon, st)
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

	matched := matcher.Match(pokemon, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for distant pokemon, got %d", len(matched))
	}

	// Close - should match
	pokemon.Latitude = 51.5005
	matched = matcher.Match(pokemon, st)
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

	matched := matcher.Match(pokemon, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for weight in range, got %d", len(matched))
	}

	pokemon.Weight = 15.0 // Too heavy
	matched = matcher.Match(pokemon, st)
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

	matched := matcher.Match(pokemon, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 PVP match, got %d", len(matched))
	}

	// Rank too high
	pokemon.PVPBestRank[1500] = []pvp.LeagueRank{{Rank: 100, CP: 1490, Caps: []int{50}}}
	matched = matcher.Match(pokemon, st)
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

	matched := matcher.Match(pokemon, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for PVP cap 50, got %d", len(matched))
	}

	// Pokemon only has cap 51 — should not match cap 50 tracking
	pokemon.PVPBestRank[1500] = []pvp.LeagueRank{{Rank: 10, CP: 1490, Caps: []int{51}}}
	matched = matcher.Match(pokemon, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for PVP cap 51 when tracking cap 50, got %d", len(matched))
	}

	// Tracking rule with cap 0 (any) — should match any caps
	monster.PVPRankingCap = 0
	st = makeTestState([]*db.MonsterTracking{monster}, map[string]*db.Human{"user1": human})
	pokemon.PVPBestRank[1500] = []pvp.LeagueRank{{Rank: 10, CP: 1490, Caps: []int{51}}}
	matched = matcher.Match(pokemon, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 match for PVP cap 0 (any), got %d", len(matched))
	}

	// Pokemon has empty caps with cap filter set — should not filter (caps check skipped when empty)
	monster.PVPRankingCap = 50
	st = makeTestState([]*db.MonsterTracking{monster}, map[string]*db.Human{"user1": human})
	pokemon.PVPBestRank[1500] = []pvp.LeagueRank{{Rank: 10, CP: 1490, Caps: nil}}
	matched = matcher.Match(pokemon, st)
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

	matched := matcher.Match(pokemon, st)
	if len(matched) != 1 {
		t.Errorf("Expected 1 PVP evolution direct match for Scolipede, got %d", len(matched))
	}
	if len(matched) == 1 && matched[0].PokemonID != 545 {
		t.Errorf("Expected matched PokemonID 545 (Scolipede), got %d", matched[0].PokemonID)
	}

	// With evolution direct tracking disabled, should not match
	matcher2 := &PokemonMatcher{PVPQueryMaxRank: 100, PVPEvolutionDirectTracking: false}
	matched = matcher2.Match(pokemon, st)
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

	matched := matcher.Match(pokemon, st)
	if len(matched) != 1 {
		t.Fatalf("Expected 1 PVP evolution match for Sylveon via Eevee, got %d", len(matched))
	}
	if matched[0].PokemonID != 700 {
		t.Errorf("Expected matched PokemonID 700 (Sylveon), got %d", matched[0].PokemonID)
	}

	// With evolution direct tracking disabled, should not match
	matcher2 := &PokemonMatcher{PVPQueryMaxRank: 4096, PVPEvolutionDirectTracking: false}
	matched2 := matcher2.Match(pokemon, st)
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

	matched3 := matcher.Match(pokemon, st2)
	if len(matched3) != 1 {
		t.Fatalf("Expected 1 PVP evolution match for Umbreon via Eevee in ultra, got %d", len(matched3))
	}
	if matched3[0].PokemonID != 197 {
		t.Errorf("Expected matched PokemonID 197 (Umbreon), got %d", matched3[0].PokemonID)
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

	matched := matcher.Match(pokemon, st)
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

	matched := matcher.Match(pokemon, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for low TTH, got %d", len(matched))
	}

	pokemon.TTHSeconds = 600
	matched = matcher.Match(pokemon, st)
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

	matched := matcher.Match(pokemon, st)
	if len(matched) != 0 {
		t.Errorf("Expected 0 matches for wrong profile, got %d", len(matched))
	}
}
