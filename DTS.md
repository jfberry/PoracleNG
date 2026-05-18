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

### The `default: true` flag and help templates

`default: true` on an entry means "match any query of this (type, platform, language) whose id doesn't have an exact match". It's evaluated at priority level 3 of the selection chain and **does not check the id**.

For most template types that's harmless — a default monster template legitimately catches any tracking rule that didn't specify its own template id. For `type: "help"` it has a subtle side-effect:

- `!help` (no args) queries id `"index"`.
- `!help track` queries id `"track"`.
- `!help raid` queries id `"raid"`. ...etc.

If your custom help entry is `{type: "help", default: true, ...}` with any id (e.g. `"1"`), it matches **every one of those queries** at level 3, shadowing the shipped `help/track`, `help/raid`, etc. entries entirely. That's the correct behavior if you want your entry to be the complete help surface — but it surprises operators migrating from PoracleJS where no per-topic help shipped.

| Intent | Config |
|---|---|
| My entry is the complete help (replaces all topics too) | `{id: "<anything>", default: true}` |
| My entry is the landing page; shipped `!help track` / `!help raid` / ... still work | `{id: "index", default: false}` |

The processor emits a startup advisory when it sees a user `type: "help"` entry with `default: true`, pointing to this doc.

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
| `diademUrl` | string | Diadem map link |
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
| `distance` | number | Distance from the user's registered location to the alert, in metres |
| `bearing` | int | Bearing from user to alert, in degrees |
| `bearingEmoji` | string | Directional arrow emoji for `bearing` |
| `userDistanceTrack` | bool | True when the matched tracking rule was distance-based (e.g. `!track pikachu d:500` or `!raid T5 d:1000`) rather than area-based. Useful for conditioning the template on *why* the user received the alert. |
| `userTrackDistance` | int | The matched rule's distance threshold in metres. `0` when the rule was area-based. For pokemon alerts where multiple rules can match one user, this is the largest threshold across matching rules. |

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
| `boostingWeathersEmoji` | string | Concatenated emoji string for every weather that boosts this pokemon's types (e.g. `"☀️💨"`) |
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
| `weaknessEmoji` | string | Flat space-separated `"<value>x<typeEmoji>"` per category, e.g. `"2x💧⚡ 4x🪨 "` — for templates that don't iterate `weaknessList`. |

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
| `userHasPvpTracks` | bool | True when at least one matched rule was a real PVP tracking rule (league > 0 with a meaningful worst threshold). Pokemon alerts only. |
| `pvpUserRanking` | int | The matched rule's worst-rank threshold (0 when the rule was not PVP-based) |
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
| `seenType` | string | Normalised encounter source. See [seenType values](#seentype-values). |
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

`distance`, `bearing`, `bearingEmoji`, `userDistanceTrack`, `userTrackDistance` are documented in Common Fields.

### seenType values

`{{seenType}}` is normalised from Golbat's raw `seen_type` (see Golbat's
webhooks reference). Use it in templates to switch on how the pokemon was
discovered — wild encounters carry IVs / weight / height, nearby spawns
do not.

| `seenType` | Source `seen_type` | Meaning |
|---|---|---|
| `cell` | `nearby_cell` | Seen on a cell's nearby list — location is the S2 cell centre, imprecise. Also returned for RDM-style scanners that report no spawn id and no fort id. |
| `pokestop` | `nearby_stop` | Seen on a fort's nearby list — location is the fort's coordinates, imprecise. |
| `wild` | `wild` | Seen in the wild feed — real spawn-point location, no IVs yet. |
| `encounter` | `encounter` | Full encounter decoded — IVs, moves, weight, height, size, PVP all known. |
| `lure` | `lure_wild` | Seen on a lure's map list (pre-encounter). No IVs yet. |
| `lure_encounter` | `lure_encounter` | Full disk encounter decoded for a lure-spawned pokemon. |
| `tappable` | `tappable_encounter`, `tappable_lure_encounter` | Full encounter decoded via `PROCESS_TAPPABLE` — overworld tappable objects (and their lured variant). Both Golbat sub-types collapse to a single value here so templates only need one switch arm. |

Empty string is returned when no `seen_type` is supplied and the legacy
RDM-style fallback can't infer one.

