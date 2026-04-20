package tracker

import (
	"sync"
	"time"
)

// WeatherCareEntry represents a user who cares about weather in a cell
// because they have a matched pokemon there.
type WeatherCareEntry struct {
	ID         string
	Name       string
	Type       string
	Language   string
	Template   string
	Clean      int
	Ping       string
	CaresUntil int64 // unix timestamp (pokemon disappear_time)
}

// WeatherCareTracker tracks which users care about weather changes in which
// S2 cells. Users are registered when a pokemon match occurs and expire when
// the pokemon despawns.
type WeatherCareTracker struct {
	mu    sync.Mutex
	cells map[string]map[string]*WeatherCareEntry // cellID -> userID -> entry
}

// NewWeatherCareTracker creates a new tracker.
func NewWeatherCareTracker() *WeatherCareTracker {
	wct := &WeatherCareTracker{
		cells: make(map[string]map[string]*WeatherCareEntry),
	}
	go wct.cleanupLoop()
	return wct
}

// Register records that a user cares about weather in a cell until caresUntil.
// If the user already cares, extends the expiry if later.
func (wct *WeatherCareTracker) Register(cellID string, entry WeatherCareEntry) {
	wct.mu.Lock()
	defer wct.mu.Unlock()

	if wct.cells[cellID] == nil {
		wct.cells[cellID] = make(map[string]*WeatherCareEntry)
	}

	existing, ok := wct.cells[cellID][entry.ID]
	if ok {
		// Extend expiry if this pokemon lasts longer
		if entry.CaresUntil > existing.CaresUntil {
			existing.CaresUntil = entry.CaresUntil
		}
		// Merge clean flags: OR the bits so any matched pokemon's flags are preserved
		if entry.Clean > 0 {
			existing.Clean = existing.Clean | entry.Clean
		}
		existing.Ping = entry.Ping
		existing.Language = entry.Language
		existing.Template = entry.Template
	} else {
		e := entry
		wct.cells[cellID][entry.ID] = &e
	}
}

// GetCaringUsers returns all users who currently care about weather in a cell.
func (wct *WeatherCareTracker) GetCaringUsers(cellID string) []WeatherCareEntry {
	wct.mu.Lock()
	defer wct.mu.Unlock()

	now := time.Now().Unix()
	entries := wct.cells[cellID]
	if entries == nil {
		return nil
	}

	var result []WeatherCareEntry
	for id, e := range entries {
		if e.CaresUntil <= now {
			delete(entries, id)
			continue
		}
		result = append(result, *e)
	}
	return result
}

func (wct *WeatherCareTracker) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		wct.cleanup()
	}
}

func (wct *WeatherCareTracker) cleanup() {
	wct.mu.Lock()
	defer wct.mu.Unlock()

	now := time.Now().Unix()
	for cellID, entries := range wct.cells {
		for id, e := range entries {
			if e.CaresUntil <= now {
				delete(entries, id)
			}
		}
		if len(entries) == 0 {
			delete(wct.cells, cellID)
		}
	}
}
