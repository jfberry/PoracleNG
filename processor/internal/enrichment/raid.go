package enrichment

import (
	"fmt"
	"time"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// Raid builds enrichment fields for a raid or egg webhook.
func (e *Enricher) Raid(raid *webhook.RaidWebhook, firstNotification bool, tileMode int) (map[string]any, *staticmap.TilePending) {
	m := make(map[string]any)
	m["pokemon_id"] = raid.PokemonID
	m["gym_id"] = raid.GymID
	m["firstNotification"] = firstNotification

	tz := geo.GetTimezone(raid.Latitude, raid.Longitude)

	addSunTimes(m, raid.Latitude, raid.Longitude, tz)

	// Cell weather
	cellID := tracker.GetWeatherCellID(raid.Latitude, raid.Longitude)
	m["gameWeatherId"] = e.WeatherProvider.GetCurrentWeatherInCell(cellID)

	// Icon URLs
	if raid.PokemonID > 0 {
		// Hatched raid pokemon icon
		if e.ImgUicons != nil {
			m["imgUrl"] = e.ImgUicons.PokemonIcon(raid.PokemonID, raid.Form, raid.Evolution, raid.Gender, raid.Costume, raid.Alignment, false, 0)
		}
		if e.ImgUiconsAlt != nil {
			m["imgUrlAlt"] = e.ImgUiconsAlt.PokemonIcon(raid.PokemonID, raid.Form, raid.Evolution, raid.Gender, raid.Costume, raid.Alignment, false, 0)
		}
		if e.StickerUicons != nil {
			m["stickerUrl"] = e.StickerUicons.PokemonIcon(raid.PokemonID, raid.Form, raid.Evolution, raid.Gender, raid.Costume, raid.Alignment, false, 0)
		}
	} else {
		// Egg icon
		if e.ImgUicons != nil {
			m["imgUrl"] = e.ImgUicons.EggIcon(raid.Level, false, false)
		}
		if e.ImgUiconsAlt != nil {
			m["imgUrlAlt"] = e.ImgUiconsAlt.EggIcon(raid.Level, false, false)
		}
		if e.StickerUicons != nil {
			m["stickerUrl"] = e.StickerUicons.EggIcon(raid.Level, false, false)
		}
	}

	// Unix timestamps as integers — avoids float64 scientific notation from
	// the raw webhook layer (Go JSON decodes numbers as float64 in map[string]any).
	// DTS templates use these for Discord timestamps: <t:{{start}}:R>
	m["start"] = raid.Start
	m["end"] = raid.End
	m["endTimestamp"] = raid.End       // unix int for Discord <t:N:R>
	m["hatchTimestamp"] = raid.Start   // unix int for Discord <t:N:R>

	if raid.PokemonID > 0 {
		// Hatched raid: disappearTime from end, tth from now to end
		m["disappearTime"] = geo.FormatTime(raid.End, tz, e.TimeLayout)
		m["tth"] = geo.ComputeTTH(raid.End)

		// Weather change timestamp: the hour boundary before end
		// (used later in per-language enrichment if forecast shows a boost change)
		m["weatherChangeTS"] = raid.End - (raid.End % 3600)

		// Weather forecast for boost change detection (triggers AccuWeather fetch if configured)
		forecast := e.GetForecast(cellID)
		m["weatherForecastCurrent"] = forecast.Current
		m["weatherForecastNext"] = forecast.Next
		m["nextHourTimestamp"] = tracker.GetNextHourTimestamp()
	} else {
		// Egg: hatchTime from start, tth from now to start, disappearTime from end
		m["hatchTime"] = geo.FormatTime(raid.Start, tz, e.TimeLayout)
		m["disappearTime"] = geo.FormatTime(raid.End, tz, e.TimeLayout)
		m["tth"] = geo.ComputeTTH(raid.Start)
	}

	// Format RSVP timeslots — only include future timeslots (matching alerter behavior).
	// Always set m["rsvps"] when the webhook has RSVP data to shadow the raw webhook
	// layer — otherwise {{#if rsvps}} would find the unfiltered raw array via the
	// webhook fields fallback and render expired entries with mismatched field names.
	if len(raid.RSVPs) > 0 {
		nowMs := time.Now().UnixMilli()
		var rsvpTimes []map[string]any
		for _, r := range raid.RSVPs {
			if r.Timeslot <= nowMs {
				continue // skip past timeslots
			}
			tsSec := (r.Timeslot + 999) / 1000 // ceil to seconds (matching alerter Math.ceil)
			rsvpTimes = append(rsvpTimes, map[string]any{
				"timeslot":    tsSec,
				"timeSlot":    tsSec,        // camelCase for DTS templates
				"going_count": r.GoingCount,
				"goingCount":  r.GoingCount, // camelCase for DTS templates
				"maybe_count": r.MaybeCount,
				"maybeCount":  r.MaybeCount, // camelCase for DTS templates
				"time":        geo.FormatTime(r.Timeslot/1000, tz, e.TimeLayout),
			})
		}
		m["rsvps"] = rsvpTimes // nil if all expired — shadows raw webhook rsvps
	}

	// Map URLs
	e.addMapURLs(m, raid.Latitude, raid.Longitude, "gyms", raid.GymID)

	// Campfire deep link
	m["campfireUrl"] = CampfireURL(raid.Latitude, raid.Longitude, raid.GymID, raid.GymName, raid.GymURL)

	// Reverse geocoding
	e.addGeoResult(m, raid.Latitude, raid.Longitude)

	// Static map tile
	pending := e.addStaticMap(m, "raid", raid.Latitude, raid.Longitude, map[string]any{
		"pokemon_id": raid.PokemonID,
		"form":       raid.Form,
		"level":      raid.Level,
		"teamId":     raid.TeamID,
		"evolution":  raid.Evolution,
		"costume":    raid.Costume,
	}, tileMode)

	// Game data enrichment
	if e.GameData != nil {
		gd := e.GameData

		// Team color
		if info, ok := gd.Util.Teams[raid.TeamID]; ok {
			m["gymColor"] = info.Color
			m["color"] = info.Color // deprecated alias
		}

		// Raid level name — pogo-translations uses identifier keys raid_1..raid_N,
		// NOT the English strings from util.json. util.json is only used to
		// enumerate valid levels (see bot/argmatch.go).
		if e.Translations != nil && raid.Level > 0 {
			key := fmt.Sprintf("raid_%d", raid.Level)
			m["levelNameEng"] = e.Translations.For("en").T(key)
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
				boostingWeathers := gd.GetBoostingWeathers(monster.Types)
				m["boostingWeatherIds"] = boostingWeathers
				m["boostingWeatherEmojiKeys"] = gd.GetWeatherEmojiKeys(boostingWeathers)

				// Evolution chain
				m["hasEvolutions"] = len(monster.Evolutions) > 0
				m["hasMegaEvolutions"] = len(monster.TempEvolutions) > 0

				// Shiny possible
				if e.ShinyProvider != nil {
					rate := e.ShinyProvider.GetShinyRate(raid.PokemonID)
					if rate > 0 {
						m["shinyPossible"] = true
						m["shinyPossibleEmojiKey"] = "shiny"
					} else {
						m["shinyPossible"] = false
					}
				}
			}
		}
	}

	if raid.PokemonID > 0 {
		e.setFallbackImg(m, e.FallbackImgURL)
	} else {
		e.setFallbackImg(m, e.FallbackImgEgg)
	}
	return m, pending
}

