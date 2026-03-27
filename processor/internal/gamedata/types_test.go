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
		currentBoost int
		newWeather   int
		want         bool
	}{
		{
			name:         "grass pokemon loses boost: clear -> rainy",
			types:        []int{12}, // grass
			currentBoost: 1,         // currently boosted by clear
			newWeather:   2,         // rainy doesn't boost grass
			want:         true,
		},
		{
			name:         "grass pokemon stays boosted: clear -> clear",
			types:        []int{12}, // grass
			currentBoost: 1,         // currently boosted by clear
			newWeather:   1,         // clear still boosts grass
			want:         false,
		},
		{
			name:         "water pokemon gains boost: clear -> rainy",
			types:        []int{11}, // water
			currentBoost: 0,         // not boosted
			newWeather:   2,         // rainy boosts water
			want:         true,
		},
		{
			name:         "water pokemon not affected: clear -> windy",
			types:        []int{11}, // water
			currentBoost: 0,         // not boosted
			newWeather:   5,         // windy doesn't boost water
			want:         false,
		},
		{
			name:         "dual type fire/flying loses fire boost: clear -> windy",
			types:        []int{10, 3}, // fire, flying
			currentBoost: 1,            // boosted by clear (fire)
			newWeather:   5,            // windy boosts flying
			want:         false,        // still boosted via flying type
		},
		{
			name:         "empty types",
			types:        nil,
			currentBoost: 0,
			newWeather:   1,
			want:         false,
		},
		{
			name:         "invalid weather",
			types:        []int{12},
			currentBoost: 0,
			newWeather:   99,
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsAffectedByWeatherChange(tt.types, tt.currentBoost, tt.newWeather)
			if got != tt.want {
				t.Errorf("IsAffectedByWeatherChange(%v, %d, %d) = %v, want %v",
					tt.types, tt.currentBoost, tt.newWeather, got, tt.want)
			}
		})
	}
}
