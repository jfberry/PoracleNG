package tracker

import (
	"maps"
	"math"
	"sort"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// Rarity group constants matching the alerter's conventions.
const (
	RarityUnknown   = -1
	RarityCommon    = 1
	RarityUncommon  = 2
	RarityRare      = 3
	RarityVeryRare  = 4
	RarityUltraRare = 5
	RarityNever     = 6

	// minIVSeenForShiny is the minimum number of IV-scanned encounters
	// required before reporting shiny stats for a species.
	minIVSeenForShiny = 100
)

// StatsConfig holds configurable rarity/shiny thresholds.
type StatsConfig struct {
	MinSampleSize       int
	WindowHours         int
	RefreshIntervalMins int
	Uncommon            float64 // percentage threshold for uncommon
	Rare                float64
	VeryRare            float64
	UltraRare           float64
}

// speciesCounts aggregates one species' sightings inside a single time bucket.
type speciesCounts struct {
	all   int32
	iv    int32 // sightings that carried IV data (a full encounter)
	shiny int32 // IV-scanned sightings confirmed shiny
}

// statsBucket holds one minute of aggregated sightings. Storing counters per
// (minute, species) instead of one record per sighting is what keeps this
// tracker's memory bounded: size is a function of the window length and the
// number of distinct species, never of webhook throughput.
//
// This ring is mechanically the same as webhook.RateCounter's: slot index is
// (unix/60 % len), a slot whose stored minute differs is stale and reset in
// place rather than allocated, and reads filter by age instead of sweeping.
// The two differ in what they hold (per-species counters here, per-type counts
// there) and in whether the window is configurable, which is why they are not
// one type today. Keep them in step: a boundary bug found in one is almost
// certainly present in the other.
type statsBucket struct {
	minute  int64 // wall-clock minute this bucket covers; -1 when unused
	total   int64
	species map[int32]speciesCounts
}

// reset re-points a bucket at a new minute, dropping whatever it held. The
// map is cleared rather than reallocated so the ring reaches a steady state
// and stops allocating entirely.
func (b *statsBucket) reset(minute int64) {
	b.minute = minute
	b.total = 0
	if b.species == nil {
		b.species = make(map[int32]speciesCounts)
		return
	}
	clear(b.species)
}

// ShinyStats holds the stats for a single pokemon.
type ShinyStats struct {
	Total int64   `json:"total"`
	Seen  int64   `json:"seen"`
	Ratio float64 `json:"ratio"` // total / seen (e.g. 512 means 1:512)
}

// StatsTracker maintains rolling pokemon sighting counts and computes
// rarity groups and shiny stats within the same time window.
type StatsTracker struct {
	mu      sync.RWMutex
	cfg     StatsConfig
	buckets []statsBucket      // ring of per-minute counters covering the window
	groups  map[int]int        // pokemon_id -> rarity group
	shiny   map[int]ShinyStats // pokemon_id -> shiny stats (cached)

	// nowFunc is the clock, injectable so window expiry can be tested
	// without sleeping. Defaults to time.Now. Set once at construction and
	// never reassigned: recalcLoop reads it from its own goroutine, so a
	// later write would be a data race.
	nowFunc func() time.Time
}

// NewStatsTracker creates a new stats tracker with the given config.
func NewStatsTracker(cfg StatsConfig) *StatsTracker {
	return newStatsTrackerWithClock(cfg, time.Now)
}

// newStatsTrackerWithClock creates a tracker with an injectable clock, used in
// tests to control the rolling window without wall-clock dependencies.
// Mirrors newRateCounterWithClock in internal/webhook.
//
// The clock is a constructor parameter rather than an exported field because
// recalcLoop starts here: anything assigned afterwards would be written by the
// caller while that goroutine reads it.
func newStatsTrackerWithClock(cfg StatsConfig, nowFunc func() time.Time) *StatsTracker {
	st := &StatsTracker{
		cfg:     cfg,
		buckets: newBucketRing(cfg.WindowHours),
		groups:  make(map[int]int),
		shiny:   make(map[int]ShinyStats),
		nowFunc: nowFunc,
	}
	go st.recalcLoop()
	return st
}

// newBucketRing sizes the ring to cover the whole window inclusively: one
// bucket per minute plus the current partial minute.
func newBucketRing(windowHours int) []statsBucket {
	count := windowMinutes(windowHours) + 1
	ring := make([]statsBucket, count)
	for i := range ring {
		ring[i].minute = -1
	}
	return ring
}

// windowMinutes is the configured rolling window expressed in whole minutes,
// floored at one so a misconfigured zero still yields a usable ring.
func windowMinutes(windowHours int) int {
	if windowHours < 1 {
		return 1
	}
	return windowHours * 60
}

// RecordSighting records a pokemon sighting. ivScanned indicates whether the
// pokemon had IV data (a full encounter). isShiny indicates a confirmed shiny.
func (st *StatsTracker) RecordSighting(pokemonID int, ivScanned bool, isShiny bool) {
	// Bucket keys are int32 to halve the map key width. POST / is
	// unauthenticated and does not range-check pokemon_id, so an
	// out-of-range value would wrap to a negative key and surface in the
	// rarity and shiny exports as a nonsense species. Drop it instead.
	if pokemonID <= 0 || pokemonID > math.MaxInt32 {
		return
	}

	st.mu.Lock()
	defer st.mu.Unlock()

	minute := st.nowFunc().Unix() / 60
	b := &st.buckets[int(minute%int64(len(st.buckets)))]
	if b.minute != minute {
		b.reset(minute)
	}

	c := b.species[int32(pokemonID)]
	c.all++
	if ivScanned {
		c.iv++
		if isShiny {
			c.shiny++
		}
	}
	b.species[int32(pokemonID)] = c
	b.total++
}

// GetRarityGroup returns the rarity group for a pokemon.
// Returns RarityUnknown (-1) if not enough data has accumulated.
func (st *StatsTracker) GetRarityGroup(pokemonID int) int {
	st.mu.RLock()
	defer st.mu.RUnlock()
	if g, ok := st.groups[pokemonID]; ok {
		return g
	}
	return RarityUnknown
}

// GetShinyRate returns the shiny ratio for a pokemon, or 0 if unknown.
func (st *StatsTracker) GetShinyRate(pokemonID int) float64 {
	st.mu.RLock()
	defer st.mu.RUnlock()
	if s, ok := st.shiny[pokemonID]; ok {
		return s.Ratio
	}
	return 0
}

// defaultRecalcIntervalMins is the fallback cadence when the configured value
// is missing or nonsensical. Matches the [stats] refresh_interval_mins default.
const defaultRecalcIntervalMins = 5

// recalcInterval converts the configured refresh interval into a ticker
// duration, flooring non-positive values.
//
// time.NewTicker panics on a non-positive interval, and an explicit
// `refresh_interval_mins = 0` survives config defaulting because toml decodes
// over the pre-populated defaults struct. Without the floor that panic fires
// in recalcLoop's goroutine during startup and takes the processor with it.
// windowMinutes floors its sibling field for the same reason.
func recalcInterval(mins int) time.Duration {
	if mins < 1 {
		mins = defaultRecalcIntervalMins
	}
	return time.Duration(mins) * time.Minute
}

func (st *StatsTracker) recalcLoop() {
	interval := recalcInterval(st.cfg.RefreshIntervalMins)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		st.recalculate()
	}
}

