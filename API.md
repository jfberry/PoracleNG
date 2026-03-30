# PoracleNG API Reference

All API endpoints are available through the processor (default port 3030). The processor handles most endpoints directly and proxies remaining ones to the alerter transparently.

## Authentication

All `/api/*` endpoints require the `X-Poracle-Secret` header matching the configured `[alerter] api_secret` value. Health and metrics endpoints do not require authentication.

```bash
curl -H "X-Poracle-Secret: your-secret" http://localhost:3030/api/tracking/pokemon/123456789
```

## Response Format

All endpoints return JSON with a `status` field:

```json
{"status": "ok", ...}
{"status": "error", "message": "description of the problem"}
{"status": "authError", "reason": "incorrect or missing api secret"}
```

---

## Health & Monitoring

### GET /health

Health check.

```bash
curl http://localhost:3030/health
```

```json
{"status": "healthy"}
```

### GET /metrics

Prometheus metrics endpoint.

---

## Tracking CRUD

All tracking types follow the same 4-endpoint pattern. The `{id}` parameter is the user's Discord/Telegram ID.

### Common Query Parameters

| Parameter | Description |
|-----------|-------------|
| `profile_no` | Override the user's current profile number |
| `silent` | Suppress confirmation message to user |
| `suppressMessage` | Alias for `silent` |

### GET /api/tracking/{type}/{id}

List all tracking rules for a user. Returns rules with a human-readable `description` field.

**Types:** `pokemon`, `raid`, `egg`, `quest`, `invasion`, `lure`, `nest`, `gym`, `fort`, `maxbattle`

```bash
curl -H "X-Poracle-Secret: secret" http://localhost:3030/api/tracking/pokemon/123456789
```

```json
{
  "status": "ok",
  "pokemon": [
    {
      "uid": 42,
      "pokemon_id": 1,
      "form": 0,
      "min_iv": 90,
      "max_iv": 100,
      "distance": 500,
      "template": "1",
      "description": "**Bulbasaur** | iv: 90%-100% | cp: 0-9000 | ..."
    }
  ]
}
```

### POST /api/tracking/{type}/{id}

Create or update tracking rules. Accepts a single object or array. Returns counts of new/updated/unchanged rules and sends a confirmation message to the user (unless `silent`).

The endpoint uses smart diff logic: if an incoming rule matches an existing one on its key fields and only display settings (distance, template, clean) differ, it updates in place rather than creating a duplicate.

```bash
curl -X POST -H "X-Poracle-Secret: secret" -H "Content-Type: application/json" \
  http://localhost:3030/api/tracking/pokemon/123456789 \
  -d '[{"pokemon_id": 1, "min_iv": 90, "max_iv": 100, "distance": 500}]'
```

```json
{
  "status": "ok",
  "message": "New: **Bulbasaur** | iv: 90%-100% | ...",
  "newUids": [42],
  "alreadyPresent": 0,
  "updates": 0,
  "insert": 1
}
```

### DELETE /api/tracking/{type}/{id}/byUid/{uid}

Delete a single tracking rule by its unique ID.

```bash
curl -X DELETE -H "X-Poracle-Secret: secret" \
  http://localhost:3030/api/tracking/pokemon/123456789/byUid/42
```

```json
{"status": "ok"}
```

### POST /api/tracking/{type}/{id}/delete

Bulk delete tracking rules. Body is a JSON array of UIDs.

```bash
curl -X POST -H "X-Poracle-Secret: secret" -H "Content-Type: application/json" \
  http://localhost:3030/api/tracking/pokemon/123456789/delete \
  -d '[42, 43, 44]'
```

```json
{"status": "ok"}
```

### GET /api/tracking/all/{id}

Get all tracking rules across all types for the user's current profile.

```json
{
  "status": "ok",
  "human": {"id": "123", "name": "User", ...},
  "pokemon": [...],
  "raid": [...],
  "egg": [...],
  "quest": [...],
  "invasion": [...],
  "lure": [...],
  "nest": [...],
  "gym": [...],
  "maxbattle": [...],
  "fort": [...]
}
```

### GET /api/tracking/pokemon/refresh

Force a state reload (same as POST /api/reload).

---

## Type-Specific POST Fields

