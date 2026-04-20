# DTS Template Field Reference

DTS (Data Template System) templates use Handlebars syntax to render alert messages for Discord and Telegram. Templates are rendered by the Go processor using [jfberry/raymond](https://github.com/jfberry/raymond) (a fork of mailgun/raymond with a `FieldResolver` interface for zero-copy view lookup).

## Template Loading

Templates are loaded from multiple sources in this order:

1. **`fallbacks/dts.json`** — bundled defaults (shipped with the code, marked **readonly**)
2. **`config/dts.json`** — user's main configuration file
3. **`config/dts/*.json`** — additional template files (one or more entries per file)

All entries are merged into a single list. When multiple entries match the same type/platform/id/language, the **last-loaded entry wins** — so `config/dts/` overrides `config/dts.json`, which overrides `fallbacks/dts.json`. This means you can customize any bundled template by creating an entry with the same key in your config.

Each entry may use:
- Inline `template` object — JSON with `{{fieldName}}` placeholders
- `"templateFile": "dts/filename.txt"` — external file read as raw Handlebars text (allows non-JSON constructs like unquoted Handlebars expressions in value positions)
- `"@include filename"` — include directive for shared partials

## Template Saving (DTS Editor API)

The `POST /api/dts/templates` endpoint saves templates safely without destroying user file organization:

- Each saved template is written to its **own file** in `config/dts/` (e.g., `monster-1-discord.json`, `raid-default-telegram-de.json`)
- If the template previously existed in `config/dts.json` or another file, it is **removed from the old file** (other entries in that file are preserved)
- **Readonly templates** (from `fallbacks/dts.json`) are never modified — saving creates an override in `config/dts/` that takes precedence via the last-match-wins loading order
- Source files that become empty after removal are automatically deleted (except the main `config/dts.json`)
- Templates can be reloaded from disk via `GET /api/dts/reload` without restarting the processor

## Poracle Fields vs Raw Webhook Fields

Templates have access to both **Poracle enriched fields** and **raw webhook fields** from Golbat. The enriched fields are recommended because they are:
- Translated to the user's language
- Formatted consistently (times, names, emojis)
- Stable across scanner versions

Raw webhook fields use snake_case (e.g. `pokemon_id`, `team_id`, `move_1`) while Poracle fields use camelCase (e.g. `pokemonId`, `teamId`, `quickMoveId`). Both are available simultaneously. Where a raw webhook field has a Poracle equivalent, the tables below show both with the **recommended** field marked.

**Tip:** Use triple braces `{{{staticMap}}}` for URLs to prevent HTML escaping.

## Common Fields (all alert types)

These fields are available in every template:

| Field | Type | Description |
|-------|------|-------------|
| `latitude` | number | Alert location latitude |
| `longitude` | number | Alert location longitude |
| `now` | string | Current date/time (RFC3339) |
| `nowISO` | string | Current time as ISO 8601 string |
| `areas` | string | Comma-separated names of matched geofence areas |
| `matched` | array | Array of lowercase matched area names (for `{{#each matched}}`) |
| `addr` | string | Formatted address from reverse geocoding |
| `formattedAddress` | string | Full formatted address string |
| `streetName` | string | Street name |
| `streetNumber` | string | Street number |
| `city` | string | City name |
| `state` | string | State/province |
| `zipcode` | string | Postal code |
| `country` | string | Country name |
| `countryCode` | string | Two-letter country code |
| `neighbourhood` | string | Neighbourhood name |
| `suburb` | string | Suburb name |
| `flag` | string | Country flag emoji |
| `staticMap` | string | Static map tile image URL |
| `staticmap` | string | *Deprecated* — alias for `staticMap` |
| `imgUrl` | string | Primary icon URL |
| `imgUrlAlt` | string | Alternative icon URL |
| `stickerUrl` | string | Sticker image URL |
| `googleMapUrl` | string | Google Maps link |
| `appleMapUrl` | string | Apple Maps link |
| `wazeMapUrl` | string | Waze link |
| `rdmUrl` | string | RDM map link |
| `reactMapUrl` | string | ReactMap link |
| `rocketMadUrl` | string | RocketMAD link |
| `mapurl` | string | *Deprecated* — alias for `googleMapUrl` |
| `applemap` | string | *Deprecated* — alias for `appleMapUrl` |
| `nightTime` | bool | Is it night at the alert location |
| `dawnTime` | bool | Is it dawn |
| `duskTime` | bool | Is it dusk |
| `tthd` | int | Days component of time remaining |
| `tthh` | int | Hours component of time remaining |
| `tthm` | int | Minutes component of time remaining |
| `tths` | int | Seconds component of time remaining |
| `distime` | string | *Deprecated* — alias for `disappearTime` |

### Weather Fields (types with S2 cell data: pokemon, raid, egg, invasion, maxbattle)

| Field | Type | Description |
|-------|------|-------------|
| `gameWeatherId` | int | Current game weather ID |
| `gameWeatherName` | string | Translated current weather name |
| `gameWeatherEmoji` | string | Current weather emoji |
| `gameweather` | string | *Deprecated* — alias for `gameWeatherName` |
| `gameweatheremoji` | string | *Deprecated* — alias for `gameWeatherEmoji` |

### Boost Fields (types with pokemon type data: pokemon, raid, maxbattle)

| Field | Type | Description |
|-------|------|-------------|
| `boosted` | bool | Is weather boosted |
| `boostWeatherId` | int/string | Boosting weather ID (empty string if not boosted) |
| `boostWeatherName` | string | Translated boost weather name |
| `boostWeatherEmoji` | string | Boost weather emoji |
| `boost` | string | *Deprecated* — alias for `boostWeatherName` |
| `boostemoji` | string | *Deprecated* — alias for `boostWeatherEmoji` |

---

## Pokemon (`monster` / `monsterNoIv`)

Template type `monster` is used for encountered pokemon (with IV data). Template type `monsterNoIv` is used for unencountered pokemon.

### Identity

| Poracle Field | Webhook Field | Type | Description |
|---------------|---------------|------|-------------|
| **`name`** | | string | Translated pokemon name (recommended) |
| **`fullName`** | | string | Name + form combined (recommended) |
| `nameEng` | | string | English pokemon name |
| `fullNameEng` | | string | English name + form |
| `formNameEng` | | string | English form name (raw, not normalised) |
| **`formName`** | | string | Translated form name |
| `formNormalised` | | string | Form name (empty if "Normal") |
| `pokemonId` | `pokemon_id` | int | Pokemon ID |
| `id` | `pokemon_id` | int | Same as pokemon_id |
| `formId` | `form` | int | Form ID |
| `encounterId` | `encounter_id` | string | Unique encounter ID |

### Stats (only in `monster` template, not `monsterNoIv`)

| Poracle Field | Webhook Field | Type | Description |
|---------------|---------------|------|-------------|
| **`iv`** | | number | IV percentage (0-100) |
| **`atk`** | `individual_attack` | int | Attack IV (0-15) |
| **`def`** | `individual_defense` | int | Defense IV (0-15) |
| **`sta`** | `individual_stamina` | int | Stamina IV (0-15) |
| **`cp`** | `cp` | int | Combat Power |
| **`level`** | `pokemon_level` | int | Pokemon level |
| `weight` | `weight` | string | Weight in kg (2 decimals) |
| `height` | `height` | string | Height in m (2 decimals) |
| **`size`** | `size` | int | Size category (1=XXS to 5=XXL) |
| **`ivColor`** | | string | Hex color based on IV range |
| `catchBase` | | string | Base catch rate % |
| `catchGreat` | | string | Great ball catch rate % |
| `catchUltra` | | string | Ultra ball catch rate % |

### Type & Weakness

| Field | Type | Description |
|-------|------|-------------|
| `typeName` | string | Translated type names (comma-separated) |
| `color` | string | Primary type color hex |
| `emojiString` | string | Type emojis concatenated |
| `typeEmoji` | string | Type emojis concatenated (resolved from emoji keys) |
| `weaknessList` | array | Weakness categories: `{value, types: [{typeId, name, typeEmoji}]}` |

### Moves

| Poracle Field | Webhook Field | Type | Description |
|---------------|---------------|------|-------------|
| **`quickMoveName`** | | string | Translated fast move name (recommended) |
| **`chargeMoveName`** | | string | Translated charged move name (recommended) |
| `quickMoveId` | `move_1` | int | Fast move ID |
| `chargeMoveId` | `move_2` | int | Charged move ID |
| `quickMoveEmoji` | | string | Fast move type emoji |
| `chargeMoveEmoji` | | string | Charged move type emoji |
| `quickMoveNameEng` | | string | English fast move name |
| `chargeMoveNameEng` | | string | English charged move name |

### Weather

Weather boost fields (`boosted`, `boostWeatherId`, `boostWeatherName`, `boostWeatherEmoji`) and game weather fields (`gameWeatherId`, `gameWeatherName`, `gameWeatherEmoji`) are documented in the Common Fields section above.

| Field | Type | Description |
|-------|------|-------------|
| `weather` | int | Boosted weather ID (from webhook) |
| `weatherChangeTime` | string | Time of next hour boundary (potential weather change) |
| `weatherForecastCurrent` | int | Forecast current weather ID (if AccuWeather configured) |
| `weatherForecastNext` | int | Forecast next-hour weather ID (if AccuWeather configured) |
| `nextHourTimestamp` | int | Unix timestamp of next hour boundary |
| `weatherChange` | string | Composed weather forecast text (e.g. "Warning Possible weather change at 14:00 : Clear -> Rain") |
| `weatherCurrentName` | string | Translated current forecast weather name |
| `weatherCurrentEmoji` | string | Current forecast weather emoji |
| `weatherNextName` | string | Translated next forecast weather name |
| `weatherNextEmoji` | string | Next forecast weather emoji |

### PVP

Base enrichment (available to all users):

| Field | Type | Description |
|-------|------|-------------|
| `pvpBestRank` | object | Best rank across leagues |
| `bestGreatLeagueRank` | object | Best Great League rank entry |
| `bestUltraLeagueRank` | object | Best Ultra League rank entry |
| `bestLittleLeagueRank` | object | Best Little League rank entry |

Per-user enrichment (varies based on user's tracking filters):

| Field | Type | Description |
|-------|------|-------------|
| `pvpGreat` | array | Great League PVP display list (filtered by user's tracking) |
| `pvpGreatBest` | object | Best entry from `pvpGreat` |
| `pvpUltra` | array | Ultra League PVP display list |
| `pvpUltraBest` | object | Best entry from `pvpUltra` |
| `pvpLittle` | array | Little League PVP display list |
| `pvpLittleBest` | object | Best entry from `pvpLittle` |
| `pvpAvailable` | bool | Any PVP data available for this pokemon |
| `userHasPvpTracks` | bool | User has PVP tracking rules that matched |
| `pvpDisplayGreatMinCP` | int | Minimum CP threshold for Great League display |
| `pvpDisplayUltraMinCP` | int | Minimum CP threshold for Ultra League display |

Each PVP display entry has: `{pokemon, pokemonName, fullName, formName, rank, cp, percentage, level, evolution, cap, pokemon_id, form}`
Each "best" object has the same fields as a display entry.

### Timing

| Poracle Field | Webhook Field | Type | Description |
|---------------|---------------|------|-------------|
| **`time`** | | string | Disappear time (formatted for user's timezone) |
| **`disappearTime`** | | string | Same as time |
| `confirmedTime` | `disappear_time_verified` | bool | Is disappear time verified by scanner |
| | `disappear_time` | int | Raw unix timestamp (use `time` instead) |

Time-remaining fields (`tthd`, `tthh`, `tthm`, `tths`) are in the Common Fields section.

| Field | Type | Description |
|-------|------|-------------|
| `tthSeconds` | int | Total seconds until despawn |

### Other

| Field | Type | Description |
|-------|------|-------------|
| `encountered` | bool | Whether pokemon was encountered (has IV data) |
| `seenType` | string | Encounter type from scanner (e.g. "wild", "pokestop", "encounter") |
| `cell_coords` | array | S2 cell vertices for cell spawns |
| `generation` | int | Generation number |
| `generationRoman` | string | Generation as Roman numeral (I, II, etc.) |
| `generationName` | string | Translated generation name |
| `generationNameEng` | string | English generation name (e.g. "Kanto") |
| `legendary` | bool | Is a legendary pokemon |
| `mythic` | bool | Is a mythical pokemon |
| `ultraBeast` | bool | Is an ultra beast |
| `shinyPossible` | bool | Can this pokemon be shiny |
| `shinyStats` | int | Shiny rate (1 in N) |
| `shinyPossibleEmoji` | string | Shiny sparkle emoji (empty if not shiny possible) |
| `rarityGroup` | int | Rarity group (1-6) |
| `rarityName` | string | Translated rarity name |
| `rarityNameEng` | string | English rarity name (e.g. "Common") |
| `sizeName` | string | Translated size name |
| `sizeNameEng` | string | English size name (e.g. "XXL") |
| `genderData` | object | `{name, emoji}` |
| `genderEmoji` | string | Gender emoji |
| `genderNameEng` | string | English gender name |
| `typeNameEng` | array | Array of English type name strings |
| `baseStats` | object | `{baseAttack, baseDefense, baseStamina}` |
| `evolutions` | array | Evolution chain entries |
| `hasEvolutions` | bool | Has evolutions |
| `megaEvolutions` | array | Mega evolution entries |
| `hasMegaEvolutions` | bool | Has mega evolutions |
| `pokestopName` | string | Nearby pokestop name (if applicable) |
| `distance` | number | Distance from user (per-user enrichment) |
| `bearing` | int | Bearing degrees from user |
| `bearingEmoji` | string | Directional arrow emoji |

---

## Raid (`raid`)

Hatched raid with a boss pokemon.

### Pokemon

| Field | Type | Description |
|-------|------|-------------|
| `pokemon_id` | int | Boss pokemon ID |
| `id` | int | Same as pokemon_id (computed alias) |
| `pokemonId` | int | *Deprecated* — alias for `pokemon_id` |
| `name` | string | Translated pokemon name |
| `formName` | string | Translated form name |
| `formNormalised` | string | Form name (empty if "Normal") |
| `fullName` | string | Name + form |
| `nameEng` | string | English name |
| `fullNameEng` | string | English name + form |
| `formNormalisedEng` | string | English form name (empty if "Normal") |
| `evolutionName` | string | Evolution name (for mega raids) |
| `megaName` | string | Mega evolution name (fullName when evolved, name when not) |
| `form` | int | Form ID (from webhook) |
| `level` | int | Raid level |

### Gym

| Poracle Field | Webhook Field | Type | Description |
|---------------|---------------|------|-------------|
| **`gymName`** | `gym_name` / `name` | string | Gym name (recommended) |
| `gymId` | `gym_id` | string | Gym ID |
| `gymUrl` | `gym_url` / `url` | string | Gym image URL |
| **`teamId`** | `team_id` | int | Gym team (0=uncontested, 1=mystic, 2=valor, 3=instinct) |
| **`teamName`** | | string | Translated team name (recommended) |
| `teamNameEng` | | string | English team name |
| **`gymColor`** | | string | Team color hex |
| `color` | | string | Same as gymColor (deprecated alias) |
| `teamEmoji` | | string | Team emoji |
| `ex` | `ex_raid_eligible` | bool | EX raid eligible |

### Moves

| Poracle Field | Webhook Field | Type | Description |
|---------------|---------------|------|-------------|
| **`quickMoveName`** | | string | Translated fast move name (recommended) |
| **`chargeMoveName`** | | string | Translated charged move name (recommended) |
| `quickMoveId` | `move_1` | int | Fast move ID |
| `chargeMoveId` | `move_2` | int | Charged move ID |
| `quickMoveEmoji` | | string | Fast move type emoji |
| `chargeMoveEmoji` | | string | Charged move type emoji |
| `quickMoveNameEng` | | string | English fast move name |
| `chargeMoveNameEng` | | string | English charged move name |

### Type & Stats

| Field | Type | Description |
|-------|------|-------------|
| `typeName` | string | Translated type names |
| `typeEmoji` | string | Type emojis concatenated |
| `baseStats` | object | `{baseAttack, baseDefense, baseStamina}` |
| `weaknessList` | array | Weakness categories |
| `generation` | int | Generation number |
| `generationRoman` | string | Roman numeral |
| `generationName` | string | Translated generation name |
| `genderData` | object | `{name, emoji}` |
| `genderEmoji` | string | Gender emoji |
| `genderNameEng` | string | English gender name |
| `typeNameEng` | array | Array of English type name strings |
| `generationNameEng` | string | English generation name |
| `shinyPossible` | bool | Can raid boss be shiny |
| `shinyPossibleEmoji` | string | Shiny sparkle emoji (empty if not shiny possible) |
| `hasEvolutions` | bool | Boss has evolutions |
| `evolutions` | array | Evolution chain entries |
| `hasMegaEvolutions` | bool | Boss has mega evolutions |
| `megaEvolutions` | array | Mega evolution entries |

### Timing

| Poracle Field | Webhook Field | Type | Description |
|---------------|---------------|------|-------------|
| **`time`** | | string | Raid end time (formatted for user's timezone) |
| **`disappearTime`** | | string | Same as time |
| `start` | `start` | int | Raid start unix timestamp |
| `end` | `end` | int | Raid end unix timestamp |

Time-remaining fields (`tthd`, `tthh`, `tthm`, `tths`) are in the Common Fields section.

### Weather & Boost

Weather and boost fields (`gameWeatherId`, `gameWeatherName`, `gameWeatherEmoji`, `boosted`, `boostWeatherId`, `boostWeatherName`, `boostWeatherEmoji`) are documented in the Common Fields section.

| Field | Type | Description |
|-------|------|-------------|
| `boostingWeatherIds` | array | All weather IDs that boost this pokemon's types |
| `weatherChangeTime` | string | Time of next hour boundary (potential weather change) |
| `weatherForecastCurrent` | int | Forecast current weather ID (if AccuWeather configured) |
| `weatherForecastNext` | int | Forecast next-hour weather ID (if AccuWeather configured) |
| `nextHourTimestamp` | int | Unix timestamp of next hour boundary |

| `boostingWeathersEmoji` | string | All boosting weather emojis concatenated |
| `weatherChange` | string | Composed weather forecast text (e.g. "Warning Possible weather change at 14:00 : Clear -> Rain") |
| `weatherCurrentName` | string | Translated current forecast weather name |
| `weatherCurrentEmoji` | string | Current forecast weather emoji |
| `weatherNextName` | string | Translated next forecast weather name |
| `weatherNextEmoji` | string | Next forecast weather emoji |

### RSVP

| Field | Type | Description |
|-------|------|-------------|
| `rsvps` | array | RSVP time slots: `{timeSlot, time, goingCount, maybeCount}` |

---

## Egg (`egg`)

Unhatched raid egg.

| Field | Type | Description |
|-------|------|-------------|
| `level` | int | Egg level |
| `levelName` | string | Translated level name |
| `gymId` | string | Gym ID (alias for `gym_id`) |
| `gymName` | string | Gym name (alias for `gym_name`) |
| `gymUrl` | string | Gym image URL (alias for `gym_url`) |
| `teamId` | int | Gym team (alias for `team_id`) |
| `teamName` | string | Translated team name |
| `teamNameEng` | string | English team name |
| `teamEmoji` | string | Team emoji |
| `teamColor` | string | Team color hex |
| `gymColor` | string | Gym color hex (alias for `gym_color`) |
| `ex` | bool | EX raid eligible (alias for `is_ex_raid_eligible`) |
| `hatchTime` | string | Hatch time (formatted) |
| `time` | string | Alias for `disappearTime` (raid end time) |
| `disappearTime` | string | Raid end time (formatted) |
| `start` | int | Hatch unix timestamp |
| `end` | int | Raid end unix timestamp |
| `rsvps` | array | RSVP time slots |
| `gameWeatherName` | string | Current weather name |
| `gameWeatherEmoji` | string | Weather emoji |

Time-remaining fields (`tthd`, `tthh`, `tthm`, `tths`) are computed from hatch time (not end time).

---

## Quest (`quest`)

| Field | Type | Description |
|-------|------|-------------|
| `pokestopName` | string | Pokestop name (alias for `pokestop_name`) |
| `pokestopUrl` | string | Pokestop image URL (alias for `pokestop_url`) |
| `questString` | string | Translated quest objective |
| `questStringEng` | string | English quest objective |
| `rewardString` | string | All rewards as text (translated) |
| `rewardStringEng` | string | English rewards text |
| `dustAmount` | int | Stardust reward amount |
| `itemAmount` | int | Item reward amount |
| `energyAmount` | int | Mega energy amount (first reward) |
| `candyAmount` | int | Candy amount (first reward) |
| `isShiny` | bool | Reward pokemon is shiny |
| `shinyPossible` | bool | Can reward be shiny |
| `shinyPossibleEmoji` | string | Shiny emoji |
| `shinyStats` | int | Shiny rate (1 in N) for reward pokemon |
| `baseStats` | object | `{baseAttack, baseDefense, baseStamina}` for reward pokemon |
| `time` | string | Quest expiry time (alias for `disappearTime`) |
| `disappearTime` | string | Quest expiry time (end of day) |
| `futureEvent` | bool | Quest may change due to upcoming event |
| `futureEventTime` | string | Event start time |
| `futureEventName` | string | Event name |
| `futureEventTrigger` | string | Event trigger type |

Time-remaining fields (`tthd`, `tthh`, `tthm`, `tths`) are in the Common Fields section.

Raw webhook fields (`pokestop_id`, `pokestop_name`, `pokestop_url`, `quest_title`, `target`) are also available.

### Reward Name Strings

These are flat top-level strings, not nested under a `rewardData` object:

| Field | Type | Description |
|-------|------|-------------|
| `monsterNames` | string | Comma-separated pokemon reward names (translated) |
| `monsterNamesEng` | string | English pokemon reward names |
| `itemNames` | string | Comma-separated item names (translated) |
| `itemNamesEng` | string | English item names |
| `dustText` | string | Translated stardust text (e.g. "500 Stardust") |
| `dustTextEng` | string | English stardust text |
| `energyMonstersNames` | string | Mega energy reward text (translated) |
| `energyMonstersNamesEng` | string | English energy reward text |
| `candyMonstersNames` | string | Candy reward text (translated) |
| `candyMonstersNamesEng` | string | English candy reward text |
| `monsterList` | array | *Deprecated* — alias for `monsters` |
| `monsters` | array | Pokemon encounter rewards: `{pokemonId, formId, shiny, name, formName, fullName, nameEng, fullNameEng}` |
| `items` | array | Item rewards: `{id, amount, name, nameEng}` |
| `energyMonsters` | array | Mega energy rewards: `{pokemonId, amount, name, nameEng}` |
| `candy` | array | Candy rewards: `{pokemonId, amount, name, nameEng}` |

---

## Invasion (`invasion`)

| Field | Type | Description |
|-------|------|-------------|
| `pokestopName` | string | Pokestop name (alias for `pokestop_name`) |
| `pokestopUrl` | string | Pokestop image URL (alias for `pokestop_url`) |
| `gruntTypeId` | int | Grunt type ID |
| `gruntName` | string | Translated grunt name |
| `gruntType` | string | Translated grunt type, e.g. "Water" (alias for `gruntTypeName`) |
| `gruntTypeName` | string | Translated grunt type name |
| `gruntTypeColor` | string | Type color hex |
| `gruntTypeEmoji` | string | Type emoji |
| `gruntRewards` | string | Reward pokemon summary (translated) |
| `gruntRewardsList` | object | Structured: `{first: {chance, monsters}, second: {chance, monsters}}` |
| `gruntRewardIDs` | object | Raw reward pokemon IDs (without translation) |
| `gruntGender` | int | Grunt gender (1=male, 2=female) |
| `gender` | int | Alias for `gruntGender` (0/1/2) |
| `genderData` | object | `{name, emoji}` |
| `genderEmoji` | string | Gender emoji |
| `genderNameEng` | string | English gender name |
| `gruntLineupList` | object | Confirmed catch lineup: `{confirmed: true, monsters: [{id, formId, name, formName, fullName}]}` |
| `displayTypeId` | int | Event display type |
| `time` | string | Expiry time (alias for `disappearTime`) |
| `disappearTime` | string | Invasion expiry time |

Time-remaining fields (`tthd`, `tthh`, `tthm`, `tths`) are in the Common Fields section.

Raw webhook fields (`pokestop_id`, `pokestop_name`, `pokestop_url`) are also available.

Weather fields (`gameWeatherId`, `gameWeatherName`, `gameWeatherEmoji`) are available for invasions.

---

## Lure (`lure`)

| Field | Type | Description |
|-------|------|-------------|
| `pokestopName` | string | Pokestop name (alias for `pokestop_name`) |
| `pokestopUrl` | string | Pokestop image URL (alias for `pokestop_url`) |
| `lureTypeId` | int | Lure type ID (501-506) |
| `lureTypeName` | string | Translated lure type name |
| `lureTypeNameEng` | string | English lure type name |
| `lureType` | string | *Deprecated* — alias for `lureTypeName` |
| `lureTypeColor` | string | Lure color hex (alias for `lureColor`) |
| `lureTypeEmoji` | string | Lure emoji |
| `time` | string | Lure expiry time (alias for `disappearTime`) |
| `disappearTime` | string | Lure expiry time |

Time-remaining fields (`tthd`, `tthh`, `tthm`, `tths`) are in the Common Fields section.

Raw webhook fields (`pokestop_id`, `pokestop_name`, `pokestop_url`, `lure_id`) are also available.

---

## Gym (`gym`)

| Field | Type | Description |
|-------|------|-------------|
| `gymId` | string | Gym ID (alias for `gym_id`) |
| `gymName` | string | Gym name (alias for webhook `name`) |
| `gymUrl` | string | Gym image URL (alias for webhook `url`) |
| `teamId` | int | New team ID (alias for `team_id`) |
| `oldTeamId` | int | Previous team ID |
| `previousControlId` | int | Previous controller's team ID (from `last_owner_id` webhook field) |
| `teamName` | string | Translated new team name |
| `teamNameEng` | string | English team name |
| `teamColor` | string | Team color hex |
| `oldTeamName` | string | Translated previous team name |
| `oldTeamNameEng` | string | English old team name |
| `previousControlName` | string | Translated previous controller team name |
| `previousControlNameEng` | string | English previous controller team name |
| `gymColor` | string | New team color hex |
| `color` | string | Same as gymColor (*deprecated*) |
| `teamEmoji` | string | New team emoji |
| `previousControlTeamEmoji` | string | Previous team emoji |
| `slotsAvailable` | int | Available defender slots |
| `oldSlotsAvailable` | int | Previous available slots |
| `trainerCount` | int | Current defenders (6 - slots) |
| `oldTrainerCount` | int | Previous defenders |
| `ex` | bool | EX raid eligible |
| `inBattle` | bool | Gym in battle |
| `conqueredTime` | string | Formatted time of team change (current time) |

Time-remaining fields are set with a fixed 1-hour TTH.

---

## Nest (`nest`)

| Field | Type | Description |
|-------|------|-------------|
| `nestName` | string | Nest/park name (from webhook) |
| `pokemonId` | int | Pokemon ID (camelCase alias) |
| `pokemonCount` | int | Observed spawn count (from webhook) |
| `pokemonSpawnAvg` | float | Average spawns per hour |
| `name` | string | Translated pokemon name |
| `nameEng` | string | English pokemon name |
| `formName` | string | Translated form name |
| `formNormalised` | string | Form name (empty if "Normal") |
| `fullName` | string | Name + form |
| `fullNameEng` | string | English name + form |
| `formNormalisedEng` | string | English form name (empty if "Normal") |
| `typeName` | string | Translated type names |
| `color` | string | Type color hex |
| `typeEmoji` | string | Type emojis concatenated |
| `shinyPossible` | bool | Can be shiny |
| `shinyPossibleEmoji` | string | Shiny sparkle emoji (empty if not shiny possible) |
| `resetDate` | string | Nest rotation date |
| `resetTime` | string | Nest rotation time |
| `disappearTime` | string | Expiry time (reset + 7 days) |
| `disappearDate` | string | Expiry date |
| `time` | string | Alias for `disappearTime` |

Time-remaining fields (`tthd`, `tthh`, `tthm`, `tths`) are in the Common Fields section.

Raw webhook fields (`nest_id`, `nest_name`, `pokemon_id`, `form`, `pokemon_avg`) are also available.

Map URLs (`googleMapUrl`, `appleMapUrl`, `wazeMapUrl`, etc.) are available for nests.

**Nest autoposition:** If the webhook includes `poly_path` (polygon coordinates), the enrichment computes optimal `zoom`, `map_latitude`, and `map_longitude` for the static map tile.

---

## Max Battle (`maxbattle`)

| Field | Type | Description |
|-------|------|-------------|
| `color` | string | Max battle color hex (D000C0) |
| `stationId` | string | Battle station ID |
| `stationName` | string | Station name |
| `pokemonId` | int | Battle pokemon ID |
| `id` | int | Same as pokemonId |
| `name` | string | Translated pokemon name |
| `formName` | string | Translated form name |
| `fullName` | string | Name + form |
| `level` | int | Battle level |
| `levelName` | string | Translated level name |
| `gmax` | bool | Is Gigantamax |
| `megaName` | string | Mega/Gmax name |
| `quickMoveName` | string | Translated fast move |
| `chargeMoveName` | string | Translated charged move |
| `quickMoveEmoji` | string | Fast move type emoji |
| `chargeMoveEmoji` | string | Charged move type emoji |
| `typeName` | string | Translated type names |
| `emoji` | array | Type emojis |
| `typeEmoji` | string | Type emojis concatenated |
| `boosted` | bool | Is weather boosted |
| `boostWeatherName` | string | Boost weather name |
| `boostWeatherEmoji` | string | Boost weather emoji |
| `gameWeatherName` | string | Current weather name |
| `gameWeatherEmoji` | string | Weather emoji |
| `generationName` | string | Generation name |
| `shinyPossible` | bool | Can be shiny |
| `shinyPossibleEmoji` | string | Shiny emoji |
| `genderData` | object | `{name, emoji}` |
| `baseStats` | object | `{baseAttack, baseDefense, baseStamina}` |
| `weaknessList` | array | Weakness categories |
| `evolutions` | array | Evolution chain |
| `megaEvolutions` | array | Mega evolutions |
| `time` | string | Battle end time |
| `tthd` | int | Days remaining |
| `tthh` | int | Hours |
| `tthm` | int | Minutes |
| `tths` | int | Seconds |

---

## Weather Change (`weatherchange`)

| Field | Type | Description |
|-------|------|-------------|
| `weatherId` | int | New weather condition ID |
| `oldWeatherId` | int | Previous weather condition ID |
| `condition` | int | Same as weatherId |
| `weatherName` | string | Translated new weather name |
| `oldWeatherName` | string | Translated previous weather name |
| `weatherEmoji` | string | New weather emoji |
| `oldWeatherEmoji` | string | Previous weather emoji |
| `weatherCellId` | string | S2 cell ID |
| `s2_cell_id` | string | Same as weatherCellId |
| `source` | string | Change source ("webhook" or "fromMonster") |
| `activePokemons` | array | Affected tracked pokemon (when configured) |

Each `activePokemons` entry:

| Field | Type | Description |
|-------|------|-------------|
| `pokemon_id` | int | Pokemon ID |
| `form` | int | Form ID |
| `iv` | number | IV percentage |
| `cp` | int | CP |
| `latitude` | float | Pokemon latitude |
| `longitude` | float | Pokemon longitude |
| `disappearTime` | string | Disappear time |
| `name` | string | Translated pokemon name |
| `fullName` | string | Name + form |
| `formName` | string | Form name |
| `imgUrl` | string | Pokemon icon URL |

---

## Fort Update (`fort-update`)

| Field | Type | Description |
|-------|------|-------------|
| `id` | string | Fort ID |
| `fortType` | string | "pokestop" or "gym" |
| `fortTypeText` | string | "Pokestop" or "Gym" |
| `isEmpty` | bool | True if fort has no name or description |
| `name` | string | Fort name (best available from new/old snapshot) |
| `description` | string | Fort description (best available) |
| `imgUrl` | string | Fort image URL (best available) |
| `changeTypeText` | string | "New", "Edit", or "Removal" |
| `isNew` | bool | Is a new fort |
| `isEdit` | bool | Is an edit to existing fort |
| `isRemoval` | bool | Is a removal |
| `isEditName` | bool | Name was changed |
| `isEditDescription` | bool | Description was changed |
| `isEditLocation` | bool | Location was changed |
| `isEditImageUrl` | bool | Image was changed |
| `isEditImgUrl` | bool | Alias for `isEditImageUrl` |
| `oldName` | string | Previous name |
| `oldDescription` | string | Previous description |
| `oldImageUrl` | string | Previous image URL |
| `oldImgUrl` | string | Alias for `oldImageUrl` |
| `oldLatitude` | float | Previous latitude |
| `oldLongitude` | float | Previous longitude |
| `newName` | string | New name |
| `newDescription` | string | New description |
| `newImageUrl` | string | New image URL |
| `newImgUrl` | string | Alias for `newImageUrl` |
| `newLatitude` | float | New latitude |
| `newLongitude` | float | New longitude |
| `zoom` | float | Autopositioned zoom level (if location edit with old/new coords) |
| `map_latitude` | float | Autopositioned map center latitude |
| `map_longitude` | float | Autopositioned map center longitude |

Raw webhook fields (`change_type`, `edit_types`) are also available.

**Not yet implemented:** `tth`, `disappearTime`, `resetDate`, `resetTime` timing fields are not set for fort updates.

---

## Handlebars Helpers

Templates can use built-in Handlebars block helpers plus custom helpers registered by the processor:

**Built-in:**
- `{{#if field}}` / `{{#unless field}}` — conditional rendering
- `{{#each array}}` — iteration (`{{this}}` for current item, `{{@index}}` for index, `{{isFirst}}`/`{{isLast}}` as context properties)
- `{{#forEach array}}` — like each, with `{{@total}}` data variable

**Comparison:** `{{#eq a b}}`, `{{#ne a b}}`, `{{#isnt a b}}`, `{{#gt a b}}`, `{{#lt a b}}`, `{{#gte a b}}`, `{{#lte a b}}`, `{{#and a b ...}}` (variadic), `{{#or a b ...}}` (variadic), `{{#neither a b ...}}` (variadic, inverse of or), `{{#not a}}`, `{{#contains collection value}}`, `{{#compare a "op" b}}`

All comparison helpers work both as block helpers (`{{#eq a b}}X{{else}}Y{{/eq}}`) and as subexpressions (`{{#if (eq a b)}}`).

**Math:** `{{add a b}}`, `{{plus a b}}`, `{{subtract a b}}`, `{{minus a b}}`, `{{multiply a b}}`, `{{divide a b}}`, `{{round v}}`, `{{floor v}}`, `{{ceil v}}`, `{{toFixed v decimals}}`, `{{toInt v}}`

**String:** `{{contains str sub}}`, `{{join arr sep}}`, `{{concat a b ...}}` (variadic), `{{lowercase str}}`, `{{uppercase str}}`, `{{capitalize str}}`, `{{replace str old new}}` (all occurrences), `{{replaceFirst str old new}}` (first occurrence only), `{{truncate str maxLen [suffix="..."]}}`

**Array:** `{{length arr}}`, `{{first arr [n=1]}}`, `{{last arr [n=1]}}`

**Formatting:** `{{pad0 value width}}` (zero-pad), `{{numberFormat value decimals}}`, `{{addCommas value}}` (thousand separators), `{{default value fallback}}`

**Game Data:**
- `{{pokemonName id}}` — translated pokemon name (uses template language)
- `{{pokemonNameEng id}}` — English pokemon name
- `{{pokemonNameAlt id}}` — alt-language pokemon name
- `{{pokemonForm formId}}` / `{{pokemonFormEng formId}}` / `{{pokemonFormAlt formId}}` — form names
- `{{#pokemon id form}}...{{/pokemon}}` — block helper providing rich pokemon context (`name`, `nameEng`, `formName`, `fullName`, `typeName`, `typeEmoji`, `baseStats`, `hasEvolutions`)
- `{{pokemonBaseStats id form}}` — returns `{baseAttack, baseDefense, baseStamina}`
- `{{calculateCp baseStats level ivAtk ivDef ivSta}}` or `{{calculateCp pokemonId formId level ivAtk ivDef ivSta}}` — calculate CP
- `{{moveName id}}` / `{{moveNameEng id}}` / `{{moveNameAlt id}}` — move name lookups
- `{{moveType moveId}}` / `{{moveTypeEng moveId}}` / `{{moveTypeAlt moveId}}` — move type name
- `{{moveEmoji moveId}}` / `{{moveEmojiEng moveId}}` / `{{moveEmojiAlt moveId}}` — move type emoji
- `{{getEmoji key}}` — emoji lookup by key (platform-aware)
- `{{translateAlt text}}` — translate key in alt language
- `{{#getPowerUpCost startLevel endLevel}}...{{/getPowerUpCost}}` — power-up costs (`stardust`, `candy`, `xlCandy`)
- `{{#map "mapName" value}}...{{/map}}` / `{{#map2 "mapName" value fallback}}...{{/map2}}` — custom map lookups (from `config/customMaps/`)

**Utility:** `{{escape value}}` — explicit HTML escaping

## Example Template

```json
{
  "content": "",
  "embed": {
    "color": "{{ivColor}}",
    "title": "{{round iv}}% {{name}} cp:{{cp}} L:{{level}} {{atk}}/{{def}}/{{sta}}",
    "description": "End: {{time}} ({{tthh}}h{{tthm}}m{{tths}}s)\n{{addr}}\nMoves: {{quickMoveName}} / {{chargeMoveName}}",
    "thumbnail": {"url": "{{imgUrl}}"},
    "image": {"url": "{{{staticMap}}}"}
  }
}
```

**Note:** Use triple braces `{{{staticMap}}}` for URLs to prevent HTML escaping.