---

## Pokemon Changed (`monsterChanged`)

`monsterChanged` fires for **post-encounter** changes to an already-tracked
pokemon — form, species, gender, or weather-boost shift. The non-IV → IV
encounter event itself stays on the regular `monster` template (it's the
fulfilment of the existing alert, not a "change"); both kinds of update are
dispatched as a reply to the prior message via the implicit reply key on
every pokemon render. See [API.md](API.md#pokemon-change-template-monsterchanged)
for the operator-facing summary.

The template receives every field listed under [Pokemon (`monster` /
`monsterNoIv`)](#pokemon-monster--monsternoiv) — those describe the **new**
state. Additionally, `{{original.X}}` exposes the same field set for the
**prior** sighting (minus PVP, which is stripped at storage time): identity
(`original.fullName`, `original.formName`, `original.pokemonId`, …), battle
stats (`original.cp`, `original.iv`, `original.atk/def/sta`,
`original.level`), weather (`original.weatherName`, `original.gameWeatherId`),
images and map URLs (`original.imgUrl`, `original.staticMap`,
`original.mapurl`), and so on.

`{{original.X}}` is rendered per recipient: each user's language picks the
appropriate translated names (`original.fullName` for a German user is
"Glumanda", for an English user "Charmander"). PVP rankings are not
available under `original.*`.

**Guard stat fields with `{{#if encountered}}`.** A species/form change
fires `monsterChanged` as soon as the new species is *known*, which can
be before it has been encountered (Golbat's wild webhook lands before
the encounter webhook). In that window, `cp`, `level`, `atk/def/sta`,
`iv`, `quickMoveName`, etc. are all zero/empty, and `iv` is `-1`. Wrap
the stats portion of your template in `{{#if encountered}}…{{else}}…{{/if}}`
so you don't render "−1% Foo cp:0 L:0 0/0/0" while waiting for the
encounter. The shipped fallbacks do this.

Only set when fired by a true change event — the dispatcher also leaves
`{{original.X}}` empty when:
- The encounter event reuses the regular `monster` template (CP 0 → >0).
- The matched user had no prior message for the encounter (a fresh
  `monster` render is sent instead, no reply, no `original`).
- Pokemon change tracking is disabled via `[tracking]
  pokemon_change_tracking = false`.

### Change dimension fields

`monsterChanged` also exposes two fields describing the kind of change that
fired the alert. Use these in templates to switch wording or styling:

| Field | Type | Description |
|---|---|---|
| `changeType` | string | One of `species` or `stats`. See table below. |
| `changeTypeText` | string | Localised label (e.g. "species change", "stats change"). |

| `changeType` | Fires for | Meaning |
|---|---|---|
| `species` | `ChangeSpecies`, `ChangeForm`, `ChangeGender` | Identity change. Community-day re-classifications, or the "A/B pokemon" anomaly where Golbat reports a different species ID for the same encounter. |
| `stats` | `ChangeWeatherBoost`, `ChangeStats` | Same pokemon, different effective stats. Weather-boost shifts the CP/level post-encounter; `ChangeStats` fires when Golbat re-reports the same encounter with different raw IVs (atk/def/sta) — the scanner anomaly where successive webhooks for the same encounter ID carry different IV values. |

The `ChangeEncountered` dimension (CP 0 → >0, "IVs just arrived") does **not**
fire `monsterChanged`. Users tracking IV-insensitively (`!track pikachu`, no
filter) get a regular `monster` reply via the matched path. Users with strict
IV filters never matched the wild webhook in the first place (the matcher
rejects rules with `min_iv > -1` when CP=0), so there is no prior message
to follow up.

PoracleNG ships a default `monsterChanged` template per platform in
`fallbacks/dts.json`; admins override via `config/dts.json` or
`config/dts/` like any other type.

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
| `weaknessEmoji` | string | Flat `"<value>x<typeEmoji>"` per category |
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

## Raid/Egg RSVP Updates (`rsvpChanges`)

A **single shared template type** for compact RSVP-change notifications, used for both raid and egg lifecycles. Opt-in by file presence — falls back to the full `raid` / `egg` template when not defined.

### When it fires

| Condition | Outcome |
|---|---|
| First visible message for this user / this raid lifecycle | Full `raid` or `egg` template (never `rsvpChanges`). |
| Tracking rule has `edit` bit set | Full `raid` / `egg`, edited in-place. `rsvpChanges` is bypassed even when present. |
| Subsequent same-type webhook AND `rsvpChanges` template exists with matching id | `rsvpChanges` renders. |
| Subsequent webhook AND no `rsvpChanges` entry for the user's effective template id | Full `raid` / `egg` (fallback). |

"Same-type" is the source webhook type (raid or egg) — tracked separately from the template chosen. An egg → raid hand-off treats the raid notification as the first raid-type visible message, so the raid uses the full template even though an egg was already sent.

### Template ID matching

`rsvpChanges` existence is checked with **strict equality** against the user's effective template id:

- If the tracking rule has `template:dark` → looks for `rsvpChanges` with `id:dark`. No match → fallback to `raid` / `egg`.
- If the rule has no explicit template → looks for `rsvpChanges` with id matching `[general] default_template_name`.

This avoids silent template-id substitution. If you ship operators a `rsvpChanges` with `id:1`, set `default_template_name = "1"` (matches the shipped `fallbacks/dts.json`).

### Reply chain

Every raid + egg job carries `ReplyKey = "raidlife:{gymID}:{raidEnd}"`. The dispatcher tracks each message under that key; subsequent jobs for the same lifecycle find the prior message and post as a reply.

The chain reads as one thread in Discord/Telegram:

```
egg → raid → rsvpChanges → rsvpChanges → …
```

Reply threading works without `clean`/`edit` bits — every raid/egg message is stored in the tracker when `ReplyKey` is set, even with `clean=0`. Auto-delete on TTH expiry still gates on the `clean` bit; the reply-only tracker entry self-evicts at natural TTL with no side effect on the user's message.

### Cleanup TTH

For `rsvpChanges` jobs with `clean` set, the auto-delete TTH is the **latest future RSVP timeslot in the rendered state** (not `raid.End`). The original raid/egg alert keeps `raid.End` cleanup; the compact RSVP cards clean shortly after the meaningful party window so they don't linger past it.

If there are no future RSVP timeslots (all past at send time), the override is not applied and the message uses the default raid/end TTH.

### Available fields

`rsvpChanges` uses the **raid alias set** internally — every field from the `raid` table above is available (gym, RSVP, weather, boost, ex, moves, forms, etc.). When rendering for an egg-source webhook, the raid-only fields (moves, forms, pokemon name) are simply empty — operators can branch with `{{#if pokemonId}}` or `{{#if fullName}}` to handle the egg-vs-hatched case in a single template.

### Example: minimal compact template

See `examples/dts/rsvpChanges/rsvp-update.json` for an installable starting point.

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
| `conditionString` | string | Comma-joined completion conditions, translated, e.g. "Excellent Throw, Curve Ball" |
| `conditionStringEng` | string | English copy of `conditionString` |
| `conditionList` | array | Per-condition objects: `{type, name, formatted}` where `name` is the bare label ("Throw Type") and `formatted` includes the payload ("Excellent Throw"). Falls back to bare name when the webhook payload doesn't carry the data needed for the formatted variant. |
| `conditionListEng` | array | English copy of `conditionList` |
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

## Quest Summary (`questSummary`)

`questSummary` templates render a *grouped* quest message rather than a per-quest one. Quest tracking rules with bit 4 set on `clean` (use the `summary` keyword) skip immediate delivery; their matches are buffered until the user's `[summary_schedules]` active hours fire (or `!summary quest now` is invoked). At dispatch the buffered quests are grouped by `(rewardType, reward)` and rendered once per group.

The view passed to `questSummary` is shaped differently from a regular `quest` template: the reward fields (icon, translated name, count) live at the top level, and the per-pokestop entries live under the `quests` array. Per-entry fields mirror the regular `quest` view (see above), so `{{#each quests}}` rows can use `{{pokestopName}}`, `{{googleMapUrl}}`, `{{addr}}`, etc. just like a single-pokestop quest template. The only `questSummary`-specific per-entry field is `withAR`, which lets a row label AR-required quests separately.

| Field | Type | Description |
|-------|------|-------------|
| `rewardType` | int | Reward type ID (2=item, 3=stardust, 4=candy, 7=pokemon, 12=mega energy) |
| `reward` | int | Reward ID (item ID for type 2, dust amount for type 3, pokemon ID for types 4/7/12) |
| `rewardForm` | int | Pokemon form ID for `rewardType == 7` (so e.g. two different Spinda forms group separately). `0` for all other reward types. |
| `rewardName` | string | Translated reward name for the group header. Formatted to match the per-row reward strings from regular `quest` enrichment, **with amounts stripped** for types 2/4/12 because amounts vary across stops within a group. Examples: `"Spinda 01"` (type 7 + form, matches per-row `fullName`), `"Lapras Candy"` (type 4), `"Charizard Mega Energy"` (type 12), `"Razz Berry"` (type 2), `"1500 Stardust"` (type 3 — amount is included because it's part of the group key). |
| `imgUrl` | string | Reward icon URL — best used as a Discord thumbnail/image. Telegram's `/sendSticker` is stricter; use `stickerUrl` there. |
| `stickerUrl` | string | Reward sticker URL — sized and formatted for Telegram's sticker constraints. Use this for the Telegram `sticker` field. |
| `staticMap` | string | Multi-pin static map URL — autopositioned over the pokestops in **this chunk** only |
| `count` | int | Total number of pokestops in the reward group (across every chunk, not just this message) |
| `chunk` | int | 1-based index of this message when an oversized group is split across multiple messages. Always `1` when `chunks == 1`. |
| `chunks` | int | Total number of chunks the group was split into. Wrap chunk-suffix output in `{{#if (gt chunks 1)}}…{{/if}}` so single-message groups stay clean. |
| `quests` | array | Per-pokestop entries for **this chunk** — each carries the same fields as a regular `quest` template view (see [Quest](#quest-quest)) plus `withAR` |
| `quests[i].withAR` | bool | True if this pokestop's quest requires the AR scanner |

The static map is built via the `questSummary` tile type. Like every other tile type, the URL pattern is `/staticmap/poracle-questsummary`; map mode is configurable via `[geocoding.static_map_type] questSummary = "..."` if you want anything other than the default `staticMap`. Each chunk's map shows only the pokestops in that chunk so the bullet list and pins always match.

### Chunking

When a single reward group would render to a Discord embed bigger than the platform allows (description length, field count, or total embed size), the dispatcher splits the group into multiple messages. Each message gets its own `chunk`/`chunks`/`quests`/`staticMap`; `count` stays at the full group total so the header can read e.g. "60× Rare Candy (1/3)". A single-chunk group has `chunks == 1` — guard chunk-suffix output with `{{#if (gt chunks 1)}}…{{/if}}`.

`questSummary` messages are always fresh sends — edit-mode and reply-threading don't apply. The source rule's `clean` bit is OR'd across the constituent rules contributing to a single reward group, so the summary message for that group inherits clean-deletion if any rule had it enabled. The TTH used for clean-deletion is the latest `ExpiresAt` within the same reward group (the "summarised block" — the one logical message, or the chunks it splits into when oversized), so the message lives at least as long as the longest constituent quest. Different reward groups in the same dispatch compute their own clean + TTH independently.

---

## Invasion (`invasion`)

| Field | Type | Description |
|-------|------|-------------|
| `pokestopName` | string | Pokestop name (alias for `pokestop_name`) |
| `pokestopUrl` | string | Pokestop image URL (alias for `pokestop_url`) |
| `gruntTypeId` | int | Grunt type ID |
| `gruntName` | string | Translated grunt name |
| `gruntType` | string | English title-case grunt type for regular grunts (e.g. "Water"). Lowercase event name for events (e.g. "kecleon", "showcase", "gold-stop"). Use this for `{{#if (eq gruntType 'kecleon')}}` template dispatch — for displaying the localised name use `gruntTypeName`. |
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

## Incident (`incident`)

**When it fires**: invasion webhooks where `gruntTypeID == 0 && displayType >= 7`. Per the shipped `util.json` `pokestopEvent` map (matching PoracleJS):

| `displayType` | Event |
|---|---|
| `7` | Gold-Stop |
| `8` | Kecleon |
| `9` | Showcase |

Regular Team Rocket grunt invasions (`gruntTypeID > 0`) continue to use the `invasion` template.

The split is webhook-level: every matched user for an incident webhook gets the `incident` template; every matched user for a grunt webhook gets the `invasion` template. There is no per-user toggle.

A fallback `incident` entry is shipped in `fallbacks/dts.json`, so the template selection chain always finds something even if you have not yet added an `incident` entry to `config/dts/`. To customise, copy the entry from `fallbacks/dts.json` into `config/dts/` and edit from there.

### Incident-specific fields

These aliases are added on top of the pokestop / location / time / weather fields that are shared with the invasion template:

| Field | Type | Description |
|-------|------|-------------|
| `incidentTypeName` | string | Translated display-type label (e.g. "Gold Pokéstop", "Kecleon"). Alias for `gruntName`. |
| `displayType` | int | Numeric event identifier per util.json: `7`=Gold-Stop, `8`=Kecleon, `9`=Showcase. Alias for `displayTypeId`. Use for dispatch logic: `{{#if (eq displayType 8)}}Kecleon-specific text{{/if}}`. |
| `incidentEmoji` | string | Resolved per-platform emoji for the event icon. Alias for `gruntTypeEmoji`. |
| `color` | string | Event color hex for the Discord embed `color` field. Alias for `gruntTypeColor`. |

### Available pokestop / location / time fields

| Field | Type | Description |
|-------|------|-------------|
| `pokestopName` | string | Pokestop name (alias for `pokestop_name`) |
| `pokestopUrl` | string | Pokestop image URL (alias for `pokestop_url`) |
| `pokestopId` | string | Pokestop ID (alias for `pokestop_id`) |
| `displayTypeId` | int | Raw display type ID |
| `disappearTime` | string | Incident expiry time (formatted) |
| `time` | string | Alias for `disappearTime` |
| `expirationTimestamp` | int | Unix expiry timestamp (use with `<t:N:R>` in Discord) |

Time-remaining fields (`tthd`, `tthh`, `tthm`, `tths`) and all common fields (location, maps, weather) are also available — see the Common Fields section above.

Weather fields (`gameWeatherId`, `gameWeatherName`, `gameWeatherEmoji`) are available for incidents.

### Not available for incidents

Grunt and reward fields (`gruntName`, `gruntRewardsList`, `gruntLineupList`, `genderEmoji`, `gender`, etc.) are **not populated** for incidents. Use the aliased fields above (`incidentTypeName` instead of `gruntName`). For numeric dispatch use `{{displayType}}`; for slug-style dispatch use `{{gruntType}}` directly (e.g. `{{#if (eq gruntType "kecleon")}}`).

### Example

```json
{
  "id": 1,
  "type": "incident",
  "platform": "discord",
  "language": "en",
  "template": {
    "embed": {
      "title": "{{{incidentEmoji}}} {{incidentTypeName}} at {{{pokestopName}}}",
      "color": "{{color}}",
      "description": "Ends: {{disappearTime}} ({{#if tthh}}{{tthh}}h {{/if}}{{tthm}}m {{tths}}s)\n{{{addr}}}\n[Google]({{{googleMapUrl}}}) | [Apple]({{{appleMapUrl}}})",
      "thumbnail": { "url": "{{{imgUrl}}}" }
    }
  }
}
```

The `fallbacks/dts.json` incident entries (Discord and Telegram) serve as the starting sample — copy and customise from there.

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
| `weaknessEmoji` | string | Flat `"<value>x<typeEmoji>"` per category |
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

**Comparison:** `{{#eq a b}}`, `{{#is a b}}` (alias for `eq`), `{{#ne a b}}`, `{{#isnt a b}}` (alias for `ne`), `{{#gt a b}}`, `{{#lt a b}}`, `{{#gte a b}}`, `{{#lte a b}}`, `{{#and a b ...}}` (variadic), `{{#or a b ...}}` (variadic), `{{#neither a b ...}}` (variadic, inverse of or), `{{#oneOf value a b c ...}}` (value equals any of a/b/c — use this instead of `{{#or value a b}}` which is "any arg truthy"), `{{#not a}}`, `{{#contains collection value}}`, `{{#compare a "op" b}}`

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
