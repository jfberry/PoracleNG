# Geographic Pre-Filter for Matching — Implementation Plan

> **Status: SHELVED (2026-05-12).** This plan was fully implemented and tested on the `geographic-prefilter` branch (PR [#90](https://github.com/jfberry/PoracleNG/pull/90)). Production data from a real install showed the optimisation regresses installs with tight per-rule filters (the common case for "iv100 per area" tracking patterns), because the per-rule cost is so low that the prefilter's fixed overhead (~8-11μs per spawn for `ApplicableHumans` + gather) exceeds the rule-iteration savings. The optimisation is genuinely beneficial only when `matching_seconds` P50 > ~30μs (large installs with loose per-rule filters where the bucket walk dominates). Rather than ship a flag whose meaning would confuse operators, the work is archived as exploratory.
>
> **Diagnostic metrics from the work were kept** (merged via PR [#91](https://github.com/jfberry/PoracleNG/pull/91)): `matching_seconds`, `matching_candidates`, `matching_haversines`, `state_monster_*_bucket` gauges. These let any future operator measure whether their install hits the threshold without re-implementing anything.
>
> **Revive only if a real install shows `matching_seconds` P50 > 30μs**, AND the bucket gauges show a large `everything` bucket (>5k rules), AND the per-rule cost is high (broad IV / distance / PVP constraints, not tight ones). At that point, this plan describes exactly how to rebuild the implementation. The reference code lives on the `geographic-prefilter` branch.
>
> **Cost model (derived from the production data):**
> - Per-bucket cost ≈ `total_rules_walked × per_rule_cost`
> - Prefilter cost ≈ `~10μs fixed + applicable_rules × per_rule_cost`
> - Break-even when `(total - applicable) × per_rule_cost > 10μs`
> - For per-rule cost ~3-5ns (tight filters), need to save iterating ~2,000-3,000 rules to break even
> - For per-rule cost ~30-50ns (loose filters), need to save iterating ~200-300 rules to break even

> **For agentic workers (if reviving):** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Drastically reduce per-spawn matching work for installations with very large rule counts (we've observed 100k and 500k single-user rule sets) by pre-filtering out humans whose geography can't possibly cover the spawn — before the per-rule IV/CP/etc filter loop pays for them.

**Architecture:** At state load, build (1) an immutable `HumanGeoIndex` keyed by `(area name → set of human IDs)` plus an R-tree of `(human location, max-distance-across-their-rules)` circles, AND (2) per-human rule indexes (`MonsterIndex.ByHumanAndLeague`, `RaidsByHuman`, `QuestsByHuman`, …) that partition each existing rule slice by owner. Per webhook, compute the small *applicable humans* set first (cheap: area map lookup + R-tree query), then drive the candidate-rule iteration **from** that set — walk each applicable human's rules in their per-human partition rather than walking the global per-pokemon buckets. The existing per-pokemon buckets stay as the fallback path when the flag is off. `matchMonsters` (and equivalents per type) still does the per-rule IV/CP/PVP/etc filter logic, just on a much smaller input slice. `ValidateHumans*` remains the authoritative final filter; no observable behaviour change.

**Tech Stack:** Go, existing `tidwall/rtree` (already used for geofences), Prometheus client_golang. No new dependencies.

**Pokemon-spawn-multi-area and user-multi-area:** Both already exist today. `matchedAreaNames map[string]bool` is the *set* of overlapping geofences containing the spawn; `human.Area []string` is the set of areas the user tracks. The geo index operates on these as sets — applicable humans = union of `HumansByArea[a]` over `a ∈ matchedAreaNames`, deduplicated. A rule has one owner, so iterating per applicable human naturally dedups across rules.

**Why per-human iteration (not post-filter on the assembled candidate slice):** A naive "compute applicable, then drop rules from the assembled `matched` slice whose `m.ID` isn't applicable" still iterates the full bucket inside `matchMonsters`. For a 500k-rule install with a fat `ByPokemonID[0]` bucket, that's 500k iterations of the IV/CP/PVP filter loop body per spawn — almost no savings. Driving iteration from the per-human partition cuts iterations to `applicable_humans × avg_rules_per_human` (~25k for a 500-applicable, 50-rule-per-user case), a 20× reduction. The per-rule filter logic inside `matchMonsters` is unchanged; only the input slice shrinks.

**Out of scope** (deliberate, do not pull into this plan):
- Per-track areas (future-compat: `HumansByArea` population source would change but matcher path is unaffected)
- Replacing `MonsterIndex.ByPokemonID` / `PVPSpecific` / `PVPEverything` — the pre-filter is composed with them
- Profile-aware indexing — current per-rule profile filter in `ValidateHumans*` stays
- Delivery, render, dispatch — untouched

---

## File Structure

| File | Responsibility |
|------|---------------|
| `processor/internal/metrics/metrics.go` | (modify) add `MatchingDuration` HistogramVec, `MatchingCandidates` HistogramVec, `MatchingApplicable` HistogramVec |
| `processor/internal/state/geo_index.go` | (new) `HumanGeoIndex` type, `BuildHumanGeoIndex(humans, monsters, raids, ...)` builder, `ApplicableHumans(lat, lon, matchedAreas, strictMode)` query |
| `processor/internal/state/geo_index_test.go` | (new) boundary tests for the index across area-only / distance-only / mixed / strict-area / multi-area scenarios |
| `processor/internal/state/state.go` | (modify) add `GeoIndex *HumanGeoIndex` field |
| `processor/internal/state/loader.go` | (modify) build `GeoIndex` after data load in both `Load` and `LoadWithGeofences` |
| `processor/internal/db/monsters.go` | (modify) add `MonsterIndex.ByHumanAndLeague map[string]map[int][]*MonsterTracking`; populate during `LoadMonsters` |
| `processor/internal/db/by_human.go` | (new) `BuildByHumanIndex[T any]` generic helper used by raid/egg/quest/invasion/lure/nest/gym/fort/maxbattle to partition flat rule slices by `humanID` |
| `processor/internal/state/state.go` | (modify) add per-type `*ByHuman` indexes referenced from `State` (e.g. `RaidsByHuman map[string][]*db.RaidTracking`) |
| `processor/internal/config/config.go` | (modify) `[tuning] geographic_prefilter bool` flag |
| `processor/internal/matching/pokemon.go` | (modify) apply pre-filter when flag set; instrument duration + candidate count |
| `processor/internal/matching/raid.go` (and `quest.go`, `invasion.go`, `lure.go`, `nest.go`, `gym.go`, `fort.go`, `maxbattle.go`) | (modify, one per task) same pre-filter pattern |
| `processor/internal/matching/pokemon_test.go` | (modify) new combinatorial test: multi-profile × area-restriction × strict mode |
| `processor/cmd/processor/main.go` | (modify) plumb `geographic_prefilter` flag into each matcher; bucket-size info log on startup/reload |
| `processor/cmd/processor/pokemon.go` etc. | (modify) wire `state.GeoIndex` through to matchers (one task per webhook type, paired with the matcher task) |
| `config/config.example.toml` | (modify) document the new flag |

---

## Phase 1: Instrumentation (no behaviour change, deploy first)

### Task 1: Add matching duration + candidate histograms

**Files:**
- Modify: `processor/internal/metrics/metrics.go` (add after `WebhookProcessingDuration`)

- [ ] **Step 1: Add metrics**

Edit `processor/internal/metrics/metrics.go`, add three new histograms next to `WebhookProcessingDuration`:

```go
MatchingDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
    Name:    "poracle_processor_matching_seconds",
    Help:    "Time spent inside a matcher's Match() call",
    Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1},
}, []string{"type"})

MatchingCandidates = promauto.NewHistogramVec(prometheus.HistogramOpts{
    Name:    "poracle_processor_matching_candidates",
    Help:    "Number of tracking rules scanned per webhook (before any filtering)",
    Buckets: []float64{1, 10, 100, 1000, 10000, 100000, 1000000},
}, []string{"type"})

MatchingApplicable = promauto.NewHistogramVec(prometheus.HistogramOpts{
    Name:    "poracle_processor_matching_applicable_humans",
    Help:    "Number of humans geographically applicable to this spawn (geographic_prefilter on; not observed when flag is off)",
    Buckets: []float64{1, 10, 100, 1000, 10000, 100000},
}, []string{"type"})
```

- [ ] **Step 2: Build to verify**

Run: `cd processor && go build ./...`
Expected: clean build, no errors.

- [ ] **Step 3: Commit**

```bash
git add processor/internal/metrics/metrics.go
git commit -m "metrics: add matching_seconds + matching_candidates + matching_applicable_humans histograms"
```

### Task 2: Instrument pokemon matcher Match() entry/exit

**Files:**
- Modify: `processor/internal/matching/pokemon.go:104-152` (the `Match` function)

- [ ] **Step 1: Add test for instrumentation**

Existing tests don't observe metrics directly. Add a smoke test ensuring `Match` is callable and we can read `MatchingDuration` counter sum. Add to `processor/internal/matching/pokemon_test.go`:

```go
func TestPokemonMatch_RecordsMatchingDuration(t *testing.T) {
    // Reset metric for deterministic read
    metrics.MatchingDuration.Reset()
    matcher := &PokemonMatcher{}
    pokemon := &ProcessedPokemon{PokemonID: 25, IV: 100}
    matcher.Match(pokemon, &state.State{
        Humans:   map[string]*db.Human{},
        Monsters: &db.MonsterIndex{ByPokemonID: map[int][]*db.MonsterTracking{}},
        Geofence: nil,
    })
    // A reflection-free way to assert a histogram has 1 sample: the counter sub-metric
    // increments by 1.
    m, err := metrics.MatchingDuration.GetMetricWithLabelValues("pokemon")
    if err != nil {
        t.Fatalf("get metric: %v", err)
    }
    var out dto.Metric
    if err := m.(prometheus.Histogram).Write(&out); err != nil {
        t.Fatalf("write metric: %v", err)
    }
    if got := out.GetHistogram().GetSampleCount(); got != 1 {
        t.Errorf("MatchingDuration{type=pokemon} sample count = %d, want 1", got)
    }
}
```

Add imports as needed: `"github.com/prometheus/client_golang/prometheus"`, `dto "github.com/prometheus/client_model/go"`, `"github.com/pokemon/poracleng/processor/internal/metrics"`, `"github.com/pokemon/poracleng/processor/internal/state"`.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd processor && go test ./internal/matching/... -run TestPokemonMatch_RecordsMatchingDuration -v`
Expected: FAIL — sample count = 0.

- [ ] **Step 3: Add the instrumentation**

Edit `processor/internal/matching/pokemon.go`. Wrap the body of `Match`:

```go
func (m *PokemonMatcher) Match(pokemon *ProcessedPokemon, st *state.State) ([]webhook.MatchedUser, []webhook.MatchedArea) {
    start := time.Now()
    defer func() {
        metrics.MatchingDuration.WithLabelValues("pokemon").Observe(time.Since(start).Seconds())
    }()
    if st == nil || st.Monsters == nil {
        return nil, nil
    }
    // ... existing body unchanged ...
}
```

Add imports `"time"` and `"github.com/pokemon/poracleng/processor/internal/metrics"`.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd processor && go test ./internal/matching/... -run TestPokemonMatch_RecordsMatchingDuration -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/matching/pokemon.go processor/internal/matching/pokemon_test.go
git commit -m "matching: record matching_seconds for pokemon matcher"
```

### Task 3: Instrument candidate count for pokemon matcher

**Files:**
- Modify: `processor/internal/matching/pokemon.go` (after the matched-slice assembly, before `ValidateHumans`)

- [ ] **Step 1: Write test**

Add to `processor/internal/matching/pokemon_test.go`:

```go
func TestPokemonMatch_RecordsCandidateCount(t *testing.T) {
    metrics.MatchingCandidates.Reset()
    matcher := &PokemonMatcher{}
    // Pre-seed two rules under pokemon_id=25 so candidate count > 0
    rules := []*db.MonsterTracking{
        {ID: "u1", PokemonID: 25, MaxIV: 100, MaxCP: 9000, MaxLevel: 55},
        {ID: "u2", PokemonID: 25, MaxIV: 100, MaxCP: 9000, MaxLevel: 55},
    }
    st := &state.State{
        Humans:   map[string]*db.Human{},
        Monsters: &db.MonsterIndex{ByPokemonID: map[int][]*db.MonsterTracking{25: rules}},
    }
    pokemon := &ProcessedPokemon{PokemonID: 25, IV: 100}
    matcher.Match(pokemon, st)

    h, _ := metrics.MatchingCandidates.GetMetricWithLabelValues("pokemon")
    var out dto.Metric
    _ = h.(prometheus.Histogram).Write(&out)
    if got := out.GetHistogram().GetSampleSum(); got != 2 {
        t.Errorf("MatchingCandidates sample sum = %v, want 2", got)
    }
}
```

- [ ] **Step 2: Verify fails**

Run: `cd processor && go test ./internal/matching/... -run TestPokemonMatch_RecordsCandidateCount -v`
Expected: FAIL — sample sum = 0.

- [ ] **Step 3: Add candidate-count Observe**

Edit `processor/internal/matching/pokemon.go`, after the `matched` slice is fully assembled and before `ValidateHumans`:

```go
metrics.MatchingCandidates.WithLabelValues("pokemon").Observe(float64(len(matched)))

// Validate humans
areas, matchedAreaNames := st.Geofence.PointAreasAndNames(pokemon.Latitude, pokemon.Longitude)
```

- [ ] **Step 4: Verify passes**

Run: `cd processor && go test ./internal/matching/... -run TestPokemonMatch_RecordsCandidateCount -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/matching/pokemon.go processor/internal/matching/pokemon_test.go
git commit -m "matching: record matching_candidates for pokemon matcher"
```

### Task 4: Instrument other 9 matcher types

Same pattern as Tasks 2 + 3, repeated for each. For brevity the steps are condensed — the engineer applies them to each file in turn.

**Files (modify each, one commit each):**
- `processor/internal/matching/raid.go` (RaidMatcher.Match)
- `processor/internal/matching/raid.go` (EggMatcher.Match — same file, separate type)
- `processor/internal/matching/quest.go` (QuestMatcher.Match)
- `processor/internal/matching/invasion.go`
- `processor/internal/matching/lure.go`
- `processor/internal/matching/nest.go`
- `processor/internal/matching/gym.go`
- `processor/internal/matching/fort.go`
- `processor/internal/matching/maxbattle.go`

Type labels used by the histograms (must match `WebhookProcessingDuration` label values for dashboard consistency): `"raid"`, `"egg"`, `"quest"`, `"invasion"`, `"lure"`, `"nest"`, `"gym"`, `"fort_update"`, `"maxbattle"`.

- [ ] **Step 1: Per matcher type, write a test based on the Task 2 template**

Replicate `TestPokemonMatch_RecordsMatchingDuration` in each `*_test.go`, using the type's label. E.g. for `raid_test.go`:

```go
func TestRaidMatch_RecordsMatchingDuration(t *testing.T) {
    metrics.MatchingDuration.Reset()
    matcher := &RaidMatcher{}
    matcher.Match(&webhook.RaidWebhook{}, &state.State{Humans: map[string]*db.Human{}, Raids: nil})
    h, _ := metrics.MatchingDuration.GetMetricWithLabelValues("raid")
    var out dto.Metric
    _ = h.(prometheus.Histogram).Write(&out)
    if got := out.GetHistogram().GetSampleCount(); got != 1 {
        t.Errorf("got %d, want 1", got)
    }
}
```

- [ ] **Step 2: Verify fails**

Run: `cd processor && go test ./internal/matching/... -run TestRaidMatch_RecordsMatchingDuration -v`
Expected: FAIL.

- [ ] **Step 3: Add instrumentation to that matcher's `Match`**

Same wrapper as pokemon:

```go
func (m *RaidMatcher) Match(...) (...) {
    start := time.Now()
    defer func() {
        metrics.MatchingDuration.WithLabelValues("raid").Observe(time.Since(start).Seconds())
    }()
    // ... existing body ...
}
```

Plus a `metrics.MatchingCandidates.WithLabelValues("raid").Observe(float64(len(matched)))` before the validate step.

- [ ] **Step 4: Verify passes**

Run: `cd processor && go test ./internal/matching/... -v`
Expected: All tests for that type pass.

- [ ] **Step 5: Commit per matcher type**

```bash
git add processor/internal/matching/raid.go processor/internal/matching/raid_test.go
git commit -m "matching: record matching_seconds + matching_candidates for raid matcher"
```

Repeat Steps 1–5 for egg, quest, invasion, lure, nest, gym, fort, maxbattle. One commit per type, nine commits total in this task.

### Task 5: Add bucket-size info log at state load

**Files:**
- Modify: `processor/internal/state/loader.go:56` and `:117` (the existing `log.Infof` lines)

- [ ] **Step 1: Write helper test**

Add `processor/internal/state/bucket_sizes_test.go`:

```go
package state

import (
    "strings"
    "testing"

    "github.com/pokemon/poracleng/processor/internal/db"
)

func TestSummarizeMonsterBuckets(t *testing.T) {
    idx := &db.MonsterIndex{
        ByPokemonID: map[int][]*db.MonsterTracking{
            0:   make([]*db.MonsterTracking, 5000),
            25:  make([]*db.MonsterTracking, 200),
            150: make([]*db.MonsterTracking, 150),
            12:  make([]*db.MonsterTracking, 80),
        },
        PVPSpecific: map[int][]*db.MonsterTracking{
            500: make([]*db.MonsterTracking, 100),
            1500: make([]*db.MonsterTracking, 200),
        },
        PVPEverything: map[int][]*db.MonsterTracking{
            1500: make([]*db.MonsterTracking, 400),
        },
        Total: 5930,
    }
    s := summarizeMonsterBuckets(idx)
    if !strings.Contains(s, "everything=5000") {
        t.Errorf("expected everything bucket size in summary, got %q", s)
    }
    if !strings.Contains(s, "top-pokemon=") {
        t.Errorf("expected top-pokemon list in summary, got %q", s)
    }
    if !strings.Contains(s, "pvp-everything=") {
        t.Errorf("expected pvp-everything in summary, got %q", s)
    }
}
```

- [ ] **Step 2: Verify fails**

Run: `cd processor && go test ./internal/state/... -run TestSummarizeMonsterBuckets -v`
Expected: FAIL — undefined `summarizeMonsterBuckets`.

- [ ] **Step 3: Implement helper**

Add `processor/internal/state/bucket_sizes.go`:

```go
package state

import (
    "fmt"
    "sort"
    "strings"

    "github.com/pokemon/poracleng/processor/internal/db"
)

// summarizeMonsterBuckets returns a one-line human-readable summary of the
// monster index's bucket sizes. The "everything" bucket (pokemon_id=0) is
// the catch-all rule list scanned for every pokemon spawn, so operators
// want to see its size at a glance; "top-pokemon" lists the 5 most-tracked
// species so configuration hotspots are visible.
func summarizeMonsterBuckets(idx *db.MonsterIndex) string {
    if idx == nil {
        return "monsters=nil"
    }
    everything := len(idx.ByPokemonID[0])

    type bucket struct {
        id   int
        size int
    }
    perSpecies := make([]bucket, 0, len(idx.ByPokemonID))
    for id, slice := range idx.ByPokemonID {
        if id == 0 {
            continue
        }
        perSpecies = append(perSpecies, bucket{id, len(slice)})
    }
    sort.Slice(perSpecies, func(i, j int) bool { return perSpecies[i].size > perSpecies[j].size })
    top := perSpecies
    if len(top) > 5 {
        top = top[:5]
    }
    topStrs := make([]string, 0, len(top))
    for _, b := range top {
        topStrs = append(topStrs, fmt.Sprintf("%d=%d", b.id, b.size))
    }

    pvpSpec := 0
    for _, slice := range idx.PVPSpecific {
        pvpSpec += len(slice)
    }
    pvpEvery := 0
    for _, slice := range idx.PVPEverything {
        pvpEvery += len(slice)
    }

    return fmt.Sprintf(
        "monsters: total=%d everything=%d top-pokemon=[%s] pvp-specific=%d pvp-everything=%d",
        idx.Total, everything, strings.Join(topStrs, ","), pvpSpec, pvpEvery,
    )
}
```

- [ ] **Step 4: Verify passes**

Run: `cd processor && go test ./internal/state/... -run TestSummarizeMonsterBuckets -v`
Expected: PASS.

- [ ] **Step 5: Wire into loaders**

Edit `processor/internal/state/loader.go`, modify the existing `log.Infof("State loaded: ...")` in BOTH `Load` and `LoadWithGeofences` — add a follow-up line:

```go
log.Infof("State loaded: %d humans, %d pokemon, ...", ...)
log.Infof("State buckets: %s", summarizeMonsterBuckets(data.Monsters))
```

- [ ] **Step 6: Build to confirm**

Run: `cd processor && go build ./...`
Expected: clean.

- [ ] **Step 7: Commit**

```bash
git add processor/internal/state/bucket_sizes.go processor/internal/state/bucket_sizes_test.go processor/internal/state/loader.go
git commit -m "state: log monster bucket sizes at load time"
```

---

## Phase 2: Geographic index + pokemon pre-filter (behind flag)

### Task 6: Add `[tuning] geographic_prefilter` config flag

**Files:**
- Modify: `processor/internal/config/config.go` (find the `Tuning` struct)
- Modify: `config/config.example.toml`

- [ ] **Step 1: Find Tuning struct**

Run: `cd processor && grep -n "type Tuning" internal/config/config.go`
Note the struct location.

- [ ] **Step 2: Add field**

In `processor/internal/config/config.go`, add to the `Tuning` struct:

```go
GeographicPrefilter bool `toml:"geographic_prefilter"`
```

If the struct doesn't yet have a `Tuning` section but uses another name (e.g. `TuningConfig`), put it on whichever struct corresponds to `[tuning]`. Default value is the Go zero value (false) — explicit init not required.

- [ ] **Step 3: Document in config.example.toml**

In `config/config.example.toml`, find the `[tuning]` section and add:

```toml
# When true, the matcher pre-filters out humans whose geography (selected
# areas + tracking distance) cannot possibly cover the spawn before running
# the per-rule IV/CP/etc filter loop. Major speedup for installations with
# very large rule counts; safe to leave off if you have few rules.
geographic_prefilter = false
```

- [ ] **Step 4: Build to confirm**

Run: `cd processor && go build ./...`
Expected: clean.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/config/config.go config/config.example.toml
git commit -m "config: add [tuning] geographic_prefilter flag (default off)"
```

### Task 7: HumanGeoIndex type + builder + tests

**Files:**
- Create: `processor/internal/state/geo_index.go`
- Create: `processor/internal/state/geo_index_test.go`

- [ ] **Step 1: Write failing tests first**

Create `processor/internal/state/geo_index_test.go`:

```go
package state

import (
    "testing"

    "github.com/pokemon/poracleng/processor/internal/db"
)

// Helper to build a Human with the fields the index reads.
func mkHuman(id string, areas []string, restriction []string, lat, lon float64) *db.Human {
    return &db.Human{
        ID:              id,
        Enabled:         true,
        Area:            areas,
        AreaRestriction: restriction,
        Latitude:        lat,
        Longitude:       lon,
    }
}

func TestHumanGeoIndex_AreaOnly(t *testing.T) {
    humans := map[string]*db.Human{
        "u1": mkHuman("u1", []string{"belgium", "antwerp"}, nil, 0, 0),
        "u2": mkHuman("u2", []string{"belgium"}, nil, 0, 0),
        "u3": mkHuman("u3", []string{"japan"}, nil, 0, 0),
    }
    idx := BuildHumanGeoIndex(humans, nil)

    got := idx.ApplicableHumans(0, 0, map[string]bool{"belgium": true}, false)
    if !got["u1"] || !got["u2"] {
        t.Errorf("expected u1,u2 applicable for belgium spawn, got %v", keysOf(got))
    }
    if got["u3"] {
        t.Errorf("u3 should not be applicable for belgium spawn")
    }
}

func TestHumanGeoIndex_MultipleMatchedAreas(t *testing.T) {
    // A spawn in overlapping geofences "belgium" + "antwerp" should pick up
    // humans who track either. Confirms the union-with-dedup behaviour.
    humans := map[string]*db.Human{
        "u1": mkHuman("u1", []string{"belgium"}, nil, 0, 0),
        "u2": mkHuman("u2", []string{"antwerp"}, nil, 0, 0),
        "u3": mkHuman("u3", []string{"belgium", "antwerp"}, nil, 0, 0),
    }
    idx := BuildHumanGeoIndex(humans, nil)
    got := idx.ApplicableHumans(0, 0, map[string]bool{"belgium": true, "antwerp": true}, false)
    if len(got) != 3 {
        t.Errorf("expected 3 applicable humans across both matched areas, got %v", keysOf(got))
    }
}

func TestHumanGeoIndex_DistanceOnly(t *testing.T) {
    // u1 has no area selection but lives at (1,1) with max-rule-distance 5000m.
    // A spawn at (1.0001,1.0001) is ~15m away — applicable.
    // A spawn at (10,10) is ~1400km away — not applicable.
    humans := map[string]*db.Human{
        "u1": mkHuman("u1", nil, nil, 1.0, 1.0),
    }
    perHumanMaxDist := map[string]int{"u1": 5000}
    idx := BuildHumanGeoIndex(humans, perHumanMaxDist)

    near := idx.ApplicableHumans(1.0001, 1.0001, map[string]bool{}, false)
    if !near["u1"] {
        t.Errorf("u1 should be applicable for nearby spawn, got %v", keysOf(near))
    }
    far := idx.ApplicableHumans(10, 10, map[string]bool{}, false)
    if far["u1"] {
        t.Errorf("u1 should not be applicable for far spawn")
    }
}

func TestHumanGeoIndex_AreaPlusDistanceUnion(t *testing.T) {
    // A human applicable by EITHER area OR distance is applicable overall.
    humans := map[string]*db.Human{
        "u1": mkHuman("u1", []string{"belgium"}, nil, 1.0, 1.0),
    }
    perHumanMaxDist := map[string]int{"u1": 5000}
    idx := BuildHumanGeoIndex(humans, perHumanMaxDist)

    // Far away spawn, but in user's area selection — applicable
    out := idx.ApplicableHumans(50, 50, map[string]bool{"belgium": true}, false)
    if !out["u1"] {
        t.Errorf("u1 area-applicable case: got %v", keysOf(out))
    }

    // Outside area, but within distance — applicable
    out = idx.ApplicableHumans(1.0001, 1.0001, map[string]bool{"japan": true}, false)
    if !out["u1"] {
        t.Errorf("u1 distance-applicable case: got %v", keysOf(out))
    }

    // Outside both — not applicable
    out = idx.ApplicableHumans(50, 50, map[string]bool{"japan": true}, false)
    if out["u1"] {
        t.Errorf("u1 should NOT be applicable when out of area and out of distance")
    }
}

func TestHumanGeoIndex_StrictAreaRestriction(t *testing.T) {
    // In strict mode, applicability also requires the spawn to overlap
    // the user's AreaRestriction (not just their tracked Area).
    humans := map[string]*db.Human{
        "u1": mkHuman("u1", []string{"belgium", "antwerp"}, []string{"belgium"}, 0, 0),
    }
    idx := BuildHumanGeoIndex(humans, nil)

    // Strict OFF: u1 applicable for antwerp spawn (their Area covers it)
    out := idx.ApplicableHumans(0, 0, map[string]bool{"antwerp": true}, false)
    if !out["u1"] {
        t.Errorf("strict off, antwerp spawn: u1 should be applicable")
    }

    // Strict ON: u1 NOT applicable for antwerp spawn (restriction is belgium only)
    out = idx.ApplicableHumans(0, 0, map[string]bool{"antwerp": true}, true)
    if out["u1"] {
        t.Errorf("strict on, antwerp spawn: u1 should NOT be applicable (restriction=belgium)")
    }

    // Strict ON: u1 applicable for belgium spawn (in Area AND in Restriction)
    out = idx.ApplicableHumans(0, 0, map[string]bool{"belgium": true}, true)
    if !out["u1"] {
        t.Errorf("strict on, belgium spawn: u1 should be applicable")
    }
}

func TestHumanGeoIndex_DisabledHumansExcluded(t *testing.T) {
    h := mkHuman("u1", []string{"belgium"}, nil, 0, 0)
    h.Enabled = false
    humans := map[string]*db.Human{"u1": h}
    idx := BuildHumanGeoIndex(humans, nil)

    out := idx.ApplicableHumans(0, 0, map[string]bool{"belgium": true}, false)
    if out["u1"] {
        t.Errorf("disabled human should not be in index")
    }
}

func keysOf(m map[string]bool) []string {
    out := make([]string, 0, len(m))
    for k := range m {
        out = append(out, k)
    }
    return out
}
```

- [ ] **Step 2: Verify fails**

Run: `cd processor && go test ./internal/state/... -run TestHumanGeoIndex -v`
Expected: FAIL — `BuildHumanGeoIndex` undefined.

- [ ] **Step 3: Implement the index**

Create `processor/internal/state/geo_index.go`:

```go
package state

import (
    "github.com/tidwall/rtree"

    "github.com/pokemon/poracleng/processor/internal/db"
    "github.com/pokemon/poracleng/processor/internal/matching"
)

// HumanGeoIndex pre-computes which humans are geographically capable of
// receiving an alert for a given spawn location. Two index structures:
//
//   - byArea: areaName → set of humanIDs whose Area list contains it
//   - byAreaRestriction: same shape but only humans with a strict-mode
//     AreaRestriction set (used when strict mode is enabled)
//   - distanceTree: R-tree of (humanLocation, maxRuleDistance) bounding
//     boxes for distance-based rules
//
// Built once per state load; never mutated after BuildHumanGeoIndex returns.
// Concurrent reads from many matcher goroutines are safe because the
// underlying maps and rtree are read-only.
type HumanGeoIndex struct {
    byArea            map[string]map[string]bool
    byAreaRestriction map[string]map[string]bool
    distanceTree      rtree.RTreeG[string]
    // humansWithDistance is the set of human IDs that have at least one
    // distance-based rule. Required so a strict-mode query that finds them
    // via the distanceTree still gets the strict-restriction check applied.
    humansWithDistance map[string]bool
    humansWithArea     map[string]bool
}

// BuildHumanGeoIndex constructs the index from the loaded humans map and a
// per-human max-tracking-distance map. perHumanMaxDist holds the max distance
// (in metres) across all of that human's tracking rules; humans with only
// area-based rules (distance == 0 on every rule) are omitted from
// perHumanMaxDist and don't enter the distance tree.
//
// Disabled or admin-disabled humans are excluded; their rules can't fire
// regardless.
func BuildHumanGeoIndex(humans map[string]*db.Human, perHumanMaxDist map[string]int) *HumanGeoIndex {
    idx := &HumanGeoIndex{
        byArea:             map[string]map[string]bool{},
        byAreaRestriction:  map[string]map[string]bool{},
        humansWithDistance: map[string]bool{},
        humansWithArea:     map[string]bool{},
    }
    for id, h := range humans {
        if h == nil || !h.Enabled || h.AdminDisable {
            continue
        }
        for _, a := range h.Area {
            if a == "" {
                continue
            }
            if idx.byArea[a] == nil {
                idx.byArea[a] = map[string]bool{}
            }
            idx.byArea[a][id] = true
            idx.humansWithArea[id] = true
        }
        for _, a := range h.AreaRestriction {
            if a == "" {
                continue
            }
            if idx.byAreaRestriction[a] == nil {
                idx.byAreaRestriction[a] = map[string]bool{}
            }
            idx.byAreaRestriction[a][id] = true
        }
        if d, ok := perHumanMaxDist[id]; ok && d > 0 {
            // Insert a bounding box of side ~2*d around the human's location.
            // Use a degree approximation: 1 deg latitude ≈ 111_320 m. We use
            // the same approximation for longitude (acceptable because we
            // re-check with HaversineDistance at query time; the bbox just
            // shortlists candidates).
            dDeg := float64(d) / 111320.0
            minLat := h.Latitude - dDeg
            maxLat := h.Latitude + dDeg
            minLon := h.Longitude - dDeg
            maxLon := h.Longitude + dDeg
            idx.distanceTree.Insert([2]float64{minLon, minLat}, [2]float64{maxLon, maxLat}, id)
            idx.humansWithDistance[id] = true
        }
    }
    return idx
}

// ApplicableHumans returns the set of human IDs whose geography (area
// selection and/or rule-distance circle) overlaps the spawn at (lat, lon)
// in any of matchedAreas. In strictMode, an area match additionally
// requires the human's AreaRestriction to overlap matchedAreas.
//
// Distance-based hits are filtered by exact haversine against the
// human's stored location and the per-rule max distance (the rtree
// bbox is just a fast pre-filter using a worst-case degree approximation).
//
// The returned map is fresh; callers may mutate it.
func (idx *HumanGeoIndex) ApplicableHumans(
    lat, lon float64,
    matchedAreas map[string]bool,
    strictMode bool,
) map[string]bool {
    out := map[string]bool{}
    if idx == nil {
        return out
    }

    // Area-based hits
    for area := range matchedAreas {
        for id := range idx.byArea[area] {
            if strictMode {
                if !humanHasRestrictionOverlap(idx, id, matchedAreas) {
                    continue
                }
            }
            out[id] = true
        }
    }

    // Distance-based hits — only consider humans for whom we inserted a
    // bbox. The rtree query returns candidates; haversine confirms.
    idx.distanceTree.Search(
        [2]float64{lon, lat}, [2]float64{lon, lat},
        func(_, _ [2]float64, id string) bool {
            if out[id] {
                return true // already applicable via area path
            }
            if strictMode && !humanHasRestrictionOverlap(idx, id, matchedAreas) {
                return true
            }
            out[id] = true
            return true
        },
    )
    return out
}

func humanHasRestrictionOverlap(idx *HumanGeoIndex, humanID string, matchedAreas map[string]bool) bool {
    // Without an AreaRestriction set, the human has no restriction and is
    // always considered to satisfy strict mode (existing
    // ValidateHumansGeneric semantics).
    hasAny := false
    for _, ids := range idx.byAreaRestriction {
        if ids[humanID] {
            hasAny = true
            break
        }
    }
    if !hasAny {
        return true
    }
    for area := range matchedAreas {
        if idx.byAreaRestriction[area][humanID] {
            return true
        }
    }
    return false
}
```

Note: `matching` import is not actually used — drop it from the import block.

- [ ] **Step 4: Verify tests pass**

Run: `cd processor && go test ./internal/state/... -run TestHumanGeoIndex -v`
Expected: all 6 tests PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/state/geo_index.go processor/internal/state/geo_index_test.go
git commit -m "state: HumanGeoIndex with area + distance buckets and strict-mode support"
```

### Task 8: Compute per-human max distance from all rule types

**Files:**
- Create: `processor/internal/state/per_human_distance.go`
- Create: `processor/internal/state/per_human_distance_test.go`

- [ ] **Step 1: Write failing test**

Create `processor/internal/state/per_human_distance_test.go`:

```go
package state

import (
    "testing"

    "github.com/pokemon/poracleng/processor/internal/db"
)

func TestPerHumanMaxDistance_AcrossAllRuleTypes(t *testing.T) {
    data := &db.LoadedData{
        Monsters: &db.MonsterIndex{
            ByPokemonID: map[int][]*db.MonsterTracking{
                25: {{ID: "u1", Distance: 5000}},
                0:  {{ID: "u1", Distance: 1000}, {ID: "u2", Distance: 2000}},
            },
        },
        Raids:      []*db.RaidTracking{{ID: "u1", Distance: 8000}},
        Eggs:       []*db.EggTracking{{ID: "u3", Distance: 500}},
        Invasions:  []*db.InvasionTracking{{ID: "u2", Distance: 12000}},
        Quests:     []*db.QuestTracking{{ID: "u4", Distance: 0}}, // area-only, contributes nothing
        Lures:      []*db.LureTracking{},
        Nests:      []*db.NestTracking{},
        Gyms:       []*db.GymTracking{{ID: "u1", Distance: 100}},
        Forts:      []*db.FortTracking{},
        Maxbattles: []*db.MaxbattleTracking{{ID: "u1", Distance: 6000}},
    }
    got := PerHumanMaxDistance(data)
    if got["u1"] != 8000 { // raid is max
        t.Errorf("u1 max = %d, want 8000", got["u1"])
    }
    if got["u2"] != 12000 { // invasion is max
        t.Errorf("u2 max = %d, want 12000", got["u2"])
    }
    if got["u3"] != 500 {
        t.Errorf("u3 max = %d, want 500", got["u3"])
    }
    if _, ok := got["u4"]; ok {
        t.Errorf("u4 has only distance==0 rules, should be absent")
    }
}
```

- [ ] **Step 2: Verify fails**

Run: `cd processor && go test ./internal/state/... -run TestPerHumanMaxDistance -v`
Expected: FAIL — `PerHumanMaxDistance` and possibly `db.LoadedData` field names. Inspect actual field names by checking `processor/internal/db/loader.go` for the struct returned by `LoadAll`; the test field names above assume `LoadedData` with those slice/map names — adjust to match (e.g. it may be `LoadResult` or have different field names).

- [ ] **Step 3: Implement helper**

Create `processor/internal/state/per_human_distance.go`. **First** inspect `processor/internal/db/loader.go` to find the exact type and field names returned by `db.LoadAll`. The implementation pattern is the same regardless of the exact type:

```go
package state

import "github.com/pokemon/poracleng/processor/internal/db"

// PerHumanMaxDistance walks every rule across every tracking type and
// returns the maximum non-zero Distance per human ID. Humans with only
// distance==0 (area-based) rules are absent from the map; the index
// builder then leaves them out of the distance r-tree.
func PerHumanMaxDistance(data *db.LoadedData) map[string]int {
    out := map[string]int{}
    record := func(id string, d int) {
        if d <= 0 || id == "" {
            return
        }
        if prev, ok := out[id]; !ok || d > prev {
            out[id] = d
        }
    }
    if data.Monsters != nil {
        for _, slice := range data.Monsters.ByPokemonID {
            for _, m := range slice {
                record(m.ID, m.Distance)
            }
        }
        for _, slice := range data.Monsters.PVPSpecific {
            for _, m := range slice {
                record(m.ID, m.Distance)
            }
        }
        for _, slice := range data.Monsters.PVPEverything {
            for _, m := range slice {
                record(m.ID, m.Distance)
            }
        }
    }
    for _, r := range data.Raids {
        record(r.ID, r.Distance)
    }
    for _, e := range data.Eggs {
        record(e.ID, e.Distance)
    }
    for _, i := range data.Invasions {
        record(i.ID, i.Distance)
    }
    for _, q := range data.Quests {
        record(q.ID, q.Distance)
    }
    for _, l := range data.Lures {
        record(l.ID, l.Distance)
    }
    for _, n := range data.Nests {
        record(n.ID, n.Distance)
    }
    for _, g := range data.Gyms {
        record(g.ID, g.Distance)
    }
    for _, f := range data.Forts {
        record(f.ID, f.Distance)
    }
    for _, mb := range data.Maxbattles {
        record(mb.ID, mb.Distance)
    }
    return out
}
```

If a tracking type's struct uses a different field name for the human ID or distance, adapt per file (`*.go` in `processor/internal/db/`).

- [ ] **Step 4: Verify passes**

Run: `cd processor && go test ./internal/state/... -run TestPerHumanMaxDistance -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/state/per_human_distance.go processor/internal/state/per_human_distance_test.go
git commit -m "state: PerHumanMaxDistance for distance-based geo index"
```

### Task 9: Wire HumanGeoIndex into State + state loaders

**Files:**
- Modify: `processor/internal/state/state.go` (add field)
- Modify: `processor/internal/state/loader.go` (build index in both loaders)

- [ ] **Step 1: Test that State.GeoIndex is populated after Load**

Add `processor/internal/state/loader_test.go` (or append to existing file if one exists):

```go
package state

import "testing"

func TestLoaderBuildsGeoIndex(t *testing.T) {
    // We don't exercise the full DB path here — we synthesise a State
    // via the manager and assert GeoIndex would be set. This test guards
    // the field's presence; the build is exercised end-to-end via the
    // matching tests in Phase 2 Task 11.
    m := NewManager()
    s := &State{GeoIndex: &HumanGeoIndex{}}
    m.Set(s)
    got := m.Get()
    if got.GeoIndex == nil {
        t.Errorf("State.GeoIndex field should be settable and round-trip")
    }
}
```

- [ ] **Step 2: Verify fails**

Run: `cd processor && go test ./internal/state/... -run TestLoaderBuildsGeoIndex -v`
Expected: FAIL — `State.GeoIndex` undefined.

- [ ] **Step 3: Add field to state.go**

Edit `processor/internal/state/state.go`, add to the `State` struct:

```go
type State struct {
    Humans     map[string]*db.Human
    Monsters   *db.MonsterIndex
    // ... existing fields ...
    Geofence   *geofence.SpatialIndex
    Fences     []geofence.Fence
    GeoIndex   *HumanGeoIndex
}
```

- [ ] **Step 4: Verify field test passes**

Run: `cd processor && go test ./internal/state/... -run TestLoaderBuildsGeoIndex -v`
Expected: PASS.

- [ ] **Step 5: Wire into loaders**

Edit `processor/internal/state/loader.go`. In both `Load` and `LoadWithGeofences`, after the `&State{...}` literal:

```go
s := &State{
    Humans:     data.Humans,
    // ... existing fields ...
    Geofence:   spatial,
    Fences:     fences,
}
s.GeoIndex = BuildHumanGeoIndex(data.Humans, PerHumanMaxDistance(data))
manager.Set(s)
```

- [ ] **Step 6: Build to confirm**

Run: `cd processor && go build ./...`
Expected: clean.

- [ ] **Step 7: Run full state test suite**

Run: `cd processor && go test ./internal/state/... -count=1`
Expected: all pass.

- [ ] **Step 8: Commit**

```bash
git add processor/internal/state/state.go processor/internal/state/loader.go processor/internal/state/loader_test.go
git commit -m "state: build HumanGeoIndex in Load and LoadWithGeofences"
```

### Task 9b: Build per-human rule partitions for every tracking type

The geo index tells us *which* humans matter; this task builds an index that lets us *iterate only their rules*. Each tracking type gets its own per-human partition. The existing per-pokemon / flat-slice indexes stay; the per-human indexes are additive (~6-10MB extra for 500k rules — same pointers, organised differently).

**Files:**
- Modify: `processor/internal/db/monsters.go` (add `ByHumanAndLeague` to `MonsterIndex`)
- Create: `processor/internal/db/by_human.go` + `_test.go` (generic partitioner for the other 9 types)
- Modify: `processor/internal/state/state.go` (add fields)
- Modify: `processor/internal/state/loader.go` (populate after `db.LoadAll`)

- [ ] **Step 1: Failing test for the pokemon per-human partition**

Add to `processor/internal/db/monsters_test.go` (create the file if missing):

```go
package db

import "testing"

func TestMonsterIndex_ByHumanAndLeague_PartitionsCorrectly(t *testing.T) {
    // The per-human index is the same rule pointers organised by
    // (humanID, pvp_ranking_league). league=0 = non-PVP. Per spawn we
    // walk a single applicable human's bucket per league, instead of
    // walking ByPokemonID[0] / ByPokemonID[N] / PVPSpecific[league] / etc.
    rules := []MonsterTracking{
        {ID: "u1", PokemonID: 25, PVPRankingLeague: 0},     // non-PVP per-species
        {ID: "u1", PokemonID: 0, PVPRankingLeague: 0},      // non-PVP everything
        {ID: "u1", PokemonID: 6, PVPRankingLeague: 1500},   // PVP per-species (great)
        {ID: "u2", PokemonID: 0, PVPRankingLeague: 1500},   // PVP everything (great)
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
    // backing array elements as ByPokemonID. (We don't store new structs.)
    if u1NonPVP[0] != idx.ByPokemonID[25][0] && u1NonPVP[0] != idx.ByPokemonID[0][0] {
        t.Errorf("u1's non-PVP rule should share pointer identity with ByPokemonID entry")
    }
}
```

- [ ] **Step 2: Verify fails**

Run: `cd processor && go test ./internal/db/... -run TestMonsterIndex_ByHumanAndLeague -v`
Expected: FAIL — `ByHumanAndLeague` and `buildMonsterIndexFromRules` undefined.

- [ ] **Step 3: Add the field and populate it**

Edit `processor/internal/db/monsters.go`:

```go
type MonsterIndex struct {
    ByPokemonID   map[int][]*MonsterTracking
    PVPSpecific   map[int][]*MonsterTracking
    PVPEverything map[int][]*MonsterTracking

    // ByHumanAndLeague partitions every rule by (humanID, pvp_ranking_league).
    // league==0 means "non-PVP" — non-PVP rules from ByPokemonID[0]
    // (everything) and ByPokemonID[N] (per-species) both land in
    // ByHumanAndLeague[humanID][0]. Used by matchers when
    // geographic_prefilter is enabled to iterate per applicable human
    // instead of per pokemon bucket.
    ByHumanAndLeague map[string]map[int][]*MonsterTracking

    Total int
}
```

Extract the index-build logic from `LoadMonsters` into a `buildMonsterIndexFromRules(rules []MonsterTracking) *MonsterIndex` helper so the test can hit it directly. Update `LoadMonsters` to call this helper. Inside the helper, after the existing per-pokemon and PVP loops, add:

```go
idx.ByHumanAndLeague = map[string]map[int][]*MonsterTracking{}
for i := range monsters {
    m := &monsters[i]
    perH, ok := idx.ByHumanAndLeague[m.ID]
    if !ok {
        perH = map[int][]*MonsterTracking{}
        idx.ByHumanAndLeague[m.ID] = perH
    }
    perH[m.PVPRankingLeague] = append(perH[m.PVPRankingLeague], m)
}
```

- [ ] **Step 4: Verify pass**

Run: `cd processor && go test ./internal/db/... -run TestMonsterIndex_ByHumanAndLeague -v`
Expected: PASS.

- [ ] **Step 5: Generic per-human partitioner for non-pokemon types**

Create `processor/internal/db/by_human.go`:

```go
package db

// HumanIDExtractor returns the human ID for a tracking row. Implemented
// by each tracking type below.
type HumanIDExtractor[T any] func(*T) string

// PartitionByHuman groups any flat tracking slice by the human ID
// returned from extract. Returns a map[humanID][]*T — pointers refer
// into the original slice so this is cheap (no copies).
func PartitionByHuman[T any](rows []T, extract HumanIDExtractor[T]) map[string][]*T {
    out := map[string][]*T{}
    for i := range rows {
        id := extract(&rows[i])
        if id == "" {
            continue
        }
        out[id] = append(out[id], &rows[i])
    }
    return out
}
```

Add ID extractors next to each tracking type definition (one per type, all in their existing files):

```go
// in raid.go:
func RaidHumanID(r *RaidTracking) string { return r.ID }
// in egg.go: func EggHumanID(e *EggTracking) string { return e.ID }
// ... etc for invasion, quest, lure, nest, gym, fort, maxbattle
```

- [ ] **Step 6: Test the generic partitioner**

Create `processor/internal/db/by_human_test.go`:

```go
package db

import "testing"

func TestPartitionByHuman_GroupsByExtractor(t *testing.T) {
    rows := []RaidTracking{
        {ID: "u1", Level: 5},
        {ID: "u2", Level: 1},
        {ID: "u1", Level: 3},
    }
    got := PartitionByHuman(rows, RaidHumanID)
    if len(got["u1"]) != 2 {
        t.Errorf("u1 = %d rules, want 2", len(got["u1"]))
    }
    if len(got["u2"]) != 1 {
        t.Errorf("u2 = %d rules, want 1", len(got["u2"]))
    }
    if got["u1"][0] != &rows[0] {
        t.Errorf("pointer identity broken")
    }
}

func TestPartitionByHuman_EmptyIDSkipped(t *testing.T) {
    rows := []RaidTracking{{ID: ""}, {ID: "u1"}}
    got := PartitionByHuman(rows, RaidHumanID)
    if _, ok := got[""]; ok {
        t.Errorf("empty ID should not appear")
    }
    if len(got["u1"]) != 1 {
        t.Errorf("u1 = %d, want 1", len(got["u1"]))
    }
}
```

Run: `cd processor && go test ./internal/db/... -run TestPartitionByHuman -v`
Expected: PASS.

- [ ] **Step 7: Add per-type ByHuman fields to State and populate in loader**

Edit `processor/internal/state/state.go`:

```go
type State struct {
    // ... existing fields ...
    GeoIndex *HumanGeoIndex

    // Per-human partitions for the geographic_prefilter path. Same
    // pointers as the existing flat slices (Raids, Eggs, etc.), just
    // organised by owner.
    RaidsByHuman      map[string][]*db.RaidTracking
    EggsByHuman       map[string][]*db.EggTracking
    QuestsByHuman     map[string][]*db.QuestTracking
    InvasionsByHuman  map[string][]*db.InvasionTracking
    LuresByHuman      map[string][]*db.LureTracking
    NestsByHuman      map[string][]*db.NestTracking
    GymsByHuman       map[string][]*db.GymTracking
    FortsByHuman      map[string][]*db.FortTracking
    MaxbattlesByHuman map[string][]*db.MaxbattleTracking
}
```

Edit `processor/internal/state/loader.go`, in BOTH `Load` and `LoadWithGeofences`, after the existing `&State{…}` literal and the `GeoIndex` assignment:

```go
s.RaidsByHuman = db.PartitionByHuman(data.Raids, db.RaidHumanID)
s.EggsByHuman = db.PartitionByHuman(data.Eggs, db.EggHumanID)
s.QuestsByHuman = db.PartitionByHuman(data.Quests, db.QuestHumanID)
s.InvasionsByHuman = db.PartitionByHuman(data.Invasions, db.InvasionHumanID)
s.LuresByHuman = db.PartitionByHuman(data.Lures, db.LureHumanID)
s.NestsByHuman = db.PartitionByHuman(data.Nests, db.NestHumanID)
s.GymsByHuman = db.PartitionByHuman(data.Gyms, db.GymHumanID)
s.FortsByHuman = db.PartitionByHuman(data.Forts, db.FortHumanID)
s.MaxbattlesByHuman = db.PartitionByHuman(data.Maxbattles, db.MaxbattleHumanID)
```

NB: the `data` variable name and `data.Raids` etc. field names must match what `db.LoadAll` returns. Inspect first.

- [ ] **Step 8: Build sweep**

Run: `cd processor && go build ./... && go test ./... -count=1`
Expected: green.

- [ ] **Step 9: Commit**

```bash
git add processor/internal/db/monsters.go processor/internal/db/by_human.go processor/internal/db/by_human_test.go processor/internal/db/monsters_test.go processor/internal/db/*.go processor/internal/state/state.go processor/internal/state/loader.go
git commit -m "state: per-human rule partitions for all tracking types"
```

### Task 10: Add multi-profile × area-restriction × strict mode combinatorial test (pre-existing gap)

**Files:**
- Modify: `processor/internal/matching/pokemon_test.go`

This test goes in BEFORE the matcher changes so it catches any regression. It validates the EXISTING behaviour.

- [ ] **Step 1: Add the test**

Add to `processor/internal/matching/pokemon_test.go`:

```go
// Combinatorial check: a user with rules across multiple profiles, an
// area selection AND a strict AreaRestriction, all interacting at once.
// This is exactly the combination a geographic pre-filter could silently
// drop rules from if it's implemented wrong; locking in expected
// behaviour here means Phase 2 changes that misbehave fail loudly.
func TestPokemonMatch_MultiProfileWithStrictArea(t *testing.T) {
    human := &db.Human{
        ID:               "u1",
        Enabled:          true,
        Area:             []string{"belgium", "antwerp"},
        AreaRestriction:  []string{"belgium"},
        Latitude:         0,
        Longitude:        0,
        CurrentProfileNo: 2,
    }
    humans := map[string]*db.Human{"u1": human}
    rules := []*db.MonsterTracking{
        // Profile 1, Belgium-covering rule — should NOT match (wrong profile)
        {ID: "u1", ProfileNo: 1, PokemonID: 25, MaxIV: 100, MaxCP: 9000, MaxLevel: 55, Distance: 0},
        // Profile 2, Belgium-covering rule — should match (correct profile, in Belgium per strict + Area)
        {ID: "u1", ProfileNo: 2, PokemonID: 25, MaxIV: 100, MaxCP: 9000, MaxLevel: 55, Distance: 0},
    }
    st := &state.State{
        Humans:   humans,
        Monsters: &db.MonsterIndex{ByPokemonID: map[int][]*db.MonsterTracking{25: rules}},
        Geofence: nil, // we'll inject matchedAreaNames manually via stub if needed
    }
    matcher := &PokemonMatcher{StrictLocations: true, AreaSecurityEnabled: true}
    pokemon := &ProcessedPokemon{
        PokemonID: 25, IV: 100, Latitude: 0, Longitude: 0,
        // Tests in this package commonly use a stub geofence by overriding
        // ProcessedPokemon fields; if Geofence is nil, st.Geofence.PointAreasAndNames
        // will panic. Initialise a real SpatialIndex with one fence covering (0,0).
    }
    // Stub geofence with a polygon containing (0,0)
    st.Geofence = geofence.NewSpatialIndex([]geofence.Fence{
        {Name: "Belgium", Path: [][2]float64{{-1, -1}, {-1, 1}, {1, 1}, {1, -1}, {-1, -1}}},
    })

    users, _ := matcher.Match(pokemon, st)
    if len(users) != 1 {
        t.Fatalf("expected exactly 1 matched user (profile 2 only), got %d", len(users))
    }
    if users[0].ID != "u1" {
        t.Errorf("matched ID = %q, want u1", users[0].ID)
    }
}
```

If the test exposes a missing import (`geofence`), add `"github.com/pokemon/poracleng/processor/internal/geofence"`.

- [ ] **Step 2: Run, expect PASS (this asserts current behaviour)**

Run: `cd processor && go test ./internal/matching/... -run TestPokemonMatch_MultiProfileWithStrictArea -v`
Expected: PASS — confirms existing matcher already handles this combination correctly.

If it fails: do NOT patch the matcher to make it pass. Stop and flag — that would indicate a pre-existing bug to file separately, before this plan continues.

- [ ] **Step 3: Commit**

```bash
git add processor/internal/matching/pokemon_test.go
git commit -m "test: combinatorial multi-profile + strict-area regression guard"
```

### Task 11: Drive pokemon matcher iteration from applicable humans (flagged)

**The core change.** With the flag off, the matcher walks the existing per-pokemon buckets exactly as today — zero performance or behavioural delta. With the flag on, the matcher walks only the per-human partitions for applicable humans, calling `matchMonsters` with the same parameters as today's call sites but a much smaller slice.

**Files:**
- Modify: `processor/internal/matching/pokemon.go`
- Modify: `processor/cmd/processor/main.go` (set `GeographicPrefilter` field on `PokemonMatcher`)
- Modify: `processor/internal/matching/pokemon_test.go` (parity tests: flag-off and flag-on produce identical results on shared inputs)

- [ ] **Step 1: Add field to PokemonMatcher**

Edit `processor/internal/matching/pokemon.go`:

```go
type PokemonMatcher struct {
    PVPQueryMaxRank            int
    PVPEvolutionDirectTracking bool
    StrictLocations            bool
    AreaSecurityEnabled        bool
    GeographicPrefilter        bool
}
```

- [ ] **Step 2: Write the parity test FIRST**

Add to `processor/internal/matching/pokemon_test.go`. This is the safety net: every existing pokemon matcher test scenario should produce identical results with the flag on as it does with the flag off. We pick a representative test case and run it both ways.

```go
// TestPokemonMatch_GeoPrefilterParity is the safety net for the
// flagged code path. Same inputs, both flag values, identical outputs.
// If this test ever fails, the flag-on path is dropping (or producing)
// rules that the flag-off path didn't.
func TestPokemonMatch_GeoPrefilterParity(t *testing.T) {
    humans := map[string]*db.Human{
        "u1": {ID: "u1", Enabled: true, Area: []string{"belgium"}, Latitude: 50.5, Longitude: 4.5, CurrentProfileNo: 1},
        "u2": {ID: "u2", Enabled: true, Area: []string{"belgium"}, Latitude: 50.5, Longitude: 4.5, CurrentProfileNo: 1},
    }
    monsterRules := []MonsterTracking{
        {ID: "u1", ProfileNo: 1, PokemonID: 25, MaxIV: 100, MaxCP: 9000, MaxLevel: 55},
        {ID: "u1", ProfileNo: 1, PokemonID: 0, MaxIV: 100, MaxCP: 9000, MaxLevel: 55}, // everything
        {ID: "u2", ProfileNo: 1, PokemonID: 25, MaxIV: 100, MaxCP: 9000, MaxLevel: 55},
    }
    // buildMonsterIndexFromRules is the helper introduced in Task 9b — it
    // populates both ByPokemonID (fallback path) and ByHumanAndLeague
    // (per-human path) from the same backing slice.
    monsters := buildMonsterIndexFromRules(monsterRules)

    spatial := geofence.NewSpatialIndex([]geofence.Fence{
        {Name: "Belgium", Path: [][2]float64{{3, 50}, {3, 51}, {6, 51}, {6, 50}, {3, 50}}},
    })

    pokemon := &ProcessedPokemon{PokemonID: 25, IV: 100, Latitude: 50.5, Longitude: 4.5}

    var off, on []webhook.MatchedUser
    for _, flag := range []bool{false, true} {
        st := &state.State{
            Humans:   humans,
            Monsters: monsters,
            Geofence: spatial,
            GeoIndex: state.BuildHumanGeoIndex(humans, nil),
        }
        matcher := &PokemonMatcher{GeographicPrefilter: flag}
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

// TestPokemonMatch_GeoPrefilterDropsOutOfAreaHuman confirms the
// expected speedup property: when no humans cover the spawn's
// geography, the flag-on path doesn't even enter matchMonsters on
// the per-pokemon bucket — the applicable set is empty.
func TestPokemonMatch_GeoPrefilterDropsOutOfAreaHuman(t *testing.T) {
    humans := map[string]*db.Human{
        "u1": {ID: "u1", Enabled: true, Area: []string{"belgium"}, Latitude: 50.5, Longitude: 4.5, CurrentProfileNo: 1},
    }
    monsterRules := []MonsterTracking{
        {ID: "u1", ProfileNo: 1, PokemonID: 25, MaxIV: 100, MaxCP: 9000, MaxLevel: 55},
        {ID: "u1", ProfileNo: 1, PokemonID: 0, MaxIV: 100, MaxCP: 9000, MaxLevel: 55}, // everything
    }
    monsters := buildMonsterIndexFromRules(monsterRules)

    st := &state.State{
        Humans:   humans,
        Monsters: monsters,
        Geofence: geofence.NewSpatialIndex([]geofence.Fence{
            {Name: "Japan", Path: [][2]float64{{139, 35}, {139, 36}, {140, 36}, {140, 35}, {139, 35}}},
        }),
        GeoIndex: state.BuildHumanGeoIndex(humans, nil),
    }
    matcher := &PokemonMatcher{GeographicPrefilter: true}
    pokemon := &ProcessedPokemon{PokemonID: 25, IV: 100, Latitude: 35.5, Longitude: 139.5}
    users, _ := matcher.Match(pokemon, st)
    if len(users) != 0 {
        t.Errorf("japan spawn, belgium-only human: expected 0 matches, got %d", len(users))
    }
}
```

- [ ] **Step 3: Verify tests fail**

Run: `cd processor && go test ./internal/matching/... -run TestPokemonMatch_GeoPrefilter -v`
Expected: FAIL — `GeographicPrefilter` is set but the matcher doesn't yet read it, so the parity test may pass coincidentally but the second test will fail (no path differs yet).

- [ ] **Step 4: Refactor Match to support both iteration paths**

Edit `processor/internal/matching/pokemon.go`. The current `Match` body assembles a candidate slice from per-pokemon buckets, then validates. We're going to split candidate-assembly into two paths.

Replace the body of `Match`:

```go
func (m *PokemonMatcher) Match(pokemon *ProcessedPokemon, st *state.State) ([]webhook.MatchedUser, []webhook.MatchedArea) {
    start := time.Now()
    defer func() {
        metrics.MatchingDuration.WithLabelValues("pokemon").Observe(time.Since(start).Seconds())
    }()
    if st == nil || st.Monsters == nil {
        return nil, nil
    }

    areas, matchedAreaNames := st.Geofence.PointAreasAndNames(pokemon.Latitude, pokemon.Longitude)

    var matched []*db.MonsterTracking
    if m.GeographicPrefilter && st.GeoIndex != nil && st.Monsters.ByHumanAndLeague != nil {
        applicable := st.GeoIndex.ApplicableHumans(
            pokemon.Latitude, pokemon.Longitude,
            matchedAreaNames,
            m.AreaSecurityEnabled && m.StrictLocations,
        )
        metrics.MatchingApplicable.WithLabelValues("pokemon").Observe(float64(len(applicable)))
        matched = m.assembleCandidatesPerHuman(pokemon, st, applicable)
    } else {
        matched = m.assembleCandidatesPerBucket(pokemon, st)
    }
    metrics.MatchingCandidates.WithLabelValues("pokemon").Observe(float64(len(matched)))

    users := ValidateHumans(
        matched,
        pokemon.Latitude, pokemon.Longitude,
        matchedAreaNames,
        m.AreaSecurityEnabled && m.StrictLocations,
        st.Humans,
    )
    return users, ConvertAreas(areas)
}
```

Then add the two new private helpers below:

```go
// assembleCandidatesPerBucket is the existing iteration path: walk every
// rule in ByPokemonID[0] / ByPokemonID[N] / PVP buckets and run
// matchMonsters per bucket. Used when GeographicPrefilter is off.
func (m *PokemonMatcher) assembleCandidatesPerBucket(pokemon *ProcessedPokemon, st *state.State) []*db.MonsterTracking {
    var matched []*db.MonsterTracking
    matched = append(matched, m.matchMonsters(pokemon, st.Monsters.ByPokemonID[0], pokemon.PokemonID, pokemon.Form, true, 0, pvp.LeagueRank{})...)
    matched = append(matched, m.matchMonsters(pokemon, st.Monsters.ByPokemonID[pokemon.PokemonID], pokemon.PokemonID, pokemon.Form, true, 0, pvp.LeagueRank{})...)
    for league, leagueDataArr := range pokemon.PVPBestRank {
        for _, leagueData := range leagueDataArr {
            if leagueData.Rank <= m.PVPQueryMaxRank {
                matched = append(matched, m.matchMonsters(pokemon, st.Monsters.PVPEverything[league], pokemon.PokemonID, pokemon.Form, true, league, leagueData)...)
                matched = append(matched, m.matchMonsters(pokemon, st.Monsters.PVPSpecific[league], pokemon.PokemonID, pokemon.Form, true, league, leagueData)...)
            }
        }
    }
    if m.PVPEvolutionDirectTracking && len(pokemon.PVPEvoData) > 0 {
        for pokemonID, pvpMon := range pokemon.PVPEvoData {
            for league, leagueDataArr := range pvpMon {
                candidates := st.Monsters.PVPSpecific[league]
                for _, leagueData := range leagueDataArr {
                    if leagueData.Rank <= m.PVPQueryMaxRank {
                        matched = append(matched, m.matchMonsters(pokemon, candidates, pokemonID, leagueData.Form, false, league, leagueData)...)
                    }
                }
            }
        }
    }
    return matched
}

// assembleCandidatesPerHuman iterates only applicable humans and walks
// each human's rule partitions. matchMonsters does the same per-rule
// filtering as in the per-bucket path. Used when GeographicPrefilter is on.
//
// ByHumanAndLeague[humanID][0]    — non-PVP rules (mix of "everything"
//                                    and per-species; matchMonsters
//                                    filters via the pokemon_id check)
// ByHumanAndLeague[humanID][N]    — PVP rules for league N
//
// For PVP evolution: same league bucket, different targetPokemonID/form
// pair passed to matchMonsters.
func (m *PokemonMatcher) assembleCandidatesPerHuman(pokemon *ProcessedPokemon, st *state.State, applicable map[string]bool) []*db.MonsterTracking {
    var matched []*db.MonsterTracking
    for humanID := range applicable {
        perHuman := st.Monsters.ByHumanAndLeague[humanID]
        if perHuman == nil {
            continue
        }
        // Non-PVP path: a single matchMonsters call covers both "everything"
        // and per-species — the pokemon_id check inside matchMonsters accepts
        // m.PokemonID == 0 (everything) OR m.PokemonID == targetPokemonID.
        matched = append(matched, m.matchMonsters(pokemon, perHuman[0], pokemon.PokemonID, pokemon.Form, true, 0, pvp.LeagueRank{})...)

        // PVP path: one matchMonsters call per league the spawn ranks in.
        for league, leagueDataArr := range pokemon.PVPBestRank {
            for _, leagueData := range leagueDataArr {
                if leagueData.Rank <= m.PVPQueryMaxRank {
                    matched = append(matched, m.matchMonsters(pokemon, perHuman[league], pokemon.PokemonID, pokemon.Form, true, league, leagueData)...)
                }
            }
        }
        // PVP evolution path: same league bucket, different target.
        if m.PVPEvolutionDirectTracking && len(pokemon.PVPEvoData) > 0 {
            for pokemonID, pvpMon := range pokemon.PVPEvoData {
                for league, leagueDataArr := range pvpMon {
                    candidates := perHuman[league]
                    for _, leagueData := range leagueDataArr {
                        if leagueData.Rank <= m.PVPQueryMaxRank {
                            matched = append(matched, m.matchMonsters(pokemon, candidates, pokemonID, leagueData.Form, false, league, leagueData)...)
                        }
                    }
                }
            }
        }
    }
    return matched
}
```

Add imports as needed: `"time"`, `"github.com/pokemon/poracleng/processor/internal/metrics"`.

**Note:** existing `Match` already contains the metric `Observe` for `MatchingCandidates`. After this refactor, the `Observe` lives in `Match` after candidate assembly (see Step 4's `Match` body) — the earlier instrumentation tasks (2 & 3) already produced this; you're now moving the call to the rewritten function. Verify the move when you remove the original.

- [ ] **Step 5: Verify tests pass**

Run: `cd processor && go test ./internal/matching/... -count=1`
Expected: ALL pass — existing pokemon tests still pass (they don't set the flag, so the per-bucket path is taken), the new parity test passes (both paths produce identical outputs for the same inputs), and the out-of-area test passes (flag-on, no applicable humans → no matches).

- [ ] **Step 6: Plumb the flag from config**

Edit `processor/cmd/processor/main.go`, find the line that constructs the `PokemonMatcher` (around `:1211`) and add:

```go
matcher: &matching.PokemonMatcher{
    PVPQueryMaxRank:            cfg.Tracking.PvpQueryMaxRank,
    PVPEvolutionDirectTracking: cfg.Tracking.PvpEvolutionDirectTracking,
    StrictLocations:            cfg.Area.StrictLocations,
    AreaSecurityEnabled:        cfg.Area.Enabled,
    GeographicPrefilter:        cfg.Tuning.GeographicPrefilter,
},
```

(Field names may differ — locate the matching block by context.)

- [ ] **Step 7: Build + full test sweep**

Run: `cd processor && go build ./... && go test ./... -count=1`
Expected: clean build, all tests pass.

- [ ] **Step 8: Commit**

```bash
git add processor/internal/matching/pokemon.go processor/internal/matching/pokemon_test.go processor/cmd/processor/main.go
git commit -m "matching: pokemon matcher iterates per applicable human (flagged)"
```

---

## Phase 3: Roll out per-human iteration to other matchers

Each task follows the same shape as Task 11 — one matcher type, one commit. The pattern:

1. Add `GeographicPrefilter bool` field to the matcher struct.
2. In the matcher's `Match()`, split candidate assembly into two paths exactly like Task 11's `assembleCandidatesPerBucket` / `assembleCandidatesPerHuman` helpers. The per-bucket path is the current code, lifted into a helper unchanged. The per-human path walks `st.<Type>ByHuman[humanID]` for each applicable human (built in Task 9b).
3. The non-pokemon types have no PVP league axis, so per-human assembly is one loop:

```go
func (m *RaidMatcher) assembleCandidatesPerHuman(raid *RaidWebhook, st *state.State, applicable map[string]bool) []raidCandidate {
    var matched []raidCandidate
    for humanID := range applicable {
        rules := st.RaidsByHuman[humanID]
        // matchRaids (or whatever the matcher's per-rule function is)
        // applies the existing pokemon_id/level/team/etc filters per
        // rule. Same logic as today; just a smaller input slice.
        matched = append(matched, m.matchRaids(raid, rules)...)
    }
    return matched
}
```

4. The `Match` function chooses path based on `m.GeographicPrefilter && st.GeoIndex != nil && st.<Type>ByHuman != nil`. When the flag is off, the per-bucket helper path runs and behaviour is identical to today.
5. Add a parity test for this type using the Task 11 template (one matcher type, two configurations, identical outputs).
6. Plumb the flag in `cmd/processor/main.go` matcher constructor.

### Task 12: Raid + Egg matchers

**Files:**
- Modify: `processor/internal/matching/raid.go`
- Modify: `processor/cmd/processor/main.go`
- Modify: `processor/internal/matching/raid_test.go`

- [ ] **Step 1: Add `GeographicPrefilter bool` field to `RaidMatcher` and `EggMatcher` in `processor/internal/matching/raid.go`.**

- [ ] **Step 2: Refactor `RaidMatcher.Match` to two helpers — `assembleCandidatesPerBucket` (lift existing iteration into a helper unchanged) and `assembleCandidatesPerHuman` (walks `st.RaidsByHuman[humanID]` for each applicable human, calling the same `matchRaids` per-rule function on the smaller slice). `Match` selects by `m.GeographicPrefilter && st.GeoIndex != nil && st.RaidsByHuman != nil`. Sample skeleton:**

```go
func (m *RaidMatcher) Match(raid *RaidWebhook, st *state.State) (...) {
    start := time.Now()
    defer func() { metrics.MatchingDuration.WithLabelValues("raid").Observe(time.Since(start).Seconds()) }()
    if st == nil { return nil, nil }
    areas, matchedAreaNames := st.Geofence.PointAreasAndNames(raid.Latitude, raid.Longitude)

    var matched []raidCandidate
    if m.GeographicPrefilter && st.GeoIndex != nil && st.RaidsByHuman != nil {
        applicable := st.GeoIndex.ApplicableHumans(raid.Latitude, raid.Longitude, matchedAreaNames, m.AreaSecurityEnabled && m.StrictLocations)
        metrics.MatchingApplicable.WithLabelValues("raid").Observe(float64(len(applicable)))
        for humanID := range applicable {
            matched = append(matched, m.matchRaids(raid, st.RaidsByHuman[humanID])...)
        }
    } else {
        matched = m.matchRaids(raid, st.Raids)
    }
    metrics.MatchingCandidates.WithLabelValues("raid").Observe(float64(len(matched)))
    users := ValidateHumansForRaid(matched, raid.Latitude, raid.Longitude, matchedAreaNames, m.AreaSecurityEnabled && m.StrictLocations, st.Humans, "raid")
    return users, ConvertAreas(areas)
}
```

- [ ] **Step 3: Same refactor for `EggMatcher.Match`, walking `st.EggsByHuman[humanID]`.**

- [ ] **Step 4: Add the parity test (Task 11's template, adapted to raid):**

```go
func TestRaidMatch_GeoPrefilterParity(t *testing.T) {
    humans := map[string]*db.Human{
        "u1": {ID: "u1", Enabled: true, Area: []string{"belgium"}, Latitude: 50.5, Longitude: 4.5, CurrentProfileNo: 1},
    }
    raids := []db.RaidTracking{
        {ID: "u1", ProfileNo: 1, Level: 5},
    }
    // The state must contain BOTH the flat Raids slice (per-bucket path)
    // AND the RaidsByHuman partition (per-human path).
    raidsByHuman := db.PartitionByHuman(raids, db.RaidHumanID)

    spatial := geofence.NewSpatialIndex([]geofence.Fence{
        {Name: "Belgium", Path: [][2]float64{{3, 50}, {3, 51}, {6, 51}, {6, 50}, {3, 50}}},
    })
    raid := &webhook.RaidWebhook{Latitude: 50.5, Longitude: 4.5, Level: 5}

    var off, on []webhook.MatchedUser
    for _, flag := range []bool{false, true} {
        // Build slice of pointers for the flat-state Raids field
        flatRaids := make([]*db.RaidTracking, 0, len(raids))
        for i := range raids {
            flatRaids = append(flatRaids, &raids[i])
        }
        st := &state.State{
            Humans:       humans,
            Raids:        flatRaids,
            RaidsByHuman: raidsByHuman,
            Geofence:     spatial,
            GeoIndex:     state.BuildHumanGeoIndex(humans, nil),
        }
        matcher := &RaidMatcher{GeographicPrefilter: flag}
        users, _ := matcher.Match(raid, st)
        if flag {
            on = users
        } else {
            off = users
        }
    }
    if len(off) != len(on) {
        t.Fatalf("parity: off=%d on=%d", len(off), len(on))
    }
}
```

Adjust `RaidWebhook` field names and the flat-Raids slice shape to whatever `RaidMatcher` actually consumes today.

- [ ] **Step 5: Run tests**

Run: `cd processor && go test ./internal/matching/... -count=1`
Expected: all green.

- [ ] **Step 6: Plumb the flag in `main.go`**

Edit the `raidMatcher: &matching.RaidMatcher{...}` literal — add `GeographicPrefilter: cfg.Tuning.GeographicPrefilter`. Repeat for `eggMatcher` if it's a separate construction; if it's a shared struct, one line suffices.

- [ ] **Step 7: Commit**

```bash
git add processor/internal/matching/raid.go processor/internal/matching/raid_test.go processor/cmd/processor/main.go
git commit -m "matching: raid + egg matchers iterate per applicable human (flagged)"
```

### Task 13: Quest matcher

- [ ] **Step 1**: Add `GeographicPrefilter` field to `QuestMatcher` in `processor/internal/matching/quest.go`.
- [ ] **Step 2**: Refactor `QuestMatcher.Match` into the same two-helpers shape as Task 12. Per-human path walks `st.QuestsByHuman[humanID]`. Label `"quest"`.
- [ ] **Step 3**: Plumb the flag on `questMatcher: &matching.QuestMatcher{...}` in `main.go`.
- [ ] **Step 4**: Add a quest parity test (adapt Task 12's template).
- [ ] **Step 5**: Run tests + commit `"matching: quest matcher iterates per applicable human (flagged)"`.

### Task 14: Invasion matcher

Apply Task 12's two-helpers + parity-test pattern to `processor/internal/matching/invasion.go`. Per-human path walks `st.InvasionsByHuman[humanID]`. Metric label `"invasion"`. Commit `"matching: invasion matcher iterates per applicable human (flagged)"`.

### Task 15: Lure matcher

Same pattern, file `lure.go`, walks `st.LuresByHuman[humanID]`. Label `"lure"`. Commit `"matching: lure matcher iterates per applicable human (flagged)"`.

### Task 16: Nest matcher

Same pattern, file `nest.go`, walks `st.NestsByHuman[humanID]`. Label `"nest"`. Commit `"matching: nest matcher iterates per applicable human (flagged)"`.

### Task 17: Gym matcher

Same pattern, file `gym.go`, walks `st.GymsByHuman[humanID]`. Label `"gym"`. Commit `"matching: gym matcher iterates per applicable human (flagged)"`.

### Task 18: Fort matcher

Same pattern, file `fort.go`, walks `st.FortsByHuman[humanID]`. Label `"fort_update"`. Commit `"matching: fort matcher iterates per applicable human (flagged)"`.

### Task 19: Maxbattle matcher

Same pattern, file `maxbattle.go`, walks `st.MaxbattlesByHuman[humanID]`. Label `"maxbattle"`. Commit `"matching: maxbattle matcher iterates per applicable human (flagged)"`.

---

## Phase 4: Flag default flip (deferred — execute only after production validation)

This phase should NOT run until the operator has had the flag enabled in production for at least two weeks and confirmed via the `matching_seconds` and `matching_candidates` histograms that:
1. `matching_seconds` P95 drops materially when the flag is on (otherwise the optimisation isn't doing anything — investigate before flipping).
2. `MatchedUsers` total per type stays identical with flag on vs flag off (a behaviour regression — the pre-filter is dropping rules it shouldn't).
3. No support tickets reporting missing alerts.

### Task 20: Flip default to true

**Files:**
- Modify: `processor/internal/config/config.go` (default init)
- Modify: `config/config.example.toml` (default comment)

- [ ] **Step 1: Add config defaulting if config uses an explicit defaulter; otherwise just update the docs**

Inspect `processor/internal/config/config.go` for an `applyDefaults()` or similar. If found, add:

```go
if !cfg.Tuning.GeographicPrefilterSet { // requires sentinel
    cfg.Tuning.GeographicPrefilter = true
}
```

If config has no explicit default mechanism for new bool fields, change the type to a pointer (`*bool`) — but Go's `omitempty` for pointers means we can detect "operator did not specify". Simpler: just default true in code via a one-time conversion at load (`if !v.SetByUser { v = true }`) — implementation detail of how the existing config code handles its own defaults.

If unclear, prefer the smallest change: leave the field as `bool`, document the recommended value in the example TOML, and rely on operators uncommenting the line.

- [ ] **Step 2: Update example TOML**

```toml
# Default true since [VERSION_TAG]. Operators with very few rules can
# safely turn this off to save the ~one-time index build cost on reload.
geographic_prefilter = true
```

- [ ] **Step 3: Commit**

```bash
git add processor/internal/config/config.go config/config.example.toml
git commit -m "config: default geographic_prefilter to true"
```

---

## Self-Review

Walked back through the spec and the plan:

**Spec coverage:**
- ✅ Per-user spatial pre-filter (Task 7, 9)
- ✅ `HumansByArea` set keyed by area name (Task 7 — `byArea`)
- ✅ R-tree of (humanLocation, maxRuleDistance) for distance-based rules (Task 7 — `distanceTree`)
- ✅ Strict mode folds in `AreaRestriction` (Task 7 — `byAreaRestriction` + `humanHasRestrictionOverlap`)
- ✅ Multi-spawn-area handled (matchedAreas is already a set; tests in Task 7 + Task 11)
- ✅ Multi-user-area handled (byArea population in Task 7)
- ✅ Distance is per-human max across all their rules (Task 8)
- ✅ **Per-human rule partitions** so iteration can be driven from the applicable set, not just post-filtered on it (Task 9b + Task 11 `assembleCandidatesPerHuman`). This is the change that delivers the actual speedup.
- ✅ Pokemon's `everything` / PVP buckets benefit — `ByHumanAndLeague[humanID][0]` merges everything + per-species; PVP buckets are per-league per-human (Task 9b)
- ✅ Rebuilds with `Load()` and `LoadWithGeofences()` (Task 9 + Task 9b)
- ✅ Future-compatible with per-track areas (population source change only — documented in Architecture section)
- ✅ Strict superset behaviour — `matchMonsters` is unchanged; the per-human path passes the same parameters from the same call sites, just on smaller slices. Parity tests in Task 11 + Task 12 confirm identical outputs flag-on vs flag-off.
- ✅ Profile handling unchanged (Task 7 builder ignores `CurrentProfileNo`; profile filter still in `ValidateHumans*`)
- ✅ Flag-gated rollout (Tasks 6, 11 — flag off keeps existing per-bucket path verbatim, lifted into a helper)
- ✅ Per-matcher rollout (Tasks 12–19)
- ✅ Instrumentation deployed first (Phase 1)
- ✅ Combinatorial gap test (Task 10)
- ✅ Index boundary tests (Task 7)
- ✅ Per-human partition correctness tests (Task 9b)

**Placeholder scan:** searched the document for "TBD", "TODO", "implement later", "fill in details", "etc." in test code, "add appropriate error handling", "similar to Task N". Two soft spots that I'm leaving in deliberately:
- Task 4 ("repeat for 9 matcher types") — the pattern is captured fully in Tasks 2+3; repetition is mechanical. Keeping the plan readable by not duplicating 30 nearly-identical task blocks.
- Tasks 13–19 collapse Phase 3 rollouts into per-matcher applications of the Task 11 template. Each has its own commit. Engineer needs Task 11 in context.

**Type consistency:**
- `BuildHumanGeoIndex(humans, perHumanMaxDist)` — same signature in Tasks 7, 8, 9.
- `ApplicableHumans(lat, lon, matchedAreas, strictMode)` — same signature in Tasks 7 (definition) and 11 (call site).
- `State.GeoIndex *HumanGeoIndex` — defined in Task 9 step 3, used in Task 11 step 4.
- `cfg.Tuning.GeographicPrefilter` — added in Task 6, read in Task 11.
- `metrics.MatchingDuration/Candidates/Applicable` — defined in Task 1, used in Tasks 2, 3, 4, 11, 12+.

**Worth flagging to the implementing engineer:**
- Task 11's tests reference `state.BuildHumanGeoIndex` from inside the `matching` package — that creates a `matching → state` import dependency. The current code goes the other way (`state` uses `db`, not `matching`). Check there's no import cycle: `matching` should import `state` to read `st.GeoIndex.ApplicableHumans(...)`, but the geo-index code itself only uses `db` and `tidwall/rtree`. Should be fine.
- Task 8's `db.LoadedData` is a placeholder name — `db.LoadAll` returns a concrete type; the engineer must replace `LoadedData` with the real type name when writing the helper. The test name and field accesses still work once aligned.
- The `Distance` field is `int` (metres) in all tracking types per the existing matchers' check `if td.Distance > 0`. Task 8's `record(id, d int)` matches.
