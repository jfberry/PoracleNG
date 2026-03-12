package gamedata

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// WeatherTypeBoost maps weather condition ID to the type IDs it boosts.
// Source: alerter/src/util/util.json weatherTypeBoost
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
type PokemonTypes struct {
	types map[string][]int // "pokemonID_form" -> type IDs
}

// monstersEntry is used to parse the relevant fields from monsters.json.
type monstersEntry struct {
	Types []struct {
		ID int `json:"id"`
	} `json:"types"`
}

// LoadPokemonTypes parses monsters.json and extracts type IDs per pokemon/form.
func LoadPokemonTypes(path string) (*PokemonTypes, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read monsters.json: %w", err)
	}

	var raw map[string]monstersEntry
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse monsters.json: %w", err)
	}

	pt := &PokemonTypes{
		types: make(map[string][]int, len(raw)),
	}

	for key, entry := range raw {
		// Validate key format "pokemonID_form"
		parts := strings.SplitN(key, "_", 2)
		if len(parts) != 2 {
			continue
		}
		if _, err := strconv.Atoi(parts[0]); err != nil {
			continue
		}

		typeIDs := make([]int, len(entry.Types))
		for i, t := range entry.Types {
			typeIDs[i] = t.ID
		}
		pt.types[key] = typeIDs
	}

	return pt, nil
}

// GetTypes returns the type IDs for a pokemon/form, falling back to form 0.
func (pt *PokemonTypes) GetTypes(pokemonID, form int) []int {
	key := fmt.Sprintf("%d_%d", pokemonID, form)
	if types, ok := pt.types[key]; ok {
		return types
	}
	// Fallback to form 0
	key = fmt.Sprintf("%d_0", pokemonID)
	return pt.types[key]
}

// IsAffectedByWeatherChange returns true if a pokemon with the given types
// gains or loses weather boost due to the weather changing from oldWeather to newWeather.
func IsAffectedByWeatherChange(types []int, currentBoost int, newWeather int) bool {
	if len(types) == 0 {
		return false
	}

	// Check if new weather boosts any of this pokemon's types
	newBoosts := false
	if boosted, ok := WeatherTypeBoost[newWeather]; ok {
		for _, t := range types {
			for _, b := range boosted {
				if t == b {
					newBoosts = true
					break
				}
			}
			if newBoosts {
				break
			}
		}
	}

	if currentBoost > 0 {
		// Currently boosted: affected if new weather does NOT boost it (losing boost)
		return !newBoosts
	}
	// Not boosted: affected if new weather DOES boost it (gaining boost)
	return newBoosts
}
