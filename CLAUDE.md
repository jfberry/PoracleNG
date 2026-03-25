# PoracleNG

Pokemon GO webhook alerting system. Receives webhooks from Golbat (scanner), matches them against user-defined tracking rules, and delivers personalized alerts to Discord and Telegram.

## Architecture

Two processes, one shared MySQL database, one shared TOML config:

```
Golbat (scanner)
    |  POST / (webhook array)
    v
Processor (Go, port 3030)
    |  - Parses webhooks (pokemon, raid, invasion, quest, lure, gym, nest, fort_update, max_battle, weather)
    |  - Loads tracking rules from MySQL into memory, atomically swaps on reload
    |  - Matches each webhook against all tracking rules (geofence, distance, filters)
    |  - Enriches with computed fields:
    |      Base: timezone-aware times, weather, PVP ranks, rarity, IV, map URLs, static map tiles,
    |            icon URLs (uicons), reverse geocoding, weakness, generation, evolution chains
    |      Per-language: translated pokemon/move/type/weather names (using pogo-translations)
    |      Per-user: PVP display lists, distance/bearing
    |  - Batches matched results and POSTs to alerter
    |  - Also proxies /api/* to alerter (so external tools only need one endpoint)
    |  - Serves geofence data, tile generation, and geocoding APIs
    v
Alerter (Node.js/Fastify, port 3031)
    |  POST /api/matched (batch of matched payloads)
    |  - Routes each payload to the appropriate controller by type
    |  - Controller does: per-platform emoji lookups, night time, template rendering
    |  - Renders Handlebars DTS templates into Discord/Telegram messages
    |  - Queues messages for delivery
    v
Discord / Telegram
    (DM, channel, webhook, group)
```

Both processes run together via `start.sh` (or Docker). The processor starts first, waits for health check, then the alerter starts.

## Directory Structure

