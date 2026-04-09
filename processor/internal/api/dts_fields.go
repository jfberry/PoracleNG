package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// FieldDef describes a single template field for the DTS editor.
type FieldDef struct {
	Name                 string `json:"name"`
	Type                 string `json:"type"`
	Description          string `json:"description"`
	Category             string `json:"category"`
	Preferred            bool   `json:"preferred,omitempty"`
	Deprecated           bool   `json:"deprecated,omitempty"`
	RawWebhook           bool   `json:"rawWebhook,omitempty"`
	PreferredAlternative string `json:"preferredAlternative,omitempty"`
}

// BlockScope describes fields available inside a block helper context.
type BlockScope struct {
	Helper         string     `json:"helper"`
	Args           []string   `json:"args,omitempty"`
	Description    string     `json:"description"`
	Fields         []FieldDef `json:"fields"`
	IterableFields []string   `json:"iterableFields,omitempty"`
}

var commonFields = []FieldDef{
	// Location
	{Name: "latitude", Type: "number", Description: "Alert location latitude", Category: "location", Preferred: true},
	{Name: "longitude", Type: "number", Description: "Alert location longitude", Category: "location", Preferred: true},
	{Name: "addr", Type: "string", Description: "Formatted address", Category: "location", Preferred: true},
	{Name: "streetName", Type: "string", Description: "Street name", Category: "location"},
	{Name: "streetNumber", Type: "string", Description: "Street number", Category: "location"},
	{Name: "neighbourhood", Type: "string", Description: "Neighbourhood", Category: "location"},
	{Name: "city", Type: "string", Description: "City name", Category: "location"},
	{Name: "suburb", Type: "string", Description: "Suburb", Category: "location"},
	{Name: "state", Type: "string", Description: "State/province", Category: "location"},
	{Name: "zipcode", Type: "string", Description: "Zip/postal code", Category: "location"},
	{Name: "country", Type: "string", Description: "Country name", Category: "location"},
	{Name: "countryCode", Type: "string", Description: "Country code", Category: "location"},
	{Name: "flag", Type: "string", Description: "Country flag emoji", Category: "location"},
	{Name: "areas", Type: "string", Description: "Comma-separated matched areas", Category: "location"},
	{Name: "areasList", Type: "array", Description: "Matched areas as array", Category: "location"},
	{Name: "distance", Type: "number", Description: "Distance from user location", Category: "location"},
	{Name: "bearing", Type: "int", Description: "Bearing degrees from user", Category: "location"},
	{Name: "bearingEmoji", Type: "string", Description: "Directional arrow emoji", Category: "location"},
	// Maps
	{Name: "staticMap", Type: "string", Description: "Static map image URL", Category: "maps", Preferred: true},
	{Name: "staticmap", Type: "string", Description: "Deprecated alias for staticMap", Category: "maps", Deprecated: true, PreferredAlternative: "staticMap"},
	{Name: "imgUrl", Type: "string", Description: "Primary icon URL", Category: "maps", Preferred: true},
	{Name: "imgUrlAlt", Type: "string", Description: "Alternative icon URL", Category: "maps"},
	{Name: "stickerUrl", Type: "string", Description: "Sticker icon URL", Category: "maps"},
	{Name: "googleMapUrl", Type: "string", Description: "Google Maps link", Category: "maps", Preferred: true},
	{Name: "appleMapUrl", Type: "string", Description: "Apple Maps link", Category: "maps", Preferred: true},
	{Name: "wazeMapUrl", Type: "string", Description: "Waze link", Category: "maps"},
	{Name: "rdmUrl", Type: "string", Description: "RDM map link", Category: "maps"},
	{Name: "reactMapUrl", Type: "string", Description: "ReactMap link", Category: "maps"},
	{Name: "rocketMadUrl", Type: "string", Description: "RocketMad link", Category: "maps"},
	{Name: "mapurl", Type: "string", Description: "Deprecated alias for googleMapUrl", Category: "maps", Deprecated: true, PreferredAlternative: "googleMapUrl"},
	{Name: "applemap", Type: "string", Description: "Deprecated alias for appleMapUrl", Category: "maps", Deprecated: true, PreferredAlternative: "appleMapUrl"},
	// Time
	{Name: "tthd", Type: "int", Description: "Days remaining", Category: "time"},
	{Name: "tthh", Type: "int", Description: "Hours remaining", Category: "time", Preferred: true},
	{Name: "tthm", Type: "int", Description: "Minutes remaining", Category: "time", Preferred: true},
	{Name: "tths", Type: "int", Description: "Seconds remaining", Category: "time", Preferred: true},
	{Name: "now", Type: "string", Description: "Current date/time", Category: "time"},
	{Name: "nowPokemon", Type: "string", Description: "Current date/time (pokemon format)", Category: "time"},
	{Name: "sunsetTime", Type: "string", Description: "Sunset time", Category: "time"},
	{Name: "sunriseTime", Type: "string", Description: "Sunrise time", Category: "time"},
	{Name: "isNight", Type: "bool", Description: "Is nighttime", Category: "time"},
	{Name: "isDusk", Type: "bool", Description: "Is dusk", Category: "time"},
	{Name: "isDawn", Type: "bool", Description: "Is dawn", Category: "time"},
}

