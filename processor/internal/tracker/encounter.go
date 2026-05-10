package tracker

import (
	"sync"
	"time"
)

// EncounterState holds the state of a pokemon encounter for change detection.
type EncounterState struct {
	PokemonID     int
	Form          int
	Gender        int
	Weather       int
	CP            int
	ATK           int
	DEF           int
	STA           int
	DisappearTime int64
	InsertedAt    int64 // unix timestamp when this entry was first created
}

// EncounterChange holds old and new state when a change is detected.
type EncounterChange struct {
	EncounterID string
	Type        ChangeType
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
		cp.InsertedAt = time.Now().Unix()
		et.entries[encounterID] = &cp
		return true, nil
	}

	// Detect change type. Priority order: species > form > gender > encountered > weather_boost.
	// Gender change only fires when both old and new are non-zero (initial gender resolution doesn't count).
	// Weather-boost shift only fires post-encounter (both CPs > 0) AND when the CP actually moved.
	// Raw IV (atk/def/sta) drift is ignored — physical IVs don't change post-encounter.
	var changeType ChangeType
	switch {
	case old.PokemonID != newState.PokemonID:
		changeType = ChangeSpecies
	case old.Form != newState.Form:
		changeType = ChangeForm
	case old.Gender != newState.Gender && old.Gender != 0 && newState.Gender != 0:
		changeType = ChangeGender
	case old.CP == 0 && newState.CP > 0:
		changeType = ChangeEncountered
	case old.Weather != newState.Weather && old.CP > 0 && newState.CP > 0 && old.CP != newState.CP:
		changeType = ChangeWeatherBoost
	}

	if changeType != ChangeNone {
		change := &EncounterChange{
			EncounterID: encounterID,
			Type:        changeType,
			Old:         *old,
			New:         newState,
		}
		cp := newState
		cp.InsertedAt = old.InsertedAt
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

const maxEncounterAge int64 = 1 * 60 * 60 // 1 hour in seconds

func (et *EncounterTracker) evict() {
	et.mu.Lock()
	defer et.mu.Unlock()

	now := time.Now().Unix()
	for id, state := range et.entries {
		if (state.DisappearTime > 0 && now > state.DisappearTime+300) ||
			(state.InsertedAt > 0 && now-state.InsertedAt > maxEncounterAge) {
			delete(et.entries, id)
		}
	}
}