```
processor/                      # Go binary
  cmd/processor/
    main.go                     # Entry point, component init, HTTP server
    pokemon.go                  # Pokemon webhook handler
    raid.go                     # Raid/egg webhook handler
    fort.go                     # Fort update webhook handler
    maxbattle.go                # Max battle webhook handler
    ...                         # One file per webhook type
  internal/
    config/                     # TOML config loader
    db/                         # MySQL loaders (humans, monsters, raids, etc.)
      migrations/               # SQL migration files
    gamedata/                   # Raw masterfile loader (pokemon, moves, types, items, invasions, weather, util.json)
    geofence/                   # GeoJSON parser, R-tree spatial index, PIP
    matching/                   # In-memory matchers per webhook type
      generic.go                # ValidateHumansGeneric — shared human/distance/area validation
      pokemon.go                # Pokemon-specific filter logic
      raid.go, fort.go, ...     # Per-type matchers
    enrichment/                 # Three-layer enrichment: base (universal), per-language (translated), per-user (PVP/distance)
      pokemon.go                # Pokemon enrichment (types, weakness, stats, IV, maps, weather, evolutions, PVP)
      translate.go              # Per-language translation helpers using pogo-translations
      peruser.go                # Per-user PVP display computation with filter matching
    pvp/                        # PVP rank calculator (from Golbat webhook data)
    tracker/                    # Weather cells, encounter changes, duplicates, rarity stats
    state/                      # Immutable state snapshot with atomic RWMutex swap
    webhook/                    # HTTP receiver (POST /), sender (batched to alerter), types
    api/                        # Processor API endpoints (/health, /api/reload, /api/stats/*, geofence, tiles)
    ratelimit/                  # Per-destination message rate limiting
    i18n/                       # Translation system (flat JSON, identifier keys, {0} placeholders)
      locale/                   # Embedded locale files (en.json, de.json, ...)
    uicons/                     # Icon URL resolution (pokemon, raid, gym, weather) with scheduled index refresh
    staticmap/                  # Static map tile generation (tileservercache, google, osm, mapbox)
    geocoding/                  # Reverse/forward geocoding (nominatim, google) with pogreb + ttlcache
    scanner/                    # Scanner DB interface (RDM, Golbat) for nearby stops

alerter/                        # Node.js application
  src/
    app.js                      # Entry point, Fastify server, matched queue loop
    controllers/
      controller.js             # Base class: template rendering, emoji lookup, getLanguageEnrichment
      monster.js                # Pokemon alert: per-platform emoji, template rendering (all data pre-computed by processor)
      raid.js                   # Raid + egg alerts
      fortupdate.js             # Fort name/image/location change alerts
      maxbattle.js              # Max battle alerts
      quest.js, gym.js, nest.js, pokestop.js, pokestop_lure.js, weather.js
      common/                   # Shared: night time
    lib/
      discord/
        commando/
          index.js              # Discord bot setup, command registration
          events/messageCreate.js  # Command parser (underscore→space, pipe groups, translation)
          commands/              # Admin commands (poracle, channel, webhook, autocreate, role, etc.)
        discordWorker.js        # DM + channel delivery, FairPromiseQueue, clean message deletion
        discordWebhookWorker.js # Webhook URL delivery with retry/rate-limit handling
        poracleDiscordState.js  # State wrapper passed to command handlers
        poracleDiscordUtil.js   # buildTarget, commandAllowed, permission checks
        poracleDiscordMessage.js # Message abstraction (reply, react, replyWithAttachment)
      telegram/
        Telegram.js             # Bot setup, message delivery (sticker/photo/text/venue/location)
        poracleTelegramState.js # State wrapper for Telegram commands
      poracleMessage/
        commands/               # User commands: track, raid, egg, quest, nest, lure, gym, fort, invasion, maxbattle, etc.
        commandUtil.js          # Shared: reportUnrecognizedArgs
      FairPromiseQueue.js       # Concurrency limiter: max N concurrent, 1 per destination
      configAdapter.js          # TOML → internal config conversion (snake_case → camelCase)
      configResolver.js         # Config/fallback file resolution, config directory
      geofenceLoader.js         # GeoJSON/Poracle fence parser, R-tree builder
    routes/
      postMatched.js            # POST /api/matched — receives batches from processor
      apiTracking.js            # GET /api/tracking/all/:id — all tracking for a user
      apiTrackingMonster.js     # CRUD for pokemon tracking (pattern for all types)
      apiTrackingRaid.js, apiTrackingEgg.js, ...  # Per-type tracking CRUD
      apiHumans.js              # User management, area assignment
      apiProfiles.js            # Profile CRUD
      apiConfig.js              # GET /api/config/poracleWeb — expose config to web UI
      apiMasterData.js          # GET /api/masterdata/monsters, /grunts
      apiGeofence.js            # Geofence reload (tiles and data served by processor)
      apiTrackingFort.js        # CRUD for fort tracking
    util/
      regex.js                  # Command argument regex factory (translated command names)
      translate.js              # Translator class (locale JSON files)
      translatorFactory.js      # Multi-locale translator management

config/                         # Shared config directory
  config.toml                   # Main config (copied from config.example.toml)
  geofences/                    # Geofence JSON files
  .cache/geofences/             # Koji geofence cache

fallbacks/                      # Bundled defaults (dts.json, testdata.json, locale files)
resources/
  data/                         # Shared game data (util.json, monsters.json for alerter)
  rawdata/                      # Raw masterfile (pokemon, moves, types, etc. for processor)
  locale/                       # Game data translations from pogo-translations
  gamelocale/                   # Identifier-key game locale files for processor i18n
```

## Webhook Flow (End to End)

### 1. Reception (Processor)

Golbat POSTs a JSON array to `POST /` on the processor:
```json
[{"type": "pokemon", "message": {...}}, {"type": "raid", "message": {...}}]
```

`webhook/receiver.go` routes each item by type string to the corresponding handler (e.g., `ProcessPokemon`, `ProcessRaid`, `ProcessFortUpdate`). Each handler runs in the worker pool (default 4 goroutines).

### 2. Parsing & Dedup

Each handler unmarshals the raw JSON into a typed struct (`webhook/types.go`). Duplicate check follows — keyed on encounter ID + CP + disappear time for pokemon, station ID + battle end + pokemon ID for maxbattle, etc. Duplicates are silently dropped.

**Important**: Fort update webhooks use a nested format from Golbat (`change_type`, `edit_types`, `new`/`old` snapshot objects), not flat fields.

### 3. Matching

The handler creates a `matching.*Data` struct and calls the matcher. All matchers follow the same pattern:

1. Iterate tracking rules from in-memory state (loaded from MySQL)
2. Apply type-specific filters (IV range, level, team, form, move, rarity, PVP rank, etc.)
3. Call `ValidateHumansGeneric()` which checks:
   - Human exists, is enabled, not admin-disabled
   - Alert type not in `blocked_alerts`
   - Profile number matches
   - Distance check (haversine) if distance > 0, else area overlap check
   - Strict area restriction (if configured)
