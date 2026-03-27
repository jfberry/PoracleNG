# Tileserver Template Reference

This document lists the fields available in each tileservercache template. Templates are in `alerter/tileservercache_templates/` and are named `poracle-{type}.json` (or `poracle-multi-{type}.json` for multiStaticMap).

The processor generates static map tiles by POSTing JSON data to the tileserver. Only the fields listed below are sent — your template should only reference these fields.

## Common Fields (all alert types)

These fields are included in every pregenerate tile request:

| Field | Type | Description |
|-------|------|-------------|
| `latitude` | float | Alert latitude |
| `longitude` | float | Alert longitude |
| `imgUrl` | string | Primary icon URL (pokemon, raid egg, weather, etc.) |
| `imgUrlAlt` | string | Alternative icon URL |
| `nightTime` | bool | Whether it's currently night at the alert location |
| `duskTime` | bool | Whether it's currently dusk |
| `dawnTime` | bool | Whether it's currently dawn |
| `style` | string | Map style override (day/dawn/dusk/night based on time) |

For non-pregenerate (URL query parameter) mode, only the fields listed under "Non-pregen keys" are sent.

## Additional Fields: `nearbyStops`

If `include_stops = true` is configured in `[geocoding.tileserver_settings]` for a tile type, the following is also included:

| Field | Type | Description |
|-------|------|-------------|
| `nearbyStops` | array | Array of nearby pokestops and gyms within the tile bounds |
| `nearbyStops[].latitude` | float | Stop/gym latitude |
| `nearbyStops[].longitude` | float | Stop/gym longitude |
| `nearbyStops[].type` | string | `"stop"` or `"gym"` |
| `nearbyStops[].teamId` | int | Gym team ID (0-3, only for gyms) |
| `nearbyStops[].slots` | int | Available gym slots (only for gyms) |
| `nearbyStops[].imgUrl` | string | Gym icon URL (only for gyms) |
| `uiconPokestopUrl` | string | Default pokestop icon URL |

---

## Pokemon (`poracle-monster` / `poracle-multi-monster`)

| Field | Type | Description |
|-------|------|-------------|
| `pokemon_id` | int | Pokemon ID |
| `display_pokemon_id` | int | Display pokemon ID (for ditto transforms) |
| `form` | int | Form ID |
| `costume` | int | Costume ID |
| `pokemonId` | int | Alias for pokemon_id |
| `generation` | int | Pokemon generation number |
| `weather` | int | Boosted weather ID |
| `confirmedTime` | bool | Whether disappear time is verified |
| `shinyPossible` | bool | Whether the pokemon can be shiny |
| `seenType` | string | How the pokemon was seen (`"wild"`, `"nearby_cell"`, etc.) |
| `seen_type` | string | Alias for seenType |
| `cell_coords` | array | S2 cell corner coordinates (for cell-spawned pokemon) |
| `verified` | bool | Alias for confirmedTime |

**Non-pregen keys:** `pokemon_id`, `latitude`, `longitude`, `form`, `costume`, `imgUrl`, `imgUrlAlt`, `style`

---

## Raid (`poracle-raid`)

| Field | Type | Description |
|-------|------|-------------|
| `pokemon_id` | int | Pokemon ID (0 = egg, no pokemon hatched yet) |
| `form` | int | Form ID |
| `level` | int | Raid level (1-7+) |
| `teamId` | int | Gym team ID (0=uncontested, 1=mystic, 2=valor, 3=instinct) |
| `evolution` | int | Evolution ID (for mega raids) |
| `costume` | int | Costume ID |

**Non-pregen keys:** `pokemon_id`, `latitude`, `longitude`, `form`, `level`, `teamId`, `evolution`, `costume`, `imgUrl`, `imgUrlAlt`, `style`

**Example template logic:**
```
#if(pokemon_id == 0):egg by level#else:pokemon#endif
#if(evolution > 0):mega pokemon#endif
#if(costume > 0):costumed pokemon#endif
```

---

## Pokestop / Invasion / Lure (`poracle-pokestop`)

Fields are only present when non-zero. Use `!= nil` checks in templates.

| Field | Type | Present when | Description |
|-------|------|-------------|-------------|
| `gruntTypeId` | int | Invasion active | Grunt type ID |
| `displayTypeId` | int | Event stop | Display type (7=gold stop, 8=kecleon, 9=showcase) |
| `lureTypeId` | int | Lured stop | Lure module ID (501-506) |

**Important:** These fields are omitted (nil) when zero, not sent as `0`. Use `#if(gruntTypeId != nil)` not `#if(gruntTypeId != 0)`.

**Non-pregen keys:** `latitude`, `longitude`, `imgUrl`, `gruntTypeId`, `displayTypeId`, `style`

---

## Quest (`poracle-quest`)

