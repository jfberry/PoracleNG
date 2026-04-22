package enrichment

import (
	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// Lure builds enrichment fields for a lure webhook.
func (e *Enricher) Lure(lure *webhook.LureWebhook, tileMode int) (map[string]any, *staticmap.TilePending) {
	m := make(map[string]any)

	// Pokestop identity — lure webhook uses "name" field, normalize to pokestop_name
	m["pokestop_id"] = lure.PokestopID
	m["pokestop_name"] = lure.Name

	if lure.LureExpiration > 0 {
		tz := geo.GetTimezone(lure.Latitude, lure.Longitude)
		m["lure_expiration"] = lure.LureExpiration
		m["expirationTimestamp"] = lure.LureExpiration // unix int for Discord <t:N:R>
		m["disappearTime"] = geo.FormatTime(lure.LureExpiration, tz, e.TimeLayout)
		m["tth"] = geo.ComputeTTH(lure.LureExpiration)
		addSunTimes(m, lure.Latitude, lure.Longitude, tz)
	}

	// Icon URLs
	if e.ImgUicons != nil {
		m["imgUrl"] = e.ImgUicons.PokestopIcon(lure.LureID, false, 0, false)
	}
	if e.ImgUiconsAlt != nil {
		m["imgUrlAlt"] = e.ImgUiconsAlt.PokestopIcon(lure.LureID, false, 0, false)
	}
	if e.StickerUicons != nil {
		m["stickerUrl"] = e.StickerUicons.PokestopIcon(lure.LureID, false, 0, false)
	}

	// Map URLs
	e.addMapURLs(m, lure.Latitude, lure.Longitude, "pokestops", lure.PokestopID)

	// Reverse geocoding
	e.addGeoResult(m, lure.Latitude, lure.Longitude)

	// Static map tile — only pass non-zero lureTypeId so tileserver template nil checks work
	var tileFields map[string]any
	if lure.LureID != 0 {
		tileFields = map[string]any{"lureTypeId": lure.LureID}
	}
	pending := e.addStaticMap(m, "pokestop", lure.Latitude, lure.Longitude, tileFields, tileMode)

	// Lure data from util.json
	m["lureTypeId"] = lure.LureID
	if e.GameData != nil {
		if info, ok := e.GameData.Util.Lures[lure.LureID]; ok {
			m["lureColor"] = info.Color
			m["lureEmojiKey"] = info.Emoji
			m["lureTypeNameEng"] = info.Name // util.json names are English
		}
	}

	// Invasion fields — a pokestop can have both a lure and an invasion
	gruntTypeID := lure.IncidentGruntType
	if gruntTypeID == 0 {
		gruntTypeID = lure.GruntType
	}
	displayType := lure.DisplayType
	if displayType == 0 {
		displayType = lure.IncidentDisplayType
	}
	m["gruntTypeId"] = gruntTypeID
	m["displayTypeId"] = displayType

	e.setFallbackImg(m, e.FallbackImgPokestop)
	if _, ok := m["pokestop_url"]; !ok && e.FallbackPokestopURL != "" {
		m["pokestop_url"] = e.FallbackPokestopURL
	}

	return m, pending
}

// LureTranslate adds per-language translated fields.
func (e *Enricher) LureTranslate(base map[string]any, lureID int, lang string) map[string]any {
	if e.GameData == nil || e.Translations == nil {
		return nil
	}

	m := make(map[string]any, 8)

	gd := e.GameData
	tr := e.Translations.For(lang)
	if info, ok := gd.Util.Lures[lureID]; ok {
		m["lureTypeName"] = tr.T(info.Name)
	}

	// Translate invasion fields if present on this pokestop
	gruntTypeID := toInt(base["gruntTypeId"])
	if gruntTypeID > 0 {
		grunt := gd.GetGrunt(gruntTypeID)
		if grunt != nil {
			m["gruntName"] = tr.T(grunt.CategoryKey())
			if typeKey := grunt.TypeKey(); typeKey != "" {
				m["gruntTypeName"] = tr.T(typeKey)
			}
		}
	}

	return m
}