var monsterFields = []FieldDef{
	// Identity
	{Name: "name", Type: "string", Description: "Translated pokemon name", Category: "identity", Preferred: true},
	{Name: "fullName", Type: "string", Description: "Name + form combined", Category: "identity", Preferred: true},
	{Name: "formName", Type: "string", Description: "Translated form name", Category: "identity", Preferred: true},
	{Name: "formNormalised", Type: "string", Description: "Form name (empty for Normal)", Category: "identity"},
	{Name: "pokemonId", Type: "int", Description: "Pokemon ID", Category: "identity", Preferred: true},
	{Name: "pokemon_id", Type: "int", Description: "Pokemon ID (webhook)", Category: "identity", RawWebhook: true, PreferredAlternative: "pokemonId"},
	{Name: "formId", Type: "int", Description: "Form ID", Category: "identity", Preferred: true},
	{Name: "nameEng", Type: "string", Description: "English pokemon name", Category: "identity"},
	{Name: "fullNameEng", Type: "string", Description: "English name + form", Category: "identity"},
	{Name: "formNameEng", Type: "string", Description: "English form name", Category: "identity"},
	// Stats
	{Name: "iv", Type: "number", Description: "IV percentage (0-100)", Category: "stats", Preferred: true},
	{Name: "atk", Type: "int", Description: "Attack IV (0-15)", Category: "stats", Preferred: true},
	{Name: "def", Type: "int", Description: "Defense IV (0-15)", Category: "stats", Preferred: true},
	{Name: "sta", Type: "int", Description: "Stamina IV (0-15)", Category: "stats", Preferred: true},
	{Name: "cp", Type: "int", Description: "Combat Power", Category: "stats", Preferred: true},
	{Name: "level", Type: "int", Description: "Pokemon level", Category: "stats", Preferred: true},
	{Name: "ivColor", Type: "string", Description: "Hex color based on IV range", Category: "stats", Preferred: true},
	{Name: "weight", Type: "string", Description: "Weight in kg", Category: "stats"},
	{Name: "height", Type: "string", Description: "Height in m", Category: "stats"},
	{Name: "baseStats", Type: "object", Description: "{baseAttack, baseDefense, baseStamina}", Category: "stats"},
	{Name: "encountered", Type: "bool", Description: "Has IV data", Category: "stats"},
	{Name: "individual_attack", Type: "int", Description: "Attack IV (webhook)", Category: "stats", RawWebhook: true, PreferredAlternative: "atk"},
	{Name: "individual_defense", Type: "int", Description: "Defense IV (webhook)", Category: "stats", RawWebhook: true, PreferredAlternative: "def"},
	{Name: "individual_stamina", Type: "int", Description: "Stamina IV (webhook)", Category: "stats", RawWebhook: true, PreferredAlternative: "sta"},
	// Moves
	{Name: "quickMoveName", Type: "string", Description: "Translated fast move name", Category: "moves", Preferred: true},
	{Name: "chargeMoveName", Type: "string", Description: "Translated charged move name", Category: "moves", Preferred: true},
	{Name: "quickMoveEmoji", Type: "string", Description: "Fast move type emoji", Category: "moves"},
	{Name: "chargeMoveEmoji", Type: "string", Description: "Charged move type emoji", Category: "moves"},
	{Name: "quickMoveNameEng", Type: "string", Description: "English fast move name", Category: "moves"},
	{Name: "chargeMoveNameEng", Type: "string", Description: "English charged move name", Category: "moves"},
	{Name: "quickMoveId", Type: "int", Description: "Fast move ID", Category: "moves"},
	{Name: "chargeMoveId", Type: "int", Description: "Charged move ID", Category: "moves"},
	{Name: "move_1", Type: "int", Description: "Fast move ID (webhook)", Category: "moves", RawWebhook: true, PreferredAlternative: "quickMoveId"},
	{Name: "move_2", Type: "int", Description: "Charged move ID (webhook)", Category: "moves", RawWebhook: true, PreferredAlternative: "chargeMoveId"},
	// Time
	{Name: "time", Type: "string", Description: "Disappear time (formatted)", Category: "time", Preferred: true},
	{Name: "disappearTime", Type: "string", Description: "Disappear time", Category: "time"},
	{Name: "confirmedTime", Type: "bool", Description: "Is disappear time verified", Category: "time"},
	{Name: "tthSeconds", Type: "int", Description: "Total seconds remaining", Category: "time"},
	{Name: "distime", Type: "string", Description: "Deprecated alias for disappearTime", Category: "time", Deprecated: true, PreferredAlternative: "disappearTime"},
	// Types
	{Name: "typeName", Type: "string", Description: "Comma-joined translated type names (e.g. \"Fire, Flying\")", Category: "types", Preferred: true},
	{Name: "typeNameEng", Type: "array", Description: "Array of English type names", Category: "types"},
	{Name: "typeEmojiKeys", Type: "array", Description: "Array of type emoji keys (raw)", Category: "types"},
	{Name: "typeEmoji", Type: "array", Description: "Array of resolved type emoji strings", Category: "types", Preferred: true},
	{Name: "emoji", Type: "array", Description: "Alias for typeEmoji (resolved)", Category: "types"},
	{Name: "color", Type: "string", Description: "Primary type color hex", Category: "types", Preferred: true},
	// Weather
	{Name: "boostWeatherEmoji", Type: "string", Description: "Boost weather emoji", Category: "weather", Preferred: true},
	{Name: "boostWeatherName", Type: "string", Description: "Translated boost weather", Category: "weather"},
	{Name: "boosted", Type: "bool", Description: "Is weather boosted", Category: "weather"},
	{Name: "gameWeatherName", Type: "string", Description: "Current game weather name", Category: "weather"},
	{Name: "gameWeatherEmoji", Type: "string", Description: "Current game weather emoji", Category: "weather"},
	{Name: "weatherChange", Type: "string", Description: "Weather forecast text", Category: "weather"},
	{Name: "weatherCurrentName", Type: "string", Description: "Current weather name", Category: "weather"},
	{Name: "weatherNextName", Type: "string", Description: "Next hour weather name", Category: "weather"},
	// PVP
	{Name: "pvpGreat", Type: "array", Description: "Great League PVP display list", Category: "pvp", Preferred: true},
	{Name: "pvpUltra", Type: "array", Description: "Ultra League PVP display list", Category: "pvp", Preferred: true},
	{Name: "pvpLittle", Type: "array", Description: "Little League PVP display list", Category: "pvp"},
	// Other
	{Name: "generation", Type: "int", Description: "Generation number", Category: "other"},
	{Name: "generationName", Type: "string", Description: "Generation name", Category: "other"},
	{Name: "genderName", Type: "string", Description: "Gender name", Category: "other"},
	{Name: "genderEmoji", Type: "string", Description: "Gender emoji", Category: "other"},
	{Name: "sizeName", Type: "string", Description: "Size category name", Category: "other"},
	{Name: "rarityName", Type: "string", Description: "Rarity group name", Category: "other"},
	{Name: "costume", Type: "int", Description: "Costume ID", Category: "other"},
	{Name: "shinyPossible", Type: "bool", Description: "Can be shiny", Category: "other"},
	{Name: "weaknessList", Type: "array", Description: "Type weakness list", Category: "other"},
	{Name: "evolutions", Type: "array", Description: "Evolution entries", Category: "other"},
	{Name: "megaEvolutions", Type: "array", Description: "Mega evolution entries", Category: "other"},
	{Name: "hasEvolutions", Type: "bool", Description: "Has evolutions", Category: "other"},
	{Name: "hasMegaEvolutions", Type: "bool", Description: "Has mega evolutions", Category: "other"},
	{Name: "pokestopName", Type: "string", Description: "Nearby pokestop name", Category: "other"},
}

