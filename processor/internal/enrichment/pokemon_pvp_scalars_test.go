package enrichment

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/pvp"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// stubWeather satisfies WeatherProvider with zero-value responses.
type stubWeather struct{}

func (stubWeather) GetCurrentWeatherInCell(_ string) int { return 0 }
func (stubWeather) GetWeatherForecast(_ string) tracker.WeatherForecast {
	return tracker.WeatherForecast{}
}

// TestBestLeagueRankScalarsBaseOnly asserts that when PVPBestRank for league 1500
// contains both a base-form entry (Evolution==0, rank 50) and a mega entry
// (Evolution==2, rank 1), bestGreatLeagueRank resolves to 50 — NOT 1.
// Regression for commit 48de8388 which started including mega entries in BestRank.
func TestBestLeagueRankScalarsBaseOnly(t *testing.T) {
	e := &Enricher{
		WeatherProvider: stubWeather{},
	}

	processed := &matching.ProcessedPokemon{
		PVPBestRank: map[int][]pvp.LeagueRank{
			1500: {
				{Rank: 50, CP: 1450, Evolution: 0, Caps: []int{50}}, // base form
				{Rank: 1, CP: 2900, Evolution: 2, Caps: []int{50}},  // Mega X — must not win
			},
		},
	}

	m, _ := e.Pokemon(&webhook.PokemonWebhook{}, processed, 0)

	got, ok := m["bestGreatLeagueRank"]
	if !ok {
		t.Fatal("bestGreatLeagueRank missing from enrichment map")
	}
	if got != 50 {
		t.Errorf("bestGreatLeagueRank = %v, want 50 (mega rank 1 must not override base rank 50)", got)
	}

	gotCP, ok := m["bestGreatLeagueRankCP"]
	if !ok {
		t.Fatal("bestGreatLeagueRankCP missing from enrichment map")
	}
	if gotCP != 1450 {
		t.Errorf("bestGreatLeagueRankCP = %v, want 1450", gotCP)
	}
}

// TestBestLeagueRankScalarsMegaOnly verifies that when only mega entries exist
// in BestRank (no base entry), the scalar falls through to the initial 4096 sentinel
// — i.e. the league bucket is set but shows "no base match".
func TestBestLeagueRankScalarsMegaOnlyFallsThrough(t *testing.T) {
	e := &Enricher{
		WeatherProvider: stubWeather{},
	}

	processed := &matching.ProcessedPokemon{
		PVPBestRank: map[int][]pvp.LeagueRank{
			1500: {
				{Rank: 1, CP: 2900, Evolution: 1, Caps: []int{50}}, // Mega only
			},
		},
	}

	m, _ := e.Pokemon(&webhook.PokemonWebhook{}, processed, 0)

	// With no base entry, bestRank stays at the initial 4096 sentinel.
	got, ok := m["bestGreatLeagueRank"]
	if !ok {
		t.Fatal("bestGreatLeagueRank missing from enrichment map")
	}
	if got != 4096 {
		t.Errorf("bestGreatLeagueRank = %v, want 4096 (no base entry, sentinel expected)", got)
	}
}
