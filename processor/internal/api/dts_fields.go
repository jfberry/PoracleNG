package api

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

// commonFields resolve for every alert type: location + reverse geocoding,
// matched areas, icons/static map, and the render-time computed fields.
// Groups that only some alert types produce live in mapLinkFields,
// tthFields and sunFields and are composed per type in fieldsByType.
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
	{Name: "intersection", Type: "string", Description: "Nearest street intersection (\"Street1 & Street2\") from GeoNames; empty when disabled or none nearby", Category: "location"},
	{Name: "areas", Type: "string", Description: "Comma-separated matched areas", Category: "location"},
	{Name: "matched", Type: "array", Description: "Matched area names, lowercased (for {{#each matched}}{{map 'arealist' this}}{{/each}})", Category: "location"},
	{Name: "userDistanceTrack", Type: "bool", Description: "True when the matched rule was distance-based (e.g. d:500) vs area-based", Category: "location"},
	// Icons / static map
	{Name: "staticMap", Type: "string", Description: "Static map image URL", Category: "maps", Preferred: true},
	{Name: "staticmap", Type: "string", Description: "Deprecated alias for staticMap", Category: "maps", Deprecated: true, PreferredAlternative: "staticMap"},
	{Name: "imgUrl", Type: "string", Description: "Primary icon URL", Category: "maps", Preferred: true},
	{Name: "imgUrlAlt", Type: "string", Description: "Alternative icon URL", Category: "maps"},
	{Name: "stickerUrl", Type: "string", Description: "Sticker icon URL", Category: "maps"},
	// Time
	{Name: "now", Type: "string", Description: "Current date/time (RFC3339, UTC)", Category: "time"},
	{Name: "nowISO", Type: "string", Description: "Current date/time (ISO 8601 with milliseconds, UTC)", Category: "time"},
}

// mapLinkFields are the map deep links. Every alert type except weatherchange
// (a weather cell has no single entity to link to).
var mapLinkFields = []FieldDef{
	{Name: "googleMapUrl", Type: "string", Description: "Google Maps link", Category: "maps", Preferred: true},
	{Name: "appleMapUrl", Type: "string", Description: "Apple Maps link", Category: "maps", Preferred: true},
	{Name: "wazeMapUrl", Type: "string", Description: "Waze link", Category: "maps"},
	{Name: "rdmUrl", Type: "string", Description: "RDM map link", Category: "maps"},
	{Name: "reactMapUrl", Type: "string", Description: "ReactMap link", Category: "maps"},
	{Name: "diademUrl", Type: "string", Description: "Diadem map link", Category: "maps"},
	{Name: "rocketMadUrl", Type: "string", Description: "RocketMad link", Category: "maps"},
	{Name: "mapurl", Type: "string", Description: "Deprecated alias for googleMapUrl", Category: "maps", Deprecated: true, PreferredAlternative: "googleMapUrl"},
	{Name: "applemap", Type: "string", Description: "Deprecated alias for appleMapUrl", Category: "maps", Deprecated: true, PreferredAlternative: "appleMapUrl"},
}

// tthFields are the time-remaining components. Every alert type except
// weatherchange (see weatherTth) and fort-update (no expiry).
var tthFields = []FieldDef{
	{Name: "tthd", Type: "int", Description: "Days remaining", Category: "time"},
	{Name: "tthh", Type: "int", Description: "Hours remaining", Category: "time", Preferred: true},
	{Name: "tthm", Type: "int", Description: "Minutes remaining", Category: "time", Preferred: true},
	{Name: "tths", Type: "int", Description: "Seconds remaining", Category: "time", Preferred: true},
}

// sunFields are the time-of-day flags at the alert location. Every alert type
// except quest and nest.
var sunFields = []FieldDef{
	{Name: "nightTime", Type: "bool", Description: "It is night at the alert location", Category: "time"},
	{Name: "dawnTime", Type: "bool", Description: "It is dawn at the alert location", Category: "time"},
	{Name: "duskTime", Type: "bool", Description: "It is dusk at the alert location", Category: "time"},
}

