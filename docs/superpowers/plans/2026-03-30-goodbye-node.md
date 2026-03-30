# Goodbye Node: Complete Alerter Elimination Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Eliminate the Node.js alerter entirely, consolidating all functionality into the Go processor as a single binary.

**Architecture:** The Go processor becomes the single process — it runs Discord (discordgo) and Telegram (go-telegram-bot-api) bots internally, handles all command processing with a platform-agnostic command framework, and delivers all messages (alerts and confirmations) via REST APIs. No Node.js, no npm, no start.sh orchestration.

**Tech Stack:** Go, discordgo, go-telegram-bot-api/v5, existing processor packages (gamedata, i18n, geofence, delivery, dts, api)

---

## Sub-Plan Structure

This migration is broken into 6 phases, each producing working, testable software. Phases 1-3 can be developed with the alerter still running (for comparison testing). Phases 4-5 replace the alerter's bot gateways. Phase 6 deletes it.

| Phase | Name | Depends On | Effort |
|-------|------|-----------|--------|
| 1 | [Command Framework & Infrastructure](#phase-1-command-framework--infrastructure) | Nothing | Medium |
| 2 | [Simple & Medium Commands](#phase-2-simple--medium-commands) | Phase 1 | Medium |
| 3 | [Complex Commands](#phase-3-complex-commands) | Phase 2 | High |
| 4 | [Discord Bot](#phase-4-discord-bot-discordgo) | Phase 3 | High |
| 5 | [Telegram Bot](#phase-5-telegram-bot-go-telegram-bot-api) | Phase 3 | Medium |
| 6 | [Cleanup & Deletion](#phase-6-cleanup--deletion) | Phases 4+5 | Low |

---

## Phase 1: Command Framework & Infrastructure

**Goal:** Build the foundation that all commands use — parsing, translation, permission model, reply abstraction, and the `POST /api/command` endpoint for testing commands before bots are ready.

### File Structure

```
processor/internal/bot/
  command.go              # Command interface, registry, Reply types
  parser.go               # Text → parsed command (prefix, underscore, quotes, pipes, translation)
  parser_test.go
  context.go              # CommandContext: user info, permissions, language, platform
  reply.go                # Reply builder: text, embed, react, image, attachment
  registry.go             # Command registration, lookup, help metadata
  registry_test.go
  permissions.go          # Admin check, command security, delegated admin, community admin
  permissions_test.go
  pokemon_resolver.go     # Pokemon name/ID resolution from raw masterfile (fuzzy, aliases)
  pokemon_resolver_test.go
  arg_patterns.go         # Translated regex patterns (distance, template, form, gen, IV, CP, etc.)
  arg_patterns_test.go
  target.go               # buildTarget: resolve who command is for (user, channel, webhook)
  target_test.go

processor/internal/bot/commands/
  (one file per command — created in Phases 2-3)

processor/internal/api/
  command.go              # POST /api/command endpoint (for testing without bots)
```

### Translation Migration

Command names and argument keywords need identifier keys in the processor's i18n system. Add to `processor/internal/i18n/locale/en.json`:

```json
{
  "cmd.track": "track",
  "cmd.untrack": "untrack",
  "cmd.raid": "raid",
  "cmd.egg": "egg",
  "cmd.quest": "quest",
  "cmd.gym": "gym",
  "cmd.lure": "lure",
  "cmd.invasion": "invasion",
  "cmd.incident": "incident",
  "cmd.nest": "nest",
  "cmd.maxbattle": "maxbattle",
  "cmd.area": "area",
  "cmd.profile": "profile",
  "cmd.language": "language",
  "cmd.location": "location",
  "cmd.weather": "weather",
  "cmd.fort": "fort",
  "cmd.help": "help",
  "cmd.info": "info",
  "cmd.start": "start",
  "cmd.stop": "stop",
  "cmd.tracked": "tracked",
  "cmd.poracle": "poracle",
  "cmd.version": "version",
  "cmd.enable": "enable",
  "cmd.disable": "disable",
  "cmd.unregister": "unregister",
  "cmd.userlist": "userlist",
  "cmd.community": "community",
  "cmd.broadcast": "broadcast",
  "cmd.backup": "backup",
  "cmd.restore": "restore",
  "cmd.script": "script",
  "cmd.apply": "apply",
  "cmd.channel": "channel",
  "cmd.webhook": "webhook",
  "cmd.role": "role",
  "cmd.autocreate": "autocreate",
  "cmd.poracle_test": "poracle-test",
  "cmd.poracle_clean": "poracle-clean",
  "cmd.poracle_emoji": "poracle-emoji",
  "cmd.poracle_id": "poracle-id",

  "arg.remove": "remove",
  "arg.everything": "everything",
  "arg.any": "any",
  "arg.clean": "clean",
  "arg.list": "list",
  "arg.add": "add",
  "arg.show": "show",
  "arg.clear": "clear",
  "arg.individually": "individually",
  "arg.include_empty": "include empty",
  "arg.allprofiles": "allprofiles",

  "arg.male": "male",
  "arg.female": "female",
  "arg.genderless": "genderless",

  "arg.valor": "valor",
  "arg.mystic": "mystic",
  "arg.instinct": "instinct",
  "arg.harmony": "harmony",

  "arg.glacial": "glacial",
  "arg.magnetic": "magnetic",
  "arg.mossy": "mossy",
  "arg.rainy": "rainy",
  "arg.sparkly": "sparkly",

  "arg.ex": "ex",
  "arg.shiny": "shiny",
  "arg.gmax": "gmax",

  "arg.rsvp": "rsvp",
  "arg.rsvp_only": "rsvp only",
  "arg.no_rsvp": "no rsvp",

  "arg.pokestop": "pokestop",
  "arg.location": "location",
  "arg.new": "new",
  "arg.removal": "removal",
  "arg.photo": "photo",
  "arg.slot_changes": "slot changes",
  "arg.battle_changes": "battle changes",

  "arg.great": "great",
  "arg.ultra": "ultra",
  "arg.little": "little",
  "arg.pvp": "pvp"
}
```

Translations for other languages are ported from `alerter/locale/{lang}.json`, converting English-as-key entries to `cmd.*` / `arg.*` identifier keys.

### Core Interfaces

```go
// command.go

// Command is implemented by every bot command.
type Command interface {
    // Name returns the canonical command name (e.g. "track").
    Name() string
    // Run executes the command and returns replies.
    Run(ctx *CommandContext, args []string) []Reply
}

// CommandContext carries everything a command needs.
type CommandContext struct {
    UserID        string
    UserName      string
    Platform      string          // "discord" or "telegram"
    ChannelID     string          // channel/group where command was sent
    GuildID       string          // Discord guild (empty for Telegram)
    IsDM          bool            // true if sent via DM/private message
    IsAdmin       bool
    IsCommunityAdmin bool
    Language      string
    ProfileNo     int
    HasLocation   bool
    HasArea       bool

    // Injected dependencies
    DB            *sqlx.DB
    Config        *config.Config
    State         *state.Manager
    GameData      *gamedata.GameData
    Translations  *i18n.Bundle
    Geofence      *geofence.SpatialIndex
    Dispatcher    *delivery.Dispatcher  // for sending replies
    Resolver      *PokemonResolver      // name→ID resolution
    ArgPatterns   *ArgPatterns           // translated regex patterns

    // Platform-specific (set by bot, nil when not applicable)
    Discord       *DiscordPlatform      // guild member fetch, role management
    Telegram      *TelegramPlatform     // group membership checks
}

// Reply represents a response to send back to the user.
type Reply struct {
    Text        string          // plain text (Telegram) or content (Discord)
    Embed       json.RawMessage // Discord embed JSON (nil for text-only)
    React       string          // emoji reaction (e.g. "✅", "👌", "🙅")
    ImageURL    string          // image to attach
    Attachment  *Attachment     // file attachment
    IsDM        bool            // force send as DM even if command was in channel
}

type Attachment struct {
    Filename string
    Content  []byte
}
```

### Parser Design

The parser replaces the alerter's flawed "reverse translate to English" model with **direct identifier-key lookup**. Instead of translating user input back to English (which causes collisions when different languages share words), the parser builds a multi-language lookup table: every translated form of every command/keyword maps directly to its identifier key.

```go
// parser.go

// ParsedCommand represents one command invocation after parsing.
type ParsedCommand struct {
    CommandKey string   // identifier key (e.g. "cmd.track", "cmd.raid") — NOT English text
    Args       []string // remaining arguments (lowercased, underscores→spaces)
}

// Parser handles text → structured command parsing.
type Parser struct {
    prefix     string
    commandMap map[string]string // "track"→"cmd.track", "verfolgen"→"cmd.track", "suivre"→"cmd.track"
    keywordMap map[string]string // "remove"→"arg.remove", "entfernen"→"arg.remove"
}

// NewParser builds the multi-language lookup tables from the i18n bundle.
// For each command identifier (cmd.track, cmd.raid, ...), it loads all
// translations across all languages and maps them to the identifier key.
// Longer translations are checked first to avoid partial matches.
func NewParser(prefix string, bundle *i18n.Bundle, languages []string) *Parser

// Parse splits raw message text into one or more commands.
func (p *Parser) Parse(text string) []ParsedCommand
```

Key parsing rules:
1. Strip prefix (e.g. `!`)
2. Split by newlines (multi-line commands)
3. For each line: tokenize preserving quoted strings (`"quoted arg"`)
4. Lowercase all tokens
5. Replace underscores with spaces in tokens (e.g. `slot_changes` → `slot changes`)
6. Look up first token in `commandMap` → get identifier key (e.g. `"cmd.track"`)
7. Split remaining tokens by `|` pipe for command groups
8. Each group shares the same command key but has different args

**No reverse translation.** The commandMap is a flat lookup built at startup from all configured languages. If German `"verfolgen"` and French `"suivre"` both map to `cmd.track`, there are no collisions — each maps to the identifier key directly.

Commands match on identifier keys, not English text:
```go
func (r *Registry) Lookup(key string) Command {
    return r.commands[key] // key is "cmd.track", not "track"
}
```

### User Language + English Fallback

The command name is resolved from the global multi-language lookup (all configured languages). But argument parsing uses only **two languages**: the user's configured language from the database, plus English as a universal fallback. This avoids cross-language collisions while allowing users to mix their language with English (which is how most bilingual communities actually use Poracle).

```go
func (b *Bot) handleCommand(userID, text string) {
    // 1. Resolve command name (multi-language lookup — no ambiguity for identifier keys)
    cmdKey := parser.LookupCommand(tokens[0])

    // 2. Get user's language from DB (or config default locale if unregistered)
    lang := getUserLanguage(userID) // "de", "fr", "en", etc.

    // 3. Parse arguments using user's language + English fallback
    params := command.Params()
    parsed := parser.MatchArgs(tokens[1:], params, lang)
}
```

### Concrete Example

Poracle has 3 languages configured: English, German, French. User `James` has `language = "de"`.

James types: `!track relaxo iv100 d500 remove`

**Command lookup:** `"track"` matches English entry in global map → `cmd.track`. We don't need to detect language here — we already know James speaks German from the DB.

**Argument matching** — the `!track` command declares its parameter types. The parser walks args trying each type's matcher with German (de) + English (en) fallback:

| Token | Matchers tried | Result |
|-------|---------------|--------|
| `relaxo` | Prefix patterns (iv, cp, d...) → no match. Keywords (de: entfernen, alles...) → no. Keywords (en: remove, everything...) → no. Pokemon names (de) → **"Relaxo" = Snorlax ID 143** ✅ |
| `iv100` | IV prefix matcher → **min_iv=100** ✅ |
| `d500` | Distance matcher (de: entfernung) → no. Distance matcher (en: d) → **distance=500** ✅ |
| `remove` | Keywords (de: entfernen) → no. Keywords (en: remove) → **arg.remove** ✅ |

James could equally type `!track relaxo iv100 entfernung500 entfernen` — German keywords matched via user language. Or mix: `!verfolgen relaxo iv100 d500 entfernen` — German command name, English distance, German remove. Both work.

**French user `Marie`** with `language = "fr"` types: `!suivre ronflex iv100 d500 supprimer`

| Token | Result |
|-------|--------|
| `suivre` | Command map → `cmd.track` |
| `ronflex` | Pokemon names (fr) → Snorlax ID 143 ✅ |
| `iv100` | IV matcher (structural, same in all languages) ✅ |
| `d500` | Distance matcher (en fallback) ✅ |
| `supprimer` | Keywords (fr) → `arg.remove` ✅ |

**Key:** Only two languages are tried per user (their language + English), never all three. French keywords never interfere with German parsing. The only collision risk is between the user's language and English, which is a much smaller surface area.

**Unregistered users** (before `!poracle`): fall back to `[general] locale` from config.
```

### Typed Parameter Matchers

Instead of a flat keyword map (which has the same collision problem as reverse translation), each command declares what **parameter types** it accepts. The parser tries each declared type's matcher against the args, using the detected language.

```go
// Each command declares its parameter schema
func (c *TrackCommand) Params() []ParamType {
    return []ParamType{
        PokemonName,    // matches pokemon names in user's language → ID
        TypeName,       // matches type names → type ID
        FormPrefix,     // matches form:<name> → form ID
        GenPrefix,      // matches gen<N> → generation int
        IVRange,        // matches iv<min>-<max> → min, max
        CPRange,        // matches cp<min>-<max> → min, max
        Distance,       // matches d<N> → meters int
        Template,       // matches t:<name> → template string
        PVPLeague,      // matches great<rank>, ultra<rank> → league, rank
        Gender,         // matches male/female/genderless (translated) → gender int
        Keyword,        // matches remove, everything, clean, individually (translated)
    }
}
```

Each `ParamType` matcher knows how to match a token using the translator for the detected language. Pokemon names are only tried in commands that accept them. Team names only in commands that accept teams. No global ambiguity.

**Priority ordering** matters: prefix patterns (`iv100`, `cp2500`, `d500`, `form:alola`) are tried first since they're structurally unambiguous. Then bare keywords (`remove`, `clean`, `male`). Then pokemon/type names last as the fallback catch-all. A token consumed by one matcher is removed from the list so later matchers don't re-match it.

This is the same model as the alerter's `!track` command (which uses `parameterDefinition` with ordered regex patterns), but formalized as a type system shared across all commands.

### Pokemon Resolver

Uses the raw masterfile (already loaded in `gamedata` package) instead of monsters.json:

```go
// pokemon_resolver.go

type PokemonResolver struct {
    gameData    *gamedata.GameData
    translations *i18n.Bundle
    aliases     map[string]int  // from pokemonAlias.json
}

// Resolve takes a user-typed name (any language) and returns matching pokemon IDs.
// Handles: exact match, alias lookup, fuzzy match, numeric IDs.
func (r *PokemonResolver) Resolve(name string, language string) []ResolvedPokemon

type ResolvedPokemon struct {
    PokemonID int
    Form      int
}
```

The resolver uses `poke_{id}` translation keys from `resources/locale/{lang}.json` for multi-language pokemon name matching. Aliases come from `config/pokemonAlias.json` (already loaded by the processor).

### Argument Patterns

Translated regex patterns for filter arguments:

```go
// arg_patterns.go

type ArgPatterns struct {
    Distance   *regexp.Regexp // d<number> or distance<number>
    Template   *regexp.Regexp // t:<name> or template:<name>
    Form       *regexp.Regexp // form:<name>
    Gen        *regexp.Regexp // gen<number>
    IV         *regexp.Regexp // iv<min>-<max> or iv<exact>
    MinIV      *regexp.Regexp // miniv<number>
    MaxIV      *regexp.Regexp // maxiv<number>
    CP         *regexp.Regexp // cp<min>-<max>
    MinCP      *regexp.Regexp // mincp<number>
    MaxCP      *regexp.Regexp // maxcp<number>
    Level      *regexp.Regexp // level<min>-<max>
    Atk        *regexp.Regexp // atk<min>-<max>
    Def        *regexp.Regexp // def<min>-<max>
    Sta        *regexp.Regexp // sta<min>-<max>
    Weight     *regexp.Regexp // weight<min>-<max>
    Rarity     *regexp.Regexp // rarity<min>-<max>
    Size       *regexp.Regexp // size<min>-<max>
    Time       *regexp.Regexp // t<seconds>
    Move       *regexp.Regexp // move:<name>
    Cap        *regexp.Regexp // cap<number>
    LatLon     *regexp.Regexp // <lat>,<lon>
    Great      *regexp.Regexp // great<rank>
    Ultra      *regexp.Regexp // ultra<rank>
    Little     *regexp.Regexp // little<rank>
    GreatHigh  *regexp.Regexp // greathigh<rank>
    UltraHigh  *regexp.Regexp // ultrahigh<rank>
    LittleHigh *regexp.Regexp // littlehigh<rank>
    MinSpawn   *regexp.Regexp // minspawn<number>
}

// NewArgPatterns builds patterns from translated keywords for a given language.
func NewArgPatterns(tr *i18n.Translator) *ArgPatterns
```

Each pattern is built from the translated keyword. For example, if German translates "d" to "entfernung", the distance pattern matches both `d500` and `entfernung500`.

### POST /api/command Endpoint

For testing commands before bots are wired up:

```go
// api/command.go

// POST /api/command
// Request: {"text": "!track pikachu iv100 d500", "user_id": "123", "user_name": "James",
//           "platform": "discord", "channel_id": "456", "guild_id": "789", "is_dm": true}
// Response: {"status": "ok", "replies": [{"text": "...", "react": "✅"}]}
```

### Existing Processor Code Reused by Commands

The processor already has significant infrastructure that commands will use directly:

- **`internal/api/tracking.go`** — `diffTracking()` reflection-based comparison (insert/update/duplicate detection), all tracking CRUD helpers, `flexBool`/`flexInt` coercion. Commands don't reimplement diff logic — they populate typed structs and call the existing functions.
- **`internal/rowtext/`** — Human-readable description generators for all 10 tracking types (`MonsterRowText`, `RaidRowText`, `EggRowText`, etc.). Used by `!tracked` for listing and by tracking commands for confirmation messages.
- **`internal/db/`** — Typed structs (`db.MonsterTracking`, `db.RaidTracking`, etc.) with field definitions. Commands parse args into these structs.
- **`internal/gamedata/`** — Raw masterfile data (pokemon, moves, types, items, grunts, util). Commands use this for name resolution, type lookups, generation ranges.
- **`internal/i18n/`** — Translation bundle with identifier keys. Commands use this for translating keywords and reply messages.
- **`internal/geofence/`** — Spatial index for area validation and point-in-polygon checks.
- **`internal/delivery/`** — Dispatcher for sending confirmation replies directly (replaces alerter's `postMessage`).

The command framework's job is essentially: **parse text args → populate typed DB structs → call existing CRUD/diff → format reply using rowtext**. The heavy lifting is already done.

### Confirmation Messages

Currently the processor calls `POST /api/postMessage` on the alerter for tracking confirmation messages. Instead, send them through the existing `delivery.Dispatcher`:

```go
// In api/tracking.go, after successful tracking mutation:
// Instead of: POST /api/postMessage to alerter
// Do: dispatcher.Dispatch(&delivery.Job{Target: id, Type: humanType, Message: confirmJSON, ...})
```

### Config & Masterdata Endpoints

Move from alerter to processor:

- `GET /api/config/poracleWeb` — serve config subset from already-loaded TOML config
- `GET /api/masterdata/monsters` — serve the poracle-v2 format `resources/data/monsters.json` (already downloaded at startup). PoracleWeb expects this legacy format. Serve the file directly rather than converting from raw masterfile.
- `GET /api/masterdata/grunts` — serve `resources/data/grunts.json` (already downloaded). Same legacy format for PoracleWeb compatibility.

**Note:** These endpoints serve the old poracle-v2 format for backward compatibility with PoracleWeb. New raw masterfile APIs for updated clients will be added in a later phase when PoracleWeb is updated. The processor's internal command processing uses the raw masterfile via `gamedata.GameData`, not these legacy endpoints.

---

## Phase 2: Simple & Medium Commands

**Goal:** Implement the 20+ simpler commands using the framework from Phase 1. Each command is one file in `processor/internal/bot/commands/`.

### File Structure

```
processor/internal/bot/commands/
  start.go          # !start — enable user
  stop.go           # !stop — disable user
  enable.go         # !enable — admin enable user(s)
  disable.go        # !disable — admin disable user(s)
  language.go       # !language — set user language
  unregister.go     # !unregister — delete user and tracking
  version.go        # !version — show version info
  area.go           # !area — list/add/remove areas, maps
  location.go       # !location — set location (geocoding, maps)
  profile.go        # !profile — create/delete/switch profiles
  egg.go            # !egg — egg tracking
  lure.go           # !lure — lure tracking
  gym.go            # !gym — gym tracking
  fort.go           # !fort — fort update tracking
  incident.go       # !incident/!invasion — grunt tracking
  nest.go           # !nest — nest tracking
  untrack.go        # !untrack — remove pokemon tracking
  tracked.go        # !tracked — list all tracking
  community.go      # !community — manage memberships (admin)
  userlist.go       # !userlist — list users (admin)
  poracle.go        # !poracle — user registration
  weather.go        # !weather — weather tracking
```

### Command Pattern

Every command follows the same pattern:

```go
// commands/start.go
package commands

type StartCommand struct{}

func (c *StartCommand) Name() string { return "start" }

func (c *StartCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
    _, err := ctx.DB.Exec("UPDATE humans SET enabled = 1, fails = 0 WHERE id = ?", ctx.UserID)
    if err != nil {
        return []bot.Reply{{React: "🙅"}}
    }
    ctx.TriggerReload()
    return []bot.Reply{
        {Text: ctx.Translate("cmd.start.success"), React: "✅"},
    }
}
```

### Command-by-Command Notes

**Simple (direct port):**

| Command | Key Behavior | Notes |
|---------|-------------|-------|
| `start` | Set `enabled=1, fails=0` | Warn if already started |
| `stop` | Set `enabled=0` | Warn if args provided (common mistake: `!stop pokemon`) |
| `enable` | Admin: set `admin_disable=0` for target IDs | Supports user mentions, numeric IDs |
| `disable` | Admin: set `admin_disable=1` for target IDs | Same target resolution as enable |
| `language` | Set `language` column | Match by code or display name, validate against `availableLanguages` |
| `unregister` | Admin: delete human + all tracking. Non-admin: self only | Cascading delete via `deleteHuman()` |
| `version` | Show version string | Admin: include git status/commits |

**Medium (need careful porting):**

| Command | Key Behavior | Special Notes |
|---------|-------------|--------------|
| `area` | Subcommands: list, add, remove, show, overview | Area security filtering by community membership. Map generation via existing tile APIs. Distance regex parsing. |
| `location` | Geocoding → set lat/lon | Forward geocode via `/api/geocode/forward`. Area restriction validation. Map embed generation. `remove` sets to 0,0. |
| `profile` | CRUD profiles | Auto-increment profile_no. Cascading delete (all tracking tables). Switch active profile. Copy area/location from parent. |
| `egg` | Level-based tracking | `level` can be single int or array. `everything` = level 90. Team names (translated). EX filter. RSVP modes. Compare existing vs new for updates. |
| `lure` | Lure type tracking | Map lure names to IDs (501-506). `everything` = ID 0. |
| `gym` | Team + slot/battle changes | Team names (translated + color aliases: red/blue/yellow). `battle_changes` gated by config. `everything` = team 4. |
| `fort` | Fort type + change types | Fort type: pokestop/gym/everything. Change types: location/name/photo/removal/new. `include_empty` flag. |
| `incident` | Grunt type + gender | Grunt name resolution from `gamedata.Grunts`. Gender filter. Also accepts pokestop event types from `utilData.pokestopEvent`. Shares entry point with `!invasion`. |
| `nest` | Pokemon + spawn avg | Pokemon resolver for names. `minspawn` threshold. Type/gen filtering. |
| `untrack` | Remove pokemon by name/type/everything | Pokemon resolver. Type-based filtering (remove all grass types). |
| `tracked` | List all tracking | Query all 10 tracking tables + profiles. Format with `rowtext` descriptions (already in processor). |
| `community` | Manage community memberships | Admin only. Subcommands: add, remove, show, clear, list. Updates `community_membership` and `area_restriction` on humans table. |
| `userlist` | List registered users | Admin/community-admin. Filters: enabled/disabled/discord/telegram/webhook/user/group/channel. Community filtering. |
| `poracle` | User registration | **Different for Discord vs Telegram.** Discord: DM or channel, area security, community auto-assign, greeting via DTS render. Telegram: group-only, different greeting flow. |
| `weather` | Weather change tracking | Weather condition names (translated). Similar insert/update pattern. |

### Tracking Command Shared Logic

All tracking commands (egg, lure, gym, fort, incident, nest, weather) follow the same pattern:

1. Parse args → extract filters, distance, template, clean, remove flag
2. If `remove`: query existing, delete matching, reply with summary
3. If not remove: build tracking objects, compare with existing (same key fields → update, else insert)
4. Trigger state reload
5. Reply with per-entry details or summary (if >20 changes, summarize)

This shared logic should be extracted into helpers in `processor/internal/bot/`:

```go
// tracking_helpers.go
func CompareAndApply[T any](ctx *CommandContext, table string, existing []T, incoming []T, keyFunc func(T) string) TrackingResult
```

---

## Phase 3: Complex Commands

**Goal:** Implement the hardest commands that require significant game data integration, complex argument parsing, or multiple modes.

### File Structure

```
processor/internal/bot/commands/
  track.go          # !track — pokemon tracking (THE most complex)
  track_test.go     # extensive tests for argument parsing
  raid.go           # !raid — raid tracking
  quest.go          # !quest — quest tracking (3 modes)
  maxbattle.go      # !maxbattle — max battle tracking
  help.go           # !help — DTS template rendering
  info.go           # !info — pokemon info, stats, weather
  script.go         # !script — export tracking as commands
  backup.go         # !backup — save tracking to file
  restore.go        # !restore — load tracking from file
  broadcast.go      # !broadcast — mass messaging (admin)
  apply.go          # !apply — execute as other users (admin)
  poracle_test.go   # !poracle-test — test webhooks (already mostly in processor)
```

### Command-by-Command Notes

| Command | Complexity | Key Challenges |
|---------|-----------|---------------|
| `track` | **VERY HIGH** | 20+ regex patterns. Pokemon name resolution (multi-language, fuzzy, aliases). PVP ranking with league filters (great/ultra/little + cap + high/low). Everything-flag permissions (3 modes: deny, require-filter, allow). Form filtering. Type filtering (all pokemon of a type). Generation filtering. `individually` flag (splits into per-pokemon entries). Default/max distance enforcement from config. |
| `raid` | **HIGH** | Dual mode: by pokemon name OR by level. Move filtering (by name or by type). Form/type/gen filtering. EX gym filter. RSVP modes. `pokemon_form` array support. |
| `quest` | **HIGH** | Three separate modes: pokemon rewards, item rewards, stardust. Each mode has different fields. PVP ranking for pokemon rewards. Stardust thresholds. Energy/candy sub-types. `everything` permission gating. |
| `maxbattle` | **HIGH** | Pokemon + move + gmax filtering. Type/gen filtering. Similar to raid but with dynamax-specific fields. |
| `help` | **MEDIUM** | Fetch available DTS templates from `GET /api/config/templates`. Render help template via `POST /api/dts/render`. Language detection from command aliases. Cache template list. |
| `info` | **HIGH** | Multiple subcommands: `poracle` (queue stats), `translate` (debug), `dts` (template info), `shiny`, `rarity`, `moves`, `items`, `weather`, `<pokemon>`. Pokemon info needs type matchups, evolution chains, base stats, weather boost, move pools. Uses processor's existing gamedata + enrichment. |
| `script` | **MEDIUM** | Reverse-engineer command text from tracking DB rows. Map DB fields back to command arguments. Multi-line output. Profile iteration. |
| `backup` | **SIMPLE** | Query all tracking tables, write JSON to `backups/` directory. |
| `restore` | **SIMPLE** | Read JSON from `backups/`, delete existing, insert restored data. |
| `broadcast` | **MEDIUM** | Haversine SQL query for location-based targeting. Platform-specific message building. Dry-run mode. Message queue integration. |
| `apply` | **MEDIUM** | Parse target IDs/names, look up humans, execute command with overridden context. Dynamic command dispatch. |
| `poracle-test` | **LOW** | Already mostly implemented — `POST /api/test` exists. Just need command argument parsing (type, test-id, template, language). |

### !track Deep Dive

The most complex command. Argument parsing groups:

```
Pokemon resolution:    pokemon names, IDs, aliases, "everything"
Form filter:           form:<name>
Type filter:           pokemon type names (grass, fire, etc.)
Generation filter:     gen<N>
IV filter:             iv<min>-<max>, iv<exact>, miniv<N>, maxiv<N>
CP filter:             cp<min>-<max>, mincp<N>, maxcp<N>
Level filter:          level<min>-<max>, minlevel<N>, maxlevel<N>
Stat filters:          atk<min>-<max>, def<min>-<max>, sta<min>-<max>
Weight filter:         weight<min>-<max>, maxweight<N>
Rarity filter:         rarity<min>-<max>, maxrarity<N>
Size filter:           size<min>-<max>
Time filter:           t<seconds>
Gender filter:         male, female, genderless
PVP filter:            great<rank>, ultra<rank>, little<rank> (+ high, cp variants)
PVP cap:               cap<level>
Distance:              d<meters>
Template:              t:<name> (conflicts with time! resolved by colon presence)
Clean:                 clean
Individually:          individually
Remove:                remove
```

The `everything` flag has 3 permission modes configured in `[tracking] everything_flag_permissions`:
- `"deny"` — refuse the command
- `"require-pokemon-filter"` — allow only with stat/PVP filters
- `"allow"` — allow unconditionally

### Testing Strategy for Phase 3

Each complex command needs table-driven tests covering:
- Valid argument combinations
- Edge cases (empty args, conflicting filters, unknown pokemon)
- Permission checks (admin-only, everything flag)
- Multi-language argument parsing
- Comparison with existing tracking (insert vs update vs unchanged)

Use the existing alerter test suite as reference: `alerter/src/lib/poracleMessage/commands/test/`

---

## Phase 4: Discord Bot (discordgo)

**Goal:** Replace Discord.js with discordgo. Handle gateway events, command routing, Discord-specific commands, and reconciliation.

### File Structure

```
processor/internal/discordbot/
  bot.go                # Discord bot lifecycle: connect, event handlers, shutdown
  bot_test.go
  handler.go            # messageCreate handler: parse → command → reply
  registration.go       # !poracle command (Discord-specific registration flow)
  channel.go            # !channel — register channels/webhooks
  webhook.go            # !webhook — Discord webhook API management
  role.go               # !role — multi-guild role management
  role_setter.go        # DiscordRoleSetter equivalent
  autocreate.go         # !autocreate — bulk channel/role creation
  clean.go              # !poracle-clean — delete bot messages
  emoji.go              # !poracle-emoji — upload uicons emojis
  id_export.go          # !poracle-id — export emoji/role IDs
  reconciliation.go     # Periodic role sync + event-driven member updates
  reconciliation_test.go
  permissions.go        # Discord permission model (roles, guilds, delegated admin)
  message.go            # Discord message sending (split at 2000 chars, embeds)
  suggest.go            # suggest_on_dm: suggest commands for unrecognised DMs
```

### Key Design Points

**Gateway Setup:**
```go
func NewDiscordBot(cfg *config.Config, ...) *DiscordBot {
    dg, _ := discordgo.New("Bot " + token)
    dg.Identify.Intents = discordgo.IntentsGuildMessages |
        discordgo.IntentsDirectMessages |
        discordgo.IntentsGuildMembers |
        discordgo.IntentsGuildMessageReactions
    dg.AddHandler(bot.onMessageCreate)
    dg.AddHandler(bot.onGuildMemberUpdate)
    dg.AddHandler(bot.onGuildMemberRemove)
    dg.AddHandler(bot.onChannelDelete)
    dg.Open()
}
```

**Message Create Handler:**
```go
func (b *DiscordBot) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
    // 1. Ignore bots (unless admin)
    // 2. Log DMs to dmLogChannel if configured
    // 3. Check prefix
    // 4. Parse command text → []ParsedCommand
    // 5. Build CommandContext with Discord permissions
    // 6. Execute each command
    // 7. Send replies (text, embeds, reactions)
    // 8. If DM and unrecognised: suggest_on_dm behaviour
}
```

**Discord-Only Commands:**

| Command | Key Implementation Notes |
|---------|------------------------|
| `channel` | `s.Channel(id)` for validation. Register as `discord:channel` or `webhook` in humans table. Webhook URL validation. |
| `webhook` | `s.ChannelWebhooks(channelID)` for listing. `s.WebhookCreate()` for creation. Admin permission check. |
| `role` | Multi-guild: iterate `cfg.Discord.Guilds`. `s.GuildMember()` to fetch roles. `s.GuildMemberRoleAdd/Remove()`. Exclusive role sets from `cfg.Discord.UserRoleSubscription`. |
| `autocreate` | Load template from `config/channelTemplate.json`. `s.GuildChannelCreateComplex()` for categories/channels. `s.GuildRoleCreate()`. Permission overwrites via discordgo. Execute sub-commands per created channel. |
| `poracle-emoji` | Fetch uicons index. `s.GuildEmojiCreate()`. Generate emoji.json config. |
| `poracle-clean` | `s.ChannelMessages(id, 100)`. Filter by bot author. `s.ChannelMessageDelete()`. |
| `poracle-id` | `s.GuildEmojis(guildID)`. `s.GuildRoles(guildID)`. Format as text attachment. |

**Reconciliation:**

Port `discordReconciliation.js` logic:
- Periodic sync (configurable interval, default 1 hour)
- `s.GuildMembers()` to fetch all members (paginated, 1000 at a time)
- 15s stagger between guilds
- Role membership → community mapping
- Greeting/goodbye via DTS render + dispatcher delivery
- Event-driven: `onGuildMemberUpdate` and `onGuildMemberRemove` for immediate detection
- `blocked_alerts` sync from `command_security` config

**Message Sending:**

Discord has a 2000-char limit. Split logic:
```go
func splitMessage(text string, maxLen int) []string {
    // Split at newlines, not mid-line
    // If a single line > maxLen, split at maxLen
}
```

For embeds, use discordgo's `MessageSend` struct directly.

---

## Phase 5: Telegram Bot (go-telegram-bot-api)

**Goal:** Replace Telegraf with go-telegram-bot-api. Handle polling/webhook updates, command routing, and Telegram-specific features.

### File Structure

```
processor/internal/telegrambot/
  bot.go                # Telegram bot lifecycle: start polling, shutdown
  bot_test.go
  handler.go            # Update handler: parse → command → reply
  registration.go       # !poracle command (Telegram-specific: group-only)
  reconciliation.go     # Group membership verification
  message.go            # Telegram message sending (split at 4095 chars, markdown)
```

### Key Design Points

**Bot Setup:**
```go
func NewTelegramBot(cfg *config.Config, ...) *TelegramBot {
    bot, _ := tgbotapi.NewBotAPI(token)
    u := tgbotapi.NewUpdate(0)
    u.Timeout = 60
    updates := bot.GetUpdatesChan(u)
    go b.processUpdates(updates)
}
```

**Update Handler:**
```go
func (b *TelegramBot) processUpdates(updates tgbotapi.UpdatesChannel) {
    for update := range updates {
        if update.Message == nil { continue }
        // 1. Check if message or channel_post
        // 2. Handle location messages (set user location)
        // 3. Check prefix
        // 4. Parse command text → []ParsedCommand
        // 5. Build CommandContext (simpler permissions than Discord)
        // 6. Execute each command
        // 7. Send replies (text with parse_mode, photos)
    }
}
```

**Telegram-Specific:**
- No role-based permissions (`commandAllowed` always returns true)
- Registration only in groups, not DMs
- 4095-char message limit (vs Discord's 2000)
- Markdown formatting (MarkdownV2 parse mode)
- Location messages handled as `!location <lat>,<lon>`
- No webhooks, emojis, channels, or role management

**Reconciliation:**
- Simpler than Discord: verify group membership via `getChatMember` Bot API
- Community-based access control (area security)
- Greeting/goodbye via DTS render + dispatcher

---

## Phase 6: Cleanup & Deletion

**Goal:** Remove everything Node.js and simplify deployment to a single binary.

### Deletions

```
DELETE: alerter/                    # Entire Node.js application
DELETE: start.sh                    # Two-process orchestration
DELETE: ecosystem.config.js         # pm2 config
DELETE: package.json                # Root npm config (if exists)
DELETE: .npmrc                      # npm config (if exists)
```

### Modifications

```
MODIFY: processor/cmd/processor/main.go
  - Remove alerter proxy middleware
  - Remove alerter health check wait
  - Start Discord + Telegram bots in main()
  - Single-process shutdown: bots → render → dispatcher → cleanup

MODIFY: processor/internal/config/config.go
  - Remove alerter_url field
  - Add discord bot fields if not already present
  - Add telegram bot fields if not already present

MODIFY: Dockerfile
  - Remove Node.js build stage
  - Remove npm install
  - Single Go binary stage
  - Much smaller image

MODIFY: Makefile
  - Remove install-alerter target
  - Remove alerter-related targets
  - Simplify build to just Go

MODIFY: README.md
  - Single binary architecture
  - Remove alerter setup instructions
  - Update architecture diagram
  - Simplify quick start

MODIFY: CLAUDE.md
  - Remove all alerter references
  - Update architecture description
  - Single process model
```

### Shutdown Ordering (Updated)

```
Signal received
  → Discord bot: close gateway connection
  → Telegram bot: stop polling
  → Cancel webhook context (stop accepting new webhooks)
  → Wait for webhook workers to drain
  → Close render channel, wait for render workers
  → Stop dispatcher (drains delivery queue)
  → Close sender
  → Close static map, geocoder, duplicates, rate limiter
  → Save tracker state to disk
```

---

## Testing Strategy

### Unit Tests
- Every command has table-driven tests for argument parsing
- Parser tests for edge cases (quotes, pipes, multi-line, unicode)
- Pokemon resolver tests (exact match, fuzzy, alias, multi-language)
- Permission tests (admin, community admin, command security)

### Integration Tests
- `POST /api/command` endpoint with real database
- Compare output with alerter for same inputs (parity testing)
- Bot-level tests with mock Discord/Telegram sessions

### Parity Testing
During development (Phases 1-3), the alerter is still running. Test parity by:
1. Send command to alerter via Discord/Telegram
2. Send same command text to `POST /api/command`
3. Compare replies and database state

### Migration Testing Checklist
Before Phase 6 (deletion):
- [ ] All 32+ commands produce equivalent output
- [ ] Registration works for Discord DM, Discord channel, Telegram group
- [ ] Discord reconciliation: role sync, greeting, goodbye, blocked_alerts
- [ ] Telegram reconciliation: group membership verification
- [ ] Multi-language commands (German, French at minimum)
- [ ] Area security: community filtering, location restrictions
- [ ] PVP tracking with all league/cap combinations
- [ ] Webhook and channel registration/management
- [ ] Role management with exclusive sets
- [ ] Backup/restore round-trip
- [ ] poracle-test for all webhook types
- [ ] Confirmation messages delivered via processor (not postMessage)
- [ ] Clean message deletion on TTH expiry
- [ ] Edit-before-send for weather/pokemon changes

---

## Estimated Effort

| Phase | Effort | Can Parallel? |
|-------|--------|--------------|
| Phase 1: Framework | 1 week | No (foundation) |
| Phase 2: Simple/Medium Commands | 1-2 weeks | After Phase 1 |
| Phase 3: Complex Commands | 2 weeks | After Phase 2 |
| Phase 4: Discord Bot | 1-2 weeks | After Phase 3 |
| Phase 5: Telegram Bot | 1 week | After Phase 3, parallel with Phase 4 |
| Phase 6: Cleanup | 2-3 days | After Phases 4+5 |
| **Total** | **6-8 weeks** | |

Phases 4 and 5 can run in parallel since they're independent platform implementations using the same command framework.
