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
    |  - Enriches with computed fields (timezone-aware times, weather, PVP ranks, rarity)
    |  - Batches matched results and POSTs to alerter
    |  - Also proxies /api/* to alerter (so external tools only need one endpoint)
    v
Alerter (Node.js/Fastify, port 3031)
    |  POST /api/matched (batch of matched payloads)
    |  - Routes each payload to the appropriate controller by type
    |  - Controller enriches further (translations, emoji, map URLs, template rendering)
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
    geofence/                   # GeoJSON parser, R-tree spatial index, PIP
    matching/                   # In-memory matchers per webhook type
      generic.go                # ValidateHumansGeneric — shared human/distance/area validation
      pokemon.go                # Pokemon-specific filter logic
      raid.go, fort.go, ...     # Per-type matchers
    enrichment/                 # Computed fields added to matched payloads
    pvp/                        # PVP rank calculator (from Golbat webhook data)
    tracker/                    # Weather cells, encounter changes, duplicates, rarity stats
    state/                      # Immutable state snapshot with atomic RWMutex swap
    webhook/                    # HTTP receiver (POST /), sender (batched to alerter), types
    api/                        # Processor API endpoints (/health, /api/reload, /api/stats/*)
    ratelimit/                  # Per-destination message rate limiting
    i18n/                       # Translation system (flat JSON, identifier keys, {0} placeholders)
      locale/                   # Embedded locale files (en.json, de.json, ...)

alerter/                        # Node.js application
  src/
    app.js                      # Entry point, Fastify server, matched queue loop
    controllers/
      controller.js             # Base class: geocoding, static maps, template rendering, DB queries
      monster.js                # Pokemon alert: PVP display, type calc, weakness calc
      raid.js                   # Raid + egg alerts
      fortupdate.js             # Fort name/image/location change alerts
      maxbattle.js              # Max battle alerts
      quest.js, gym.js, nest.js, pokestop.js, pokestop_lure.js, weatherData.js
      common/                   # Shared: weather forecast, night time, evolution calculator
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
      apiGeofence.js            # Geofence map tile generation
    util/
      regex.js                  # Command argument regex factory (translated command names)
      translate.js              # Translator class (locale JSON files)
      translatorFactory.js      # Multi-locale translator management

config/                         # Shared config directory
  config.toml                   # Main config (copied from config.example.toml)
  geofences/                    # Geofence JSON files
  .cache/geofences/             # Koji geofence cache

fallbacks/                      # Bundled defaults (dts.json, testdata.json, locale files)
resources/                      # Generated game data (monsters.json, moves.json, etc.)
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

### 4. Enrichment

If users matched, the handler calls the enricher which computes:
- **Timezone**: `geo.GetTimezone(lat, lon)` via tzf library → IANA timezone name
- **Time formatting**: `geo.FormatTime(unix, tz, goLayout)` — formats in the correct local timezone. The Go layout is converted from Moment.js format (`LTS` → `HH:mm:ss` → Go `15:04:05`) at startup.
- **TTH**: `geo.ComputeTTH(targetUnix)` — days/hours/minutes/seconds remaining
- **Weather**: Current S2 cell weather ID, forecast for next hour
- **Sun times**: Night/dawn/dusk booleans
- **PVP ranks**: Best rank per league across configured level caps (pokemon only)
- **Rarity**: Statistical rarity group from rolling window
- **Shiny rate**: If shiny provider configured

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

Each controller (e.g., `monster.js`) receives `(data, matchedUsers, matchedAreas)` and:

1. **Enriches data** — map URLs, pokemon/move/type names from GameData, PVP display lists, weakness calculations, emoji lookups
2. **Checks TTH** — drops if already expired or below `alert_minimum_time`
3. **Fetches assets** — pokemon icon URL (uicons), reverse geocoding, static map tile
4. **Per-user message loop**:
   - Translate names to user's language
   - Look up platform-specific emoji
   - Build the template view object with all computed fields
   - Call `createMessage()` which renders the Handlebars DTS template
   - Build a job: `{target, type, message, tth, clean, ...}`
5. **Returns jobs array** — pushed to Discord or Telegram queue by `processMessages()`

### 8. Delivery

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

Loads test webhooks from `fallbacks/testdata.json` (bundled) merged with `config/testdata.json` (user custom). Simulates the full matched pipeline — builds enrichment fields locally (timezone, TTH, formatted times), creates a matched payload with the commanding user as the target, and pushes to the matched queue.

Supported types: `pokemon`, `raid`, `pokestop`, `gym`, `nest`, `quest`, `fort-update`, `max-battle`

### Test Data

- **Bundled**: `fallbacks/testdata.json` — sample webhooks for all types with named test IDs
- **Custom**: `config/testdata.json` — user-provided test webhooks (overrides bundled by test ID)
- **Webhook logs**: Enable `[webhookLogging]` in config to log raw Golbat webhooks to `logs/webhooks.log` (rotated hourly). These can be replayed or used to create custom test data.
- **Matched webhook logs**: Enable `[logging.enable_logs] webhooks = true` to log matched payloads sent to the alerter.

### Unit Tests

- **Processor**: `go test ./...` from `processor/` — tests for matching, PVP, duplicate cache, geofence, game data
- **Alerter**: `npx mocha 'src/lib/poracleMessage/commands/__tests__/*.test.js'` — command argument validation tests (on `command-arg-validation` branch)
- **Lint**: `npm run lint` from `alerter/` — eslint with airbnb-base

## API Patterns

All alerter API endpoints:
- Require `x-poracle-secret` header (must match `[alerter] api_secret`)
- Check IP whitelist/blacklist
- Accept user ID as `:id` URL parameter
- Return `{status: "ok", ...}` or `{status: "error", message: "..."}` or `{status: "authError", ...}`

### Key Endpoints

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
| POST | `/api/reload` | Trigger processor state reload |
| GET | `/health` | Processor health check |
| GET | `/metrics` | Prometheus metrics |

The same CRUD pattern (GET list, POST create, DELETE by uid, PATCH by uid) applies to: pokemon, raid, egg, quest, invasion, lure, nest, gym, maxbattle.

The processor proxies all `/api/*` requests to the alerter, so external tools only need to know the processor's address.

## State Management (Processor)

State is loaded from MySQL into an immutable snapshot, then atomically swapped:

1. `db.LoadAll()` — queries all tracking tables + humans
2. Build `state.State` struct with indexed data (MonsterIndex for O(1) pokemon lookup)
3. `manager.Set(newState)` — atomic swap via `sync.RWMutex`
4. All webhook handlers call `manager.Get()` to get current snapshot
5. Reload triggered every `reload_interval_secs` (default 60s) or via `POST /api/reload`

Tracking API routes (`apiTracking*.js`) call `triggerReloadAlerts()` after mutations to push changes to the processor immediately.

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
