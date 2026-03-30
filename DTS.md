# DTS Template Field Reference

DTS (Data Template System) templates use Handlebars syntax to render alert messages for Discord and Telegram. Templates are rendered by the Go processor using [jfberry/raymond](https://github.com/jfberry/raymond) (a fork of mailgun/raymond with a `FieldResolver` interface for zero-copy view lookup).

Templates are loaded from `config/dts.json` (with `fallbacks/dts.json` as fallback) plus additional JSON files from `config/dts/`. Each entry may use:
- Inline `template` object — JSON with `{{fieldName}}` placeholders
- `"templateFile": "dts/filename.txt"` — external file read as raw Handlebars text (allows non-JSON constructs like unquoted Handlebars expressions in value positions)
- `"@include filename"` — include directive for shared partials

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
| `now` | Date | Current date/time |
| `nowISO` | string | Current time as ISO 8601 string |
| `areas` | string | Comma-separated names of matched geofence areas |
| `addr` | string | Formatted address from reverse geocoding |
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
| `nightTime` | bool | Is it night at the alert location |
| `dawnTime` | bool | Is it dawn |
| `duskTime` | bool | Is it dusk |

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
| `emoji` | array | Array of type emoji strings |
| `emojiString` | string | Type emojis concatenated |
| `typeEmoji` | string | Same as emojiString |
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

### Weather

| Field | Type | Description |
|-------|------|-------------|
| `weather` | int | Boosted weather ID |
| `gameWeatherId` | int | Current game weather ID |
| `gameWeatherName` | string | Translated current weather name |
| `gameWeatherEmoji` | string | Current weather emoji |
| `boosted` | bool | Is boosted by current weather |
| `boostWeatherId` | int | Weather that boosts this pokemon |
| `boostWeatherName` | string | Translated boost weather name |
| `boostWeatherEmoji` | string | Boost weather emoji |
| `weatherChange` | string | Weather change message (if forecast available) |
| `weatherCurrentName` | string | Current weather name |
| `weatherCurrentEmoji` | string | Current weather emoji |
| `weatherNextName` | string | Forecast weather name |
| `weatherNextEmoji` | string | Forecast weather emoji |

### PVP

| Field | Type | Description |
|-------|------|-------------|
| `pvpBestRank` | object | Best rank across leagues |
| `bestGreatLeagueRank` | object | Best Great League rank entry |
| `bestUltraLeagueRank` | object | Best Ultra League rank entry |
| `bestLittleLeagueRank` | object | Best Little League rank entry |
| `pvpDisplayGreat` | array | Great League PVP display list (per user filter) |
| `pvpDisplayUltra` | array | Ultra League PVP display list |
| `pvpDisplayLittle` | array | Little League PVP display list |
| `pvpDisplayMax` | array | All PVP display entries combined |

Each PVP display entry has: `{pokemon, pokemonName, fullName, formName, rank, cp, percentage, level, evolution, cap, pokemon_id, form}`

### Timing

| Poracle Field | Webhook Field | Type | Description |
|---------------|---------------|------|-------------|
| **`time`** | | string | Disappear time (formatted for user's timezone) |
| **`disappearTime`** | | string | Same as time |
| **`tthh`** | | int | Hours remaining |
| **`tthm`** | | int | Minutes remaining |
| **`tths`** | | int | Seconds remaining |
| `confirmedTime` | `disappear_time_verified` | bool | Is disappear time verified by scanner |
| | `disappear_time` | int | Raw unix timestamp (use `time` instead) |

### Other

| Field | Type | Description |
|-------|------|-------------|
| `encountered` | bool | Whether pokemon was encountered (has IV data) |
| `generation` | int | Generation number |
| `generationRoman` | string | Generation as Roman numeral (I, II, etc.) |
| `generationName` | string | Translated generation name |
| `shinyPossible` | bool | Can this pokemon be shiny |
| `shinyStats` | int | Shiny rate (1 in N) |
| `shinyPossibleEmoji` | string | Shiny sparkle emoji (empty if not shiny possible) |
| `rarityGroup` | int | Rarity group (1-6) |
| `rarityName` | string | Translated rarity name |
| `sizeName` | string | Translated size name |
| `genderData` | object | `{name, emoji}` |
| `genderEmoji` | string | Gender emoji |
| `baseStats` | object | `{baseAttack, baseDefense, baseStamina}` |
| `evolutions` | array | Evolution chain entries |
| `hasEvolutions` | bool | Has evolutions |
| `megaEvolutions` | array | Mega evolution entries |
| `hasMegaEvolutions` | bool | Has mega evolutions |
| `pokestopName` | string | Nearby pokestop name (if applicable) |
| `distance` | number | Distance from user (per-user enrichment) |
| `bearing` | int | Bearing degrees from user |
| `bearingEmoji` | string | Directional arrow emoji |
| `intersection` | string | Street intersection |

---

## Raid (`raid`)

Hatched raid with a boss pokemon.

### Pokemon

| Field | Type | Description |
|-------|------|-------------|
| `pokemon_id` | int | Boss pokemon ID |
| `id` | int | Same as pokemon_id |
| `pokemonId` | int | Same as pokemon_id |
| `name` | string | Translated pokemon name |
| `formName` | string | Translated form name |
| `fullName` | string | Name + form |
| `nameEng` | string | English name |
| `evolutionName` | string | Evolution name (for mega raids) |
| `megaName` | string | Mega evolution name |
| `form` | int | Form ID |
| `formId` | int | Same as form |
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

### Type & Stats

| Field | Type | Description |
|-------|------|-------------|
| `typeName` | string | Translated type names |
| `emoji` | array | Type emoji array |
| `typeEmoji` | string | Type emojis concatenated |
| `baseStats` | object | `{baseAttack, baseDefense, baseStamina}` |
| `weaknessList` | array | Weakness categories |
| `weaknessEmoji` | string | Weakness summary with emojis |
| `generation` | int | Generation number |
| `generationRoman` | string | Roman numeral |
| `generationName` | string | Translated generation name |
| `shinyPossible` | bool | Can be shiny |
| `shinyPossibleEmoji` | string | Shiny emoji |

### Timing

| Poracle Field | Webhook Field | Type | Description |
|---------------|---------------|------|-------------|
| **`time`** | | string | Raid end time (formatted for user's timezone) |
| **`disappearTime`** | | string | Same as time |
| **`tthh`** | | int | Hours remaining |
| **`tthm`** | | int | Minutes remaining |
| **`tths`** | | int | Seconds remaining |
| `confirmedTime` | | bool | Is time verified |
| | `start` | int | Raw raid start unix timestamp |
| | `end` | int | Raw raid end unix timestamp |

### Weather & Boost

| Field | Type | Description |
|-------|------|-------------|
| `gameWeatherId` | int | Current weather |
| `gameWeatherName` | string | Translated weather name |
| `gameWeatherEmoji` | string | Weather emoji |
| `boosted` | bool | Is weather boosted |
| `boostWeatherId` | int | Boosting weather ID |
| `boostWeatherName` | string | Boost weather name |
| `boostWeatherEmoji` | string | Boost weather emoji |
| `boostingWeathers` | array | All boosting weather IDs |
| `boostingWeathersEmoji` | string | Boosting weather emojis |
| `weatherChange` | string | Weather change forecast message |

### RSVP

| Field | Type | Description |
|-------|------|-------------|
| `rsvps` | array | RSVP time slots: `{timeSlot, time, goingCount, maybeCount}` |

### Evolutions

| Field | Type | Description |
|-------|------|-------------|
| `evolutions` | array | Evolution chain: `{id, form, name, formName, typeEmoji}` |
| `hasEvolutions` | bool | Has evolutions |
| `megaEvolutions` | array | Mega evolutions: `{fullName, evolution, typeEmoji}` |
| `hasMegaEvolutions` | bool | Has mega evolutions |

---

## Egg (`egg`)

Unhatched raid egg.

| Field | Type | Description |
|-------|------|-------------|
| `level` | int | Egg level |
| `levelName` | string | Translated level name |
| `gymId` | string | Gym ID |
| `gymName` | string | Gym name |
| `gymUrl` | string | Gym image URL |
| `teamId` | int | Gym team |
| `teamName` | string | Translated team name |
| `teamEmoji` | string | Team emoji |
| `gymColor` | string | Team color hex |
| `ex` | bool | EX raid eligible |
| `hatchTime` | string | Hatch time (formatted) |
| `time` | string | Same as hatchTime |
| `tthh` | int | Hours until hatch |
| `tthm` | int | Minutes until hatch |
| `tths` | int | Seconds until hatch |
| `rsvps` | array | RSVP time slots |
| `gameWeatherName` | string | Current weather name |
| `gameWeatherEmoji` | string | Weather emoji |

---

## Quest (`quest`)

| Field | Type | Description |
|-------|------|-------------|
| `pokestopId` | string | Pokestop ID |
| `pokestopName` | string | Pokestop name |
| `pokestopUrl` | string | Pokestop image URL |
| `questString` | string | Translated quest objective |
| `questStringEng` | string | English quest objective |
| `questTitle` | string | Quest title from webhook |
| `target` | int | Quest target count |
| `rewardString` | string | All rewards as text |
| `rewardStringEng` | string | English rewards text |
| `dustAmount` | int | Stardust reward amount |
| `itemAmount` | int | Item reward amount |
| `isShiny` | bool | Reward pokemon is shiny |
| `shinyPossible` | bool | Can reward be shiny |
| `shinyPossibleEmoji` | string | Shiny emoji |
| `time` | string | Quest expiry time |
| `tthh` | int | Hours remaining |
| `tthm` | int | Minutes remaining |
| `tths` | int | Seconds remaining |

### Reward Data

| Field | Type | Description |
|-------|------|-------------|
| `rewardData.monsters` | array | Pokemon rewards: `{pokemonId, formId, shiny, name, form, fullName}` |
| `rewardData.monsterNames` | string | Comma-separated pokemon reward names |
| `rewardData.items` | array | Item rewards: `{id, amount, name}` |
| `rewardData.itemNames` | string | Comma-separated item names |
| `rewardData.dustAmount` | int | Stardust amount |
| `rewardData.energyMonsters` | array | Mega energy rewards |
| `rewardData.energyMonstersNames` | string | Energy reward text |
| `rewardData.candy` | array | Candy rewards |
| `rewardData.candyMonstersNames` | string | Candy reward text |

---

## Invasion (`invasion`)

| Field | Type | Description |
|-------|------|-------------|
| `pokestopId` | string | Pokestop ID |
| `pokestopName` | string | Pokestop name |
| `pokestopUrl` | string | Pokestop image URL |
| `gruntTypeId` | int | Grunt type ID |
| `gruntName` | string | Translated grunt name |
| `gruntType` | string | Translated grunt type (e.g. "Water") |
| `gruntTypeName` | string | Same as gruntType |
| `gruntTypeColor` | string | Type color hex |
| `gruntTypeEmoji` | string | Type emoji |
| `gruntRewards` | string | Reward pokemon summary |
| `gruntRewardsList` | object | Structured: `{first: {chance, monsters}, second: {chance, monsters}}` |
| `gruntLineupList` | object | Confirmed lineup: `{confirmed, monsters}` |
| `gender` | int | Grunt gender (1=male, 2=female) |
| `genderData` | object | `{name, emoji}` |
| `displayTypeId` | int | Event display type |
| `time` | string | Expiry time |
| `tthd` | int | Days remaining |
| `tthh` | int | Hours remaining |
| `tthm` | int | Minutes remaining |
| `tths` | int | Seconds remaining |

---

## Lure (`lure`)

| Field | Type | Description |
|-------|------|-------------|
| `pokestopId` | string | Pokestop ID |
| `pokestopName` | string | Pokestop name |
| `pokestopUrl` | string | Pokestop image URL |
| `lureTypeId` | int | Lure type ID (501-506) |
| `lureTypeName` | string | Translated lure type name |
| `lureType` | string | Same as lureTypeName |
| `lureTypeColor` | string | Lure color hex |
| `lureTypeEmoji` | string | Lure emoji |
| `time` | string | Lure expiry time |
| `tthh` | int | Hours remaining |
| `tthm` | int | Minutes remaining |
| `tths` | int | Seconds remaining |

---

## Gym (`gym`)

| Field | Type | Description |
|-------|------|-------------|
| `gymId` | string | Gym ID |
| `gymName` | string | Gym name |
| `gymUrl` | string | Gym image URL |
| `teamId` | int | New team ID |
| `oldTeamId` | int | Previous team ID |
| `teamName` | string | Translated new team name |
| `oldTeamName` | string | Translated previous team name |
| `gymColor` | string | New team color hex |
| `color` | string | Same as gymColor (deprecated alias) |
| `teamEmoji` | string | New team emoji |
| `previousControlTeamEmoji` | string | Previous team emoji |
| `slotsAvailable` | int | Available defender slots |
| `oldSlotsAvailable` | int | Previous available slots |
| `trainerCount` | int | Current defenders (6 - slots) |
| `oldTrainerCount` | int | Previous defenders |
| `ex` | bool | EX raid eligible |
| `inBattle` | bool | Gym in battle |
| `time` | string | Event time |
| `tthh` | int | Hours |
| `tthm` | int | Minutes |
| `tths` | int | Seconds |

---

## Nest (`nest`)

| Field | Type | Description |
|-------|------|-------------|
| `nestId` | string | Nest area ID |
| `nestName` | string | Nest name |
| `pokemonId` | int | Nesting pokemon ID |
| `name` | string | Translated pokemon name |
| `formName` | string | Translated form name |
| `fullName` | string | Name + form |
| `formId` | int | Form ID |
| `pokemonCount` | int | Observed spawn count |
| `pokemonSpawnAvg` | number | Average spawns per hour |
| `typeName` | string | Translated type names |
| `color` | string | Type color hex |
| `emoji` | array | Type emoji array |
| `emojiString` | string | Type emojis concatenated |
| `typeEmoji` | string | Same as emojiString |
| `shinyPossible` | bool | Can be shiny |
| `shinyPossibleEmoji` | string | Shiny emoji |
| `resetDate` | string | Next nest rotation date |
| `resetTime` | string | Next nest rotation time |
| `time` | string | Reset time |
| `tthd` | int | Days until reset |
| `tthh` | int | Hours |
| `tthm` | int | Minutes |
| `tths` | int | Seconds |

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
| `name` | string | Fort name |
| `description` | string | Fort description |
| `imgUrl` | string | Fort image URL |
| `change_type` | string | "new", "edit", or "removal" |
| `changeType` | string | Same as change_type |
| `changeTypeText` | string | "New", "Edit", or "Removal" |
| `changeTypes` | array | All change types |
| `isNew` | bool | Is a new fort |
| `isEdit` | bool | Is an edit to existing fort |
| `isRemoval` | bool | Is a removal |
| `isEditName` | bool | Name was changed |
| `isEditDescription` | bool | Description was changed |
| `isEditLocation` | bool | Location was changed |
| `isEditImageUrl` | bool | Image was changed |
| `oldName` | string | Previous name |
| `oldDescription` | string | Previous description |
| `oldImageUrl` | string | Previous image URL |
| `oldLatitude` | float | Previous latitude |
| `oldLongitude` | float | Previous longitude |
| `newName` | string | New name |
| `newDescription` | string | New description |
| `newImageUrl` | string | New image URL |
| `newLatitude` | float | New latitude |
| `newLongitude` | float | New longitude |

---

## Handlebars Helpers

Templates can use built-in Handlebars block helpers plus ~47 custom helpers registered by the processor:

**Built-in:**
- `{{#if field}}` / `{{#unless field}}` — conditional rendering
- `{{#each array}}` — iteration (`{{this}}` for current item, `{{@index}}` for index)

**Comparison:** `{{#eq a b}}`, `{{#ne a b}}`, `{{#gt a b}}`, `{{#lt a b}}`, `{{#gte a b}}`, `{{#lte a b}}`, `{{#and a b}}`, `{{#or a b}}`, `{{#not a}}`

**Math:** `{{add a b}}`, `{{subtract a b}}`, `{{multiply a b}}`, `{{divide a b}}`, `{{round v}}`, `{{floor v}}`, `{{ceil v}}`, `{{abs v}}`, `{{toFixed v decimals}}`

**String:** `{{contains str sub}}`, `{{split str sep}}`, `{{trim str}}`, `{{join arr sep}}`, `{{concat a b ...}}`, `{{lowercase str}}`, `{{uppercase str}}`, `{{replace str old new}}`

**Formatting:** `{{pad value width}}`, `{{len array}}`

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
