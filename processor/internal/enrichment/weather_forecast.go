package enrichment

import (
	"github.com/pokemon/poracleng/processor/internal/geo"
)

// addWeatherChangeForecast sets weatherCurrent, weatherNext and
// weatherChangeTime on m when the next-hour forecast may flip the weather
// boost of a pokemon with the given types (ported from PoracleJS monster.js).
//
// boostWeather is the weather currently boosting the pokemon (0 when not
// boosted). m must already carry nextHourTimestamp, weatherForecastCurrent,
// weatherForecastNext and weatherChangeTS; nothing is set unless endUnix is
// after the next hour boundary. Per-language enrichment then derives the
// translated names/emoji from weatherNext, and the DTS view composes
// {{weatherChange}}.
func (e *Enricher) addWeatherChangeForecast(m map[string]any, types []int, boostWeather int, endUnix int64, lat, lon float64) {
	gd := e.GameData
	if gd == nil || endUnix <= 0 {
		return
	}
	nextHourTS, _ := m["nextHourTimestamp"].(int64)
	if nextHourTS <= 0 || endUnix <= nextHourTS {
		return
	}
	forecastCurrent, _ := m["weatherForecastCurrent"].(int)
	forecastNext, _ := m["weatherForecastNext"].(int)
	if forecastNext <= 0 {
		return
	}

	shouldBeBoosted := forecastCurrent > 0 && gd.IsBoostedByWeather(types, forecastCurrent)
	boostMayChange := (boostWeather > 0 && forecastNext != boostWeather) ||
		(forecastCurrent > 0 && forecastNext != forecastCurrent) ||
		(shouldBeBoosted && boostWeather == 0)
	if !boostMayChange {
		return
	}

	willBeBoosted := gd.IsBoostedByWeather(types, forecastNext)
	if (boostWeather > 0 && willBeBoosted) || (boostWeather == 0 && !willBeBoosted) {
		return
	}

	switch {
	case shouldBeBoosted && boostWeather == 0:
		m["weatherCurrent"] = 0
	case boostWeather > 0:
		m["weatherCurrent"] = boostWeather
	default:
		m["weatherCurrent"] = forecastCurrent
	}
	m["weatherNext"] = forecastNext
	// Format weatherChangeTime only when the change matters (matching JS),
	// stripping seconds — JS uses .slice(0, -3) to remove ":SS".
	if ts, ok := m["weatherChangeTS"].(int64); ok {
		formatted := geo.FormatTime(ts, geo.GetTimezone(lat, lon), e.TimeLayout)
		if len(formatted) >= 3 {
			formatted = formatted[:len(formatted)-3]
		}
		m["weatherChangeTime"] = formatted
	}
}
