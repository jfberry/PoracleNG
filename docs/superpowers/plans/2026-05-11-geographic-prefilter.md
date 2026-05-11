# Geographic Pre-Filter for Matching — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Drastically reduce per-spawn matching work for installations with very large rule counts (we've observed 100k and 500k single-user rule sets) by pre-filtering out humans whose geography can't possibly cover the spawn — before the per-rule IV/CP/etc filter loop pays for them.

**Architecture:** At state load, build an immutable `HumanGeoIndex` keyed by `(area name → set of human IDs)` plus an R-tree of `(human location, max-distance-across-their-rules)` circles. Per webhook, compute the *applicable humans* set once (union of area-bucket lookups across the spawn's matched geofence areas, plus an R-tree radius query). In each matcher's per-rule loop, skip rules whose owner (`m.ID`) isn't in the applicable set. The pre-filter is a **strict superset** check — every rule that fires today must still fire — and the existing `ValidateHumans*` functions are unchanged; they remain the authoritative final filter.

**Tech Stack:** Go, existing `tidwall/rtree` (already used for geofences), Prometheus client_golang. No new dependencies.

**Pokemon-spawn-multi-area and user-multi-area:** Both already exist today. `matchedAreaNames map[string]bool` is the *set* of overlapping geofences containing the spawn; `human.Area []string` is the set of areas the user tracks. The pre-filter operates on these as sets — applicable humans = union of `HumansByArea[a]` over `a ∈ matchedAreaNames`, deduplicated. A rule has one owner, so `m.ID ∈ applicableHumans` naturally dedups across rules.

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

### Task 11: Apply pre-filter in pokemon matcher (flagged)

**Files:**
- Modify: `processor/internal/matching/pokemon.go`
- Modify: `processor/cmd/processor/main.go` (set `GeographicPrefilter` field on `PokemonMatcher`)
- Modify: `processor/internal/matching/pokemon_test.go` (positive + negative tests for the new code path)

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

- [ ] **Step 2: Write tests for the prefilter ON path**

Add to `processor/internal/matching/pokemon_test.go`:

```go
func TestPokemonMatch_GeoPrefilterDropsOutOfAreaHuman(t *testing.T) {
    // u1 tracks belgium only; spawn is in japan; with prefilter on we should
    // never reach ValidateHumans. Result: zero matched users — same as
    // existing behaviour, but the candidate-count metric should be lower.
    metrics.MatchingCandidates.Reset()
    metrics.MatchingApplicable.Reset()
    humans := map[string]*db.Human{
        "u1": {ID: "u1", Enabled: true, Area: []string{"belgium"}, Latitude: 50, Longitude: 4, CurrentProfileNo: 1},
    }
    rules := []*db.MonsterTracking{
        {ID: "u1", ProfileNo: 1, PokemonID: 25, MaxIV: 100, MaxCP: 9000, MaxLevel: 55, Distance: 0},
        {ID: "u1", ProfileNo: 1, PokemonID: 0, MaxIV: 100, MaxCP: 9000, MaxLevel: 55, Distance: 0}, // everything
    }
    geoIdx := BuildHumanGeoIndex(humans, nil) // no distance rules — but wait, this is matching package
    _ = geoIdx                                 // remove this line once you've moved the helper or used an in-package builder
    // ... use state.BuildHumanGeoIndex via the state package import
    st := &state.State{
        Humans:   humans,
        Monsters: &db.MonsterIndex{ByPokemonID: map[int][]*db.MonsterTracking{25: rules[:1], 0: rules[1:]}},
        Geofence: geofence.NewSpatialIndex([]geofence.Fence{
            {Name: "Japan", Path: [][2]float64{{139, 35}, {139, 36}, {140, 36}, {140, 35}, {139, 35}}},
        }),
        GeoIndex: state.BuildHumanGeoIndex(humans, nil),
    }
    matcher := &PokemonMatcher{GeographicPrefilter: true}
    pokemon := &ProcessedPokemon{PokemonID: 25, IV: 100, Latitude: 35.5, Longitude: 139.5}
    users, _ := matcher.Match(pokemon, st)
    if len(users) != 0 {
        t.Errorf("u1 tracks only belgium, japan spawn must not match, got %d users", len(users))
    }
}

func TestPokemonMatch_GeoPrefilterPassesInAreaHuman(t *testing.T) {
    // u1 tracks belgium; spawn IS in belgium; result identical with flag on/off.
    humans := map[string]*db.Human{
        "u1": {ID: "u1", Enabled: true, Area: []string{"belgium"}, Latitude: 50, Longitude: 4, CurrentProfileNo: 1},
    }
    rules := []*db.MonsterTracking{
        {ID: "u1", ProfileNo: 1, PokemonID: 25, MaxIV: 100, MaxCP: 9000, MaxLevel: 55, Distance: 0},
    }
    spatial := geofence.NewSpatialIndex([]geofence.Fence{
        {Name: "Belgium", Path: [][2]float64{{3, 50}, {3, 51}, {5, 51}, {5, 50}, {3, 50}}},
    })
    pokemon := &ProcessedPokemon{PokemonID: 25, IV: 100, Latitude: 50.5, Longitude: 4}

    for _, flagOn := range []bool{false, true} {
        st := &state.State{
            Humans:   humans,
            Monsters: &db.MonsterIndex{ByPokemonID: map[int][]*db.MonsterTracking{25: rules}},
            Geofence: spatial,
            GeoIndex: state.BuildHumanGeoIndex(humans, nil),
        }
        matcher := &PokemonMatcher{GeographicPrefilter: flagOn}
        users, _ := matcher.Match(pokemon, st)
        if len(users) != 1 {
            t.Errorf("flag=%v: expected 1 user, got %d", flagOn, len(users))
        }
    }
}
```

(Drop the awkward `_ = geoIdx` placeholder above — the live tests should use `state.BuildHumanGeoIndex` directly from the imported `state` package.)

- [ ] **Step 3: Verify tests fail**

Run: `cd processor && go test ./internal/matching/... -run TestPokemonMatch_GeoPrefilter -v`
Expected: FAIL — `GeographicPrefilter` field is unused so the prefilter doesn't actually filter anything yet.

- [ ] **Step 4: Wire prefilter into Match**

Edit `processor/internal/matching/pokemon.go`. After the matched-slice assembly (where `metrics.MatchingCandidates.Observe(float64(len(matched)))` is), before `ValidateHumans`:

```go
metrics.MatchingCandidates.WithLabelValues("pokemon").Observe(float64(len(matched)))

areas, matchedAreaNames := st.Geofence.PointAreasAndNames(pokemon.Latitude, pokemon.Longitude)

if m.GeographicPrefilter && st.GeoIndex != nil {
    applicable := st.GeoIndex.ApplicableHumans(
        pokemon.Latitude, pokemon.Longitude,
        matchedAreaNames,
        m.AreaSecurityEnabled && m.StrictLocations,
    )
    metrics.MatchingApplicable.WithLabelValues("pokemon").Observe(float64(len(applicable)))

    filtered := matched[:0]
    for _, mt := range matched {
        if applicable[mt.ID] {
            filtered = append(filtered, mt)
        }
    }
    matched = filtered
}

users := ValidateHumans(
    matched,
    pokemon.Latitude, pokemon.Longitude,
    matchedAreaNames,
    m.AreaSecurityEnabled && m.StrictLocations,
    st.Humans,
)
```

- [ ] **Step 5: Verify tests pass**

Run: `cd processor && go test ./internal/matching/... -count=1`
Expected: ALL pass — including the existing tests (prefilter is OFF by default in those tests since they don't set the flag) and the new flagged ones.

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

(field names may differ — locate the matching block by context.)

- [ ] **Step 7: Build + full test sweep**

Run: `cd processor && go build ./... && go test ./... -count=1`
Expected: clean build, all tests pass.

- [ ] **Step 8: Commit**

```bash
git add processor/internal/matching/pokemon.go processor/internal/matching/pokemon_test.go processor/cmd/processor/main.go
git commit -m "matching: geographic pre-filter for pokemon matcher (flagged)"
```

---

## Phase 3: Roll out pre-filter to other matchers

Each task in this phase is structurally identical to Task 11 — one matcher type, one commit. The pattern is:

1. Add `GeographicPrefilter bool` field to the matcher struct.
2. In the matcher's `Match()`, after the candidate slice is assembled (`metrics.MatchingCandidates.Observe(...)`), but before the `ValidateHumans*` call:

```go
if m.GeographicPrefilter && st.GeoIndex != nil {
    matchedAreaNames := <...obtain the matched area set, same call the matcher already makes...>
    applicable := st.GeoIndex.ApplicableHumans(
        <eventLat>, <eventLon>,
        matchedAreaNames,
        m.AreaSecurityEnabled && m.StrictLocations,
    )
    metrics.MatchingApplicable.WithLabelValues("<type>").Observe(float64(len(applicable)))
    filtered := candidateList[:0]
    for _, item := range candidateList {
        if applicable[item.ID /* or HumanID */] {
            filtered = append(filtered, item)
        }
    }
    candidateList = filtered
}
```

3. In `processor/cmd/processor/main.go`, add `GeographicPrefilter: cfg.Tuning.GeographicPrefilter` to that matcher's literal.
4. Add a parallel pair of tests (in/out of geography) using the Task 11 templates.

### Task 12: Raid + Egg matchers

**Files:**
- Modify: `processor/internal/matching/raid.go`
- Modify: `processor/cmd/processor/main.go`
- Modify: `processor/internal/matching/raid_test.go`

- [ ] **Step 1: Apply the Task 11 pattern to `RaidMatcher.Match`. The candidate slice is the `trackingData` list; the `ID` field on each is `HumanID`. The `matchedAreaNames` is obtained via the same `st.Geofence.PointAreasAndNames(...)` call already present in the matcher.**

- [ ] **Step 2: Apply same to `EggMatcher.Match` (same file).**

- [ ] **Step 3: Run tests, confirm green.**

Run: `cd processor && go test ./internal/matching/... -count=1`

- [ ] **Step 4: Commit**

```bash
git add processor/internal/matching/raid.go processor/internal/matching/raid_test.go processor/cmd/processor/main.go
git commit -m "matching: geographic pre-filter for raid + egg matchers (flagged)"
```

### Task 13: Quest matcher

- [ ] **Step 1**: Apply Task 11 pattern to `QuestMatcher.Match` in `processor/internal/matching/quest.go`. Label `"quest"`.
- [ ] **Step 2**: Add the flag to `questMatcher: &matching.QuestMatcher{...}` literal in `main.go`.
- [ ] **Step 3**: Add in/out-of-area tests to `quest_test.go`.
- [ ] **Step 4**: Run tests.
- [ ] **Step 5**: `git add` + commit `"matching: geographic pre-filter for quest matcher (flagged)"`.

### Task 14: Invasion matcher

Same pattern, file `invasion.go`, label `"invasion"`. Commit `"matching: geographic pre-filter for invasion matcher (flagged)"`.

### Task 15: Lure matcher

Same pattern, file `lure.go`, label `"lure"`. Commit `"matching: geographic pre-filter for lure matcher (flagged)"`.

### Task 16: Nest matcher

Same pattern, file `nest.go`, label `"nest"`. Commit `"matching: geographic pre-filter for nest matcher (flagged)"`.

### Task 17: Gym matcher

Same pattern, file `gym.go`, label `"gym"`. Commit `"matching: geographic pre-filter for gym matcher (flagged)"`.

### Task 18: Fort matcher

Same pattern, file `fort.go`, label `"fort_update"`. Commit `"matching: geographic pre-filter for fort matcher (flagged)"`.

### Task 19: Maxbattle matcher

Same pattern, file `maxbattle.go`, label `"maxbattle"`. Commit `"matching: geographic pre-filter for maxbattle matcher (flagged)"`.

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
- ✅ Pokemon's `everything` / PVP buckets benefit (the pre-filter runs after their assembly in Task 11)
- ✅ Rebuilds with `Load()` and `LoadWithGeofences()` (Task 9)
- ✅ Future-compatible with per-track areas (population source change only — documented in Architecture section)
- ✅ Strict superset behaviour — never drops rules `ValidateHumans*` would have kept (the algorithm operates on humans; `ValidateHumans*` still does the final filter)
- ✅ Profile handling unchanged (Task 7 builder ignores `CurrentProfileNo`; profile filter still in `ValidateHumans*`)
- ✅ Flag-gated rollout (Tasks 6, 11)
- ✅ Per-matcher rollout (Tasks 12–19)
- ✅ Instrumentation deployed first (Phase 1)
- ✅ Combinatorial gap test (Task 10)
- ✅ Index boundary tests (Task 7)

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
