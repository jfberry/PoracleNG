# Phase 1: Command Framework & Infrastructure

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the platform-agnostic command framework that all bot commands use — parsing, argument matching, permission model, reply abstraction — and implement the first 3 commands (start, egg, track) to prove the framework handles simple through complex cases.

**Architecture:** Commands are Go functions that receive structured `ParsedArgs` (not raw strings) and return `[]Reply`. A `Parser` tokenizes input text and looks up the command by identifier key. An `ArgMatcher` walks tokens against the command's declared parameter types using the user's language + English fallback. The framework reuses existing processor packages: `db/` for structs, `api/tracking.go` for diff logic, `rowtext/` for descriptions, `gamedata/` for pokemon/move/type data, `i18n/` for translations.

**Tech Stack:** Go, existing processor packages, no new dependencies

**Parent plan:** `docs/superpowers/plans/2026-03-30-goodbye-node.md`

---

## File Structure

```
processor/internal/bot/
  parser.go               # Tokenize, prefix strip, command lookup, pipe split, multi-line
  parser_test.go
  argmatch.go             # ArgMatcher: walk tokens against declared params, user lang + English
  argmatch_test.go
  params.go               # ParamDef types: PrefixNumeric, PrefixString, Keyword, PokemonName, etc.
  params_test.go
  pokemon_resolver.go     # Name → ID resolution (raw masterfile + translations + aliases)
  pokemon_resolver_test.go
  command.go              # Command interface, CommandContext, Reply, ParsedArgs
  registry.go             # Command registry, lookup by identifier key
  registry_test.go
  permissions.go          # Admin, command_security, delegated_admin, community_admin
  permissions_test.go
  target.go               # buildTarget: resolve who command is for (self, channel, webhook, user override)
  target_test.go

processor/internal/bot/commands/
  start.go                # !start — prove simple command works
  start_test.go
  egg.go                  # !egg — prove medium command (levels, teams, RSVP)
  egg_test.go
  track.go                # !track — prove complex command (pokemon names, PVP, everything flag)
  track_test.go

processor/internal/api/
  command.go              # POST /api/command endpoint

processor/internal/i18n/locale/
  en.json                 # Add cmd.* and arg.* keys
  de.json                 # Port from alerter/locale/de.json
  fr.json                 # Port from alerter/locale/fr.json
  (other languages)
```

---

## Task 1: Translation Key Migration

**Files:**
- Modify: `processor/internal/i18n/locale/en.json`
- Modify: `processor/internal/i18n/locale/de.json` (and other locale files)

Add identifier keys for all command names and argument keywords. These replace the alerter's English-as-key translations.

- [ ] **Step 1: Add English command and argument keys to en.json**

Add these keys (showing subset — full list in parent plan):

```json
{
  "cmd.track": "track",
  "cmd.raid": "raid",
  "cmd.egg": "egg",
  "cmd.start": "start",
  "cmd.stop": "stop",
  "...": "...",

  "arg.remove": "remove",
  "arg.everything": "everything",
  "arg.clean": "clean",
  "arg.individually": "individually",
  "arg.ex": "ex",
  "arg.shiny": "shiny",
  "arg.male": "male",
  "arg.female": "female",
  "arg.genderless": "genderless",
  "arg.valor": "valor",
  "arg.mystic": "mystic",
  "arg.instinct": "instinct",
  "arg.harmony": "harmony",
  "arg.red": "red",
  "arg.blue": "blue",
  "arg.yellow": "yellow",
  "arg.gray": "gray",
  "arg.rsvp": "rsvp",
  "arg.no_rsvp": "no rsvp",
  "arg.rsvp_only": "rsvp only",
  "arg.slot_changes": "slot changes",
  "arg.battle_changes": "battle changes",
  "arg.include_empty": "include empty",
  "arg.pokestop": "pokestop",
  "arg.gym": "gym",
  "arg.location": "location",
  "arg.new": "new",
  "arg.removal": "removal",
  "arg.photo": "photo",
  "arg.list": "list",
  "arg.add": "add",
  "arg.show": "show",
  "arg.overview": "overview",
  "arg.switch": "switch",
  "arg.pvp": "pvp",
  "arg.great": "great",
  "arg.ultra": "ultra",
  "arg.little": "little",
  "arg.gmax": "gmax",

  "arg.prefix.d": "d",
  "arg.prefix.t": "t",
  "arg.prefix.iv": "iv",
  "arg.prefix.miniv": "miniv",
  "arg.prefix.maxiv": "maxiv",
  "arg.prefix.cp": "cp",
  "arg.prefix.mincp": "mincp",
  "arg.prefix.maxcp": "maxcp",
  "arg.prefix.level": "level",
  "arg.prefix.maxlevel": "maxlevel",
  "arg.prefix.atk": "atk",
  "arg.prefix.maxatk": "maxatk",
  "arg.prefix.def": "def",
  "arg.prefix.maxdef": "maxdef",
  "arg.prefix.sta": "sta",
  "arg.prefix.maxsta": "maxsta",
  "arg.prefix.weight": "weight",
  "arg.prefix.maxweight": "maxweight",
  "arg.prefix.rarity": "rarity",
  "arg.prefix.maxrarity": "maxrarity",
  "arg.prefix.size": "size",
  "arg.prefix.maxsize": "maxsize",
  "arg.prefix.gen": "gen",
  "arg.prefix.cap": "cap",
  "arg.prefix.form": "form",
  "arg.prefix.template": "template",
  "arg.prefix.move": "move",
  "arg.prefix.great": "great",
  "arg.prefix.greathigh": "greathigh",
  "arg.prefix.greatcp": "greatcp",
  "arg.prefix.ultra": "ultra",
  "arg.prefix.ultrahigh": "ultrahigh",
  "arg.prefix.ultracp": "ultracp",
  "arg.prefix.little": "little",
  "arg.prefix.littlehigh": "littlehigh",
  "arg.prefix.littlecp": "littlecp",
  "arg.prefix.minspawn": "minspawn",
  "arg.prefix.name": "name",
  "arg.prefix.user": "user",
  "arg.prefix.channel": "channel",
  "arg.prefix.guild": "guild",

  "raid.level.legendary": "legendary",
  "raid.level.mega": "mega",
  "raid.level.mega_legendary": "mega legendary",
  "raid.level.ultra_beast": "ultra beast",
  "raid.level.elite": "elite",
  "raid.level.primal": "primal",
  "raid.level.shadow": "shadow",
  "raid.level.shadow_legendary": "shadow legendary"
}
```

