---
name: project-slash-commands-plan
description: "Draft implementation plan for adding optional Discord slash commands to PoracleNG, with RecentActivity tracker, plus debate-worthy decisions on test approach"
metadata: 
  node_type: memory
  type: project
  originSessionId: 4f0cd2d1-e376-4269-8cd9-f7f43f29577e
---

# PoracleNG Discord Slash Commands — Implementation Plan (DRAFT)

> **Status:** Draft for discussion. Not committed upstream. Plan deliberately calls out open architectural decisions for debate, especially around test approach.

**Goal:** Add Discord slash commands as an **optional, additive surface** over PoracleNG's existing text-command framework. Existing `!` text commands continue to work unchanged.

**Architecture in one paragraph:** Slash handlers receive `discordgo.InteractionCreate` events, map Discord options → equivalent text-token slices, and call the existing `bot.Command.Run(ctx, args)` method on the matching registered command. The slash facade is a translation layer over the same business logic the text bot uses, so we get permission checks, target resolution, area-security, command-security, validation, DB writes, and reply construction for free. No command-handler changes. RecentActivity is a 6h TTL multi-map populated from webhook ingestion and consumed only by autocomplete handlers.

**Tech stack:** Go 1.21+, `bwmarrin/discordgo` v0.27+ (already in deps), existing `bot.Registry`, `bot.CommandContext`, `i18n.Bundle`.

---

## Architectural Approach

### How slash commands reuse the existing command framework

The existing flow for `!track pikachu iv90 d10` is:

```
text msg → Parser.Tokenize → reverse-translate "track" → cmd.track
        → bot.Registry.Get("cmd.track") → cmd.Run(ctx, ["pikachu", "iv90", "d10"])
        → ctx.ArgMatcher.Match(args, paramDefs) → ParsedArgs
        → command-specific business logic → []Reply
```

`Command.Run` takes `(ctx *CommandContext, args []string)`. The slash facade enters mid-pipeline:

```
slash interaction → SlashMapper.Map(options) → ["pikachu", "iv90", "d10"]
        → bot.Registry.Get("cmd.track") → cmd.Run(ctx, ["pikachu", "iv90", "d10"])
        → existing business logic → []Reply → render to InteractionResponseData
```

**Why synthesize tokens instead of synthesizing a `ParsedArgs` directly:**
1. No `Command` interface changes — `Run(ctx, []string)` stays as the single entry point. Adding a parallel `RunParsed(ctx, *ParsedArgs)` would require changes to all ~30 command handlers.
2. The token form goes through the same `ctx.ArgMatcher.Match` that text commands use, so any validation, normalisation, or fallback logic in `argmatch.go` applies identically. We get **surface parity by construction**.
3. Tokens are diff-able with text-command equivalents — easy to write tests that assert "slash invocation A produces the same tokens as text command B."
4. The synthesised tokens never carry case-sensitive content, so no information is lost vs the text path.