// alertFields composes the common groups for a regular alert type.
func alertFields(mapLinks, tth, sun bool, typeFields ...[]FieldDef) []FieldDef {
	out := append([]FieldDef{}, commonFields...)
	if mapLinks {
		out = append(out, mapLinkFields...)
	}
	if tth {
		out = append(out, tthFields...)
	}
	if sun {
		out = append(out, sunFields...)
	}
	for _, f := range typeFields {
		out = append(out, f...)
	}
	return out
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
	// Per-user location (pokemon alerts only — other types have no per-user layer)
	{Name: "distance", Type: "int", Description: "Distance from the user's location to the pokemon, in metres", Category: "location"},
	{Name: "bearing", Type: "int", Description: "Bearing from the user's location, in degrees", Category: "location"},
	{Name: "bearingEmoji", Type: "string", Description: "Directional arrow emoji for bearing", Category: "location"},
	{Name: "userTrackDistance", Type: "int", Description: "The matched rule's distance threshold in metres (0 = area-based rule); largest across matching rules", Category: "location"},
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
	{Name: "despawnTimestamp", Type: "int", Description: "Unix despawn timestamp (for Discord <t:N:R>)", Category: "time"},
	{Name: "tthSeconds", Type: "int", Description: "Total seconds remaining", Category: "time"},
	{Name: "distime", Type: "string", Description: "Deprecated alias for disappearTime", Category: "time", Deprecated: true, PreferredAlternative: "disappearTime"},
	// Types
	{Name: "typeName", Type: "string", Description: "Comma-joined translated type names (e.g. \"Fire, Flying\")", Category: "types", Preferred: true},
	{Name: "typeNameEng", Type: "array", Description: "Array of English type names", Category: "types"},
	{Name: "typeEmojiKeys", Type: "array", Description: "Array of type emoji keys (raw)", Category: "types"},
	{Name: "typeEmoji", Type: "string", Description: "Resolved type emoji, concatenated", Category: "types", Preferred: true},
	{Name: "emoji", Type: "array", Description: "Resolved type emoji as an array (iterate with {{#each emoji}})", Category: "types"},
	{Name: "color", Type: "string", Description: "Primary type color hex", Category: "types", Preferred: true},
	// Weather
	{Name: "boostWeatherEmoji", Type: "string", Description: "Boost weather emoji", Category: "weather", Preferred: true},
	{Name: "boostWeatherName", Type: "string", Description: "Translated boost weather", Category: "weather"},
	{Name: "boosted", Type: "bool", Description: "Is weather boosted", Category: "weather"},
	{Name: "boostingWeathersEmoji", Type: "string", Description: "Concatenated emoji string for every weather that boosts this pokemon's types (e.g. \"☀️💨\")", Category: "weather"},
	{Name: "gameWeatherName", Type: "string", Description: "Current game weather name", Category: "weather"},
	{Name: "gameWeatherEmoji", Type: "string", Description: "Current game weather emoji", Category: "weather"},
	{Name: "weatherChange", Type: "string", Description: "Weather forecast text", Category: "weather"},
	{Name: "weatherCurrentName", Type: "string", Description: "Current weather name", Category: "weather"},
	{Name: "weatherNextName", Type: "string", Description: "Next hour weather name", Category: "weather"},
	// PVP
	{Name: "pvpGreat", Type: "array", Description: "Great League PVP display list", Category: "pvp", Preferred: true},
	{Name: "pvpUltra", Type: "array", Description: "Ultra League PVP display list", Category: "pvp", Preferred: true},
	{Name: "pvpLittle", Type: "array", Description: "Little League PVP display list", Category: "pvp"},
	{Name: "pvpGreatBest", Type: "object", Description: "Best entry from pvpGreat (rank, list, name)", Category: "pvp"},
	{Name: "pvpUltraBest", Type: "object", Description: "Best entry from pvpUltra", Category: "pvp"},
	{Name: "pvpLittleBest", Type: "object", Description: "Best entry from pvpLittle", Category: "pvp"},
	{Name: "pvpAvailable", Type: "bool", Description: "True when any PVP data is available for this pokemon", Category: "pvp"},
	{Name: "personalShiny", Type: "bool", Description: "Shiny for at least one of the recipient's linked game accounts (per-user; needs [shiny_rates])", Category: "other"},
	{Name: "personalShinyAccounts", Type: "array", Description: "Labels of the recipient's accounts the pokemon is shiny for; unlabelled accounts appear as …<last 4 chars of the player ID>", Category: "other"},
	{Name: "personalBackground", Type: "bool", Description: "Has the special (event) background for at least one of the recipient's linked accounts", Category: "other"},
	{Name: "personalBackgroundAccounts", Type: "array", Description: "Labels of the recipient's accounts the pokemon has the special background for", Category: "other"},
	{Name: "hasLinkedAccounts", Type: "bool", Description: "The recipient has at least one linked game account", Category: "other"},
	{Name: "pvpUserRanking", Type: "int", Description: "The matched rule's worst-rank threshold (0 when the rule was not PVP-based)", Category: "pvp"},
	{Name: "userHasPvpTracks", Type: "bool", Description: "True when at least one matched rule was a real PVP tracking rule", Category: "pvp"},
	// Other
	{Name: "generation", Type: "int", Description: "Generation number", Category: "other"},
	{Name: "generationName", Type: "string", Description: "Generation name", Category: "other"},
	{Name: "genderName", Type: "string", Description: "Gender name", Category: "other"},
	{Name: "genderEmoji", Type: "string", Description: "Gender emoji", Category: "other"},
	{Name: "sizeName", Type: "string", Description: "Size category name", Category: "other"},
	{Name: "rarityName", Type: "string", Description: "Rarity group name", Category: "other"},
	{Name: "costume", Type: "int", Description: "Costume ID", Category: "other"},
	{Name: "costumeName", Type: "string", Description: "Translated costume name (empty when no costume). Note: fullName already includes it parenthesised.", Category: "other"},
	{Name: "shinyPossible", Type: "bool", Description: "Can be shiny", Category: "other"},
	{Name: "shinyRate", Type: "int", Description: "Expected shiny rate as 1-in-N from the [shiny_rates] feed at this location (absent without the feed or for unlisted variants)", Category: "other"},
	{Name: "shinyRateBoosted", Type: "bool", Description: "The feed's shiny rate here is above the 1/512 base (an event)", Category: "other"},
	{Name: "weaknessList", Type: "array", Description: "Type weakness list", Category: "other"},
	{Name: "weaknessEmoji", Type: "string", Description: "Flat string of all weakness multipliers + type emoji (e.g. \"2x💧⚡ 4x🪨 \").", Category: "other"},
	{Name: "evolutions", Type: "array", Description: "Evolution entries", Category: "other"},
	{Name: "megaEvolutions", Type: "array", Description: "Mega evolution entries", Category: "other"},
	{Name: "hasEvolutions", Type: "bool", Description: "Has evolutions", Category: "other"},
	{Name: "hasMegaEvolutions", Type: "bool", Description: "A mega evolution exists anywhere in the evolution chain (e.g. Electrike → Mega Manectric)", Category: "other"},
	{Name: "pokestopName", Type: "string", Description: "Nearby pokestop name", Category: "other"},
}

