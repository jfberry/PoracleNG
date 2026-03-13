package enrichment

import (
	"github.com/pokemon/poracleng/processor/internal/geo"
)

// Quest builds enrichment fields for a quest webhook.
func (e *Enricher) Quest(lat, lon float64) map[string]interface{} {
	m := make(map[string]interface{})

	endOfDay := geo.EndOfDay(lat, lon)
	tz := geo.GetTimezone(lat, lon)
	m["disappearTime"] = geo.FormatTime(endOfDay, tz, e.TimeLayout)
	m["disappear_time"] = endOfDay
	m["tth"] = geo.ComputeTTH(endOfDay)

	return m
}
