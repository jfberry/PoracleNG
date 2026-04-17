# PoracleNG

Pokemon GO webhook alerting system. Receives webhooks from Golbat (scanner), matches them against user-defined tracking rules, and delivers personalized alerts to Discord and Telegram.

## Architecture

Single Go process, one MySQL database, one shared TOML config:

```
Golbat (scanner)
    |  POST / (webhook array)
    v
Processor (Go, port 3030)
    |  - Parses webhooks (pokemon, raid, invasion, quest, lure, gym, nest, fort_update, max_battle, weather)
    |  - Loads tracking rules from MySQL into memory, atomically swaps on reload
    |  - Matches each webhook against all tracking rules (geofence, distance, filters)
    |  - Enriches with computed fields (three layers: base, per-language, per-user)
    |  - Renders DTS Handlebars templates (jfberry/raymond) into Discord/Telegram messages
    |  - Delivers messages directly via Discord REST API and Telegram Bot API
    |  - Serves all /api/* endpoints (tracking CRUD, humans, profiles, geofence, tiles, config, masterdata, roles)
    |  - Discord bot (discordgo gateway: commands, reconciliation, role management)
    |  - Telegram bot (go-telegram-bot-api: commands, reconciliation)
    v
Discord / Telegram
    (DM, channel, webhook, group, thread)
```

Runs via `start.sh` (or Docker) as a single process.

## Directory Structure