var raidFields = []FieldDef{
	// Identity
	{Name: "name", Type: "string", Description: "Translated pokemon name", Category: "identity", Preferred: true},
	{Name: "fullName", Type: "string", Description: "Name + form", Category: "identity", Preferred: true},
	{Name: "formName", Type: "string", Description: "Translated form name", Category: "identity"},
	{Name: "nameEng", Type: "string", Description: "English pokemon name", Category: "identity"},
	{Name: "fullNameEng", Type: "string", Description: "English name + form", Category: "identity"},
	{Name: "pokemonId", Type: "int", Description: "Pokemon ID", Category: "identity"},
	{Name: "level", Type: "int", Description: "Raid level", Category: "identity", Preferred: true},
	{Name: "levelName", Type: "string", Description: "Raid level name", Category: "identity", Preferred: true},
	// Gym
	{Name: "gymName", Type: "string", Description: "Gym name", Category: "gym", Preferred: true},
	{Name: "gym_name", Type: "string", Description: "Gym name (webhook)", Category: "gym", RawWebhook: true, PreferredAlternative: "gymName"},
	{Name: "gymUrl", Type: "string", Description: "Gym image URL", Category: "gym"},
	{Name: "gymColor", Type: "string", Description: "Team color hex", Category: "gym"},
	{Name: "teamName", Type: "string", Description: "Team name", Category: "gym"},
	{Name: "teamEmoji", Type: "string", Description: "Team emoji", Category: "gym"},
	{Name: "ex", Type: "bool", Description: "EX raid eligible", Category: "gym", Preferred: true},
	// Stats
	{Name: "cp", Type: "int", Description: "Boss CP (20/25)", Category: "stats"},
	{Name: "cp20", Type: "int", Description: "Boss CP at level 20", Category: "stats"},
	{Name: "cp25", Type: "int", Description: "Boss CP at level 25", Category: "stats"},
	// Moves
	{Name: "quickMoveName", Type: "string", Description: "Translated fast move", Category: "moves", Preferred: true},
	{Name: "chargeMoveName", Type: "string", Description: "Translated charged move", Category: "moves", Preferred: true},
	{Name: "quickMoveEmoji", Type: "string", Description: "Fast move type emoji", Category: "moves"},
	{Name: "chargeMoveEmoji", Type: "string", Description: "Charged move type emoji", Category: "moves"},
	// Time
	{Name: "time", Type: "string", Description: "End time", Category: "time", Preferred: true},
	{Name: "hatchTime", Type: "string", Description: "Hatch/start time", Category: "time"},
	// Types
	{Name: "typeName", Type: "string", Description: "Comma-joined translated type names", Category: "types"},
	{Name: "typeNameEng", Type: "array", Description: "English type names", Category: "types"},
	{Name: "typeEmoji", Type: "array", Description: "Resolved type emoji strings", Category: "types"},
	{Name: "color", Type: "string", Description: "Primary type color hex", Category: "types"},
	// Weather
	{Name: "boostWeatherEmoji", Type: "string", Description: "Boost weather emoji", Category: "weather"},
	{Name: "boostWeatherName", Type: "string", Description: "Translated boost weather", Category: "weather"},
	// Other
	{Name: "shinyPossible", Type: "bool", Description: "Can be shiny", Category: "other"},
	{Name: "evolutions", Type: "array", Description: "Evolution entries", Category: "other"},
	{Name: "megaEvolutions", Type: "array", Description: "Mega evolution entries", Category: "other"},
	{Name: "hasEvolutions", Type: "bool", Description: "Has evolutions", Category: "other"},
	{Name: "hasMegaEvolutions", Type: "bool", Description: "Has mega evolutions", Category: "other"},
	{Name: "weaknessList", Type: "array", Description: "Type weakness list", Category: "other"},
	{Name: "rsvps", Type: "array", Description: "RSVP timeslot entries", Category: "other"},
}

var eggFields = []FieldDef{
	{Name: "level", Type: "int", Description: "Egg level", Category: "identity", Preferred: true},
	{Name: "levelName", Type: "string", Description: "Raid level name", Category: "identity", Preferred: true},
	{Name: "gymName", Type: "string", Description: "Gym name", Category: "gym", Preferred: true},
	{Name: "gymUrl", Type: "string", Description: "Gym image URL", Category: "gym"},
	{Name: "gymColor", Type: "string", Description: "Team color hex", Category: "gym"},
	{Name: "teamName", Type: "string", Description: "Team name", Category: "gym"},
	{Name: "teamEmoji", Type: "string", Description: "Team emoji", Category: "gym"},
	{Name: "ex", Type: "bool", Description: "EX raid eligible", Category: "gym"},
	{Name: "time", Type: "string", Description: "Hatch time", Category: "time", Preferred: true},
	{Name: "hatchTime", Type: "string", Description: "Hatch time", Category: "time"},
	{Name: "rsvps", Type: "array", Description: "RSVP timeslot entries", Category: "other"},
}