- [ ] **Step 2: Port German translations from alerter/locale/de.json**

Read `alerter/locale/de.json`, convert English-as-key entries to identifier keys. For example:
- `"track": "verfolgen"` → `"cmd.track": "verfolgen"`
- `"remove": "entfernen"` → `"arg.remove": "entfernen"`
- `"distance": "entfernung"` → `"arg.prefix.d": "entfernung"`

Repeat for each configured language (de, fr, it, ru, pl, se, nb-no).

- [ ] **Step 3: Verify translations load**

Run: `cd processor && go test ./internal/i18n/ -v -run TestLoad`

- [ ] **Step 4: Commit**

```bash
git add processor/internal/i18n/locale/
git commit -m "feat(bot): add command and argument translation keys"
```

---

## Task 2: Core Types — Command, Context, Reply, ParsedArgs

**Files:**
- Create: `processor/internal/bot/command.go`

This file defines all the core interfaces and types. No logic, just structures.

- [ ] **Step 1: Write command.go**

```go
package bot

import (
    "encoding/json"

    "github.com/jmoiron/sqlx"
    "github.com/pokemon/poracleng/processor/internal/config"
    "github.com/pokemon/poracleng/processor/internal/delivery"
    "github.com/pokemon/poracleng/processor/internal/gamedata"
    "github.com/pokemon/poracleng/processor/internal/geofence"
    "github.com/pokemon/poracleng/processor/internal/i18n"
    "github.com/pokemon/poracleng/processor/internal/rowtext"
    "github.com/pokemon/poracleng/processor/internal/state"
)

// Command is implemented by every bot command handler.
type Command interface {
    // Name returns the primary identifier key (e.g. "cmd.track").
    Name() string
    // Aliases returns additional identifier keys (e.g. "cmd.pokemon" → same as track).
    Aliases() []string
    // Run executes the command. Args are pre-parsed; commands never do regex parsing.
    Run(ctx *CommandContext, args []string) []Reply
}

// CommandContext carries user info, permissions, and injected dependencies.
type CommandContext struct {
    // User identity
    UserID   string
    UserName string
    Platform string // "discord" or "telegram"
    ChannelID string
    GuildID   string // Discord only, empty for Telegram
    IsDM      bool

    // Permissions (resolved before command runs)
    IsAdmin          bool
    IsCommunityAdmin bool
    Permissions      Permissions

    // User state (loaded from DB before command runs)
    Language   string
    ProfileNo  int
    HasLocation bool
    HasArea     bool

    // Target override (set by buildTarget for admin commands)
    TargetID   string // defaults to UserID if no override
    TargetName string
    TargetType string // "discord:user", "discord:channel", "webhook", "telegram:user", etc.

    // Injected dependencies
    DB           *sqlx.DB
    Config       *config.Config
    StateMgr     *state.Manager
    GameData     *gamedata.GameData
    Translations *i18n.Bundle
    Geofence     *geofence.SpatialIndex
    Fences       []geofence.Fence
    Dispatcher   *delivery.Dispatcher
    RowText      *rowtext.Generator
    Resolver     *PokemonResolver
    ArgMatcher   *ArgMatcher
}

// Permissions holds resolved permission state for the current command invocation.
type Permissions struct {
    ChannelTracking bool   // delegated admin: can manage tracking in this channel
    WebhookAdmin    string // delegated admin: can manage this webhook name (empty = no)
}

// Tr returns a translator for the command's target language.
func (ctx *CommandContext) Tr() *i18n.Translator {
    return ctx.Translations.For(ctx.Language)
}

// TriggerReload signals that tracking state should be reloaded from DB.
// This is called after any tracking mutation (insert/update/delete).
func (ctx *CommandContext) TriggerReload() {
    // Will be wired to ProcessorService.triggerReload()
    // For now, a no-op placeholder that gets connected during integration.
}

// Reply represents a single response message to send back to the user.
type Reply struct {
    Text       string          `json:"text,omitempty"`
    Embed      json.RawMessage `json:"embed,omitempty"`    // Discord embed JSON
    React      string          `json:"react,omitempty"`    // emoji reaction: "✅", "👌", "🙅"
    ImageURL   string          `json:"imageUrl,omitempty"` // image to embed/attach
    Attachment *Attachment     `json:"attachment,omitempty"`
    IsDM       bool            `json:"isDM,omitempty"`     // force DM even if command was in channel
}

// Attachment is a file to attach to the reply message.
type Attachment struct {
    Filename string `json:"filename"`
    Content  []byte `json:"content"`
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd processor && go build ./internal/bot/`