### Pokemon Tracking

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `pokemon_id` | int | 0 | Pokemon ID (0 = everything) |
| `form` | int | 0 | Form ID |
| `min_iv` | int | -1 | Minimum IV% (-1 = unencountered) |
| `max_iv` | int | 100 | Maximum IV% |
| `min_cp` | int | 0 | Minimum CP |
| `max_cp` | int | 9000 | Maximum CP |
| `min_level` | int | 0 | Minimum level |
| `max_level` | int | 40 | Maximum level |
| `atk` / `def` / `sta` | int | 0 | Minimum IVs |
| `max_atk` / `max_def` / `max_sta` | int | 15 | Maximum IVs |
| `gender` | int | 0 | Gender filter (0=any, 1=male, 2=female) |
| `size` / `max_size` | int | 0/5 | Size range (1=XXS to 5=XXL) |
| `rarity` / `max_rarity` | int | -1/6 | Rarity range (1=Common to 6=Unseen) |
| `pvp_ranking_league` | int | 0 | PVP league CP cap (500/1500/2500) |
| `pvp_ranking_best` / `pvp_ranking_worst` | int | | PVP rank range |
| `pvp_ranking_min_cp` | int | 0 | Minimum CP for PVP |
| `pvp_ranking_cap` | int | 0 | Level cap for PVP |
| `distance` | int | 0 | Distance in metres (0 = use area) |
| `template` | string | config default | DTS template name |
| `clean` | bool | false | Auto-delete message after TTH |

### Raid Tracking

Raid tracking supports three input modes. Each request object uses **one** of these:

**Mode 1: Track by level** — set `level` (pokemon_id defaults to 9000):
```json
{"level": 5, "team": 4, "distance": 500}
```

`level` can be a single int or an array — each level becomes a separate tracking rule:
```json
{"level": [1, 3, 5], "team": 4}
```

Use `level: 90` for all levels.

**Mode 2: Track specific pokemon** — set `pokemon_id` (level is ignored):
```json
{"pokemon_id": 150, "form": 0, "distance": 500}
```

**Mode 3: Track multiple pokemon** — use `pokemon_form` array. Each entry becomes a separate tracking rule with `level=9000`. Do NOT use `pokemon_id`/`form`/`level` with this mode:
```json
{"pokemon_form": [{"pokemon_id": 150, "form": 0}, {"pokemon_id": 151, "form": 0}], "distance": 500}
```

**Common fields (used with all modes):**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `team` | int | 4 | Gym team (0-3 specific, 4=any) |
| `exclusive` | bool | false | EX raid only |
| `move` | int | 9000 | Move ID filter (9000=any) |
| `evolution` | int | 9000 | Evolution filter |
| `gym_id` | string | null | Specific gym ID |
| `rsvp_changes` | int | 0 | RSVP mode (0=without, 1=including, 2=only) |
| `distance` | int | 0 | Distance in metres |
| `template` | string | config default | DTS template |
| `clean` | bool | false | Auto-delete |

### Egg Tracking

`level` is required. It can be a single int or an array — each level becomes a separate tracking rule. Use `90` for all levels.