var questFields = []FieldDef{
	{Name: "pokestopName", Type: "string", Description: "Pokestop name", Category: "location", Preferred: true},
	{Name: "questString", Type: "string", Description: "Quest description", Category: "quest", Preferred: true},
	{Name: "rewardString", Type: "string", Description: "Reward description", Category: "quest", Preferred: true},
	{Name: "dustAmount", Type: "int", Description: "Stardust amount", Category: "quest"},
	{Name: "itemAmount", Type: "int", Description: "Item amount", Category: "quest"},
	{Name: "itemName", Type: "string", Description: "Item name", Category: "quest"},
	{Name: "monsterName", Type: "string", Description: "Reward pokemon name", Category: "quest"},
	{Name: "monsterFullName", Type: "string", Description: "Reward pokemon full name", Category: "quest"},
	{Name: "energyAmount", Type: "int", Description: "Mega energy amount", Category: "quest"},
	{Name: "energyMonsterName", Type: "string", Description: "Mega energy pokemon name", Category: "quest"},
	{Name: "candyAmount", Type: "int", Description: "Candy amount", Category: "quest"},
	{Name: "candyMonsterName", Type: "string", Description: "Candy pokemon name", Category: "quest"},
	{Name: "xlCandyAmount", Type: "int", Description: "XL candy amount", Category: "quest"},
	{Name: "xlCandyMonsterName", Type: "string", Description: "XL candy pokemon name", Category: "quest"},
	{Name: "shiny", Type: "bool", Description: "Reward is shiny", Category: "quest"},
}

var invasionFields = []FieldDef{
	{Name: "pokestopName", Type: "string", Description: "Pokestop name", Category: "location", Preferred: true},
	{Name: "gruntName", Type: "string", Description: "Grunt name", Category: "invasion", Preferred: true},
	{Name: "gruntTypeName", Type: "string", Description: "Grunt type name", Category: "invasion", Preferred: true},
	{Name: "gruntTypeEmoji", Type: "string", Description: "Grunt type emoji", Category: "invasion"},
	{Name: "gruntTypeColor", Type: "string", Description: "Grunt type color hex", Category: "invasion"},
	{Name: "gruntTypeId", Type: "int", Description: "Grunt type ID", Category: "invasion"},
	{Name: "displayTypeId", Type: "int", Description: "Display type ID", Category: "invasion"},
	{Name: "genderName", Type: "string", Description: "Grunt gender name", Category: "invasion"},
	{Name: "genderEmoji", Type: "string", Description: "Grunt gender emoji", Category: "invasion"},
	{Name: "gruntRewardsList", Type: "object", Description: "Reward pokemon lists", Category: "invasion"},
	{Name: "gruntLineupList", Type: "array", Description: "Confirmed lineup pokemon", Category: "invasion"},
	{Name: "time", Type: "string", Description: "End time", Category: "time", Preferred: true},
}

var lureFields = []FieldDef{
	{Name: "pokestopName", Type: "string", Description: "Pokestop name", Category: "location", Preferred: true},
	{Name: "lureTypeId", Type: "int", Description: "Lure type ID", Category: "lure", Preferred: true},
	{Name: "lureTypeName", Type: "string", Description: "Translated lure type name", Category: "lure", Preferred: true},
	{Name: "lureTypeEmoji", Type: "string", Description: "Lure type emoji", Category: "lure"},
	{Name: "lureTypeColor", Type: "string", Description: "Lure type color hex", Category: "lure"},
	{Name: "time", Type: "string", Description: "Expiry time", Category: "time", Preferred: true},
	{Name: "gruntTypeId", Type: "int", Description: "Co-existing invasion grunt type", Category: "invasion"},
	{Name: "displayTypeId", Type: "int", Description: "Co-existing invasion display type", Category: "invasion"},
}

var nestFields = []FieldDef{
	{Name: "name", Type: "string", Description: "Translated pokemon name", Category: "identity", Preferred: true},
	{Name: "fullName", Type: "string", Description: "Name + form", Category: "identity", Preferred: true},
	{Name: "formName", Type: "string", Description: "Translated form name", Category: "identity"},
	{Name: "nameEng", Type: "string", Description: "English pokemon name", Category: "identity"},
	{Name: "nest_name", Type: "string", Description: "Nest name", Category: "location", Preferred: true},
	{Name: "pokemonId", Type: "int", Description: "Pokemon ID", Category: "identity"},
	{Name: "pokemonCount", Type: "int", Description: "Pokemon count", Category: "stats"},
	{Name: "pokemonSpawnAvg", Type: "number", Description: "Avg spawns per hour", Category: "stats"},
	{Name: "pokemonRatio", Type: "number", Description: "Pokemon spawn ratio", Category: "stats"},
	{Name: "shinyPossible", Type: "bool", Description: "Can be shiny", Category: "other"},
	{Name: "typeName", Type: "string", Description: "Comma-joined translated type names", Category: "types"},
	{Name: "typeEmoji", Type: "array", Description: "Resolved type emoji strings", Category: "types"},
	{Name: "color", Type: "string", Description: "Primary type color hex", Category: "types"},
}

var gymFields = []FieldDef{
	{Name: "gymName", Type: "string", Description: "Gym name", Category: "gym", Preferred: true},
	{Name: "teamName", Type: "string", Description: "Current team name", Category: "gym", Preferred: true},
	{Name: "teamEmoji", Type: "string", Description: "Team emoji", Category: "gym"},
	{Name: "gymColor", Type: "string", Description: "Team color hex", Category: "gym"},
	{Name: "oldTeamName", Type: "string", Description: "Previous team name", Category: "gym"},
	{Name: "oldTeamEmoji", Type: "string", Description: "Previous team emoji", Category: "gym"},
	{Name: "slotsAvailable", Type: "int", Description: "Available gym slots", Category: "gym"},
	{Name: "oldSlotsAvailable", Type: "int", Description: "Previous slot count", Category: "gym"},
	{Name: "inBattle", Type: "bool", Description: "Is gym in battle", Category: "gym"},
	{Name: "ex", Type: "bool", Description: "EX raid eligible", Category: "gym"},
	{Name: "previousControlName", Type: "string", Description: "Previous owner name", Category: "gym"},
}