4. Returns `[]MatchedUser` with destination info (id, type, template, clean, ping, distance)

**Pokemon matching intentionally does NOT deduplicate by user ID.** A single user can appear multiple times in the result — once per matching tracking rule (basic IV, PVP great league, PVP ultra league, PVP evolution, etc.). Each entry carries different `PVPRankingLeague`, `PVPRankingWorst`, and `PVPRankingCap` values. This metadata flows into `consolidateUsers()` in per-user enrichment, which merges entries by user ID and collects their PVP filters to build the per-user PVP display list. The alerter then deduplicates by user ID (`monster.js` lines 108-114) to send exactly one message per user. All other matchers (raid, egg, invasion, quest, etc.) deduplicate in the matcher itself since they don't carry per-tracking PVP metadata.

**Cardinal direction labels are intentionally mirrored** vs standard compass directions (e.g. 45° bearing returns `"northwest"` not `"northeast"`). These strings are only used as emoji lookup keys into `util.json`, where `"northwest"` maps to the northeast-pointing arrow emoji. The user sees the correct arrow — the raw string is never displayed in templates. This convention is inherited from the original JS `getBearingEmoji`.

### 4. Enrichment

Enrichment is computed in three layers:

**Base enrichment** (computed once per webhook, universal):
- **Timezone**: `geo.GetTimezone(lat, lon)` via tzf library → IANA timezone name
- **Time formatting**: `geo.FormatTime(unix, tz, goLayout)` — formats in the correct local timezone. The Go layout is converted from Moment.js format (`LTS` → `HH:mm:ss` → Go `15:04:05`) at startup.
- **TTH**: `geo.ComputeTTH(targetUnix)` — days/hours/minutes/seconds remaining
- **Weather**: Current S2 cell weather ID, forecast for next hour, weather change impact
- **Sun times**: Night/dawn/dusk booleans
- **PVP ranks**: Best rank per league across configured level caps (pokemon only)
- **Rarity**: Statistical rarity group from rolling window
- **Shiny rate**: If shiny provider configured
- **Game data**: Types, weakness calculation, generation, base stats, IV/IV color, evolution chains, boosting weathers
- **Map URLs**: Google Maps, Apple Maps, Waze, RDM, ReactMap, RocketMad
- **Icons**: Pokemon/raid/gym/weather icon URLs via uicons (with scheduled index refresh)
- **Static map**: Tileserver tile URL (pregenerate or query params), with scanner DB stop lookups
- **Geocoding**: Reverse geocode address via nominatim/google with pogreb on-disk + ttlcache in-memory cache

**Per-language enrichment** (computed once per distinct language among matched users):
- Translated pokemon/form/move/type/weather/team/generation names using pogo-translations identifier keys (`poke_1`, `move_14`, `form_46`, etc.)
- Emoji keys for per-platform resolution by the alerter
- Weakness list with translated type names
- Evolution/mega evolution entries with translated names

**Per-user enrichment** (computed per matched user):
- PVP display lists filtered by user's tracking criteria
- Distance and bearing from user's location

The enrichment payload sent to the alerter includes `enrichment` (base), `perLanguageEnrichment` (keyed by language code), and `perUserEnrichment` (keyed by user ID). The alerter merges these via `getLanguageEnrichment()` and per-user data lookup.

**Language fallback**: When a user has no `language` set in the humans table, the processor falls back to `[general] locale` from config (not hardcoded `"en"`). The alerter does the same via `config.general.locale`. These must match — if they don't, the per-language enrichment lookup will miss and translated names (fullName, etc.) will be empty.

**Timezone note**: The Docker image must have `tzdata` installed (Alpine). The Go binary also embeds `time/tzdata` as fallback.

### 5. Sending to Alerter

`webhook/sender.go` batches `OutboundPayload` objects and POSTs them to `{alerter_url}/api/matched`:
```json
[{
  "type": "pokemon",
  "message": {... raw Golbat webhook ...},
  "enrichment": {"disappearTime": "05:00:00", "tth": {"hours": 1, ...}, "gameWeatherId": 3, ...},
  "matched_areas": [{"name": "Berlin", ...}],
  "matched_users": [{"id": "123", "type": "discord:user", "template": "1", ...}]
}]
```

Default batch: 50 items or 100ms flush interval, whichever comes first.

### 6. Alerter Processing

`app.js` receives batches at `POST /api/matched`, pushes each payload to `matchedQueue`. A 100ms interval loop shifts items to an internal `hookQueue` processed by a `PromiseQueue` (default concurrency 10).