- [ ] **Step 3: Commit**

```bash
git add processor/internal/bot/command.go
git commit -m "feat(bot): core types — Command, CommandContext, Reply"
```

---

## Task 3: Parser — Tokenize, Command Lookup, Pipe Split

**Files:**
- Create: `processor/internal/bot/parser.go`
- Create: `processor/internal/bot/parser_test.go`

- [ ] **Step 1: Write parser tests**

```go
package bot

import (
    "testing"

    "github.com/pokemon/poracleng/processor/internal/i18n"
)

func newTestParser() *Parser {
    bundle := i18n.NewBundle()
    bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{
        "cmd.track": "track",
        "cmd.raid":  "raid",
        "cmd.egg":   "egg",
        "cmd.start": "start",
        "cmd.area":  "area",
    }))
    bundle.AddTranslator(i18n.NewTranslator("de", map[string]string{
        "cmd.track": "verfolgen",
        "cmd.raid":  "raid",
        "cmd.egg":   "ei",
        "cmd.start": "starten",
        "cmd.area":  "gebiet",
    }))
    return NewParser("!", bundle, []string{"en", "de"})
}

func TestParserBasic(t *testing.T) {
    p := newTestParser()
    cmds := p.Parse("!track pikachu iv100")
    if len(cmds) != 1 { t.Fatalf("expected 1 command, got %d", len(cmds)) }
    if cmds[0].CommandKey != "cmd.track" { t.Errorf("key = %q", cmds[0].CommandKey) }
    if len(cmds[0].Args) != 2 { t.Errorf("args = %v", cmds[0].Args) }
}

func TestParserGermanCommand(t *testing.T) {
    p := newTestParser()
    cmds := p.Parse("!verfolgen relaxo iv100")
    if len(cmds) != 1 { t.Fatalf("expected 1, got %d", len(cmds)) }
    if cmds[0].CommandKey != "cmd.track" { t.Errorf("key = %q", cmds[0].CommandKey) }
}

func TestParserPipeSplit(t *testing.T) {
    p := newTestParser()
    cmds := p.Parse("!track pikachu | bulbasaur")
    if len(cmds) != 2 { t.Fatalf("expected 2, got %d", len(cmds)) }
    if cmds[0].Args[0] != "pikachu" { t.Errorf("first = %v", cmds[0].Args) }
    if cmds[1].Args[0] != "bulbasaur" { t.Errorf("second = %v", cmds[1].Args) }
    // Both share same command key
    if cmds[1].CommandKey != "cmd.track" { t.Errorf("key = %q", cmds[1].CommandKey) }
}

func TestParserMultiLine(t *testing.T) {
    p := newTestParser()
    cmds := p.Parse("!track pikachu\n!raid level5")
    if len(cmds) != 2 { t.Fatalf("expected 2, got %d", len(cmds)) }
    if cmds[0].CommandKey != "cmd.track" { t.Errorf("first = %q", cmds[0].CommandKey) }
    if cmds[1].CommandKey != "cmd.raid" { t.Errorf("second = %q", cmds[1].CommandKey) }
}

func TestParserQuotedArgs(t *testing.T) {
    p := newTestParser()
    cmds := p.Parse(`!track "mr. mime" iv100`)
    if len(cmds) != 1 { t.Fatalf("expected 1, got %d", len(cmds)) }
    if cmds[0].Args[0] != "mr. mime" { t.Errorf("arg = %q", cmds[0].Args[0]) }
}

func TestParserUnderscoreToSpace(t *testing.T) {
    p := newTestParser()
    cmds := p.Parse("!track slot_changes")
    if len(cmds) != 1 { t.Fatalf("expected 1, got %d", len(cmds)) }
    if cmds[0].Args[0] != "slot changes" { t.Errorf("arg = %q", cmds[0].Args[0]) }
}

func TestParserUnknownCommand(t *testing.T) {
    p := newTestParser()
    cmds := p.Parse("!notacommand hello")
    if len(cmds) != 1 { t.Fatalf("expected 1, got %d", len(cmds)) }
    if cmds[0].CommandKey != "" { t.Errorf("expected empty key, got %q", cmds[0].CommandKey) }
}

func TestParserNoPrefix(t *testing.T) {
    p := newTestParser()
    cmds := p.Parse("track pikachu")
    if len(cmds) != 0 { t.Errorf("expected 0, got %d", len(cmds)) }
}

func TestParserCaseInsensitive(t *testing.T) {
    p := newTestParser()
    cmds := p.Parse("!TRACK Pikachu IV100")
    if len(cmds) != 1 { t.Fatalf("expected 1, got %d", len(cmds)) }
    if cmds[0].CommandKey != "cmd.track" { t.Errorf("key = %q", cmds[0].CommandKey) }
    if cmds[0].Args[0] != "pikachu" { t.Errorf("arg = %q", cmds[0].Args[0]) }
}
```

