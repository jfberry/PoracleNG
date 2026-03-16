package enrichment

import (
	"time"

	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

// WeatherProvider looks up weather conditions for an S2 cell.
type WeatherProvider interface {
	GetCurrentWeatherInCell(cellID string) int
	GetWeatherForecast(cellID string) tracker.WeatherForecast
}

// ForecastProvider fetches external weather forecasts on demand.
type ForecastProvider interface {
	EnsureForecast(cellID string)
}

// ShinyRateProvider returns shiny rate for a pokemon.
type ShinyRateProvider interface {
	GetShinyRate(pokemonID int) float64
}

// Enricher computes additional fields to accompany webhook messages
// sent to the alerter. The enrichment map is sent alongside the original
// raw message so neither needs to be re-encoded.
type Enricher struct {
	TimeLayout       string
	DateLayout       string
	WeatherProvider  WeatherProvider
	ForecastProvider ForecastProvider  // optional; triggers AccuWeather fetch
	ShinyProvider    ShinyRateProvider // optional; provides shiny rates
	EventChecker     *PogoEventChecker
}

// New creates a new Enricher.
func New(timeLayout, dateLayout string, weather WeatherProvider, eventChecker *PogoEventChecker) *Enricher {
	return &Enricher{
		TimeLayout:      timeLayout,
		DateLayout:      dateLayout,
		WeatherProvider: weather,
		EventChecker:    eventChecker,
	}
}

// GetForecast returns the weather forecast for a cell, triggering an external
// forecast fetch if a ForecastProvider is configured.
func (e *Enricher) GetForecast(cellID string) tracker.WeatherForecast {
	if e.ForecastProvider != nil {
		e.ForecastProvider.EnsureForecast(cellID)
	}
	return e.WeatherProvider.GetWeatherForecast(cellID)
}

// addSunTimes adds nightTime/dawnTime/duskTime booleans to the enrichment map.
func addSunTimes(m map[string]any, lat, lon float64, tz string) {
	nowUnix := time.Now().Unix()
	sunTimes := geo.ComputeSunTimes(nowUnix, lat, lon, tz)
	m["nightTime"] = sunTimes.NightTime
	m["dawnTime"] = sunTimes.DawnTime
	m["duskTime"] = sunTimes.DuskTime
}