var raidFields = []FieldDef{
	// Identity
	{Name: "name", Type: "string", Description: "Translated pokemon name", Category: "identity", Preferred: true},
	{Name: "fullName", Type: "string", Description: "Name + form + costume (costume parenthesised)", Category: "identity", Preferred: true},
	{Name: "megaName", Type: "string", Description: "Full display name incl. mega evolution + form + costume (equals fullName)", Category: "identity", Preferred: true},
	{Name: "formName", Type: "string", Description: "Translated form name", Category: "identity"},
	{Name: "costumeName", Type: "string", Description: "Translated costume name (empty when none); fullName/megaName already include it parenthesised", Category: "identity"},
	{Name: "nameEng", Type: "string", Description: "English pokemon name", Category: "identity"},
	{Name: "fullNameEng", Type: "string", Description: "English name + form + costume", Category: "identity"},
	{Name: "pokemonId", Type: "int", Description: "Pokemon ID", Category: "identity"},
	{Name: "formId", Type: "int", Description: "Form ID", Category: "identity"},
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
	{Name: "campfireUrl", Type: "string", Description: "Niantic Campfire deep link for this gym", Category: "maps"},
	// Stats
	{Name: "cp", Type: "int", Description: "Boss CP (webhook)", Category: "stats", RawWebhook: true},
	// Moves
	{Name: "quickMoveName", Type: "string", Description: "Translated fast move", Category: "moves", Preferred: true},
	{Name: "chargeMoveName", Type: "string", Description: "Translated charged move", Category: "moves", Preferred: true},
	{Name: "quickMoveEmoji", Type: "string", Description: "Fast move type emoji", Category: "moves"},
	{Name: "chargeMoveEmoji", Type: "string", Description: "Charged move type emoji", Category: "moves"},
	// Time
	{Name: "time", Type: "string", Description: "End time", Category: "time", Preferred: true},
	{Name: "hatchTime", Type: "string", Description: "Hatch/start time", Category: "time"},
	{Name: "endTimestamp", Type: "int", Description: "Unix raid end timestamp (for Discord <t:N:R>)", Category: "time"},
	{Name: "hatchTimestamp", Type: "int", Description: "Unix hatch timestamp (for Discord <t:N:R>)", Category: "time"},
	// Types
	{Name: "typeName", Type: "string", Description: "Comma-joined translated type names", Category: "types"},
	{Name: "typeNameEng", Type: "array", Description: "English type names", Category: "types"},
	{Name: "typeEmoji", Type: "string", Description: "Resolved type emoji, concatenated", Category: "types"},
	{Name: "emoji", Type: "array", Description: "Resolved type emoji as an array", Category: "types"},
	{Name: "color", Type: "string", Description: "Gym team color hex (deprecated alias for gymColor — not the boss type color)", Category: "gym"},
	// Weather
	{Name: "boostWeatherEmoji", Type: "string", Description: "Boost weather emoji", Category: "weather"},
	{Name: "boostWeatherName", Type: "string", Description: "Translated boost weather", Category: "weather"},
	{Name: "boostingWeathersEmoji", Type: "string", Description: "Concatenated emoji string for every weather that boosts the raid boss's types", Category: "weather"},
	// Other
	{Name: "shinyPossible", Type: "bool", Description: "Can be shiny", Category: "other"},
	{Name: "shinyRate", Type: "int", Description: "Expected shiny rate as 1-in-N from the [shiny_rates] feed at this location (absent without the feed or for unlisted variants)", Category: "other"},
	{Name: "shinyRateBoosted", Type: "bool", Description: "The feed's shiny rate here is above the 1/512 base (an event)", Category: "other"},
	{Name: "evolutions", Type: "array", Description: "Evolution entries", Category: "other"},
	{Name: "megaEvolutions", Type: "array", Description: "Mega evolution entries", Category: "other"},
	{Name: "hasEvolutions", Type: "bool", Description: "Has evolutions", Category: "other"},
	{Name: "hasMegaEvolutions", Type: "bool", Description: "A mega evolution exists anywhere in the evolution chain (e.g. Electrike → Mega Manectric)", Category: "other"},
	{Name: "weaknessList", Type: "array", Description: "Type weakness list", Category: "other"},
	{Name: "weaknessEmoji", Type: "string", Description: "Flat string of all weakness multipliers + type emoji (e.g. \"2x💧⚡ 4x🪨 \").", Category: "other"},
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
	{Name: "campfireUrl", Type: "string", Description: "Niantic Campfire deep link for this gym", Category: "maps"},
	{Name: "time", Type: "string", Description: "Hatch time", Category: "time", Preferred: true},
	{Name: "hatchTime", Type: "string", Description: "Hatch time", Category: "time"},
	{Name: "hatchTimestamp", Type: "int", Description: "Unix hatch timestamp (for Discord <t:N:R>)", Category: "time"},
	{Name: "endTimestamp", Type: "int", Description: "Unix raid end timestamp (for Discord <t:N:R>)", Category: "time"},
	{Name: "rsvps", Type: "array", Description: "RSVP timeslot entries", Category: "other"},
}

var questFields = []FieldDef{
	{Name: "pokestopName", Type: "string", Description: "Pokestop name", Category: "location", Preferred: true},
	{Name: "questString", Type: "string", Description: "Quest description", Category: "quest", Preferred: true},
	{Name: "rewardString", Type: "string", Description: "Reward description", Category: "quest", Preferred: true},
	{Name: "dustAmount", Type: "int", Description: "Stardust amount", Category: "quest"},
	{Name: "pokecoinAmount", Type: "int", Description: "Pokecoin amount", Category: "quest"},
	{Name: "itemAmount", Type: "int", Description: "Item amount (first item reward)", Category: "quest"},
	{Name: "itemNames", Type: "string", Description: "Comma-joined translated item rewards, with amount when > 1 (e.g. \"3 Razz Berry\")", Category: "quest"},
	{Name: "monsterNames", Type: "string", Description: "Comma-joined translated reward pokemon full names (name + form)", Category: "quest"},
	{Name: "energyAmount", Type: "int", Description: "Mega energy amount (first energy reward)", Category: "quest"},
	{Name: "energyMonstersNames", Type: "string", Description: "Comma-joined mega energy rewards (e.g. \"50 Charizard Mega Energy\")", Category: "quest"},
	{Name: "candyAmount", Type: "int", Description: "Candy amount (first candy reward)", Category: "quest"},
	{Name: "candyMonstersNames", Type: "string", Description: "Comma-joined candy rewards (e.g. \"3 Lapras Candy\")", Category: "quest"},
	{Name: "isShiny", Type: "bool", Description: "Reward pokemon is shiny", Category: "quest"},
	{Name: "conditionString", Type: "string", Description: "Quest completion conditions, joined (e.g. 'Excellent Throw, Curve Ball')", Category: "quest"},
	{Name: "conditionStringEng", Type: "string", Description: "English copy of conditionString", Category: "quest"},
	{Name: "conditionList", Type: "array", Description: "Quest conditions as objects with type/name/formatted", Category: "quest"},
	{Name: "conditionListEng", Type: "array", Description: "English copy of conditionList", Category: "quest"},
}

var invasionFields = []FieldDef{
	{Name: "pokestopName", Type: "string", Description: "Pokestop name", Category: "location", Preferred: true},
	{Name: "gruntName", Type: "string", Description: "Grunt name", Category: "invasion", Preferred: true},
	{Name: "gruntTypeName", Type: "string", Description: "Grunt type name (translated)", Category: "invasion", Preferred: true},
	{Name: "gruntType", Type: "string", Description: "English type name for grunts (e.g. \"Fire\"), lowercase event name for events (e.g. \"kecleon\"). Use for {{#if (eq gruntType 'kecleon')}} dispatch.", Category: "invasion"},
	{Name: "gruntTypeEmoji", Type: "string", Description: "Grunt type emoji", Category: "invasion"},
	{Name: "gruntTypeColor", Type: "string", Description: "Grunt type color hex", Category: "invasion"},
	{Name: "gruntTypeId", Type: "int", Description: "Grunt type ID", Category: "invasion"},
	{Name: "displayTypeId", Type: "int", Description: "Display type ID", Category: "invasion"},
	{Name: "genderName", Type: "string", Description: "Grunt gender name", Category: "invasion"},
	{Name: "genderEmoji", Type: "string", Description: "Grunt gender emoji", Category: "invasion"},
	{Name: "gruntRewardsList", Type: "object", Description: "Reward pokemon by slot: {first: {chance, monsters}, second: {chance, monsters}}", Category: "invasion"},
	{Name: "gruntLineupList", Type: "object", Description: "Confirmed lineup (only when the webhook carries one): {confirmed, monsters: [{id, formId, name, formName, fullName}]}", Category: "invasion"},
	{Name: "time", Type: "string", Description: "End time", Category: "time", Preferred: true},
	{Name: "expirationTimestamp", Type: "int", Description: "Unix expiry timestamp (for Discord <t:N:R>)", Category: "time"},
}

