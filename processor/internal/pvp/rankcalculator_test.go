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
	pokemon := &webhook.PokemonWebhook{
		PokemonID: 25,
		Form:      0,
		PVP: map[string][]webhook.PVPRankEntry{
			"great": {
				{Pokemon: 25, Form: 0, Rank: 10, CP: 1490, Cap: 50, Evolution: 0},
				{Pokemon: 10025, Form: 0, Rank: 5, CP: 1480, Cap: 50, Evolution: 1}, // Mega
			},
		},
	}

	cfg := &Config{
		LevelCaps:            []int{50},
		PVPFilterMaxRank:     100,
		IncludeMegaEvolution: false,
	}

	result := Calculate(pokemon, cfg)
	bestRanks := result.BestRank[1500]

	// Should only have rank 10 (mega filtered out)
	for _, r := range bestRanks {
		if r.Rank == 5 {
			t.Error("Expected mega evolution to be filtered out")
		}
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

func TestCapsContain(t *testing.T) {
	caps := []int{40, 50, 51}

	if !CapsContain(caps, 50) {
		t.Error("Expected caps to contain 50")
	}
	if CapsContain(caps, 45) {
		t.Error("Expected caps to not contain 45")
	}
}