`processOne()` merges `payload.enrichment` into `payload.message` via `Object.assign`, then routes by type to the controller's `handleMatched()`.

### 7. Controller Rendering

Each controller (e.g., `monster.js`) receives `(data, matchedUsers, matchedAreas)` where data already contains all enrichment from the processor. Controllers are now thin:

1. **Checks TTH** — drops if already expired or below `alert_minimum_time`
2. **Per-user message loop**:
   - Merge per-language enrichment via `getLanguageEnrichment(data, language)`
   - Merge per-user enrichment from `data.perUserEnrichment[userId]`
   - Look up per-platform emoji (using emoji keys from enrichment — the only thing that varies by platform)
   - Night time calculation
   - Build the template view object with all computed fields
   - Call `createMessage()` which renders the Handlebars DTS template
   - Build a job: `{target, type, message, tth, clean, ...}`
3. **Returns jobs array** — pushed to Discord or Telegram queue by `processMessages()`

All game data lookups, translations, geocoding, static maps, icon URLs, weakness calculations, PVP display, map URLs, and evolution chains are pre-computed by the processor. The alerter only resolves emoji per platform and renders templates.

### 8. Delivery

**Discord embed format note**: DTS templates may use either `embed` (singular, legacy) or `embeds` (plural array, modern). All Discord workers normalize `embed` → `embeds[]` before sending and coerce string `color` values to integers. Color detection: `#`-prefixed or exactly 6 chars → hex (e.g. `"#A040A0"`, `"A040A0"`); anything else → decimal (e.g. `"1216493"`). The `uploadEmbedImages` feature downloads the tile from `embeds[0].image.url` and re-uploads as an attachment; if the download fails, the message is sent with the URL in the embed instead.

**Discord DM/Channel** (`discordWorker.js`):
- `FairPromiseQueue`: max N concurrent sends, max 1 per destination (prevents flooding one user)
- Sends via Discord.js client (`user.send()` or `channel.send()`)
- Optionally uploads map image as attachment
- If `clean: true`, schedules message deletion after TTH
- Consecutive failure tracking: in-memory counter, disables user after threshold

**Discord Webhook** (`discordWebhookWorker.js`):
- Same FairPromiseQueue pattern
- POSTs to webhook URL via axios
- Handles 429 rate limits with retry-after + jitter backoff
- Can upload images as multipart form data

**Telegram** (`Telegram.js`):
- Same FairPromiseQueue pattern
- Supports configurable send order: sticker, photo, text, venue, location
- Text supports MarkdownV2, HTML, or Markdown parse modes
- Handles 429 rate limits with retry (up to 5 attempts)
- Clean message deletion via scheduled `deleteMessage()` calls

## Rate Limiting

Rate limiting is handled by the processor (`internal/ratelimit/`), applied before sending to the alerter:

- **Per-destination limits**: Configurable via `[alert_limits]` — `dm_limit` (default 20), `channel_limit` (default 40) per `timing_period` (default 240 minutes)
- **Implementation**: Sliding window counter per destination ID. When a destination exceeds its limit, the matched payload is dropped and an i18n-translated rate limit message is sent to the user instead.
- **`max_limits_before_stop`**: After N consecutive rate limit hits (default 10), the user is auto-disabled (`admin_disable = 1`) to prevent permanent flood. User must re-register with `!poracle`.
- **`blocked_alerts`**: Per-user JSON array in `humans` table (e.g., `["pokemon","raid"]`). Parsed into a `BlockedAlertsSet` map at state load for O(1) lookup. Blocks specific alert types without disabling the user entirely.
- **Limit overrides**: `[alert_limits.overrides]` allows per-user or per-role custom limits (array-of-tables format in TOML).

## Discord Reconciliation

Reconciliation syncs Discord role membership with Poracle user registration. Configured via `[reconciliation.discord]` and triggered by `[discord] check_role = true`.

**Periodic sync** (`syncDiscordRole`, every `check_role_interval` hours):
1. Loads all `discord:user` humans from DB
2. Fetches all guild members from configured `guilds` (with 15s stagger between guilds to avoid gateway rate limits)
3. For each user, checks if they hold any role in `user_role` list
4. Actions based on role membership changes:
   - **Lost role**: If `remove_invalid_users = true`, calls `disableUser()` which checks `role_check_mode`:
     - `"disable-user"`: Sets `admin_disable = 1`, removes subscription roles, sends goodbye message
     - `"delete"`: Deletes all tracking data + human record
     - `"ignore"` (default): Logs but takes no action
   - **Gained role**: Creates human record or reactivates if previously disabled, sends greeting
   - **Still has role**: Updates name (if `update_user_names`), syncs `blocked_alerts` from `command_security`

