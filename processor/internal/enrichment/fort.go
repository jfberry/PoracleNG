package enrichment

import (
	"math"

	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// FortUpdate builds enrichment fields for a fort_update webhook.
func (e *Enricher) FortUpdate(lat, lon float64, fortID string, fort *webhook.FortWebhook) map[string]any {
	m := make(map[string]any)

	tz := geo.GetTimezone(lat, lon)
	addSunTimes(m, lat, lon, tz)

	// Map URLs
	e.addMapURLs(m, lat, lon, "pokestops", fortID)

	// Autoposition: compute optimal zoom and center from old/new locations
	var markers []staticmap.LatLon
	if fort.Old != nil && fort.Old.Location.Lat != 0 {
		markers = append(markers, staticmap.LatLon{
			Latitude:  fort.Old.Location.Lat,
			Longitude: fort.Old.Location.Lon,
		})
	}
	if fort.New != nil && fort.New.Location.Lat != 0 {
		markers = append(markers, staticmap.LatLon{
			Latitude:  fort.New.Location.Lat,
			Longitude: fort.New.Location.Lon,
		})
	}

	position := staticmap.Autoposition(staticmap.AutopositionShape{
		Markers: markers,
	}, 500, 250, 1.25, 17.5)

	if position != nil {
		m["zoom"] = math.Min(position.Zoom, 16)
		m["map_latitude"] = position.Latitude
		m["map_longitude"] = position.Longitude
	}

	// Reverse geocoding
	e.addGeoResult(m, lat, lon)

	// Static map tile — use autopositioned center if available, else original coords
	mapLat, mapLon := lat, lon
	if position != nil {
		mapLat = position.Latitude
		mapLon = position.Longitude
	}
	e.addStaticMap(m, "fort-update", mapLat, mapLon, nil)

	return m
}