- [ ] **Step 2: Run tests, verify they fail**

Run: `cd processor && go test ./internal/bot/ -v -run TestParser`
Expected: compilation error (Parser not defined)

- [ ] **Step 3: Implement Parser**

```go
package bot

import (
    "regexp"
    "strings"

    "github.com/pokemon/poracleng/processor/internal/i18n"
)

// ParsedCommand represents one command invocation after parsing.
type ParsedCommand struct {
    CommandKey string   // identifier key (e.g. "cmd.track"), empty if unrecognised
    Args       []string // remaining arguments (lowercased, underscores→spaces)
}

// Parser handles text → structured commands.
type Parser struct {
    prefix     string
    commandMap map[string]string // lowercased translated name → identifier key
}

var tokenRe = regexp.MustCompile(`"([^"]*)"|\S+`)

// NewParser builds the multi-language command lookup table.
// For each cmd.* key in the bundle, all translations across the given languages
// are mapped to the identifier key. Longer names are checked first implicitly
// since the map lookup is exact-match.
func NewParser(prefix string, bundle *i18n.Bundle, languages []string) *Parser {
    cmdMap := make(map[string]string)
    for _, lang := range languages {
        tr := bundle.For(lang)
        if tr == nil { continue }
        for key, val := range tr.Messages() {
            if !strings.HasPrefix(key, "cmd.") { continue }
            lower := strings.ToLower(val)
            if lower == "" { continue }
            // First mapping wins (earlier languages have priority)
            if _, exists := cmdMap[lower]; !exists {
                cmdMap[lower] = key
            }
        }
    }
    return &Parser{prefix: strings.ToLower(prefix), commandMap: cmdMap}
}

// Parse splits raw message text into one or more ParsedCommands.
func (p *Parser) Parse(text string) []ParsedCommand {
    var results []ParsedCommand

    // Multi-line: split by newlines, each line is independent
    lines := strings.Split(text, "\n")
    for _, line := range lines {
        line = strings.TrimSpace(line)
        if line == "" { continue }

        // Check prefix
        lower := strings.ToLower(line)
        if !strings.HasPrefix(lower, p.prefix) { continue }
        line = line[len(p.prefix):]

        // Tokenize preserving quoted strings
        tokens := tokenize(line)
        if len(tokens) == 0 { continue }

        // Look up command name (first token)
        cmdKey := p.commandMap[tokens[0]]

        // Remaining args: lowercase, underscore→space
        args := make([]string, 0, len(tokens)-1)
        for _, tok := range tokens[1:] {
            args = append(args, strings.ReplaceAll(tok, "_", " "))
        }

        // Pipe splitting: split args by "|" into groups sharing the same command
        groups := splitByPipe(args)
        if len(groups) == 0 {
            results = append(results, ParsedCommand{CommandKey: cmdKey, Args: nil})
        } else {
            for _, group := range groups {
                results = append(results, ParsedCommand{CommandKey: cmdKey, Args: group})
            }
        }
    }

    return results
}

