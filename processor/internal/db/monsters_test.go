package db

import "testing"

func TestBuildMonsterIndexFromRules(t *testing.T) {
	rules := []MonsterTracking{
		{ID: "u1", PokemonID: 25, PVPRankingLeague: 0},   // per-species non-PVP
		{ID: "u1", PokemonID: 0, PVPRankingLeague: 0},    // catch-all non-PVP
		{ID: "u1", PokemonID: 6, PVPRankingLeague: 1500}, // PVP per-species (great)
		{ID: "u2", PokemonID: 0, PVPRankingLeague: 1500}, // PVP catch-all (great)
		{ID: "u2", PokemonID: 0, PVPRankingLeague: 2500}, // PVP catch-all (ultra)
	}

	idx := BuildMonsterIndexFromRules(rules)

	if idx.Total != 5 {
		t.Errorf("Total = %d, want 5", idx.Total)
	}

	// ByPokemonID: entries with PVPRankingLeague==0
	if len(idx.ByPokemonID[25]) != 1 {
		t.Errorf("ByPokemonID[25] = %d, want 1", len(idx.ByPokemonID[25]))
	}
	if len(idx.ByPokemonID[0]) != 1 {
		t.Errorf("ByPokemonID[0] = %d, want 1", len(idx.ByPokemonID[0]))
	}

	// PVPSpecific: PVP entries with specific pokemon_id
	if len(idx.PVPSpecific[1500]) != 1 {
		t.Errorf("PVPSpecific[1500] = %d, want 1", len(idx.PVPSpecific[1500]))
	}
	if idx.PVPSpecific[1500][0].PokemonID != 6 {
		t.Errorf("PVPSpecific[1500][0].PokemonID = %d, want 6", idx.PVPSpecific[1500][0].PokemonID)
	}

	// PVPEverything: PVP entries with pokemon_id==0
	if len(idx.PVPEverything[1500]) != 1 {
		t.Errorf("PVPEverything[1500] = %d, want 1", len(idx.PVPEverything[1500]))
	}
	if len(idx.PVPEverything[2500]) != 1 {
		t.Errorf("PVPEverything[2500] = %d, want 1", len(idx.PVPEverything[2500]))
	}

	// Pointer identity: entries in the index should point into the original slice
	if idx.ByPokemonID[25][0] != &rules[0] {
		t.Errorf("ByPokemonID[25][0] does not share pointer identity with rules[0]")
	}
	if idx.ByPokemonID[0][0] != &rules[1] {
		t.Errorf("ByPokemonID[0][0] does not share pointer identity with rules[1]")
	}
}

func TestBuildMonsterIndex_CarriesEvolution(t *testing.T) {
	rules := []MonsterTracking{
		{ID: "u1", PokemonID: 6, PVPRankingLeague: 1500, PVPRankingEvolution: 2},
	}
	idx := BuildMonsterIndexFromRules(rules)
	got := idx.PVPSpecific[1500]
	if len(got) != 1 || got[0].PVPRankingEvolution != 2 {
		t.Fatalf("expected one PVP rule with PVPRankingEvolution=2, got %#v", got)
	}
}

func TestBuildMonsterIndexFromRulesEmpty(t *testing.T) {
	idx := BuildMonsterIndexFromRules(nil)
	if idx == nil {
		t.Fatal("expected non-nil index for empty input")
	}
	if idx.Total != 0 {
		t.Errorf("Total = %d, want 0", idx.Total)
	}
	if idx.ByPokemonID == nil {
		t.Errorf("ByPokemonID should not be nil")
	}
	if idx.PVPSpecific == nil {
		t.Errorf("PVPSpecific should not be nil")
	}
	if idx.PVPEverything == nil {
		t.Errorf("PVPEverything should not be nil")
	}
}

// pvp_ranking_worst 0 is a legacy "no upper limit", not a rank: left as 0 the
// matcher's rank > worst check would make a PVP rule match nothing.
func TestBuildMonsterIndex_WorstZeroMeansNoLimit(t *testing.T) {
	idx := BuildMonsterIndexFromRules([]MonsterTracking{
		{ID: "u1", PokemonID: 6, PVPRankingLeague: 1500, PVPRankingWorst: 0},
		{ID: "u1", PokemonID: 6, PVPRankingLeague: 1500, PVPRankingWorst: 100},
	})
	got := idx.PVPSpecific[1500]
	if got[0].PVPRankingWorst != 4096 {
		t.Errorf("worst 0 loaded as %d, want 4096", got[0].PVPRankingWorst)
	}
	if got[1].PVPRankingWorst != 100 {
		t.Errorf("explicit worst loaded as %d, want 100", got[1].PVPRankingWorst)
	}
}