var fortUpdateFields = []FieldDef{
	{Name: "name", Type: "string", Description: "Fort name", Category: "identity", Preferred: true},
	{Name: "id", Type: "string", Description: "Fort ID", Category: "identity"},
	{Name: "fortType", Type: "string", Description: "pokestop or gym", Category: "identity", Preferred: true},
	{Name: "changeType", Type: "string", Description: "new, edit, or removal", Category: "change", Preferred: true},
	{Name: "changeTypeText", Type: "string", Description: "Translated change type", Category: "change"},
	{Name: "editTypesList", Type: "array", Description: "Types of edits made", Category: "change"},
	{Name: "isEditName", Type: "bool", Description: "Name was changed", Category: "change"},
	{Name: "isEditLocation", Type: "bool", Description: "Location was changed", Category: "change"},
	{Name: "isEditImage", Type: "bool", Description: "Image was changed", Category: "change"},
	{Name: "newName", Type: "string", Description: "New fort name", Category: "change"},
	{Name: "oldName", Type: "string", Description: "Previous fort name", Category: "change"},
	{Name: "newDescription", Type: "string", Description: "New fort description", Category: "change"},
	{Name: "oldDescription", Type: "string", Description: "Previous fort description", Category: "change"},
	{Name: "newImageUrl", Type: "string", Description: "New image URL", Category: "change"},
	{Name: "oldImageUrl", Type: "string", Description: "Previous image URL", Category: "change"},
	{Name: "isEmpty", Type: "bool", Description: "No name or description", Category: "change"},
}

var maxbattleFields = []FieldDef{
	{Name: "name", Type: "string", Description: "Translated pokemon name", Category: "identity", Preferred: true},
	{Name: "fullName", Type: "string", Description: "Name + form", Category: "identity", Preferred: true},
	{Name: "pokemonId", Type: "int", Description: "Pokemon ID", Category: "identity"},
	{Name: "level", Type: "int", Description: "Battle level", Category: "identity", Preferred: true},
	{Name: "pokestopName", Type: "string", Description: "Location name", Category: "location", Preferred: true},
	{Name: "time", Type: "string", Description: "End time", Category: "time", Preferred: true},
	{Name: "quickMoveName", Type: "string", Description: "Translated fast move", Category: "moves"},
	{Name: "chargeMoveName", Type: "string", Description: "Translated charged move", Category: "moves"},
	{Name: "quickMoveEmoji", Type: "string", Description: "Fast move type emoji", Category: "moves"},
	{Name: "chargeMoveEmoji", Type: "string", Description: "Charged move type emoji", Category: "moves"},
	{Name: "typeName", Type: "string", Description: "Comma-joined translated type names", Category: "types"},
	{Name: "typeEmoji", Type: "array", Description: "Resolved type emoji strings", Category: "types"},
	{Name: "color", Type: "string", Description: "Primary type color hex", Category: "types"},
	{Name: "gmax", Type: "bool", Description: "Is Gigantamax", Category: "other"},
	{Name: "totalStationedPokemon", Type: "int", Description: "Total stationed pokemon", Category: "other"},
	{Name: "totalStationedGmax", Type: "int", Description: "Total stationed Gmax", Category: "other"},
}

var weatherChangeFields = []FieldDef{
	{Name: "weatherName", Type: "string", Description: "New weather name", Category: "weather", Preferred: true},
	{Name: "oldWeatherName", Type: "string", Description: "Previous weather name", Category: "weather", Preferred: true},
	{Name: "weatherEmojiKey", Type: "string", Description: "New weather emoji key", Category: "weather"},
	{Name: "oldWeatherEmojiKey", Type: "string", Description: "Previous weather emoji key", Category: "weather"},
	{Name: "weatherTthh", Type: "int", Description: "Hours until weather change", Category: "time"},
	{Name: "weatherTthm", Type: "int", Description: "Minutes until weather change", Category: "time"},
	{Name: "weatherTths", Type: "int", Description: "Seconds until weather change", Category: "time"},
	{Name: "enrichedActivePokemons", Type: "array", Description: "Active pokemon affected by weather change", Category: "pokemon"},
}

var greetingFields = []FieldDef{
	{Name: "pokemonId", Type: "int", Description: "Random pokemon ID", Category: "identity"},
	{Name: "pokemonName", Type: "string", Description: "Translated random pokemon name", Category: "identity"},
}

var pvpEntryFields = []FieldDef{
	{Name: "rank", Type: "int", Description: "PVP rank"},
	{Name: "cp", Type: "int", Description: "CP at this rank"},
	{Name: "level", Type: "number", Description: "Level at this rank"},
	{Name: "levelWithCap", Type: "string", Description: "Level with cap notation (e.g. 40/50 if uncapped)"},
	{Name: "cap", Type: "int", Description: "Level cap"},
	{Name: "capped", Type: "bool", Description: "True when this rank is at the level cap"},
	{Name: "percentage", Type: "string", Description: "Stat product percentage (formatted)"},
	{Name: "pokemon", Type: "int", Description: "Pokemon ID for this rank"},
	{Name: "form", Type: "int", Description: "Form ID for this rank"},
	{Name: "evolution", Type: "int", Description: "Evolution form ID (mega etc.)"},
	{Name: "name", Type: "string", Description: "Translated pokemon name"},
	{Name: "fullName", Type: "string", Description: "Pokemon name + form"},
	{Name: "formName", Type: "string", Description: "Translated form name"},
	{Name: "formNormalised", Type: "string", Description: "Form name (empty for Normal)"},
	{Name: "nameEng", Type: "string", Description: "English pokemon name"},
	{Name: "fullNameEng", Type: "string", Description: "English name + form"},
	{Name: "formNormalisedEng", Type: "string", Description: "English form name (empty for Normal)"},
	{Name: "baseStats", Type: "object", Description: "{baseAttack, baseDefense, baseStamina}"},
	{Name: "baseAttack", Type: "int", Description: "Base attack stat"},
	{Name: "baseDefense", Type: "int", Description: "Base defense stat"},
	{Name: "baseStamina", Type: "int", Description: "Base stamina stat"},
}