// tokenize splits text into tokens, preserving quoted strings.
// Quotes are stripped from the result. All tokens are lowercased.
func tokenize(text string) []string {
    matches := tokenRe.FindAllStringSubmatch(text, -1)
    tokens := make([]string, 0, len(matches))
    for _, m := range matches {
        tok := m[0]
        if m[1] != "" {
            tok = m[1] // captured quoted content (without quotes)
        }
        tokens = append(tokens, strings.ToLower(tok))
    }
    return tokens
}

// splitByPipe splits args by the "|" token into groups.
// Each group gets its own command invocation.
func splitByPipe(args []string) [][]string {
    if len(args) == 0 { return nil }
    var groups [][]string
    current := make([]string, 0)
    for _, a := range args {
        if a == "|" {
            if len(current) > 0 {
                groups = append(groups, current)
            }
            current = make([]string, 0)
        } else {
            current = append(current, a)
        }
    }
    if len(current) > 0 {
        groups = append(groups, current)
    }
    return groups
}
```

Note: This requires `Translator.Messages()` to be exposed. Add to i18n:

```go
// Messages returns the raw message map (read-only access for building lookup tables).
func (t *Translator) Messages() map[string]string {
    if t == nil { return nil }
    return t.messages
}
```

- [ ] **Step 4: Run tests, verify they pass**

Run: `cd processor && go test ./internal/bot/ -v -run TestParser`
Expected: all PASS

- [ ] **Step 5: Commit**

```bash
git add processor/internal/bot/parser.go processor/internal/bot/parser_test.go processor/internal/i18n/i18n.go
git commit -m "feat(bot): parser — tokenize, command lookup, pipe split, multi-line"
```

---

## Task 4: Argument Matchers — ParamDef Types and ArgMatcher

**Files:**
- Create: `processor/internal/bot/params.go`
- Create: `processor/internal/bot/argmatch.go`
- Create: `processor/internal/bot/argmatch_test.go`

This is the core of the typed parameter system. Each matcher type handles one class of argument.

- [ ] **Step 1: Define ParamDef types in params.go**

```go
package bot

// ParamDef defines a parameter that a command accepts.
// The ArgMatcher tries each ParamDef against unmatched tokens.
type ParamDef struct {
    Type     ParamType
    Key      string // identifier key for translated prefix/keyword (e.g. "arg.prefix.iv", "arg.remove")
    ResultID string // key in ParsedArgs to store the result (e.g. "min_iv", "remove")
}

type ParamType int

const (
    ParamPrefixRange     ParamType = iota // iv100, iv50-100, cp2500-3000, level20-30, atk15
    ParamPrefixSingle    ParamType = iota // d500, t60, gen3, cap50, miniv90, maxcp3000
    ParamPrefixString    ParamType = iota // form:alola, template:2, move:hydropump, name:foo
    ParamKeyword         ParamType = iota // remove, everything, clean, ex, shiny
    ParamTeam            ParamType = iota // valor/red, mystic/blue, instinct/yellow, harmony/gray
    ParamGender          ParamType = iota // male, female, genderless
    ParamPokemonName     ParamType = iota // pikachu, relaxo, 25, mr. mime
    ParamTypeName        ParamType = iota // grass, fire, dragon
    ParamLureType        ParamType = iota // glacial, mossy, magnetic, rainy, sparkly, normal
    ParamGruntType       ParamType = iota // grunt names from gamedata
    ParamRaidLevelName   ParamType = iota // legendary, mega, shadow, ultra beast, elite, primal
    ParamPVPLeague       ParamType = iota // great5, ultra10-50, greathigh3, greatcp1400
    ParamLatLon          ParamType = iota // 51.28,1.08
    ParamSubcommand      ParamType = iota // list, add, remove, show, overview (first arg only)
)
```

- [ ] **Step 2: Write ArgMatcher and tests**

The ArgMatcher walks through tokens trying each declared ParamDef in priority order. Uses user's language + English.

Key behaviors to test:
- `d500` matches PrefixSingle for distance
- `iv100` matches PrefixRange with min=100, max=100
- `iv50-100` matches PrefixRange with min=50, max=100
- `form:alola` matches PrefixString
- `remove` matches Keyword
- `valor` and `red` both match Team → team ID 2
- `male` matches Gender → 1
- `pikachu` matches PokemonName → ID 25
- `grass` matches TypeName
- `legendary` matches RaidLevelName → level 5
- `great5` matches PVPLeague → league=1500, worst=5
- `great1-5` matches PVPLeague → league=1500, best=1, worst=5
- `greathigh3` matches PVPLeague → league=1500, best=3
- `51.28,1.08` matches LatLon
- Unmatched tokens are collected in `Unrecognized` list
- German prefix `entfernung500` matches distance (when user lang=de)

- [ ] **Step 3: Implement ArgMatcher**

```go
// ParsedArgs holds structured results from argument matching.
type ParsedArgs struct {
    Ranges       map[string]Range       // "iv" → {Min:50, Max:100}
    Singles      map[string]int         // "distance" → 500
    Strings      map[string]string      // "form" → "alola", "template" → "2"
    Keywords     map[string]bool        // "remove" → true, "clean" → true
    Team         int                    // 0-4 (4=unset)
    Gender       int                    // 0=unset, 1=male, 2=female, 3=genderless
    Pokemon      []ResolvedPokemon      // matched pokemon
    Types        []int                  // matched type IDs
    LureType     int                    // lure ID (0=any, 501-506)
    GruntType    string                 // grunt type identifier
    RaidLevels   []int                  // raid level numbers from name matching
    PVP          map[string]PVPFilter   // "great" → {Best:1, Worst:5, MinCP:0}
    LatLon       *LatLon                // parsed coordinates
    Subcommand   string                 // first arg if it matches a subcommand
    Unrecognized []string               // tokens that matched nothing
}

