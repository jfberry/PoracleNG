// Package gamedata loads the Pokemon game master data needed for enrichment.
//
// Data is loaded from two sources:
//   - Raw masterfile (resources/rawdata/) — pokemon, forms, moves, types, items,
//     invasions, weather. All IDs are numeric. Names come from pogo-translations.
//   - Utility JSON (resources/data/util.json) — static game constants like
//     generation ranges, weather boosts, team info, emoji keys, etc.
//
// Translation keys follow pogo-translations conventions:
//   - Pokemon:  poke_{id}
//   - Move:     move_{id}
//   - Type:     poke_type_{id}
//   - Form:     form_{id}
//   - Item:     item_{id}
//   - Weather:  weather_{id}
//   - Grunt:    grunt_{id}
package gamedata

import (
	_ "embed"
	"fmt"
	"path/filepath"
)

//go:embed util.json
var embeddedUtilJSON []byte

// MonsterKey uniquely identifies a pokemon by species ID and form ID.
type MonsterKey struct {
	ID   int
	Form int
}

// GameData holds all loaded game master data for enrichment.
type GameData struct {
	Monsters map[MonsterKey]*Monster // {ID, Form} → Monster
	Moves    map[int]*Move           // moveId → Move
	Types    map[int]*TypeInfo       // typeId → TypeInfo
	Items    map[int]*Item           // itemId → Item
	Grunts   map[int]*Grunt          // gruntTypeId → Grunt
	Weather  map[int]*WeatherData    // weatherId → WeatherData (boosted types)
	Util     *UtilData               // static game constants
}

// WeatherData holds weather boost information from the raw masterfile.
type WeatherData struct {
	WeatherID int
	Types     []int // boosted type IDs
}

// Load reads all game data from the resources directory.
func Load(baseDir string) (*GameData, error) {
	rawDir := filepath.Join(baseDir, "resources", "rawdata")

	monsters, err := LoadMonsters(
		filepath.Join(rawDir, "pokemon.json"),
		filepath.Join(rawDir, "forms.json"),
	)
	if err != nil {
		return nil, fmt.Errorf("loading monsters: %w", err)
	}

	moves, err := LoadMoves(filepath.Join(rawDir, "moves.json"))
	if err != nil {
		return nil, fmt.Errorf("loading moves: %w", err)
	}

	types, err := LoadTypes(filepath.Join(rawDir, "types.json"))
	if err != nil {
		return nil, fmt.Errorf("loading types: %w", err)
	}

	items, err := LoadItems(filepath.Join(rawDir, "items.json"))
	if err != nil {
		return nil, fmt.Errorf("loading items: %w", err)
	}

	grunts, err := LoadGrunts(filepath.Join(rawDir, "invasions.json"))
	if err != nil {
		return nil, fmt.Errorf("loading grunts: %w", err)
	}

	weather, err := loadWeather(filepath.Join(rawDir, "weather.json"))
	if err != nil {
		return nil, fmt.Errorf("loading weather: %w", err)
	}

	// util.json is embedded in the binary — no filesystem dependency
	util, err := ParseUtilData(embeddedUtilJSON)
	if err != nil {
		return nil, fmt.Errorf("loading embedded util.json: %w", err)
	}

	// Enrich TypeInfo with display data from util.json
	for _, td := range util.Types {
		if ti, ok := types[td.ID]; ok {
			ti.Color = td.Color
			ti.Emoji = td.Emoji
		}
	}

	return &GameData{
		Monsters: monsters,
		Moves:    moves,
		Types:    types,
		Items:    items,
		Grunts:   grunts,
		Weather:  weather,
		Util:     util,
	}, nil
}

// GetMonster looks up a monster by pokemon ID and form, falling back to form 0.
func (gd *GameData) GetMonster(pokemonID, form int) *Monster {
	if m, ok := gd.Monsters[MonsterKey{pokemonID, form}]; ok {
		return m
	}
	return gd.Monsters[MonsterKey{pokemonID, 0}]
}

// GetMove looks up a move by ID.
func (gd *GameData) GetMove(moveID int) *Move {
	return gd.Moves[moveID]
}

// GetItem looks up an item by ID.
func (gd *GameData) GetItem(itemID int) *Item {
	return gd.Items[itemID]
}

// GetGrunt looks up a grunt by type ID.
func (gd *GameData) GetGrunt(gruntTypeID int) *Grunt {
	return gd.Grunts[gruntTypeID]
}

