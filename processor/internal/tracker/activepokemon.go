package tracker

import (
	"sync"
	"time"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
)

// ActivePokemon represents a pokemon that a user has matched in a weather cell.
type ActivePokemon struct {
	PokemonID     int
	Form          int
	IV            float64
	CP            int
	Latitude      float64
	Longitude     float64
	DisappearTime int64
	Boosted       bool  // whether this pokemon is currently weather-boosted
	Types         []int // pokemon type IDs
}

// ActivePokemonTracker tracks matched pokemon per S2 cell and user, so that
// weather change alerts can include which pokemon are affected.
type ActivePokemonTracker struct {
	mu         sync.Mutex
	cells      map[string]map[string]map[string]*ActivePokemon // cellID -> userID -> encounterID -> pokemon
	maxPerUser int
}

// NewActivePokemonTracker creates a new tracker with a per-user cap per cell.
func NewActivePokemonTracker(maxPerUser int) *ActivePokemonTracker {
	if maxPerUser <= 0 {
		maxPerUser = 50
	}
	apt := &ActivePokemonTracker{
		cells:      make(map[string]map[string]map[string]*ActivePokemon),
		maxPerUser: maxPerUser,
	}
	go apt.cleanupLoop()
	return apt
}

// Register adds or updates a pokemon entry for a user in a cell.
// Lazily evicts expired entries for this cell+user.
func (apt *ActivePokemonTracker) Register(cellID, userID, encounterID string, pokemon ActivePokemon) {
	apt.mu.Lock()
	defer apt.mu.Unlock()

	if apt.cells[cellID] == nil {
		apt.cells[cellID] = make(map[string]map[string]*ActivePokemon)
	}
	if apt.cells[cellID][userID] == nil {
		apt.cells[cellID][userID] = make(map[string]*ActivePokemon)
	}

	encounters := apt.cells[cellID][userID]

	// Lazy eviction of expired entries for this user
	now := time.Now().Unix()
	for eid, p := range encounters {
		if p.DisappearTime <= now {
			delete(encounters, eid)
		}
	}

	// Add/update the entry
	encounters[encounterID] = &pokemon

	// Enforce per-user cap: if over limit, remove oldest by DisappearTime
	if len(encounters) > apt.maxPerUser {
		var oldestID string
		var oldestTime int64 = 1<<63 - 1
		for eid, p := range encounters {
			if p.DisappearTime < oldestTime {
				oldestTime = p.DisappearTime
				oldestID = eid
			}
		}
		if oldestID != "" {
			delete(encounters, oldestID)
		}
	}
}

// GetAffectedPokemon returns pokemon for a user in a cell that are affected by
// a weather change from oldWeather to newWeather. Expired entries are pruned.
// Returns up to maxCount results.
func (apt *ActivePokemonTracker) GetAffectedPokemon(cellID, userID string, oldWeather, newWeather int, maxCount int) []ActivePokemon {
	apt.mu.Lock()
	defer apt.mu.Unlock()

	cellUsers := apt.cells[cellID]
	if cellUsers == nil {
		return nil
	}
	encounters := cellUsers[userID]
	if encounters == nil {
		return nil
	}

	now := time.Now().Unix()
	var result []ActivePokemon

	for eid, p := range encounters {
		if p.DisappearTime <= now {
			delete(encounters, eid)
			continue
		}

		if gamedata.IsAffectedByWeatherChange(p.Types, p.Boosted, newWeather) {
			result = append(result, *p)
			if len(result) >= maxCount {
				break
			}
		}
	}

	return result
}

func (apt *ActivePokemonTracker) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		apt.cleanup()
	}
}

func (apt *ActivePokemonTracker) cleanup() {
	apt.mu.Lock()
	defer apt.mu.Unlock()

	now := time.Now().Unix()
	for cellID, users := range apt.cells {
		for userID, encounters := range users {
			for eid, p := range encounters {
				if p.DisappearTime <= now {
					delete(encounters, eid)
				}
			}
			if len(encounters) == 0 {
				delete(users, userID)
			}
		}
		if len(users) == 0 {
			delete(apt.cells, cellID)
		}
	}
}