// RaidTranslate adds per-language translated fields to a raid enrichment map.
func (e *Enricher) RaidTranslate(base map[string]any, raid *webhook.RaidWebhook, lang string) map[string]any {
	if e.GameData == nil || e.Translations == nil {
		return nil
	}

	m := make(map[string]any, 15) // only translated fields; caller merges base + perLang

	gd := e.GameData
	tr := e.Translations.For(lang)

	// Team
	addTeamFields(m, gd, tr, e.Translations.For("en"), raid.TeamID)

	// Weather
	gameWeatherID := toInt(base["gameWeatherId"])
	m["gameWeatherName"] = TranslateWeatherName(tr, gameWeatherID)
	if gameWeatherID > 0 {
		if wInfo, ok := gd.Util.Weather[gameWeatherID]; ok {
			m["gameWeatherEmojiKey"] = wInfo.Emoji
		}
	}

	// Weather forecast names — only set when base enrichment determined a
	// meaningful weather change (weatherNext is set). When weatherCurrent is
	// 0 (de-boost case), weatherCurrentName falls back to the translated
	// "unknown" string, matching JS monster.js behavior.
	weatherNext, _ := base["weatherNext"].(int)
	if weatherNext > 0 {
		weatherCurrent, _ := base["weatherCurrent"].(int)
		if weatherCurrent > 0 {
			m["weatherCurrentName"] = TranslateWeatherName(tr, weatherCurrent)
			if wInfo, ok := gd.Util.Weather[weatherCurrent]; ok {
				m["weatherCurrentEmojiKey"] = wInfo.Emoji
			}
		} else {
			m["weatherCurrentName"] = tr.T("weather.unknown")
		}
		m["weatherNextName"] = TranslateWeatherName(tr, weatherNext)
		if wInfo, ok := gd.Util.Weather[weatherNext]; ok {
			m["weatherNextEmojiKey"] = wInfo.Emoji
		}
		m["weatherChangePossibleAt"] = tr.T("weather.possible_change_at")
	}

	// Level name
	// Raid level name — look up pogo-translations identifier key for the
	// user's language (raid_1..raid_N).
	if raid.Level > 0 {
		m["levelName"] = tr.T(fmt.Sprintf("raid_%d", raid.Level))
	}

	if raid.PokemonID > 0 {
		monster := gd.GetMonster(raid.PokemonID, raid.Form)
		if monster == nil {
			return m
		}

		// Pokemon name
		TranslateMonsterNamesEng(m, gd, tr, e.Translations, raid.PokemonID, raid.Form, raid.Evolution)

		enTr := e.Translations.For("en")

		// Type names
		TranslateTypeNames(m, tr, enTr, monster.Types)

		// Moves
		addMoveFields(m, gd, tr, enTr, raid.Move1, raid.Move2)

		// Weather boost
		weather := toInt(base["gameWeatherId"])
		addWeatherFields(m, gd, tr, monster.Types, weather)

		// Generation
		addGenerationFields(m, gd, tr, raid.PokemonID, raid.Form)

		// Gender
		addGenderFields(m, gd, tr, enTr, raid.Gender)

		// Evolution name + megaName
		if raid.Evolution > 0 {
			if info, ok := gd.Util.Evolution[raid.Evolution]; ok {
				m["evolutionName"] = tr.T(info.Name)
			}
			// megaName = fullName when evolved (mega/primal)
			if fn, ok := m["fullName"].(string); ok {
				m["megaName"] = fn
			}
		} else {
			// megaName = base pokemon name when not evolved
			if n, ok := m["name"].(string); ok {
				m["megaName"] = n
			}
		}

		// Weakness
		if weaknesses, ok := base["weaknessList"].([]gamedata.WeaknessCategory); ok {
			m["weaknessList"] = TranslateWeaknessCategories(weaknesses, tr, gd)
		}

		// Evolution chain (same helper as pokemon)
		evolutions, megaEvolutions := e.buildEvolutions(gd, tr, raid.PokemonID, raid.Form)
		m["evolutions"] = evolutions
		m["megaEvolutions"] = megaEvolutions
		m["prevEvolutions"] = e.buildPrevEvolutions(gd, tr, raid.PokemonID)
	}

	return m
}
