package enrichment

import (
	"github.com/pokemon/poracleng/processor/internal/geo"
)

// FortUpdate builds enrichment fields for a fort_update webhook.
func (e *Enricher) FortUpdate(lat, lon float64, fortID string) map[string]any {
	m := make(map[string]any)

	tz := geo.GetTimezone(lat, lon)
	addSunTimes(m, lat, lon, tz)

	// Map URLs
	e.addMapURLs(m, lat, lon, "pokestops", fortID)

	// Static map tile
	m["latitude"] = lat
	m["longitude"] = lon
	e.addStaticMap(m, "fort-update")

	return m
}
