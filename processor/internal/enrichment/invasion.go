package enrichment

import (
	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

// Invasion builds enrichment fields for an invasion webhook.
func (e *Enricher) Invasion(lat, lon float64, expiration int64, pokestopID string, gruntTypeID, displayType, lureID int) (map[string]any, *staticmap.TilePending) {
	m := make(map[string]any)

	tz := geo.GetTimezone(lat, lon)
	addSunTimes(m, lat, lon, tz)

	cellID := tracker.GetWeatherCellID(lat, lon)
	m["gameWeatherId"] = e.WeatherProvider.GetCurrentWeatherInCell(cellID)

	if expiration > 0 {
		m["disappearTime"] = geo.FormatTime(expiration, tz, e.TimeLayout)
		m["tth"] = geo.ComputeTTH(expiration)
	}

	// Icon URLs: event invasions (displayType >= 7 with no grunt) use pokestop icon,
	// regular invasions use invasion icon
	if (gruntTypeID == 0) && displayType >= 7 {
		if e.ImgUicons != nil {
			m["imgUrl"] = e.ImgUicons.PokestopIcon(lureID, true, displayType, false)
		}
		if e.ImgUiconsAlt != nil {
			m["imgUrlAlt"] = e.ImgUiconsAlt.PokestopIcon(lureID, true, displayType, false)
		}
		if e.StickerUicons != nil {
			m["stickerUrl"] = e.StickerUicons.PokestopIcon(lureID, true, displayType, false)
		}
	} else {
		if e.ImgUicons != nil {
			m["imgUrl"] = e.ImgUicons.InvasionIcon(gruntTypeID)
		}
		if e.ImgUiconsAlt != nil {
			m["imgUrlAlt"] = e.ImgUiconsAlt.InvasionIcon(gruntTypeID)
		}
		if e.StickerUicons != nil {
			m["stickerUrl"] = e.StickerUicons.InvasionIcon(gruntTypeID)
		}
	}

	// Map URLs
	e.addMapURLs(m, lat, lon, "pokestops", pokestopID)

	// Reverse geocoding
	e.addGeoResult(m, lat, lon)

	// Static map tile — only pass non-zero IDs so tileserver template nil checks work
	tileFields := make(map[string]any)
	if gruntTypeID != 0 {
		tileFields["gruntTypeId"] = gruntTypeID
	}
	if displayType != 0 {
		tileFields["displayTypeId"] = displayType
	}
	if lureID != 0 {
		tileFields["lureTypeId"] = lureID
	}
	pending := e.addStaticMap(m, "pokestop", lat, lon, tileFields)

	// Grunt data
	if e.GameData != nil {
		grunt := e.GameData.GetGrunt(gruntTypeID)
		if grunt != nil {
			m["gruntType"] = grunt.Type
			m["gruntGender"] = grunt.Gender

			// Reward pokemon IDs for first slot
			firstRewards := grunt.EncountersByPosition("first")
			if len(firstRewards) > 0 {
				rewardIDs := make([]map[string]int, len(firstRewards))
				for i, r := range firstRewards {
					rewardIDs[i] = map[string]int{"pokemon_id": r.ID, "form": r.FormID}
				}
				m["gruntRewards"] = rewardIDs
			}
		}
	}

	return m, pending
}

// InvasionTranslate adds per-language translated fields.
func (e *Enricher) InvasionTranslate(base map[string]any, gruntTypeID int, lang string) map[string]any {
	if e.GameData == nil || e.Translations == nil {
		return base
	}

	m := make(map[string]any, len(base)+5)
	for k, v := range base {
		m[k] = v
	}

	gd := e.GameData
	tr := e.Translations.For(lang)
	gameWeatherID := toInt(base["gameWeatherId"])
	m["gameWeatherName"] = TranslateWeatherName(tr, gameWeatherID)
	if gameWeatherID > 0 {
		if wInfo, ok := gd.Util.Weather[gameWeatherID]; ok {
			m["gameWeatherEmojiKey"] = wInfo.Emoji
		}
	}

	// Grunt name
	grunt := e.GameData.GetGrunt(gruntTypeID)
	if grunt != nil {
		m["gruntName"] = tr.T(grunt.Name)
		m["gruntTypeName"] = tr.T(grunt.Type)
	}

	return m
}