```
processor/                      # Go binary
  cmd/processor/
    main.go                     # Entry point, component init, HTTP server, render pool
    pokemon.go                  # Pokemon webhook handler
    raid.go                     # Raid/egg webhook handler
    fort.go                     # Fort update webhook handler
    maxbattle.go                # Max battle webhook handler
    render.go                   # Render pool: processRenderJob, delivery job construction
    test.go                     # poracle-test handlers (all 8 types)
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
    webhook/                    # HTTP receiver (POST /), types
    api/                        # All /api/* endpoints: tracking CRUD, humans, profiles, geofence, tiles, config, reload, test
      tracking.go               # Shared tracking CRUD helpers, diff logic, flexBool/flexInt types
      trackingMonster.go        # Pokemon tracking endpoints
      trackingRaid.go, ...      # Per-type tracking endpoints (10 types)
    delivery/                   # Go-native message delivery (Discord REST, Telegram Bot API)
      dispatcher.go             # Top-level orchestrator: Dispatch, Start, Stop
      queue.go                  # FairQueue with per-destination mutex, per-platform semaphores
      discord.go                # Discord REST sender: DM, channel, thread, webhook, image upload
      telegram.go               # Telegram REST sender: sticker, photo, text, venue, location
      ratelimit.go              # Per-route Discord rate limiting + global 50 req/sec token bucket
      tracker.go                # MessageTracker: TTL-based clean deletion, edit key lookup, disk persistence
      image.go                  # Image download, multipart builder, embed normalization
      delivery.go               # Core types: Job, SentMessage, Sender interface, PermanentError
    ratelimit/                  # Per-destination alert rate limiting (not Discord API rate limiting)
    i18n/                       # Translation system (flat JSON, identifier keys, {0} placeholders)
      locale/                   # Embedded locale files (en.json, de.json, ...)
        pokemon/                # Pokemon-name gap fillers for locales pogo-translations doesn't ship (zh-cn, ...); not a Crowdin source
    uicons/                     # Icon URL resolution (pokemon, raid, gym, weather) with scheduled index refresh
    staticmap/                  # Static map tile generation (tileservercache, google, osm, mapbox)
    geocoding/                  # Reverse/forward geocoding (nominatim, google) with pogreb + ttlcache
    dts/                        # DTS template rendering (Handlebars via jfberry/raymond)
      renderer.go               # RenderPokemon, RenderAlert, renderForUsers, renderGrouped
      layered_view.go           # LayeredView implementing raymond.FieldResolver (zero-copy 8-layer lookup)
      view.go                   # View builder: alias mapping, emoji resolution, computed fields
      helpers.go                # ~47 Handlebars helpers (comparison, math, string, game data)
      emoji.go                  # Per-platform emoji resolution
      templates.go              # Template loading, selection chain, templateFile (raw text), @include
      shlink.go                 # URL shortening via Shlink
    scanner/                    # Scanner DB interface (RDM, Golbat) for nearby stops
    rowtext/                    # Tracking rule description generator (for API confirmation messages)
    bot/                        # Platform-agnostic command framework
      command.go                # Command interface, CommandContext, Reply types, BotDeps
      parser.go                 # Multi-language command lookup, tokenization, pipe splitting
      argmatch.go               # 14 typed parameter matchers with language fallback
      pokemon_resolver.go       # Name→ID resolution (translations, aliases, evolution chains)
      area_logic.go             # Area validation/filtering (no DB dependency)
      community_logic.go        # Community membership management
      permissions.go            # Admin checks, command security, delegated admin
      target.go                 # BuildTarget (user/webhook override resolution)
      commands/                 # ~35 command implementations
    discordbot/                 # Discord gateway bot (discordgo)
      bot.go                    # Gateway setup, message handling, NLP suggest
      reconciliation.go         # Periodic role sync + event-driven reconciliation
      channel.go, webhook.go    # Channel/webhook management commands
      role.go                   # Role subscription command
      autocreate.go             # Channel auto-creation from template
    telegrambot/                # Telegram polling bot (go-telegram-bot-api)
      bot.go                    # Polling setup, command dispatch
      channel.go                # Channel/group management
      reconciliation.go         # Group membership verification
    discordroles/               # Shared Discord role helpers (used by bot + API)
    store/                      # Database abstraction layer
      human.go                  # HumanStore interface
      human_sql.go              # SQL implementation
      tracking.go               # TrackingStore[T] generic interface + ApplyDiff
      tracking_sql.go           # Per-type SQL implementations
      mock_human.go             # Mock for testing
    nlp/                        # Natural language → command suggestion parser

config/                         # Shared config directory
  config.toml                   # Main config (copied from config.example.toml)
  geofences/                    # Geofence JSON files
  .cache/geofences/             # Koji geofence cache

fallbacks/                      # Bundled defaults (dts.json, testdata.json, locale files)
resources/
  data/                         # Game data (util.json for UI constants)
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

**Important**: Fort update webhooks use a nested format from Golbat (`change_type`, `edit_types`, `new`/`old` snapshot objects), not flat fields. The enrichment layer flattens these into template-friendly fields (`name`, `oldName`, `newName`, `isEditName`, `changeTypeText`, etc.).

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

**Pokemon matching intentionally does NOT deduplicate by user ID.** A single user can appear multiple times in the result — once per matching tracking rule (basic IV, PVP great league, PVP ultra league, PVP evolution, etc.). Each entry carries different `PVPRankingLeague`, `PVPRankingWorst`, and `PVPRankingCap` values. This metadata flows into `consolidateUsers()` in per-user enrichment, which merges entries by user ID and collects their PVP filters to build the per-user PVP display list. The processor deduplicates by user ID after consolidation to render and send exactly one message per user. All other matchers (raid, egg, invasion, quest, etc.) deduplicate in the matcher itself since they don't carry per-tracking PVP metadata.

**Unencountered pokemon skip encounter-only stat filters.** When a pokemon is not encountered, Golbat omits CP, level, IVs, and weight. If a tracking rule constrains any of these beyond their defaults (`min_cp > 0`, `max_cp < 9000`, `min_level > 0`, `max_level < 55`, individual IVs, weight), the rule is skipped for unencountered pokemon. Rules with default stat values still match.

**PVP evolution caps: nil means "match any cap".** When Golbat sends PVP evolution data with `cap == 0 && !capped` (the "not ohbem" case), the evolution entry's `Caps` is nil. In the matcher, this bypasses the cap filter entirely (matching any `pvp_ranking_cap` value). This matches JS behavior where `caps: null` short-circuits the cap check. The best-rank path (non-evolution) defaults to `[50]` for the same case.

**Debounced state reload** (`ProcessorService.triggerReload`): All reload triggers — periodic timer excluded — use a centralized 500ms debounce timer. This coalesces burst mutations (e.g. PoracleWeb adding 50 tracking rules, or multiple rate-limit disables in the same window) into a single DB reload. Sources: API tracking mutations, rate-limit user disable, profile scheduler. The periodic timer runs its own direct `state.Load()` since it already operates on a fixed interval.

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
- Emoji keys for per-platform resolution during DTS view building
- Weakness list with translated type names
- Evolution/mega evolution entries with translated names
- PVP ranking lists (`pvp_rankings_great_league`, `pvp_rankings_ultra_league`, `pvp_rankings_little_league`) with enriched entries (`levelWithCap`, `nameEng`, pokemon/form names) for backward-compatible DTS templates

**Per-user enrichment** (computed per matched user):
- PVP display lists filtered by user's tracking criteria
- Distance and bearing from user's location

The enrichment is structured as `enrichment` (base), `perLanguageEnrichment` (keyed by language code), and `perUserEnrichment` (keyed by user ID). The DTS `LayeredView` merges these layers at render time without copying.

**Language fallback**: When a user has no `language` set in the humans table, the processor falls back to `[general] locale` from config (not hardcoded `"en"`). If the fallback locale doesn't match any per-language enrichment key, translated names (fullName, etc.) will be empty.

**Timezone note**: The Docker image must have `tzdata` installed (Alpine). The Go binary also embeds `time/tzdata` as fallback.

### 5. Template Rendering (Processor)

The processor renders DTS templates using `jfberry/raymond` (a fork of `mailgun/raymond/v2` with a `FieldResolver` interface for zero-copy view lookup):

1. Webhook handler enqueues a `RenderJob` to the render channel (buffered, configurable pool size)
2. Render worker picks up the job, resolves pending static map tile (blocking wait with deadline)
3. For each matched user: build `LayeredView` (8-layer priority lookup), select DTS template, render with raymond, URL-shorten `<S< ... >S>` markers, parse JSON result, append ping
4. **Group rendering optimization**: for non-pokemon types, users with the same (template, platform, language) share a single render — only per-user fields (distance, bearing) are patched afterward
5. Rendered delivery jobs dispatched directly to the `delivery.Dispatcher`

**Queue pressure**: When the render channel is >80% full, tile generation is skipped to reduce backpressure.

**Shutdown ordering** (`ProcessorService.Close` in `cmd/processor/main.go`): webhook workers → render channel (close) → render workers (drain) → dispatcher (stop, which stops its queue and tracker) → static map (close) → duplicates → rate limiter → gym state save → geocoder (close).

### 6. Message Delivery (Processor)

Delivery is handled via platform REST APIs (Discord API v10, Telegram Bot API).

**Dispatcher** (`delivery/dispatcher.go`):
- Receives rendered `delivery.Job` from render workers
- Routes to platform-specific `Sender` (Discord or Telegram)
- Manages per-platform concurrency semaphores

**FairQueue** (`delivery/queue.go`):
- Per-destination `sync.Mutex` via `sync.Map` ensures message ordering per channel/user
- Per-platform semaphores limit concurrent API calls
- Edit-before-send: if a job has an edit key matching a tracked message, edits instead of sending new

**Discord** (`delivery/discord.go`):
- Pure REST API (Discord API v10) — separate from the discordgo gateway used for commands/reconciliation
- DM channel creation and caching
- Channel, thread, and webhook sending
- Image upload as multipart form data (`uploadEmbedImages` feature)
- `embed` → `embeds[]` normalization, string color → int coercion
- Permanent error detection (50007 cannot DM, 10003 unknown channel, 10013 unknown user)
- Consecutive failure tracking: disables user after threshold

**Telegram** (`delivery/telegram.go`):
- Pure REST Bot API — separate from the go-telegram-bot-api polling used for commands
- Configurable send order: sticker, photo, text, location, venue
- Parse mode normalization (MarkdownV2, HTML, Markdown)
- 429 rate limit retry

**Rate Limiting** (`delivery/ratelimit.go`):
- Per-route Discord rate limits with `X-RateLimit-*` header parsing
- Global 50 req/sec token bucket
- `Retry-After` parsing with Dexter's heuristic (>1000 → milliseconds)
- Cleanup when map >1000 entries

**Message Tracker** (`delivery/tracker.go`):
- TTL cache (`ttlcache/v3`) keyed by `target:messageID`
- On TTH expiry: async clean deletion callback
- Edit key lookup for message updates
- Disk persistence (save/load) for crash recovery

## Rate Limiting

Rate limiting is handled by the processor (`internal/ratelimit/`) using a two-phase model: a cheap pre-filter at match time and the authoritative count at delivery time.

- **Per-destination limits**: Configurable via `[alert_limits]` — `dm_limit` (default 20), `channel_limit` (default 40) per `timing_period` (default 240 **seconds**, ~4 min). Fixed/tumbling window: when the first message arrives the window starts and lasts `timing_period` seconds; a new window only begins after full expiry.
- **Phase 1 — pre-filter (match time)**: After matching, each webhook handler calls `filterBlocked(matched)` which uses `Limiter.IsBlocked()` to drop users whose destination is currently over the limit. **Non-mutating** — it does not increment counters or fire notifications. This avoids burning render/enrichment work on destinations we already know are paused.
- **Phase 2 — authoritative count (delivery time)**: `FairQueue.processJob` (in `internal/delivery/queue.go`) calls `Limiter.Check()` per job, immediately before `Sender.Send`. Only deliveries that actually go on the wire count against a destination's quota. `JustBreached` triggers `RateLimitHooks.OnBreach` (which dispatches the i18n notification); `Banned` triggers `OnBan` (which disables the user, sends a farewell, and posts to the shame channel if configured). Hooks are implemented by `ProcessorService` in `cmd/processor/helpers.go`.
- **What does NOT count**:
  - **Edits** of an already-tracked message (`EditKey` matched in `MessageTracker`) — these are mutations of a send we already counted.
  - Jobs marked `BypassRateLimit=true` — used for the rate-limit notification itself and the ban farewell, so the limiter cannot swallow the message reporting on itself. Use `Dispatcher.DispatchBypass(job)` to enqueue these.
- **`max_limits_before_stop`**: After N consecutive rate-limit breaches in 24h (default 10), the user is auto-disabled to prevent permanent flood. `disable_on_stop=true` sets `admin_disable=1` (user must re-register with `!poracle`); `false` sets `enabled=0` (user can `!start` again). A debounced state reload follows so the user is removed from matching.
- **`blocked_alerts`**: Per-user JSON array in `humans` table (e.g., `["pokemon","raid"]`). Parsed into a `BlockedAlertsSet` map at state load for O(1) lookup. Blocks specific alert types without disabling the user entirely.
- **Limit overrides**: `[alert_limits.overrides]` allows per-user or per-role custom limits (array-of-tables format in TOML).

## Discord Reconciliation

Reconciliation syncs Discord role membership with Poracle user registration. Runs in-process via discordgo in `internal/discordbot/reconciliation.go`. Configured via `[reconciliation.discord]` and triggered by `[discord] check_role = true`.

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

**Event-driven** (`GuildMemberUpdate`, `GuildMemberRemove`):
- discordgo gateway events trigger `reconcileSingleUser()` for immediate role change detection
- Requires `GuildMembers` privileged gateway intent enabled in Discord Developer Portal

**Area Security mode** (`[area_security] enabled = true`):
- Instead of a single `user_role` list, roles are mapped to communities
- Each community has its own `user_role`, geofence areas, and location restrictions
- Users get `community_membership` and `area_restriction` fields updated based on their roles
- The processor enforces area restrictions during matching

**Key config requirements**:
- `[discord] check_role = true` and `guilds` must be set
- `[general] role_check_mode = "disable-user"` for actual removal (default `"ignore"` does nothing)
- Discord bot must have "Server Members Intent" enabled in Developer Portal

## API Security

**Shared secret** (`x-poracle-secret`):
- All `/api/*` endpoints are protected by the `X-Poracle-Secret` header matching `[processor] api_secret` (with legacy `[alerter] api_secret` as a backward-compatible fallback — if the processor key is empty, the alerter key is copied over at config load)
- If `api_secret` is empty/unset, auth is disabled (all requests allowed)
- The `RequireSecretGin` middleware in `api/middleware.go` is applied to the `/api/*` route group in `cmd/processor/main.go`

**Unprotected endpoints**:
- `GET /health` — health check
- `GET /metrics` — Prometheus metrics
- `POST /` — webhook receiver from Golbat (no auth, Golbat doesn't authenticate)

## Command System

### Parsing (`bot/parser.go`)

1. Split message by newlines (multi-line commands)
2. Tokenize by spaces, preserving quoted strings
3. Convert to lowercase, **replace underscores with spaces** (e.g., `slot_changes` → `slot changes`)
4. Reverse-translate from user's language to English
5. Split by `|` pipe for multiple command groups
6. Look up command handler by translated name
7. Execute command via `CommandContext`

### Command Pattern

All tracking commands (track, raid, egg, quest, nest, lure, gym, fort, invasion, maxbattle) follow:

1. `BuildTarget(args)` — resolve who the command is for (DM user, channel, webhook, admin override)
2. Parse args using typed parameter matchers (`bot/argmatch.go`)
3. Track consumed args, report unrecognized args
4. Validate distance/area requirements
5. **Template validation**: When user explicitly specifies `template:X`, the command validates the template exists in loaded DTS templates for the relevant type/platform. Non-admins are blocked with an error; admins receive a warning but can proceed.
6. Call store layer for DB operations, trigger state reload, send confirmation messages

**Default template storage**: Commands store an empty string for `template` when the user does not specify `template:X`. The renderer resolves empty template to `[general] default_template_name` from config at render time, allowing the admin to change the default without updating existing tracking rules.

**`!track remove` delegates to `!untrack`**: `!track remove ...` is equivalent to `!untrack ...`, including gen/type filtering support.

### Command Security & Target Resolution Rules

These rules govern who can execute commands, what they target, and where they can be run. **Do not change these without understanding the full security model.**

#### Context: Where commands run

| Context | Target Resolution | Notes |
|---------|-------------------|-------|
| **DM** | Always targets the sender | Any registered user can run commands |
| **Guild channel (registered)** | Admin/delegated → targets the channel; Non-admin → targets the sender | Channel must be registered with `!channel add` |
| **Guild channel (unregistered)** | **BLOCKED** — error "does not seem to be registered" | Prevents users from accidentally modifying personal tracking from random channels |
| **`user:ID` override** | Admin only — targets the specified user by ID | Bypasses channel check (explicit target) |
| **`name:webhookname` override** | Admin or webhook admin — targets the named webhook | Bypasses channel check (explicit target) |

#### Exceptions to BuildTarget

| Command | Behavior | Reason |
|---------|----------|--------|
| `!poracle` (all language variants) | Skips BuildTarget entirely, always targets sender | Registration command — runs in designated registration channels, has own `IsRegistrationChannel` validation |
| Discord-specific commands (`!channel`, `!webhook`, `!role`, `!autocreate`, `!poracle-clean`, `!poracle-id`, `!poracle-emoji`) | Handled before BuildTarget by `handleDiscordCommand` | Require discordgo session directly, have their own permission checks |

#### Permission levels

| Level | Who | Can do |
|-------|-----|--------|
| **Admin** | Users in `discord.admins` / `telegram.admins` | All commands, target override (`user:`, `name:`), manage channels/webhooks, enable/disable users, broadcast, apply |
| **Delegated channel admin** | Users/roles in `delegated_administration.channel_tracking` | Manage tracking for specific channels (without full admin) |
| **Delegated webhook admin** | Users in `delegated_administration.webhook_tracking` | Manage specific webhooks by name |
| **Community admin (Telegram)** | Users in `area_security.communities.*.telegram.admins` | Manage community channels via `/channel add` |
| **Regular user** | Registered via `!poracle` | Track/untrack, area, location, profile, language, start/stop, script, help, info, ask |
| **Unregistered user** | Not in humans table | Can only run `!poracle` (registration) and `!version` |

#### Command security (`command_security` config)

Maps Discord role IDs to allowed command sets. Users without the required role for a command get 🙅. Checked via `CommandAllowed()` after registration check, before BuildTarget.

#### Area security (`area_security.enabled`)

When enabled, users belong to communities (determined by Discord roles or Telegram group membership). Each community has:
- `allowed_areas` — geofence areas the user can select
- `location_fence` — geographic restriction on alert delivery
- Registration channels — where `!poracle` works for that community

`AreaLogic.ValidateAndPrune` removes areas no longer permitted when community membership changes.

#### `everything` flag permissions (`tracking.everything_flag_permissions`)

Controls whether non-admin users can use the `everything` keyword in `!track`:
- `deny` (default) — `everything` keyword not available to non-admins
- `allow-any` — `everything` and `individually` both available
- `allow-and-always-individually` — `everything` expands to all pokemon individually
- `allow-and-ignore-individually` — `everything` available but `individually` keyword hidden

Bare `!track everything` with no meaningful filters (IV, CP, level, PVP, type, gender) is always rejected for non-admins regardless of this setting.

### Key Commands

- `!track <pokemon> [filters]` — the most complex, uses `parameterDefinition` regex map (already has full arg validation). Supports multi-league PVP: `!track pikachu great5 ultra10` creates separate tracking rules per league. PVP filters enforce config min CP floors (`pvp_filter_great_min_cp` etc., defaults 1400/2350/450), clamp worst rank to `pvp_filter_max_rank`, and validate level caps against `level_caps` config.
- `!raid`, `!egg` — level/pokemon + team/exclusive/move/template/distance/rsvp
- `!quest` — stardust/energy/candy/item/pokemon rewards
- `!nest`, `!lure`, `!gym`, `!fort`, `!invasion`, `!maxbattle` — type-specific filters
- `!tracked` — list all active tracking, shows `[id:XX]` per rule for targeted removal
- `!untrack id:45` or `!raid remove id:12` — remove a specific tracking rule by database UID (works for all tracking types)
- `!poracle` — register/start
- `!profile` — switch/create/delete profiles
- `!area` — add/remove geofence areas
- `!location` — set lat/lon

## Testing

### Test Command (`poracle-test`)

`!poracle-test <type>,<test-id> [template:<n>] [language:<code>]`

The `!poracle-test` command calls the processor's `/api/test` endpoint, which loads test webhooks from `fallbacks/testdata.json` (bundled) merged with `config/testdata.json` (user custom), runs full enrichment, renders via the render queue, and delivers directly via the dispatcher.

Supported types: `pokemon`, `raid`, `pokestop`, `gym`, `nest`, `quest`, `fort-update`, `max-battle`

### Test Data

- **Bundled**: `fallbacks/testdata.json` — sample webhooks for all types with named test IDs
- **Custom**: `config/testdata.json` — user-provided test webhooks (overrides bundled by test ID)
- **Webhook logs**: Enable `[webhookLogging]` in config to log raw Golbat webhooks to `logs/webhooks.log` (rotated hourly). These can be replayed or used to create custom test data.

### Unit Tests

- `go test ./...` from `processor/` — tests covering matching, PVP, game data loading, enrichment, per-user PVP display, translations, icons, static map field filtering, autoposition, delivery (discord, telegram, queue, tracker, rate limiter, image), command parsing, bot commands

## API Endpoints

All API endpoints are accessed via the processor (port 3030). The processor handles all endpoints directly.

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
| GET | `/api/stats/shiny-possible` | Shiny-possible spawn data |
| GET | `/api/geofence/all` | All geofence data |
| GET | `/api/geofence/all/hash` | MD5 hashes of geofence paths |
| GET | `/api/geofence/all/geojson` | GeoJSON export |
| GET | `/api/geofence/{area}/map` | Geofence area tile |
| GET | `/api/geofence/distanceMap/{lat}/{lon}/{distance}` | Distance circle tile |
| GET | `/api/geofence/locationMap/{lat}/{lon}` | Location pin tile |
| POST | `/api/geofence/overviewMap` | Multi-area overview tile |
| GET | `/api/geofence/weatherMap/{lat}/{lon}` | Weather S2 cell tile |
| GET | `/api/config/templates` | DTS template list (?includeDescriptions=true) |
| GET | `/api/config/poracleWeb` | Server config for web UI |
| GET | `/api/config/schema` | Config editor schema with field metadata |
| GET | `/api/config/values` | Current merged config values |
| POST | `/api/config/values` | Save config changes (writes to overrides.json) |
| POST | `/api/config/validate` | Dry-run config validation |
| POST | `/api/config/migrate` | Move web-editable values from config.toml to overrides.json |
| POST | `/api/dts/render` | Render a DTS template with provided data |
| GET | `/api/dts/templates` | DTS template entries with full content (editor) |
| POST | `/api/dts/templates` | Save DTS template entries |
| DELETE | `/api/dts/templates` | Delete a DTS template entry |
| PUT | `/api/dts/templates/file` | Update raw templateFile content |
| GET | `/api/dts/emoji` | Emoji lookup map for template editing |
| POST | `/api/dts/enrich` | Run webhook through enrichment pipeline |
| GET | `/api/dts/fields` | List all DTS type names |
| GET | `/api/dts/fields/{type}` | Template fields, block scopes, and snippets for a type |
| GET | `/api/dts/partials` | Handlebars partials for client-side rendering |
| POST | `/api/dts/sendtest` | Render and deliver a test message |
| GET | `/api/dts/testdata` | Test webhook scenarios from testdata.json |
| GET/POST | `/api/dts/reload` | Reload DTS templates and partials from disk |
| POST | `/api/deliverMessages` | Deliver confirmation/notification messages |
| POST | `/api/postMessage` | Legacy alias for /api/deliverMessages |
| POST | `/api/resolve` | Batch resolve Discord/Telegram IDs to names |
| POST | `/api/command` | Execute a bot command via API |
| GET | `/api/masterdata/monsters` | All pokemon with names, forms, types (built from raw masterfile) |
| GET | `/api/masterdata/grunts` | Grunt types (built from classic.json) |
| GET | `/api/tracking/all/{id}` | All tracking for a user (all types) |
| GET | `/api/tracking/allProfiles/{id}` | All tracking across all profiles |
| GET | `/api/tracking/pokemon/refresh` | Force state reload |
| GET | `/api/tracking/{type}/{id}` | List tracking rules with descriptions |
| POST | `/api/tracking/{type}/{id}` | Create/update tracking rules (diff logic) |
| DELETE | `/api/tracking/{type}/{id}/byUid/{uid}` | Delete single tracking rule |
| POST | `/api/tracking/{type}/{id}/delete` | Bulk delete by UID array |
| GET | `/api/humans/{id}` | User available areas (filtered by community) |
| GET | `/api/humans/one/{id}` | Full human record |
| POST | `/api/humans` | Create a new user |
| POST | `/api/humans/{id}/start` | Enable a user |
| POST | `/api/humans/{id}/stop` | Disable a user |
| POST | `/api/humans/{id}/adminDisabled` | Toggle admin disable flag |
| POST | `/api/humans/{id}/setLocation/{lat}/{lon}` | Update user location |
| GET | `/api/humans/{id}/checkLocation/{lat}/{lon}` | Check if location is in allowed areas |
| POST | `/api/humans/{id}/setAreas` | Set user's selected geofence areas |
| POST | `/api/humans/{id}/switchProfile/{profile}` | Switch active profile |
| GET | `/api/humans/{id}/roles` | List Discord roles across guilds |
| POST | `/api/humans/{id}/roles/add/{roleId}` | Add a Discord role to a user |
| POST | `/api/humans/{id}/roles/remove/{roleId}` | Remove a Discord role from a user |
| GET | `/api/humans/{id}/getAdministrationRoles` | Get delegated admin permissions |
| GET | `/api/profiles/{id}` | List profiles |
| POST | `/api/profiles/{id}/add` | Create new profile(s) |
| POST | `/api/profiles/{id}/update` | Update profile active_hours |
| POST | `/api/profiles/{id}/copy/{from}/{to}` | Copy tracking rules between profiles |
| DELETE | `/api/profiles/{id}/byProfileNo/{profile_no}` | Delete profile |
| GET | `/health` | Health check |
| GET | `/metrics` | Prometheus metrics |

Tracking types: pokemon, raid, egg, quest, invasion, lure, nest, gym, fort, maxbattle.

**Flexible JSON type coercion**: All tracking CRUD POST endpoints accept flexible JSON types for numeric and boolean fields. Third-party clients like ReactMap may send `"clean": false` (boolean) instead of `"clean": 0` (number). The `flexBool` and `flexInt` custom JSON types in `processor/internal/api/tracking.go` handle this coercion transparently:
- `flexBool`: accepts `true`/`false`, `0`/`1`, `"0"`/`"1"` — coerces to int (0 or 1)
- `flexInt`: accepts numbers, booleans, quoted strings — coerces to int

## State Management (Processor)

State is loaded from MySQL into an immutable snapshot, then atomically swapped:

1. `db.LoadAll()` — queries all tracking tables + humans
2. Build `state.State` struct with indexed data (MonsterIndex for O(1) pokemon lookup)
3. `manager.Set(newState)` — atomic swap via `sync.RWMutex`
4. All webhook handlers call `manager.Get()` to get current snapshot
5. Reload triggered every `reload_interval_secs` (default 60s) or via `/api/reload`

**Two reload modes:**
- **`state.Load()`** — DB only, reuses existing geofence data. Used by `/api/reload`, periodic timer, and tracking API mutations via `triggerReload()`
- **`state.LoadWithGeofences()`** — full reload including geofence files and Koji fetch. Used at startup and `/api/geofence/reload`

Tracking API routes call `triggerReload()` after mutations to push changes immediately. All 10 tracking types trigger reloads on create/update/delete.

**Gym state persistence**: Gym team/slot state is saved to `config/.cache/gym-state.json` on shutdown and restored on startup. This prevents false team-change alerts after a restart, since the processor would otherwise treat all gyms as "new" and trigger change notifications.

### Clean Flag Bitmask

The `clean` field on tracking rules is a bitmask with two bits (`db/clean.go`):

- `0` = no message lifecycle tracking
- `1` = clean (auto-delete message on TTH expiry via MessageTracker)
- `2` = edit (track message for later editing/updating)
- `3` = edit + clean (both bits set)

Helper functions `IsClean(clean)` and `IsEdit(clean)` test individual bits. When `IsEdit` is true, the renderer generates an `EditKey` (a stable identifier derived from the webhook event, e.g. gym ID + pokemon ID for raids). This EditKey flows through `RenderJob` into the `delivery.Job`. The `FairQueue` in delivery checks the `MessageTracker` for an existing message with the same EditKey; if found, it edits the existing message instead of sending a new one. First consumer: raids with RSVP updates (`rsvp_changes` tracking), where the same raid alert is edited in-place as RSVP counts change.

## Template System (DTS)

DTS (Data Template System) templates are Handlebars templates rendered by the Go processor using `jfberry/raymond` (fork of `mailgun/raymond/v2` with `FieldResolver` interface). Templates define per-platform message formats for each alert type.

### Template Loading

Templates are loaded from `config/dts.json` (or `fallbacks/dts.json` as fallback), plus additional JSON files from `config/dts/` directory. Each template entry may use:
- Inline `template` object — JSON that gets stringified after processing
- `"templateFile": "dts/fort_update.txt"` — external file read as raw Handlebars text (NOT parsed as JSON). This allows non-JSON constructs like unquoted Handlebars expressions in value positions.
- `"@include filename"` — include directive for shared partials (resolved from `config/dts/`)
- Array values for multi-line `description` fields — joined into strings (only arrays of all strings; arrays containing objects like `embed.fields` are preserved)

**DTS Partials** (`config/partials.json` or `fallbacks/partials.json`): Named Handlebars partials loaded per-template and registered with raymond before rendering. Reloaded on DTS reload (`/api/reload`). Partials are available in templates via `{{> partialName}}`.

### Template Selection Chain

Template selection uses a two-pass mechanism. The first pass considers only user entries (non-readonly, from `config/dts.json` or `config/dts/`). If nothing matches, the second pass considers only fallback entries (readonly, from `fallbacks/dts.json`). Within each pass, 5 priority levels are tried in order:

1. Exact match: type + template ID + platform + language
2. Template + platform: type + template ID + platform (entry has empty language)
3. Default + language: type + default template + platform + language
4. Default: type + default template + platform (entry has empty language)
5. Default + any language: type + default template + platform (last resort, any language)

Within each level, the **last match wins** so that `config/dts/` overrides `config/dts.json` (later-loaded files are appended). Levels 3 and 4 share a return check: a level 4 match supersedes a level 3 match.

### LayeredView (zero-copy template context)

The `LayeredView` implements raymond's `FieldResolver` interface, providing an 8-layer priority lookup without copying maps:

1. **perUser** — PVP display, distance, bearing (per matched user)
2. **emoji** — platform-specific emoji strings
3. **perLang** — translated names, weakness, evolutions
4. **computed** — TTH recalculation, area list, derived fields
5. **base** — enrichment output (maps, geocoding, weather, game data)
6. **aliases** — camelCase mappings for backward compatibility with existing DTS templates
7. **webhook** — raw webhook fields (fallback for any field not in enrichment)
8. **dtsDict** — user-configured `[general] dts_dictionary` key-value pairs

**HTML escaping is disabled**: raymond's `EscapeFunc` is set to a no-op. DTS output is JSON for Discord/Telegram, not HTML — the default HTML escaping would corrupt `<` characters in Handlebars expressions and URL parameters. The `{{escape}}` helper is available for explicit HTML escaping when needed.

**DTS alias tables are per webhook type** (not global) to avoid cross-type field name conflicts. Each webhook type registers its own camelCase alias mappings in its enrichment code. The `gameweather` alias maps to `gameWeatherName` (translated name), not `gameWeatherId` (numeric).

**LayeredView is the only render path.** Legacy view code (`BuildPokemonView`, `mergeMaps`, `addAliases`, `addComputedFields`) has been removed.

### Handlebars Helpers

The processor registers ~47 Handlebars helpers via `dts/helpers.go`:
- **Comparison**: `eq`, `ne`, `gt`, `lt`, `gte`, `lte`, `and`, `or`, `not`, `isPokemon` — work in both block mode (`{{#eq a b}}...{{/eq}}`) and subexpression mode (`{{#if (eq a b)}}...{{/if}}`). The `and`/`or` helpers are variadic (accept 2+ args) and registered as options-only helpers in the raymond fork to support correct argument collection.
- **Math**: `add`, `subtract`, `multiply`, `divide`, `round`, `roundPokemon`, `floor`, `ceil`, `abs`, `toFixed`
- **String**: `contains`, `split`, `trim`, `join`, `concat`, `lowercase`, `uppercase`, `capitalizePokemon`, `replace`
- **Array**: `len`
- **Formatting**: `pad`, `pokemonPad`, `pokemon3Pad`, `pokemonPadPokemon`
- **Game data**: pokemon/move/type/evolution/mega lookups, emoji resolution, map URL builders

### Emoji Resolution

Per-platform emoji resolution uses `config/emoji.json` (or `fallbacks/emoji.json`) and `resources/data/util.json`. Enrichment provides emoji keys (e.g., `emojiWeather`, `emojiTeam`, `emojiDirection`), and the view builder resolves these to platform-specific strings (Discord custom emoji syntax, Telegram unicode, etc.) via explicit key mapping. The `getEmoji(key, platform)` function looks up the emoji by exact key — no suffix-guessing heuristics.

### Shlink URL Shortening

Templates can wrap URLs in `<S< ... >S>` markers. After rendering, the processor scans for these markers and replaces each with a shortened URL via a configured Shlink instance. If Shlink is unavailable, the original URL is preserved.

Templates receive the full view object with all enriched data. Common fields: `{{name}}`, `{{iv}}`, `{{cp}}`, `{{level}}`, `{{time}}` (disappear time), `{{tthh}}:{{tthm}}:{{tths}}` (time remaining), `{{addr}}` (address), `{{mapurl}}` (Google Maps), `{{imgUrl}}` (pokemon icon), `{{staticMap}}` (map tile).

## Translation / i18n

### Translation system

`processor/internal/i18n/`
- Flat JSON files with **dotted identifier keys**: `{"rate_limit.reached": "Das Limit von {0}..."}`
- `{0}` placeholder syntax
- English is a first-class locale file (`en.json`), not hardcoded in source
- Merge order (later wins):
  1. Embedded (`processor/internal/i18n/locale/*.json` and `processor/internal/i18n/locale/pokemon/*.json`) — bundled processor messages + Pokemon-name gap fillers
  2. `resources/locale/*.json` — game data from pogo-translations
  3. `config/custom.{lang}.json` — admin overrides
- The `locale/pokemon/` subdirectory is for Pokemon-name translations (`poke_{id}` keys) in locales pogo-translations doesn't ship — zh-cn currently. Files there merge into the same locale translator as the UI file (e.g. `pokemon/zh-cn.json` adds to the zh-cn translator alongside `locale/zh-cn.json`). Only Crowdin-managed files sit directly in `locale/`; `locale/pokemon/` is intentionally outside Crowdin's source list so translators for other languages aren't asked to re-translate Pokemon names that already come from gamelocale.
- Identifier keys are stable: renaming English text doesn't break translations
- **Per-key English fallback**: Non-English translators fall back to English on a per-key basis (not per-locale). After all locale files are loaded, `Bundle.LinkFallbacks()` links each non-English `Translator` to the English one. When `T("key")` finds no value in the user's language, it checks the English translator before returning the raw key. This means a German user still sees English pokemon names for any `poke_*` keys missing from the German locale files.

### Adding new translated strings

1. Add the identifier and English text to `processor/internal/i18n/locale/en.json`
2. Add translations to each `{lang}.json` in the same directory
3. Use `tr.T("key")` or `tr.Tf("key", args...)` at the call site
4. Strings are embedded in the binary at build time; admin overrides via `config/custom.{lang}.json`

### Translation key sources

| Data | Key format | Source | Example |
|------|-----------|--------|---------|
| Pokemon names | `poke_{id}` | resources/locale/ (pogo-translations) | `poke_25` → "Pikachu" |
| Form names | `form_{formId}` | resources/locale/ | `form_65` → "Alolan" |
| Move names | `move_{moveId}` | resources/locale/ | `move_14` → "Hyper Beam" |
| Type names | `poke_type_{typeId}` | resources/gamelocale/ | `poke_type_12` → "Grass" |
| Grunt category | `character_category_{id}` | resources/gamelocale/ | `character_category_2` → "Grunt" |
| Quest titles | `quest_title_{title}` | resources/gamelocale/ | `quest_title_quest_catch_pokemon_plural` → "Catch %{amount_0} Pokemon" |
| Item names | `item_{id}` | resources/gamelocale/ | `item_701` → "Razz Berry" |
| Weather names | `weather_{id}` | resources/locale/ | `weather_1` → "Clear" |
| Command names | `cmd.*` | processor/internal/i18n/locale/ (embedded) | `cmd.track` → "track" |
| Response text | `msg.*` | processor/internal/i18n/locale/ (embedded) | `msg.tracked.pokemon` |
| UI strings | `rate_limit.*` | processor/internal/i18n/locale/ (embedded) | `rate_limit.reached` |

**Named placeholders**: gamelocale strings use `%{name}` placeholders (e.g. `%{amount_0}`, `%{pokemon}`), not `{0}` positional. The `FormatNamed()` / `TfNamed()` functions in `processor/internal/i18n/` handle these. The processor's own embedded strings use `{0}` positional via `Format()` / `Tf()`.

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

Migrations in `processor/internal/db/migrations/` (SQL files, run on processor startup). The DSN includes `multiStatements=true` to support multi-statement migration files on fresh installations.

### HumanStore boundary

All `humans` and `profiles` table reads and mutations outside the store implementation go through `store.HumanStore` (`processor/internal/store/human.go`). The interface covers:

- `Get` — full record (typed `*store.Human` with `bool` flags and `[]string` JSON columns) for cold paths that need parsed data.
- `GetLite` — lightweight projection (`*store.HumanLite`) for hot paths (tracking CRUD `lookupHuman`) that only need ID / profile / language.
- `ListByType`, `ListByTypeEnabled`, `ListByTypes`, `ListAll` — bulk queries.
- `GetProfiles` / `SwitchProfile` / `AddProfile` / `DeleteProfile` — profile management.
- Per-field setters (`SetEnabled`, `SetArea`, `SetLocation`, `SetAdminDisable`, etc.) plus a dynamic `Update(id, map)` escape hatch for partial updates that don't fit a dedicated setter.

The only human/profile-related code remaining in `db/human_queries.go` is `DeleteHumanAndTracking` (used by the store itself for cross-table cascade) and the `trackingTables` list it walks. Everything a caller would reach for is on `store.HumanStore`.

API handlers serialise through DTOs to preserve the legacy JSON wire format:
- `api.HumanResponse` (`processor/internal/api/human_response.go`) mirrors the legacy `db.HumanFull` shape (int flags, JSON-string array columns, `null.String` for nullable columns). `humanToResponse(*store.Human)` adapts at the boundary.
- `api.ProfileResponse` (`processor/internal/api/profile_response.go`) mirrors the legacy `db.ProfileRow` shape (area as JSON-encoded string). `profileToResponse(store.Profile)` / `profilesToResponse([]store.Profile)` adapt at the boundary.

Key benefits this boundary provides:
- **User-customised schemas are tolerated.** Explicit column lists in the store mean operators can add their own columns to `humans` (e.g. `sub_end` for subscription tracking) without the processor failing to scan.
- **Typed end-state for internal code.** `db.HumanFull`, `db.HumanAPI`, `db.SelectOneHumanFull`, `db.SelectOneHuman`, and the `UpdateHuman*` helpers were retired; there is no raw-row shape outside the store.
- **API wire format is preserved.** Existing clients (PoracleWeb) continue to receive the same JSON shape for every endpoint.

## Game Data

The processor uses the **raw masterfile** (`master-latest-raw.json`) from Masterfile-Generator, split into `resources/rawdata/` (pokemon, forms, moves, types, items, invasions, weather). The masterdata API endpoints (`/api/masterdata/monsters`, `/api/masterdata/grunts`) build poracle-v2 format on-the-fly from the raw masterfile for PoracleWeb compatibility.

**Grunt/invasion data** uses `classic.json` from WatWowMap/event-info (downloaded to `resources/rawdata/invasions.json`). This format uses numeric type IDs (`character.type.id`) and proto template strings (`CHARACTER_GRASS_GRUNT_MALE`), NOT English name strings. Translation uses identifier keys: `poke_type_{typeID}` for grunt type, `character_category_{categoryID}` for grunt category.

**Pokemon aliases** (`config/pokemonAlias.json` with fallback to `fallbacks/pokemonAlias.json`): Maps alias names to one or more pokemon IDs. Supports multi-ID aliases (e.g., `"laketrio": [480, 481, 482]`). Used by the pokemon resolver in bot commands for convenient shorthand names.

**Important: resource download collision.** The raw masterfile (`master-latest-raw.json`) also contains an `"invasions"` category in the old formatted.json format. `downloadRawMaster` skips this category to avoid overwriting the `classic.json` saved by `downloadGrunts`. If this skip is removed, the processor will silently load empty grunt data.

### Resource directory layout

Resources are downloaded at startup (`resources.Download`):

| Directory | Format | Contents |
|-----------|--------|----------|
| `resources/data/` | poracle-v2 (English names) | util.json (UI constants) |
| `resources/rawdata/` | Raw masterfile + classic.json | pokemon.json, moves.json, types.json, invasions.json (classic), weather.json |
| `resources/locale/` | enRefMerged (English-as-key) | en.json, de.json, ... (pogo-translations, layer 3 in i18n merge) |
| `resources/gamelocale/` | Identifier keys | en.json, de.json, ... (quest_title_*, poke_type_*, item_*, character_category_*, layer 2 in i18n merge) |

`resources/data/util.json` provides UI constants (teams, genders, types with colors/emoji, weather, generation ranges, raid levels, lures, pokestop events). Loaded via `gamedata.LoadUtilData`.

## Tileserver

Static map tiles are generated by an external tileserver (typically
[tileservercache](https://github.com/123FLO321/SwiftTileserverCache)). The
processor talks to it over HTTP; the rendered message carries a URL that
Discord/Telegram fetch to display the image.

### Configuration

- `[geocoding] static_provider_url` — the **public** URL of the tileserver. Embedded in rendered messages; must be reachable by Discord/Telegram.
- `[geocoding] static_internal_url` — optional **private** URL the processor uses for its own tileserver calls (render POST, pregenerate POST, upload-images pre-fetch). Defaults to `static_provider_url`. Set this when the public URL goes through a proxy/CDN (e.g. Cloudflare) and the processor can reach the tileserver directly — avoids proxy buffering/latency on the hot path.

### Tile modes

`cmd/processor/tilemode.go` decides per render batch:

- `Skip` — template doesn't reference `{{staticMap}}`.
- `Inline` — every destination supports upload-images (Discord with `uploadEmbedImages=true`). The processor POSTs for bytes directly (no pregenerate); bytes flow through `RenderJob.TileImageData` and attach to each `delivery.Job`. No second fetch.
- `URL` — at least one destination needs a fetchable URL (Telegram always; Discord with `uploadEmbedImages=false`). The processor pregenerates; the returned URL is embedded in the rendered message; each consumer fetches the URL itself. If `uploadEmbedImages=true` and no Telegram/upload-off destinations are in the batch, this mode isn't used.
- `URLWithBytes` — mixed batches where some destinations need a URL (Telegram / upload-off Discord) and some would benefit from prefetched bytes (Discord with `uploadEmbedImages=true`). The processor pregenerates once to obtain the public URL, then issues a single internal GET via `static_internal_url` for the bytes. The public URL goes into the message; the bytes attach to every `delivery.Job` in the batch. Delivery's existing `len(StaticMapData) > 0 && imageURL != ""` short-circuit in `delivery/discord.go` means Discord-upload jobs consume the bytes (no per-destination fetch), Telegram jobs ignore them, and upload-off Discord jobs also ignore them (because `NormalizeAndExtractImage` returns empty `imageURL` when `uploadImages=false`).

Why `URLWithBytes` exists: before this mode, a single event fanning out to N Discord-upload destinations plus a Telegram destination triggered N separate downloads of the public URL from the processor (one per destination, inside each job's critical section). Each download was a chance for Cloudflare-style proxy buffering to fail — losing the map for that destination. `URLWithBytes` collapses those N downloads into one, routes it through the internal URL, and guarantees the same bytes for every Discord destination in the batch.

## Configuration

Single TOML file at `config/config.toml`, used by the processor. See `config/config.example.toml` for all options with comments.

Key sections: `[processor]`, `[database]`, `[geofence]`, `[pvp]`, `[weather]`, `[discord]`, `[telegram]`, `[geocoding]`, `[tuning]`, `[tracking]`, `[alert_limits]`, `[stats]`, `[logging]`.

The legacy `[alerter]` section is still read for backward-compatible `api_secret` only (users upgrading from older versions); all other alerter functionality has been absorbed into the processor.

## Deployment

**Docker** (recommended): Single Go binary. `Dockerfile` uses multi-stage build (Go builder → Alpine runtime). Requires `tzdata` package for timezone support.

**Bare metal**: `./start.sh` — builds processor if needed, runs single binary.

## Development Notes

- Build and run: `cd processor && go build ./cmd/processor && ./poracle-processor -basedir ..`
- Config paths resolve relative to project root via `-basedir` flag
- Cache files (clean-cache, geofence cache, gym-state) resolve relative to `getConfigDir()` — the `config/` directory
