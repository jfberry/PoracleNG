package enrichment

import (
	"github.com/pokemon/poracleng/processor/internal/geo"
)

// Invasion builds enrichment fields for an invasion webhook.
func (e *Enricher) Invasion(lat, lon float64, expiration int64) map[string]interface{} {
	m := make(map[string]interface{})

	tz := geo.GetTimezone(lat, lon)

	addSunTimes(m, lat, lon, tz)

	if expiration > 0 {
		m["disappearTime"] = geo.FormatTime(expiration, tz, e.TimeLayout)
		m["tth"] = geo.ComputeTTH(expiration)
	}

	return m
}
