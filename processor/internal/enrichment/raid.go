package enrichment

import (
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// Raid builds enrichment fields for a raid or egg webhook.
func (e *Enricher) Raid(raid *webhook.RaidWebhook, firstNotification bool) map[string]any {
	m := make(map[string]any)
	m["firstNotification"] = firstNotification

	tz := geo.GetTimezone(raid.Latitude, raid.Longitude)

	addSunTimes(m, raid.Latitude, raid.Longitude, tz)

	// Cell weather
	cellID := tracker.GetWeatherCellID(raid.Latitude, raid.Longitude)
	m["gameWeatherId"] = e.WeatherProvider.GetCurrentWeatherInCell(cellID)

	if raid.PokemonID > 0 {
		// Hatched raid: disappearTime from end, tth from now to end
		m["disappearTime"] = geo.FormatTime(raid.End, tz, e.TimeLayout)
		m["tth"] = geo.ComputeTTH(raid.End)

		// Weather change time: the hour boundary before end
		weatherChangeTS := raid.End - (raid.End % 3600)
		m["weatherChangeTime"] = geo.FormatTime(weatherChangeTS, tz, e.TimeLayout)

		// Weather forecast for boost change detection (triggers AccuWeather fetch if configured)
		forecast := e.GetForecast(cellID)
		m["weatherForecastCurrent"] = forecast.Current
		m["weatherForecastNext"] = forecast.Next
		m["nextHourTimestamp"] = tracker.GetNextHourTimestamp()
	} else {
		// Egg: hatchTime from start, tth from now to start
		m["hatchTime"] = geo.FormatTime(raid.Start, tz, e.TimeLayout)
		m["tth"] = geo.ComputeTTH(raid.Start)
	}

	// Format RSVP timeslots
	if len(raid.RSVPs) > 0 {
		rsvpTimes := make([]map[string]any, len(raid.RSVPs))
		for i, r := range raid.RSVPs {
			rsvpTimes[i] = map[string]any{
				"timeslot":    r.Timeslot,
				"going_count": r.GoingCount,
				"maybe_count": r.MaybeCount,
				"time":        geo.FormatTime(r.Timeslot/1000, tz, e.TimeLayout),
			}
		}
		m["rsvps"] = rsvpTimes
	}

	// Map URLs
	e.addMapURLs(m, raid.Latitude, raid.Longitude, "gyms", raid.GymID)

	// Game data enrichment
	if e.GameData != nil {
		gd := e.GameData

		// Team color
		if info, ok := gd.Util.Teams[raid.TeamID]; ok {
			m["gymColor"] = info.Color
		}

		// Raid level name
		if levelName, ok := gd.Util.RaidLevels[raid.Level]; ok {
			m["levelNameEng"] = levelName
		}

		if raid.PokemonID > 0 {
			monster := gd.GetMonster(raid.PokemonID, raid.Form)
			if monster != nil {
				m["types"] = monster.Types
				m["typeEmojiKeys"] = gd.GetTypeEmojiKeys(monster.Types)
				m["baseStats"] = map[string]int{
					"baseAttack":  monster.Attack,
					"baseDefense": monster.Defense,
					"baseStamina": monster.Stamina,
				}

				// Generation
				gen := gd.GetGeneration(raid.PokemonID, raid.Form)
				m["generation"] = gen
				if info := gd.GetGenerationInfo(gen); info != nil {
					m["generationRoman"] = info.Roman
				}

				// Weakness
				m["weaknessList"] = gamedata.CalculateWeaknesses(monster.Types, gd.Types)

				// Weather boost
				m["boostingWeatherIds"] = gd.GetBoostingWeathers(monster.Types)
			}
		}
	}

	return m
}

// RaidTranslate adds per-language translated fields to a raid enrichment map.
func (e *Enricher) RaidTranslate(base map[string]any, raid *webhook.RaidWebhook, lang string) map[string]any {
	if e.GameData == nil || e.Translations == nil {
		return base
	}

	m := make(map[string]any, len(base)+15)
	for k, v := range base {
		m[k] = v
	}

	gd := e.GameData
	tr := e.Translations.For(lang)

	// Team
	addTeamFields(m, gd, tr, raid.TeamID)

	// Weather
	gameWeatherID := base["gameWeatherId"].(int)
	m["gameWeatherName"] = TranslateWeatherName(tr, gameWeatherID)
	if gameWeatherID > 0 {
		if wInfo, ok := gd.Util.Weather[gameWeatherID]; ok {
			m["gameWeatherEmojiKey"] = wInfo.Emoji
		}
	}

	// Level name
	if levelName, ok := base["levelNameEng"].(string); ok {
		m["levelName"] = tr.T(levelName)
	}

	if raid.PokemonID > 0 {
		monster := gd.GetMonster(raid.PokemonID, raid.Form)
		if monster == nil {
			return m
		}

		// Pokemon name
		TranslateMonsterNamesEng(m, gd, tr, e.Translations, raid.PokemonID, raid.Form, raid.Evolution)

		// Type names
		TranslateTypeNames(m, tr, monster.Types)

		// Moves
		addMoveFields(m, gd, tr, raid.Move1, raid.Move2)

		// Weather boost
		weather := base["gameWeatherId"].(int)
		addWeatherFields(m, gd, tr, monster.Types, weather)

		// Generation
		addGenerationFields(m, gd, tr, raid.PokemonID, raid.Form)

		// Gender
		addGenderFields(m, gd, tr, raid.Gender)

		// Evolution name
		if raid.Evolution > 0 {
			if info, ok := gd.Util.Evolution[raid.Evolution]; ok {
				m["evolutionName"] = tr.T(info.Name)
			}
		}

		// Weakness
		if weaknesses, ok := base["weaknessList"].([]gamedata.WeaknessCategory); ok {
			m["weaknessList"] = TranslateWeaknessCategories(weaknesses, tr)
		}
	}

	return m
}
