package tracker

import (
	"sync"
	"time"

	"github.com/pokemon/poracleng/processor/internal/webhook"
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
// OldWebhook carries the prior sighting's already-parsed PokemonWebhook
// (with PVP fields cleared at storage time). The pokemon change handler
// re-runs the regular base + per-language enrichment pipeline against this
// struct to build the {{original.X}} template namespace, so monsterChanged
// templates can reference the same field set as `monster` (minus PVP) for
// the prior state. Nil when the prior sighting was tracked without a
// webhook (older callers, tests).
type EncounterChange struct {
	EncounterID string
	Type        ChangeType
	Old         EncounterState
	New         EncounterState
	OldWebhook  *webhook.PokemonWebhook
}

// encounterEntry pairs the diff-detection state snapshot with the prior
// webhook struct used to rebuild a full {{original.X}} view at change time.
// The caller is expected to clear PVP fields on the stored struct (see
// ProcessPokemon) so map memory for large rankings doesn't pin in the tracker.
type encounterEntry struct {
	state   EncounterState
	webhook *webhook.PokemonWebhook
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
// The pokemon struct is stored by pointer alongside the state snapshot —
// the caller is responsible for clearing PVP fields (PVP, PVPRankings*)
// before calling so the tracker's resident memory stays bounded. Pass nil
// if no prior-webhook view is needed (e.g. in tests).
func (et *EncounterTracker) Track(encounterID string, newState EncounterState, pokemon *webhook.PokemonWebhook) (bool, *EncounterChange) {
	et.mu.Lock()
	defer et.mu.Unlock()

	prev, exists := et.entries[encounterID]
	if !exists {
		cp := newState
		cp.InsertedAt = time.Now().Unix()
		et.entries[encounterID] = &encounterEntry{state: cp, webhook: pokemon}
		return true, nil
	}

	old := prev.state

	// Detect change type. Priority order: species > form > gender > encountered > weather_boost > stats.
	// Gender change only fires when both old and new are non-zero (initial gender resolution doesn't count).
	// Weather-boost shift only fires post-encounter (both CPs > 0) AND when the CP actually moved.
	// Stats drift (raw atk/def/sta differing between two encountered webhooks) fires when
	// Golbat re-reports the same encounter with different IVs — happens with the A/B
	// scanner anomaly. Physical IVs don't change in-game, but scanner reports can.
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
	case old.CP > 0 && newState.CP > 0 && (old.ATK != newState.ATK || old.DEF != newState.DEF || old.STA != newState.STA):
		changeType = ChangeStats
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
		prev.webhook = pokemon
		return false, change
	}

	// Wild re-scan of an already-encountered pokemon: don't downgrade.
	// Golbat re-emits wild webhooks (CP=0, zeroed IVs) after the
	// initial encounter, often after weather shifts. Overwriting the
	// encountered snapshot with that zero data makes the next encounter
	// webhook look like a fresh CP 0→>0 transition (ChangeEncountered),
	// hiding the real stats/weather diff and dropping prior recipients
	// silently.
	if old.CP > 0 && newState.CP == 0 {
		return false, nil
	}

	// Refresh state for accurate next-change comparison. The webhook
	// struct is NOT refreshed: it must stick from the most recent
	// change (or first sighting) so {{original.X}} renders against
	// the right prior point.
	prev.state = newState
	prev.state.InsertedAt = old.InsertedAt

	return false, nil
}

// Has reports whether the tracker currently holds an entry for the
// encounter. Used by the pokemon handler to decide whether to keep
// tracking on a subsequent webhook even when nobody currently matches:
// if someone matched at T1 (the entry exists), changes at T2 still
// need to be detected so prior recipients can be notified.
//
// O(1) read under RLock — safe to call on every pokemon webhook.
func (et *EncounterTracker) Has(encounterID string) bool {
	et.mu.RLock()
	defer et.mu.RUnlock()
	_, ok := et.entries[encounterID]
	return ok
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