```json
{"level": 5, "team": 4}
{"level": [1, 3, 5]}
{"level": 90}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `level` | int/int[] | required | Egg level(s) (90=all) |
| `team` | int | 4 | Gym team (0-3 specific, 4=any) |
| `exclusive` | bool | false | EX gym only |
| `gym_id` | string | null | Specific gym ID |
| `rsvp_changes` | int | 0 | RSVP mode (0=without, 1=including, 2=only) |
| `distance` | int | 0 | Distance |
| `template` | string | config default | DTS template |
| `clean` | bool | false | Auto-delete |

### Quest Tracking

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `reward_type` | int | required | Reward type (2=item, 3=stardust, 4=candy, 7=pokemon, 12=mega energy) |
| `reward` | int | 0 | Reward ID (pokemon ID, item ID, or stardust amount) |
| `form` | int | 0 | Form ID (for pokemon rewards) |
| `shiny` | bool | false | Shiny only |
| `amount` | int | 0 | Minimum reward amount |
| `distance` | int | 0 | Distance |
| `template` | string | config default | DTS template |
| `clean` | bool | false | Auto-delete |

### Invasion Tracking

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `grunt_type` | string | "" | Grunt type name (empty=any) |
| `gender` | int | 0 | Gender (0=any, 1=male, 2=female) |
| `distance` | int | 0 | Distance |
| `template` | string | config default | DTS template |
| `clean` | bool | false | Auto-delete |

### Lure Tracking

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `lure_id` | int | 0 | Lure type (0=any, 501-506 specific) |
| `distance` | int | 0 | Distance |
| `template` | string | config default | DTS template |
| `clean` | bool | false | Auto-delete |

### Nest Tracking

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `pokemon_id` | int | 0 | Pokemon ID (0=everything) |
| `form` | int | 0 | Form ID |
| `min_spawn_avg` | int | 0 | Minimum spawns per hour |
| `distance` | int | 0 | Distance |
| `template` | string | config default | DTS template |
| `clean` | bool | false | Auto-delete |

### Gym Tracking

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `team` | int | required | Team (0-3 specific, 4=any) |
| `slot_changes` | bool | false | Track slot changes |
| `battle_changes` | bool | false | Track battle changes |
| `gym_id` | string | null | Specific gym ID |
| `distance` | int | 0 | Distance |
| `template` | string | config default | DTS template |
| `clean` | bool | false | Auto-delete |

### Fort Update Tracking

```json
{"fort_type": "pokestop", "change_types": ["name", "location"], "distance": 500}
{"fort_type": "everything", "include_empty": true}
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `fort_type` | string | "everything" | Type: "pokestop", "gym", or "everything" |
| `include_empty` | bool | false | Include changes with no name/description |
| `change_types` | string[] | [] | Change types to track: "location", "name", "image_url", "removal", "new" |
| `distance` | int | 0 | Distance |
| `template` | string | config default | DTS template |

### Max Battle Tracking

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `pokemon_id` | int | 9000 | Pokemon ID (9000=any) |
| `level` | int | required if pokemon_id=9000 | Battle level |
| `form` | int | 0 | Form ID |
| `move` | int | 9000 | Move filter (9000=any) |
| `gmax` | int | 0 | Gigantamax filter |
| `evolution` | int | 9000 | Evolution filter |
| `station_id` | string | null | Specific station ID |
| `distance` | int | 0 | Distance |
| `template` | string | config default | DTS template |
| `clean` | bool | false | Auto-delete |

---

## Human Management

### GET /api/humans/{id}

Get available geofence areas for a user. Areas are filtered by community membership if area security is enabled.

```json
{
  "status": "ok",
  "areas": [
    {"name": "Canterbury", "group": "Kent", "description": "", "userSelectable": true},
    {"name": "Dover", "group": "Kent", "description": "Port area", "userSelectable": true}
  ]
}
```

### GET /api/humans/one/{id}

Get full human record.

```json
{
  "status": "ok",
  "human": {
    "id": "123456789",
    "name": "UserName",
    "type": "discord:user",
    "enabled": true,
    "area": "[\"canterbury\"]",
    "latitude": 51.28,
    "longitude": 1.08,
    "language": "en",
    "current_profile_no": 1
  }
}
```

### POST /api/humans

Create a new user.

```bash
curl -X POST -H "X-Poracle-Secret: secret" -H "Content-Type: application/json" \
  http://localhost:3030/api/humans \
  -d '{"id": "123456789", "name": "NewUser", "type": "discord:user"}'
```

Optional fields: `enabled`, `area`, `latitude`, `longitude`, `language`, `admin_disable`, `community`, `profile_name`, `notes`.

### POST /api/humans/{id}/start

Enable a user.

### POST /api/humans/{id}/stop

Disable a user.

### POST /api/humans/{id}/adminDisabled

Toggle admin disable flag.

```json
{"state": true}
```

### POST /api/humans/{id}/setLocation/{lat}/{lon}

Update user location. Validates against area restrictions if area security is enabled.

### GET /api/humans/{id}/checkLocation/{lat}/{lon}

Check if a location is within the user's allowed areas.

```json
{"status": "ok", "locationOk": true}
```

### POST /api/humans/{id}/setAreas

Set user's selected geofence areas. Body is a JSON array of area names. Areas are validated against the user's community membership.

```bash
curl -X POST -H "X-Poracle-Secret: secret" -H "Content-Type: application/json" \
  http://localhost:3030/api/humans/123456789/setAreas \
  -d '["canterbury", "dover"]'
```

```json
{"status": "ok", "setAreas": ["canterbury", "dover"]}
```

### POST /api/humans/{id}/switchProfile/{profile}

Switch the user's active profile.

### GET /api/humans/{id}/roles

List Discord roles across all configured guilds.

### POST /api/humans/{id}/roles/add/{roleId}