type Range struct { Min, Max int }
type PVPFilter struct { Best, Worst, MinCP int }
type LatLon struct { Lat, Lon float64 }
```

The ArgMatcher tries matchers in priority order:
1. PrefixRange, PrefixSingle, PrefixString (structural — prefix makes them unambiguous)
2. LatLon (structural — two decimals with comma)
3. PVPLeague (prefix: great/ultra/little + value)
4. RaidLevelName (exact match against translated level names)
5. Keyword (exact match against translated keywords)
6. Team, Gender, LureType, GruntType (exact match against known sets)
7. TypeName (exact match against translated type names)
8. PokemonName (last resort — fuzzy match)

For each token, the matcher tries user's language first, then English fallback. First match wins and the token is consumed.

- [ ] **Step 4: Run tests, verify they pass**

Run: `cd processor && go test ./internal/bot/ -v -run TestArgMatch`

- [ ] **Step 5: Commit**

```bash
git add processor/internal/bot/params.go processor/internal/bot/argmatch.go processor/internal/bot/argmatch_test.go
git commit -m "feat(bot): argument matchers — typed parameter parsing with language fallback"
```

---

## Task 5: Pokemon Resolver

**Files:**
- Create: `processor/internal/bot/pokemon_resolver.go`
- Create: `processor/internal/bot/pokemon_resolver_test.go`

- [ ] **Step 1: Write tests**

Test cases:
- `"pikachu"` (en) → `[{PokemonID: 25, Form: 0}]`
- `"relaxo"` (de) → `[{PokemonID: 143, Form: 0}]` (Snorlax)
- `"25"` → `[{PokemonID: 25, Form: 0}]` (numeric ID)
- `"mr. mime"` → `[{PokemonID: 122, Form: 0}]` (space in name, from alias)
- `"eevee+"` → `[{25: eevee}, {134: vaporeon}, {135: jolteon}, ...]` (evolution chain)
- `"notapokemon"` → `[]` (no match)

- [ ] **Step 2: Implement PokemonResolver**

```go
type PokemonResolver struct {
    nameToIDs map[string]map[string][]int // lang → lowercase name → pokemon IDs
    aliases   map[string]int              // alias → pokemon ID
    gameData  *gamedata.GameData
}

func NewPokemonResolver(gd *gamedata.GameData, bundle *i18n.Bundle, languages []string, aliases map[string]int) *PokemonResolver

// Resolve returns matching pokemon for a user-typed name.
// Checks: numeric ID, alias, exact name match (user lang then English).
// The + suffix triggers evolution chain inclusion.
func (r *PokemonResolver) Resolve(name string, lang string) []ResolvedPokemon

// ResolveWithEvolutions returns the pokemon + all evolutions recursively.
func (r *PokemonResolver) ResolveWithEvolutions(pokemonID int) []int
```

The resolver builds name→ID maps at construction time from `poke_{id}` translation keys. This is a one-time cost at startup.

- [ ] **Step 3: Run tests, verify pass**
- [ ] **Step 4: Commit**

---

## Task 6: Permissions

**Files:**
- Create: `processor/internal/bot/permissions.go`
- Create: `processor/internal/bot/permissions_test.go`

Three layers of security:

```go
// IsAdmin checks if user is in the admin list.
func IsAdmin(cfg *config.Config, platform, userID string) bool

