package enrichment

import (
	"time"

	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// Pokemon builds enrichment fields for a pokemon webhook.
func (e *Enricher) Pokemon(pokemon *webhook.PokemonWebhook, processed *matching.ProcessedPokemon) map[string]interface{} {
	m := map[string]interface{}{
		"rarityGroup":      processed.RarityGroup,
		"pvpBestRank":      processed.PVPBestRank,
		"pvpEvolutionData": processed.PVPEvoData,
	}

	// Shiny rate and shiny possible
	if e.ShinyProvider != nil {
		rate := e.ShinyProvider.GetShinyRate(pokemon.PokemonID)
		if rate > 0 {
			m["shinyStats"] = rate
			m["shinyPossible"] = true
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
		m["disappearTime"] = geo.FormatTime(pokemon.DisappearTime, tz, e.TimeLayout)
		m["tth"] = geo.ComputeTTH(pokemon.DisappearTime)

		// Weather change time: the hour boundary before disappear_time
		weatherChangeTS := pokemon.DisappearTime - (pokemon.DisappearTime % 3600)
		m["weatherChangeTime"] = geo.FormatTime(weatherChangeTS, tz, e.TimeLayout)

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

	return m
}
