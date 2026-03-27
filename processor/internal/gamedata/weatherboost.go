package gamedata

import "slices"

// GetWeatherBoostTypes returns the type IDs boosted by a weather ID.
// Uses the raw masterfile weather data first, falling back to util.json weatherTypeBoost.
func (gd *GameData) GetWeatherBoostTypes(weatherID int) []int {
	if w, ok := gd.Weather[weatherID]; ok && len(w.Types) > 0 {
		return w.Types
	}
	if gd.Util != nil {
		if boost, ok := gd.Util.WeatherTypeBoost[weatherID]; ok {
			return boost
		}
	}
	return WeatherTypeBoost[weatherID]
}

// GetBoostingWeathers returns the weather IDs that boost any of the given type IDs.
func (gd *GameData) GetBoostingWeathers(typeIDs []int) []int {
	var weathers []int
	for weatherID := 1; weatherID <= 7; weatherID++ {
		boostedTypes := gd.GetWeatherBoostTypes(weatherID)
		for _, t := range typeIDs {
			if slices.Contains(boostedTypes, t) {
				weathers = append(weathers, weatherID)
				break
			}
		}
	}
	return weathers
}

// GetAlteringWeathers returns weather IDs that would change the boost status
// of a pokemon with the given types. If boostStatus > 0 (currently boosted),
// returns non-boosting weathers. Otherwise returns boosting weathers.
func (gd *GameData) GetAlteringWeathers(typeIDs []int, boostStatus int) []int {
	boosting := gd.GetBoostingWeathers(typeIDs)
	if boostStatus > 0 {
		allWeathers := []int{1, 2, 3, 4, 5, 6, 7}
		var result []int
		for _, w := range allWeathers {
			if !slices.Contains(boosting, w) {
				result = append(result, w)
			}
		}
		return result
	}
	return boosting
}

// IsBoostedByWeather returns true if any of the given type IDs are boosted
// by the given weather ID.
func (gd *GameData) IsBoostedByWeather(typeIDs []int, weatherID int) bool {
	if weatherID == 0 {
		return false
	}
	boostedTypes := gd.GetWeatherBoostTypes(weatherID)
	for _, t := range typeIDs {
		if slices.Contains(boostedTypes, t) {
			return true
		}
	}
	return false
}

// FindIvColor returns the IV color hex string based on Discord IV color ranges.
// The colors array is [gray, white, green, blue, purple, orange] from config.
func FindIvColor(iv float64, ivColors []string) string {
	if len(ivColors) < 6 {
		return ""
	}
	switch {
	case iv < 25:
		return ivColors[0]
	case iv < 50:
		return ivColors[1]
	case iv < 82:
		return ivColors[2]
	case iv < 90:
		return ivColors[3]
	case iv < 100:
		return ivColors[4]
	default:
		return ivColors[5]
	}
}
