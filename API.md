# PoracleNG API Reference

All API endpoints are available through the processor (default port 3030). The processor handles all endpoints directly.

## Contents

- [Authentication](#authentication)
- [Response Format](#response-format)
- [Health & Monitoring](#health--monitoring)
- [Tracking CRUD](#tracking-crud)
- [Type-Specific POST Fields](#type-specific-post-fields)
- [Human Management](#human-management)
- [Profile Management](#profile-management)
- [Geofence Data & Tiles](#geofence-data--tiles)
- [State Management](#state-management)
- [Statistics](#statistics)
- [Weather](#weather)
- [Configuration](#configuration)
- [DTS Editor](#dts-editor) — includes [`/api/dts/reload`](#getpost-apidtsreload)
- [Config Editor](#config-editor)
- [Game Data](#game-data)
- [Geocoding](#geocoding)
- [Confirmation Messages](#confirmation-messages)
- [Test](#test)

### Reload endpoints at a glance

| Endpoint | What it reloads |
|----------|-----------------|
| [`/api/reload`](#getpost-apireload) | Tracking rules from the database. Keeps current geofences and DTS templates in place. |
| [`/api/geofence/reload`](#getpost-apigeofencereload) | Geofence files on disk **and re-fetches Koji geofences**, then reloads tracking rules. Use after editing fences or rotating a Koji bearer token. |
| [`/api/dts/reload`](#getpost-apidtsreload) | DTS templates and partials from `config/dts.json`, `config/dts/`, and `config/partials.json`. Does **not** refresh Koji or tracking rules. |

## Authentication

All `/api/*` endpoints require the `X-Poracle-Secret` header matching the configured `[processor] api_secret` value (with `[alerter] api_secret` as a backward-compatible fallback). Health and metrics endpoints do not require authentication.

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

Available DTS templates by platform, type, and language (metadata only, no template content).

---

## DTS Editor

All DTS editor endpoints require the `X-Poracle-Secret` header. CORS is enabled globally.

### GET /api/dts/templates

Returns DTS template entries with full template content. Filterable by query parameters. Each entry includes a `readonly` flag — entries from `fallbacks/dts.json` are readonly.

| Parameter | Description |
|-----------|-------------|
| `type` | Filter by DTS type (monster, raid, egg, etc.) |
| `platform` | Filter by platform (discord, telegram) |
| `language` | Filter by language code (en, de, etc.) or empty for language-independent |
| `id` | Filter by template ID |

When a user has any non-readonly entry for a given (type, platform), fallback entries for that combo are suppressed — the user has taken ownership in the editor.

For entries using `templateFile` (external Handlebars files), the response includes `templateFileContent` with the resolved raw file content. Entries with inline `template` objects do not have this field.

```json
{
  "status": "ok",
  "templates": [
    {
      "id": "1",
      "type": "monster",
      "platform": "discord",
      "language": "",
      "default": true,
      "readonly": true,
      "template": {"embed": {"title": "{{round iv}}% {{fullName}} ..."}},
      "name": "Default Monster",
      "description": "Standard pokemon alert"
    },
    {
      "id": "1",
      "type": "fort-update",
      "platform": "discord",
      "language": "",
      "default": true,
      "templateFile": "dts/fort_update.txt",
      "templateFileContent": "{{#eq fortType 'pokestop'}}..raw handlebars..{{/eq}}"
    }
  ]
}
```

**Editor note:** Entries with `templateFile` use raw Handlebars text (not JSON). The editor should display these differently from inline `template` entries and use `PUT /api/dts/templates/file` to save changes to the file content.

### POST /api/dts/templates

Save DTS template entries. Accepts a JSON array of entries. Each entry requires at minimum `type`, `platform`, and `template`.

**Save behaviour:**
- Each entry is saved to its own file in `config/dts/` (e.g., `monster-1-discord.json`)
- If the entry previously existed in another file (`config/dts.json` or another `config/dts/*.json`), it is removed from the old file (other entries in that file are preserved)
- Saving a **readonly** entry (from fallbacks) creates an override copy in `config/dts/` — the fallback is not modified, and the override takes precedence via the loading order
- The `id` field defaults to empty if not provided (matches the "default" template)

**Required fields per entry:** `type`, `platform`

```bash
curl -X POST -H "X-Poracle-Secret: secret" -H "Content-Type: application/json" \
  http://localhost:3030/api/dts/templates \
  -d '[{
    "id": "1",
    "type": "monster",
    "platform": "discord",
    "language": "en",
    "default": false,
    "template": {"embed": {"title": "{{round iv}}% {{fullName}}"}}
  }]'
```

**Success:**
```json
{"status": "ok", "saved": 1}
```

**Errors:**
```json
{"status": "error", "message": "no templates provided"}
{"status": "error", "message": "entry 0 missing required fields (type=\"\", platform=\"\", id=\"\")"}
```

### DELETE /api/dts/templates

Delete a DTS template entry. Removes from memory and from the source file on disk. Readonly entries (from fallbacks) cannot be deleted.

| Parameter | Description |
|-----------|-------------|
| `type` | DTS type (required) |
| `platform` | Platform (required) |
| `id` | Template ID (required) |
| `language` | Language (empty string matches language-independent entries) |

**Errors:**
```json
{"status": "error", "message": "template not found"}
{"status": "error", "message": "template monster/discord/1/ is readonly"}
```

### PUT /api/dts/templates/file

Update the raw content of a `templateFile` entry. The file path is resolved from the template's key fields — no client-supplied paths are accepted, preventing path traversal. Readonly entries are rejected.

| Parameter | Description |
|-----------|-------------|
| `type` | DTS type (required) |
| `platform` | Platform (required) |
| `id` | Template ID (required) |
| `language` | Language code |

**Request body:**
```json
{"content": "{{#eq fortType 'pokestop'}}...raw handlebars text...{{/eq}}"}
```

**Response:**
```json
{"status": "ok", "templateFile": "dts/fort_update.txt"}
```

**Errors:**
```json
{"status": "error", "message": "template not found"}
{"status": "error", "message": "template uses inline JSON, not a templateFile"}
{"status": "error", "message": "template is readonly (bundled default)"}
```

### GET /api/dts/emoji

Returns the emoji lookup map for template editing. Emojis come from `util.json` (defaults) overlaid with `emoji.json` (per-platform custom overrides). Used for resolving `{{getEmoji 'key'}}` in the editor and presenting an emoji picklist.

**Per-platform merged map** (what the renderer uses):
```
GET /api/dts/emoji?platform=discord
```
```json
{
  "status": "ok",
  "platform": "discord",
  "emoji": {"team_0": "<:team_unknown:123>", "weather_1": "☀️"}
}
```

**Full structure** (for UIs showing customised vs default):
```
GET /api/dts/emoji
```
```json
{
  "status": "ok",
  "defaults": {"team_0": "❓", "weather_1": "☀️"},
  "platforms": {
    "discord": {"team_0": "<:team_unknown:123>"},
    "telegram": {}
  }
}
```

### POST /api/dts/enrich

Run a raw webhook through the enrichment pipeline and return the enriched variable map — the same data that Handlebars templates see during rendering. Includes all layers: base enrichment, translated fields, PVP display, aliases, resolved emoji, and computed fields (tthh/tthm, areas, weatherChange, etc.).

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `type` | string | yes | | Webhook type: pokemon, raid, egg, quest, invasion, lure, nest, gym, fort_update, max_battle |
| `webhook` | object | yes | | Raw webhook payload (same format as Golbat sends) |
| `language` | string | no | "en" | Language code for translations |
| `platform` | string | no | "discord" | Platform for emoji resolution (discord or telegram) |

```bash
curl -X POST -H "X-Poracle-Secret: secret" -H "Content-Type: application/json" \
  http://localhost:3030/api/dts/enrich \
  -d '{"type":"pokemon","webhook":{"pokemon_id":129,"latitude":51.28,"longitude":1.08,"disappear_time":9999999999,"individual_attack":15,"individual_defense":15,"individual_stamina":15},"language":"en","platform":"discord"}'
```

```json
{
  "status": "ok",
  "variables": {
    "name": "Magikarp",
    "fullName": "Magikarp",
    "pokemonId": 129,
    "iv": 100,
    "cp": 212,
    "level": 27,
    "quickMoveName": "Splash",
    "chargeMoveName": "Struggle",
    "pvpGreat": [...],
    "pvpUltra": [...]
  }
}
```

### GET /api/dts/fields/:type

Returns available template fields, block scopes, and insertable snippets for a DTS type.

**Field properties:** `name`, `type`, `description`, `category`, `preferred` (recommended for new templates), `deprecated` (use preferredAlternative instead), `rawWebhook` (direct from scanner, prefer enriched equivalent).

**Block scopes:** describe what fields are available inside block helpers like `{{#each pvpGreat}}`. Each scope lists `iterableFields` (which arrays it applies to) and `fields` (what's available on each item). Scopes are per-iterable — `pvpGreat` items have different fields from `weaknessList` items.

**Snippets:** pre-made Handlebars expressions for quick insertion. Each has `label`, `insert` (the text to insert), `description`, `category`, and optional `platform` (`"discord"`, `"telegram"`, or omitted for all). Uses single quotes (Poracle convention).

```json
{
  "status": "ok",
  "type": "monster",
  "fields": [
    {"name": "fullName", "type": "string", "description": "Name + form combined", "category": "identity", "preferred": true},
    {"name": "pokemon_id", "type": "int", "description": "Pokemon ID (webhook)", "category": "identity", "rawWebhook": true, "preferredAlternative": "pokemonId"},
    {"name": "despawnTimestamp", "type": "int", "description": "Unix despawn timestamp (for Discord <t:N:R>)", "category": "time"}
  ],
  "blockScopes": [
    {"helper": "each", "args": ["pvpGreat"], "iterableFields": ["pvpGreat","pvpUltra","pvpLittle"], "description": "Iterate over a PVP league display list", "fields": [...]},
    {"helper": "each", "args": ["weaknessList"], "iterableFields": ["weaknessList"], "description": "Iterate over weakness categories", "fields": [...]},
    {"helper": "each", "args": ["evolutions"], "iterableFields": ["evolutions"], "description": "Iterate over evolution chain entries", "fields": [...]},
    {"helper": "pokemon", "args": ["id","form"], "description": "Pokemon data block helper", "fields": [...]},
    {"helper": "getPowerUpCost", "args": ["levelStart","levelEnd"], "description": "Power-up cost between two levels", "fields": [...]}
  ],
  "snippets": [
    {"label": "Round IV", "insert": "{{round iv}}", "description": "IV rounded to integer", "category": "pokemon"},
    {"label": "IV or 💯", "insert": "{{#isnt iv 100}}{{round iv}}%{{else}}💯{{/isnt}}", "description": "Show IV% or 💯 for hundos", "category": "pokemon"},
    {"label": "Countdown", "insert": "<t:{{despawnTimestamp}}:R>", "description": "Discord relative countdown", "category": "pokemon", "platform": "discord"},
    {"label": "getEmoji", "insert": "{{getEmoji 'key'}}", "description": "Look up emoji by key", "category": "emoji"}
  ]
}
```

Types: monster, monsterNoIv, raid, egg, quest, invasion, lure, nest, gym, fort-update, maxbattle, weatherchange, greeting.

### GET /api/dts/fields

Returns the list of all available DTS type names as a string array.

### GET /api/dts/partials

Returns Handlebars partials for client-side template rendering. Register these with the Handlebars engine before rendering templates that use `{{> partialName}}`.

```json
{"status": "ok", "partials": {"remainingTime": "{{#if tthh}}{{tthh}}h{{/if}}{{tthm}}m{{tths}}s"}}
```

### POST /api/dts/sendtest

Compile a template with provided variables and deliver the rendered message to a Discord/Telegram user. Used by the editor to preview exactly what Discord/Telegram will show.

| Field | Type | Required | Default | Description |
|-------|------|----------|---------|-------------|
| `template` | object | yes | | DTS template object (same format as in dts.json) |
| `variables` | object | yes | | Enriched variable map (from /api/dts/enrich) |
| `target.id` | string | yes | | Discord user/channel ID or Telegram chat ID |
| `target.type` | string | no | "discord:user" | Destination type (discord:user, discord:channel, telegram:user, telegram:group) |
| `platform` | string | no | "discord" | Platform for rendering |
| `language` | string | no | "en" | Language for rendering |

```json
{"status": "ok", "message": "sent"}
```

### GET /api/dts/testdata

Returns test webhook scenarios from `testdata.json`. The editor can use these as sample payloads for the enrich endpoint. Config dir entries override fallback entries by type+test key.

| Parameter | Description |
|-----------|-------------|
| `type` | Filter by webhook type (pokemon, raid, pokestop, gym, max_battle, quest, fort_update) |

```json
{
  "status": "ok",
  "testdata": [
    {"type": "pokemon", "test": "hundo", "location": "current", "webhook": {...}},
    {"type": "pokemon", "test": "great-rank1", "location": "current", "webhook": {...}},
    {"type": "pokemon", "test": "shiny", "location": "current", "webhook": {...}}
  ]
}
```

Available test scenarios: boring, hundo, great-rank1, great-rank9, ultra1, unencountered, boosted, shiny (pokemon); egg1, level1, egg5, level5, egg6, level3 (raid); invasion, lure, giovanni, kecleon, goldstop, goldlure, showcase, pokemoncontest (pokestop); teamchange (gym); level1, level3 (max_battle); quest-item, quest-stardust, quest-pokemon, quest-energy (quest); edit, new, remove, etc. (fort_update).

### GET/POST /api/dts/reload

Reload DTS templates and partials from disk (`config/dts.json`, `config/dts/*.json`, `config/partials.json`, plus the shipped fallbacks). Use after editing files directly or after saving via the API if you want to pick up changes from other sources.

Scope is DTS only — this does **not** re-fetch Koji geofences or reload tracking rules. Use [`/api/geofence/reload`](#getpost-apigeofencereload) for a Koji refresh and [`/api/reload`](#getpost-apireload) for the DB tracking state.

```json
{"status": "ok"}
```

---

## Config Editor

All config editor endpoints require the `X-Poracle-Secret` header. Sensitive settings (database, tokens, bind addresses) are excluded — they remain TOML-only.

### GET /api/config/schema

Returns the config schema with field metadata for the editor. Each field includes:

| Property | Description |
|----------|-------------|
| `name` | TOML field name |
| `type` | `string`, `int`, `float`, `bool`, `string[]`, `int[]`, `color[]`, `select`, `map` |
| `default` | Default value |
| `description` | Help text |
| `hotReload` | `true` if changes take effect immediately, `false` if restart needed |
| `sensitive` | `true` for fields masked in values response |
| `deprecated` | `true` if the field/option is no longer recommended — editor should warn or hide unless already set |
| `advanced` | `true` if the field should be hidden behind a "show advanced" toggle |
| `hideDefault` | `true` if the editor should NOT pre-fill the default value (e.g. fallback URLs that the user shouldn't normally see) |
| `minLength`, `maxLength` | For array types: minimum/maximum number of entries (e.g., `iv_colors` requires exactly 6) |
| `resolve` | ID resolution hint: `discord:user`, `discord:role`, `discord:channel`, `discord:guild`, `discord:target`, `discord:user\|role`, `telegram:chat`, `geofence:area`, `destination` |
| `options` | For `select` type: `[{value, label, description, deprecated?}]` |
| `dependsOn` | Visibility dependency: `{field, value}` — hide when parent field doesn't match |

**Type notes:**
- `color[]` — array of CSS hex colour strings (e.g., `iv_colors`); editor should render colour pickers
- `int[]` — array of integers (e.g., `pvp.level_caps`)
- `map` — `map[string]any`; the field's `resolve` hint applies to the values where appropriate (e.g., `command_security` values are user/role IDs)

**Deprecated handling:** Field-level `deprecated: true` means the entire field is deprecated. Option-level `deprecated: true` (inside `options`) means a specific select value is deprecated but the field itself is fine. Editor behaviour suggestion: hide deprecated items unless they're already set, in which case show them with a warning badge.

Response is grouped by `sections`, each with `fields` and optional `tables` (array-of-tables like delegated_admins, communities, role_subscriptions).

### GET /api/config/values

Returns current merged config values (TOML + overrides) plus a list of fields that are currently overridden by `config/overrides.json`. The editor uses `overridden` to display badges showing which fields come from the web editor vs the user's `config.toml`. Only web-editable fields. Sensitive fields are masked.

| Parameter | Description |
|-----------|-------------|
| `section` | (optional) Return only this section |

```json
{
  "status": "ok",
  "values": {
    "general": {"locale": "en", "max_pokemon": 0},
    "discord": {"admins": ["344179542874914817"], "check_role": true}
  },
  "overridden": ["discord.admins", "alert_limits.dm_limit"]
}
```

The processor also logs a prominent banner at startup listing every field overridden by `overrides.json` — so users editing `config.toml` directly can see at a glance which of their values are being shadowed.

### POST /api/config/values

Save config changes. Accepts partial updates — only changed fields. Writes to `config/overrides.json` (never modifies config.toml). Hot-reloadable settings are applied immediately.

**Sensitive field handling:** Fields marked sensitive in the schema are returned as `"****"` by `GET /values`. When you POST a value of `"****"` for a sensitive field, the processor silently strips it before saving — preserving the existing secret. This lets the editor resubmit a whole form without wiping secrets the user didn't change. To actually update a secret, send a real value.

```json
{
  "discord": {"admins": ["344179542874914817", "999888777"]},
  "alert_limits": {"dm_limit": 30}
}
```

**Response:**
```json
{
  "status": "ok",
  "saved": 2,
  "restart_required": false
}
```

If any changed field requires restart:
```json
{
  "status": "ok",
  "saved": 3,
  "restart_required": true,
  "restart_fields": ["discord.check_role"]
}
```

### POST /api/config/validate

Dry-run validation. Same request body as `POST /api/config/values` but only checks for problems without writing. Useful for live previews — the editor can call this whenever a value changes and show validation issues immediately.

The save endpoint runs the same validators internally; any field with an `error`-severity issue rejects the save with HTTP 400. `warning`-severity issues are advisory and don't block saves.

**Request:** same as `POST /api/config/values`

**Response:**
```json
{
  "status": "ok",
  "issues": [
    {
      "field": "discord.iv_colors[2]",
      "severity": "error",
      "message": "not a valid hex colour (expected #RGB or #RRGGBB): \"red\""
    },
    {
      "field": "discord.iv_colors",
      "severity": "error",
      "message": "requires at least 6 entries (got 5)"
    },
    {
      "field": "geofence.paths[1]",
      "severity": "warning",
      "message": "file does not exist (yet) at /path/to/config/geofences/foo.json"
    },
    {
      "field": "geofence.paths[2]",
      "severity": "error",
      "message": "absolute paths not allowed; use a path relative to the config directory"
    }
  ]
}
```

Empty `issues` array means everything is valid. Each issue is one of:

| Severity | Meaning |
|----------|---------|
| `error` | Save will be rejected. The field value is invalid and the user must fix it. |
| `warning` | Save proceeds. The value is suspicious but technically allowed (e.g., a geofence path that doesn't exist on disk yet — the user might be configuring a fence they haven't created). |

**Validators currently applied:**
- `color[]` fields: each entry must be a valid hex colour (`#RGB` or `#RRGGBB`)
- `MinLength`/`MaxLength`: array length must fall within bounds
- `geofence.paths`: each entry must be either an http(s):// URL or a relative path under the config directory; absolute paths and `..` escapes are rejected; non-existent files trigger a warning

### POST /api/config/migrate

Slim `config.toml` by moving every web-editable non-default value into `config/overrides.json`. Useful after a user has been using the web editor for a while — it cleans up `config.toml` so it contains only TOML-only fields (database, tokens, processor host/port).

**Process:**
1. Backs up the current `config.toml` to `config.toml.bak.YYYY-MM-DD_HHMMSS`
2. For every web-editable field with a non-default value, copies it to `overrides.json` (without overwriting existing overrides — existing overrides win)
3. Rewrites `config.toml` containing only fields NOT in the editor schema (database, tokens, etc.) with a header comment pointing to the backup

**Idempotent:** running it twice produces the same result. Safe to retry on errors.

**Reversible:** delete `overrides.json` and restore the backup file to undo.

```bash
curl -X POST -H "X-Poracle-Secret: secret" http://localhost:3030/api/config/migrate
```

**Response:**
```json
{
  "status": "ok",
  "backup": "config.toml.bak.2026-04-08_153022",
  "fields_moved": [
    "alert_limits.dm_limit",
    "discord.admins",
    "general.locale"
  ],
  "fields_kept": [
    "alerter.api_secret",
    "database.host",
    "database.password",
    "discord.token",
    "processor.api_secret",
    "processor.port",
    "telegram.token"
  ]
}
```

### POST /api/resolve

Batch resolve Discord/Telegram IDs to human-readable names. Results cached for 10 minutes. IDs that cannot be resolved are omitted (not an error). Discord/Telegram sections are omitted when the respective bot is not configured.

**Request:**
```json
{
  "discord": {
    "users": ["344179542874914817"],
    "roles": ["987654321"],
    "channels": ["111222333"],
    "guilds": ["444555666"]
  },
  "telegram": {
    "chats": ["789012345", "-100123456"]
  },
  "destinations": ["111222333", "raid-feed", "999000111"]
}
```

The `destinations` array is for IDs of unknown type — used when a schema field has `resolve: "destination"` (e.g., `alert_limits.overrides.target` which can be a Discord channel/user/webhook/Telegram chat). The processor tries the humans table first, then Discord (channel → user → role → guild), then Telegram, returning whatever matches first.

**Response:**
```json
{
  "status": "ok",
  "discord": {
    "users": {"344179542874914817": {"name": "JamesBerry", "globalName": "James Berry"}},
    "roles": {"987654321": {"name": "Moderator", "guild": "My Server", "guildId": "444555666"}},
    "channels": {"111222333": {"name": "raid-alerts", "type": "text", "guild": "My Server", "guildId": "444555666", "categoryName": "Pokemon"}},
    "guilds": {"444555666": {"name": "My Server"}}
  },
  "telegram": {
    "chats": {
      "789012345": {"name": "James Berry", "type": "private"},
      "-100123456": {"name": "Pokemon Group", "type": "supergroup"}
    }
  },
  "destinations": {
    "111222333": {
      "kind": "discord:channel",
      "name": "raid-alerts",
      "enabled": true,
      "notes": "EU South RAID alerts",
      "areas": ["london"],
      "type": "text",
      "guild": "My Server",
      "guildId": "444555666"
    },
    "raid-feed": {
      "kind": "webhook",
      "name": "raid-feed",
      "enabled": true,
      "notes": "Discord raid feed for #raids"
    },
    "999000111": {
      "kind": "discord:channel",
      "name": "old-channel",
      "enabled": true,
      "notes": "channel deleted after server cleanup",
      "stale": true
    }
  }
}
```

**Stale flag**: when a destination matches an entry in PoracleNG's humans table but the platform API can't find the corresponding entity (e.g., a channel that was deleted, a user who left the server), the result includes `"stale": true`. The editor should warn the user before letting them keep stale targets in their config — these IDs are registered but no longer reachable.

The `kind` field tells the editor what type was matched: `webhook`, `discord:channel`, `discord:user`, `discord:role`, `discord:guild`, `telegram:user`, `telegram:channel`, `telegram:group`, etc.

For `geofence:area` resolve hints, the editor uses the existing `GET /api/geofence/all` endpoint to populate autocomplete.

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

### POST /api/deliverMessages

Send a confirmation message to a user via Discord or Telegram. Used internally by the processor for API operation confirmations (e.g. tracking added/removed). This is the canonical endpoint; `POST /api/postMessage` is a legacy alias that behaves identically.

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