**Event-driven** (`guildMemberUpdate`, `guildMemberRemove`):
- Discord.js events trigger `reconcileSingleUser()` for immediate role change detection
- Requires `GuildMembers` privileged gateway intent enabled in Discord Developer Portal

**Area Security mode** (`[area_security] enabled = true`):
- Instead of a single `user_role` list, roles are mapped to communities
- Each community has its own `user_role`, geofence areas, and location restrictions
- Users get `community_membership` and `area_restriction` fields updated based on their roles
- The processor enforces area restrictions during matching

**Channel reconciliation** (if `update_channel_names` or `unregister_missing_channels`):
- Verifies registered `discord:channel` entries still exist
- Updates channel names/notes, disables missing channels

**Key config requirements**:
- `[discord] check_role = true` and `guilds` must be set
- `[general] role_check_mode = "disable-user"` for actual removal (default `"ignore"` does nothing)
- Discord bot must have "Server Members Intent" enabled in Developer Portal

## API Security

**Shared secret** (`x-poracle-secret`):
- Both processor and alerter `/api/*` endpoints are protected by the `X-Poracle-Secret` header matching `[alerter] api_secret`
- The processor copies `[alerter] api_secret` to its own config at startup
- If `api_secret` is empty/unset, auth is disabled (all requests allowed)
- The `RequireSecret` middleware in `api/api.go` wraps all processor `/api/*` handlers
- The alerter also checks IP whitelist/blacklist before authentication

