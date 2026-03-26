package gamedata

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
)

// rawWeather is the raw masterfile format for weather.json.
type rawWeather struct {
	WeatherID   int    `json:"weatherId"`
	WeatherName string `json:"weatherName"`
	Types       []int  `json:"types"` // boosted type IDs
}

// loadWeather parses the raw masterfile weather.json file.
func loadWeather(path string) (map[int]*WeatherData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var raw map[string]rawWeather
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	weather := make(map[int]*WeatherData, len(raw))
	for key, entry := range raw {
		id, err := strconv.Atoi(key)
		if err != nil {
			continue
		}
		weather[id] = &WeatherData{
			WeatherID: entry.WeatherID,
			Types:     entry.Types,
		}
	}

	return weather, nil
}
