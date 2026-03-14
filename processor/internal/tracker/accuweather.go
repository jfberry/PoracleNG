package tracker

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sync"
	"time"

	"github.com/golang/geo/s2"
	log "github.com/sirupsen/logrus"
)

// AccuWeatherConfig holds configuration for the AccuWeather forecast client.
type AccuWeatherConfig struct {
	APIKeys                 []string
	DayQuota                int
	ForecastRefreshInterval int  // hours between API calls
	LocalFirstFetchHOD      int  // first fetch hour of day (e.g. 3 = 3am local)
	SmartForecast           bool // pull on demand when no data available
}

// AccuWeatherClient fetches weather forecasts from AccuWeather and stores
// per-hour PoGo weather conditions into a WeatherTracker.
type AccuWeatherClient struct {
	cfg     AccuWeatherConfig
	tracker *WeatherTracker
	client  *http.Client

	mu           sync.Mutex
	keyUsage     map[string]int // date-key -> count
	keyUsageDate string        // YYYY-MM-DD
	cellMutexes  map[string]*sync.Mutex
	cellLocations map[string]string // cellID -> AccuWeather location key
	cellForecasts map[string]*forecastState
}

type forecastState struct {
	forecastTimeout int64 // unix timestamp when forecast expires
	lastForecastLoad int64 // hour timestamp of last fetch attempt
}

// accuWeatherHourlyResponse is one element from the 12-hour forecast API.
type accuWeatherHourlyResponse struct {
	EpochDateTime int64 `json:"EpochDateTime"`
	WeatherIcon   int   `json:"WeatherIcon"`
	Wind          struct {
		Speed struct {
			Value float64 `json:"Value"`
		} `json:"Speed"`
	} `json:"Wind"`
	WindGust struct {
		Speed struct {
			Value float64 `json:"Value"`
		} `json:"Speed"`
	} `json:"WindGust"`
}

// accuWeatherLocationResponse is the geoposition search response.
type accuWeatherLocationResponse struct {
	Key string `json:"Key"`
}

// NewAccuWeatherClient creates a new AccuWeather forecast client.
func NewAccuWeatherClient(cfg AccuWeatherConfig, tracker *WeatherTracker) *AccuWeatherClient {
	return &AccuWeatherClient{
		cfg:           cfg,
		tracker:       tracker,
		client:        &http.Client{Timeout: 15 * time.Second},
		keyUsage:      make(map[string]int),
		cellMutexes:   make(map[string]*sync.Mutex),
		cellLocations: make(map[string]string),
		cellForecasts: make(map[string]*forecastState),
	}
}

// EnsureForecast ensures the given cell has up-to-date forecast data.
// Called by WeatherTracker.GetWeatherForecast when forecast is enabled.
func (aw *AccuWeatherClient) EnsureForecast(cellID string) {
	now := time.Now().Unix()
	currentHour := now - (now % 3600)
	nextHour := currentHour + 3600

	// Check if we already have valid data
	aw.mu.Lock()
	fs := aw.cellForecasts[cellID]
	if fs == nil {
		fs = &forecastState{}
		aw.cellForecasts[cellID] = fs
	}

	// Already have next hour data and forecast hasn't expired
	hasNextHour := aw.tracker.hasHourWeather(cellID, nextHour)
	if hasNextHour && fs.forecastTimeout > currentHour {
		aw.mu.Unlock()
		return
	}

	// Already attempted this hour
	if fs.lastForecastLoad == currentHour {
		aw.mu.Unlock()
		return
	}

	// Get or create per-cell mutex
	cellMu := aw.cellMutexes[cellID]
	if cellMu == nil {
		cellMu = &sync.Mutex{}
		aw.cellMutexes[cellID] = cellMu
	}
	aw.mu.Unlock()

	// Serialize requests per cell
	cellMu.Lock()
	defer cellMu.Unlock()

	// Double-check after acquiring lock
	aw.mu.Lock()
	fs = aw.cellForecasts[cellID]
	if fs.lastForecastLoad == currentHour {
		aw.mu.Unlock()
		return
	}
	fs.lastForecastLoad = currentHour
	aw.mu.Unlock()

	aw.fetchForecast(cellID, currentHour)
}

