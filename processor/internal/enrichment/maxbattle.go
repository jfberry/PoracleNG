package enrichment

import (
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// Maxbattle builds enrichment fields for a maxbattle webhook.
func (e *Enricher) Maxbattle(lat, lon float64, battleEnd int64, mb *webhook.MaxbattleWebhook) (map[string]any, *staticmap.TilePending) {
	m := make(map[string]any)

	tz := geo.GetTimezone(lat, lon)
	addSunTimes(m, lat, lon, tz)

	cellID := tracker.GetWeatherCellID(lat, lon)
	m["gameWeatherId"] = e.WeatherProvider.GetCurrentWeatherInCell(cellID)

	if battleEnd > 0 {
		m["disappearTime"] = geo.FormatTime(battleEnd, tz, e.TimeLayout)
		m["tth"] = geo.ComputeTTH(battleEnd)

		// Weather change and forecast (same as raid)
		weatherChangeTS := battleEnd - (battleEnd % 3600)
		m["weatherChangeTime"] = geo.FormatTime(weatherChangeTS, tz, e.TimeLayout)
	}

	forecast := e.GetForecast(cellID)
	m["weatherForecastCurrent"] = forecast.Current
	m["weatherForecastNext"] = forecast.Next
	m["nextHourTimestamp"] = tracker.GetNextHourTimestamp()

	// Icon URLs
	if mb != nil && mb.BattlePokemonID > 0 {
		if e.ImgUicons != nil {
			m["imgUrl"] = e.ImgUicons.PokemonIcon(mb.BattlePokemonID, mb.BattlePokemonForm, 0, mb.BattlePokemonGender, mb.BattlePokemonCostume, mb.BattlePokemonAlignment, false)
		}
		if e.ImgUiconsAlt != nil {
			m["imgUrlAlt"] = e.ImgUiconsAlt.PokemonIcon(mb.BattlePokemonID, mb.BattlePokemonForm, 0, mb.BattlePokemonGender, mb.BattlePokemonCostume, mb.BattlePokemonAlignment, false)
		}
		if e.StickerUicons != nil {
			m["stickerUrl"] = e.StickerUicons.PokemonIcon(mb.BattlePokemonID, mb.BattlePokemonForm, 0, mb.BattlePokemonGender, mb.BattlePokemonCostume, mb.BattlePokemonAlignment, false)
		}
	}

	if mb == nil {
		e.addGeoResult(m, lat, lon)
		return m, nil
	}

	// Map URLs
	e.addMapURLs(m, lat, lon, "pokestops", mb.ID)

	// Reverse geocoding
	e.addGeoResult(m, lat, lon)

	// Static map tile
	pending := e.addStaticMap(m, "maxbattle", lat, lon, map[string]any{
		"battle_level":      mb.BattleLevel,
		"battle_pokemon_id": mb.BattlePokemonID,
	})

	m["color"] = "D000C0" // hardcoded maxbattle color (matches alerter)

	// Game data enrichment
	if e.GameData != nil {
		gd := e.GameData

		// Level name
		if levelName, ok := gd.Util.MaxbattleLevels[mb.BattleLevel]; ok {
			m["levelNameEng"] = levelName
		}

		// Battle pokemon data
		if mb.BattlePokemonID > 0 {
			monster := gd.GetMonster(mb.BattlePokemonID, mb.BattlePokemonForm)
			if monster != nil {
				m["types"] = monster.Types
				m["typeEmojiKeys"] = gd.GetTypeEmojiKeys(monster.Types)
				m["baseStats"] = map[string]int{
					"baseAttack":  monster.Attack,
					"baseDefense": monster.Defense,
					"baseStamina": monster.Stamina,
				}
				m["weaknessList"] = gamedata.CalculateWeaknesses(monster.Types, gd.Types)
			}
		}
	}

	e.setFallbackImg(m, e.FallbackImgURL)

	return m, pending
}

// MaxbattleTranslate adds per-language translated fields.
func (e *Enricher) MaxbattleTranslate(base map[string]any, mb *webhook.MaxbattleWebhook, lang string) map[string]any {
	if e.GameData == nil || e.Translations == nil || mb == nil {
		return nil
	}

	m := make(map[string]any, 10) // only translated fields; caller merges base + perLang

	gd := e.GameData
	tr := e.Translations.For(lang)

	gameWeatherID := toInt(base["gameWeatherId"])
	m["gameWeatherName"] = TranslateWeatherName(tr, gameWeatherID)
	if gameWeatherID > 0 {
		if wInfo, ok := gd.Util.Weather[gameWeatherID]; ok {
			m["gameWeatherEmojiKey"] = wInfo.Emoji
		}
	}

	if levelName, ok := base["levelNameEng"].(string); ok {
		m["levelName"] = tr.T(levelName)
	}

	if mb.BattlePokemonID > 0 {
		TranslateMonsterNamesEng(m, gd, tr, e.Translations, mb.BattlePokemonID, mb.BattlePokemonForm, 0)
		monster := gd.GetMonster(mb.BattlePokemonID, mb.BattlePokemonForm)
		if monster != nil {
			TranslateTypeNames(m, tr, e.Translations.For("en"), monster.Types)
			addWeatherFields(m, gd, tr, monster.Types, toInt(base["gameWeatherId"]))
			if weaknesses, ok := base["weaknessList"].([]gamedata.WeaknessCategory); ok {
				m["weaknessList"] = TranslateWeaknessCategories(weaknesses, tr, gd)
			}
		}
		addMoveFields(m, gd, tr, e.Translations.For("en"), mb.BattlePokemonMove1, mb.BattlePokemonMove2)
	}

	return m
}
