package tracker

import (
	"sync"
	"time"
)

// GymState holds cached gym state for change detection.
type GymState struct {
	TeamID         int
	SlotsAvailable int
	InBattle       bool
	LastOwnerID    int
	LastSeen       time.Time
}

// GymStateTracker tracks gym state for detecting team/slot/battle changes.
type GymStateTracker struct {
	mu   sync.Mutex
	gyms map[string]*GymState
}

// NewGymStateTracker creates a new gym state tracker.
func NewGymStateTracker() *GymStateTracker {
	gst := &GymStateTracker{
		gyms: make(map[string]*GymState),
	}
	go gst.cleanupLoop()
	return gst
}

// Update stores current gym state and returns old state.
// Returns nil if this is the first time seeing the gym.
func (gst *GymStateTracker) Update(gymID string, teamID, slotsAvailable int, inBattle bool, lastOwnerID int) *GymState {
	gst.mu.Lock()
	defer gst.mu.Unlock()

	old := gst.gyms[gymID]
	var oldCopy *GymState
	if old != nil {
		c := *old
		oldCopy = &c
		old.TeamID = teamID
		old.SlotsAvailable = slotsAvailable
		old.InBattle = inBattle
		old.LastOwnerID = lastOwnerID
		old.LastSeen = time.Now()
	} else {
		gst.gyms[gymID] = &GymState{
			TeamID:         teamID,
			SlotsAvailable: slotsAvailable,
			InBattle:       inBattle,
			LastOwnerID:    lastOwnerID,
			LastSeen:       time.Now(),
		}
	}
	return oldCopy
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
