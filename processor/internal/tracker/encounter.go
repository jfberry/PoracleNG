package tracker

import (
	"sync"
	"time"
)

// EncounterState holds the state of a pokemon encounter for change detection.
type EncounterState struct {
	PokemonID     int
	Form          int
	Weather       int
	CP            int
	ATK           int
	DEF           int
	STA           int
	DisappearTime int64
}

// EncounterChange holds old and new state when a change is detected.
type EncounterChange struct {
	EncounterID string
	Old         EncounterState
	New         EncounterState
}

// EncounterTracker tracks pokemon by encounter_id to detect changes.
type EncounterTracker struct {
	mu      sync.RWMutex
	entries map[string]*EncounterState
}

// NewEncounterTracker creates a new encounter tracker.
func NewEncounterTracker() *EncounterTracker {
	et := &EncounterTracker{
		entries: make(map[string]*EncounterState),
	}
	go et.evictionLoop()
	return et
}

// Track records an encounter and returns a change if one was detected.
// Returns (isNew, change) where isNew is true for first-time sightings.
func (et *EncounterTracker) Track(encounterID string, newState EncounterState) (bool, *EncounterChange) {
	et.mu.Lock()
	defer et.mu.Unlock()

	old, exists := et.entries[encounterID]
	if !exists {
		// First sighting
		cp := newState
		et.entries[encounterID] = &cp
		return true, nil
	}

	// Only trigger a change event for meaningful changes (species or form),
	// not for stats being filled in from an encounter (CP/ATK/DEF/STA going
	// from 0 to a real value) or weather fluctuations.
	changed := old.PokemonID != newState.PokemonID ||
		old.Form != newState.Form

	if changed {
		change := &EncounterChange{
			EncounterID: encounterID,
			Old:         *old,
			New:         newState,
		}
		// Update stored state
		cp := newState
		et.entries[encounterID] = &cp
		return false, change
	}

	// Update stored state with latest data (stats, weather, disappear time)
	*old = newState

	return false, nil
}

func (et *EncounterTracker) evictionLoop() {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		et.evict()
	}
}

func (et *EncounterTracker) evict() {
	et.mu.Lock()
	defer et.mu.Unlock()

	now := time.Now().Unix()
	for id, state := range et.entries {
		if state.DisappearTime > 0 && now > state.DisappearTime+300 {
			delete(et.entries, id)
		}
	}
}
