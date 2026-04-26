package dts

import (
	"strings"
)

// ViewBuilder constructs the template view (map[string]any) by merging
// enrichment layers, resolving emoji keys, and adding backward-compatible aliases.
type ViewBuilder struct {
	emoji         *EmojiLookup
	dtsDictionary map[string]any
}

// NewViewBuilder creates a ViewBuilder with the given emoji lookup and DTS dictionary.
// Both parameters may be nil for simple use cases.
func NewViewBuilder(emoji *EmojiLookup, dtsDictionary map[string]any) *ViewBuilder {
	return &ViewBuilder{
		emoji:         emoji,
		dtsDictionary: dtsDictionary,
	}
}

// singleEmojiKeys lists all enrichment fields that contain a single emoji key string.
// Each is resolved via emoji.Lookup(key, platform) and stored without the "Key" suffix.
var singleEmojiKeys = []struct {
	keyField    string
	outputField string
}{
	// Pokemon / Raid / Maxbattle
	{"genderEmojiKey", "genderEmoji"},
	{"quickMoveTypeEmojiKey", "quickMoveEmoji"},
	{"chargeMoveTypeEmojiKey", "chargeMoveEmoji"},
	{"boostWeatherEmojiKey", "boostWeatherEmoji"},
	{"gameWeatherEmojiKey", "gameWeatherEmoji"},
	{"bearingEmojiKey", "bearingEmoji"},
	{"shinyPossibleEmojiKey", "shinyPossibleEmoji"},
	// Invasion
	{"gruntTypeEmojiKey", "gruntTypeEmoji"},
	// Lure
	{"lureEmojiKey", "lureTypeEmoji"},
	// Gym
	{"teamEmojiKey", "teamEmoji"},
	{"oldTeamEmojiKey", "previousControlTeamEmoji"},
	// Weather
	{"weatherEmojiKey", "weatherEmoji"},
	{"oldWeatherEmojiKey", "oldWeatherEmoji"},
	// Weather forecast (pokemon)
	{"weatherCurrentEmojiKey", "weatherCurrentEmoji"},
	{"weatherNextEmojiKey", "weatherNextEmoji"},
}

// arrayEmojiKeys lists enrichment fields that contain arrays of emoji key strings.
var arrayEmojiKeys = []struct {
	keyField    string
	outputField string
}{
	{"typeEmojiKeys", "typeEmoji"},
	{"boostingWeatherEmojiKeys", "boostingWeatherEmojis"},
}

// resolveEmojiArray resolves an array of emoji keys to emoji strings.
func (vb *ViewBuilder) resolveEmojiArray(raw any, platform string) []string {
	if raw == nil {
		return nil
	}
	var keys []string
	switch v := raw.(type) {
	case []string:
		keys = v
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				keys = append(keys, s)
			}
		}
	default:
		return nil
	}
	resolved := make([]string, len(keys))
	for i, key := range keys {
		resolved[i] = vb.emoji.Lookup(key, platform)
	}
	return resolved
}

// aliasMapping maps alias names to their source fields.
// These cover both backward-compat aliases and snake_case → camelCase conversions
// that the alerter controllers used to do manually.
// aliasPair maps a DTS template field name to its enrichment/webhook source field.
type aliasPair struct {
	alias  string
	source string
}

// Common aliases shared by all webhook types.
var commonAliases = []aliasPair{
	{"mapurl", "googleMapUrl"},
	{"applemap", "appleMapUrl"},
	{"distime", "disappearTime"},
	{"staticmap", "staticMap"},
	{"matched", "matchedAreaNames"},
	// Weather aliases — available on all types that have S2 cell weather data
	{"gameweather", "gameWeatherName"},
	{"gameweatheremoji", "gameWeatherEmoji"},
	{"boost", "boostWeatherName"},
	{"boostemoji", "boostWeatherEmoji"},
}

