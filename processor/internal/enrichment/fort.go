package enrichment

import (
	"math"

	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// FortUpdate builds enrichment fields for a fort_update webhook.
func (e *Enricher) FortUpdate(lat, lon float64, fortID string, fort *webhook.FortWebhook) (map[string]any, *staticmap.TilePending) {
	m := make(map[string]any)

	tz := geo.GetTimezone(lat, lon)
	addSunTimes(m, lat, lon, tz)

	// Map URLs
	e.addMapURLs(m, lat, lon, "pokestops", fortID)

	// Fort ID
	m["id"] = fortID

	// isEmpty — true if the fort has no name or description
	isEmpty := true
	if fort.New != nil && (fort.New.Name != "" || fort.New.Description != "") {
		isEmpty = false
	} else if fort.Old != nil && fort.Old.Name != "" {
		isEmpty = false
	}
	m["isEmpty"] = isEmpty

	// Fort type
	fortType := "unknown"
	if fort.New != nil && fort.New.FortType != "" {
		fortType = fort.New.FortType
	} else if fort.Old != nil && fort.Old.FortType != "" {
		fortType = fort.Old.FortType
	}
	m["fortType"] = fortType

	// Fort type display text
	if fortType == "pokestop" {
		m["fortTypeText"] = "Pokestop"
	} else {
		m["fortTypeText"] = "Gym"
	}

	// Change type flags
	changeType := fort.ChangeType
	if changeType == "edit" && (fort.Old == nil || (fort.Old.Name == "" && fort.Old.Description == "")) {
		changeType = "new"
	}
	changeTypes := fort.AllChangeTypes()

	m["isEdit"] = changeType == "edit"
	m["isNew"] = changeType == "new"
	m["isRemoval"] = changeType == "removal"

	switch changeType {
	case "edit":
		m["changeTypeText"] = "Edit"
	case "removal":
		m["changeTypeText"] = "Removal"
	case "new":
		m["changeTypeText"] = "New"
	default:
		m["changeTypeText"] = changeType
	}

	isEditLocation := false
	isEditName := false
	isEditDescription := false
	isEditImageUrl := false
	for _, ct := range changeTypes {
		switch ct {
		case "location":
			isEditLocation = true
		case "name":
			isEditName = true
		case "description":
			isEditDescription = true
		case "image_url":
			isEditImageUrl = true
		}
	}
	m["isEditLocation"] = isEditLocation
	m["isEditName"] = isEditName
	m["isEditDescription"] = isEditDescription
	m["isEditImageUrl"] = isEditImageUrl
	m["isEditImgUrl"] = isEditImageUrl

	// Fort name and description from snapshots
	name := "unknown"
	if fort.New != nil && fort.New.Name != "" {
		name = fort.New.Name
	} else if fort.Old != nil && fort.Old.Name != "" {
		name = fort.Old.Name
	}
	m["name"] = name

	description := ""
	if fort.New != nil && fort.New.Description != "" {
		description = fort.New.Description
	} else if fort.Old != nil && fort.Old.Description != "" {
		description = fort.Old.Description
	}
	m["description"] = description

	imgUrl := ""
	if fort.New != nil && fort.New.ImageURL != "" {
		imgUrl = fort.New.ImageURL
	} else if fort.Old != nil && fort.Old.ImageURL != "" {
		imgUrl = fort.Old.ImageURL
	}
	m["imgUrl"] = imgUrl
	m["stickerUrl"] = imgUrl

	// Old snapshot fields
	if fort.Old != nil {
		m["oldName"] = fort.Old.Name
		m["oldDescription"] = fort.Old.Description
		m["oldImageUrl"] = fort.Old.ImageURL
		m["oldImgUrl"] = fort.Old.ImageURL
		m["oldLatitude"] = fort.Old.Location.Lat
		m["oldLongitude"] = fort.Old.Location.Lon
	} else {
		m["oldName"] = ""
		m["oldDescription"] = ""
		m["oldImageUrl"] = ""
		m["oldImgUrl"] = ""
		m["oldLatitude"] = 0.0
		m["oldLongitude"] = 0.0
	}

	// New snapshot fields
	if fort.New != nil {
		m["newName"] = fort.New.Name
		m["newDescription"] = fort.New.Description
		m["newImageUrl"] = fort.New.ImageURL
		m["newImgUrl"] = fort.New.ImageURL
		m["newLatitude"] = fort.New.Location.Lat
		m["newLongitude"] = fort.New.Location.Lon
	} else {
		m["newName"] = ""
		m["newDescription"] = ""
		m["newImageUrl"] = ""
		m["newImgUrl"] = ""
		m["newLatitude"] = 0.0
		m["newLongitude"] = 0.0
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
	pending := e.addStaticMap(m, "fort-update", mapLat, mapLon, webhookFields)

	e.setFallbackImg(m, e.FallbackImgPokestop)

	return m, pending
}