// incidentFields is the trimmed surface for the "incident" template type
// (Gold Pokestop, Kecleon, Showcase, …). Grunt/reward/gender fields are
// absent — incidents don't have grunts. Three aliased fields are added for
// convenience: incidentType, incidentEmoji, color.
// incidentFields are the pokestop-identity / time / display-type fields shared
// by the "incident" and "showcase" template types (both resolve them).
var incidentFields = []FieldDef{
	{Name: "pokestopName", Type: "string", Description: "Pokestop name", Category: "location", Preferred: true},
	{Name: "pokestopUrl", Type: "string", Description: "Pokestop image URL", Category: "location"},
	{Name: "pokestopId", Type: "string", Description: "Pokestop ID", Category: "location"},
	{Name: "displayType", Type: "int", Description: "Display type ID — numeric event identifier (e.g. 7=Gold-Stop, 8=Kecleon, 9=Showcase). Use for dispatch logic: {{#if (eq displayType 8)}}.", Category: "incident", Preferred: true},
	{Name: "displayTypeId", Type: "int", Description: "Display type ID (raw enrichment field — prefer the displayType alias).", Category: "incident"},
	{Name: "disappearTime", Type: "string", Description: "Incident expiry time", Category: "time", Preferred: true},
	{Name: "time", Type: "string", Description: "Expiry time (alias for disappearTime)", Category: "time", Preferred: true},
	{Name: "expirationTimestamp", Type: "int", Description: "Unix expiry timestamp (for Discord <t:N:R>)", Category: "time"},
}

// incidentOnlyFields resolve only for the "incident" template type (Gold-Stop /
// Kecleon). Showcases hardcode their title and don't alias these, so they are
// excluded from the showcase field list.
var incidentOnlyFields = []FieldDef{
	{Name: "incidentTypeName", Type: "string", Description: "Translated display-type label (e.g. \"Gold Pokéstop\", \"Kecleon\"). Alias for gruntName.", Category: "incident", Preferred: true},
	{Name: "incidentEmoji", Type: "string", Description: "Resolved per-platform emoji for the event icon. Alias for gruntTypeEmoji.", Category: "incident", Preferred: true},
	{Name: "color", Type: "string", Description: "Event color hex for the embed. Alias for gruntTypeColor.", Category: "incident", Preferred: true},
}

// showcaseFields is the extra surface for the dedicated "showcase" template type
// (a specialised incident display: focus + leaderboard). The showcase type also
// includes all incidentFields (pokestop identity, time, maps).
var showcaseFields = []FieldDef{
	{Name: "showcasePresent", Type: "bool", Description: "True when showcase_rankings data is present. Guard leaderboard blocks with {{#if showcasePresent}}.", Category: "showcase", Preferred: true},
	{Name: "showcaseTotalEntries", Type: "int", Description: "Total number of contestants in the Showcase.", Category: "showcase", Preferred: true},
	{Name: "showcaseLastUpdate", Type: "int", Description: "Unix timestamp of the last leaderboard update.", Category: "showcase"},
	{Name: "showcaseLastUpdateFormatted", Type: "string", Description: "Formatted last-update time using the operator's configured time layout.", Category: "showcase"},
	{Name: "showcase", Type: "array", Description: "Array of up to 3 enriched contestant entries — see Showcase fields in DTS.md for per-entry field list.", Category: "showcase", Preferred: true},
	{Name: "showcaseFirst", Type: "object", Description: "Convenience alias for showcase[0] (the winner). nil when no contestants.", Category: "showcase", Preferred: true},
	{Name: "showcaseFocusPresent", Type: "bool", Description: "True when the Showcase's featured focus was decoded. Guard focus blocks with {{#if showcaseFocusPresent}}.", Category: "showcase"},
	{Name: "showcaseFocusType", Type: "string", Description: "Raw focus class: pokemon, type, alignment, class, family, buddy, generation, hatched, mega, shiny.", Category: "showcase"},
	{Name: "showcaseFocusCategory", Type: "string", Description: "Translated focus category label (e.g. \"Type\", \"Buddy\").", Category: "showcase", Preferred: true},
	{Name: "showcaseFocusName", Type: "string", Description: "Translated featured value (e.g. \"Steel\" for a type focus, \"3+\" for a buddy focus). Empty for flag focuses (hatched/shiny/mega).", Category: "showcase", Preferred: true},
	{Name: "showcaseFocusEmoji", Type: "string", Description: "Emoji for the focus category (emoji key showcase-focus-<type>, overridable in emoji.json).", Category: "showcase"},
}

var incidentSnippets = []Snippet{
	{Label: "Incident title line", Insert: "{{{incidentEmoji}}} {{incidentTypeName}} at {{{pokestopName}}}", Description: "Event emoji + label + pokestop name", Category: "incident"},
	{Label: "Time remaining", Insert: "{{disappearTime}} ({{#if tthh}}{{tthh}}h {{/if}}{{tthm}}m {{tths}}s)", Description: "Expiry time with TTH", Category: "incident"},
	{Label: "Countdown", Insert: "<t:{{expirationTimestamp}}:R>", Description: "Discord relative countdown", Category: "incident", Platform: "discord"},
	{Label: "Kecleon dispatch (numeric)", Insert: "{{#if (eq displayType 8)}}🦎{{else}}✨{{/if}}", Description: "Branch on specific incident type using numeric displayType", Category: "incident"},
	{Label: "Kecleon dispatch (slug)", Insert: "{{#if (eq gruntType \"kecleon\")}}🦎{{else}}✨{{/if}}", Description: "Branch on specific incident type using gruntType slug", Category: "incident"},
}

// showcaseSnippets are the showcase-only building blocks (the showcase type
// also inherits incidentSnippets for the title/time lines).
var showcaseSnippets = []Snippet{
	{Label: "Showcase focus", Insert: "{{#if showcaseFocusPresent}}🎯 Featuring {{showcaseFocusCategory}}{{#if showcaseFocusName}}: {{showcaseFocusName}}{{/if}}{{/if}}", Description: "What the contest is featuring (e.g. \"Type: Steel\")", Category: "showcase"},
	{Label: "Showcase leaderboard", Insert: "{{#if showcasePresent}}\n🏆 Top contestants ({{showcaseTotalEntries}} entries):\n{{#each showcase}}{{rank}}. {{fullName}}{{#if shiny}} ✨{{/if}}{{#if costumeName}} ({{costumeName}}){{/if}} — {{scoreFormatted}}\n{{/each}}{{/if}}", Description: "Full leaderboard block", Category: "showcase"},
	{Label: "Showcase winner only", Insert: "{{#if showcasePresent}}🏆 {{showcaseFirst.fullName}} ({{showcaseFirst.scoreFormatted}}){{/if}}", Description: "Single-line winner entry", Category: "showcase"},
}