// counters is the per-species aggregate over the whole window.
type counters struct {
	allScanned   int64
	ivScanned    int64
	shinyScanned int64
}

// oldestMinute is the earliest minute still inside the rolling window.
//
// One definition so aggregate and totalInWindow cannot answer "is this bucket
// still in the window?" differently. Caller must hold at least a read lock.
func (st *StatsTracker) oldestMinute() int64 {
	return st.nowFunc().Unix()/60 - int64(windowMinutes(st.cfg.WindowHours))
}

// aggregate sums every bucket still inside the window. Buckets that have
// fallen out are left alone — they are recycled in place the next time
// RecordSighting lands on their ring slot.
//
// Caller must hold at least a read lock.
func (st *StatsTracker) aggregate() (map[int]*counters, int64) {
	oldest := st.oldestMinute()

	counts := make(map[int]*counters)
	var totalAll int64
	for i := range st.buckets {
		b := &st.buckets[i]
		if b.minute < oldest {
			continue
		}
		for id, sc := range b.species {
			c := counts[int(id)]
			if c == nil {
				c = &counters{}
				counts[int(id)] = c
			}
			c.allScanned += int64(sc.all)
			c.ivScanned += int64(sc.iv)
			c.shinyScanned += int64(sc.shiny)
		}
		totalAll += b.total
	}
	return counts, totalAll
}

