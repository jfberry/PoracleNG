package gamedata

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadPokemonTypes(t *testing.T) {
	// Create a minimal monsters.json for testing
	content := `{
		"1_0": {
			"types": [{"id": 4, "name": "Poison"}, {"id": 12, "name": "Grass"}]
		},
		"1_163": {
			"types": [{"id": 4, "name": "Poison"}, {"id": 12, "name": "Grass"}]
		},
		"6_0": {
			"types": [{"id": 10, "name": "Fire"}, {"id": 3, "name": "Flying"}]
		}
	}`
	dir := t.TempDir()
	path := filepath.Join(dir, "monsters.json")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	pt, err := LoadPokemonTypes(path)
	if err != nil {
		t.Fatalf("LoadPokemonTypes failed: %v", err)
	}

	// Test exact form match
	types := pt.GetTypes(1, 163)
	if len(types) != 2 || types[0] != 4 || types[1] != 12 {
		t.Errorf("GetTypes(1, 163) = %v, want [4, 12]", types)
	}

	// Test fallback to form 0
	types = pt.GetTypes(1, 999)
	if len(types) != 2 || types[0] != 4 || types[1] != 12 {
		t.Errorf("GetTypes(1, 999) fallback = %v, want [4, 12]", types)
	}

	// Test Charizard types
	types = pt.GetTypes(6, 0)
	if len(types) != 2 || types[0] != 10 || types[1] != 3 {
		t.Errorf("GetTypes(6, 0) = %v, want [10, 3]", types)
	}

	// Test unknown pokemon
	types = pt.GetTypes(9999, 0)
	if types != nil {
		t.Errorf("GetTypes(9999, 0) = %v, want nil", types)
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
			types:        []int{7}, // water
			currentBoost: 0,        // not boosted
			newWeather:   2,        // rainy boosts water
			want:         true,
		},
		{
			name:         "water pokemon not affected: clear -> windy",
			types:        []int{7}, // water
			currentBoost: 0,        // not boosted
			newWeather:   5,        // windy doesn't boost water
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