No type-specific fields beyond the common set. The quest icon is in `imgUrl`.

**Non-pregen keys:** `latitude`, `longitude`, `imgUrl`, `style`

---

## Gym (`poracle-gym`)

| Field | Type | Description |
|-------|------|-------------|
| `team_id` | int | Gym team ID (0-3) |
| `slotsAvailable` | int | Number of available defender slots |
| `inBattle` | bool | Whether the gym is currently in battle |
| `ex` | bool | Whether the gym is EX raid eligible |

**Non-pregen keys:** `latitude`, `longitude`, `imgUrl`, `team_id`, `style`

---

## Nest (`poracle-nest`)

| Field | Type | Description |
|-------|------|-------------|
| `pokemon_id` | int | Nesting pokemon ID |
| `form` | int | Form ID |
| `pokemonSpawnAvg` | float | Average spawns per hour |

**Non-pregen keys:** `pokemon_id`, `latitude`, `longitude`, `form`, `imgUrl`, `style`

---

## Weather (`poracle-weather`)

| Field | Type | Description |
|-------|------|-------------|
| `gameplay_condition` | int | Weather condition ID (1-7) |
| `coords` | array | S2 level-10 cell corner coordinates `[[lat,lon], ...]` |
| `activePokemons` | array | Affected pokemon list (when `show_altered_pokemon_static_map` enabled) |
| `activePokemons[].latitude` | float | Pokemon latitude |
| `activePokemons[].longitude` | float | Pokemon longitude |
| `activePokemons[].imgUrl` | string | Pokemon icon URL |

**Non-pregen keys:** `latitude`, `longitude`, `gameplay_condition`, `coords`, `activePokemons`, `imgUrl`, `style`

---

## Max Battle (`poracle-maxbattle`)

| Field | Type | Description |
|-------|------|-------------|
| `battle_level` | int | Max battle level |
| `battle_pokemon_id` | int | Battle pokemon ID |

**Non-pregen keys:** `latitude`, `longitude`, `imgUrl`, `battle_level`, `style`

---

## Fort Update (`poracle-fort-update`)

| Field | Type | Description |
|-------|------|-------------|
| `isEditLocation` | bool | Whether this is a location change |
| `fortType` | string | `"pokestop"` or `"gym"` |
| `map_latitude` | float | Autopositioned map center latitude |
| `map_longitude` | float | Autopositioned map center longitude |
| `oldLatitude` | float | Previous latitude (for location changes) |
| `oldLongitude` | float | Previous longitude (for location changes) |
| `zoom` | float | Autopositioned zoom level |

**Note:** Fort update does NOT use `pregenBase` — it has its own field list without `imgUrl`.

**Non-pregen keys:** `latitude`, `longitude`, `isEditLocation`, `fortType`, `map_latitude`, `map_longitude`, `oldLatitude`, `oldLongitude`, `zoom`, `style`

---

## Geofence Tiles (API-generated)

These are generated by the processor's tile API endpoints, not by webhook enrichment.

### Area (`poracle-area`)

| Field | Type | Description |
|-------|------|-------------|
| `zoom` | float | Autopositioned zoom level |
| `polygons` | array | Array of polygon paths `[[[lat,lon], ...], ...]` |

### Area Overview (`poracle-areaoverview`)

| Field | Type | Description |
|-------|------|-------------|
| `zoom` | float | Autopositioned zoom level |
| `fences` | array | Array of `{color: "#hex", path: [[lat,lon], ...]}` |

### Distance (`poracle-distance`)

| Field | Type | Description |
|-------|------|-------------|
| `zoom` | float | Autopositioned zoom level |
| `distance` | float | Radius in metres |

### Location (`poracle-location`)

Only common fields (`latitude`, `longitude`).

### Weather Map (`poracle-weather` via API)

Same fields as webhook weather tile above. The weather condition can be passed as a `?weather=N` query parameter.

---

## Tileserver Settings

Per-type tile settings are configured in `[geocoding.tileserver_settings]`:

```toml
[geocoding.tileserver_settings.default]
type = "staticMap"           # or "multiStaticMap"
width = 500
height = 250
zoom = 15
pregenerate = true
include_stops = false

[geocoding.tileserver_settings.monster]
type = "multiStaticMap"
include_stops = true

[geocoding.tileserver_settings.raid]
# empty = inherits from default
```

- `type = "staticMap"` → template name is `poracle-{type}`
- `type = "multiStaticMap"` → template name is `poracle-multi-{type}`
- `pregenerate = true` → POST with JSON body, all pregen fields sent
- `pregenerate = false` → GET with query parameters, only non-pregen keys sent
- `include_stops = true` → adds `nearbyStops` array to the tile data
- Empty sections inherit from `[geocoding.tileserver_settings.default]`
