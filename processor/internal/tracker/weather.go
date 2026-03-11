package tracker

import (
	"sync"
	"time"

	"github.com/golang/geo/s2"
	log "github.com/sirupsen/logrus"
)

// WeatherChange represents a detected weather change event.
type WeatherChange struct {
	Longitude         float64 `json:"longitude"`
	Latitude          float64 `json:"latitude"`
	S2CellID          string  `json:"s2_cell_id"`
	GameplayCondition int     `json:"gameplay_condition"`
	Updated           int64   `json:"updated"`
	Source            string  `json:"source"`
}

// localCellData holds locally inferred weather data per cell.
type localCellData struct {
	weatherFromBoost     [8]int
	currentHourTimestamp int64
	monsterWeather       int
}

// controllerCellData holds weather data from weather webhooks.
type controllerCellData struct {
	lastCurrentWeatherCheck int64
	hourWeather             map[int64]int // hourTimestamp -> condition
}

// WeatherTracker manages per-S2-cell weather state.
// Port of weatherData.js.
type WeatherTracker struct {
	mu             sync.RWMutex
	controllerData map[string]*controllerCellData
	localData      map[string]*localCellData
	changes        chan WeatherChange
}

// NewWeatherTracker creates a new weather tracker.
func NewWeatherTracker() *WeatherTracker {
	return &WeatherTracker{
		controllerData: make(map[string]*controllerCellData),
		localData:      make(map[string]*localCellData),
		changes:        make(chan WeatherChange, 100),
	}
}

// Changes returns the channel that emits weather change events.
func (wt *WeatherTracker) Changes() <-chan WeatherChange {
	return wt.changes
}

// GetWeatherCellID returns the S2 level-10 cell ID for a lat/lon as a string token.
func GetWeatherCellID(lat, lon float64) string {
	ll := s2.LatLngFromDegrees(lat, lon)
	cellID := s2.CellIDFromLatLng(ll).Parent(10)
	return cellID.ToToken()
}

// UpdateFromWebhook updates weather state from a direct weather webhook.
func (wt *WeatherTracker) UpdateFromWebhook(cellID string, condition int, timestamp int64) {
	wt.mu.Lock()
	defer wt.mu.Unlock()

	cd, ok := wt.controllerData[cellID]
	if !ok {
		cd = &controllerCellData{hourWeather: make(map[int64]int)}
		wt.controllerData[cellID] = cd
	}

	hourTimestamp := timestamp - (timestamp % 3600)
	cd.hourWeather[hourTimestamp] = condition
	cd.lastCurrentWeatherCheck = timestamp
}

// GetCurrentWeatherInCell returns the current weather condition for a cell.
func (wt *WeatherTracker) GetCurrentWeatherInCell(cellID string) int {
	wt.mu.RLock()
	defer wt.mu.RUnlock()

	now := time.Now().Unix()
	currentHour := now - (now % 3600)

	cd := wt.controllerData[cellID]
	ld := wt.localData[cellID]

	var weather int
	if cd != nil && cd.lastCurrentWeatherCheck >= currentHour {
		weather = cd.hourWeather[currentHour]
	}
	// Local inference overrides if we have it for this hour
	if ld != nil && ld.currentHourTimestamp == currentHour {
		weather = ld.monsterWeather
	}
	return weather
}

// CheckWeatherOnMonster analyzes an incoming pokemon's weather boost to detect
// weather changes via vote-based inference.
// Port of weatherData.js:68-123.
func (wt *WeatherTracker) CheckWeatherOnMonster(cellID string, lat, lon float64, monsterWeather int) {
	now := time.Now().Unix()
	currentHour := now - (now % 3600)
	previousHour := currentHour - 3600

	wt.mu.Lock()
	defer wt.mu.Unlock()

	// Ensure data structures exist
	if wt.localData[cellID] == nil {
		wt.localData[cellID] = &localCellData{}
	}
	if wt.controllerData[cellID] == nil {
		wt.controllerData[cellID] = &controllerCellData{hourWeather: make(map[int64]int)}
	}

	local := wt.localData[cellID]
	controller := wt.controllerData[cellID]

	// Only process if more than 30 seconds into the hour and monster has weather
	if now <= currentHour+30 || monsterWeather == 0 {
		return
	}

	if controller.lastCurrentWeatherCheck == 0 {
		controller.lastCurrentWeatherCheck = previousHour
	}

	currentWeather := controller.hourWeather[currentHour]

	// If observed weather agrees with up-to-date broadcast, reset counters
	if monsterWeather == currentWeather && controller.lastCurrentWeatherCheck >= currentHour {
		local.weatherFromBoost = [8]int{}
		return
	}

	if monsterWeather != currentWeather || (monsterWeather == currentWeather && controller.lastCurrentWeatherCheck < currentHour) {
		for i := range local.weatherFromBoost {
			if i == monsterWeather {
				local.weatherFromBoost[i]++
			} else {
				local.weatherFromBoost[i]--
			}
		}

		// Check if any weather type has enough votes (>4)
		changed := false
		for _, v := range local.weatherFromBoost {
			if v > 4 {
				changed = true
				break
			}
		}

		if changed {
			local.weatherFromBoost = [8]int{}

			if local.currentHourTimestamp != currentHour || local.monsterWeather != monsterWeather {
				local.currentHourTimestamp = currentHour
				local.monsterWeather = monsterWeather

				log.Infof("Boosted Pokemon! Force update of weather in cell %s with weather %d", cellID, monsterWeather)

				// Send non-blocking
				select {
				case wt.changes <- WeatherChange{
					Longitude:         lon,
					Latitude:          lat,
					S2CellID:          cellID,
					GameplayCondition: monsterWeather,
					Updated:           now,
					Source:            "fromMonster",
				}:
				default:
				}
			}
		}
	}
}
