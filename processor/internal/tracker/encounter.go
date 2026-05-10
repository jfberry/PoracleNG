package tracker

import (
	"encoding/json"
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
//
// OldWebhook carries the prior sighting's PVP-stripped webhook bytes (as
// previously passed to Track). The pokemon change handler re-runs the
// regular base + per-language enrichment pipeline against these bytes to
// build the {{original.X}} template namespace, so monsterChanged templates
// can reference the same field set as `monster` (minus PVP) for the prior
// state. Empty when the prior sighting was tracked without webhook bytes
// (older callers, tests).
type EncounterChange struct {
	EncounterID string
	Type        ChangeType
	Old         EncounterState
	New         EncounterState
	OldWebhook  json.RawMessage
}

// encounterEntry pairs the diff-detection state snapshot with the prior
// webhook bytes used to rebuild a full {{original.X}} view at change time.
// Bytes are stored already PVP-stripped — Track does NOT strip; the caller
// is expected to pass StripPVP(raw) to keep entries small.
type encounterEntry struct {
	state   EncounterState
	webhook json.RawMessage
}

// EncounterTracker tracks pokemon by encounter_id to detect changes.
type EncounterTracker struct {
	mu      sync.RWMutex
	entries map[string]*encounterEntry
}

// NewEncounterTracker creates a new encounter tracker.
func NewEncounterTracker() *EncounterTracker {
	et := &EncounterTracker{
		entries: make(map[string]*encounterEntry),
	}
	go et.evictionLoop()
	return et
}

// Track records an encounter and returns a change if one was detected.
// Returns (isNew, change) where isNew is true for first-time sightings.
//
// The raw bytes are stored verbatim alongside the state snapshot — the
// caller is responsible for stripping PVP (see StripPVP) before calling so
// the tracker's resident memory stays bounded. Pass nil for raw if no
// prior-webhook view is needed (e.g. in tests).
func (et *EncounterTracker) Track(encounterID string, newState EncounterState, raw json.RawMessage) (bool, *EncounterChange) {
	et.mu.Lock()
	defer et.mu.Unlock()

	prev, exists := et.entries[encounterID]
	if !exists {
		// First sighting
		cp := newState
		cp.InsertedAt = time.Now().Unix()
		et.entries[encounterID] = &encounterEntry{state: cp, webhook: raw}
		return true, nil
	}

	old := prev.state

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
			Old:         old,
			New:         newState,
			OldWebhook:  prev.webhook,
		}
		cp := newState
		cp.InsertedAt = old.InsertedAt
		prev.state = cp
		prev.webhook = raw
		return false, change
	}

	// Update stored state with latest data (stats, weather, disappear time)
	// for accurate next-change comparison. The webhook bytes are *not*
	// refreshed here: they represent "the prior change point" for
	// {{original.X}}, so they must stick from the most recent change
	// (or first sighting) until the next change fires.
	prev.state = newState
	prev.state.InsertedAt = old.InsertedAt

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
	for id, entry := range et.entries {
		state := entry.state
		if (state.DisappearTime > 0 && now > state.DisappearTime+300) ||
			(state.InsertedAt > 0 && now-state.InsertedAt > maxEncounterAge) {
			delete(et.entries, id)
		}
	}
}
