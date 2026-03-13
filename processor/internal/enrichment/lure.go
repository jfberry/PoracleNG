package enrichment

import (
	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// Lure builds enrichment fields for a lure webhook.
func (e *Enricher) Lure(lure *webhook.LureWebhook) map[string]interface{} {
	m := make(map[string]interface{})

	if lure.LureExpiration > 0 {
		tz := geo.GetTimezone(lure.Latitude, lure.Longitude)
		m["disappearTime"] = geo.FormatTime(lure.LureExpiration, tz, e.TimeLayout)
		m["tth"] = geo.ComputeTTH(lure.LureExpiration)
		addSunTimes(m, lure.Latitude, lure.Longitude, tz)
	}

	return m
}
