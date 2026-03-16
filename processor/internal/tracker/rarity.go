package tracker

import (
	"maps"
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

// sighting records a pokemon sighting with its timestamp.
type sighting struct {
	pokemonID int
	timestamp int64
	ivScanned bool // true if pokemon had IV data (full encounter)
	shiny     bool // true if confirmed shiny
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
	mu        sync.RWMutex
	cfg       StatsConfig
	sightings []sighting
	groups    map[int]int        // pokemon_id -> rarity group
	shiny     map[int]ShinyStats // pokemon_id -> shiny stats (cached)
}

// NewStatsTracker creates a new stats tracker with the given config.
func NewStatsTracker(cfg StatsConfig) *StatsTracker {
	st := &StatsTracker{
		cfg:    cfg,
		groups: make(map[int]int),
		shiny:  make(map[int]ShinyStats),
	}
	go st.recalcLoop()
	return st
}

// RecordSighting records a pokemon sighting. ivScanned indicates whether the
// pokemon had IV data (a full encounter). isShiny indicates a confirmed shiny.
func (st *StatsTracker) RecordSighting(pokemonID int, ivScanned bool, isShiny bool) {
	st.mu.Lock()
	st.sightings = append(st.sightings, sighting{
		pokemonID: pokemonID,
		timestamp: time.Now().Unix(),
		ivScanned: ivScanned,
		shiny:     isShiny,
	})
	st.mu.Unlock()
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

func (st *StatsTracker) recalcLoop() {
	interval := time.Duration(st.cfg.RefreshIntervalMins) * time.Minute
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		st.recalculate()
	}
}

func (st *StatsTracker) recalculate() {
	st.mu.Lock()
	defer st.mu.Unlock()

	// Prune sightings outside the window
	cutoff := time.Now().Unix() - int64(st.cfg.WindowHours)*3600
	pruned := st.sightings[:0]
	for _, s := range st.sightings {
		if s.timestamp >= cutoff {
			pruned = append(pruned, s)
		}
	}
	st.sightings = pruned

	// Count per species
	type counters struct {
		allScanned   int64
		ivScanned    int64
		shinyScanned int64
	}
	counts := make(map[int]*counters)
	var totalAll int64
	for _, s := range st.sightings {
		c := counts[s.pokemonID]
		if c == nil {
			c = &counters{}
			counts[s.pokemonID] = c
		}
		c.allScanned++
		totalAll++
		if s.ivScanned {
			c.ivScanned++
			if s.shiny {
				c.shinyScanned++
			}
		}
	}

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
	needsCalc := len(st.groups) == 0 && len(st.sightings) >= st.cfg.MinSampleSize
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
	st.sightings = nil
	st.groups = make(map[int]int)
	st.shiny = make(map[int]ShinyStats)
	st.mu.Unlock()
}