var lureFields = []FieldDef{
	{Name: "pokestopName", Type: "string", Description: "Pokestop name", Category: "location", Preferred: true},
	{Name: "lureTypeId", Type: "int", Description: "Lure type ID", Category: "lure", Preferred: true},
	{Name: "lureTypeName", Type: "string", Description: "Translated lure type name", Category: "lure", Preferred: true},
	{Name: "lureTypeEmoji", Type: "string", Description: "Lure type emoji", Category: "lure"},
	{Name: "lureTypeColor", Type: "string", Description: "Lure type color hex", Category: "lure"},
	{Name: "time", Type: "string", Description: "Expiry time", Category: "time", Preferred: true},
	{Name: "expirationTimestamp", Type: "int", Description: "Unix expiry timestamp (for Discord <t:N:R>)", Category: "time"},
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
	{Name: "formId", Type: "int", Description: "Form ID", Category: "identity"},
	{Name: "pokemonCount", Type: "int", Description: "Pokemon count", Category: "stats"},
	{Name: "pokemonSpawnAvg", Type: "number", Description: "Avg spawns per hour", Category: "stats"},
	{Name: "pokemon_ratio", Type: "number", Description: "Pokemon spawn ratio (webhook)", Category: "stats", RawWebhook: true},
	{Name: "shinyPossible", Type: "bool", Description: "Can be shiny", Category: "other"},
	{Name: "shinyRate", Type: "int", Description: "Expected shiny rate as 1-in-N from the [shiny_rates] feed at this location (absent without the feed or for unlisted variants)", Category: "other"},
	{Name: "shinyRateBoosted", Type: "bool", Description: "The feed's shiny rate here is above the 1/512 base (an event)", Category: "other"},
	{Name: "typeName", Type: "string", Description: "Comma-joined translated type names", Category: "types"},
	{Name: "typeEmoji", Type: "string", Description: "Resolved type emoji, concatenated", Category: "types"},
	{Name: "emoji", Type: "array", Description: "Resolved type emoji as an array", Category: "types"},
	{Name: "color", Type: "string", Description: "Primary type color hex", Category: "types"},
}

var gymFields = []FieldDef{
	{Name: "gymName", Type: "string", Description: "Gym name", Category: "gym", Preferred: true},
	{Name: "gymUrl", Type: "string", Description: "Gym photo URL (Niantic CDN)", Category: "gym"},
	{Name: "teamName", Type: "string", Description: "Current team name", Category: "gym", Preferred: true},
	{Name: "teamEmoji", Type: "string", Description: "Team emoji", Category: "gym"},
	{Name: "gymColor", Type: "string", Description: "Team color hex", Category: "gym"},
	{Name: "oldTeamName", Type: "string", Description: "Previous team name", Category: "gym"},
	{Name: "oldTeamEmoji", Type: "string", Description: "Old team emoji (old_team_id)", Category: "gym"},
	{Name: "previousControlTeamEmoji", Type: "string", Description: "Previous controlling team's emoji (last non-zero team seen on this gym)", Category: "gym"},
	{Name: "slotsAvailable", Type: "int", Description: "Available gym slots", Category: "gym"},
	{Name: "oldSlotsAvailable", Type: "int", Description: "Previous slot count", Category: "gym"},
	{Name: "inBattle", Type: "bool", Description: "Is gym in battle", Category: "gym"},
	{Name: "ex", Type: "bool", Description: "EX raid eligible", Category: "gym"},
	{Name: "previousControlName", Type: "string", Description: "Previous controlling team name (translated)", Category: "gym"},
}

var fortUpdateFields = []FieldDef{
	{Name: "name", Type: "string", Description: "Fort name", Category: "identity", Preferred: true},
	{Name: "id", Type: "string", Description: "Fort ID", Category: "identity"},
	{Name: "fortType", Type: "string", Description: "pokestop or gym", Category: "identity", Preferred: true},
	{Name: "fortTypeText", Type: "string", Description: "\"Pokestop\" or \"Gym\"", Category: "identity"},
	{Name: "isNew", Type: "bool", Description: "A new fort (also set for an edit of a fort with no previous name/description)", Category: "change", Preferred: true},
	{Name: "isEdit", Type: "bool", Description: "An edit to an existing fort", Category: "change", Preferred: true},
	{Name: "isRemoval", Type: "bool", Description: "A removed fort", Category: "change", Preferred: true},
	{Name: "changeTypeText", Type: "string", Description: "\"New\", \"Edit\" or \"Removal\" (English, not translated)", Category: "change"},
	{Name: "change_type", Type: "string", Description: "new, edit, or removal (webhook)", Category: "change", RawWebhook: true},
	{Name: "edit_types", Type: "array", Description: "Edited properties, e.g. [\"name\", \"location\", \"image_url\"] (webhook)", Category: "change", RawWebhook: true},
	{Name: "isEditName", Type: "bool", Description: "Name was changed", Category: "change"},
	{Name: "isEditDescription", Type: "bool", Description: "Description was changed", Category: "change"},
	{Name: "isEditLocation", Type: "bool", Description: "Location was changed", Category: "change"},
	{Name: "isEditImageUrl", Type: "bool", Description: "Image was changed", Category: "change"},
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
	{Name: "fullName", Type: "string", Description: "Name + form + costume (costume parenthesised)", Category: "identity", Preferred: true},
	{Name: "megaName", Type: "string", Description: "Full display name incl. form + costume (equals fullName)", Category: "identity", Preferred: true},
	{Name: "costumeName", Type: "string", Description: "Translated costume name (empty when none); fullName/megaName already include it parenthesised", Category: "identity"},
	{Name: "pokemonId", Type: "int", Description: "Pokemon ID", Category: "identity"},
	{Name: "id", Type: "int", Description: "Pokemon ID (same as pokemonId; for the station use stationId)", Category: "identity"},
	{Name: "level", Type: "int", Description: "Battle level", Category: "identity", Preferred: true},
	{Name: "stationName", Type: "string", Description: "Station name", Category: "location", Preferred: true},
	{Name: "stationId", Type: "string", Description: "Station ID", Category: "location"},
	{Name: "time", Type: "string", Description: "End time", Category: "time", Preferred: true},
	{Name: "endTimestamp", Type: "int", Description: "Unix end timestamp (for Discord <t:N:R>)", Category: "time"},
	{Name: "quickMoveName", Type: "string", Description: "Translated fast move", Category: "moves"},
	{Name: "chargeMoveName", Type: "string", Description: "Translated charged move", Category: "moves"},
	{Name: "quickMoveEmoji", Type: "string", Description: "Fast move type emoji", Category: "moves"},
	{Name: "chargeMoveEmoji", Type: "string", Description: "Charged move type emoji", Category: "moves"},
	{Name: "typeName", Type: "string", Description: "Comma-joined translated type names", Category: "types"},
	{Name: "typeEmoji", Type: "string", Description: "Resolved type emoji, concatenated", Category: "types"},
	{Name: "emoji", Type: "array", Description: "Resolved type emoji as an array", Category: "types"},
	{Name: "color", Type: "string", Description: "Max battle embed color hex (always D000C0)", Category: "other"},
	{Name: "gmax", Type: "int", Description: "1 when Gigantamax, else 0 — test with {{#if gmax}}", Category: "other"},
	{Name: "total_stationed_pokemon", Type: "int", Description: "Total stationed pokemon", Category: "other"},
	{Name: "total_stationed_gmax", Type: "int", Description: "Total stationed Gmax", Category: "other"},
}