// GetGeneration returns the generation number for a pokemon ID and form.
// Uses the genId from the raw masterfile, with genException overrides from util.json.
func (gd *GameData) GetGeneration(pokemonID, form int) int {
	// Check exceptions first (regional forms that belong to a different gen)
	if gen, ok := gd.Util.GenException[MonsterKey{pokemonID, form}]; ok {
		return gen
	}
	// Use genId from the monster data
	m := gd.GetMonster(pokemonID, form)
	if m != nil && m.GenID > 0 {
		return m.GenID
	}
	// Fallback to genData ranges
	for gen, data := range gd.Util.GenData {
		if pokemonID >= data.Min && pokemonID <= data.Max {
			return gen
		}
	}
	return 0
}

// GetGenerationInfo returns the GenInfo for a generation number.
func (gd *GameData) GetGenerationInfo(gen int) *GenInfo {
	if info, ok := gd.Util.GenData[gen]; ok {
		return &info
	}
	return nil
}

// GetTypeColor returns the color hex string for the first type ID in a list.
func (gd *GameData) GetTypeColor(typeIDs []int) string {
	if len(typeIDs) == 0 {
		return ""
	}
	if ti, ok := gd.Types[typeIDs[0]]; ok {
		return ti.Color
	}
	return ""
}

// GetTypeEmojiKeys returns the emoji keys for a list of type IDs.
func (gd *GameData) GetTypeEmojiKeys(typeIDs []int) []string {
	keys := make([]string, 0, len(typeIDs))
	for _, id := range typeIDs {
		if ti, ok := gd.Types[id]; ok {
			keys = append(keys, ti.Emoji)
		}
	}
	return keys
}

// GetWeatherEmojiKeys returns the emoji keys for a list of weather IDs.
func (gd *GameData) GetWeatherEmojiKeys(weatherIDs []int) []string {
	if gd.Util == nil {
		return nil
	}
	keys := make([]string, 0, len(weatherIDs))
	for _, id := range weatherIDs {
		if wInfo, ok := gd.Util.Weather[id]; ok {
			keys = append(keys, wInfo.Emoji)
		}
	}
	return keys
}

// TranslationKey helpers for pogo-translations identifier keys.

// PokemonTranslationKey returns "poke_{id}" for a pokemon ID.
func PokemonTranslationKey(pokemonID int) string {
	return fmt.Sprintf("poke_%d", pokemonID)
}

// FormTranslationKey returns "form_{id}" for a form ID.
func FormTranslationKey(formID int) string {
	return fmt.Sprintf("form_%d", formID)
}

// MoveTranslationKey returns "move_{id}" for a move ID.
func MoveTranslationKey(moveID int) string {
	return fmt.Sprintf("move_%d", moveID)
}

// TypeTranslationKey returns "poke_type_{id}" for a type ID.
func TypeTranslationKey(typeID int) string {
	return fmt.Sprintf("poke_type_%d", typeID)
}

// ItemTranslationKey returns "item_{id}" for an item ID.
func ItemTranslationKey(itemID int) string {
	return fmt.Sprintf("item_%d", itemID)
}

// WeatherTranslationKey returns "weather_{id}" for a weather ID.
func WeatherTranslationKey(weatherID int) string {
	return fmt.Sprintf("weather_%d", weatherID)
}

// GruntTranslationKey returns "grunt_{id}" for a grunt type ID.
func GruntTranslationKey(gruntTypeID int) string {
	return fmt.Sprintf("grunt_%d", gruntTypeID)
}

// MegaEvoTranslationKey returns "poke_{id}_e{evoId}" for a mega evolution.
func MegaEvoTranslationKey(pokemonID, tempEvoID int) string {
	return fmt.Sprintf("poke_%d_e%d", pokemonID, tempEvoID)
}

// MonsterNameInfo holds the computed name components for a pokemon.
// Names are looked up via translation keys at enrichment time.
type MonsterNameInfo struct {
	PokemonKey      string // translation key: "poke_{id}"
	FormKey         string // translation key: "form_{formId}" (empty if form 0)
	MegaNamePattern string // "{0}" or "Mega {0}" etc from util.MegaName
}

// MonsterNameKeys returns the translation keys needed to construct a pokemon's
// display name. The actual translated strings are resolved at enrichment time.
func (gd *GameData) MonsterNameKeys(pokemonID, form, evolution int) MonsterNameInfo {
	info := MonsterNameInfo{
		PokemonKey:      PokemonTranslationKey(pokemonID),
		MegaNamePattern: "{0}",
	}

	if form > 0 {
		info.FormKey = FormTranslationKey(form)
	}

	if evolution > 0 {
		if pattern, ok := gd.Util.MegaName[evolution]; ok {
			info.MegaNamePattern = pattern
		}
	}

	return info
}