// Per-type alias tables. Each type includes commonAliases plus type-specific mappings.
// This avoids conflicts where the same alias (e.g. "gymName") maps to different source
// fields depending on webhook type ("gym_name" for raids vs "name" for gym changes).
var typeAliases = map[string][]aliasPair{
	"monster": {
		{"formname", "formName"},
		{"ivcolor", "ivColor"},
		{"pokestopName", "pokestop_name"},
		{"pokestopId", "pokestop_id"},
		{"spawnpointId", "spawnpoint_id"},
		{"encounterId", "encounter_id"},
		{"individual_attack", "atk"},
		{"individual_defense", "def"},
		{"individual_stamina", "sta"},
		{"quickMove", "quickMoveName"},
		{"chargeMove", "chargeMoveName"},
		{"move1emoji", "quickMoveEmoji"},
		{"move2emoji", "chargeMoveEmoji"},
		{"pokemonId", "pokemon_id"},
	},
	"monsterNoIv": {
		{"formname", "formName"},
		{"pokestopId", "pokestop_id"},
		{"spawnpointId", "spawnpoint_id"},
		{"encounterId", "encounter_id"},
		{"pokemonId", "pokemon_id"},
	},
	"raid": {
		{"gymName", "gym_name"},
		{"gymUrl", "gym_url"},
		{"gymColor", "gym_color"},
		{"gymId", "gym_id"},
		{"teamId", "team_id"},
		{"hatchtime", "hatchTime"},
		{"ex", "is_ex_raid_eligible"},
		{"move1", "quickMoveName"},
		{"move2", "chargeMoveName"},
		{"formname", "formName"},
		{"quickMove", "quickMoveName"},
		{"chargeMove", "chargeMoveName"},
		{"move1emoji", "quickMoveEmoji"},
		{"move2emoji", "chargeMoveEmoji"},
		{"pokemonId", "pokemon_id"},
	},
	"egg": {
		{"gymName", "gym_name"},
		{"gymUrl", "gym_url"},
		{"gymColor", "gym_color"},
		{"gymId", "gym_id"},
		{"teamId", "team_id"},
		{"hatchtime", "hatchTime"},
		{"ex", "is_ex_raid_eligible"},
	},
	"gym": {
		{"gymName", "name"},
		{"gymUrl", "url"},
		{"gymId", "gym_id"},
		{"gymColor", "gymColor"},
		{"teamId", "team_id"},
	},
	"invasion": {
		{"pokestopName", "pokestop_name"},
		{"pokestopUrl", "pokestop_url"},
		{"pokestopId", "pokestop_id"},
		{"name", "pokestop_name"},
		{"url", "pokestop_url"},
		{"gruntType", "gruntTypeName"},
		{"gender", "gruntGender"},
	},
	"quest": {
		{"pokestopName", "pokestop_name"},
		{"pokestopUrl", "pokestop_url"},
		{"pokestopId", "pokestop_id"},
		{"name", "pokestop_name"},
		{"url", "pokestop_url"},
	},
	"lure": {
		{"pokestopName", "pokestop_name"},
		{"pokestopUrl", "pokestop_url"},
		{"pokestopId", "pokestop_id"},
		{"name", "pokestop_name"},
		{"url", "pokestop_url"},
		{"lureTypeColor", "lureColor"},
		{"lureType", "lureTypeName"},
	},
	"nest": {
		{"nestName", "nest_name"},
		{"nestId", "nest_id"},
	},
	"weatherchange": {
		// Lowercase aliases for legacy / hand-written templates.
		// The canonical fields (weatherName, oldWeatherName) come from
		// per-language enrichment; weatherEmoji / oldWeatherEmoji come from
		// the resolved-emoji layer (singleEmojiKeys).
		{"weather", "weatherName"},
		{"oldweather", "oldWeatherName"},
		{"weathername", "weatherName"},
		{"oldweathername", "oldWeatherName"},
		{"weatheremoji", "weatherEmoji"},
		{"oldweatheremoji", "oldWeatherEmoji"},
	},
	"fort-update": {},
	"maxbattle": {
		{"gymName", "gym_name"},
		{"gymUrl", "gym_url"},
		{"gymId", "gym_id"},
		{"stationId", "station_id"},
		{"stationName", "station_name"},
		{"formname", "formName"},
		{"quickMove", "quickMoveName"},
		{"chargeMove", "chargeMoveName"},
		{"move1emoji", "quickMoveEmoji"},
		{"move2emoji", "chargeMoveEmoji"},
	},
	"greeting": {},
}

// escapeJSONString replaces characters that could break JSON or message formatting.
func escapeJSONString(s string) string {
	s = strings.ReplaceAll(s, `\`, "?")
	s = strings.ReplaceAll(s, `"`, "''")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", "")
	return s
}

// escapeUserContentLayered sanitizes user-generated text fields across all layers.
// Called during LayeredView construction to ensure escaped values are in the computed layer.
func escapeUserContentLayered(computed map[string]any, layers ...map[string]any) {
	for _, field := range []string{"pokestop_name", "pokestop_url", "gym_name", "name"} {
		for _, layer := range layers {
			if layer == nil {
				continue
			}
			if v, ok := layer[field].(string); ok {
				computed[field] = escapeJSONString(v)
				break // first layer wins
			}
		}
	}
}
