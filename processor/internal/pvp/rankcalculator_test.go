package pvp

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/webhook"
)

func TestCalculateBasic(t *testing.T) {
	pokemon := &webhook.PokemonWebhook{
		PokemonID: 25,
		Form:      0,
		PVP: map[string][]webhook.PVPRankEntry{
			"great": {
				{Pokemon: 25, Form: 0, Rank: 10, CP: 1490, Cap: 50, Capped: false},
				{Pokemon: 26, Form: 0, Rank: 5, CP: 1480, Cap: 50, Capped: false}, // Evolution
			},
		},
	}

	cfg := &Config{
		LevelCaps:                  []int{50},
		PVPFilterMaxRank:           100,
		PVPEvolutionDirectTracking: true,
		PVPFilterGreatMinCP:        1400,
	}

	result := Calculate(pokemon, cfg)

	// Should have great league results
	if _, ok := result.BestRank[1500]; !ok {
		t.Fatal("Expected great league (1500) results")
	}

	bestRanks := result.BestRank[1500]
	if len(bestRanks) == 0 {
		t.Fatal("Expected at least one best rank entry")
	}

	// Best rank tracks the minimum rank across all entries (including evolutions)
	// So best should be 5 (from the evolution entry for pokemon 26)
	found := false
	for _, r := range bestRanks {
		if r.Rank == 5 {
			found = true
		}
	}
	if !found {
		t.Errorf("Expected rank 5 in best ranks, got %+v", bestRanks)
	}

	// Should have evolution data for pokemon 26
	if evoData, ok := result.EvolutionData[26]; ok {
		if leagueData, ok := evoData[1500]; ok {
			if len(leagueData) == 0 {
				t.Error("Expected evolution data for pokemon 26")
			}
		} else {
			t.Error("Expected league 1500 evolution data for pokemon 26")
		}
	} else {
		t.Error("Expected evolution data for pokemon 26")
	}
}

// TestCalculate_EvolutionDataIncludesMega ensures cross-species evolution
// direct tracking captures mega/temporary-evolution entries (tagged with their
// Evolution), so a `mega` rule on the evolved species can fire on a
// pre-evolution. A Charmander (4) spawns; its great-league PVP lists the evolved
// Charizard (6) as base (evo 0) and Mega X (evo 2).
func TestCalculate_EvolutionDataIncludesMega(t *testing.T) {
	pokemon := &webhook.PokemonWebhook{
		PokemonID: 4,
		Form:      0,
		PVP: map[string][]webhook.PVPRankEntry{
			"great": {
				{Pokemon: 6, Form: 178, Rank: 50, CP: 1500, Cap: 50, Capped: true, Evolution: 0}, // base Charizard
				{Pokemon: 6, Form: 178, Rank: 3, CP: 1480, Cap: 50, Capped: true, Evolution: 2},  // Mega Charizard X
			},
		},
	}
	cfg := &Config{LevelCaps: []int{50}, PVPFilterMaxRank: 100, PVPEvolutionDirectTracking: true}
	result := Calculate(pokemon, cfg)

	evo := result.EvolutionData[6][1500]
	got := map[int]int{} // evolution -> rank
	for _, r := range evo {
		got[r.Evolution] = r.Rank
	}
	if got[0] != 50 {
		t.Errorf("expected base Charizard (evo 0) rank 50 in EvolutionData[6]; got %#v", got)
	}
	if got[2] != 3 {
		t.Errorf("expected Mega Charizard X (evo 2) rank 3 in EvolutionData[6]; got %#v", got)
	}
}

func TestCalculateMultipleCaps(t *testing.T) {
	pokemon := &webhook.PokemonWebhook{
		PokemonID: 25,
		Form:      0,
		PVP: map[string][]webhook.PVPRankEntry{
			"great": {
				{Pokemon: 25, Form: 0, Rank: 10, CP: 1490, Cap: 40, Capped: true},
				{Pokemon: 25, Form: 0, Rank: 15, CP: 1485, Cap: 50, Capped: false},
			},
		},
	}

	cfg := &Config{
		LevelCaps:        []int{40, 50},
		PVPFilterMaxRank: 100,
	}

	result := Calculate(pokemon, cfg)
	bestRanks := result.BestRank[1500]

	// Should have entries for the two caps
	if len(bestRanks) == 0 {
		t.Fatal("Expected best rank entries")
	}

	// Cap 40 and 50 should both have rank 10 (since capped=true means all caps >= 40 get that rank)
	for _, r := range bestRanks {
		if r.Rank == 10 {
			if len(r.Caps) == 0 {
				t.Error("Expected caps for rank 10")
			}
		}
	}
}

func TestCalculateNoMegaFilter(t *testing.T) {
	// Mega entries are no longer dropped; both base and mega appear as separate evolution-tagged entries.
	pokemon := &webhook.PokemonWebhook{
		PokemonID: 25,
		Form:      0,
		PVP: map[string][]webhook.PVPRankEntry{
			"great": {
				{Pokemon: 25, Form: 0, Rank: 10, CP: 1490, Cap: 50, Capped: true, Evolution: 0},
				{Pokemon: 25, Form: 0, Rank: 5, CP: 1480, Cap: 50, Capped: true, Evolution: 1}, // Mega
			},
		},
	}

	cfg := &Config{
		LevelCaps:        []int{50},
		PVPFilterMaxRank: 100,
	}

	result := Calculate(pokemon, cfg)
	bestRanks := result.BestRank[1500]

	// Both base (evolution=0, rank=10) and mega (evolution=1, rank=5) should appear.
	foundBase := false
	foundMega := false
	for _, r := range bestRanks {
		if r.Evolution == 0 && r.Rank == 10 {
			foundBase = true
		}
		if r.Evolution == 1 && r.Rank == 5 {
			foundMega = true
		}
	}
	if !foundBase {
		t.Errorf("Expected base entry (evolution=0, rank=10) in best ranks, got %+v", bestRanks)
	}
	if !foundMega {
		t.Errorf("Expected mega entry (evolution=1, rank=5) in best ranks, got %+v", bestRanks)
	}
}

