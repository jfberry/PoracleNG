package enrichment

import (
	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// Fort builds enrichment fields for a fort_update webhook.
// Expiration is reset_time + 7 days.
func (e *Enricher) Fort(fort *webhook.FortWebhook, resetTime int64) map[string]any {
	m := make(map[string]any)

	expiration := resetTime + 7*24*60*60
	tz := geo.GetTimezone(fort.Latitude, fort.Longitude)

	m["tth"] = geo.ComputeTTH(expiration)
	m["disappearTime"] = geo.FormatTime(expiration, tz, e.TimeLayout)
	m["disappearDate"] = geo.FormatTime(expiration, tz, e.DateLayout)
	m["resetTime"] = geo.FormatTime(resetTime, tz, e.TimeLayout)
	m["resetDate"] = geo.FormatTime(resetTime, tz, e.DateLayout)

	return m
}
