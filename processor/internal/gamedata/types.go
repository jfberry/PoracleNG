package gamedata

import (
	"fmt"
	"slices"
)

// WeatherTypeBoost maps weather condition ID to the type IDs it boosts.
// This is the hardcoded fallback; prefer GameData.Weather or UtilData.WeatherTypeBoost
// when GameData is loaded.
var WeatherTypeBoost = map[int][]int{
	1: {5, 10, 12}, // clear: fire, ground, grass
	2: {7, 11, 13}, // rainy: water, electric, bug
	3: {1, 6},      // partly cloudy: normal, rock
	4: {2, 4, 18},  // cloudy: fairy, poison, fighting
	5: {3, 14, 16}, // windy: flying, dragon, psychic
	6: {9, 15},     // snow: ice, steel
	7: {8, 17},     // fog: ghost, dark
}

// PokemonTypes holds the type IDs for each pokemon_form combination.
// Used by the weather change tracker for quick lookups.
type PokemonTypes struct {
	types map[string][]int // "pokemonID_form" -> type IDs
}

// PokemonTypesFromGameData creates a PokemonTypes from already-loaded GameData monsters.
func PokemonTypesFromGameData(monsters map[MonsterKey]*Monster) *PokemonTypes {
	pt := &PokemonTypes{
		types: make(map[string][]int, len(monsters)),
	}
	for key, m := range monsters {
		skey := fmt.Sprintf("%d_%d", key.ID, key.Form)
		pt.types[skey] = m.Types
	}
	return pt
}

// GetTypes returns the type IDs for a pokemon/form, falling back to form 0.
func (pt *PokemonTypes) GetTypes(pokemonID, form int) []int {
	key := fmt.Sprintf("%d_%d", pokemonID, form)
	if types, ok := pt.types[key]; ok {
		return types
	}
	key = fmt.Sprintf("%d_0", pokemonID)
	return pt.types[key]
}

// IsAffectedByWeatherChange returns true if a pokemon with the given types
// gains or loses weather boost due to the weather changing from oldWeather to newWeather.
// IsAffectedByWeatherChange checks if a pokemon's boost status would change
// with the new weather. Matches the alerter's getAlteringWeathers logic:
// - If currently boosted: affected if new weather does NOT boost its types (loses boost)
// - If not boosted: affected if new weather DOES boost its types (gains boost)
func IsAffectedByWeatherChange(types []int, currentlyBoosted bool, newWeather int) bool {
	if len(types) == 0 {
		return false
	}

	newBoosts := false
	if boosted, ok := WeatherTypeBoost[newWeather]; ok {
		for _, t := range types {
			if slices.Contains(boosted, t) {
				newBoosts = true
				break
			}
		}
	}

	if currentlyBoosted {
		return !newBoosts // was boosted, affected if would lose boost
	}
	return newBoosts // wasn't boosted, affected if would gain boost
}

// IsBoostedByWeather checks if a pokemon with the given types is boosted by the given weather.
func IsBoostedByWeather(types []int, weather int) bool {
	boosted, ok := WeatherTypeBoost[weather]
	if !ok {
		return false
	}
	for _, t := range types {
		if slices.Contains(boosted, t) {
			return true
		}
	}
	return false
}
