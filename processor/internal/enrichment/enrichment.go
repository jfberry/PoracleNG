package enrichment

import (
	"time"

	"github.com/pokemon/poracleng/processor/internal/geo"
)

// WeatherProvider looks up the current weather condition for an S2 cell.
type WeatherProvider interface {
	GetCurrentWeatherInCell(cellID string) int
}

// Enricher computes additional fields to accompany webhook messages
// sent to the alerter. The enrichment map is sent alongside the original
// raw message so neither needs to be re-encoded.
type Enricher struct {
	TimeLayout      string
	DateLayout      string
	WeatherProvider WeatherProvider
}

// New creates a new Enricher.
func New(timeLayout, dateLayout string, weather WeatherProvider) *Enricher {
	return &Enricher{
		TimeLayout:      timeLayout,
		DateLayout:      dateLayout,
		WeatherProvider: weather,
	}
}

// addSunTimes adds nightTime/dawnTime/duskTime booleans to the enrichment map.
func addSunTimes(m map[string]interface{}, lat, lon float64, tz string) {
	nowUnix := time.Now().Unix()
	sunTimes := geo.ComputeSunTimes(nowUnix, lat, lon, tz)
	m["nightTime"] = sunTimes.NightTime
	m["dawnTime"] = sunTimes.DawnTime
	m["duskTime"] = sunTimes.DuskTime
}
