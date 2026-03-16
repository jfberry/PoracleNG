package tracker

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/golang/geo/s2"
	log "github.com/sirupsen/logrus"

	"github.com/pokemon/poracleng/processor/internal/metrics"
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

	mu            sync.Mutex
	keyUsage      map[string]int // date-key -> count
	keyUsageDate  string         // YYYY-MM-DD
	cellMutexes   map[string]*sync.Mutex
	cellLocations map[string]string // cellID -> AccuWeather location key
	cellForecasts map[string]*forecastState
}

type forecastState struct {
	forecastTimeout  int64 // unix timestamp when forecast expires
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
		if isQuotaExhaustedErr(err) {
			log.Warnf("AccuWeather: cannot get location for cell %s: %v", cellID, err)
		} else {
			log.Errorf("AccuWeather: failed to get location for cell %s: %v", cellID, err)
		}
		return
	}

	// Get API key
	apiKey, exhausted := aw.getLaziestKey()
	if apiKey == "" {
		if exhausted {
			log.Warn("AccuWeather: no API key available with free quota")
		} else {
			log.Error("AccuWeather: no API keys configured")
		}
		return
	}

	// Fetch 12-hour forecast
	url := fmt.Sprintf("https://dataservice.accuweather.com/forecasts/v1/hourly/12hour/%s?apikey=%s&details=true&metric=true", locationKey, apiKey)
	log.Debugf("AccuWeather: fetching forecast for cell %s", cellID)

	resp, err := aw.client.Get(url)
	if err != nil {
		metrics.AccuWeatherRequests.WithLabelValues("forecast", "error").Inc()
		log.Errorf("AccuWeather: forecast request failed for cell %s: %v", cellID, err)
		return
	}
	defer resp.Body.Close()

	aw.logRateLimitHeaders(resp, "forecast", apiKey)

	if resp.StatusCode != 200 {
		metrics.AccuWeatherRequests.WithLabelValues("forecast", fmt.Sprintf("http_%d", resp.StatusCode)).Inc()
		log.Errorf("AccuWeather: forecast request returned %d for cell %s", resp.StatusCode, cellID)
		return
	}

	metrics.AccuWeatherRequests.WithLabelValues("forecast", "success").Inc()

	var forecasts []accuWeatherHourlyResponse
	if err := json.NewDecoder(resp.Body).Decode(&forecasts); err != nil {
		log.Errorf("AccuWeather: failed to decode forecast for cell %s: %v", cellID, err)
		return
	}

	// Store forecast data in the weather tracker
	var logString strings.Builder
	for _, f := range forecasts {
		hourTS := f.EpochDateTime - (f.EpochDateTime % 3600)
		if hourTS >= currentHour {
			pogoWeather := mapPoGoWeather(f)
			aw.tracker.SetHourWeather(cellID, hourTS, pogoWeather)
			logString.WriteString(fmt.Sprintf("%s=%d ", time.Unix(hourTS, 0).UTC().Format("15:04"), pogoWeather))
		}
	}
	log.Infof("AccuWeather: cell %s forecast [UTC] %s", cellID, logString.String())

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

	// Convert S2 cell ID (numeric string) to lat/lon
	numID, err := strconv.ParseUint(cellID, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid cell ID %q: %w", cellID, err)
	}
	cell := s2.CellID(numID)
	ll := cell.LatLng()
	lat := ll.Lat.Degrees()
	lon := ll.Lng.Degrees()

	apiKey, exhausted := aw.getLaziestKey()
	if apiKey == "" {
		if exhausted {
			return "", fmt.Errorf("all API keys exhausted for today")
		}
		return "", fmt.Errorf("no API keys configured")
	}

	url := fmt.Sprintf("https://dataservice.accuweather.com/locations/v1/cities/geoposition/search?apikey=%s&q=%f%%2C%f", apiKey, lat, lon)
	log.Debugf("AccuWeather: fetching location for cell %s (%f, %f)", cellID, lat, lon)

	resp, err := aw.client.Get(url)
	if err != nil {
		metrics.AccuWeatherRequests.WithLabelValues("location", "error").Inc()
		return "", fmt.Errorf("location request failed: %w", err)
	}
	defer resp.Body.Close()

	aw.logRateLimitHeaders(resp, "location", apiKey)

	if resp.StatusCode != 200 {
		metrics.AccuWeatherRequests.WithLabelValues("location", fmt.Sprintf("http_%d", resp.StatusCode)).Inc()
		return "", fmt.Errorf("location request returned %d", resp.StatusCode)
	}

	metrics.AccuWeatherRequests.WithLabelValues("location", "success").Inc()

	var locResp accuWeatherLocationResponse
	if err := json.NewDecoder(resp.Body).Decode(&locResp); err != nil {
		return "", fmt.Errorf("failed to decode location response: %w", err)
	}

	aw.mu.Lock()
	aw.cellLocations[cellID] = locResp.Key
	aw.mu.Unlock()

	return locResp.Key, nil
}