var weatherChangeFields = []FieldDef{
	{Name: "weatherName", Type: "string", Description: "New weather name", Category: "weather", Preferred: true},
	{Name: "oldWeatherName", Type: "string", Description: "Previous weather name", Category: "weather", Preferred: true},
	{Name: "weatherEmoji", Type: "string", Description: "New weather emoji", Category: "weather", Preferred: true},
	{Name: "oldWeatherEmoji", Type: "string", Description: "Previous weather emoji", Category: "weather"},
	{Name: "weatherEmojiKey", Type: "string", Description: "New weather emoji key", Category: "weather"},
	{Name: "oldWeatherEmojiKey", Type: "string", Description: "Previous weather emoji key", Category: "weather"},
	{Name: "gameplay_condition", Type: "int", Description: "New weather ID (webhook)", Category: "weather", RawWebhook: true},
	{Name: "old_gameplay_condition", Type: "int", Description: "Previous weather ID (webhook)", Category: "weather", RawWebhook: true},
	{Name: "s2_cell_id", Type: "string", Description: "S2 weather cell ID (webhook)", Category: "weather", RawWebhook: true},
	{Name: "source", Type: "string", Description: "How the change was detected: \"webhook\" or \"fromMonster\" (webhook)", Category: "weather", RawWebhook: true},
	{Name: "weatherTth", Type: "object", Description: "Time until the next hourly weather update: {days, hours, minutes, seconds} — e.g. {{weatherTth.minutes}}", Category: "time"},
	{Name: "enrichedActivePokemons", Type: "array", Description: "The user's tracked pokemon affected by the change: {pokemon_id, form, iv, cp, latitude, longitude, disappearTime (unix), name, nameEng, formName, formNormalised, fullName, fullNameEng, imgUrl}", Category: "pokemon"},
	{Name: "activePokemons", Type: "array", Description: "Same as enrichedActivePokemons, set only when [weather] show_altered_pokemon_static_map is enabled", Category: "pokemon"},
}

// greetingFields is the whole greeting view: the registration/greeting
// renderers (bot/commands/poracle.go, discordbot + telegrambot
// reconciliation) pass only {prefix} — no alert fields.
var greetingFields = []FieldDef{
	{Name: "prefix", Type: "string", Description: "Command prefix for the platform (e.g. \"!\" on Discord, \"/\" on Telegram)", Category: "other", Preferred: true},
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
	{Name: "pokemonId", Type: "int", Description: "Pokemon ID for this rank (same as pokemon)"},
	{Name: "matchesUserTrack", Type: "bool", Description: "True when this rank satisfies one of the user's PVP tracking rules"},
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
	{Name: "nameEng", Type: "string", Description: "English pokemon name"},
	{Name: "fullNameEng", Type: "string", Description: "English name + form"},
	{Name: "formNameEng", Type: "string", Description: "English form name"},
	{Name: "formNormalisedEng", Type: "string", Description: "English form name (empty for Normal)"},
	{Name: "typeName", Type: "string", Description: "Comma-joined translated type names"},
	{Name: "typeEmoji", Type: "string", Description: "Joined type emoji string for the platform"},
	{Name: "emoji", Type: "array", Description: "Type emoji strings for the platform"},
	{Name: "typeEmojiKeys", Type: "array", Description: "Array of type emoji keys"},
	{Name: "baseStats", Type: "object", Description: "{baseAttack, baseDefense, baseStamina}"},
	{Name: "evolutionRequirement", Type: "string", Description: "Translated evolution requirement (e.g. \"50 candies\")"},
}

var megaEvolutionEntryFields = []FieldDef{
	{Name: "evolution", Type: "int", Description: "Temp evolution ID"},
	{Name: "fullName", Type: "string", Description: "Translated mega name"},
	{Name: "fullNameEng", Type: "string", Description: "English mega name"},
	{Name: "typeName", Type: "string", Description: "Comma-joined translated type names"},
	{Name: "typeEmoji", Type: "string", Description: "Joined type emoji string for the platform"},
	{Name: "emoji", Type: "array", Description: "Type emoji strings for the platform"},
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
		Args:           []string{"emoji"},
		IterableFields: []string{"emoji", "typeNameEng"},
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
		Args:           []string{"emoji"},
		IterableFields: []string{"emoji", "typeNameEng"},
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
	Platform    string `json:"platform,omitempty"` // "discord", "telegram", or empty for all
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
	{Label: "oneOf", Insert: "{{#oneOf fieldName 1 5}}...{{/oneOf}}", Description: "Block fires when fieldName equals any of the following args. Use this instead of {{#or value 1 5}} which is \"any arg truthy\".", Category: "control"},
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
	{Label: "capitalizeAll", Insert: "{{capitalizeAll fieldName}}", Description: "Capitalise the first letter of every word", Category: "string"},
	{Label: "ellipsis", Insert: "{{ellipsis fieldName 20}}", Description: "First N characters, plus … when longer", Category: "string"},
	{Label: "remove", Insert: "{{remove fieldName 'text'}}", Description: "Remove all occurrences of text", Category: "string"},
	{Label: "startsWith", Insert: "{{#startsWith 'prefix' fieldName}}...{{else}}...{{/startsWith}}", Description: "Case-sensitive prefix test (also usable as a subexpression)", Category: "string"},
	// Custom maps (config/customMaps/)
	{Label: "map", Insert: "{{map 'mapName' fieldName}}", Description: "Custom map lookup; prints nothing when the key is missing", Category: "customMap"},
	{Label: "map (object value)", Insert: "{{#map 'mapName' fieldName}}{{property}}{{else}}...{{/map}}", Description: "Custom map lookup with an object value as the block context; else runs when the key is missing", Category: "customMap"},
	{Label: "map2 (fallback key)", Insert: "{{map2 'mapName' (concat pokemonId '_' formId) pokemonId}}", Description: "Custom map lookup trying a second key when the first is missing", Category: "customMap"},
	{Label: "if map has key", Insert: "{{#if (map 'mapName' fieldName)}}...{{/if}}", Description: "Test whether a custom map has the key", Category: "customMap"},
	// Iteration
	{Label: "each", Insert: "{{#each arrayField}}{{this}}{{/each}}", Description: "Iterate over an array", Category: "iteration"},
	{Label: "each with index", Insert: "{{#each arrayField}}{{@index}}: {{this}}{{/each}}", Description: "Iterate with index", Category: "iteration"},
	// Emoji
	{Label: "getEmoji", Insert: "{{getEmoji 'key'}}", Description: "Look up emoji by key", Category: "emoji"},
	// Links
	{Label: "Shortlink", Insert: "<S<{{url}}>S>", Description: "Wrap a URL for Shlink shortening", Category: "link"},
	// Maps
	{Label: "Static map image", Insert: "[\u200A]({{{staticMap}}})", Description: "Invisible-text image link for Telegram", Category: "link", Platform: "telegram"},
	{Label: "Google Maps link", Insert: "[Google]({{{googleMapUrl}}})", Description: "Clickable Google Maps link", Category: "link"},
	{Label: "Apple Maps link", Insert: "[Apple]({{{appleMapUrl}}})", Description: "Clickable Apple Maps link", Category: "link"},
}

