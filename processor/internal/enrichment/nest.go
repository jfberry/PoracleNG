package enrichment

import (
	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// Nest builds enrichment fields for a nest webhook.
// Expiration is reset_time + 7 days.
func (e *Enricher) Nest(nest *webhook.NestWebhook) map[string]any {
	m := make(map[string]any)

	expiration := nest.ResetTime + 7*24*60*60
	tz := geo.GetTimezone(nest.Latitude, nest.Longitude)

	m["tth"] = geo.ComputeTTH(expiration)
	m["disappearTime"] = geo.FormatTime(expiration, tz, e.TimeLayout)
	m["disappearDate"] = geo.FormatTime(expiration, tz, e.DateLayout)
	m["resetTime"] = geo.FormatTime(nest.ResetTime, tz, e.TimeLayout)
	m["resetDate"] = geo.FormatTime(nest.ResetTime, tz, e.DateLayout)

	return m
}