// CommandAllowed checks command_security for the command identifier key.
// Returns true if:
//   - No command_security entry exists for this command (unrestricted)
//   - User ID is in the allowed list
//   - Any of the user's roles is in the allowed list
// Discord only — Telegram always returns true (no role system).
func CommandAllowed(cfg *config.Config, cmdKey string, userID string, userRoles []string) bool

// CalculateChannelPermissions checks delegated_administration.channel_tracking.
// Returns true if the user (by ID or role) is allowed to manage tracking
// in the given channel/guild/category.
func CalculateChannelPermissions(cfg *config.Config, userID string, userRoles []string, channelID, guildID, categoryID string) bool

// CanAdminWebhook checks delegated_administration.webhook_tracking.
func CanAdminWebhook(cfg *config.Config, userID string, webhookName string) bool

// BlockedAlerts derives blocked_alerts from command_security.
// For reconciliation: if a user lacks the role for "raid", "monster", etc.,
// those alert types are added to their blocked list.
func BlockedAlerts(cfg *config.Config, userRoles []string) []string
```

- [ ] **Step 1: Write tests for each permission layer**
- [ ] **Step 2: Implement**
- [ ] **Step 3: Commit**

---

## Task 7: Target Resolution (buildTarget)

**Files:**
- Create: `processor/internal/bot/target.go`
- Create: `processor/internal/bot/target_test.go`

Resolves who the command is for. Handles:
- Default: command runs for the message author (DM → discord:user, channel → discord:channel)
- `name<webhookname>`: admin targets a registered webhook
- `user<userid>`: admin targets a specific user
- Channel commands: command targets the current channel

```go
type Target struct {
    ID        string
    Name      string
    Type      string // "discord:user", "discord:channel", "webhook", "telegram:user", etc.
    Language  string
    ProfileNo int
    HasLocation bool
    HasArea     bool
    IsAdmin     bool
}

// BuildTarget resolves the command target from args.
// Strips consumed args (name<X>, user<X>) from the args slice.
// Returns the target and remaining args.
func BuildTarget(ctx *CommandContext, args []string) (*Target, []string, error)
```

- [ ] **Step 1: Write tests**
- [ ] **Step 2: Implement**
- [ ] **Step 3: Commit**

---

## Task 8: Command Registry

**Files:**
- Create: `processor/internal/bot/registry.go`
- Create: `processor/internal/bot/registry_test.go`

```go
type Registry struct {
    commands map[string]Command // identifier key → Command
}

func NewRegistry() *Registry

// Register adds a command. Registers both Name() and Aliases().
func (r *Registry) Register(cmd Command)

// Lookup returns the command for an identifier key, or nil.
func (r *Registry) Lookup(key string) Command
```

- [ ] **Step 1: Write tests**
- [ ] **Step 2: Implement**
- [ ] **Step 3: Commit**

---

## Task 9: First Command — !start

**Files:**
- Create: `processor/internal/bot/commands/start.go`
- Create: `processor/internal/bot/commands/start_test.go`

Proves the full pipeline works: parse → match → context → command → reply.

```go
package commands

import "github.com/pokemon/poracleng/processor/internal/bot"

type StartCommand struct{}

func (c *StartCommand) Name() string      { return "cmd.start" }
func (c *StartCommand) Aliases() []string { return nil }