var weaknessEntryFields = []FieldDef{
	{Name: "value", Type: "number", Description: "Damage multiplier (e.g. 1.6, 2.56)"},
	{Name: "typeName", Type: "string", Description: "Comma-joined translated type names"},
	{Name: "types", Type: "array", Description: "Array of {typeId, name, emojiKey} entries"},
	{Name: "typeEmojiKeys", Type: "array", Description: "Array of type emoji keys"},
}

var evolutionEntryFields = []FieldDef{
	{Name: "id", Type: "int", Description: "Pokemon ID of the evolution"},
	{Name: "form", Type: "int", Description: "Form ID of the evolution"},
	{Name: "name", Type: "string", Description: "Translated pokemon name"},
	{Name: "fullName", Type: "string", Description: "Translated name + form"},
	{Name: "formName", Type: "string", Description: "Translated form name"},
	{Name: "formNormalised", Type: "string", Description: "Form name (empty for Normal)"},
	{Name: "typeName", Type: "string", Description: "Comma-joined translated type names"},
	{Name: "typeEmojiKeys", Type: "array", Description: "Array of type emoji keys"},
	{Name: "baseStats", Type: "object", Description: "{baseAttack, baseDefense, baseStamina}"},
}

var megaEvolutionEntryFields = []FieldDef{
	{Name: "evolution", Type: "int", Description: "Temp evolution ID"},
	{Name: "fullName", Type: "string", Description: "Translated mega name"},
	{Name: "typeName", Type: "string", Description: "Comma-joined translated type names"},
	{Name: "typeEmojiKeys", Type: "array", Description: "Array of type emoji keys"},
	{Name: "baseStats", Type: "object", Description: "{baseAttack, baseDefense, baseStamina}"},
}

var rsvpEntryFields = []FieldDef{
	{Name: "time", Type: "string", Description: "Formatted timeslot time"},
	{Name: "timeslot", Type: "int", Description: "Timeslot in seconds (snake_case)"},
	{Name: "timeSlot", Type: "int", Description: "Timeslot in seconds (camelCase)"},
	{Name: "going_count", Type: "int", Description: "Number going (snake_case)"},
	{Name: "goingCount", Type: "int", Description: "Number going (camelCase)"},
	{Name: "maybe_count", Type: "int", Description: "Number maybe (snake_case)"},
	{Name: "maybeCount", Type: "int", Description: "Number maybe (camelCase)"},
}

var monsterBlockScopes = []BlockScope{
	{
		Helper:         "each",
		Args:           []string{"pvpGreat"},
		IterableFields: []string{"pvpGreat", "pvpUltra", "pvpLittle"},
		Description:    "Iterate over a PVP league display list",
		Fields:         pvpEntryFields,
	},
	{
		Helper:         "each",
		Args:           []string{"weaknessList"},
		IterableFields: []string{"weaknessList"},
		Description:    "Iterate over weakness categories (grouped by multiplier)",
		Fields:         weaknessEntryFields,
	},
	{
		Helper:         "each",
		Args:           []string{"evolutions"},
		IterableFields: []string{"evolutions"},
		Description:    "Iterate over evolution chain entries",
		Fields:         evolutionEntryFields,
	},
	{
		Helper:         "each",
		Args:           []string{"megaEvolutions"},
		IterableFields: []string{"megaEvolutions"},
		Description:    "Iterate over mega evolution entries",
		Fields:         megaEvolutionEntryFields,
	},
	{
		Helper:         "each",
		Args:           []string{"typeEmoji"},
		IterableFields: []string{"typeEmoji", "typeNameEng", "emoji"},
		Description:    "Iterate over a type emoji / English type name array (each item is a string)",
	},
	{
		Helper:      "pokemon",
		Args:        []string{"id", "form"},
		Description: "Pokemon data block helper",
		Fields: []FieldDef{
			{Name: "name", Type: "string", Description: "Translated pokemon name"},
			{Name: "nameEng", Type: "string", Description: "English pokemon name"},
			{Name: "formName", Type: "string", Description: "Translated form name"},
			{Name: "formNameEng", Type: "string", Description: "English form name"},
			{Name: "fullName", Type: "string", Description: "Name + form"},
			{Name: "fullNameEng", Type: "string", Description: "English name + form"},
			{Name: "typeName", Type: "array", Description: "Translated type names"},
			{Name: "typeNameEng", Type: "array", Description: "English type names"},
			{Name: "typeEmoji", Type: "array", Description: "Type emoji strings"},
			{Name: "baseStats", Type: "object", Description: "{baseAttack, baseDefense, baseStamina}"},
			{Name: "hasEvolutions", Type: "bool", Description: "Has evolutions"},
		},
	},
	{
		Helper:      "getPowerUpCost",
		Args:        []string{"levelStart", "levelEnd"},
		Description: "Power-up cost between two levels",
		Fields: []FieldDef{
			{Name: "stardust", Type: "int", Description: "Stardust cost"},
			{Name: "candy", Type: "int", Description: "Candy cost"},
			{Name: "xlCandy", Type: "int", Description: "XL Candy cost"},
		},
	},
}

var raidBlockScopes = []BlockScope{
	{
		Helper:         "each",
		Args:           []string{"weaknessList"},
		IterableFields: []string{"weaknessList"},
		Description:    "Iterate over weakness categories",
		Fields:         weaknessEntryFields,
	},
	{
		Helper:         "each",
		Args:           []string{"evolutions"},
		IterableFields: []string{"evolutions"},
		Description:    "Iterate over evolution chain entries",
		Fields:         evolutionEntryFields,
	},
	{
		Helper:         "each",
		Args:           []string{"megaEvolutions"},
		IterableFields: []string{"megaEvolutions"},
		Description:    "Iterate over mega evolution entries",
		Fields:         megaEvolutionEntryFields,
	},
	{
		Helper:         "each",
		Args:           []string{"rsvps"},
		IterableFields: []string{"rsvps"},
		Description:    "Iterate over RSVP timeslots",
		Fields:         rsvpEntryFields,
	},
	{
		Helper:         "each",
		Args:           []string{"typeEmoji"},
		IterableFields: []string{"typeEmoji", "typeNameEng"},
		Description:    "Iterate over a type emoji / English type name array",
	},
}

