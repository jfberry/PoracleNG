package matching

import (
	"fmt"
	"strconv"
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

// buildBenchFences builds a 10×10 grid of 100 rectangular fences covering
// lat 45–55, lon 0–10. Fence i occupies:
//
//	row := i/10,  col := i%10
//	lat [45+row … 46+row],  lon [col … col+1]
//
// Fence names are "area 0" … "area 99" with spaces, matching the normalised
// form that NewSpatialIndex produces (strings.ReplaceAll(name, "_", " ")).
func buildBenchFences() []geofence.Fence {
	fences := make([]geofence.Fence, 100)
	for i := 0; i < 100; i++ {
		row := i / 10
		col := i % 10
		minLat := 45.0 + float64(row)
		maxLat := 46.0 + float64(row)
		minLon := float64(col)
		maxLon := 1.0 + float64(col)
		fences[i] = geofence.Fence{
			Name:             "area " + strconv.Itoa(i),
			DisplayInMatches: true,
			// Path is [lat, lon] pairs; close the ring by repeating the first vertex.
			Path: [][2]float64{
				{minLat, minLon},
				{maxLat, minLon},
				{maxLat, maxLon},
				{minLat, maxLon},
				{minLat, minLon},
			},
		}
	}
	return fences
}

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
// Each human subscribes to three area_N fences drawn from the 100-fence grid,
// so every area name maps to a real geofence in the spatial index.
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
// The geofence layout is a realistic 10×10 grid of 100 fences covering
// lat 45–55, lon 0–10. The spawn lands in the centre of fence "area 50"
// (lat 50.5, lon 0.5 — row 5, col 0). Humans subscribe to 3 areas each,
// drawn from all 100 fences, so every area name maps to a real fence.
//
// Run with:
//
//	cd processor && go test -bench BenchmarkPokemonMatch -benchmem -count=1 -run=^$ ./internal/matching/...
func BenchmarkPokemonMatch_LargeRuleSet(b *testing.B) {
	humans, monsters := buildLargeBenchState(b)

	// 10×10 grid of 100 real fences. The spawn (50.5, 0.5) lands in the centre
	// of "area 50" (row 5, col 0 → lat 50–51, lon 0–1). The R-tree query
	// returns exactly one matching fence, matching real-world geofence sparsity.
	spatial := geofence.NewSpatialIndex(buildBenchFences())

	// Humans with i%100 ∈ {50, 49, 48} subscribe to "area 50".
	// That gives ≈30 humans out of 1000 as the applicable set.
	geoIdx := state.BuildHumanGeoIndex(humans, nil)

	// Spawn at the centre of "area 50": lat 50.5, lon 0.5.
	spawnLat, spawnLon := 50.5, 0.5

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
		Latitude:    spawnLat,
		Longitude:   spawnLon,
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

		// Report matched areas at spawn to confirm geofence layout is correct.
		_, matchedAreaNames := spatial.PointAreasAndNames(spawnLat, spawnLon)
		b.Logf("bench setup: %d humans, %d rules, 100 fences, %d matched areas at spawn, %d matched users",
			len(humans), monsters.Total, len(matchedAreaNames), len(off))
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
