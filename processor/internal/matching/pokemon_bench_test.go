package matching

import (
	"fmt"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/geofence"
	"github.com/pokemon/poracleng/processor/internal/pvp"
	"github.com/pokemon/poracleng/processor/internal/state"
)

// Benchmark workload parameters — tweak to simulate different install sizes.
const (
	benchHumans        = 1000
	benchRulesPerHuman = 500 // 1000 × 500 ≈ 500 k rules total
)

// buildLargeBenchState synthesises a realistic "large install" workload and
// returns the humans map plus monster index ready for use in a benchmark.
//
// Rule distribution per human (500 rules, 4 buckets × 125):
//
//	25 % — everything, IV 0+ (catch-all, no stat filter)
//	25 % — everything, IV 90+ (encounter-only stat filter)
//	25 % — everything, PVP great-league top-100
//	25 % — per-species (pokemon IDs 1–100, round-robin)
//
// Each human subscribes to three area_N fences so geofence overlap is
// realistic and different humans share areas.
func buildLargeBenchState(b *testing.B) (map[string]*db.Human, *db.MonsterIndex) {
	b.Helper()

	humans := make(map[string]*db.Human, benchHumans)
	rules := make([]db.MonsterTracking, 0, benchHumans*benchRulesPerHuman)

	for i := 0; i < benchHumans; i++ {
		id := fmt.Sprintf("u%d", i)

		// Each human covers three consecutive area buckets (mod 100).
		// Area names use spaces (not underscores) to match the NormalizedName
		// that NewSpatialIndex computes (strings.ReplaceAll(name, "_", " ")).
		areas := []string{
			fmt.Sprintf("area %d", i%100),
			fmt.Sprintf("area %d", (i+1)%100),
			fmt.Sprintf("area %d", (i+2)%100),
		}

		humans[id] = &db.Human{
			ID:               id,
			Enabled:          true,
			Area:             areas,
			Latitude:         50.0,
			Longitude:        4.0,
			CurrentProfileNo: 1,
		}

		bucketSize := benchRulesPerHuman / 4

		for j := 0; j < benchRulesPerHuman; j++ {
			var r db.MonsterTracking
			r.ID = id
			r.ProfileNo = 1
			// Common non-stat defaults
			r.MaxIV = 100
			r.MaxCP = defaultMaxCP
			r.MaxLevel = defaultMaxLevel
			r.MaxATK = 15
			r.MaxDEF = 15
			r.MaxSTA = 15
			r.MaxWeight = defaultMaxWeight
			r.Rarity = -1
			r.MaxRarity = 6
			r.Size = -1
			r.MaxSize = 5
			r.PVPRankingWorst = 4096

			switch {
			case j < bucketSize:
				// Bucket 0: everything IV 0+
				r.PokemonID = 0
				r.MinIV = 0

			case j < bucketSize*2:
				// Bucket 1: everything IV 90+
				r.PokemonID = 0
				r.MinIV = 90

			case j < bucketSize*3:
				// Bucket 2: everything PVP great-league top-100
				r.PokemonID = 0
				r.PVPRankingLeague = 1500
				r.PVPRankingBest = 1
				r.PVPRankingWorst = 100
				r.PVPRankingMinCP = 1400

			default:
				// Bucket 3: per-species, rotate IDs 1–100
				r.PokemonID = 1 + (j % 100)
			}

			rules = append(rules, r)
		}
	}

	monsters := db.BuildMonsterIndexFromRules(rules)
	return humans, monsters
}

// BenchmarkPokemonMatch_LargeRuleSet measures PokemonMatcher.Match against a
// ~500 k-rule workload with and without the geographic pre-filter.
//
// Run with:
//
//	cd processor && go test -bench BenchmarkPokemonMatch -benchmem -count=1 -run=^$ ./internal/matching/...
func BenchmarkPokemonMatch_LargeRuleSet(b *testing.B) {
	humans, monsters := buildLargeBenchState(b)

	// Single geofence covering the spawn location.
	// "area 0" normalises to "area 0" (NewSpatialIndex lowercases and replaces
	// underscores with spaces). Humans subscribed to "area 0" will be included.
	// Path entries are [lat, lon] to match PointInPolygon's coordinate convention.
	spatial := geofence.NewSpatialIndex([]geofence.Fence{
		{
			Name:             "area 0",
			DisplayInMatches: true,
			Path:             [][2]float64{{49, 3}, {51, 3}, {51, 6}, {49, 6}, {49, 3}},
		},
	})

	// Humans with i%100 ∈ {0, 98, 99} subscribe to "area 0".
	// That gives ≈30 humans out of 1000 as the applicable set.
	geoIdx := state.BuildHumanGeoIndex(humans, nil)

	pokemon := &ProcessedPokemon{
		PokemonID:   25,
		IV:          100,
		CP:          1000,
		Level:       30,
		ATK:         15,
		DEF:         15,
		STA:         15,
		Weight:      6.0,
		Size:        1,
		RarityGroup: 1,
		TTHSeconds:  600,
		Latitude:    50.0,
		Longitude:   4.0,
		Encountered: true,
		PVPBestRank: map[int][]pvp.LeagueRank{
			1500: {{Rank: 50, CP: 1490, Caps: []int{50}}},
		},
		PVPEvoData: make(map[int]map[int][]pvp.LeagueRank),
	}

	// Sanity: both flag values must produce the same number of matched users.
	// A discrepancy here means the workload exposes a parity bug.
	{
		stOff := &state.State{
			Humans:   humans,
			Monsters: monsters,
			Geofence: spatial,
			GeoIndex: geoIdx,
		}
		stOn := &state.State{
			Humans:   humans,
			Monsters: monsters,
			Geofence: spatial,
			GeoIndex: geoIdx,
		}
		mOff := &PokemonMatcher{GeographicPrefilter: false, PVPQueryMaxRank: 4096}
		mOn := &PokemonMatcher{GeographicPrefilter: true, PVPQueryMaxRank: 4096}

		off, _ := mOff.Match(pokemon, stOff)
		on, _ := mOn.Match(pokemon, stOn)

		if len(off) != len(on) {
			b.Fatalf("parity violation in bench setup: off=%d on=%d", len(off), len(on))
		}
		b.Logf("bench setup: %d humans, %d rules, %d matched users",
			len(humans), monsters.Total, len(off))
	}

	for _, flag := range []bool{false, true} {
		flag := flag // capture loop variable for closure
		st := &state.State{
			Humans:   humans,
			Monsters: monsters,
			Geofence: spatial,
			GeoIndex: geoIdx,
		}
		matcher := &PokemonMatcher{
			GeographicPrefilter: flag,
			PVPQueryMaxRank:     4096,
		}

		b.Run(fmt.Sprintf("prefilter=%v", flag), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = matcher.Match(pokemon, st)
			}
		})
	}
}
