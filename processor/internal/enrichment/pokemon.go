package enrichment

import (
	"fmt"
	"math"
	"time"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/staticmap"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// Pokemon builds enrichment fields for a pokemon webhook.
// Returns a base enrichment map (universal fields) and, if GameData is loaded,
// also includes game data enrichment (types, weakness, stats, maps, etc.).
func (e *Enricher) Pokemon(pokemon *webhook.PokemonWebhook, processed *matching.ProcessedPokemon, tileMode int) (map[string]any, *staticmap.TilePending) {
	verified := pokemon.DisappearTimeVerified || pokemon.Verified
	m := map[string]any{
		"pokemon_id":       pokemon.PokemonID,
		"pokemonId":        pokemon.PokemonID,
		"verified":         verified,
		"confirmedTime":    verified,
		"rarityGroup":      processed.RarityGroup,
		"pvpBestRank":      processed.PVPBestRank,
		"pvpEvolutionData": processed.PVPEvoData,
	}

	// Best rank per league (scalar convenience for templates)
	if processed.PVPBestRank != nil {
		for league, ranks := range processed.PVPBestRank {
			bestRank := 4096
			bestCP := 0
			for _, r := range ranks {
				if r.Rank < bestRank {
					bestRank = r.Rank
					bestCP = r.CP
				}
			}
			switch league {
			case 500:
				m["bestLittleLeagueRank"] = bestRank
				m["bestLittleLeagueRankCP"] = bestCP
			case 1500:
				m["bestGreatLeagueRank"] = bestRank
				m["bestGreatLeagueRankCP"] = bestCP
			case 2500:
				m["bestUltraLeagueRank"] = bestRank
				m["bestUltraLeagueRankCP"] = bestCP
			}
		}
	}

	// Shiny rate and shiny possible
	if e.ShinyProvider != nil {
		rate := e.ShinyProvider.GetShinyRate(pokemon.PokemonID)
		if rate > 0 {
			m["shinyStats"] = int(math.Round(rate))
			m["shinyPossible"] = true
			m["shinyPossibleEmojiKey"] = "shiny"
		} else {
			m["shinyPossible"] = false
		}
	}

	// Cell weather
	cellID := tracker.GetWeatherCellID(pokemon.Latitude, pokemon.Longitude)
	m["gameWeatherId"] = e.WeatherProvider.GetCurrentWeatherInCell(cellID)

	// Weather forecast for boost change detection (triggers AccuWeather fetch if configured)
	forecast := e.GetForecast(cellID)
	m["weatherForecastCurrent"] = forecast.Current
	m["weatherForecastNext"] = forecast.Next
	m["nextHourTimestamp"] = tracker.GetNextHourTimestamp()

	// Time enrichment
	if pokemon.DisappearTime > 0 {
		tz := geo.GetTimezone(pokemon.Latitude, pokemon.Longitude)
		m["disappear_time"] = pokemon.DisappearTime
		m["despawnTimestamp"] = pokemon.DisappearTime // unix int for Discord <t:N:R>
		m["disappearTime"] = geo.FormatTime(pokemon.DisappearTime, tz, e.TimeLayout)
		m["tth"] = geo.ComputeTTH(pokemon.DisappearTime)
		tthSec := max(pokemon.DisappearTime-time.Now().Unix(), 0)
		m["tthSeconds"] = int(tthSec)

		// Weather change timestamp: the hour boundary before disappear_time
		// (used later if forecast shows a boost change)
		m["weatherChangeTS"] = pokemon.DisappearTime - (pokemon.DisappearTime % 3600)

		addSunTimes(m, pokemon.Latitude, pokemon.Longitude, tz)

		// Future event check
		if e.EventChecker != nil {
			now := time.Now().Unix()
			if result := e.EventChecker.EventChangesSpawn(now, pokemon.DisappearTime, tz); result != nil {
				m["futureEvent"] = result.FutureEvent
				m["futureEventTime"] = result.FutureEventTime
				m["futureEventName"] = result.FutureEventName
				m["futureEventTrigger"] = result.FutureEventTrigger
			}
		}
	}

	// S2 cell coords for cell-spawned pokemon
	isCellSpawn := pokemon.SeenType == "nearby_cell" ||
		(pokemon.PokestopID == "None" && pokemon.SpawnpointID == "None")
	if isCellSpawn {
		m["cell_coords"] = geo.GetCellCoords(pokemon.Latitude, pokemon.Longitude, 15)
	}

	// Encountered status
	encountered := pokemon.IndividualAttack != nil
	m["encountered"] = encountered

	// Compute IV/stats fields
	if encountered {
		atk, def, sta := *pokemon.IndividualAttack, 0, 0
		if pokemon.IndividualDefense != nil {
			def = *pokemon.IndividualDefense
		}
		if pokemon.IndividualStamina != nil {
			sta = *pokemon.IndividualStamina
		}
		iv := float64(atk+def+sta) / 0.45
		m["iv"] = fmt.Sprintf("%.2f", iv)
		m["atk"] = atk
		m["def"] = def
		m["sta"] = sta
		m["cp"] = pokemon.CP
		m["level"] = pokemon.PokemonLevel

		// IV color
		m["ivColor"] = gamedata.FindIvColor(iv, e.IvColors)

		// Weight and height (formatted to 2dp, matching alerter)
		m["weight"] = fmt.Sprintf("%.2f", pokemon.Weight)
		m["height"] = fmt.Sprintf("%.2f", pokemon.Height)

		// Catch rates
		m["catchBase"] = fmt.Sprintf("%.2f", pokemon.BaseCatch*100)
		m["catchGreat"] = fmt.Sprintf("%.2f", pokemon.GreatCatch*100)
		m["catchUltra"] = fmt.Sprintf("%.2f", pokemon.UltraCatch*100)
	} else {
		m["iv"] = -1
		m["atk"] = 0
		m["def"] = 0
		m["sta"] = 0
		m["cp"] = 0
		m["level"] = 0
		m["catchBase"] = 0
		m["catchGreat"] = 0
		m["catchUltra"] = 0
	}

	// Seen type
	m["seenType"] = computeSeenType(pokemon)

	// Icon URLs
	shinyPossible, _ := m["shinyPossible"].(bool)
	shiny := shinyPossible && e.RequestShinyImages
	if e.ImgUicons != nil {
		m["imgUrl"] = e.ImgUicons.PokemonIcon(pokemon.PokemonID, pokemon.Form, 0, pokemon.Gender, pokemon.Costume, 0, shiny)
	}
	if e.ImgUiconsAlt != nil {
		m["imgUrlAlt"] = e.ImgUiconsAlt.PokemonIcon(pokemon.PokemonID, pokemon.Form, 0, pokemon.Gender, pokemon.Costume, 0, shiny)
	}
	if e.StickerUicons != nil {
		m["stickerUrl"] = e.StickerUicons.PokemonIcon(pokemon.PokemonID, pokemon.Form, 0, pokemon.Gender, pokemon.Costume, 0, shiny)
	}

	// Game data enrichment (types, weakness, stats, maps, generation, etc.)
	if e.GameData != nil {
		e.enrichPokemonGameData(m, pokemon, encountered)
	}

	// Reverse geocoding
	e.addGeoResult(m, pokemon.Latitude, pokemon.Longitude)

	// Static map tile
	weather := pokemon.BoostedWeather
	if weather == 0 {
		weather = pokemon.Weather
	}
	pending := e.addStaticMap(m, "monster", pokemon.Latitude, pokemon.Longitude, map[string]any{
		"pokemon_id":         pokemon.PokemonID,
		"display_pokemon_id": pokemon.DisplayPokemonID,
		"pokemonId":          pokemon.PokemonID,
		"costume":            pokemon.Costume,
		"form":               pokemon.Form,
		"verified":           pokemon.DisappearTimeVerified || pokemon.Verified,
		"confirmedTime":      pokemon.DisappearTimeVerified || pokemon.Verified,
		"weather":            weather,
		"seen_type":          pokemon.SeenType,
	}, tileMode)

	e.setFallbackImg(m, e.FallbackImgURL)
	return m, pending
}

// enrichPokemonGameData adds game data enrichment fields that are universal
// (not per-language). Per-language fields are added by PokemonTranslate.
func (e *Enricher) enrichPokemonGameData(m map[string]any, pokemon *webhook.PokemonWebhook, encountered bool) {
	gd := e.GameData
	monster := gd.GetMonster(pokemon.PokemonID, pokemon.Form)
	if monster == nil {
		return
	}

	// Types
	m["types"] = monster.Types
	m["typeEmojiKeys"] = gd.GetTypeEmojiKeys(monster.Types)
	m["color"] = gd.GetTypeColor(monster.Types)

	// Base stats
	m["baseStats"] = map[string]int{
		"baseAttack":  monster.Attack,
		"baseDefense": monster.Defense,
		"baseStamina": monster.Stamina,
	}

	// Generation
	gen := gd.GetGeneration(pokemon.PokemonID, pokemon.Form)
	m["generation"] = gen
	if info := gd.GetGenerationInfo(gen); info != nil {
		m["generationRoman"] = info.Roman
	}

	// Weather boost
	weather := pokemon.BoostedWeather
	if weather == 0 {
		weather = pokemon.Weather
	}
	m["boostingWeatherIds"] = gd.GetBoostingWeathers(monster.Types)
	m["alteringWeathers"] = gd.GetAlteringWeathers(monster.Types, weather)

	// Weakness calculation
	m["weaknessList"] = gamedata.CalculateWeaknesses(monster.Types, gd.Types)

	// Weather forecast impact
	if pokemon.DisappearTime > 0 {
		nextHourTS, _ := m["nextHourTimestamp"].(int64)
		if nextHourTS > 0 && pokemon.DisappearTime > nextHourTS {
			forecastCurrent, _ := m["weatherForecastCurrent"].(int)
			forecastNext, _ := m["weatherForecastNext"].(int)
			weather := pokemon.BoostedWeather
			if weather == 0 {
				weather = pokemon.Weather
			}

			if forecastNext > 0 {
				pokemonShouldBeBoosted := forecastCurrent > 0 && gd.IsBoostedByWeather(monster.Types, forecastCurrent)

				boostMayChange := (weather > 0 && forecastNext != weather) ||
					(forecastCurrent > 0 && forecastNext != forecastCurrent) ||
					(pokemonShouldBeBoosted && weather == 0)

				if boostMayChange {
					pokemonWillBeBoosted := gd.IsBoostedByWeather(monster.Types, forecastNext)

					if (weather > 0 && !pokemonWillBeBoosted) || (weather == 0 && pokemonWillBeBoosted) {
						if pokemonShouldBeBoosted && weather == 0 {
							m["weatherCurrent"] = 0
						} else if weather > 0 {
							m["weatherCurrent"] = weather
						} else {
							m["weatherCurrent"] = forecastCurrent
						}
						m["weatherNext"] = forecastNext
						// Format weatherChangeTime only when change matters (matching JS)
						// Strip seconds — JS uses .slice(0, -3) to remove ":SS"
						if ts, ok := m["weatherChangeTS"].(int64); ok {
							tz := geo.GetTimezone(pokemon.Latitude, pokemon.Longitude)
							formatted := geo.FormatTime(ts, tz, e.TimeLayout)
							if len(formatted) >= 3 {
								formatted = formatted[:len(formatted)-3]
							}
							m["weatherChangeTime"] = formatted
						}
					}
				}
			}
		}
	}

	// Map URLs
	e.addMapURLs(m, pokemon.Latitude, pokemon.Longitude, "pokemon", pokemon.EncounterID)

	// Move type emoji keys for encountered pokemon
	if encountered {
		if quickMove := gd.GetMove(pokemon.Move1); quickMove != nil && quickMove.TypeID > 0 {
			if ti, ok := gd.Types[quickMove.TypeID]; ok {
				m["quickMoveTypeEmojiKey"] = ti.Emoji
			}
		}
		if chargeMove := gd.GetMove(pokemon.Move2); chargeMove != nil && chargeMove.TypeID > 0 {
			if ti, ok := gd.Types[chargeMove.TypeID]; ok {
				m["chargeMoveTypeEmojiKey"] = ti.Emoji
			}
		}
	}

	// Disguise pokemon info
	if pokemon.DisplayPokemonID > 0 && pokemon.DisplayPokemonID != pokemon.PokemonID {
		m["disguisePokemonId"] = pokemon.DisplayPokemonID
		m["disguiseFormId"] = pokemon.DisplayForm
	}

	// Evolution data
	m["hasEvolutions"] = len(monster.Evolutions) > 0
	m["hasMegaEvolutions"] = len(monster.TempEvolutions) > 0

	// Legendary/mythic/ultra beast flags
	m["legendary"] = monster.Legendary
	m["mythic"] = monster.Mythic
	m["ultraBeast"] = monster.UltraBeast
}

// PokemonTranslate adds per-language translated fields to a pokemon enrichment map.
// Call this once per distinct language among matched users.
func (e *Enricher) PokemonTranslate(base map[string]any, pokemon *webhook.PokemonWebhook, lang string) map[string]any {
	if e.GameData == nil || e.Translations == nil {
		return nil
	}

	m := make(map[string]any, 20) // only translated fields; caller merges base + perLang

	gd := e.GameData
	tr := e.Translations.For(lang)
	enTr := e.Translations.For("en")
	monster := gd.GetMonster(pokemon.PokemonID, pokemon.Form)
	if monster == nil {
		return m
	}

	// Pokemon name, form name, full name
	TranslateMonsterNamesEng(m, gd, tr, e.Translations, pokemon.PokemonID, pokemon.Form, 0)

	// Type names
	TranslateTypeNames(m, tr, enTr, monster.Types)

	// Move names
	encountered, _ := base["encountered"].(bool)
	if encountered {
		addMoveFields(m, gd, tr, enTr, pokemon.Move1, pokemon.Move2)
	} else {
		m["quickMoveName"] = ""
		m["chargeMoveName"] = ""
	}

	// Weather names
	weather := pokemon.BoostedWeather
	if weather == 0 {
		weather = pokemon.Weather
	}
	addWeatherFields(m, gd, tr, monster.Types, weather)
	gameWeatherID := toInt(base["gameWeatherId"])
	m["gameWeatherName"] = TranslateWeatherName(tr, gameWeatherID)
	if gameWeatherID > 0 {
		if wInfo, ok := gd.Util.Weather[gameWeatherID]; ok {
			m["gameWeatherEmojiKey"] = wInfo.Emoji
		}
	}

	// Weather forecast names — only set when base enrichment determined a
	// meaningful weather change (weatherNext is set). Use the processed
	// weatherCurrent (which may be overridden to the pokemon's actual boost
	// weather), not the raw forecastCurrent — matching JS behavior.
	// When weatherCurrent is 0 (de-boost case), weatherCurrentName gets the
	// translated "unknown" string, matching JS monster.js which sets
	// weatherCurrentName = translator.translate('unknown').
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

	// Generation name
	addGenerationFields(m, gd, tr, pokemon.PokemonID, pokemon.Form)

	// Gender name
	addGenderFields(m, gd, tr, enTr, pokemon.Gender)

	// Rarity name
	rarityGroup, _ := base["rarityGroup"].(int)
	addRarityFields(m, gd, tr, rarityGroup)

	// Size name
	addSizeFields(m, gd, tr, pokemon.Size)

	// Translated weakness list
	if weaknesses, ok := base["weaknessList"].([]gamedata.WeaknessCategory); ok {
		m["weaknessList"] = TranslateWeaknessCategories(weaknesses, tr, gd)
	}

	// Evolution chain
	evolutions, megaEvolutions := e.buildEvolutions(gd, tr, pokemon.PokemonID, pokemon.Form)
	m["evolutions"] = evolutions
	m["megaEvolutions"] = megaEvolutions

	// Previous evolutions (what evolves into this pokemon)
	m["prevEvolutions"] = e.buildPrevEvolutions(gd, tr, pokemon.PokemonID)

	// Disguise name
	if dpID, ok := base["disguisePokemonId"].(int); ok {
		dpForm, _ := base["disguiseFormId"].(int)
		disguiseM := make(map[string]any)
		TranslateMonsterNames(disguiseM, gd, tr, dpID, dpForm, 0)
		m["disguisePokemonName"] = disguiseM["name"]
		m["disguiseFormName"] = disguiseM["formName"]
	}

	// Pre-enrich PVP rank entries with translated names and computed fields.
	// The alerter only needs to do per-user filter matching on these.
	e.enrichPvpRankings(m, gd, tr, pokemon)

	return m
}

// enrichPvpRankings pre-enriches PVP ranking entries with translated pokemon
// names, form names, base stats, percentage formatting, and levelWithCap.
// This removes the need for GameData.monsters lookups in the alerter's
// createPvpDisplay per-user loop.
func (e *Enricher) enrichPvpRankings(m map[string]any, gd *gamedata.GameData, tr *i18n.Translator, pokemon *webhook.PokemonWebhook) {
	// Collect league data from both new (pvp) and legacy (pvp_rankings_*_league) fields
	leagueMap := make(map[string][]webhook.PVPRankEntry)
	if pokemon.PVP != nil {
		for league, entries := range pokemon.PVP {
			leagueMap[league] = append(leagueMap[league], entries...)
		}
	}
	if pokemon.PVPRankingsGreatLeague != nil {
		leagueMap["great"] = append(leagueMap["great"], pokemon.PVPRankingsGreatLeague...)
	}
	if pokemon.PVPRankingsUltraLeague != nil {
		leagueMap["ultra"] = append(leagueMap["ultra"], pokemon.PVPRankingsUltraLeague...)
	}
	if pokemon.PVPRankingsLittleLeague != nil {
		leagueMap["little"] = append(leagueMap["little"], pokemon.PVPRankingsLittleLeague...)
	}

	if len(leagueMap) == 0 {
		return
	}

	for leagueName, entries := range leagueMap {
		enriched := make([]map[string]any, 0, len(entries))
		for _, rank := range entries {
			if rank.Rank <= 0 {
				continue
			}

			// Format percentage: Golbat sends 0-1 fraction, templates expect 0-100
			var pctFormatted string
			if rank.Percentage <= 1 {
				pctFormatted = fmt.Sprintf("%.2f", rank.Percentage*100)
			} else {
				pctFormatted = fmt.Sprintf("%.2f", rank.Percentage)
			}

			entry := map[string]any{
				"rank":       rank.Rank,
				"cp":         rank.CP,
				"level":      rank.Level,
				"cap":        rank.Cap,
				"capped":     rank.Capped,
				"pokemon":    rank.Pokemon,
				"form":       rank.Form,
				"evolution":  rank.Evolution,
				"percentage": pctFormatted,
			}

			if rank.Cap > 0 && !rank.Capped {
				entry["levelWithCap"] = fmt.Sprintf("%v/%d", rank.Level, rank.Cap)
			} else {
				entry["levelWithCap"] = rank.Level
			}

			// Monster name + stats lookup
			formID := rank.Form
			mon := gd.GetMonster(rank.Pokemon, formID)
			if mon != nil {
				nameInfo := make(map[string]any)
				TranslateMonsterNamesEng(nameInfo, gd, tr, e.Translations, rank.Pokemon, formID, rank.Evolution)
				entry["name"] = nameInfo["name"]
				entry["fullName"] = nameInfo["fullName"]
				entry["formName"] = nameInfo["formName"]
				entry["formNormalised"] = nameInfo["formNormalised"]
				entry["nameEng"] = nameInfo["nameEng"]
				entry["fullNameEng"] = nameInfo["fullNameEng"]
				entry["formNormalisedEng"] = nameInfo["formNormalisedEng"]
				entry["baseStats"] = map[string]int{
					"baseAttack":  mon.Attack,
					"baseDefense": mon.Defense,
					"baseStamina": mon.Stamina,
				}
				// Flat base stats for DTS templates using {{this.baseAttack}}
				entry["baseAttack"] = mon.Attack
				entry["baseDefense"] = mon.Defense
				entry["baseStamina"] = mon.Stamina
			} else {
				entry["name"] = fmt.Sprintf("Pokemon %d", rank.Pokemon)
				entry["fullName"] = fmt.Sprintf("Pokemon %d", rank.Pokemon)
				entry["formName"] = ""
				entry["formNormalised"] = ""
				entry["nameEng"] = fmt.Sprintf("Pokemon %d", rank.Pokemon)
				entry["fullNameEng"] = fmt.Sprintf("Pokemon %d", rank.Pokemon)
				entry["formNormalisedEng"] = ""
				entry["baseStats"] = map[string]int{
					"baseAttack": 0, "baseDefense": 0, "baseStamina": 0,
				}
				entry["baseAttack"] = 0
				entry["baseDefense"] = 0
				entry["baseStamina"] = 0
			}

			enriched = append(enriched, entry)
		}
		m[fmt.Sprintf("pvpEnriched_%s_league", leagueName)] = enriched
		// Also store under the original webhook key so existing DTS templates
		// using {{#each pvp_rankings_great_league}} get the enriched entries
		// (with levelWithCap, name, fullName, etc.) instead of raw webhook data.
		m[fmt.Sprintf("pvp_rankings_%s_league", leagueName)] = enriched
	}
}

// buildEvolutions walks the evolution tree from GameData and returns translated
// evolution and mega evolution info for use in templates.
func (e *Enricher) buildEvolutions(gd *gamedata.GameData, tr *i18n.Translator, pokemonID, form int) ([]map[string]any, []map[string]any) {
	monster := gd.GetMonster(pokemonID, form)
	if monster == nil {
		return nil, nil
	}

	var evolutions []map[string]any
	var megaEvolutions []map[string]any

	// Walk evolution chain (max depth 10 to prevent cycles)
	var walk func(m *gamedata.Monster, depth int)
	walk = func(m *gamedata.Monster, depth int) {
		if depth >= 10 {
			return
		}
		for _, evo := range m.Evolutions {
			evoMon := gd.GetMonster(evo.PokemonID, evo.FormID)
			if evoMon == nil {
				continue
			}

			nameInfo := make(map[string]any)
			TranslateMonsterNames(nameInfo, gd, tr, evo.PokemonID, evo.FormID, 0)
			TranslateTypeNames(nameInfo, tr, nil, evoMon.Types)
			nameInfo["id"] = evo.PokemonID
			nameInfo["form"] = evo.FormID
			nameInfo["typeEmojiKeys"] = gd.GetTypeEmojiKeys(evoMon.Types)
			nameInfo["baseStats"] = map[string]int{
				"baseAttack":  evoMon.Attack,
				"baseDefense": evoMon.Defense,
				"baseStamina": evoMon.Stamina,
			}
			nameInfo["evolutionRequirement"] = gamedata.EvolutionRequirementText(tr, evo)
			evolutions = append(evolutions, nameInfo)
			walk(evoMon, depth+1)
		}
		for _, te := range m.TempEvolutions {
			types := te.Types
			if len(types) == 0 {
				types = m.Types
			}

			megaInfo := make(map[string]any)
			// Mega name: apply pattern like "Mega {0}"
			baseName := tr.T(gamedata.PokemonTranslationKey(m.PokemonID))
			pattern := "{0}"
			if p, ok := gd.Util.MegaName[te.TempEvoID]; ok {
				pattern = p
			}
			megaInfo["fullName"] = i18n.Format(pattern, baseName)
			megaInfo["evolution"] = te.TempEvoID
			megaInfo["typeEmojiKeys"] = gd.GetTypeEmojiKeys(types)
			TranslateTypeNames(megaInfo, tr, nil, types)
			megaInfo["baseStats"] = map[string]int{
				"baseAttack":  te.Attack,
				"baseDefense": te.Defense,
				"baseStamina": te.Stamina,
			}
			megaEvolutions = append(megaEvolutions, megaInfo)
		}
	}

	walk(monster, 0)
	return evolutions, megaEvolutions
}

// buildPrevEvolutions returns translated info about what pokemon evolve into this one,
// walking backward recursively (max depth 5) through the precomputed PrevEvolutions index.
func (e *Enricher) buildPrevEvolutions(gd *gamedata.GameData, tr *i18n.Translator, pokemonID int) []map[string]any {
	if gd.PrevEvolutions == nil {
		return nil
	}

	var result []map[string]any
	visited := make(map[int]bool)

	var walk func(id int, depth int)
	walk = func(id int, depth int) {
		if depth >= 5 {
			return
		}
		prevs, ok := gd.PrevEvolutions[id]
		if !ok {
			return
		}
		for _, prev := range prevs {
			if visited[prev.PokemonID] {
				continue
			}
			visited[prev.PokemonID] = true

			prevMon := gd.GetMonster(prev.PokemonID, prev.FormID)
			if prevMon == nil {
				continue
			}

			info := make(map[string]any)
			TranslateMonsterNames(info, gd, tr, prev.PokemonID, prev.FormID, 0)
			info["id"] = prev.PokemonID
			info["form"] = prev.FormID
			info["evolutionRequirement"] = gamedata.EvolutionRequirementText(tr, prev.Evolution)
			result = append(result, info)

			walk(prev.PokemonID, depth+1)
		}
	}

	walk(pokemonID, 0)
	return result
}

func computeSeenType(pokemon *webhook.PokemonWebhook) string {
	if pokemon.SeenType != "" {
		switch pokemon.SeenType {
		case "nearby_stop":
			return "pokestop"
		case "nearby_cell":
			return "cell"
		case "lure", "lure_wild":
			return "lure"
		case "lure_encounter", "encounter", "wild":
			return pokemon.SeenType
		}
		return ""
	}
	/* This is to support RDM */
	if pokemon.PokestopID == "None" && pokemon.SpawnpointID == "None" {
		return "cell"
	}
	encountered := pokemon.IndividualAttack != nil
	if pokemon.PokestopID == "None" {
		if encountered {
			return "encounter"
		}
		return "wild"
	}
	return "pokestop"
}
