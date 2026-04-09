package gamedata

import (
	"testing"
)

func TestPokemonTypesFromGameData(t *testing.T) {
	gd := loadTestGameData(t)

	pt := PokemonTypesFromGameData(gd.Monsters)

	// Test Bulbasaur types via PokemonTypes
	types := pt.GetTypes(1, 0)
	if len(types) < 2 {
		t.Fatalf("GetTypes(1, 0) = %v, want at least 2 types", types)
	}

	// Test fallback to form 0
	types = pt.GetTypes(1, 999)
	if len(types) < 2 {
		t.Fatalf("GetTypes(1, 999) fallback = %v, want at least 2 types", types)
	}

	// Test unknown pokemon
	types = pt.GetTypes(99999, 0)
	if types != nil {
		t.Errorf("GetTypes(99999, 0) = %v, want nil", types)
	}
}

func TestIsAffectedByWeatherChange(t *testing.T) {
	tests := []struct {
		name         string
		types        []int
		currentBoost bool
		newWeather   int
		want         bool
	}{
		{
			name:         "grass pokemon loses boost: clear -> rainy",
			types:        []int{12}, // grass
			currentBoost: true,         // currently boosted by clear
			newWeather:   2,         // rainy doesn't boost grass
			want:         true,
		},
		{
			name:         "grass pokemon stays boosted: clear -> clear",
			types:        []int{12}, // grass
			currentBoost: true,         // currently boosted by clear
			newWeather:   1,         // clear still boosts grass
			want:         false,
		},
		{
			name:         "water pokemon gains boost: clear -> rainy",
			types:        []int{11}, // water
			currentBoost: false,         // not boosted
			newWeather:   2,         // rainy boosts water
			want:         true,
		},
		{
			name:         "water pokemon not affected: clear -> windy",
			types:        []int{11}, // water
			currentBoost: false,         // not boosted
			newWeather:   5,         // windy doesn't boost water
			want:         false,
		},
		{
			name:         "dual type fire/flying loses fire boost: clear -> windy",
			types:        []int{10, 3}, // fire, flying
			currentBoost: true,            // boosted by clear (fire)
			newWeather:   5,            // windy boosts flying
			want:         false,        // still boosted via flying type
		},
		// === User-reported scenario: Mimikyu Ghost/Fairy ===
		{
			name:         "mimikyu (ghost/fairy) not boosted, weather -> cloudy: GAINS fairy boost",
			types:        []int{8, 18}, // ghost, fairy
			currentBoost: false,        // not boosted (was in rainy/partly cloudy)
			newWeather:   4,            // cloudy boosts fairy(18)
			want:         true,
		},
		{
			name:         "mimikyu (ghost/fairy) not boosted, weather -> rainy: no boost change",
			types:        []int{8, 18}, // ghost, fairy
			currentBoost: false,        // not boosted
			newWeather:   2,            // rainy doesn't boost ghost or fairy
			want:         false,
		},
		{
			name:         "mimikyu (ghost/fairy) not boosted, weather -> fog: GAINS ghost boost",
			types:        []int{8, 18}, // ghost, fairy
			currentBoost: false,
			newWeather:   7, // fog boosts ghost(8)
			want:         true,
		},
		{
			name:         "mimikyu (ghost/fairy) boosted by cloudy, weather -> rainy: LOSES fairy boost",
			types:        []int{8, 18},
			currentBoost: true, // was boosted (e.g. cloudy boosted fairy)
			newWeather:   2,    // rainy doesn't boost ghost or fairy
			want:         true,
		},
		{
			name:         "mimikyu (ghost/fairy) boosted by fog, weather -> cloudy: still boosted via fairy",
			types:        []int{8, 18},
			currentBoost: true, // was boosted (fog boosted ghost)
			newWeather:   4,    // cloudy boosts fairy — still boosted
			want:         false,
		},
		// === Gains and losses for single-type pokemon ===
		{
			name:         "pikachu (electric) not boosted, weather -> rainy: GAINS boost",
			types:        []int{13}, // electric
			currentBoost: false,
			newWeather:   2, // rainy boosts electric
			want:         true,
		},
		{
			name:         "pikachu (electric) boosted by rainy, weather -> clear: LOSES boost",
			types:        []int{13},
			currentBoost: true,
			newWeather:   1, // clear doesn't boost electric
			want:         true,
		},
		{
			name:         "pikachu (electric) boosted by rainy, weather stays rainy: no change",
			types:        []int{13},
			currentBoost: true,
			newWeather:   2, // rainy still boosts electric
			want:         false,
		},
		// === Dual type where one type stays boosted ===
		{
			name:         "charizard (fire/flying) boosted by clear, weather -> windy: still boosted via flying",
			types:        []int{10, 3}, // fire, flying
			currentBoost: true,         // boosted by clear (fire)
			newWeather:   5,            // windy boosts flying
			want:         false,        // still boosted
		},
		{
			name:         "charizard (fire/flying) boosted by clear, weather -> rainy: LOSES all boost",
			types:        []int{10, 3},
			currentBoost: true,
			newWeather:   2, // rainy doesn't boost fire or flying
			want:         true,
		},
		// === Edge cases ===
		{
			name:         "empty types",
			types:        nil,
			currentBoost: false,
			newWeather:   1,
			want:         false,
		},
		{
			name:         "invalid weather",
			types:        []int{12},
			currentBoost: false,
			newWeather:   99,
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsAffectedByWeatherChange(tt.types, tt.currentBoost, tt.newWeather)
			if got != tt.want {
				t.Errorf("IsAffectedByWeatherChange(%v, %v, %d) = %v, want %v",
					tt.types, tt.currentBoost, tt.newWeather, got, tt.want)
			}
		})
	}
}