**Unprotected endpoints**:
- `GET /health` — health check
- `GET /metrics` — Prometheus metrics
- `POST /` — webhook receiver from Golbat (no auth, Golbat doesn't authenticate)

**Internal calls** (alerter → processor):
- All alerter commands that call processor APIs include the secret via `config.processor.headers`
- The `config.processor.headers` object is pre-built at config load time from `api_secret`
- The alerter proxy (`/api/` catch-all) forwards the original `X-Poracle-Secret` header from external callers

## Command System

### Parsing (`messageCreate.js`)

1. Split message by newlines (multi-line commands)
2. Tokenize by spaces, preserving quoted strings
3. Convert to lowercase, **replace underscores with spaces** (e.g., `slot_changes` → `slot changes`)
4. Reverse-translate from user's language to English
5. Split by `|` pipe for multiple command groups
6. Look up command handler by translated name
7. Execute `command.run(client, msg, args, options)`

### Command Pattern

All tracking commands (track, raid, egg, quest, nest, lure, gym, fort, invasion, maxbattle) follow:

1. `buildTarget(args)` — resolve who the command is for (DM user, channel, webhook, admin override)
2. Parse args in `forEach`/`for` loop — match against regex patterns and keywords
3. Track consumed args in a `Set`, report unrecognized args via `reportUnrecognizedArgs()`
4. Validate distance/area requirements
5. Query existing tracking, compute inserts/updates/unchanged
6. Insert to DB, send confirmation message, react with emoji

### Key Commands

- `!track <pokemon> [filters]` — the most complex, uses `parameterDefinition` regex map (already has full arg validation)
- `!raid`, `!egg` — level/pokemon + team/exclusive/move/template/distance/rsvp
- `!quest` — stardust/energy/candy/item/pokemon rewards
- `!nest`, `!lure`, `!gym`, `!fort`, `!invasion`, `!maxbattle` — type-specific filters
- `!tracked` — list all active tracking
- `!poracle` — register/start
- `!profile` — switch/create/delete profiles
- `!area` — add/remove geofence areas
- `!location` — set lat/lon

## Testing

### Test Command (`poracle-test`)

`!poracle-test <type>,<test-id> [template:<n>] [language:<code>]`

The alerter POSTs to the processor's `/api/test` endpoint, which loads test webhooks from `fallbacks/testdata.json` (bundled) merged with `config/testdata.json` (user custom), runs full enrichment (including geocoding, static maps, icons), and returns the matched payload for the alerter to render and deliver.

Supported types: `pokemon`, `raid`, `pokestop`, `gym`, `nest`, `quest`, `fort-update`, `max-battle`

### Test Data

- **Bundled**: `fallbacks/testdata.json` — sample webhooks for all types with named test IDs
- **Custom**: `config/testdata.json` — user-provided test webhooks (overrides bundled by test ID)
- **Webhook logs**: Enable `[webhookLogging]` in config to log raw Golbat webhooks to `logs/webhooks.log` (rotated hourly). These can be replayed or used to create custom test data.
- **Matched webhook logs**: Enable `[logging.enable_logs] webhooks = true` to log matched payloads sent to the alerter.

### Unit Tests

- **Processor**: `go test ./...` from `processor/` — 295+ tests covering matching, PVP, game data loading, enrichment equivalence (cross-language JS↔Go), per-user PVP display, translations, icons, static map field filtering, autoposition, circuit breaker
- **Alerter**: `cd alerter && npm run test:commands` — command argument validation tests (mocha)
- **Lint**: `cd alerter && npm run lint` — eslint with airbnb-base

## API Patterns

All alerter API endpoints:
- Require `x-poracle-secret` header (must match `[alerter] api_secret`)
- Check IP whitelist/blacklist
- Accept user ID as `:id` URL parameter
- Return `{status: "ok", ...}` or `{status: "error", message: "..."}` or `{status: "authError", ...}`

### Key Endpoints

**Processor-native endpoints** (handled directly by Go):

| Method | Path | Description |
|--------|------|-------------|
| POST | `/` | Receive webhooks from Golbat |
| GET/POST | `/api/reload` | Trigger DB state reload (preserves geofences) |
| GET/POST | `/api/geofence/reload` | Full reload including geofence files/Koji |
| POST | `/api/test` | Generate test alert (poracle-test) |
| GET | `/api/weather` | Get weather data for an S2 cell |
| GET | `/api/geocode/forward` | Forward geocode lookup |
| GET | `/api/stats/rarity` | Rarity group statistics |
| GET | `/api/stats/shiny` | Shiny rate statistics |
| GET | `/api/geofence/all` | All geofence data |
| GET | `/api/geofence/all/hash` | MD5 hashes of geofence paths |
| GET | `/api/geofence/all/geojson` | GeoJSON export |
| GET | `/api/geofence/{area}/map` | Geofence area tile |
| GET | `/api/geofence/distanceMap/{lat}/{lon}/{distance}` | Distance circle tile |
| GET | `/api/geofence/locationMap/{lat}/{lon}` | Location pin tile |
| POST | `/api/geofence/overviewMap` | Multi-area overview tile |
| GET | `/api/geofence/weatherMap/{lat}/{lon}` | Weather S2 cell tile |
| GET | `/health` | Health check |
| GET | `/metrics` | Prometheus metrics |

**Alerter endpoints** (proxied through processor via `/api/*`):

| Method | Path | Description |
|--------|------|-------------|
| POST | `/api/matched` | Receive matched payloads from processor |
| GET | `/api/tracking/all/:id` | All tracking for a user (all types) |
| GET | `/api/tracking/pokemon/:id` | Pokemon tracking list |
| POST | `/api/tracking/pokemon/:id` | Add pokemon tracking |
| DELETE | `/api/tracking/pokemon/:id/byUid/:uid` | Delete one tracking entry |
| PATCH | `/api/tracking/pokemon/:id/byUid/:uid` | Update one tracking entry |
| GET | `/api/humans/:id` | User info + available areas |
| PATCH | `/api/humans/:id` | Update user (location, area, language, etc.) |
| GET | `/api/profiles/:id` | List profiles |
| GET | `/api/config/poracleWeb` | Server config for web UI |
| GET | `/api/masterdata/monsters` | All pokemon with names, forms, types |
| GET | `/api/masterdata/grunts` | Grunt types |

The same CRUD pattern (GET list, POST create, DELETE by uid, PATCH by uid) applies to: pokemon, raid, egg, quest, invasion, lure, nest, gym, fort, maxbattle.

The processor proxies all `/api/*` requests to the alerter, so external tools only need to know the processor's address.

## State Management (Processor)

State is loaded from MySQL into an immutable snapshot, then atomically swapped:

1. `db.LoadAll()` — queries all tracking tables + humans
2. Build `state.State` struct with indexed data (MonsterIndex for O(1) pokemon lookup)
3. `manager.Set(newState)` — atomic swap via `sync.RWMutex`
4. All webhook handlers call `manager.Get()` to get current snapshot
5. Reload triggered every `reload_interval_secs` (default 60s) or via `/api/reload`

**Two reload modes:**
- **`state.Load()`** — DB only, reuses existing geofence data. Used by `/api/reload`, periodic timer, and tracking API mutations via `triggerReloadAlerts()`
- **`state.LoadWithGeofences()`** — full reload including geofence files and Koji fetch. Used at startup and `/api/geofence/reload`

Tracking API routes (`apiTracking*.js`) call `triggerReloadAlerts()` after mutations to push changes to the processor immediately. All 10 tracking types (pokemon, raid, egg, quest, invasion, lure, nest, gym, fort, maxbattle) trigger reloads on create/update/delete.

## Template System (DTS)

Templates are Handlebars files in `config/dts.json` or external `.json` template files. The DTS (Data Template System) maps alert types to platform-specific message formats.

Selection chain: exact match (type + template ID + platform + language) → fallback to default template → fallback to any platform match.

Templates receive the full view object with all enriched data. Common fields: `{{name}}`, `{{iv}}`, `{{cp}}`, `{{level}}`, `{{time}}` (disappear time), `{{tthh}}:{{tthm}}:{{tths}}` (time remaining), `{{addr}}` (address), `{{mapurl}}` (Google Maps), `{{imgUrl}}` (pokemon icon), `{{staticMap}}` (map tile).

## Translation / i18n

### Two systems (migrating)

The alerter and processor currently use different translation approaches. As logic moves from alerter to processor, new code uses the processor's system. Both will coexist until the migration is complete.

**Alerter (legacy)** — `alerter/src/util/translate.js`
- Flat JSON files with **English text as keys**: `{"You have reached the limit of {0} messages": "Das Limit von {0}..."}`
- Placeholders: `{0}`, `{1}`, ... replaced by `Translator.format()`
- Merge order: `resources/locale/{lang}.json` → `alerter/locale/{lang}.json` → `config/custom.{lang}.json`
- Game data (pokemon names, moves) and UI messages share the same key-value namespace
- Problem: changing English wording breaks all translations; no namespacing

**Processor (new)** — `processor/internal/i18n/`
- Flat JSON files with **dotted identifier keys**: `{"rate_limit.reached": "Das Limit von {0}..."}`
- Same `{0}` placeholder syntax as the alerter, so translated strings are compatible
- English is a first-class locale file (`en.json`), not hardcoded in source
- Merge order (later wins):
  1. Embedded (`processor/internal/i18n/locale/*.json`) — bundled processor messages
  2. `resources/locale/*.json` — game data from pogo-translations
  3. `alerter/locale/*.json` — shared alerter strings (also used by the alerter)
  4. `config/custom.{lang}.json` — admin overrides
- Identifier keys are stable: renaming English text doesn't break translations
- Dotted namespacing: `rate_limit.reached`, `pokemon.name.1`, etc.

### Locale file format

All locale files (both systems) are flat JSON: `{"key": "translated string"}`. This is intentional — the same files are consumed by the Go processor, Node alerter, and React web frontend. The format is directly supported by Crowdin, Transifex, and Weblate.

### Placeholder convention

Both systems use `{0}`, `{1}`, ... for positional arguments. In Go:
```go
tr := ps.translations.For(user.Language)
msg := tr.Tf("rate_limit.reached", result.Limit, ps.cfg.AlertLimits.TimingPeriod)
```

### Adding new translated strings

1. Add the identifier and English text to `processor/internal/i18n/locale/en.json`
2. Add translations to each `{lang}.json` in the same directory
3. Use `tr.T("key")` or `tr.Tf("key", args...)` at the call site
4. Strings are embedded in the binary at build time; admin overrides via `config/custom.{lang}.json`

### File locations

```
processor/internal/i18n/
  i18n.go                       # Bundle, Translator, Format(), JSON loader
  embed.go                      # go:embed directive for locale/*.json
  load.go                       # Load() — multi-layer merge from all sources
  locale/
    en.json                     # English (source of truth for new identifier keys)
    de.json, fr.json, ...       # Bundled translations

resources/locale/               # Game data translations (pogo-translations)
  en.json, de.json, ...         # Pokemon names, moves, types, etc. by numeric ID

alerter/locale/                 # Alerter message translations (English-as-key format)
  de.json, fr.json, ...         # Legacy format, merged into processor bundle at layer 3

config/custom.{lang}.json       # Admin overrides (highest priority)
```

### Migration plan

As code moves from alerter to processor:
1. New processor strings use identifier keys (`rate_limit.reached`, not English sentences)
2. Game data translations (`poke_1`, `move_14`, etc.) are loaded from `resources/locale/` — same source as the alerter
3. The alerter's English-as-key strings in `alerter/locale/` are merged at layer 3, so they remain available if the processor ever needs them during the transition
4. Eventually the alerter's `translate.js` will also switch to identifier keys, unifying both systems

### Crowdin integration

Translation management uses Crowdin (free for open-source). See `crowdin.yml` in the project root for the configuration. The workflow:

1. Source files (English) are uploaded to Crowdin from `processor/internal/i18n/locale/en.json`
2. Translators work in the Crowdin web UI
3. Crowdin creates a PR with updated `{lang}.json` files when translations are complete
4. The PR is reviewed and merged normally

To set up (one-time): create a Crowdin project, connect the GitHub repo, and add the `crowdin.yml` config. See below for the config file.

## Configuration

Single TOML file at `config/config.toml`, shared by both processor and alerter. See `config/config.example.toml` for all options with comments.

Key sections: `[processor]`, `[alerter]`, `[database]`, `[geofence]`, `[pvp]`, `[weather]`, `[discord]`, `[telegram]`, `[geocoding]`, `[tuning]`, `[tracking]`, `[alert_limits]`, `[stats]`, `[logging]`.

The alerter's `configAdapter.js` converts snake_case TOML keys to camelCase JS objects with sensible defaults.

**configAdapter `defaults()` gotcha**: The `defaults(target, defs)` function is **shallow** — it only sets keys missing from `target`. For nested sections like `[reconciliation.discord]`, if the user sets *any* key, the entire default object is skipped. Nested defaults must be applied individually (e.g., `defaults(config.reconciliation.discord, {...})`).

## Game Data

The processor uses the **raw masterfile** (`master-latest-raw.json`) from Masterfile-Generator, split into `resources/rawdata/` (pokemon, forms, moves, types, items, invasions, weather). The alerter continues using the poracle-v2 format from `resources/data/monsters.json` for command processing and `!tracked` display.

Key differences from poracle-v2: raw uses numeric type IDs everywhere (no string-to-ID conversion), has populated invasion encounters, richer move data (PvP stats), and is 3x smaller (2.1MB vs 5.9MB).

Shared data: `resources/data/util.json` provides UI constants (teams, genders, types with colors/emoji, weather, generation ranges, raid levels, lures, pokestop events). Used by both processor (`gamedata.LoadUtilData`) and alerter (`GameData.utilData`).

Pokemon translations come from pogo-translations identifier keys (`poke_1`, `move_14`, `form_46`, `poke_type_1`) loaded into `resources/locale/` and merged into the processor's i18n bundle.

## Database

MySQL (or MariaDB). Key tables:

- `humans` — registered users/channels/webhooks (id, type, name, enabled, area, location, language, profile, blocked_alerts, fails)
- `monsters` — pokemon tracking rules (pokemon_id, form, IV/CP/level ranges, PVP filters, distance, template)
- `raid` — raid tracking (pokemon_id, level, team, exclusive, move, gym_id, rsvp_changes)
- `egg` — egg tracking (level, team, exclusive, rsvp_changes)
- `quest` — quest tracking (reward_type, reward, shiny, distance)
- `invasion` — invasion tracking (grunt_type, gender)
- `lures` — lure tracking (lure_id)
- `nests` — nest tracking (pokemon_id, min_spawn_avg)
- `gym` — gym tracking (team, slot_changes, battle_changes, gym_id)
- `forts` — fort update tracking (fort_type, include_empty, change_types)
- `maxbattle` — max battle tracking (pokemon_id, level, gmax, move)
- `profiles` — user profiles (profile_no, name, pokemon/raid/egg/invasion area overrides)

All tracking tables have: `id` (FK to humans), `profile_no`, `distance`, `template`, `clean`, `ping`.

Migrations in `processor/internal/db/migrations/` (SQL files, run on processor startup).

## Deployment

**Docker** (recommended): Single image, both processes. `Dockerfile` uses multi-stage build (Go builder → Node builder → Alpine runtime). Requires `tzdata` package for timezone support.

**Bare metal**: `./start.sh` — builds processor if needed, installs node modules if needed, starts both with health check and monitoring.

## Development Notes

- Go processor: `cd processor && go build ./cmd/processor && ./poracle-processor -basedir ..`
- Alerter: `cd alerter && npm install && node src/app.js`
- Lint: `cd alerter && npm run lint` (eslint with `--fix`)
- Config paths resolve relative to project root via `configResolver.js` (alerter) and `-basedir` flag (processor)
- Cache files (clean-cache, geofence cache) resolve relative to `getConfigDir()` — the `config/` directory