var monsterSnippets = []Snippet{
	{Label: "Round IV", Insert: "{{round iv}}", Description: "IV rounded to integer", Category: "pokemon"},
	{Label: "IV or 💯", Insert: "{{#isnt iv 100}}{{round iv}}%{{else}}💯{{/isnt}}", Description: "Show IV% or 💯 for hundos", Category: "pokemon"},
	{Label: "IV with stars", Insert: "{{#isnt iv 100}}*{{round iv}}%*{{else}}💯{{/isnt}}", Description: "Bold IV% or 💯 (Telegram markdown)", Category: "pokemon", Platform: "telegram"},
	{Label: "Pokemon with gender", Insert: "{{fullName}} {{genderEmoji}}", Description: "Name + gender emoji", Category: "pokemon"},
	{Label: "IV line", Insert: "{{atk}}/{{def}}/{{sta}}", Description: "Individual IVs (15/15/15)", Category: "pokemon"},
	{Label: "CP and level", Insert: "CP{{cp}} L{{level}}", Description: "CP and level", Category: "pokemon"},
	{Label: "Time remaining", Insert: "{{tthh}}h {{tthm}}m {{tths}}s", Description: "Time to hide", Category: "pokemon"},
	{Label: "Despawn time", Insert: "{{time}} ({{tthm}}m{{tths}}s)", Description: "Despawn time with TTH", Category: "pokemon"},
	{Label: "Countdown", Insert: "<t:{{despawnTimestamp}}:R>", Description: "Discord relative countdown", Category: "pokemon", Platform: "discord"},
	{Label: "Weather boosted", Insert: "{{#if boosted}}{{boostWeatherEmoji}}{{/if}}", Description: "Show boost emoji if boosted", Category: "pokemon"},
	{Label: "Weather change", Insert: "{{weatherChange}}", Description: "Weather forecast text (empty if no change)", Category: "pokemon"},
	{Label: "Shiny for you", Insert: "{{#if personalShiny}}✨ Shiny for {{join personalShinyAccounts \", \"}}{{/if}}", Description: "Names the recipient's accounts the pokemon is shiny for", Category: "pokemon"},
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
	{Label: "Countdown", Insert: "<t:{{endTimestamp}}:R>", Description: "Discord relative countdown to raid end", Category: "raid", Platform: "discord"},
	{Label: "Campfire link", Insert: "[Campfire]({{{campfireUrl}}})", Description: "Niantic Campfire deep link", Category: "raid"},
	{Label: "RSVP list", Insert: "{{#if rsvps}}RSVPS:\n{{#each rsvps}}TS: <t:{{timeSlot}}:R> {{time}} Going: {{goingCount}} Maybe: {{maybeCount}}\n{{/each}}{{/if}}", Description: "RSVP timeslots with Discord countdown", Category: "raid"},
}

var eggSnippets = []Snippet{
	{Label: "Egg line", Insert: "🥚 L{{level}} egg", Description: "Egg level", Category: "egg"},
	{Label: "Hatch time", Insert: "{{time}} ({{tthm}}m)", Description: "Hatch time with TTH", Category: "egg"},
	{Label: "Countdown", Insert: "<t:{{hatchTimestamp}}:R>", Description: "Discord relative countdown to hatch", Category: "egg", Platform: "discord"},
	{Label: "RSVP list", Insert: "{{#if rsvps}}RSVPS:\n{{#each rsvps}}TS: <t:{{timeSlot}}:R> {{time}} Going: {{goingCount}} Maybe: {{maybeCount}}\n{{/each}}{{/if}}", Description: "RSVP timeslots with Discord countdown", Category: "egg"},
}

var questSnippets = []Snippet{
	{Label: "Quest line", Insert: "{{questString}} → {{rewardString}}", Description: "Quest task and reward", Category: "quest"},
	{Label: "Pokestop", Insert: "📍 {{pokestopName}}", Description: "Pokestop name with pin", Category: "quest"},
}

var invasionSnippets = []Snippet{
	{Label: "Grunt line", Insert: "{{gruntTypeEmoji}} {{gruntName}}", Description: "Grunt type emoji + name", Category: "invasion"},
	{Label: "Time remaining", Insert: "{{time}} ({{tthm}}m)", Description: "End time with TTH", Category: "invasion"},
	{Label: "Countdown", Insert: "<t:{{expirationTimestamp}}:R>", Description: "Discord relative countdown", Category: "invasion", Platform: "discord"},
}

var lureSnippets = []Snippet{
	{Label: "Lure type", Insert: "{{lureTypeEmoji}} {{lureTypeName}}", Description: "Lure type emoji + name", Category: "lure"},
	{Label: "Countdown", Insert: "<t:{{expirationTimestamp}}:R>", Description: "Discord relative countdown", Category: "lure", Platform: "discord"},
}

var maxbattleSnippets = []Snippet{
	{Label: "Countdown", Insert: "<t:{{endTimestamp}}:R>", Description: "Discord relative countdown", Category: "maxbattle", Platform: "discord"},
}

type fieldEntry struct {
	Fields      []FieldDef
	BlockScopes []BlockScope
	Snippets    []Snippet
}

// monsterChangedFields wraps the regular monster field set and adds the
// `original` namespace, which exposes the prior sighting under
// {{original.X}}. The same field shapes apply (minus PVP rankings, which
// are stripped from stored prior bytes).
var monsterChangedExtraFields = []FieldDef{
	{Name: "original", Type: "object", Description: "Prior-sighting view: the prior sighting's enrichment as original.X (e.g. original.fullName, original.cp, original.iv, original.boostWeatherName, original.googleMapUrl). Only enrichment fields — no aliases (use googleMapUrl, not mapurl), resolved emoji, computed fields, staticMap or PVP rankings.", Category: "identity", Preferred: true},
	{Name: "changeType", Type: "string", Description: "Kind of change: \"species\" (species/form/gender) or \"stats\" (weather boost / IV re-report)", Category: "identity"},
	{Name: "changeTypeText", Type: "string", Description: "Translated label for changeType", Category: "identity"},
}