var eggBlockScopes = []BlockScope{
	{
		Helper:         "each",
		Args:           []string{"rsvps"},
		IterableFields: []string{"rsvps"},
		Description:    "Iterate over RSVP timeslots",
		Fields:         rsvpEntryFields,
	},
}

// Snippet is a pre-made Handlebars expression the editor can offer for quick insertion.
type Snippet struct {
	Label       string `json:"label"`
	Insert      string `json:"insert"`
	Description string `json:"description"`
	Category    string `json:"category,omitempty"`
}

var commonSnippets = []Snippet{
	// Conditionals
	{Label: "if / else", Insert: "{{#if fieldName}}...{{else}}...{{/if}}", Description: "Conditional block", Category: "control"},
	{Label: "unless", Insert: "{{#unless fieldName}}...{{/unless}}", Description: "Inverse conditional", Category: "control"},
	{Label: "eq", Insert: "{{#eq fieldName value}}...{{/eq}}", Description: "Equal comparison block", Category: "control"},
	{Label: "isnt", Insert: "{{#isnt fieldName value}}...{{else}}...{{/isnt}}", Description: "Not-equal comparison with else", Category: "control"},
	{Label: "gt / lt / gte / lte", Insert: "{{#gt fieldName value}}...{{/gt}}", Description: "Numeric comparison block", Category: "control"},
	{Label: "and (subexpr)", Insert: "{{#if (and (gt a 1) (lt b 5))}}...{{/if}}", Description: "Combine conditions with and", Category: "control"},
	{Label: "or (subexpr)", Insert: "{{#if (or condA condB)}}...{{/if}}", Description: "Either condition true", Category: "control"},
	// Formatting
	{Label: "round", Insert: "{{round fieldName}}", Description: "Round number to nearest integer", Category: "format"},
	{Label: "toFixed", Insert: "{{toFixed fieldName 2}}", Description: "Format to N decimal places", Category: "format"},
	{Label: "pad0", Insert: "{{pad0 fieldName 3}}", Description: "Zero-pad to N characters (default 3)", Category: "format"},
	{Label: "addCommas", Insert: "{{addCommas fieldName}}", Description: "Thousand separators (12,345)", Category: "format"},
	{Label: "lowercase", Insert: "{{lowercase fieldName}}", Description: "Convert to lowercase", Category: "format"},
	{Label: "uppercase", Insert: "{{uppercase fieldName}}", Description: "Convert to uppercase", Category: "format"},
	// String
	{Label: "contains", Insert: "{{#contains fieldName 'text'}}...{{/contains}}", Description: "Check if string contains text", Category: "string"},
	{Label: "replace", Insert: "{{replace fieldName 'old' 'new'}}", Description: "Replace all occurrences", Category: "string"},
	{Label: "concat", Insert: "{{concat a b c}}", Description: "Concatenate strings", Category: "string"},
	// Iteration
	{Label: "each", Insert: "{{#each arrayField}}{{this}}{{/each}}", Description: "Iterate over an array", Category: "iteration"},
	{Label: "each with index", Insert: "{{#each arrayField}}{{@index}}: {{this}}{{/each}}", Description: "Iterate with index", Category: "iteration"},
	// Emoji
	{Label: "getEmoji", Insert: "{{getEmoji 'key'}}", Description: "Look up emoji by key", Category: "emoji"},
	// Links
	{Label: "Shortlink", Insert: "<S<{{url}}>S>", Description: "Wrap a URL for Shlink shortening", Category: "link"},
	// Maps
	{Label: "Static map image", Insert: "[\u200A]({{{staticMap}}})", Description: "Invisible-text image link (Telegram/Discord)", Category: "link"},
	{Label: "Google Maps link", Insert: "[Google]({{{googleMapUrl}}})", Description: "Clickable Google Maps link", Category: "link"},
	{Label: "Apple Maps link", Insert: "[Apple]({{{appleMapUrl}}})", Description: "Clickable Apple Maps link", Category: "link"},
}

var monsterSnippets = []Snippet{
	{Label: "Round IV", Insert: "{{round iv}}", Description: "IV rounded to integer", Category: "pokemon"},
	{Label: "IV or 💯", Insert: "{{#isnt iv 100}}{{round iv}}%{{else}}💯{{/isnt}}", Description: "Show IV% or 💯 for hundos", Category: "pokemon"},
	{Label: "IV with stars", Insert: "{{#isnt iv 100}}*{{round iv}}%*{{else}}💯{{/isnt}}", Description: "Bold IV% or 💯 (Telegram)", Category: "pokemon"},
	{Label: "Pokemon with gender", Insert: "{{fullName}} {{genderEmoji}}", Description: "Name + gender emoji", Category: "pokemon"},
	{Label: "IV line", Insert: "{{atk}}/{{def}}/{{sta}}", Description: "Individual IVs (15/15/15)", Category: "pokemon"},
	{Label: "CP and level", Insert: "CP{{cp}} L{{level}}", Description: "CP and level", Category: "pokemon"},
	{Label: "Time remaining", Insert: "{{tthh}}h {{tthm}}m {{tths}}s", Description: "Time to hide", Category: "pokemon"},
	{Label: "Despawn time", Insert: "{{time}} ({{tthm}}m{{tths}}s)", Description: "Despawn time with TTH", Category: "pokemon"},
	{Label: "Weather boosted", Insert: "{{#if boosted}}{{boostWeatherEmoji}}{{/if}}", Description: "Show boost emoji if boosted", Category: "pokemon"},
	{Label: "Weather change", Insert: "{{weatherChange}}", Description: "Weather forecast text (empty if no change)", Category: "pokemon"},
	{Label: "Shiny possible", Insert: "{{#if shinyPossible}}✨{{/if}}", Description: "Sparkle if shiny possible", Category: "pokemon"},
	{Label: "Moves", Insert: "{{quickMoveName}} / {{chargeMoveName}}", Description: "Fast and charged move names", Category: "pokemon"},
	{Label: "PVP Great League", Insert: "{{#each pvpGreat}}#{{rank}} {{fullName}} CP{{cp}} L{{levelWithCap}} {{percentage}}%\n{{/each}}", Description: "Great League PVP rankings", Category: "pvp"},
	{Label: "PVP Ultra League", Insert: "{{#each pvpUltra}}#{{rank}} {{fullName}} CP{{cp}} L{{levelWithCap}} {{percentage}}%\n{{/each}}", Description: "Ultra League PVP rankings", Category: "pvp"},
	{Label: "Power-up cost", Insert: "{{#getPowerUpCost level 50}}{{addCommas stardust}} dust, {{candy}} candy{{/getPowerUpCost}}", Description: "Power-up cost to level 50", Category: "pokemon"},
	{Label: "Power-up cost inline", Insert: "{{getPowerUpCost level 50}}", Description: "Power-up cost as text", Category: "pokemon"},
	{Label: "Size filter", Insert: "{{#if size}}{{#or (lte size 1) (gte size 5)}}📐 {{sizeName}}{{/or}}{{/if}}", Description: "Show size for XXS/XXL only", Category: "pokemon"},
}

