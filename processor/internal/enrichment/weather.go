package enrichment

import (
	"github.com/pokemon/poracleng/processor/internal/geo"
)

// Weather builds enrichment fields for a weather change event.
func (e *Enricher) Weather(lat, lon float64) map[string]any {
	m := make(map[string]any)

	nextHour := geo.NextHourBoundary()
	m["weatherTth"] = geo.ComputeTTH(nextHour)

	if lat != 0 || lon != 0 {
		tz := geo.GetTimezone(lat, lon)
		addSunTimes(m, lat, lon, tz)
	}

	return m
}
