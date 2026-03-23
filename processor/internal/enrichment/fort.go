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

	// Fort type
	fortType := "unknown"
	if fort.New != nil && fort.New.FortType != "" {
		fortType = fort.New.FortType
	} else if fort.Old != nil && fort.Old.FortType != "" {
		fortType = fort.Old.FortType
	}
	m["fortType"] = fortType

	// Change type flags for tileserver template
	changeTypes := fort.AllChangeTypes()
	isEditLocation := false
	for _, ct := range changeTypes {
		if ct == "location" {
			isEditLocation = true
			break
		}
	}
	m["isEditLocation"] = isEditLocation

	// Old/new coordinates for location change tiles
	if fort.Old != nil {
		m["oldLatitude"] = fort.Old.Location.Lat
		m["oldLongitude"] = fort.Old.Location.Lon
	} else {
		m["oldLatitude"] = 0.0
		m["oldLongitude"] = 0.0
	}

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
	webhookFields := map[string]any{
		"isEditLocation": isEditLocation,
		"fortType":       fortType,
	}
	if fort.Old != nil {
		webhookFields["oldLatitude"] = fort.Old.Location.Lat
		webhookFields["oldLongitude"] = fort.Old.Location.Lon
	}
	if position != nil {
		webhookFields["zoom"] = math.Min(position.Zoom, 16)
		webhookFields["map_latitude"] = position.Latitude
		webhookFields["map_longitude"] = position.Longitude
	}
	e.addStaticMap(m, "fort-update", mapLat, mapLon, webhookFields)

	return m
}