var raidSnippets = []Snippet{
	{Label: "Raid boss line", Insert: "⭐{{levelName}} {{fullName}}", Description: "Level name + boss name", Category: "raid"},
	{Label: "Gym line", Insert: "📍 {{gymName}}", Description: "Gym name with pin", Category: "raid"},
	{Label: "EX eligible", Insert: "{{#if ex}}🎟 EX{{/if}}", Description: "Show EX badge if eligible", Category: "raid"},
	{Label: "Time remaining", Insert: "{{time}} ({{tthm}}m)", Description: "End time with TTH", Category: "raid"},
}

var eggSnippets = []Snippet{
	{Label: "Egg line", Insert: "🥚 L{{level}} egg", Description: "Egg level", Category: "egg"},
	{Label: "Hatch time", Insert: "{{time}} ({{tthm}}m)", Description: "Hatch time with TTH", Category: "egg"},
}

var questSnippets = []Snippet{
	{Label: "Quest line", Insert: "{{questString}} → {{rewardString}}", Description: "Quest task and reward", Category: "quest"},
	{Label: "Pokestop", Insert: "📍 {{pokestopName}}", Description: "Pokestop name with pin", Category: "quest"},
}

var invasionSnippets = []Snippet{
	{Label: "Grunt line", Insert: "{{gruntTypeEmoji}} {{gruntName}}", Description: "Grunt type emoji + name", Category: "invasion"},
	{Label: "Time remaining", Insert: "{{time}} ({{tthm}}m)", Description: "End time with TTH", Category: "invasion"},
}

type fieldEntry struct {
	Fields      []FieldDef
	BlockScopes []BlockScope
	Snippets    []Snippet
}

var fieldsByType = map[string]fieldEntry{
	"monster":      {Fields: append(commonFields, monsterFields...), BlockScopes: monsterBlockScopes, Snippets: append(commonSnippets, monsterSnippets...)},
	"monsterNoIv":  {Fields: append(commonFields, monsterFields...), BlockScopes: monsterBlockScopes, Snippets: append(commonSnippets, monsterSnippets...)},
	"raid":         {Fields: append(commonFields, raidFields...), BlockScopes: raidBlockScopes, Snippets: append(commonSnippets, raidSnippets...)},
	"egg":          {Fields: append(commonFields, eggFields...), BlockScopes: eggBlockScopes, Snippets: append(commonSnippets, eggSnippets...)},
	"quest":        {Fields: append(commonFields, questFields...), Snippets: append(commonSnippets, questSnippets...)},
	"invasion":     {Fields: append(commonFields, invasionFields...), Snippets: append(commonSnippets, invasionSnippets...)},
	"lure":         {Fields: append(commonFields, lureFields...), Snippets: commonSnippets},
	"nest":         {Fields: append(commonFields, nestFields...), Snippets: commonSnippets},
	"gym":          {Fields: append(commonFields, gymFields...), Snippets: commonSnippets},
	"fort-update":  {Fields: append(commonFields, fortUpdateFields...), Snippets: commonSnippets},
	"maxbattle":    {Fields: append(commonFields, maxbattleFields...), Snippets: commonSnippets},
	"weatherchange": {Fields: append(commonFields, weatherChangeFields...), Snippets: commonSnippets},
	"greeting":     {Fields: append(commonFields, greetingFields...), Snippets: commonSnippets},
}

// HandleDTSFields returns available template fields for a DTS type.
// GET /api/dts/fields/:type
func HandleDTSFields() gin.HandlerFunc {
	return func(c *gin.Context) {
		typeName := c.Param("type")

		entry, ok := fieldsByType[typeName]
		if !ok {
			// Return just common fields for unknown types
			c.JSON(http.StatusOK, gin.H{
				"status": "ok",
				"type":   typeName,
				"fields": commonFields,
			})
			return
		}

		resp := gin.H{
			"status": "ok",
			"type":   typeName,
			"fields": entry.Fields,
		}
		if len(entry.BlockScopes) > 0 {
			resp["blockScopes"] = entry.BlockScopes
		}
		if len(entry.Snippets) > 0 {
			resp["snippets"] = entry.Snippets
		}
		c.JSON(http.StatusOK, resp)
	}
}

// HandleDTSFieldTypes returns the list of available DTS types.
// GET /api/dts/fields
func HandleDTSFieldTypes() gin.HandlerFunc {
	return func(c *gin.Context) {
		types := make([]string, 0, len(fieldsByType))
		for t := range fieldsByType {
			types = append(types, t)
		}
		c.JSON(http.StatusOK, gin.H{
			"status": "ok",
			"types":  types,
		})
	}
}