// totalInWindow counts sightings still inside the window without building the
// per-species aggregate. Caller must hold at least a read lock.
func (st *StatsTracker) totalInWindow() int64 {
	oldest := st.oldestMinute()
	var total int64
	for i := range st.buckets {
		if st.buckets[i].minute >= oldest {
			total += st.buckets[i].total
		}
	}
	return total
}

func (st *StatsTracker) recalculate() {
	st.mu.Lock()
	defer st.mu.Unlock()

	counts, totalAll := st.aggregate()

	// Rarity groups (require minimum sample size)
	if totalAll >= int64(st.cfg.MinSampleSize) {
		newGroups := make(map[int]int)
		for id, c := range counts {
			pct := float64(c.allScanned) / float64(totalAll) * 100

			var group int
			switch {
			case pct >= st.cfg.Uncommon:
				group = RarityCommon
			case pct >= st.cfg.Rare:
				group = RarityUncommon
			case pct >= st.cfg.VeryRare:
				group = RarityRare
			case pct >= st.cfg.UltraRare:
				group = RarityVeryRare
			default:
				group = RarityUltraRare
			}
			newGroups[id] = group
		}
		st.groups = newGroups
		log.Debugf("Rarity groups recalculated: %d species, %d total sightings in %dh window", len(newGroups), totalAll, st.cfg.WindowHours)
	}

	// Shiny stats (always update, independent of min sample size)
	newShiny := make(map[int]ShinyStats)
	for id, c := range counts {
		if c.ivScanned >= minIVSeenForShiny && c.shinyScanned > 0 {
			newShiny[id] = ShinyStats{
				Total: c.ivScanned,
				Seen:  c.shinyScanned,
				Ratio: float64(c.ivScanned) / float64(c.shinyScanned),
			}
		}
	}
	st.shiny = newShiny
}

// ExportGroups returns pokemon IDs grouped by rarity level.
// Keys are rarity group constants (1-5), values are sorted pokemon ID slices.
// Triggers a recalculation if groups are empty but sighting data exists.
func (st *StatsTracker) ExportGroups() map[int][]int {
	st.mu.RLock()
	needsCalc := len(st.groups) == 0 && st.totalInWindow() >= int64(st.cfg.MinSampleSize)
	st.mu.RUnlock()

	if needsCalc {
		st.recalculate()
	}

	st.mu.RLock()
	defer st.mu.RUnlock()

	result := make(map[int][]int)
	for id, group := range st.groups {
		result[group] = append(result[group], id)
	}
	for _, ids := range result {
		sort.Ints(ids)
	}
	return result
}

// ExportShinyStats returns shiny stats for all pokemon that have been seen shiny
// within the rolling window, with at least minIVSeenForShiny encounters.
func (st *StatsTracker) ExportShinyStats() map[int]ShinyStats {
	st.mu.RLock()
	defer st.mu.RUnlock()

	result := make(map[int]ShinyStats, len(st.shiny))
	maps.Copy(result, st.shiny)
	return result
}

// ExportShinyPossible returns a map of pokemon IDs that have been seen shiny,
// in the format expected by the alerter's ShinyPossible loader: {map: {id: true}}.
func (st *StatsTracker) ExportShinyPossible() map[string]any {
	st.mu.RLock()
	defer st.mu.RUnlock()

	possibleMap := make(map[int]bool)
	for id := range st.shiny {
		possibleMap[id] = true
	}
	return map[string]any{
		"map": possibleMap,
	}
}

// Reset clears all sightings and computed stats.
func (st *StatsTracker) Reset() {
	st.mu.Lock()
	st.buckets = newBucketRing(st.cfg.WindowHours)
	st.groups = make(map[int]int)
	st.shiny = make(map[int]ShinyStats)
	st.mu.Unlock()
}