func TestCalculateNilPVP(t *testing.T) {
	pokemon := &webhook.PokemonWebhook{PokemonID: 25}
	cfg := &Config{LevelCaps: []int{50}, PVPFilterMaxRank: 100}
	result := Calculate(pokemon, cfg)
	if len(result.BestRank) != 0 {
		t.Errorf("Expected empty BestRank for nil PVP, got %d entries", len(result.BestRank))
	}
	if len(result.EvolutionData) != 0 {
		t.Errorf("Expected empty EvolutionData for nil PVP, got %d entries", len(result.EvolutionData))
	}
}

func TestCalculateSentinelFiltered(t *testing.T) {
	// Cap 41 has no matching PVP data — should not appear in output
	pokemon := &webhook.PokemonWebhook{
		PokemonID: 25,
		PVP: map[string][]webhook.PVPRankEntry{
			"great": {
				{Pokemon: 25, Form: 0, Rank: 10, CP: 1490, Cap: 50, Capped: false},
			},
		},
	}

	cfg := &Config{LevelCaps: []int{41, 50}, PVPFilterMaxRank: 100}
	result := Calculate(pokemon, cfg)
	bestRanks := result.BestRank[1500]

	for _, r := range bestRanks {
		if r.Rank >= 4096 {
			t.Errorf("Sentinel rank 4096 should be filtered from output, got %+v", r)
		}
		for _, c := range r.Caps {
			if c == 41 {
				t.Errorf("Cap 41 with no data should not appear, got %+v", r)
			}
		}
	}
}

func TestCalculateEvoCapsNotOhbem(t *testing.T) {
	// When Cap==0 && !Capped (not ohbem), evo caps should be nil (match any cap)
	// This matches JS behavior: caps = null → bypasses cap filter in matcher
	pokemon := &webhook.PokemonWebhook{
		PokemonID: 25,
		PVP: map[string][]webhook.PVPRankEntry{
			"great": {
				{Pokemon: 26, Form: 0, Rank: 5, CP: 1480, Cap: 0, Capped: false},
			},
		},
	}

	cfg := &Config{
		LevelCaps:                  []int{50},
		PVPFilterMaxRank:           100,
		PVPEvolutionDirectTracking: true,
		PVPFilterGreatMinCP:        0,
	}

	result := Calculate(pokemon, cfg)
	evoData, ok := result.EvolutionData[26]
	if !ok {
		t.Fatal("Expected evolution data for pokemon 26")
	}
	leagueData, ok := evoData[1500]
	if !ok || len(leagueData) == 0 {
		t.Fatal("Expected league 1500 evolution data for pokemon 26")
	}
	// Caps should be nil (matches any cap, same as JS null)
	if leagueData[0].Caps != nil {
		t.Errorf("Expected nil caps for not-ohbem evo entry, got %v", leagueData[0].Caps)
	}
}

func TestCalculateEvoCapsExplicit(t *testing.T) {
	// When Cap is explicitly set, evo caps should contain only that cap
	pokemon := &webhook.PokemonWebhook{
		PokemonID: 25,
		PVP: map[string][]webhook.PVPRankEntry{
			"great": {
				{Pokemon: 26, Form: 0, Rank: 5, CP: 1480, Cap: 50, Capped: false},
			},
		},
	}

	cfg := &Config{
		LevelCaps:                  []int{40, 50},
		PVPFilterMaxRank:           100,
		PVPEvolutionDirectTracking: true,
		PVPFilterGreatMinCP:        0,
	}

	result := Calculate(pokemon, cfg)
	leagueData := result.EvolutionData[26][1500]
	if len(leagueData) == 0 {
		t.Fatal("Expected evolution data")
	}
	if len(leagueData[0].Caps) != 1 || leagueData[0].Caps[0] != 50 {
		t.Errorf("Expected caps=[50] for explicit cap 50, got %v", leagueData[0].Caps)
	}
}

func TestCalculate_KeepsMegaEntriesTaggedByEvolution(t *testing.T) {
	pokemon := &webhook.PokemonWebhook{
		PokemonID: 6,
		PVP: map[string][]webhook.PVPRankEntry{
			"great": {
				{Pokemon: 6, Form: 178, Rank: 10, CP: 1490, Cap: 50, Capped: true, Evolution: 0},
				{Pokemon: 6, Form: 178, Rank: 5, CP: 1480, Cap: 50, Capped: true, Evolution: 2},
				{Pokemon: 6, Form: 178, Rank: 3, CP: 1470, Cap: 50, Capped: true, Evolution: 3},
			},
		},
	}
	cfg := &Config{LevelCaps: []int{50}}
	result := Calculate(pokemon, cfg)

	got := map[int]int{} // evolution -> rank
	for _, lr := range result.BestRank[1500] {
		got[lr.Evolution] = lr.Rank
	}
	if got[0] != 10 || got[2] != 5 || got[3] != 3 {
		t.Fatalf("expected base=10, megaX=5, megaY=3 tagged by evolution; got %#v", got)
	}
}

func TestCapsContain(t *testing.T) {
	caps := []int{40, 50, 51}

	if !CapsContain(caps, 50) {
		t.Error("Expected caps to contain 50")
	}
	if CapsContain(caps, 45) {
		t.Error("Expected caps to not contain 45")
	}
}
