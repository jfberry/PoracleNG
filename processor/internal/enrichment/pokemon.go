package enrichment

import (
	"github.com/pokemon/poracleng/processor/internal/geo"
	"github.com/pokemon/poracleng/processor/internal/matching"
	"github.com/pokemon/poracleng/processor/internal/tracker"
	"github.com/pokemon/poracleng/processor/internal/webhook"
)

// Pokemon builds enrichment fields for a pokemon webhook.
func (e *Enricher) Pokemon(pokemon *webhook.PokemonWebhook, processed *matching.ProcessedPokemon) map[string]interface{} {
	m := map[string]interface{}{
		"iv":              processed.IV,
		"atk":             processed.ATK,
		"def":             processed.DEF,
		"sta":             processed.STA,
		"cp":              processed.CP,
		"level":           processed.Level,
		"tthSeconds":      processed.TTHSeconds,
		"encountered":     processed.Encountered,
		"rarityGroup":     processed.RarityGroup,
		"pvpBestRank":     processed.PVPBestRank,
		"pvpEvolutionData": processed.PVPEvoData,
	}

	// Cell weather
	cellID := tracker.GetWeatherCellID(pokemon.Latitude, pokemon.Longitude)
	m["gameWeatherId"] = e.WeatherProvider.GetCurrentWeatherInCell(cellID)

	// Time enrichment
	if pokemon.DisappearTime > 0 {
		tz := geo.GetTimezone(pokemon.Latitude, pokemon.Longitude)
		m["disappearTime"] = geo.FormatTime(pokemon.DisappearTime, tz, e.TimeLayout)
		m["tth"] = geo.ComputeTTH(pokemon.DisappearTime)

		// Weather change time: the hour boundary before disappear_time
		weatherChangeTS := pokemon.DisappearTime - (pokemon.DisappearTime % 3600)
		m["weatherChangeTime"] = geo.FormatTime(weatherChangeTS, tz, e.TimeLayout)

		addSunTimes(m, pokemon.Latitude, pokemon.Longitude, tz)
	}

	// S2 cell coords for cell-spawned pokemon
	isCellSpawn := pokemon.SeenType == "nearby_cell" ||
		(pokemon.PokestopID == "None" && pokemon.SpawnpointID == "None")
	if isCellSpawn {
		m["cell_coords"] = geo.GetCellCoords(pokemon.Latitude, pokemon.Longitude, 15)
	}

	return m
}
