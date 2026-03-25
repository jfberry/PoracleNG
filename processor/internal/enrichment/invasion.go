package enrichment

import (
	"fmt"
	"strings"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/i18n"
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

			// Type color and emoji key — map "Metal" to "Steel" for JS compatibility
			typeLookup := grunt.Type
			if typeLookup == "Metal" {
				typeLookup = "Steel"
			}
			if typeInfo, ok := e.GameData.Util.Types[typeLookup]; ok {
				m["gruntTypeColor"] = typeInfo.Color
				m["gruntTypeEmojiKey"] = typeInfo.Emoji
			}

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

		// Event invasions (gruntTypeID == 0 && displayType >= 7) use PokestopEvent data
		if gruntTypeID == 0 && displayType >= 7 {
			if eventInfo, ok := e.GameData.Util.PokestopEvent[displayType]; ok {
				m["gruntType"] = eventInfo.Name
				m["gruntTypeColor"] = eventInfo.Color
				m["gruntTypeEmojiKey"] = eventInfo.Emoji
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

	// Gender name and emoji
	gruntGender := toInt(base["gruntGender"])
	if genderInfo, ok := gd.Util.Genders[gruntGender]; ok {
		m["genderName"] = tr.T(genderInfo.Name)
		m["genderEmojiKey"] = genderInfo.Emoji
	}

	// Build gruntRewardsList with translated pokemon names
	if grunt != nil {
		type rewardSlot struct {
			chance     int
			encounters []gamedata.GruntEncounterEntry
		}

		var slots []rewardSlot

		if grunt.SecondReward {
			second := grunt.EncountersByPosition("second")
			if len(second) > 0 {
				first := grunt.EncountersByPosition("first")
				slots = append(slots, rewardSlot{chance: 85, encounters: first})
				slots = append(slots, rewardSlot{chance: 15, encounters: second})
			}
		}

		if len(slots) == 0 && grunt.ThirdReward {
			third := grunt.EncountersByPosition("third")
			if len(third) > 0 {
				slots = append(slots, rewardSlot{chance: 100, encounters: third})
			}
		}

		if len(slots) == 0 {
			first := grunt.EncountersByPosition("first")
			if len(first) > 0 {
				slots = append(slots, rewardSlot{chance: 100, encounters: first})
			}
		}

		if len(slots) > 0 {
			rewardsList := make([]map[string]any, 0, len(slots))
			var rewardsTextParts []string

			for _, slot := range slots {
				monsters := e.translateEncounterSlot(slot.encounters, gd, tr)
				entry := map[string]any{
					"chance":   slot.chance,
					"monsters": monsters,
				}
				rewardsList = append(rewardsList, entry)

				// Build flat text
				names := make([]string, len(monsters))
				for i, mon := range monsters {
					names[i] = fmt.Sprintf("%v", mon["fullName"])
				}
				joined := strings.Join(names, ", ")
				if len(slots) > 1 {
					rewardsTextParts = append(rewardsTextParts, fmt.Sprintf("%d%%: %s", slot.chance, joined))
				} else {
					rewardsTextParts = append(rewardsTextParts, joined)
				}
			}

			m["gruntRewardsList"] = rewardsList
			m["gruntRewards"] = strings.Join(rewardsTextParts, "\n")
		}
	}

	return m
}

// translateEncounterSlot translates a slice of grunt encounter entries into enrichment maps.
func (e *Enricher) translateEncounterSlot(entries []gamedata.GruntEncounterEntry, gd *gamedata.GameData, tr *i18n.Translator) []map[string]any {
	result := make([]map[string]any, len(entries))
	for i, enc := range entries {
		nameInfo := make(map[string]any)
		TranslateMonsterNames(nameInfo, gd, tr, enc.ID, enc.FormID, 0)
		result[i] = map[string]any{
			"id":       enc.ID,
			"formId":   enc.FormID,
			"name":     nameInfo["name"],
			"formName": nameInfo["formName"],
			"fullName": nameInfo["fullName"],
		}
	}
	return result
}
