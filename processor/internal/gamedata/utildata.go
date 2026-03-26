package gamedata

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// UtilData holds static game constants loaded from util.json.
type UtilData struct {
	Genders          map[int]GenderInfo
	Rarity           map[int]string       // rarityGroup → English name
	Size             map[int]string       // sizeGroup → English name
	MegaName         map[int]string       // tempEvoId → format pattern e.g. "Mega {0}"
	Evolution        map[int]EvolutionInfo
	Teams            map[int]TeamInfo
	Types            map[string]TypeDisplay // English name → {ID, Emoji, Color}
	Weather          map[int]WeatherInfo
	WeatherTypeBoost map[int][]int
	GenData          map[int]GenInfo
	GenException     map[MonsterKey]int // {pokemonID, form} → generation
	RaidLevels       map[int]string // level → English name
	MaxbattleLevels  map[int]string // level → English name
	Lures            map[int]LureInfo
	PokestopEvent    map[int]EventInfo
	Emojis           map[string]string // emoji key → unicode
}

// GenderInfo holds gender display data.
type GenderInfo struct {
	Name  string `json:"name"`
	Emoji string `json:"emoji"` // emoji key (e.g. "gender-male")
}

// EvolutionInfo holds evolution type display data.
type EvolutionInfo struct {
	Name string `json:"name"`
}

// TeamInfo holds team display data.
type TeamInfo struct {
	Name  string `json:"name"`
	Color string `json:"color"`
	Emoji string `json:"emoji"` // emoji key (e.g. "team-mystic")
}

// TypeDisplay holds type display data from util.json types section.
type TypeDisplay struct {
	ID    int    `json:"id"`
	Emoji string `json:"emoji"` // emoji key (e.g. "type-grass")
	Color string `json:"color"` // hex color
}

// WeatherInfo holds weather display data.
type WeatherInfo struct {
	Name  string `json:"name"`
	Emoji string `json:"emoji"` // emoji key (e.g. "weather-sunny")
}

// GenInfo holds generation range data.
type GenInfo struct {
	Min   int    `json:"min"`
	Max   int    `json:"max"`
	Name  string `json:"name"`  // English name (e.g. "Kanto")
	Roman string `json:"roman"` // Roman numeral (e.g. "I")
}

// LureInfo holds lure display data.
type LureInfo struct {
	Name  string `json:"name"`
	Emoji string `json:"emoji"` // emoji key
	Color string `json:"color"` // hex color
}

// EventInfo holds pokestop event display data.
type EventInfo struct {
	Name  string `json:"name"`
	Color string `json:"color"`
	Emoji string `json:"emoji"` // emoji key
}

// LoadUtilData parses util.json.
func LoadUtilData(path string) (*UtilData, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	// Parse into raw JSON map first since keys are sometimes strings of ints
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	u := &UtilData{}

	// Genders: {"0": {...}, "1": {...}}
	u.Genders = parseIntKeyMap[GenderInfo](raw["genders"])

	// Rarity: {"1": "Common", ...}
	u.Rarity = parseIntStringMap(raw["rarity"])

	// Size: {"1": "XXS", ...}
	u.Size = parseIntStringMap(raw["size"])

	// MegaName: {"0": "{0}", "1": "Mega {0}", ...}
	u.MegaName = parseIntStringMap(raw["megaName"])

	// Evolution: {"0": {"name": ""}, ...}
	u.Evolution = parseIntKeyMap[EvolutionInfo](raw["evolution"])

	// Teams: {"0": {...}, ...}
	u.Teams = parseIntKeyMap[TeamInfo](raw["teams"])

	// Types: {"Grass": {...}, ...}
	if raw["types"] != nil {
		var types map[string]TypeDisplay
		if err := json.Unmarshal(raw["types"], &types); err == nil {
			u.Types = types
		}
	}

	// Weather: {"0": {...}, ...}
	u.Weather = parseIntKeyMap[WeatherInfo](raw["weather"])

	// WeatherTypeBoost: {"1": [5,10,12], ...}
	u.WeatherTypeBoost = parseIntKeyMap[[]int](raw["weatherTypeBoost"])

	// GenData: {"1": {...}, ...}
	u.GenData = parseIntKeyMap[GenInfo](raw["genData"])

	// GenException: {"19_46": "7", ...} — values are string ints, keys are "pokemonID_form"
	if raw["genException"] != nil {
		var genExcRaw map[string]string
		if err := json.Unmarshal(raw["genException"], &genExcRaw); err == nil {
			u.GenException = make(map[MonsterKey]int, len(genExcRaw))
			for k, v := range genExcRaw {
				gen, err := strconv.Atoi(v)
				if err != nil {
					continue
				}
				parts := strings.SplitN(k, "_", 2)
				if len(parts) != 2 {
					continue
				}
				pokemonID, err1 := strconv.Atoi(parts[0])
				formID, err2 := strconv.Atoi(parts[1])
				if err1 != nil || err2 != nil {
					continue
				}
				u.GenException[MonsterKey{pokemonID, formID}] = gen
			}
		}
	}

	// RaidLevels: {"1": "Level 1", ...}
	u.RaidLevels = parseIntStringMap(raw["raidLevels"])

	// MaxbattleLevels: {"1": "1 Star Max Battle", ...}
	u.MaxbattleLevels = parseIntStringMap(raw["maxbattleLevels"])

	// Lures: {"501": {...}, ...}
	u.Lures = parseIntKeyMap[LureInfo](raw["lures"])

	// PokestopEvent: {"7": {...}, ...}
	u.PokestopEvent = parseIntKeyMap[EventInfo](raw["pokestopEvent"])

	// Emojis: {"lure-normal": "📍", ...}
	if raw["emojis"] != nil {
		var emojis map[string]string
		if err := json.Unmarshal(raw["emojis"], &emojis); err == nil {
			u.Emojis = emojis
		}
	}

	return u, nil
}

// parseIntKeyMap parses a JSON object with string-int keys into a map[int]T.
func parseIntKeyMap[T any](data json.RawMessage) map[int]T {
	if data == nil {
		return nil
	}
	var raw map[string]T
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	result := make(map[int]T, len(raw))
	for k, v := range raw {
		if id, err := strconv.Atoi(k); err == nil {
			result[id] = v
		}
	}
	return result
}

// parseIntStringMap parses {"1": "value", ...} into map[int]string.
func parseIntStringMap(data json.RawMessage) map[int]string {
	if data == nil {
		return nil
	}
	var raw map[string]string
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	result := make(map[int]string, len(raw))
	for k, v := range raw {
		if id, err := strconv.Atoi(k); err == nil {
			result[id] = v
		}
	}
	return result
}
