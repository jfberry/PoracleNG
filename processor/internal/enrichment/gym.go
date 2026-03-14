package enrichment

import (
	"github.com/pokemon/poracleng/processor/internal/geo"
)

// Gym builds enrichment fields for a gym webhook.
func (e *Enricher) Gym(lat, lon float64) map[string]interface{} {
	m := make(map[string]interface{})

	tz := geo.GetTimezone(lat, lon)

	// conqueredTime is "now" formatted in the gym's local timezone
	m["conqueredTime"] = geo.FormatNow(tz, e.TimeLayout)

	// Gym has no real expiration — use a dummy 1-hour TTH
	m["tth"] = geo.TTH{Hours: 1}

	addSunTimes(m, lat, lon, tz)

	return m
}