**Why not synthesize a text command line (string-style):**
1. Forces a second pass through `Parser.Tokenize`, which does reverse-translation against the i18n bundle. Pointless and slow.
2. Loses type information from Discord options (we'd convert int 90 → string "90" → int 90 again).
3. Brittle: a Discord option containing a space or special char would need quoting, which couples slash to text-grammar implementation details.

### CommandContext construction for slash

`CommandContext` is built by the text bot's `discordbot/bot.go` from the message event. For slash we do the same from the interaction event:

| Field | Source for text | Source for slash |
|---|---|---|
| `UserID`, `UserName` | `m.Author.ID`, `m.Author.Username` | `i.Member.User` or `i.User` (DM) |
| `ChannelID`, `GuildID`, `IsDM` | `m.ChannelID`, `m.GuildID`, `m.GuildID==""` | `i.ChannelID`, `i.GuildID`, `i.GuildID==""` |
| `Platform` | `"discord"` | `"discord"` |
| `IsAdmin`, `Permissions` | resolved from `cfg.Discord.Admins` + delegated admin lookups | identical resolution |
| `Language` | from `humans.language` (or `[general] locale`) | identical, plus optional Discord-locale fallback |
| `TargetID`, `TargetName`, `TargetType` | `BuildTarget(args, sender, channel, …)` | `BuildTarget(args, sender, channel, …)` — args include parsed `user:`/`name:` overrides |
| `ProfileNo`, `HasLocation`, `HasArea` | loaded from DB | loaded from DB |

`BuildTarget` reads `user:` and `name:` from the args slice (admins only). Since slash handlers synthesise these tokens just like text, `BuildTarget` runs unchanged.

### Reply rendering

`Command.Run` returns `[]Reply`. `Reply` is:

```go
type Reply struct {
    Text       string
    Embed      json.RawMessage
    React      string
    ImageURL   string
    Attachment *Attachment
    IsDM       bool
}
```

For text commands, the existing code in `discordbot/bot.go` sends each Reply as a separate message via `session.ChannelMessageSendComplex`. For slash:

- **First** reply → `InteractionRespondData` (or `FollowupCreate` if we deferred). React is dropped (no reactions on interaction responses). Long Text uses Discord's 2000-char limit which the existing `SplitMessage` already handles.
- **Subsequent** replies → `FollowupCreate` per reply.
- **IsDM=true** Replies (sent to user's DM channel separately from interaction response): create or reuse the DM channel, send via `ChannelMessageSendComplex`.

This is mechanical. We can reuse `delivery.discord.go`'s DM-channel-cache and image-multipart code; we already have the building blocks.

### Optional + opt-in command list

Two config keys, both default off:

```toml
[discord.slash_commands]
enabled = false                  # master switch; nothing registers if false
register_globally = true         # global vs guild-scoped registration
guilds = []                      # if non-empty, register only to these guild IDs (fast iteration, no 1h cache)

# Optional subset restriction. Empty (or absent) = all slash commands enabled.
# Use this only when an operator wants to limit the surface to a subset.
# The master `enabled = false` flag is the right way to turn slash off entirely.
enable = []
```

**Layered controls**:

1. `[discord.slash_commands] enabled = false` — master switch; no slash commands registered.
2. `[discord.slash_commands] enable = [...]` — optional subset restriction. Empty/absent = all commands.
3. `[general] disabled_commands` (existing) — disables a command on *both* surfaces. Slash dispatch consults `bot.IsCommandDisabled` before invoking the registered handler, so a command disabled here returns an ephemeral "this command is disabled" error regardless of slash registration.

This means a command can be (a) entirely off (master switch), (b) text-only via `enable` exclusion (when operator restricts the slash subset), or (c) text-only + visible-via-slash-but-rejected (when `disabled_commands` excludes it but `enable` includes it — edge case; gives a clear error).

### Command names come from i18n

Slash command names are driven by the i18n bundle under a **separate `slash.*` namespace** (distinct from `cmd.*` which the text bot uses for command parsing). The slash UI has three layers of localizable text — command, option, and choice — each with its own key prefix:

| Key | Used for | Source |
|---|---|---|
| `cmd.track` | Text command parsing (`!track`, `!verfolge`) | Existing |
| `slash.cmd.track` | Slash command name (`/track`, `/verfolge`) | New |
| `slash.desc.track` | Slash command description | New |
| `slash.opt.track.pokemon` | Option label for `/track pokemon` | New |
| `slash.opt.track.pokemon.desc` | Option description | New |
| `slash.choice.raid.team.1` | Choice label for `/raid team: Mystic` | New |

Sub-command-grouped commands (`/area`, `/profile`, `/untrack`, `/summary`) use the full dotted path so each leaf option stays addressable: `slash.opt.area.add.area` / `slash.opt.area.add.area.desc`, `slash.opt.summary.quest.settime.times`, etc.

**English seed** (`processor/internal/i18n/locale/en.json`):
```json
{
  "slash.cmd.track":               "track",
  "slash.desc.track":              "Track a Pokemon",
  "slash.opt.track.pokemon":       "pokemon",
  "slash.opt.track.pokemon.desc":  "Pokemon to track (or 'everything')",
  "slash.choice.track.size.xxl":   "XXL"
}
```

**Localizations** (`de.json`, `fr.json`, etc.) populate `NameLocalizations` / `DescriptionLocalizations` automatically at all three layers. A German user sees `/track` with an option labelled `größe` and a choice labelled `XXL` if those keys exist in `de.json`. Choice **values** stay canonical English regardless of locale — the slash mapper and text bot both resolve by Value, not display Name.

**Fallback chain**: each builder ships a hardcoded canonical English fallback (matched against the same key the operator can override). When a key is missing or — for slash names only — fails Discord's `^[\p{L}\p{N}_-]{1,32}$` regex, the canonical English is used and the localization map for that field is dropped. Translators that ship a valid German entry still see it surface, even if a different language's entry was invalid.

**Operator overrides**: the existing `config/custom.{lang}.json` mechanism (already used for text commands) extends naturally — drop `"slash.cmd.track": "alerts"` in `config/custom.en.json` and the command becomes `/alerts` for English users at next startup. No new config surface.

The `[discord.slash_commands] enable` allow-list always uses the **canonical English short name** (`"track"`, not `"verfolge"` or `"alerts"`) so operator-renamed installations have stable enable-lists. The enable check happens before name resolution; renaming is purely cosmetic.

Command names and option names are localizable via the `slash.cmd.*` / `slash.opt.*` i18n keys (see below). Operators rename per-locale via existing `config/custom.{lang}.json` overrides.

---

## File Structure

### New files

```
processor/internal/discordbot/slash/
  definitions.go            # Slash command definitions (one builder per command)
  registration.go           # Sync slash defs with Discord at startup
  dispatcher.go             # InteractionCreate routing (ApplicationCommand + Autocomplete)
  context.go                # CommandContext construction from interaction
  reply.go                  # []Reply → InteractionResponse / Followup
  confirm.go                # Verify/Cancel button helpers (used by mutating commands)
  mappers/
    track.go                # /track options → text tokens
    raid.go
    egg.go
    quest.go
    invasion.go
    lure.go
    nest.go
    maxbattle.go
    location.go
    area.go
    profile.go
    language.go
    tracked.go
    untrack.go
    poracle.go
    help.go
    info.go
    common.go               # shared helpers (range mapping, distance, template, clean)
  autocomplete/
    dispatcher.go           # central autocomplete routing by (commandName, optionName)
    pokemon.go              # pokemon name autocomplete
    template.go             # template name autocomplete
    profile.go              # profile name autocomplete
    area.go                 # area name autocomplete
    raid_boss.go            # raid boss autocomplete (uses RecentActivity)
    quest_reward.go         # quest reward autocomplete (uses RecentActivity)
    common.go               # shared 25-item limit, prefix-match scoring

processor/internal/tracker/recent_activity.go
                            # 6h TTL multi-map of recently-seen entities for autocomplete
processor/internal/tracker/recent_activity_test.go

processor/internal/discordbot/slash/slash_test.go         # registration + dispatch
processor/internal/discordbot/slash/mappers/track_test.go # one mapper-test file per mapper
…etc
```

### Modified files

```
processor/internal/config/config.go
  + DiscordSlashCommands struct, parsed from [discord.slash_commands]

processor/internal/discordbot/bot.go
  + If cfg.Discord.SlashCommands.Enabled, instantiate slash.Dispatcher
    and route onInteractionCreate to slash for ApplicationCommand /
    ApplicationCommandAutocomplete types (existing MessageComponent
    routing for autocreate stays in place).

processor/internal/discordbot/interaction.go
  + In onInteractionCreate, branch by ic.Type:
    - InteractionApplicationCommand → slash.Dispatcher.HandleCommand
    - InteractionApplicationCommandAutocomplete → slash.Dispatcher.HandleAutocomplete
    - InteractionMessageComponent → existing buttonHandler (unchanged)

processor/cmd/processor/pokemon.go,
processor/cmd/processor/raid.go,
processor/cmd/processor/quest.go,
processor/cmd/processor/maxbattle.go
  + After successful match, call tracker.RecentActivity.Record* with
    the relevant pokemon ID / item ID / level.

processor/cmd/processor/main.go
  + Construct *tracker.RecentActivity, hand to ProcessorService, hand
    to slash.Dispatcher via BotDeps.
```

---

## Worked Example 1 — `/poracle` (simplest)

Demonstrates the end-to-end flow with one option and no autocomplete.

### Definition (`slash/definitions.go`)

```go
func poracleDefinition(t *i18n.Bundle, name string) *discordgo.ApplicationCommand {
    return &discordgo.ApplicationCommand{
        Name:                     name,                                  // e.g. "poracle"
        NameLocalizations:        localizationsForCommand(t, "cmd.poracle"),
        Description:              t.For("en").T("slash.poracle.desc"),
        DescriptionLocalizations: localizationsForDesc(t, "slash.poracle.desc"),
        Options:                  nil, // !poracle takes no args
    }
}
```

### Mapper (`slash/mappers/poracle.go`)

```go
// Map a /poracle interaction to text tokens.
// /poracle has no options, so we return an empty token slice.
func PoracleMapper(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
    return nil, nil
}
```

### Dispatch flow

1. User types `/poracle` in a registration channel.
2. `slash/dispatcher.go` `HandleCommand` receives the `InteractionCreate`.
3. Look up the registered command by `i.ApplicationCommandData().Name` — find `cmd.poracle` Command.
4. Build `CommandContext` from the interaction (`context.go`).
5. Call `mappers.PoracleMapper(opts)` → empty token slice.
6. Defer the interaction response (`InteractionResponseDeferredChannelMessageWithSource`).
7. Call `cmd.Run(ctx, tokens)` → returns `[]Reply`.
8. `reply.go` sends the first Reply via `InteractionResponseEdit`, subsequent replies via `FollowupCreate`.

---

## Worked Example 2 — `/track` (complex)

Demonstrates option mapping, autocomplete, range collapsing, multi-league PVP, and confirmation flow.

### Definition

The full text-command surface for `!track` is:
- Pokemon name(s) (required)
- IV/CP/level/individual IV ranges (`iv90`, `cp2500-3000`, `level20-30`, `atk15-15`, `def0-12`, `sta`, `iv50-100`)
- Distance (`d500`)
- Clean (`clean`)
- Gender (`male`/`female`)
- Form filter (`form:alola`)
- PVP league filters (`great5`, `ultra10-50`, `littlecp450high3`)
- Template (`template:2`)
- Size (`xs`, `xxl`)
- Generation (`gen3`)
- Type (`grass`, `fire`)
- Move (`move:hydropump`)
- Min time (`t60`)
- Weight (`weight5-15`)

Discord caps top-level options at 25 per command. We pick the most-used 18-20 and rely on filters being composable later if needed. Less-common options (move, weight, individual IV stats beyond 100 IV) can move to a sub-option string or get a follow-up modal in a later iteration.

```go
func trackDefinition(t *i18n.Bundle, name string, cfg *config.Config) *discordgo.ApplicationCommand {
    return &discordgo.ApplicationCommand{
        Name:                     name,
        NameLocalizations:        localizationsForCommand(t, "cmd.track"),
        Description:              t.For("en").T("slash.track.desc"),
        DescriptionLocalizations: localizationsForDesc(t, "slash.track.desc"),
        Options: []*discordgo.ApplicationCommandOption{
            {
                Type:         discordgo.ApplicationCommandOptionString,
                Name:         "pokemon",
                Description:  t.For("en").T("slash.track.opt.pokemon"),
                Required:     true,
                Autocomplete: true,
            },
            optionInt("min_iv", "slash.track.opt.miniv", t, 0, 100),
            optionInt("max_iv", "slash.track.opt.maxiv", t, 0, 100),
            optionInt("min_cp", "slash.track.opt.mincp", t, 10, 9999),
            optionInt("max_cp", "slash.track.opt.maxcp", t, 10, 9999),
            optionInt("min_level", "slash.track.opt.minlevel", t, 1, 55),
            optionInt("max_level", "slash.track.opt.maxlevel", t, 1, 55),
            optionInt("min_atk", "slash.track.opt.minatk", t, 0, 15),
            optionInt("max_atk", "slash.track.opt.maxatk", t, 0, 15),
            // …same for def/sta…
            optionInt("distance", "slash.track.opt.distance", t, 0, cfg.Tracking.MaxDistance),
            optionChoice("gender", "slash.track.opt.gender", t, []choice{
                {"all", 0}, {"male", 1}, {"female", 2}, {"genderless", 3},
            }),
            optionChoice("pvp_league", "slash.track.opt.pvp_league", t, []choice{
                {"great", 1}, {"ultra", 2}, {"little", 3},
            }),
            optionInt("pvp_rank", "slash.track.opt.pvp_rank", t, 1, cfg.Tracking.PVPFilterMaxRank),
            {Type: discordgo.ApplicationCommandOptionBool,   Name: "clean", Description: t.For("en").T("slash.track.opt.clean")},
            {Type: discordgo.ApplicationCommandOptionString, Name: "template", Description: t.For("en").T("slash.track.opt.template"), Autocomplete: true},
            {Type: discordgo.ApplicationCommandOptionString, Name: "form",     Description: t.For("en").T("slash.track.opt.form")},
        },
    }
}
```

### Mapper (`slash/mappers/track.go`)

```go
func TrackMapper(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
    o := flattenOptions(opts) // map[string]*ApplicationCommandInteractionDataOption

    tokens := []string{}

    // Required: pokemon name(s) — accept comma-separated for parity with text
    pokemon := o["pokemon"].StringValue()
    for _, p := range splitCSV(pokemon) {
        tokens = append(tokens, strings.ToLower(p))
    }

    // IV range: collapse min/max into iv<min>-<max>, or just iv<min>
    if r := rangeToken("iv", o["min_iv"], o["max_iv"]); r != "" {
        tokens = append(tokens, r)
    }
    if r := rangeToken("cp", o["min_cp"], o["max_cp"]); r != "" {
        tokens = append(tokens, r)
    }
    if r := rangeToken("level", o["min_level"], o["max_level"]); r != "" {
        tokens = append(tokens, r)
    }
    if r := rangeToken("atk", o["min_atk"], o["max_atk"]); r != "" {
        tokens = append(tokens, r)
    }
    // …def, sta…

    if v, ok := o["distance"]; ok {
        tokens = append(tokens, fmt.Sprintf("d%d", v.IntValue()))
    }
    if v, ok := o["gender"]; ok {
        switch v.IntValue() {
        case 1: tokens = append(tokens, "male")
        case 2: tokens = append(tokens, "female")
        case 3: tokens = append(tokens, "genderless")
        }
    }
    if league, ok := o["pvp_league"]; ok {
        rank := 1
        if r, ok := o["pvp_rank"]; ok { rank = int(r.IntValue()) }
        switch league.IntValue() {
        case 1: tokens = append(tokens, fmt.Sprintf("great%d", rank))
        case 2: tokens = append(tokens, fmt.Sprintf("ultra%d", rank))
        case 3: tokens = append(tokens, fmt.Sprintf("little%d", rank))
        }
    }
    if v, ok := o["clean"]; ok && v.BoolValue() {
        tokens = append(tokens, "clean")
    }
    if v, ok := o["template"]; ok && v.StringValue() != "" {
        tokens = append(tokens, "template:"+v.StringValue())
    }
    if v, ok := o["form"]; ok && v.StringValue() != "" {
        tokens = append(tokens, "form:"+v.StringValue())
    }

    return tokens, nil
}

// rangeToken collapses a min/max into one text token.
// rangeToken("iv", 90, _) → "iv90"
// rangeToken("iv", 50, 100) → "iv50-100"
// rangeToken("iv", _, 100) → "miniv0-100" (max-only handled in caller if needed)
// rangeToken("iv", _, _) → "" (no token)
func rangeToken(prefix string, min, max *opt) string {
    if min == nil && max == nil { return "" }
    if min == nil { return fmt.Sprintf("max%s%d", prefix, max.IntValue()) }
    if max == nil { return fmt.Sprintf("%s%d", prefix, min.IntValue()) }
    return fmt.Sprintf("%s%d-%d", prefix, min.IntValue(), max.IntValue())
}
```

### Autocomplete (`slash/autocomplete/pokemon.go`)

```go
// PokemonAutocomplete handles autocomplete for the "pokemon" option.
// Sources: ResolvedPokemon list filtered by prefix-match on (English name + user-language name).
func PokemonAutocomplete(ctx context.Context, deps *bot.BotDeps, ic *discordgo.InteractionCreate, focused string, userLang string) []*discordgo.ApplicationCommandOptionChoice {
    focused = strings.ToLower(strings.TrimSpace(focused))
    if focused == "" {
        // Return empty (Discord shows nothing) — alphabetic dump of 1000 pokemon helps no one
        return nil
    }
    type scored struct {
        choice *discordgo.ApplicationCommandOptionChoice
        score  int
    }
    var results []scored
    for _, p := range deps.GameData.Pokemon {
        engName := strings.ToLower(p.NameEnglish)
        localName := strings.ToLower(deps.Translations.For(userLang).T(fmt.Sprintf("poke_%d", p.ID)))
        score := matchScore(focused, engName, localName, p.ID)
        if score == 0 {
            continue
        }
        results = append(results, scored{
            choice: &discordgo.ApplicationCommandOptionChoice{
                Name:  localName,                                 // displayed to user
                Value: strconv.Itoa(p.ID),                         // sent back as the option value
            },
            score: score,
        })
    }
    sort.Slice(results, func(i, j int) bool { return results[i].score > results[j].score })
    if len(results) > 25 { results = results[:25] }
    out := make([]*discordgo.ApplicationCommandOptionChoice, len(results))
    for i, r := range results { out[i] = r.choice }
    return out
}

// matchScore: 100 for exact match, 50 for prefix match (English), 40 prefix (local),
// 10 for substring, 5 for ID exact match, 0 otherwise.
func matchScore(needle, eng, local string, id int) int {
    if needle == eng || needle == local { return 100 }
    if strings.HasPrefix(eng, needle) { return 50 }
    if strings.HasPrefix(local, needle) { return 40 }
    if strings.Contains(eng, needle) || strings.Contains(local, needle) { return 10 }
    if n, err := strconv.Atoi(needle); err == nil && n == id { return 5 }
    return 0
}
```

**Note for debate:** the autocomplete sends the pokemon **ID** as the option value, with the localized **name** as the visible label. That decouples display from value. But then `TrackMapper` receives a numeric string like `"25"` rather than `"pikachu"`. The text `cmd.track` accepts both — `PokemonResolver` handles ID and name — so this works. We should add a test that pins this behavior.

### Confirmation flow (`slash/confirm.go`)

[Note: confirmation flow was considered and dropped — see Decisions section. The block below is retained only as a record of the design space that was explored.]

Mutating slash commands could in principle use a Verify/Cancel button pattern:

```go
// AskConfirmation returns an InteractionResponse with a verify-cancel ActionRow.
// State is encoded in the button customId so we don't need server-side session state.
//
// customId format: "slash:confirm:<cmdKey>:<verb>:<base64(JSON(args))>"
// "slash:cancel" is a global handler that just deletes the message.
func AskConfirmation(cmd string, tokens []string, summary string) *discordgo.InteractionResponse {
    payload := strings.Join(tokens, " ")
    encoded := base64.RawURLEncoding.EncodeToString([]byte(payload))
    return &discordgo.InteractionResponse{
        Type: discordgo.InteractionResponseChannelMessageWithSource,
        Data: &discordgo.InteractionResponseData{
            Embeds: []*discordgo.MessageEmbed{{Title: "Confirm", Description: summary}},
            Components: []discordgo.MessageComponent{
                discordgo.ActionsRow{Components: []discordgo.MessageComponent{
                    discordgo.Button{Label: "Verify",  Style: discordgo.SuccessStyle, CustomID: "slash:confirm:" + cmd + ":" + encoded},
                    discordgo.Button{Label: "Cancel",  Style: discordgo.DangerStyle,  CustomID: "slash:cancel"},
                }},
            },
            Flags: discordgo.MessageFlagsEphemeral,
        },
    }
}
```

**Debate point**: the customId carries the encoded token list. Discord's customId limit is 100 chars. Base64 of a typical `/track` token list (~60-80 chars) overruns this. Two alternatives:

1. **Side cache**: a short-lived `map[string]pendingMutation` keyed by UUID, customId is just `slash:confirm:<uuid>`. Survives restarts only if persisted to disk. ~5-min TTL is fine because users don't sit on confirmations.
2. **Embed-as-state**: serialise tokens into the embed's `Footer.Text` field (4096 char limit). Custom-id is just the verb. Verify-click handler re-reads the embed. Crash-resilient, but trusts that Discord doesn't let users edit embeds.
3. **No confirmation**: just execute. Slash commands are explicit enough that a typo-check confirmation is debatable value.

**Recommendation:** start with option 3 (no confirmation) for v1. Mutating slash commands have explicit option prompts already, unlike text commands where typos slip in. Add confirmation later if users actually want it. **This decision is reversible** so don't over-invest.

---

## RecentActivity Tracker

### Purpose

Boost autocomplete relevance for type-`pokemon` options on commands where the meaningful subset is "what's actually appearing right now":

- `/raid pokemon:` — show current raid bosses first
- `/quest pokemon:` — show currently-rewarding pokemon first
- `/quest item:` — show currently-rewarding items first
- `/maxbattle pokemon:` — show current max battle bosses first

### Implementation (`processor/internal/tracker/recent_activity.go`)

```go
package tracker

import (
    "sync"
    "time"
)

const recentActivityTTL = 6 * time.Hour

type RecentActivity struct {
    mu              sync.RWMutex
    raidBosses      map[int]time.Time   // pokemonID → last seen
    maxBattleBosses map[int]time.Time
    questPokemon    map[int]time.Time
    questItems      map[int]time.Time
    questCandy      map[int]time.Time
    questMega       map[int]time.Time
}

func NewRecentActivity() *RecentActivity {
    return &RecentActivity{
        raidBosses:      make(map[int]time.Time),
        maxBattleBosses: make(map[int]time.Time),
        questPokemon:    make(map[int]time.Time),
        questItems:      make(map[int]time.Time),
        questCandy:      make(map[int]time.Time),
        questMega:       make(map[int]time.Time),
    }
}

func (r *RecentActivity) RecordRaidBoss(id int)      { r.record(r.raidBosses, id) }
func (r *RecentActivity) RecordMaxBattleBoss(id int) { r.record(r.maxBattleBosses, id) }
func (r *RecentActivity) RecordQuestPokemon(id int)  { r.record(r.questPokemon, id) }
func (r *RecentActivity) RecordQuestItem(id int)     { r.record(r.questItems, id) }
func (r *RecentActivity) RecordQuestCandy(id int)    { r.record(r.questCandy, id) }
func (r *RecentActivity) RecordQuestMega(id int)     { r.record(r.questMega, id) }

func (r *RecentActivity) ActiveRaidBosses() []int { return r.active(r.raidBosses) }
// …etc

func (r *RecentActivity) record(m map[int]time.Time, id int) {
    if id <= 0 { return }
    r.mu.Lock()
    defer r.mu.Unlock()
    m[id] = time.Now()
}

func (r *RecentActivity) active(m map[int]time.Time) []int {
    r.mu.Lock()
    defer r.mu.Unlock()
    cutoff := time.Now().Add(-recentActivityTTL)
    ids := make([]int, 0, len(m))
    for id, ts := range m {
        if ts.Before(cutoff) {
            delete(m, id) // lazy GC during read
            continue
        }
        ids = append(ids, id)
    }
    return ids
}
```

### Wiring

`cmd/processor/raid.go`, `quest.go`, `maxbattle.go` each gain one line after the duplicate-check pass:

```go
// raid.go
ps.recentActivity.RecordRaidBoss(raid.PokemonID)
```

Cost: 6 record calls / sec at peak, all O(1) map writes. Negligible.

### Consumption (`slash/autocomplete/raid_boss.go`)

```go
// RaidBossAutocomplete: if focused is empty, return active raid bosses.
// Otherwise fall through to standard prefix match across all raid pokemon.
func RaidBossAutocomplete(ctx context.Context, deps *bot.BotDeps, focused, userLang string) []*discordgo.ApplicationCommandOptionChoice {
    if focused == "" {
        active := deps.RecentActivity.ActiveRaidBosses()
        return idsToChoices(active, deps, userLang)
    }
    return PokemonAutocomplete(ctx, deps, nil, focused, userLang) // fall through
}
```

---

## Locale Mapping

Discord supports these locale codes: `id, da, de, en-GB, en-US, es-ES, es-419, fr, hr, it, lt, hu, nl, no, pl, pt-BR, ro, fi, sv-SE, vi, tr, cs, el, bg, ru, uk, hi, th, zh-CN, ja, zh-TW, ko`.

PoracleNG's `i18n.Bundle` uses ISO codes (`de`, `fr`, `nl`, …). We need a map:

```go
var discordLocaleToPoracle = map[string]string{
    "de":    "de",
    "fr":    "fr",
    "es-ES": "es",
    "it":    "it",
    "nl":    "nl",
    "pl":    "pl",
    "ru":    "ru",
    "sv-SE": "sv",
    "ja":    "ja",
    "zh-CN": "zh-cn",
    "zh-TW": "zh-tw",
    "ko":    "ko",
    // …
}
```

For `name_localizations` we set Discord's localized name from `bundle.For(poracleCode).T("cmd.track")` (when non-empty and matches Discord's slash-name regex `^[\p{L}\p{N}\p{sc=Devanagari}\p{sc=Thai}_-]{1,32}$`).

For `description_localizations` we use `slash.<command>.desc` keys, which need to be added to each locale file. Initial seed: copy English description into every locale; translators fill in later.

---

## Phased Rollout — Task Breakdown

### Phase 0: Foundation (no user-visible feature)
1. Add `[discord.slash_commands]` config block with master switch.
2. Add `slash/dispatcher.go` skeleton with empty `HandleCommand`/`HandleAutocomplete`.
3. Wire `slash.Dispatcher` into `discordbot/bot.go` behind the config flag.
4. Extend `interaction.go` to branch on interaction type.

### Phase 1: First slash command — `/poracle`
5. Add `slash/definitions.go` with the `poracle` builder.
6. Add `slash/registration.go` — sync at startup, idempotent fingerprint check.
7. Add `slash/context.go` — build `CommandContext` from interaction.
8. Add `slash/reply.go` — `[]Reply` → interaction response.
9. Add `slash/mappers/poracle.go`.
10. Tests: registration produces expected Discord schema, dispatch routes to `cmd.poracle`, reply rendering produces expected interaction body.

### Phase 2: Read-only commands (no mutation, safest)
11. `/tracked` — list user's tracking. Reuse `cmd.tracked` directly. No options.
12. `/help`, `/info`, `/language` (show only), `/version`.

### Phase 3: Pokemon autocomplete + RecentActivity
13. `tracker/recent_activity.go` + wiring from 4 webhook handlers.
14. `slash/autocomplete/dispatcher.go` + `pokemon.go`.
15. Tests covering active-first ordering, ID/name dual matching, locale fallback.

### Phase 4: Mutating commands (the main payload)
16. `/track` — most complex. Multi-PVP, ranges, form filter.
17. `/raid`, `/egg` — raid boss autocomplete using RecentActivity.
18. `/quest` — quest reward autocomplete using RecentActivity.
19. `/invasion`, `/lure`, `/nest`, `/maxbattle`.
20. `/untrack id:` — pick from existing tracking via autocomplete.

### Phase 5: User profile commands
21. `/location` — set lat/lon. Show resulting map back as an embed.
22. `/area` — add/remove areas. Uses select menus (>25 → paginate).
23. `/profile change|create|delete` — sub-commands.

### Phase 6: Polish
24. Localization rollout: build per-locale `slash.*` translation keys, set `name_localizations` / `description_localizations` on registration.
25. Ephemeral mode (config-driven default + per-command override).
26. Permission integration: `command_security` checks before dispatch — Discord rejects with 🙅 equivalent.
27. Rate limit notice for slash users (DM bypass).

---

## Testing — The Debate

This is where we need to align before writing code. Four options, ranked by my preference but all have trade-offs.

### Option A — Layered unit tests (recommended)

**Levels:**
1. **Mapper tests** — `slash/mappers/track_test.go` etc. Pure functions: `(Discord options) → []string`. No Discord, no DB.
2. **Autocomplete tests** — `slash/autocomplete/pokemon_test.go`. Pure functions: `(focused string, deps) → []choice`. Build deps from in-memory game data fixture.
3. **Reply rendering tests** — `slash/reply_test.go`. Pure functions: `([]Reply, interaction) → InteractionResponse`. No HTTP.
4. **Context build tests** — `slash/context_test.go`. Pure functions: `(InteractionCreate, deps) → CommandContext`. No Discord.
5. **RecentActivity tests** — race-test with TTL fast-forward via injectable clock.
6. **Integration test, one per command** — invoke through `Dispatcher.HandleCommand` with a fake `discordgo.Session` that records calls. Asserts end-to-end: interaction → DB mutation → reply.

**Pros**: each layer testable in isolation, fast, robust to discordgo version bumps, clear failure attribution.
**Cons**: integration coverage relies on the fake session being faithful.
**Effort**: ~15-20 small test files, ~1500 LOC test code total.

### Option B — Heavy Discord mock

Mock the full `discordgo.Session` interface (or a wrapped subset) so the Dispatcher writes HTTP-ish responses to a recording mock. Test full flows: `InteractionCreate → ... → recorded API call sequence`.

**Pros**: maximum end-to-end confidence.
**Cons**: discordgo doesn't expose a clean interface — we'd build our own. Brittle to library changes. Tests are slower and harder to write.
**Verdict**: skip unless we have concrete bugs at the integration boundary.

### Option C — Snapshot tests for slash definitions

Serialize each command's `*discordgo.ApplicationCommand` to JSON, compare to golden files in `slash/testdata/*.json`. Catches accidental definition drift (option renamed, type changed, required toggled).

**Pros**: invaluable safety net — definition changes are the single biggest source of user-visible breakage in slash bots.
**Cons**: golden files need updating when intentional changes happen; team needs the discipline to review them.
**Verdict**: do this **in addition to** A. Trivial to add.

### Option D — Shared fixtures driving both surfaces

A golden fixture file (`testdata/slash_text_parity.yaml`) defines:

```yaml
- description: track pikachu great5 100 IV
  slash:
    command: track
    options:
      pokemon: pikachu
      min_iv: 100
      pvp_league: great
      pvp_rank: 5
  text: "track pikachu iv100 great5"
  expected_tokens: ["pikachu", "iv100", "great5"]
```

A test loops over each fixture and asserts that:
1. The slash mapper produces `expected_tokens` from `slash.options`.
2. The text parser produces `expected_tokens` from `text`.

This pins **surface parity** — slash and text invocations of the same command produce the same tokens, so the same business logic runs.

**Pros**: catches the biggest realistic regression class — "the slash flow silently behaves differently from text." Easy to extend per command.
**Cons**: YAML files need maintenance. Some flows (autocomplete) don't fit.
**Verdict**: do this **in addition to** A for the mapper layer.

### Recommendation

**A + C + D**, skip B. Total test surface stays manageable, and we get:
- **A** for fast attribution and developer ergonomics.
- **C** for safety against definition drift (biggest user-visible failure mode).
- **D** for surface parity (biggest behavioral failure mode).

---

## Userstate autocomplete + validation decisions (settled)

### Shared "pick from user's existing state" primitive

Reusable autocomplete handler used by `/untrack`, `/area remove`, `/profile change`, `/profile delete`. Single dispatcher walks listers by name; each lister enumerates one resource type.

- **Listers are plain exported functions**; registered explicitly in `slash.NewDispatcher` setup (one line per lister). Matches existing `cmd.Registry` convention. Mockable in tests by constructing a registry with test stubs.
- **`UserStateLister` interface:**
  ```go
  type UserStateLister func(ctx context.Context, deps *bot.BotDeps, userID string, hint UserStateHint) ([]Choice, error)
  type UserStateHint struct { Subtype, Focused string }
  type Choice struct { Label, Value string }
  ```
- **Listers shipped in v1**: `tracking` (by subtype: pokemon, raid, …), `areas`, `profiles`.
- **Label truncation**: from the front, preserving suffix-anchored identifiers (`[id:N]`, `[#N]`). 100-char Discord cap respected.
- **Empty results** (no tracking, no areas, etc.): return empty list. Discord shows no choices; user infers the absence. Revisit only if confusing in practice.
- **No autocomplete-call caching in v1.** Tracking lists are small; the latency budget on autocomplete is ~3s. Add per-handler ttlcache if profiling shows a problem.

### Validation layer ownership

Three places validation can happen for slash. Use the lowest layer that catches the violation:

| Layer | Validates | Examples |
|---|---|---|
| Discord option type | Type-correctness (free) | Int, Bool, Choice |
| Mapper | Cross-option constraints, format | `/quest` mutual-exclusion, IV range syntax |
| Command.Run | Domain rules | rate limit, registration prerequisites, `everything` permission |

### Mapper error translation

Validation errors emitted from the mapper layer are **translated**. Key namespace: `error.slash.*`.

```json
{
  "error.slash.quest.exactly_one_reward": "specify exactly one reward type — got {0}",
  "error.slash.range.invalid": "{0} must be a number or range (e.g. \"90\" or \"50-100\"), got {1}",
  "error.slash.unregistered": "You need to register first. DM me with !poracle, or run !poracle in {0}.",
  "error.slash.no_permission": "You don't have permission to run /{0}."
}
```

Mapper errors propagate to the dispatcher which sends `🛑 {translated_error}` as an ephemeral reply. Translation happens at the dispatcher level so mapper functions remain pure (returning structured errors with key + format args).

```go
// Mapper returns a typed error; dispatcher translates.
type MapperError struct {
    Key  string
    Args []any
}

func (e *MapperError) Error() string { return e.Key } // dev-facing only

// Example in QuestMapper:
if len(set) > 1 {
    return nil, &MapperError{
        Key:  "error.slash.quest.exactly_one_reward",
        Args: []any{strings.Join(set, ", ")},
    }
}
```

## Test fixture (D — surface parity) decisions (settled)

**Goal:** "Surface parity by construction." For each logical command invocation, the slash mapper and text parser must produce the same token list. One fixture per option group that can affect tokens; ~30-40 fixtures total across all commands.

- **Format: YAML.** Diff-friendly, edit-without-recompile, accessible to non-Go contributors.
- **Location: `processor/internal/bot/testdata/parity.yaml`** — shared between text parser tests and slash mapper tests.
- **Coverage enforced by a meta-test** (`TestEveryCommandAndOptionHasFixture`). Requires at least one fixture per registered slash command AND at least one fixture per option that command exposes. Failure surfaces missing coverage immediately rather than waiting for a behavioural bug.

### Coverage meta-test

```go
// processor/internal/bot/coverage_test.go
//
// Walks the slash command registry and asserts every command + every
// option appears in at least one fixture. Catches the "added a new option
// but forgot the fixture" failure mode at PR-review time.
func TestEveryCommandAndOptionHasFixture(t *testing.T) {
    fixtures := loadParityFixtures(t, "testdata/parity.yaml")

    // Build coverage map: command → set of options seen in fixtures
    covered := map[string]map[string]bool{}
    for _, fix := range fixtures {
        if _, ok := covered[fix.Slash.Name]; !ok {
            covered[fix.Slash.Name] = map[string]bool{}
        }
        for optName := range fix.Slash.Options {
            covered[fix.Slash.Name][optName] = true
        }
    }

    // Walk every registered command and check
    for _, cmd := range slash.AllDefinitions() {
        opts, ok := covered[cmd.Name]
        if !ok {
            t.Errorf("slash command %q has no fixture in parity.yaml", cmd.Name)
            continue
        }
        for _, opt := range cmd.Options {
            // Required options must be covered; optional options must be
            // covered by at least one fixture that sets them.
            if !opts[opt.Name] {
                t.Errorf("slash command %q option %q is never exercised in any parity fixture", cmd.Name, opt.Name)
            }
        }
    }
}
```

Coverage is **token-impact-shaped**: a fixture covers an option by *setting it to a non-default value* so the mapper emits a token. Boolean options need at least one `true` fixture; range options need at least one non-empty fixture. The meta-test enforces the option is *exercised*, not just declared in the YAML.

This raises the fixture count from "~30-40" to closer to **~50-70** (one per option × number of options), but each fixture is 4-6 lines of YAML. Worth the cost.

### Fixture structure

```yaml
- name: track-pikachu-multi-pvp
  description: multiple PVP leagues produce separate league tokens
  command: cmd.track
  slash:
    name: track
    options:
      pokemon: pikachu
      great_rank: 5
      ultra_rank: 10
  text: "!track pikachu great5 ultra10"
  expected_tokens: [pikachu, great5, ultra10]
```

Fields:
- `name` — unique test-case ID (passed to `t.Run`).
- `description` — short prose explanation (shown on failure).
- `command` — expected `CommandKey` from text parsing (asserts text parser routes correctly).
- `slash.name` — slash command name as registered.
- `slash.options` — `option → value` map (typed: strings stay strings, ints stay ints, bools stay bools).
- `text` — full text invocation including prefix.
- `expected_tokens` — what both surfaces should produce.

### Parity test

```go
// processor/internal/bot/parity_test.go
//
// Drives both the slash mapper and text parser from the same fixture,
// asserts both produce expected_tokens. Catches slash↔text divergence
// at the token boundary before it reaches Command.Run.
func TestSlashTextParity(t *testing.T) {
    fixtures := loadParityFixtures(t, "testdata/parity.yaml")
    deps := buildTestDeps(t)
    for _, fix := range fixtures {
        t.Run(fix.Name, func(t *testing.T) {
            mapper := mappers.Lookup(fix.Slash.Name)
            slashTokens, err := mapper(buildOptions(fix.Slash.Options))
            require.NoError(t, err)

            parsed := deps.Parser.Parse(fix.Text)
            require.Equal(t, fix.Command, parsed.CommandKey)

            assertTokensEqual(t, fix.ExpectedTokens, slashTokens)
            assertTokensEqual(t, fix.ExpectedTokens, parsed.Args)
        })
    }
}
```

Comparison is order-insensitive (`sort.Strings` both sides) — argument order doesn't affect `Command.Run`.

### Fixture coverage per command (minimum)

| Command | # fixtures | Cases to cover |
|---|---|---|
| `/track` | 4 | basic pokemon, multi-pokemon CSV, multi-PVP, form+clean+template |
| `/raid` | 2 | pokemon boss, level keyword |
| `/quest` | 5 | one per reward type (pokemon/item/stardust/candy/mega_energy) |
| `/untrack` | 1 | any type → id:N |
| `/area`, `/profile` | 1 each | sub-command add/remove/change |
| Others | 1-2 each | basic + one variant |

Total ~30-40 fixtures, YAML under 500 lines.

### What parity does NOT cover (handled elsewhere)

- **Reply output correctness** — covered by per-command integration tests (option A).
- **Autocomplete results** — separate unit tests, doesn't fit token-parity model.
- **Permission/registration flow** — dispatcher unit tests.
- **Discord schema validation** — option C snapshot tests.

## Language + permissions decisions (settled)

**Major scope clarification: slash commands are personal-DM-scope only.** Always target the sender, never channel/webhook. Admin channel/webhook management remains text-only.

Consequences:
- No `BuildTarget` indirection for slash. Target = sender, always.
- No channel-context check. Slash works in any channel (registered or not); responses are ephemeral or real DM.
- No `user:` / `name:` admin override options in slash command definitions.
- All slash commands target the sender for v1: `/untrack`, `/area`, `/profile`, `/location`, `/language`, `/tracked`, `/track`, all per-type tracking commands, etc.

### Dispatch flow

```
InteractionCreate
  ↓ defer ephemeral (within 100ms)
Resolve language (human lookup + locale fallback chain)
  ↓ no human + not in skip-registration list? → 🛑 ephemeral with registration hint
command_security check (uses existing config — same rules as text)
  ↓ blocked? → 🛑 ephemeral
Mapper builds tokens from options
  ↓ mapper error? → 🛑 ephemeral
Build CommandContext (TargetID=UserID, TargetType="discord:user")
  ↓
cmd.Run(ctx, tokens) → []Reply
  ↓
reply.Send (ephemeral by default; real DM for Reply.IsDM=true)
```

### Language resolution chain (slash dispatcher)

```go
func resolveLanguage(ic *discordgo.InteractionCreate, human *store.HumanLite, cfg *config.Config) string {
    // 1. Registered user's stored language (operator-curated; highest)
    if human != nil && human.Language != "" { return human.Language }
    // 2. Discord user locale → PoracleNG code (for /version /help guests)
    if pCode, ok := discordLocaleToPoracle[ic.Locale]; ok { return pCode }
    // 3. Operator's configured locale
    return cfg.General.Locale
}
```

### Skip-registration list

Hardcoded (matches text bot's special-case approach):
```go
var commandsSkippingRegistration = map[string]bool{
    "cmd.version": true,
}
```
Only one entry in practice. Even `/help` requires registration (matches `!help` behaviour).

### Unregistered-user error copy

```
🛑 You need to register first.
DM me with !poracle, or run !poracle in #poracle-register on this server.
```

Channel link templatised from `[discord] registration_channels` config when one is configured for the user's guild. Fall back to "DM me with !poracle" otherwise.

### Other settled

- **`command_security` shared between text and slash** — same config, same rules. A user denied `!track` is also denied `/track`.
- **Rate-limited users can still run commands.** Rate limiting only affects alert *delivery*, not command *invocation*. Slash matches this.
- **Autocomplete returns empty for unregistered users** — no misleading UI suggesting they can submit working commands.

## Registration + i18n decisions (settled)

- **Idempotent sync** via fingerprint cache at `config/.cache/slash-fingerprint.json`. Fingerprint = sha256 of deterministically-serialized command set (names, options recursively, choices, localizations). Cache stores `{global: {fingerprint, synced_at}, guilds: {<gid>: {fingerprint, synced_at}}}`. Skip the `BulkOverwrite` API call when the fingerprint matches.
- **`-sync-slash-commands` CLI flag** forces resync regardless of cache. Operator escape hatch for manual Discord-portal edits.
- **Sync runs post-`session.Open()`**; failure is non-fatal (logged, bot continues with text only).
- **Localization keys**:
  - `cmd.<name>` — reused for both text and slash command names (existing keys).
  - `slash.<name>.desc` — command description.
  - `slash.<name>.opt.<option>` — option description.
  - `slash.<name>.opt.<option>.choice.<choice>` — choice label.
- **Iterate only over installation-loaded languages** (`bundle.LoadedLanguages()`), not all Discord locales. Reverse-map Poracle code → Discord locale; skip when no Discord match exists. This keeps fingerprints stable across installations with different language sets.
- **Missing-key handling**:
  - Missing **English** key → log warning at startup (broken intent).
  - Missing **non-English** key → silent fallback to English (translators catching up; not an error).
- Filter localizations defensively against Discord's slash-name regex (`^[\p{L}\p{N}_-]{1,32}$`) — invalid entries are silently dropped, since one bad localization rejects the entire command registration.

```go
// Reverse map: PoracleNG locale code → Discord locale.
var poracleToDiscord = map[string]discordgo.Locale{
    "de":    discordgo.German,
    "fr":    discordgo.French,
    "es":    discordgo.SpanishES,
    "it":    discordgo.Italian,
    "nl":    discordgo.Dutch,
    "pl":    discordgo.Polish,
    "ru":    discordgo.Russian,
    "ja":    discordgo.Japanese,
    "zh-cn": discordgo.ChineseCN,
    "zh-tw": discordgo.ChineseTW,
    "ko":    discordgo.Korean,
    "sv":    discordgo.SwedishSE,
    "pt-br": discordgo.PortugueseBR,
    // …extend per available_languages…
}

func localizationsForKey(bundle *i18n.Bundle, key string) *map[discordgo.Locale]string {
    out := make(map[discordgo.Locale]string)
    for _, lang := range bundle.LoadedLanguages() {
        if lang == "en" { continue }
        discordCode, ok := poracleToDiscord[lang]
        if !ok { continue }
        tr := bundle.For(lang)
        if tr == nil { continue }
        val := tr.T(key)
        if val == "" || val == key { continue }  // no translation; falls back to English silently
        if !validSlashName(val) { continue }     // defensive Discord validation
        out[discordCode] = val
    }
    if len(out) == 0 { return nil }
    return &out
}
```

## Reply / error format decisions (settled)

- **`/version` requires no new business code** — existing `cmd.version` `Run` returns "PoracleNG X.Y.Z (commit, date)" as a single text Reply. Slash mapper is the trivial empty-tokens case; reply renderer renders the Text. Pure smoke test of the wiring.
- **All slash responses are ephemeral.** No config knob. Poracle conversations are inherently personal (private tracking filters, private alert lists, private profiles); alerts already arrive via DM; nothing about slash-command output is meaningful to other users in a channel. No reason to add the complexity of a public/ephemeral toggle.
- **Plain text replies with emoji prefixes** for errors (`🛑` error, `⚠️` warning, `⏳` rate limit). Matches text-command output style; no embed wrapping unless a command genuinely returns structured data.
- **`Reply.IsDM = true` preserved semantically.** Means "actual DM channel send" (operator private actions like webhook add). For slash: real DM is sent, interaction gets ephemeral `✅ Sent to DM` confirmation. This is genuinely different from ephemeral — ephemeral disappears when the user closes the message; DM persists in their conversation history.

### `reply.Send` contract

```go
// Send dispatches a Reply slice to Discord. Pre: interaction has been deferred.
// First Reply → InteractionResponseEdit. Subsequent → FollowupCreate.
// Reply.IsDM=true → actual DM send + ephemeral "✅ Sent to DM" confirmation.
// All responses ephemeral when `ephemeral` is true.
func Send(s *discordgo.Session, ic *discordgo.InteractionCreate, replies []bot.Reply, ephemeral bool) error
```

## Boot wiring decisions (settled)

- `RecentActivity` is **always constructed** at startup, regardless of `[discord.slash_commands].enabled`. Cheap (6 small maps); reusable for future text-command autocompletes, dashboards, stats endpoints.
- Slash code lives at `processor/internal/discordbot/slash/` — paired with the Discord gateway it talks to.
- Sync runs after `session.Open()` returns (we need `session.State.User.ID`). Sync failure is non-fatal — bot keeps running with text commands.
- New `BotDeps.RecentActivity *tracker.RecentActivity` field.
- New config block `[discord.slash_commands]` with master switch, registration scope, per-command rename map.

## Decisions (settled with user)

1. **Test approach:** A + C + D. Skip B (Discord mock too brittle).
2. **Confirmation flow:** Skip for v1. Slash options are explicit; revisit if users ask.
3. **Autocomplete value format:** **Canonical English names** as the option `value`, localized names as the option `name` (label). Rationale: token output identical between slash and text → test fixture D becomes trivial; logs readable; `PokemonResolver` already accepts canonical names. (Originally proposed IDs but switched after debate.)
4. **Localization rollout:** English first; locale map filled during Phase 6.
5. **Global vs guild registration:** Config flag `register_globally = true` default + optional `guilds = []` for dev (instant propagation).
6. **Effective language:** Honour `humans.language`. Fall back to Discord-locale only when `humans.language` is empty.
7. **Channel/webhook target overrides:** Skip v1. Admins keep text path. Later: `/manage-tracking` admin-only.
8. **Rate-limit interaction:** Ephemeral error when blocked. Two-phase limiter still applies; slash response counts as a delivery.
9. **`/poracle-test` slash equivalent:** **No.** Admin function, low frequency, text command stays. Other admin-only commands also remain text-only (broadcast, apply, enable/disable, etc.).
10. **First command swap:** **NOT** `/poracle` (special registration semantics, skips `BuildTarget`, registration-channel guard). Use **`/version`** as the pipeline smoke test, then **`/tracked`** as the first real command (registered-user-only, DB read, multi-message reply).

## `/quest` shape (reward-type split)

User confirmed: split reward types into separate options so the autocomplete dropdown is reward-specific and easy to find.

```go
Options: []*discordgo.ApplicationCommandOption{
    {Type: String, Name: "pokemon",     Autocomplete: true},  // pokemon-reward (uses RecentActivity)
    {Type: String, Name: "item",        Autocomplete: true},  // item-reward (uses RecentActivity)
    {Type: Int,    Name: "stardust"},                          // stardust ≥ N
    {Type: String, Name: "candy",       Autocomplete: true},  // candy for pokemon X
    {Type: String, Name: "mega_energy", Autocomplete: true},  // mega energy for pokemon X
    {Type: String, Name: "xl_candy",    Autocomplete: true},  // XL candy for pokemon X
    {Type: Int,    Name: "min_amount"},                        // min amount for items
    {Type: Bool,   Name: "shiny"},                             // shiny pokemon-reward
    {Type: Int,    Name: "ar",          Choices: [any/ar/non-ar]},  // migration 0034
    {Type: Int,    Name: "distance"},
    {Type: Bool,   Name: "clean"},
    {Type: String, Name: "template",    Autocomplete: true},
}
```

**12 options.** One /quest call = one tracking rule (one reward type set per call). User runs the command multiple times for multiple reward types — matches text command behaviour.

**Autocomplete sources** (each fed by RecentActivity where applicable):

| Option | Source | RecentActivity? |
|---|---|---|
| `pokemon` | Game data pokemon list | `ActiveQuestPokemon()` boosts to top when focused is empty |
| `item` | Game data items | `ActiveQuestItems()` boosts to top |
| `candy` | Game data pokemon | `ActiveQuestCandy()` boosts to top |
| `mega_energy` | Pokemon with mega evos | `ActiveQuestMega()` boosts to top |
| `xl_candy` | Game data pokemon | Same as candy |
| `template` | DTS template store filtered by type=quest+platform | — |

## `/untrack` shape (with autocomplete pick from existing rules)

```go
Options: []*discordgo.ApplicationCommandOption{
    {Type: Int, Name: "type", Required: true, Choices: [
        {pokemon, "pokemon"}, {raid, "raid"}, {egg, "egg"}, {quest, "quest"},
        {invasion, "invasion"}, {lure, "lure"}, {nest, "nest"}, {gym, "gym"},
        {fort, "fort"}, {maxbattle, "maxbattle"},
    ]},
    {Type: String, Name: "tracking", Required: true, Autocomplete: true},
}
```

**Two-click removal flow:**
1. User picks `type: raid`.
2. `tracking:` autocomplete fires, lists user's raid rules as `"Mewtwo team:any d:500 [id:12]"`.
3. User selects → option value is `"12"`.
4. Mapper emits token `id:12`.
5. Dispatch through `cmd.untrack` (handles `id:N` across all types — see CLAUDE.md).

**Why single `/untrack` (not per-type or `/raid remove` sub-commands):**
- One discoverable command vs 10 new top-level commands or 10 nested sub-command containers.
- Discord's slash-command list stays clean.
- Localization only happens once.
- Two clicks instead of one is a tolerable cost for the simplification.

**Autocomplete implementation** uses existing `rowtext.Generator` for descriptions so labels match `!tracked` output exactly. Truncate descriptions (never the `[id:N]` suffix) when label exceeds Discord's 100-char limit.

**Filter-based removal** (`!untrack iv:100` removes all 100% IV rules) stays **text-only**. Power-user feature, fits the text command's bulk-by-filter grammar; doesn't fit slash's typed-option model cleanly.

**Generalised pattern:** the "pick from user's existing state" autocomplete is reusable for `/area` (pick from enabled areas), `/profile` actions (pick from existing profiles), etc. Build it as a reusable primitive in `slash/autocomplete/userstate.go`.

---

## Full Command Reference (locked)

Each block lists slash options, the text-command equivalent grammar, and an example invocation showing the tokens the mapper produces. Per-type commands are **add-only**; removal lives exclusively in `/untrack`.

### `/track` — 10 options

```go
{String,  "pokemon",     Required, Autocomplete}    // pokemon ID; includes "Everything" entry
{String,  "iv",          Autocomplete}              // "100", "95", "0-0"
{Int,     "distance"}
{Int,     "great_rank"}
{Int,     "ultra_rank"}
{Int,     "little_rank"}
{Bool,    "clean"}
{String,  "template",    Autocomplete}
{String,  "form",        Autocomplete}              // cascades from selected pokemon
{String,  "size",        Choices: [xxs/xs/m/xl/xxl]}
```

**Pokemon autocomplete**:
- **Label**: `"Pikachu (#25)"` (localized pokemon name + numeric ID)
- **Value**: `"25"` (pokemon ID as decimal string), or `"everything"` for the special selector
- Includes **"Everything"** as the first option when the user is allowed by `tracking.everything_flag_permissions`. When `deny`, omit.

Text parser accepts numeric IDs (`!track 25` works identically to `!track pikachu`) so no special handling needed at command-handler layer. Slash → text parity fixture uses the ID form on both sides for consistency.

**Form autocomplete (cascading)**:
- Reads the currently-selected `pokemon` option value from the interaction.
- Returns the forms available for that pokemon (e.g. for Vulpix #37: `Normal`, `Alolan`).
- Value sent: form name string (e.g. `"alola"`) — emits text token `form:alola`.
- When no pokemon selected yet: empty (Discord shows no suggestions).

**Dropped**: `gender`, `gen`, `type`, `level`, `cp`, `move`, `atk`, `def`, `sta`, `time`, `weight`. All available via `!track`.

**Example**: `/track pokemon:25 iv:95 great_rank:5 ultra_rank:10 distance:500`
→ tokens: `["25", "iv95", "great5", "ultra10", "d500"]`

**Example with everything**: `/track pokemon:everything iv:100`
→ tokens: `["everything", "iv100"]`

---

### `/raid` — 6 options

```go
{String,  "boss",        Autocomplete}              // pokemon ID, when targeting specific pokemon
{String,  "level",       Autocomplete}              // human-readable level (e.g. "Legendary", "Shadow Level 3", "Everything")
{Int,     "team",        Choices: [any/valor/mystic/instinct/harmony]}
{Int,     "distance"}
{Bool,    "clean"}
{String,  "template",    Autocomplete}
```

**Either `boss` OR `level` is required** — mapper validates mutual exclusion (one or the other, not both). Matches text grammar where `!raid mewtwo` (pokemon) and `!raid legendary` (level) are alternatives.

**`boss` autocomplete**:
- Label: `"Mewtwo (#150)"`, value: `"150"`.
- When focused is empty: RecentActivity-active raid bosses first.
- Includes `"Everything"` (value `everything`) as the "any raid pokemon" selector.

**`level` autocomplete (Choice-list style with human-readable labels)**:

| Label | Value | Text token |
|---|---|---|
| `"Tier 1"` | `1` | `1` |
| `"Tier 3"` | `3` | `3` |
| `"Tier 5"` | `5` | `5` |
| `"Tier 6"` | `6` | `6` |
| `"Mega"` | `mega` | `mega` |
| `"Legendary"` | `legendary` | `legendary` |
| `"Shadow Tier 1"` | `shadow1` | `shadow1` |
| `"Shadow Tier 3"` | `shadow3` | `shadow3` |
| `"Shadow Tier 5"` | `shadow5` | `shadow5` |
| `"Ultra Beast"` | `ultra beast` | `ultra beast` |
| `"Everything"` | `everything` | `everything` |

Exact list driven by `ParamRaidLevelName` keywords in `bot/argmatch.go` plus the level integers configured for raids. Implementation reads from a `raidLevelChoices()` helper that builds this list from game data + the argmatch keywords.

**Dropped**: `move`, `gym`, `exclusive`. All via `!raid`.

**Example (boss = pokemon)**: `/raid boss:150 distance:1000` → `["150", "d1000"]`
**Example (level)**: `/raid level:legendary distance:500` → `["legendary", "d500"]`

---

### `/egg` — 5 options

```go
{String,  "level",       Required, Autocomplete}    // same Choice-list as /raid level
{Int,     "team",        Choices: [any/valor/mystic/instinct/harmony]}
{Int,     "distance"}
{Bool,    "clean"}
{String,  "template",    Autocomplete}
```

Shares the **`raidLevelChoices()`** helper with `/raid`. Includes `"Everything"` for tracking any raid egg.

**Example**: `/egg level:5 distance:1000` → `["5", "d1000"]`
**Example (everything)**: `/egg level:everything` → `["everything"]`

---

### `/quest` — 9 options

```go
{String,  "pokemon",     Autocomplete}     // pokemon reward (RecentActivity-boosted)
{String,  "item",        Autocomplete}     // item reward (RecentActivity-boosted)
{Int,     "stardust"}                      // stardust ≥ N
{String,  "candy",       Autocomplete}     // candy reward — pick pokemon
{String,  "mega_energy", Autocomplete}     // mega energy — pick pokemon
{String,  "xl_candy",    Autocomplete}     // XL candy — pick pokemon
{Int,     "min_amount"}                    // min quantity for items
{Int,     "distance"}
{Bool,    "clean"}
{String,  "template",    Autocomplete}
```

**Dropped**: `shiny` (never happens in practice), `ar` (specialist, AR / no-AR split via text).

User sets exactly **one** reward type per call (pokemon OR item OR stardust OR candy OR mega_energy OR xl_candy). Multiple = mapper error → ephemeral reply.

**Example (item reward)**: `/quest item:razzberry min_amount:5` → `["item:razzberry", "amount:5"]`
**Example (pokemon reward)**: `/quest pokemon:bulbasaur` → `["bulbasaur"]`
**Example (stardust)**: `/quest stardust:1000` → `["stardust:1000"]`

---

### `/invasion` — 4 options

```go
{String,  "grunt_type",  Required, Autocomplete}     // grunt or special incident; RecentActivity-boosted
{Int,     "distance"}
{Bool,    "clean"}
{String,  "template",    Autocomplete}
```

**`grunt_type` autocomplete** includes **both regular grunts AND special incident types**:
- Regular grunts: `"Fire Grunt"`, `"Water Grunt"`, `"Giovanni"`, `"Cliff"`, `"Sierra"`, `"Arlo"`, ...
- **Special incidents**: `"Kecleon"`, `"Gold Pokéstop"` (gold-stop), `"Showcase"`, `"Pokestop Spawn"`, etc. — driven by `invasions.json` (classic.json) data plus the special-incident IDs.

Sourced from `gamedata.Grunts` + a small list of special incident keywords. The text command already recognises these as `grunt_type` tokens (e.g. `!invasion kecleon`), so the mapper emits the lowercased value as-is.

**Dropped**: `gender`, `reward pokemon` (filter by grunt type only). Both via `!invasion`.

**Example**: `/invasion grunt_type:fire` → `["fire"]`
**Example (special)**: `/invasion grunt_type:kecleon` → `["kecleon"]`
**Example (gold pokestop)**: `/invasion grunt_type:gold-stop` → `["gold-stop"]`

---

### `/lure` — 4 options

```go
{String,  "lure_type",   Required, Choices: [glacial/mossy/magnetic/rainy/sparkly/normal]}
{Int,     "distance"}
{Bool,    "clean"}
{String,  "template",    Autocomplete}
```

**Example**: `/lure lure_type:glacial distance:500` → `["glacial", "d500"]`

---

### `/nest` — 5 options

```go
{String,  "pokemon",     Required, Autocomplete}
{Int,     "min_spawn_avg"}
{Int,     "distance"}
{Bool,    "clean"}
{String,  "template",    Autocomplete}
```

**Example**: `/nest pokemon:pikachu min_spawn_avg:30` → `["pikachu", "t30"]`

---

### `/maxbattle` — 5 options

```go
{String,  "pokemon",     Required, Autocomplete}    // RecentActivity-boosted
{Int,     "level",       Choices: [1..6]}
{Bool,    "gmax"}
{Int,     "distance"}
{Bool,    "clean"}
{String,  "template",    Autocomplete}
```

**Dropped**: `move`. Via `!maxbattle`.

**Example**: `/maxbattle pokemon:groudon level:6 gmax:true` → `["groudon", "level6", "gmax"]`

---

### `/gym` — 6 options

```go
{Int,     "team",        Choices: [any/valor/mystic/instinct/harmony]}
{Bool,    "slot_changes"}
{Bool,    "battle_changes"}
{Int,     "distance"}
{Bool,    "clean"}
{String,  "template",    Autocomplete}
```

**Example**: `/gym team:valor slot_changes:true` → `["valor", "slot changes"]`

(Note: text grammar uses two-word `slot changes` due to underscore→space conversion.)

---

### `/fort` — 5 options

```go
{Int,     "fort_type",   Required, Choices: [pokestop/gym]}
{Bool,    "include_empty"}
{Int,     "distance"}
{Bool,    "clean"}
{String,  "template",    Autocomplete}
```

**Dropped**: `change_types` (multi-select awkward in Discord; defer to text).

**Example**: `/fort fort_type:pokestop include_empty:true` → `["pokestop", "empty"]`

---

### `/untrack` — sub-commands per type

```
/untrack pokemon    tracking:<autocomplete>
/untrack raid       tracking:<autocomplete>
/untrack egg        tracking:<autocomplete>
/untrack quest      tracking:<autocomplete>
/untrack invasion   tracking:<autocomplete>
/untrack lure       tracking:<autocomplete>
/untrack nest       tracking:<autocomplete>
/untrack gym        tracking:<autocomplete>
/untrack fort       tracking:<autocomplete>
/untrack maxbattle  tracking:<autocomplete>
```

10 sub-commands, each with one required `tracking` option:

```go
{String, "tracking", Required, Autocomplete}     // user's existing rules of the sub-command's type
```

Mapper switches on the sub-command name to set the autocomplete lister subtype hint; the tracking option value is the UID, mapper emits `id:<uid>` token. Same UID-based removal in the underlying `cmd.untrack`.

**Why sub-commands instead of a `type` option**: one less click (the user picks `/untrack pokemon` directly rather than `/untrack` then `type: pokemon`), cleaner cascading autocomplete (the `tracking` autocomplete naturally knows the type from the sub-command name without inspecting other options), and matches Discord-native UX patterns.

**Example**: `/untrack raid tracking:<picks "Mewtwo team:any [id:12]">` → `["id:12"]`

---

### `/area` — sub-commands (already has add/remove grammar in text)

```
/area add     area:<autocomplete from configured areas>
/area remove  area:<autocomplete from user's currently-enabled areas>
/area show
```

Text grammar already uses `!area add london` / `!area remove london`, so sub-command structure mirrors. **The "pick from user state" autocomplete in `/area remove` is the same primitive as `/untrack tracking` — share the implementation.**

---

### `/location` — 1 option

```go
{String,  "place",       Required}     // "51.28, 1.08" OR a place name like "Canterbury, UK"
```

**Mapper accepts either form:**
1. **Coordinates** matching `LAT,LON` (with optional spaces) → emit canonical `"<lat>,<lon>"` token.
2. **Place name** → forward-geocode via `deps.Geocoder.Forward(place)` to get lat/lon; emit `"<lat>,<lon>"` token. Fall back to a `MapperError` with `error.slash.location.geocode_failed` if the lookup fails or returns no results.

The underlying `cmd.location` command receives a coords token in both cases, so the business logic is unchanged. Geocoding is slash-only convenience.

**Example (coords)**: `/location place:"51.28, 1.08"` → `["51.28,1.08"]`
**Example (place name)**: `/location place:"Canterbury, UK"` → geocoder returns `51.2802,1.0789` → token `["51.2802,1.0789"]`

---

### `/profile` — sub-commands

```
/profile list
/profile change   name:<autocomplete from existing profiles>
/profile create   name:<string>
/profile delete   name:<autocomplete from existing profiles>
```

`change`/`delete` use the existing-state autocomplete primitive.

---

### `/language` — 1 option

```go
{String,  "code",        Required, Choices: <built from i18n.Bundle.LoadedLanguages()>}
```

**Example**: `/language code:de` → `["de"]`

---

### `/tracked` — no options

Just list the caller's tracking. Multi-Reply output uses `Reply.IsDM` to push long lists to DM.

---

### `/help` — 1 option

```go
{String,  "topic",       Autocomplete}     // command name; empty = general help
```

---

### `/info` — no options

---

### `/version` — no options

Phase 1 smoke test command.

---

## Slash command surface summary

| Command | Options | Notes |
|---|---|---|
| `/track`     | 10 | most-used; PVP options as 3 separate ints; `everything` selector; cascading form autocomplete |
| `/raid`      | 6  | boss OR level (mutual exclusion); level is human-labeled Choice list |
| `/egg`       | 5  | shared level Choice list with /raid |
| `/quest`     | 9  | reward types split for autocomplete clarity |
| `/invasion`  | 4  | includes special incidents (kecleon, gold-stop, etc.) |
| `/lure`      | 4  | |
| `/nest`      | 5  | |
| `/maxbattle` | 5  | |
| `/gym`       | 6  | |
| `/fort`      | 5  | |
| `/untrack`   | sub-cmds | one sub-command per tracking type; tracking option uses userstate autocomplete |
| `/area`      | sub-cmds | add / remove / show |
| `/location`  | 1  | accepts coords OR place name (forward geocode) |
| `/profile`   | sub-cmds | list / change / create / delete |
| `/language`  | 1  | |
| `/tracked`   | 0  | |
| `/help`      | 1  | |
| `/info`      | 0  | |
| `/version`   | 0  | smoke test (Phase 1) |

**18 slash commands**, well under Discord's 100-command application cap.

---

## Commands deliberately NOT in slash (text-only)

| Command | Why text-only |
|---|---|
| `!poracle` | Registration, special semantics (skips BuildTarget, registration-channel guard) |
| `!poracle-test` | Admin-only test tool |
| `!broadcast` | Admin-only |
| `!apply` | Admin-only batch operations |
| `!enable` / `!disable` | Admin user management |
| `!unregister` | Account-destructive; explicit text confirmation safer |
| `!channel` / `!webhook` / `!autocreate` | Discord-specific admin |
| `!role` | Role subscription, niche |
| `!ask` / `!script` | Power-user / NLP / scripting |
| `!userlist` / `!backup` / `!restore` | Admin operations |
| `!start` / `!stop` | Per-channel enable/disable, infrequent |
| `!weather` | Read-only info; could add later as `/weather` if requested |

Power users and admins continue to use text commands for the full feature set. Slash is the **common path**; text is the **full power path**.

## Token-tokenisation philosophy

Settled: synthesize text tokens; do **not** add a `RunParsed` interface. Minimum blast radius; no command-handler changes; reuses `ctx.ArgMatcher.Match` for validation parity.

## Option-shape simplification — ranges as single string options

`/track` originally had `min_iv` + `max_iv` as separate options. Collapse to a single `iv` string option accepting text-command syntax:

```
iv: "90"          → token "iv90"        (>= 90)
iv: "50-100"      → token "iv50-100"    (range)
iv: "0-99"        → token "iv0-99"      (range, excludes 100)
```

Apply to: `iv`, `cp`, `level`, `atk`, `def`, `sta`, `time`, `weight` on `/track`; `level` on `/raid` and `/maxbattle`.

Saves ~15 options across all commands. Easier test fixture D parity (identical token shape).

**Pair each range option with autocomplete** that suggests common values (`"90"`, `"95"`, `"100"`, `"50-100"`, `"75-99"`). User can pick from the list or type a custom range. Best of both — discoverability without flexibility loss.

### Final `/track` definition

```go
Options: []*discordgo.ApplicationCommandOption{
    {Type: String, Name: "pokemon",    Required: true, Autocomplete: true},

    {Type: String, Name: "iv",         Autocomplete: true},  // "100", "95", "0-0"

    {Type: Int,    Name: "distance"},
    {Type: Int,    Name: "gender",     Choices: [all/male/female/genderless]},

    {Type: Int,    Name: "great_rank"},   // text token: "great<N>"
    {Type: Int,    Name: "ultra_rank"},   // text token: "ultra<N>"
    {Type: Int,    Name: "little_rank"},  // text token: "little<N>"

    {Type: Bool,   Name: "clean"},
    {Type: String, Name: "template",   Autocomplete: true},
    {Type: String, Name: "form"},
    {Type: String, Name: "size",       Choices: [all/xxs/xs/m/xl/xxl]},
    {Type: Int,    Name: "gen",        Choices: [1..9]},
    {Type: String, Name: "type",       Autocomplete: true},
}
```

**13 options.** Under Discord's 25 cap with comfortable headroom.

**Dropped from slash (text-only):**
- `cp` (rarely used)
- `level` (high-IV filter already correlates with level 30+)
- `move` (very niche — raid attackers, PVP move filters)
- `atk`/`def`/`sta` (niche individual IV stats)
- `time` (specialised)
- `weight` (irrelevant now)

Power users keep all of these via the `!` command. The slash surface is the "common path"; the text surface is the "full power" path.

**Kept `type`** despite the simplification drive because it's an alternative *selection* mode (`/track type:grass iv:95` = "track all grass pokemon"), not a filter. Dropping would force users to enumerate every grass pokemon individually in the autocomplete — meaningfully awkward.

**PVP as 3 options, not league+rank pair:** Multi-league is the common use case (`!track pikachu great5 ultra10`); slash matches this directly. `great_rank: 5, ultra_rank: 10` produces both `great5` and `ultra10` tokens.

### Mapper for `/track`

```go
func TrackMapper(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
    o := flattenOptions(opts)
    tokens := []string{}

    // pokemon (required): may be comma-separated; lower-cased canonical names
    for _, p := range splitCSV(o["pokemon"].StringValue()) {
        tokens = append(tokens, strings.ToLower(p))
    }

    // iv range — value already in canonical text grammar from autocomplete
    if v, ok := o["iv"]; ok && v.StringValue() != "" {
        tokens = append(tokens, "iv"+v.StringValue())
    }

    // PVP options — each non-zero league emits its own token
    for _, league := range []string{"great", "ultra", "little"} {
        if opt, ok := o[league+"_rank"]; ok && opt.IntValue() > 0 {
            tokens = append(tokens, fmt.Sprintf("%s%d", league, opt.IntValue()))
        }
    }

    if v, ok := o["distance"]; ok && v.IntValue() > 0 {
        tokens = append(tokens, fmt.Sprintf("d%d", v.IntValue()))
    }
    switch o["gender"].IntValue() {
    case 1: tokens = append(tokens, "male")
    case 2: tokens = append(tokens, "female")
    case 3: tokens = append(tokens, "genderless")
    }
    if v, ok := o["clean"]; ok && v.BoolValue() {
        tokens = append(tokens, "clean")
    }
    if v, ok := o["template"]; ok && v.StringValue() != "" {
        tokens = append(tokens, "template:"+v.StringValue())
    }
    if v, ok := o["form"]; ok && v.StringValue() != "" {
        tokens = append(tokens, "form:"+v.StringValue())
    }
    if v, ok := o["size"]; ok && v.StringValue() != "" && v.StringValue() != "all" {
        tokens = append(tokens, v.StringValue())
    }
    if v, ok := o["gen"]; ok && v.IntValue() > 0 {
        tokens = append(tokens, fmt.Sprintf("gen%d", v.IntValue()))
    }
    if v, ok := o["type"]; ok && v.StringValue() != "" {
        tokens = append(tokens, strings.ToLower(v.StringValue()))
    }
    return tokens, nil
}
```

### Autocomplete suggestion lists

```go
var ivSuggestions = []string{"100", "95", "0-0"}
```

`level`, `cp`, `time`, `weight` suggestions intentionally omitted — those options are not surfaced in slash. Add them only if/when those options return to the slash surface.

User's typed input is always inserted as the first option (so custom ranges still submit cleanly):

```go
func RangeAutocomplete(focused string, suggestions []string) []*discordgo.ApplicationCommandOptionChoice {
    focused = strings.TrimSpace(focused)
    out := []*discordgo.ApplicationCommandOptionChoice{}
    if focused != "" {
        // Echo user's input as option 0
        out = append(out, &discordgo.ApplicationCommandOptionChoice{Name: focused, Value: focused})
    }
    for _, s := range suggestions {
        if focused == "" || strings.HasPrefix(s, focused) {
            out = append(out, &discordgo.ApplicationCommandOptionChoice{Name: s, Value: s})
        }
    }
    return out
}
```

Same mechanism is reused for any future range option — `time`, `weight`, `cp` etc. — if we ever decide to surface them in slash.

### Mapper updated for range-string approach

```go
// Range option: pass through if non-empty, prepend the option name.
func rangeToken(prefix string, opt *opt) string {
    if opt == nil { return "" }
    val := strings.TrimSpace(opt.StringValue())
    if val == "" { return "" }
    return prefix + val   // "iv" + "50-100" → "iv50-100"
}

func TrackMapper(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
    o := flattenOptions(opts)
    tokens := []string{}

    // pokemon: split CSV, lowercase, append each
    for _, p := range splitCSV(o["pokemon"].StringValue()) {
        tokens = append(tokens, strings.ToLower(p))
    }

    for _, prefix := range []string{"iv", "cp", "level", "atk", "def", "sta"} {
        if tok := rangeToken(prefix, o[prefix]); tok != "" {
            tokens = append(tokens, tok)
        }
    }
    // …distance, gender, pvp_league, pvp_rank, clean, template, form, size, gen, type, move…

    return tokens, nil
}
```

### Autocomplete for range options

```go
// Common-range suggestions for IV. Customize per option (CP, level use different scales).
var ivSuggestions = []string{"90", "95", "98", "100", "50-100", "75-99", "0-50"}

func IVAutocomplete(focused string) []*discordgo.ApplicationCommandOptionChoice {
    focused = strings.TrimSpace(focused)
    if focused == "" {
        return toChoices(ivSuggestions)
    }
    // If user is typing, show their value as the first option (so they can submit)
    // plus filtered suggestions
    out := []*discordgo.ApplicationCommandOptionChoice{
        {Name: focused, Value: focused},
    }
    for _, s := range ivSuggestions {
        if strings.HasPrefix(s, focused) {
            out = append(out, &discordgo.ApplicationCommandOptionChoice{Name: s, Value: s})
        }
    }
    return out
}
```

## Revised first commands

Replace Phase 1 in the rollout:

### Phase 1: `/version` (pipeline smoke test)
- No DB. No auth. Returns a single Reply with the binary version + git SHA.
- Validates: dispatcher routing, context build (minimal), reply rendering, registration sync.

### Phase 2: `/tracked` (first real command)
- Registered user only. DB read. Multi-Reply (long output → SplitMessage).
- Validates: auth check, language-aware response, `Reply.IsDM` handling for in-channel vs DM split.

`/poracle` moves to **Phase 3** or later, where it gets handled with its own special registration-channel validation rather than being the smoke test.

---

## Non-Goals

- Not migrating users off text commands. Both surfaces coexist.
- Not adding new command verbs that text doesn't have (parity first, exclusives later).
- Not changing the `bot.Command` interface.
- Not building a Discord modal-based input UX (24-option Discord cap is enough for v1).
- Not building a Telegram inline-keyboard equivalent (separate consideration).
- Not adding slash commands to the API config editor (post-v1).

---

## Out-of-band Notes

- Memory anchor: see `MEMORY.md` → `project_slash_commands_plan` for revision history of this draft.