// questSummaryFields are the shared (group-scoped) fields available
// inside a questSummary template. The per-pokestop quest fields live
// under the `quests` array — each entry exposes the same field set as
// a regular quest template (questString, rewardString, pokestopName,
// imgUrl, latitude, longitude, etc.) plus the per-row withAR boolean.
var questSummaryFields = []FieldDef{
	{Name: "rewardType", Type: "int", Description: "Reward type code (2=item, 3=stardust, 4=candy, 7=pokemon, 8=pokecoins, 12=mega energy)", Category: "quest", Preferred: true},
	{Name: "reward", Type: "int", Description: "Reward identifier — pokemon ID for type 4/7/12, item ID for type 2, dust amount for type 3, pokecoin amount for type 8", Category: "quest", Preferred: true},
	{Name: "rewardForm", Type: "int", Description: "Pokemon form ID for type==7 rewards (so different Spinda forms, costumes, etc. group separately). 0 for all other reward types.", Category: "quest"},
	{Name: "rewardName", Type: "string", Description: "Translated display name for the shared reward — matches the per-row reward strings from regular quest enrichment, with amounts stripped for types 2/4/12 (amounts vary per stop within a group). Examples: \"Spinda 01\" (type 7 with form), \"Lapras Candy\" (type 4), \"Charizard Mega Energy\" (type 12), \"Razz Berry\" (type 2), \"1500 Stardust\" (type 3 — amount kept because it's part of the group key).", Category: "quest", Preferred: true},
	{Name: "imgUrl", Type: "string", Description: "Shared reward icon URL — best used as a Discord thumbnail/image. Discord renders webp/png/gif. Telegram's sticker endpoint is stricter — use stickerUrl there.", Category: "maps", Preferred: true},
	{Name: "stickerUrl", Type: "string", Description: "Shared sticker URL for the reward — sized/formatted for Telegram's sticker constraints. Use this for the Telegram `sticker` field; imgUrl tends to be the wrong size or format.", Category: "maps", Preferred: true},
	{Name: "staticMap", Type: "string", Description: "Multi-pin static map URL autopositioned over the pokestops in this chunk only", Category: "maps", Preferred: true},
	{Name: "count", Type: "int", Description: "Total number of pokestops in this reward group (across all chunks, not just this message)", Category: "quest", Preferred: true},
	{Name: "chunk", Type: "int", Description: "1-based index of this message when a large group is split across multiple messages. Always 1 when chunks == 1.", Category: "quest"},
	{Name: "chunks", Type: "int", Description: "Total number of chunks the group was split into. Wrap chunk-suffix output in {{#if (gt chunks 1)}}...{{/if}} so single-message groups stay clean.", Category: "quest"},
	{Name: "quests", Type: "array", Description: "Per-pokestop entries for this chunk — iterate via {{#each quests}}; each entry exposes the regular quest field set plus a withAR boolean for that row", Category: "quest", Preferred: true},
}

// questSummaryBlockScopes describes the scope inside the
// {{#each quests}} block — every entry behaves like a regular quest
// view with an extra withAR boolean.
var questSummaryBlockScopes = []BlockScope{
	{
		Helper:      "each",
		Args:        []string{"quests"},
		Description: "Iterate over each pokestop's quest entry (same fields as a regular quest template, plus withAR).",
		Fields: append([]FieldDef{
			{Name: "withAR", Type: "bool", Description: "True when the entry came from an AR-required quest", Category: "quest"},
		}, questFields...),
	},
}

// fieldsByType is the per-template-type catalogue served to the DTS editor.
// Every field must be produced for its type by enrichment, an alias, the
// computed/emoji layers or the raw webhook — TestFieldCatalogue_FieldsAreProduced
// guards against entries for fields nothing emits.
var fieldsByType = map[string]fieldEntry{
	"monster":        {Fields: alertFields(true, true, true, monsterFields), BlockScopes: monsterBlockScopes, Snippets: append(commonSnippets, monsterSnippets...)},
	"monsterNoIv":    {Fields: alertFields(true, true, true, monsterFields), BlockScopes: monsterBlockScopes, Snippets: append(commonSnippets, monsterSnippets...)},
	"monsterChanged": {Fields: alertFields(true, true, true, monsterFields, monsterChangedExtraFields), BlockScopes: monsterBlockScopes, Snippets: append(commonSnippets, monsterSnippets...)},
	"raid":           {Fields: alertFields(true, true, true, raidFields), BlockScopes: raidBlockScopes, Snippets: append(commonSnippets, raidSnippets...)},
	"rsvpChanges":    {Fields: alertFields(true, true, true, raidFields), BlockScopes: raidBlockScopes, Snippets: append(commonSnippets, raidSnippets...)},
	"egg":            {Fields: alertFields(true, true, true, eggFields), BlockScopes: eggBlockScopes, Snippets: append(commonSnippets, eggSnippets...)},
	"quest":          {Fields: alertFields(true, true, false, questFields), Snippets: append(commonSnippets, questSnippets...)},
	// questSummary's view is the group view built by dts.BuildQuestSummaryView
	// — no location/geocoding/map-link fields at the top level.
	"questSummary":  {Fields: questSummaryFields, BlockScopes: questSummaryBlockScopes, Snippets: append(commonSnippets, questSnippets...)},
	"invasion":      {Fields: alertFields(true, true, true, invasionFields), Snippets: append(commonSnippets, invasionSnippets...)},
	"incident":      {Fields: alertFields(true, true, true, incidentFields, incidentOnlyFields), Snippets: append(commonSnippets, incidentSnippets...)},
	"showcase":      {Fields: alertFields(true, true, true, incidentFields, showcaseFields), Snippets: append(append(commonSnippets, incidentSnippets...), showcaseSnippets...)},
	"lure":          {Fields: alertFields(true, true, true, lureFields), Snippets: append(commonSnippets, lureSnippets...)},
	"nest":          {Fields: alertFields(true, true, false, nestFields), Snippets: commonSnippets},
	"gym":           {Fields: alertFields(true, true, true, gymFields), Snippets: commonSnippets},
	"fort-update":   {Fields: alertFields(true, false, true, fortUpdateFields), Snippets: commonSnippets},
	"maxbattle":     {Fields: alertFields(true, true, true, maxbattleFields), Snippets: append(commonSnippets, maxbattleSnippets...)},
	"weatherchange": {Fields: alertFields(false, false, true, weatherChangeFields), Snippets: commonSnippets},
	"greeting":      {Fields: greetingFields, Snippets: commonSnippets},
}
