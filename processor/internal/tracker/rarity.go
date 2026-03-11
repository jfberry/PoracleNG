package tracker

import (
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
)

// RarityTracker maintains rolling pokemon sighting counts and computes rarity groups.
type RarityTracker struct {
	mu           sync.RWMutex
	counts       map[int]int64 // pokemon_id -> sighting count in window
	groups       map[int]int   // pokemon_id -> rarity group
	window       time.Duration
	calcInterval time.Duration
	lastCalc     time.Time
}

// NewRarityTracker creates a new rarity tracker.
func NewRarityTracker(window time.Duration) *RarityTracker {
	rt := &RarityTracker{
		counts:       make(map[int]int64),
		groups:       make(map[int]int),
		window:       window,
		calcInterval: 15 * time.Minute,
	}
	go rt.recalcLoop()
	return rt
}

// RecordSighting records a pokemon sighting.
func (rt *RarityTracker) RecordSighting(pokemonID int) {
	rt.mu.Lock()
	rt.counts[pokemonID]++
	rt.mu.Unlock()
}

// GetRarityGroup returns the rarity group for a pokemon.
// Returns RarityUnknown (-1) if not enough data has accumulated.
func (rt *RarityTracker) GetRarityGroup(pokemonID int) int {
	rt.mu.RLock()
	defer rt.mu.RUnlock()
	if g, ok := rt.groups[pokemonID]; ok {
		return g
	}
	return RarityUnknown
}

func (rt *RarityTracker) recalcLoop() {
	ticker := time.NewTicker(rt.calcInterval)
	defer ticker.Stop()
	for range ticker.C {
		rt.recalculate()
	}
}

func (rt *RarityTracker) recalculate() {
	rt.mu.Lock()
	defer rt.mu.Unlock()

	if len(rt.counts) == 0 {
		return
	}

	// Get total and sort by count descending
	type entry struct {
		id    int
		count int64
	}
	var entries []entry
	var total int64
	for id, count := range rt.counts {
		entries = append(entries, entry{id, count})
		total += count
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].count > entries[j].count
	})

	// Assign rarity groups: species sorted by count descending, walk through and
	// assign groups based on cumulative percentage of species (not sightings).
	// Species seen more frequently are "common", those rarely seen are "ultra rare".
	newGroups := make(map[int]int)
	for _, e := range entries {
		// Individual species share of total sightings
		pct := float64(e.count) / float64(total) * 100

		var group int
		switch {
		case pct >= 5.0:
			group = RarityCommon
		case pct >= 1.0:
			group = RarityUncommon
		case pct >= 0.1:
			group = RarityRare
		case pct >= 0.01:
			group = RarityVeryRare
		default:
			group = RarityUltraRare
		}
		newGroups[e.id] = group
	}

	rt.groups = newGroups
	rt.lastCalc = time.Now()

	log.Debugf("Rarity groups recalculated: %d species, %d total sightings", len(newGroups), total)
}

// Reset clears all counts and groups (e.g. on window expiry).
func (rt *RarityTracker) Reset() {
	rt.mu.Lock()
	rt.counts = make(map[int]int64)
	rt.groups = make(map[int]int)
	rt.mu.Unlock()
}