func (aw *AccuWeatherClient) fetchForecast(cellID string, currentHour int64) {
	// Get location key
	locationKey, err := aw.getLocationKey(cellID)
	if err != nil {
		log.Errorf("AccuWeather: failed to get location for cell %s: %v", cellID, err)
		return
	}

	// Get API key
	apiKey := aw.getLaziestKey()
	if apiKey == "" {
		log.Warn("AccuWeather: no API key available with free quota")
		return
	}

	// Fetch 12-hour forecast
	url := fmt.Sprintf("https://dataservice.accuweather.com/forecasts/v1/hourly/12hour/%s?apikey=%s&details=true&metric=true", locationKey, apiKey)
	log.Debugf("AccuWeather: fetching forecast for cell %s: %s", cellID, url)

	resp, err := aw.client.Get(url)
	if err != nil {
		log.Errorf("AccuWeather: forecast request failed for cell %s: %v", cellID, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Errorf("AccuWeather: forecast request returned %d for cell %s", resp.StatusCode, cellID)
		return
	}

	var forecasts []accuWeatherHourlyResponse
	if err := json.NewDecoder(resp.Body).Decode(&forecasts); err != nil {
		log.Errorf("AccuWeather: failed to decode forecast for cell %s: %v", cellID, err)
		return
	}

	// Store forecast data in the weather tracker
	var logString string
	for _, f := range forecasts {
		hourTS := f.EpochDateTime - (f.EpochDateTime % 3600)
		if hourTS >= currentHour {
			pogoWeather := mapPoGoWeather(f)
			aw.tracker.SetHourWeather(cellID, hourTS, pogoWeather)
			logString += fmt.Sprintf("%s=%d ", time.Unix(hourTS, 0).UTC().Format("15:04"), pogoWeather)
		}
	}
	log.Infof("AccuWeather: cell %s forecast [UTC] %s", cellID, logString)

	// Calculate next refresh timeout
	forecastTimeout := aw.calculateForecastTimeout(currentHour)

	aw.mu.Lock()
	fs := aw.cellForecasts[cellID]
	fs.forecastTimeout = forecastTimeout
	aw.mu.Unlock()
}

func (aw *AccuWeatherClient) getLocationKey(cellID string) (string, error) {
	aw.mu.Lock()
	if loc, ok := aw.cellLocations[cellID]; ok {
		aw.mu.Unlock()
		return loc, nil
	}
	aw.mu.Unlock()

	// Convert S2 cell ID to lat/lon
	token := cellID
	cell := s2.CellIDFromToken(token)
	ll := cell.LatLng()
	lat := ll.Lat.Degrees()
	lon := ll.Lng.Degrees()

	apiKey := aw.getLaziestKey()
	if apiKey == "" {
		return "", fmt.Errorf("no API key available")
	}

	url := fmt.Sprintf("https://dataservice.accuweather.com/locations/v1/cities/geoposition/search?apikey=%s&q=%f%%2C%f", apiKey, lat, lon)
	log.Debugf("AccuWeather: fetching location for cell %s (%f, %f)", cellID, lat, lon)

	resp, err := aw.client.Get(url)
	if err != nil {
		return "", fmt.Errorf("location request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return "", fmt.Errorf("location request returned %d", resp.StatusCode)
	}

	var locResp accuWeatherLocationResponse
	if err := json.NewDecoder(resp.Body).Decode(&locResp); err != nil {
		return "", fmt.Errorf("failed to decode location response: %w", err)
	}

	aw.mu.Lock()
	aw.cellLocations[cellID] = locResp.Key
	aw.mu.Unlock()

	return locResp.Key, nil
}

// getLaziestKey returns the API key with the fewest calls today, or "" if all are exhausted.
func (aw *AccuWeatherClient) getLaziestKey() string {
	aw.mu.Lock()
	defer aw.mu.Unlock()

	if len(aw.cfg.APIKeys) == 0 {
		return ""
	}

	today := time.Now().Format("2006-01-02")
	if aw.keyUsageDate != today {
		aw.keyUsage = make(map[string]int)
		aw.keyUsageDate = today
	}

	// Find the key with the fewest calls
	var bestKey string
	bestCount := math.MaxInt
	for _, key := range aw.cfg.APIKeys {
		if key == "" {
			continue
		}
		count := aw.keyUsage[key]
		if count < bestCount {
			bestCount = count
			bestKey = key
		}
	}

	if bestKey == "" || bestCount >= aw.cfg.DayQuota {
		return ""
	}

	aw.keyUsage[bestKey]++
	return bestKey
}

// calculateForecastTimeout computes when the next forecast refresh should happen.
func (aw *AccuWeatherClient) calculateForecastTimeout(currentHour int64) int64 {
	now := time.Now().Unix()

	if aw.cfg.SmartForecast || aw.cfg.LocalFirstFetchHOD == 0 {
		return now + int64(aw.cfg.ForecastRefreshInterval)*3600
	}

	// Align to the configured fetch schedule
	currentTime := time.Unix(currentHour, 0)
	currentHourOfDay := currentTime.Hour()
	firstFetch := aw.cfg.LocalFirstFetchHOD
	interval := aw.cfg.ForecastRefreshInterval

	remainder := currentHourOfDay % interval
	var nextUpdateHour int
	if remainder < firstFetch {
		nextUpdateHour = (currentHourOfDay/interval)*interval + firstFetch
	} else {
		nextUpdateHour = ((currentHourOfDay/interval)+1)*interval + firstFetch
	}
	nextUpdateHour = nextUpdateHour % 24

	nextUpdateInHours := nextUpdateHour - currentHourOfDay
	if nextUpdateInHours <= 0 {
		nextUpdateInHours += 24
	}

	return currentHour + int64(nextUpdateInHours)*3600
}

// mapPoGoWeather maps AccuWeather icon codes to PoGo weather conditions (1-7).
// Port of weather.js mapPoGoWeather().
func mapPoGoWeather(f accuWeatherHourlyResponse) int {
	var icon int
	switch f.WeatherIcon {
	case 1, 2, 33, 34:
		icon = 1 // Clear
	case 3, 4, 35, 36:
		icon = 3 // Partly cloudy
	case 5, 6, 7, 8, 37, 38:
		icon = 4 // Cloudy
	case 11:
		return 7 // Fog
	case 12, 15, 18, 26, 29:
		return 2 // Rain
	case 13, 16, 20, 23, 40, 42:
		return 4 // Cloudy (overcast with precipitation)
	case 14, 17, 21, 39, 41:
		return 3 // Partly cloudy (mixed)
	case 19, 22, 24, 25, 43, 44:
		return 6 // Snow
	case 32:
		return 5 // Windy
	default:
		return 0
	}

	// Wind check for clear/partly cloudy/cloudy
	if f.Wind.Speed.Value > 20 || f.WindGust.Speed.Value > 30 {
		return 5 // Windy
	}
	return icon
}
