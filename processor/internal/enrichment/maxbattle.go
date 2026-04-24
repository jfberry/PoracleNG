package enrichment

import (
	"fmt"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// Maxbattle builds enrichment fields for a maxbattle webhook.
func (e *Enricher) Maxbattle(lat, lon float64, battleEnd int64, mb *webhook.MaxbattleWebhook, tileMode int) (map[string]any, *staticmap.TilePending) {
	m := make(map[string]any)

	tz := geo.GetTimezone(lat, lon)
	addSunTimes(m, lat, lon, tz)

	cellID := tracker.GetWeatherCellID(lat, lon)
	m["gameWeatherId"] = e.WeatherProvider.GetCurrentWeatherInCell(cellID)

	if battleEnd > 0 {
		m["battle_end"] = battleEnd
		m["endTimestamp"] = battleEnd // unix int for Discord <t:N:R>
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

	// Station identity and battle metadata for DTS templates
	if mb != nil {
		m["station_id"] = mb.ID
		m["station_name"] = mb.Name
		m["battle_start"] = mb.BattleStart
		m["total_stationed_pokemon"] = mb.TotalStationedPokemon
		m["total_stationed_gmax"] = mb.TotalStationedGmax
		m["bread_mode"] = mb.BattlePokemonBreadMode
		// Authoritative gmax from bread_mode (2=gigantamax). Fall back to the
		// battle-level heuristic when bread_mode is absent (older Golbat builds).
		gmax := 0
		if mb.BattlePokemonBreadMode == 2 {
			gmax = 1
		} else if mb.BattlePokemonBreadMode == 0 && mb.BattleLevel > 6 {
			gmax = 1
		}
		m["gmax"] = gmax
	}

	// Icon URLs — pass bread_mode so _b1 (Dynamax) / _b2 (Gigantamax) icons resolve
	if mb != nil && mb.BattlePokemonID > 0 {
		bread := mb.BattlePokemonBreadMode
		if e.ImgUicons != nil {
			m["imgUrl"] = e.ImgUicons.PokemonIcon(mb.BattlePokemonID, mb.BattlePokemonForm, 0, mb.BattlePokemonGender, mb.BattlePokemonCostume, mb.BattlePokemonAlignment, false, bread)
		}
		if e.ImgUiconsAlt != nil {
			m["imgUrlAlt"] = e.ImgUiconsAlt.PokemonIcon(mb.BattlePokemonID, mb.BattlePokemonForm, 0, mb.BattlePokemonGender, mb.BattlePokemonCostume, mb.BattlePokemonAlignment, false, bread)
		}
		if e.StickerUicons != nil {
			m["stickerUrl"] = e.StickerUicons.PokemonIcon(mb.BattlePokemonID, mb.BattlePokemonForm, 0, mb.BattlePokemonGender, mb.BattlePokemonCostume, mb.BattlePokemonAlignment, false, bread)
		}
	}

	if mb == nil {
		e.addGeoResult(m, lat, lon)
		return m, nil
	}

	// Map URLs — stations are their own entity type (ReactMap `/id/stations/{id}`)
	e.addMapURLs(m, lat, lon, "stations", mb.ID)

	// Reverse geocoding
	e.addGeoResult(m, lat, lon)

	// Static map tile
	pending := e.addStaticMap(m, "maxbattle", lat, lon, map[string]any{
		"battle_level":      mb.BattleLevel,
		"battle_pokemon_id": mb.BattlePokemonID,
	}, tileMode)

	m["color"] = "D000C0" // hardcoded maxbattle color (matches alerter)

	// Game data enrichment
	if e.GameData != nil {
		gd := e.GameData

		// Level name
		// Max battle level name — pogo-translations identifier keys
		// max_battle_1..max_battle_N. util.json's maxbattleLevels map is no
		// longer consulted for display strings.
		if e.Translations != nil && mb.BattleLevel > 0 {
			key := fmt.Sprintf("max_battle_%d", mb.BattleLevel)
			m["levelNameEng"] = e.Translations.For("en").T(key)
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

	if mb.BattleLevel > 0 {
		m["levelName"] = tr.T(fmt.Sprintf("max_battle_%d", mb.BattleLevel))
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
