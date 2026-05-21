package tracker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

// GymState holds cached gym state for change detection.
type GymState struct {
	TeamID              int       `json:"team_id"`
	SlotsAvailable      int       `json:"slots_available"`
	InBattle            bool      `json:"in_battle"`
	LastOwnerID         int       `json:"last_owner_id"`
	LastSeen            time.Time `json:"last_seen"`
	BattleCooldownUntil time.Time `json:"-"`
}

// GymStateTracker tracks gym state for detecting team/slot/battle changes.
// State is persisted to disk on Save() and restored on Load() to avoid a
// burst of false team-change alerts after a restart.
type GymStateTracker struct {
	mu       sync.Mutex
	gyms     map[string]*GymState
	cacheDir string
}

const gymCacheFile = "gym-state.json"

// NewGymStateTracker creates a new gym state tracker.
// If cacheDir is non-empty, Load() and Save() will use that directory.
func NewGymStateTracker(cacheDir string) *GymStateTracker {
	gst := &GymStateTracker{
		gyms:     make(map[string]*GymState),
		cacheDir: cacheDir,
	}
	go gst.cleanupLoop()
	return gst
}

// Update stores current gym state and returns old state.
// Returns nil if this is the first time seeing the gym.
//
// LastOwnerID is computed here (Golbat doesn't ship a last_owner_id field).
// Matches PoracleJS app.js: `last_owner_id = team || cached.last_owner_id`,
// i.e. the most recent non-zero controlling team, preserved across
// Uncontested gaps. -1 means "we've never seen this gym controlled".
func (gst *GymStateTracker) Update(gymID string, teamID, slotsAvailable int, inBattle bool) *GymState {
	gst.mu.Lock()
	defer gst.mu.Unlock()

	return gst.updateLocked(gymID, teamID, slotsAvailable, inBattle, time.Now())
}

// UpdateWithBattleCooldown stores current gym state and returns the old state
// plus whether the gym was already inside the battle cooldown before this
// webhook. The cooldown decision and state transition are made under the same
// lock so concurrent first in-battle updates cannot both escape suppression.
func (gst *GymStateTracker) UpdateWithBattleCooldown(gymID string, teamID, slotsAvailable int, inBattle bool, now time.Time) (*GymState, bool) {
	gst.mu.Lock()
	defer gst.mu.Unlock()

	old := gst.gyms[gymID]
	battleCooldown := old != nil && old.BattleCooldownUntil.After(now)
	oldCopy := gst.updateLocked(gymID, teamID, slotsAvailable, inBattle, now)
	if inBattle {
		gst.gyms[gymID].BattleCooldownUntil = now.Add(5 * time.Minute)
	}
	return oldCopy, battleCooldown
}

func (gst *GymStateTracker) updateLocked(gymID string, teamID, slotsAvailable int, inBattle bool, now time.Time) *GymState {
	old := gst.gyms[gymID]
	var oldCopy *GymState

	var newLastOwner int
	switch {
	case teamID > 0:
		newLastOwner = teamID
	case old != nil && old.LastOwnerID > 0:
		newLastOwner = old.LastOwnerID
	default:
		newLastOwner = -1
	}

	if old != nil {
		c := *old
		oldCopy = &c
		old.TeamID = teamID
		old.SlotsAvailable = slotsAvailable
		old.InBattle = inBattle
		old.LastOwnerID = newLastOwner
		old.LastSeen = now
	} else {
		gst.gyms[gymID] = &GymState{
			TeamID:         teamID,
			SlotsAvailable: slotsAvailable,
			InBattle:       inBattle,
			LastOwnerID:    newLastOwner,
			LastSeen:       now,
		}
	}
	return oldCopy
}

// Save persists the gym state cache to disk.
func (gst *GymStateTracker) Save() error {
	if gst.cacheDir == "" {
		return nil
	}

	gst.mu.Lock()
	snapshot := make(map[string]*GymState, len(gst.gyms))
	for k, v := range gst.gyms {
		cp := *v
		snapshot[k] = &cp
	}
	gst.mu.Unlock()

	data, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}

	if err := os.MkdirAll(gst.cacheDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(gst.cacheDir, gymCacheFile), data, 0o644)
}

// Load restores the gym state cache from disk.
func (gst *GymStateTracker) Load() error {
	if gst.cacheDir == "" {
		return nil
	}

	path := filepath.Join(gst.cacheDir, gymCacheFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // no cache file yet
		}
		return err
	}

	var loaded map[string]*GymState
	if err := json.Unmarshal(data, &loaded); err != nil {
		log.Warnf("gym state: failed to parse cache %s: %v (starting fresh)", path, err)
		return nil
	}

	gst.mu.Lock()
	defer gst.mu.Unlock()

	// Only load entries that aren't stale (< 24h old)
	cutoff := time.Now().Add(-24 * time.Hour)
	restored := 0
	for k, v := range loaded {
		if v.LastSeen.After(cutoff) {
			gst.gyms[k] = v
			restored++
		}
	}

	log.Infof("gym state: restored %d gyms from cache", restored)
	return nil
}

func (gst *GymStateTracker) cleanupLoop() {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		gst.cleanup()
	}
}

func (gst *GymStateTracker) cleanup() {
	gst.mu.Lock()
	defer gst.mu.Unlock()

	cutoff := time.Now().Add(-24 * time.Hour)
	for id, gs := range gst.gyms {
		if gs.LastSeen.Before(cutoff) {
			delete(gst.gyms, id)
		}
	}
}
