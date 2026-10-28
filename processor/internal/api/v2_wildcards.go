package api

// Stored wildcard / sentinel values for the v2 tracking types.
//
// v2 hides every sentinel: a request that omits (or nulls) a field stores the
// wildcard, and a read of a row holding the wildcard reports null. Both
// directions — translateV2<Type> (write) and <type>RowToRule (read) — must name
// the same constant, so a wildcard cannot drift between them. The exhaustive
// check is TestV2_EveryNullableFieldRoundTripsNull.
//
// No schema `maximum` may equal one of these sentinels (spec §4.2): a sentinel
// is a stored value, not something a client may send.
const (
	// Shared across types.
	wcDistance = 0    // distance: 0 = match by geofence area, not a radius
	wcAnyID    = 9000 // pokemon_id (raid/maxbattle), costume, move, evolution: any
	wcForm     = 0    // form: 0 = any form

	// Raid / maxbattle level. A by-level rule with no tier stores wcLevelAny;
	// a specific-pokemon rule stores wcLevelPlaceholder (the matcher ignores it).
	// Both read back as null (raidLevelOrNull).
	wcLevelAny         = 90
	wcLevelPlaceholder = 9000

	// Pokemon.
	wcAnyPokemonID        = 0 // pokemon_id: 0 = catch-all (also nest)
	wcPokemonMinIV        = -1
	wcPokemonMaxIV        = 100
	wcPokemonMinCP        = 0
	wcPokemonMaxCP        = 9000
	wcPokemonMinLevel     = 0
	wcPokemonMaxLevel     = 55
	wcPokemonMinStat      = 0 // atk, def, sta
	wcPokemonMaxStat      = 15
	wcPokemonMinWeight    = 0
	wcPokemonMaxWeight    = 9000000
	wcPokemonMinTime      = 0
	wcPokemonRarity       = -1
	wcPokemonMaxRarity    = 6
	wcPokemonSize         = -1
	wcPokemonMaxSize      = 5
	wcPokemonPVPLeague    = 0
	wcPokemonPVPBest      = 1
	wcPokemonPVPWorst     = 4096
	wcPokemonPVPMinCP     = 0
	wcPokemonPVPCap       = 0
	wcPokemonPVPEvolution = 0

	// wcLegacyUnset is the 0 that rows predating the canonical rarity, size and
	// pvp rank wildcards hold; the read treats it as the wildcard (ptrUnlessAny)
	// and canonicalizePokemonRow rewrites it.
	wcLegacyUnset = 0

	// Quest.
	wcQuestReward = 0
	wcQuestAmount = 0

	// Tappable (quest-style reward columns).
	wcTappableReward = 0
	wcTappableAmount = 0

	// Nest.
	wcNestMinSpawnAvg = 0

	// Fort: include_empty defaults true (the DB column default); true reads null.
	wcFortIncludeEmpty = true
)