func (c *StartCommand) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
    tr := ctx.Tr()
    _, err := ctx.DB.Exec(
        "UPDATE humans SET enabled = 1, fails = 0 WHERE id = ? AND type = ?",
        ctx.TargetID, ctx.TargetType,
    )
    if err != nil {
        return []bot.Reply{{React: "🙅", Text: tr.T("cmd.start.error")}}
    }
    return []bot.Reply{{React: "✅", Text: tr.T("cmd.start.success")}}
}
```

- [ ] **Step 1: Write test (mock DB)**
- [ ] **Step 2: Implement**
- [ ] **Step 3: Run test**
- [ ] **Step 4: Commit**

---

## Task 10: Medium Command — !egg

**Files:**
- Create: `processor/internal/bot/commands/egg.go`
- Create: `processor/internal/bot/commands/egg_test.go`

Proves the framework handles: level parsing (single, array, "everything"), team names (translated + color aliases), raid level names (legendary, mega, shadow), RSVP modes, distance, template, clean, EX flag, and the diff/insert/update logic via existing `api/tracking.go`.

Key behaviours:
- `!egg level5` → track level 5 eggs
- `!egg legendary` → track level 5 eggs (mapped from raid level name)
- `!egg mega` → track level 6 eggs
- `!egg shadow` → track levels 11-15 (all shadow levels)
- `!egg everything` → all levels from `gameData.Util.RaidLevels`
- `!egg level5 valor d500` → team=2, distance=500
- `!egg remove level5` → delete level 5 tracking
- `!egg level5 rsvp only` → rsvp_changes=2

Raid level name resolution:
```
"legendary" → [5]
"mega" → [6]
"mega legendary" → [7]
"ultra beast" → [8]
"elite" → [9]
"primal" → [10]
"shadow" → [11, 12, 13, 14, 15]  (all shadow levels)
"shadow legendary" → [15]
```

These names come from `gameData.Util.RaidLevels` values and `raid.level.*` translation keys.

- [ ] **Step 1: Write tests for level parsing, team matching, RSVP modes**
- [ ] **Step 2: Implement egg command using framework**
- [ ] **Step 3: Run tests**
- [ ] **Step 4: Commit**

---

## Task 11: Complex Command — !track

**Files:**
- Create: `processor/internal/bot/commands/track.go`
- Create: `processor/internal/bot/commands/track_test.go`

Proves the framework handles the most complex case: pokemon name resolution, PVP parameters, everything flag permissions, form/type/gen filtering, individually flag, and full diff logic.

Key test cases:
- `!track pikachu iv100` → pokemon_id=25, min_iv=100
- `!track relaxo iv90-100 d500` (German user) → pokemon_id=143, min_iv=90, max_iv=100, distance=500
- `!track everything iv100` → pokemon_id=0 (catch-all)
- `!track everything iv100 individually` → expanded to all base forms
- `!track grass iv100` → all grass-type pokemon
- `!track gen1 iv100` → all gen 1 pokemon
- `!track pikachu great5` → pvp_ranking_league=1500, pvp_ranking_worst=5
- `!track pikachu great1-10 cap50` → best=1, worst=10, cap=50
- `!track pikachu form:alola` → form matched against gamedata
- `!track remove pikachu` → delete pikachu tracking
- `!track eevee+` → eevee + all evolutions

- [ ] **Step 1: Write extensive table-driven tests**
- [ ] **Step 2: Implement track command**
- [ ] **Step 3: Run tests**
- [ ] **Step 4: Commit**

---

## Task 12: POST /api/command Endpoint

**Files:**
- Create: `processor/internal/api/command.go`
- Modify: `processor/cmd/processor/main.go` (register route)

Wires the parser + registry + context building into an HTTP endpoint for testing.

```go
// POST /api/command
// Request:
// {
//   "text": "!track pikachu iv100",
//   "user_id": "123456789",
//   "user_name": "James",
//   "platform": "discord",
//   "channel_id": "456",
//   "guild_id": "789",
//   "is_dm": true
// }
//
// Response:
// {
//   "status": "ok",
//   "replies": [{"text": "...", "react": "✅"}]
// }
```

The handler:
1. Looks up user in humans table → get language, profile, location, area, admin status
2. Builds CommandContext with all dependencies
3. Calls parser.Parse(text) → []ParsedCommand
4. For each command: registry.Lookup(key) → command.Run(ctx, args)
5. Collects all replies and returns them

- [ ] **Step 1: Implement endpoint**
- [ ] **Step 2: Wire into main.go route registration**
- [ ] **Step 3: Manual test with curl**
- [ ] **Step 4: Commit**

---

## Design Notes

### Security Validation Flow

Every command invocation goes through this security check before `Run()` is called:

1. **Admin check**: Is user in `[discord] admins` or `[telegram] admins`?
2. **Command security**: Is command restricted by `[discord] command_security`? If so, does user have a required role?
3. **Registration check**: Is user registered in humans table? (most commands require registration)
4. **Target resolution**: If admin is targeting another user/channel, validate the target exists
5. **Delegated admin**: If targeting a channel/webhook, does user have delegated permission?

If any check fails, the command returns a 🙅 reaction and error message without executing.

### Raid Level Name Matching

The `ParamRaidLevelName` matcher builds a map from translated level names → level numbers at startup:

```go
// From gameData.Util.RaidLevels:
// {5: "Legendary", 6: "Mega", 7: "Mega Legendary", 11: "Level 1 Shadow", ...}
//
// Plus raid.level.* translation keys for each language.
//
// Special: "shadow" (without a number) matches ALL shadow levels [11-15].
```

This is used by both `!egg` and `!raid` commands.

### Unrecognized Argument Reporting

After all matchers have run, any tokens left in `ParsedArgs.Unrecognized` trigger a warning reply (🙅 reaction + message listing the unrecognized args). This matches the alerter's `reportUnrecognizedArgs()` behavior. Commands opt into this by checking `len(parsed.Unrecognized) > 0` after matching.