Add a Discord role to a user.

### POST /api/humans/{id}/roles/remove/{roleId}

Remove a Discord role from a user.

### GET /api/humans/{id}/getAdministrationRoles

Get the user's delegated administration permissions (channels, webhooks, user tracking).

---

## Profile Management

### GET /api/profiles/{id}

List all profiles for a user.

```json
{
  "status": "ok",
  "profile": [
    {"id": "123", "profile_no": 1, "name": "Home", "area": "[\"canterbury\"]", "latitude": 51.28, "longitude": 1.08, "active_hours": ""}
  ]
}
```

### POST /api/profiles/{id}/add

Create new profile(s). Body is a single object or array.

```json
{"name": "Work", "active_hours": "[{\"day\":1,\"hours\":9,\"mins\":0}]"}
```

### POST /api/profiles/{id}/update

Update active_hours on profiles.

```json
[{"profile_no": 2, "active_hours": "[{\"day\":1,\"hours\":\"17\",\"mins\":\"00\"}]"}]
```

### POST /api/profiles/{id}/copy/{from}/{to}

Copy all tracking rules from one profile to another.

### DELETE /api/profiles/{id}/byProfileNo/{profile_no}

Delete a profile and all its tracking rules.

---

## Geofence Data & Tiles

### GET /api/geofence/all

All geofences with metadata.

### GET /api/geofence/all/hash

MD5 hash of each geofence path (for cache validation).

### GET /api/geofence/all/geojson

All geofences as a GeoJSON FeatureCollection.

### GET /api/geofence/{area}/map

Generate a static map tile showing a geofence area polygon.

```json
{"status": "ok", "url": "http://tileserver:9000/staticmap/pregenerated/abc123.png"}
```

### GET /api/geofence/distanceMap/{lat}/{lon}/{distance}

Generate a static map tile showing a distance circle.

### GET /api/geofence/locationMap/{lat}/{lon}

Generate a static map tile showing a location pin.

### POST /api/geofence/overviewMap

Generate a static map tile showing multiple geofence areas with rainbow colours.

```json
{"areas": ["canterbury", "dover", "folkestone"]}
```

### GET /api/geofence/weatherMap/{lat}/{lon}

Generate a static map tile showing the weather S2 cell at a location. Optional query param `weather` for the weather condition ID.

### GET/POST /api/geofence/reload

Trigger a full geofence reload (re-fetches Koji geofences and reloads state).

---

## State Management

### GET/POST /api/reload

Reload tracking rules from the database (preserves geofences). Called automatically after tracking mutations.

---

## Statistics

### GET /api/stats/rarity

Rarity group statistics from the rolling window.

### GET /api/stats/shiny

Shiny encounter statistics.

### GET /api/stats/shiny-possible

Shiny-possible spawn data.

---

## Weather

### GET /api/weather?cell={cellId}

Get weather data for a specific S2 cell.

---

## Configuration

### GET /api/config/poracleWeb

Server configuration for the web UI (locale, prefix, PVP settings, admin lists, etc.).

### GET /api/config/templates

Available DTS templates by platform, type, and language.

---

## Game Data

### GET /api/masterdata/monsters

All pokemon with names, forms, and types.

### GET /api/masterdata/grunts

All invasion grunt types.

---

## Geocoding

### GET /api/geocode/forward?q={query}

Forward geocode a location name to coordinates.

```json
[{"latitude": 51.28, "longitude": 1.08, "city": "Canterbury", "country": "United Kingdom"}]
```

---

## Confirmation Messages

### POST /api/postMessage

Send a confirmation message to a user via Discord or Telegram. Used internally by the processor for API operation confirmations (e.g. tracking added/removed). Handled by the alerter (proxied through the processor).

```json
[{
  "target": "123456789",
  "type": "discord:user",
  "name": "UserName",
  "message": {"content": "Hello from Poracle"},
  "tth": {"hours": 1, "minutes": 0, "seconds": 0},
  "clean": false,
  "language": "en"
}]
```

---

## Test

### POST /api/test

Simulate a webhook for testing DTS templates. Used by the `!poracle-test` command.

```json
{
  "type": "pokemon",
  "webhook": {"pokemon_id": 1, "latitude": 51.28, "longitude": 1.08, ...},
  "target": {"id": "123", "name": "User", "type": "discord:user", "language": "en", "template": "1"}
}
```
