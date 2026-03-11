package state

import (
	"sync"

	"github.com/pokemon/poracleng/processor/internal/db"
	"github.com/pokemon/poracleng/processor/internal/geofence"
)

// State holds an immutable snapshot of all tracking data.
type State struct {
	Humans    map[string]*db.Human
	Monsters  *db.MonsterIndex
	Raids     []*db.RaidTracking
	Eggs      []*db.EggTracking
	Profiles  map[db.ProfileKey]*db.Profile
	Invasions []*db.InvasionTracking
	Quests    []*db.QuestTracking
	Lures     []*db.LureTracking
	Gyms      []*db.GymTracking
	Nests     []*db.NestTracking
	Forts     []*db.FortTracking
	Geofence  *geofence.SpatialIndex
	Fences    []geofence.Fence
}

// Manager manages the current state with atomic swaps.
type Manager struct {
	mu    sync.RWMutex
	state *State
}

// NewManager creates a new state manager.
func NewManager() *Manager {
	return &Manager{}
}

// Get returns the current state snapshot. Callers should hold the returned
// pointer for the duration of their processing (it won't change under them).
func (m *Manager) Get() *State {
	m.mu.RLock()
	s := m.state
	m.mu.RUnlock()
	return s
}

// Set atomically replaces the current state.
func (m *Manager) Set(s *State) {
	m.mu.Lock()
	m.state = s
	m.mu.Unlock()
}
