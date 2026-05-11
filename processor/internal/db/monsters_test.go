package db

import "testing"

func TestMonsterIndex_ByHumanAndLeague_PartitionsCorrectly(t *testing.T) {
	rules := []MonsterTracking{
		{ID: "u1", PokemonID: 25, PVPRankingLeague: 0},    // non-PVP per-species
		{ID: "u1", PokemonID: 0, PVPRankingLeague: 0},     // non-PVP everything
		{ID: "u1", PokemonID: 6, PVPRankingLeague: 1500},  // PVP per-species (great)
		{ID: "u2", PokemonID: 0, PVPRankingLeague: 1500},  // PVP everything (great)
	}
	idx := buildMonsterIndexFromRules(rules)

	u1NonPVP := idx.ByHumanAndLeague["u1"][0]
	if len(u1NonPVP) != 2 {
		t.Errorf("u1 non-PVP rules = %d, want 2", len(u1NonPVP))
	}
	u1Great := idx.ByHumanAndLeague["u1"][1500]
	if len(u1Great) != 1 {
		t.Errorf("u1 great-league rules = %d, want 1", len(u1Great))
	}
	u2Great := idx.ByHumanAndLeague["u2"][1500]
	if len(u2Great) != 1 {
		t.Errorf("u2 great-league rules = %d, want 1", len(u2Great))
	}
	// Pointer identity: per-human entries point at the same MonsterTracking
	// elements as ByPokemonID.
	if u1NonPVP[0] != idx.ByPokemonID[25][0] && u1NonPVP[0] != idx.ByPokemonID[0][0] {
		t.Errorf("u1's non-PVP rule should share pointer identity with ByPokemonID entry")
	}
}