// getLaziestKey returns the API key with the fewest calls today.
// Returns the key and true if keys exist but are exhausted, or "" and false if
// no keys are configured.
func (aw *AccuWeatherClient) getLaziestKey() (string, bool) {
	aw.mu.Lock()
	defer aw.mu.Unlock()

	if len(aw.cfg.APIKeys) == 0 {
		return "", false
	}

	today := time.Now().Format("2006-01-02")
	if aw.keyUsageDate != today {
		aw.keyUsage = make(map[string]int)
		aw.keyUsageDate = today
	}

	// Find the key with the fewest calls
	var bestKey string
	bestCount := math.MaxInt
	totalUsage := 0
	for i, key := range aw.cfg.APIKeys {
		if key == "" {
			continue
		}
		count := aw.keyUsage[key]
		totalUsage += count
		metrics.AccuWeatherKeyUsage.WithLabelValues(strconv.Itoa(i)).Set(float64(count))
		if count < bestCount {
			bestCount = count
			bestKey = key
		}
	}

	if bestKey == "" || bestCount >= aw.cfg.DayQuota {
		metrics.AccuWeatherQuotaExhausted.Inc()
		totalQuota := len(aw.cfg.APIKeys) * aw.cfg.DayQuota
		log.Warnf("AccuWeather: all API keys exhausted (%d/%d requests used today)", totalUsage, totalQuota)
		return "", true // keys exist but exhausted
	}

	aw.keyUsage[bestKey]++
	remaining := aw.cfg.DayQuota - (bestCount + 1)
	log.Debugf("AccuWeather: using key with %d/%d requests remaining today", remaining, aw.cfg.DayQuota)
	return bestKey, false
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

// logRateLimitHeaders reads AccuWeather rate limit headers, logs them, and
// corrects local key usage tracking from the authoritative server-side count.
func (aw *AccuWeatherClient) logRateLimitHeaders(resp *http.Response, requestType string, apiKey string) {
	remaining := resp.Header.Get("RateLimit-Remaining")
	if remaining == "" {
		return
	}
	remainingInt, err := strconv.Atoi(remaining)
	if err != nil {
		return
	}

	aw.mu.Lock()
	// Correct local usage from the server-reported remaining quota.
	// This self-corrects after restart: the first API call per key will
	// set keyUsage to the real value instead of 0.
	serverUsage := aw.cfg.DayQuota - remainingInt
	if serverUsage > aw.keyUsage[apiKey] {
		log.Debugf("AccuWeather: correcting local usage for key from %d to %d (server reports %d remaining)",
			aw.keyUsage[apiKey], serverUsage, remainingInt)
		aw.keyUsage[apiKey] = serverUsage
	}

	// Update per-key metrics
	for i, key := range aw.cfg.APIKeys {
		if key == apiKey {
			metrics.AccuWeatherQuotaRemaining.WithLabelValues(strconv.Itoa(i)).Set(float64(remainingInt))
			metrics.AccuWeatherKeyUsage.WithLabelValues(strconv.Itoa(i)).Set(float64(aw.keyUsage[apiKey]))
			break
		}
	}
	aw.mu.Unlock()

	if remainingInt < 10 {
		log.Warnf("AccuWeather: %s response — only %d API requests remaining", requestType, remainingInt)
	} else {
		log.Debugf("AccuWeather: %s response — %d API requests remaining", requestType, remainingInt)
	}
}

// isQuotaExhaustedErr checks if the error is due to API key quota exhaustion.
func isQuotaExhaustedErr(err error) bool {
	return err != nil && err.Error() == "all API keys exhausted for today"
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
