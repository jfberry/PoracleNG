package enrichment

import (
	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

// Maxbattle builds enrichment fields for a maxbattle webhook.
func (e *Enricher) Maxbattle(lat, lon float64, battleEnd int64) map[string]interface{} {
	m := make(map[string]interface{})

	tz := geo.GetTimezone(lat, lon)

	addSunTimes(m, lat, lon, tz)

	// Cell weather
	cellID := tracker.GetWeatherCellID(lat, lon)
	m["gameWeatherId"] = e.WeatherProvider.GetCurrentWeatherInCell(cellID)

	if battleEnd > 0 {
		m["disappearTime"] = geo.FormatTime(battleEnd, tz, e.TimeLayout)
		m["tth"] = geo.ComputeTTH(battleEnd)
	}

	return m
}
