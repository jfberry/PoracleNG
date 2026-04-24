package gamedata

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// UtilData holds static game constants loaded from util.json.
//
// Display strings (team names, weather names, lure names, rarity labels,
// generation names, raid/max-battle level titles, etc.) are NOT loaded from
// util.json anymore — those come from pogo-translations / embedded i18n
// identifier keys (team_N, weather_N, lure_N, rarity_N, generation_N,
// raid_N, max_battle_N, display_type_N, gender_N, evo_N, poke_{id}_e{N}).
// util.json is the authoritative source for non-translatable metadata only:
// emoji keys, hex colours, level-cost tables, type IDs, weather boosts, etc.
type UtilData struct {
	Genders          map[int]GenderInfo
	Rarity           map[int]struct{} // valid rarity IDs (display via embedded rarity_N)
	Size             map[int]struct{} // valid size IDs (display via embedded size_N)
	MegaName         map[int]string   // tempEvoId → format pattern e.g. "Mega {0}" (fallback when poke_{id}_e{N} is absent)
	Evolution        map[int]struct{} // valid temp-evolution IDs (display via evo_N)
	Teams            map[int]TeamInfo
	Types            map[string]TypeDisplay // English name → {ID, Emoji, Color}
	Weather          map[int]WeatherInfo
	WeatherTypeBoost map[int][]int
	GenData          map[int]GenInfo
	GenException     map[MonsterKey]int // {pokemonID, form} → generation
	RaidLevels       map[int]struct{}   // valid raid level IDs (display via raid_N)
	MaxbattleLevels  map[int]struct{}   // valid max-battle level IDs (display via max_battle_N)
	Lures            map[int]LureInfo
	PokestopEvent    map[int]EventInfo
	PowerUpCost      map[string]PowerUpCostEntry // level string → {stardust, candy, xlCandy}
	CpMultipliers    map[string]float64          // level string → CP multiplier
	Emojis           map[string]string           // emoji key → unicode
}

// PowerUpCostEntry holds the cost to power up one half-level.
type PowerUpCostEntry struct {
	Stardust int `json:"stardust"`
	Candy    int `json:"candy"`
	XLCandy  int `json:"xlCandy"`
}

// GenderInfo holds gender display metadata. The display name comes from
// embedded i18n gender_N, not util.json.
type GenderInfo struct {
	Emoji string `json:"emoji"` // emoji key (e.g. "gender-male")
}

// TeamInfo holds team display metadata. The display name comes from
// pogo-translations team_N, not util.json.
type TeamInfo struct {
	Color string `json:"color"`
	Emoji string `json:"emoji"` // emoji key (e.g. "team-mystic")
}

// TypeDisplay holds type display data from util.json types section.
type TypeDisplay struct {
	ID    int    `json:"id"`
	Emoji string `json:"emoji"` // emoji key (e.g. "type-grass")
	Color string `json:"color"` // hex color
}

// WeatherInfo holds weather display metadata. The display name comes from
// pogo-translations weather_N, not util.json.
type WeatherInfo struct {
	Emoji string `json:"emoji"` // emoji key (e.g. "weather-sunny")
}

// GenInfo holds generation range data. The display name comes from
// pogo-translations generation_N; Min/Max/Roman remain in util.json.
type GenInfo struct {
	Min   int    `json:"min"`
	Max   int    `json:"max"`
	Roman string `json:"roman"` // Roman numeral (e.g. "I")
}

// LureInfo holds lure display metadata. The display name comes from
// pogo-translations lure_N, not util.json.
type LureInfo struct {
	Emoji string `json:"emoji"` // emoji key
	Color string `json:"color"` // hex color
}

// EventInfo holds pokestop event display metadata.
//
// Name is retained because it's the canonical English identifier stored in
// the invasion tracking DB (see matching/invasion.go ResolveGruntTypeName
// and bot/commands/invasion.go). Display text for templates comes from
// pogo-translations display_type_N.
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
	return ParseUtilData(data)
}

// ParseUtilData parses util.json from raw bytes (for embedded data).
func ParseUtilData(data []byte) (*UtilData, error) {
	// Parse into raw JSON map first since keys are sometimes strings of ints
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parse util.json: %w", err)
	}

	u := &UtilData{}

	// Genders: {"0": {...}, "1": {...}}
	u.Genders = parseIntKeyMap[GenderInfo](raw["genders"])

	// Rarity: keep IDs only; display strings come from embedded i18n rarity_N
	u.Rarity = parseIntKeySet(raw["rarity"])

	// Size: keep IDs only; display strings come from embedded i18n size_N
	u.Size = parseIntKeySet(raw["size"])

	// MegaName: {"0": "{0}", "1": "Mega {0}", ...} — fallback format pattern
	u.MegaName = parseIntStringMap(raw["megaName"])

	// Evolution: keep IDs only; display strings come from pogo-translations evo_N
	u.Evolution = parseIntKeySet(raw["evolution"])

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

	// RaidLevels: keep IDs only; display strings come from pogo-translations raid_N
	u.RaidLevels = parseIntKeySet(raw["raidLevels"])

	// MaxbattleLevels: keep IDs only; display strings come from pogo-translations max_battle_N
	u.MaxbattleLevels = parseIntKeySet(raw["maxbattleLevels"])

	// Lures: {"501": {...}, ...}
	u.Lures = parseIntKeyMap[LureInfo](raw["lures"])

	// PokestopEvent: {"7": {...}, ...}
	u.PokestopEvent = parseIntKeyMap[EventInfo](raw["pokestopEvent"])

	// CpMultipliers: {"1": 0.094, "1.5": 0.135, ...}
	if raw["cpMultipliers"] != nil {
		var cpm map[string]float64
		if err := json.Unmarshal(raw["cpMultipliers"], &cpm); err == nil {
			u.CpMultipliers = cpm
		}
	}

	// PowerUpCost: {"1": {"stardust": 200, "candy": 1}, "1.5": {...}, ...}
	if raw["powerUpCost"] != nil {
		var puc map[string]PowerUpCostEntry
		if err := json.Unmarshal(raw["powerUpCost"], &puc); err == nil {
			u.PowerUpCost = puc
		}
	}

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

// parseIntKeySet parses a JSON object with string-int keys into a set
// (map[int]struct{}), ignoring whatever the values are. Used for util.json
// maps whose English string values have been superseded by translator keys —
// the set membership still matters (e.g. "is this a valid raid level?") but
// the English label is no longer needed.
func parseIntKeySet(data json.RawMessage) map[int]struct{} {
	if data == nil {
		return nil
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	result := make(map[int]struct{}, len(raw))
	for k := range raw {
		if id, err := strconv.Atoi(k); err == nil {
			result[id] = struct{}{}
		}
	}
	return result
}
