# Discord Slash Commands — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add Discord slash commands to PoracleNG as an optional, additive surface that reuses the existing text-command framework. Text commands keep working unchanged.

**Architecture:** Slash handlers receive `discordgo.InteractionCreate` events, build CommandContext, map Discord options → text tokens, and dispatch through the existing `bot.Command.Run(ctx, tokens)` of each registered command. The slash facade is a translation layer over the same business logic the text bot uses, so permission checks, validation, DB writes, and reply construction all run unchanged. `RecentActivity` is a 6h TTL multi-map populated from webhook ingestion and consumed by autocomplete handlers to prioritise currently-active entities.

**Tech Stack:** Go 1.21+, `bwmarrin/discordgo` (already in deps), existing `bot.Registry` and `bot.CommandContext`, existing `i18n.Bundle`, `rowtext.Generator`, `store.HumanStore`/`store.TrackingStores`, MySQL, TOML config.

**Companion document:** `project_slash_commands_plan.md` holds the design decisions, debate-points, and rationale that produced this plan. Read it first for the "why"; this document covers the "how."

**Scope:** 18 slash commands (`/version`, `/tracked`, `/help`, `/info`, `/language`, `/track`, `/raid`, `/egg`, `/quest`, `/invasion`, `/lure`, `/nest`, `/maxbattle`, `/gym`, `/fort`, `/untrack`, `/area`, `/profile`, `/location`), plus autocomplete (pokemon names, templates, user-state), plus localization, plus the RecentActivity tracker.

---

## File Structure

### New files

```
processor/internal/tracker/recent_activity.go
processor/internal/tracker/recent_activity_test.go

processor/internal/discordbot/slash/
  dispatcher.go              # InteractionCreate routing (ApplicationCommand + Autocomplete)
  dispatcher_test.go
  registration.go            # SyncCommands + fingerprint cache
  registration_test.go
  fingerprint.go             # Deterministic command-set hashing
  fingerprint_test.go
  context.go                 # Build CommandContext from interaction
  context_test.go
  reply.go                   # []Reply → InteractionResponse/Followup
  reply_test.go
  errors.go                  # MapperError type + translation glue
  errors_test.go
  definitions.go             # Per-command ApplicationCommand builders
  definitions_test.go        # Snapshot tests against testdata/*.json
  localization.go            # poracleToDiscord map + localizationsForKey
  localization_test.go
  testdata/                  # snapshot golden files for option C tests
    version.json
    tracked.json
    track.json
    raid.json
    ...

processor/internal/discordbot/slash/mappers/
  version.go                 # always returns []
  tracked.go                 # always returns []
  help.go
  info.go
  language.go
  track.go
  raid.go
  egg.go
  quest.go                   # mutual-exclusion validation
  invasion.go
  lure.go
  nest.go
  maxbattle.go
  gym.go
  fort.go
  untrack.go
  area.go                    # subcommand: add/remove/show
  profile.go                 # subcommand: list/change/create/delete
  location.go
  common.go                  # shared helpers (rangeToken, flattenOptions, splitCSV)
  *_test.go                  # one test file per mapper

processor/internal/discordbot/slash/autocomplete/
  registry.go                # UserStateLister + registry
  pokemon.go                 # pokemon name autocomplete (game data + RecentActivity)
  template.go                # template name autocomplete
  area.go                    # configured areas (subset visible per user)
  range.go                   # iv suggestions (e.g. "100", "95", "0-0")
  raid_boss.go               # raid pokemon names + level keywords + RecentActivity
  quest_reward.go            # type-specific reward autocompletes (pokemon/item/candy/mega)
  invasion_grunt.go          # grunt names + RecentActivity
  userstate.go               # AutocompleteUserState shared helper
  listers/
    tracking.go              # list user's tracking by subtype
    areas.go                 # list user's enabled areas
    profiles.go              # list user's profiles
  *_test.go

processor/internal/bot/testdata/
  parity.yaml                # slash↔text fixture (Phase 7)
processor/internal/bot/parity_test.go
processor/internal/bot/coverage_test.go
```

### Modified files

```
processor/internal/config/config.go
  + DiscordSlashCommands struct, [discord.slash_commands] block

processor/internal/bot/command.go
  + BotDeps.RecentActivity *tracker.RecentActivity field

processor/cmd/processor/main.go
  + Construct *tracker.RecentActivity
  + Pass to ProcessorService (existing) and BotDeps
  + Construct slash.Dispatcher when [discord.slash_commands].enabled
  + Wire SyncCommands call post-session.Open()
  + CLI flag -sync-slash-commands

processor/cmd/processor/raid.go, quest.go, maxbattle.go, invasion.go
  + After successful match, call tracker.RecentActivity.Record* with the entity ID

processor/internal/discordbot/interaction.go
  + Branch ic.Type: ApplicationCommand / Autocomplete → slash dispatcher;
    Component → existing handler unchanged
```

---

## Phase 0 — Foundation

Builds the RecentActivity tracker and slash dispatcher skeleton. Zero user-visible behaviour change; just establishes plumbing.

### Task 1: Add `tracker.RecentActivity` struct

**Files:**
- Create: `processor/internal/tracker/recent_activity.go`
- Create: `processor/internal/tracker/recent_activity_test.go`

- [ ] **Step 1: Write the failing test**

```go
// processor/internal/tracker/recent_activity_test.go
package tracker

import (
    "testing"
    "time"
)

func TestRecentActivityRaidBosses(t *testing.T) {
    r := NewRecentActivity()
    r.RecordRaidBoss(150)
    r.RecordRaidBoss(151)
    r.RecordRaidBoss(150)  // duplicate; should update timestamp

    active := r.ActiveRaidBosses()
    if len(active) != 2 {
        t.Fatalf("want 2 active bosses, got %d", len(active))
    }
}

func TestRecentActivityExpiry(t *testing.T) {
    r := NewRecentActivity()
    r.now = func() time.Time { return time.Unix(1000, 0) }
    r.RecordRaidBoss(150)
    r.now = func() time.Time { return time.Unix(1000 + int64(7*time.Hour/time.Second), 0) }

    active := r.ActiveRaidBosses()
    if len(active) != 0 {
        t.Fatalf("want 0 after TTL expiry, got %d", len(active))
    }
}

func TestRecentActivityZeroIgnored(t *testing.T) {
    r := NewRecentActivity()
    r.RecordRaidBoss(0)
    if len(r.ActiveRaidBosses()) != 0 {
        t.Fatal("zero ID should not be recorded")
    }
}

func TestRecentActivityRaceSafe(t *testing.T) {
    r := NewRecentActivity()
    done := make(chan struct{})
    go func() {
        for i := 1; i <= 1000; i++ { r.RecordRaidBoss(i) }
        close(done)
    }()
    for i := 0; i < 100; i++ { _ = r.ActiveRaidBosses() }
    <-done
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd processor && go test ./internal/tracker/ -run RecentActivity -v`
Expected: FAIL with "undefined: NewRecentActivity"

- [ ] **Step 3: Write minimal implementation**

```go
// processor/internal/tracker/recent_activity.go
package tracker

import (
    "sync"
    "time"
)

const recentActivityTTL = 6 * time.Hour

type RecentActivity struct {
    mu              sync.Mutex
    raidBosses      map[int]time.Time
    maxBattleBosses map[int]time.Time
    questPokemon    map[int]time.Time
    questItems      map[int]time.Time
    questCandy      map[int]time.Time
    questMega       map[int]time.Time
    questXL         map[int]time.Time
    invasionGrunts  map[int]time.Time
    now             func() time.Time
}

func NewRecentActivity() *RecentActivity {
    return &RecentActivity{
        raidBosses:      make(map[int]time.Time),
        maxBattleBosses: make(map[int]time.Time),
        questPokemon:    make(map[int]time.Time),
        questItems:      make(map[int]time.Time),
        questCandy:      make(map[int]time.Time),
        questMega:       make(map[int]time.Time),
        questXL:         make(map[int]time.Time),
        invasionGrunts:  make(map[int]time.Time),
        now:             time.Now,
    }
}

func (r *RecentActivity) RecordRaidBoss(id int)      { r.record(r.raidBosses, id) }
func (r *RecentActivity) RecordMaxBattleBoss(id int) { r.record(r.maxBattleBosses, id) }
func (r *RecentActivity) RecordQuestPokemon(id int)  { r.record(r.questPokemon, id) }
func (r *RecentActivity) RecordQuestItem(id int)     { r.record(r.questItems, id) }
func (r *RecentActivity) RecordQuestCandy(id int)    { r.record(r.questCandy, id) }
func (r *RecentActivity) RecordQuestMega(id int)     { r.record(r.questMega, id) }
func (r *RecentActivity) RecordQuestXL(id int)       { r.record(r.questXL, id) }
func (r *RecentActivity) RecordInvasionGrunt(id int) { r.record(r.invasionGrunts, id) }

func (r *RecentActivity) ActiveRaidBosses() []int      { return r.active(r.raidBosses) }
func (r *RecentActivity) ActiveMaxBattleBosses() []int { return r.active(r.maxBattleBosses) }
func (r *RecentActivity) ActiveQuestPokemon() []int    { return r.active(r.questPokemon) }
func (r *RecentActivity) ActiveQuestItems() []int      { return r.active(r.questItems) }
func (r *RecentActivity) ActiveQuestCandy() []int      { return r.active(r.questCandy) }
func (r *RecentActivity) ActiveQuestMega() []int       { return r.active(r.questMega) }
func (r *RecentActivity) ActiveQuestXL() []int         { return r.active(r.questXL) }
func (r *RecentActivity) ActiveInvasionGrunts() []int  { return r.active(r.invasionGrunts) }

func (r *RecentActivity) record(m map[int]time.Time, id int) {
    if id <= 0 { return }
    r.mu.Lock()
    defer r.mu.Unlock()
    m[id] = r.now()
}

func (r *RecentActivity) active(m map[int]time.Time) []int {
    r.mu.Lock()
    defer r.mu.Unlock()
    cutoff := r.now().Add(-recentActivityTTL)
    ids := make([]int, 0, len(m))
    for id, ts := range m {
        if ts.Before(cutoff) {
            delete(m, id)
            continue
        }
        ids = append(ids, id)
    }
    return ids
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd processor && go test ./internal/tracker/ -run RecentActivity -v`
Expected: PASS (4 tests)

- [ ] **Step 5: Commit**

```bash
git add processor/internal/tracker/recent_activity.go processor/internal/tracker/recent_activity_test.go
git commit -m "tracker: add RecentActivity for slash command autocomplete"
```

---

### Task 2: Wire RecentActivity into `BotDeps` and webhook handlers

**Files:**
- Modify: `processor/internal/bot/command.go` (add field to `BotDeps`)
- Modify: `processor/cmd/processor/main.go` (construct + pass)
- Modify: `processor/cmd/processor/raid.go`, `quest.go`, `maxbattle.go`, `invasion.go` (record entities)

- [ ] **Step 1: Write the failing test**

```go
// processor/cmd/processor/raid_test.go — add to existing file
func TestRaidRecordsRecentActivity(t *testing.T) {
    ra := tracker.NewRecentActivity()
    ps := newTestProcessorService(t, withRecentActivity(ra))

    err := ps.ProcessRaid(testRaidWebhook(t, 150))  // Mewtwo
    require.NoError(t, err)

    active := ra.ActiveRaidBosses()
    require.Equal(t, []int{150}, active)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd processor && go test ./cmd/processor/ -run TestRaidRecordsRecentActivity -v`
Expected: FAIL with `ProcessorService` missing `recentActivity` field or `withRecentActivity` undefined.

- [ ] **Step 3: Add field to BotDeps and ProcessorService**

```go
// processor/internal/bot/command.go — add to BotDeps struct
type BotDeps struct {
    // ... existing fields ...

    // RecentActivity tracks recently-seen pokemon/items/grunts (6h TTL)
    // used by slash autocomplete to prioritise currently-active entities.
    // Always non-nil; populated by webhook handlers post-dedup.
    RecentActivity *tracker.RecentActivity
}

// processor/cmd/processor/main.go — in ProcessorService struct
type ProcessorService struct {
    // ... existing fields ...
    recentActivity *tracker.RecentActivity
}

// In main.go startup:
recentActivity := tracker.NewRecentActivity()
botDeps.RecentActivity = recentActivity
ps.recentActivity = recentActivity
```

Then add the record calls:

```go
// processor/cmd/processor/raid.go — after successful dedup
if ps.recentActivity != nil {
    ps.recentActivity.RecordRaidBoss(raid.PokemonID)
}

// processor/cmd/processor/quest.go — after rewards parsing
for _, reward := range parsedRewards {
    switch reward.Type {
    case QuestRewardPokemon: ps.recentActivity.RecordQuestPokemon(reward.PokemonID)
    case QuestRewardItem:    ps.recentActivity.RecordQuestItem(reward.ItemID)
    case QuestRewardCandy:   ps.recentActivity.RecordQuestCandy(reward.PokemonID)
    case QuestRewardMega:    ps.recentActivity.RecordQuestMega(reward.PokemonID)
    case QuestRewardXL:      ps.recentActivity.RecordQuestXL(reward.PokemonID)
    }
}

// processor/cmd/processor/maxbattle.go — after dedup
if ps.recentActivity != nil {
    ps.recentActivity.RecordMaxBattleBoss(mb.PokemonID)
}

// processor/cmd/processor/invasion.go — after dedup
if ps.recentActivity != nil {
    ps.recentActivity.RecordInvasionGrunt(invasion.GruntType)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd processor && go test ./cmd/processor/ -run TestRaidRecordsRecentActivity -v`
Expected: PASS. Also run `go test ./...` to ensure no regressions.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/bot/command.go processor/cmd/processor/*.go
git commit -m "processor: wire RecentActivity into webhook handlers"
```

---

### Task 3: Add `[discord.slash_commands]` config block

**Files:**
- Modify: `processor/internal/config/config.go`
- Create: `processor/internal/config/slash_test.go`

- [ ] **Step 1: Write the failing test**

```go
// processor/internal/config/slash_test.go
package config

import (
    "strings"
    "testing"
)

func TestParseSlashCommandsConfig(t *testing.T) {
    raw := `
[discord.slash_commands]
enabled = true
register_globally = false
guilds = ["111", "222"]
sync_on_startup = true
enable = ["track", "raid", "tracked", "version"]
`
    cfg, err := LoadFromReader(strings.NewReader(raw))
    if err != nil { t.Fatal(err) }

    sc := cfg.Discord.SlashCommands
    if !sc.Enabled { t.Error("Enabled should be true") }
    if sc.RegisterGlobally { t.Error("RegisterGlobally should be false") }
    if len(sc.Guilds) != 2 { t.Errorf("Guilds: %v", sc.Guilds) }
    if len(sc.Enable) != 4 { t.Errorf("Enable: %v", sc.Enable) }
}

func TestSlashCommandsDefaults(t *testing.T) {
    cfg, _ := LoadFromReader(strings.NewReader(""))
    sc := cfg.Discord.SlashCommands
    if sc.Enabled { t.Error("should default disabled (master switch off)") }
    if !sc.RegisterGlobally { t.Error("should default global") }
    if !sc.SyncOnStartup { t.Error("should default sync on") }
    if len(sc.Enable) != 0 { t.Error("Enable should default to empty (meaning all)") }
}

func TestIsSlashCommandEnabledEmptyMeansAll(t *testing.T) {
    sc := DiscordSlashCommands{}  // empty Enable
    if !sc.IsEnabled("track") { t.Error("empty Enable should enable everything") }
    if !sc.IsEnabled("gym") { t.Error("empty Enable should enable everything") }
}

func TestIsSlashCommandEnabledExplicitSubset(t *testing.T) {
    sc := DiscordSlashCommands{Enable: []string{"track", "raid"}}
    if !sc.IsEnabled("track") { t.Error("track should be enabled") }
    if sc.IsEnabled("gym") { t.Error("gym should not be enabled when subset restricts") }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd processor && go test ./internal/config/ -run SlashCommands -v`
Expected: FAIL — `SlashCommands` field undefined.

- [ ] **Step 3: Add struct and defaults**

```go
// processor/internal/config/config.go — add to Discord struct
type Discord struct {
    // ... existing fields ...
    SlashCommands DiscordSlashCommands `toml:"slash_commands"`
}

type DiscordSlashCommands struct {
    Enabled          bool     `toml:"enabled"`
    RegisterGlobally bool     `toml:"register_globally"`
    Guilds           []string `toml:"guilds"`
    SyncOnStartup    bool     `toml:"sync_on_startup"`
    // Enable optionally restricts which slash commands register. Empty/nil =
    // all commands this build supports. Set explicitly only when the operator
    // wants to limit the surface to a subset. Use the master `Enabled = false`
    // flag to turn slash off entirely.
    Enable           []string `toml:"enable"`
}

// IsEnabled returns true when the given short slash name should be registered.
// Empty Enable list means "all enabled".
func (s DiscordSlashCommands) IsEnabled(name string) bool {
    if len(s.Enable) == 0 { return true }
    for _, n := range s.Enable {
        if n == name { return true }
    }
    return false
}

// In applyDefaults() — extend existing function:
// SyncOnStartup defaults true (only if not explicitly set; otherwise leave alone).
// RegisterGlobally defaults true.
// Enabled defaults false (master switch).
// Enable defaults nil (no commands — explicit opt-in).
```

The TOML loader's existing default-handling code should be examined to determine whether `RegisterGlobally=true` and `SyncOnStartup=true` need an explicit "if absent" check vs structural defaults. Match the existing pattern used by other config blocks.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd processor && go test ./internal/config/ -run SlashCommands -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/config/config.go processor/internal/config/slash_test.go
git commit -m "config: add [discord.slash_commands] block"
```

---

### Task 4: Slash dispatcher skeleton

**Files:**
- Create: `processor/internal/discordbot/slash/dispatcher.go`
- Create: `processor/internal/discordbot/slash/dispatcher_test.go`

- [ ] **Step 1: Write the failing test**

```go
// processor/internal/discordbot/slash/dispatcher_test.go
package slash

import "testing"

func TestNewDispatcherStoresConfig(t *testing.T) {
    cfg := Config{Enabled: true, Global: true}
    d := NewDispatcher(cfg)
    if !d.cfg.Enabled { t.Error("cfg.Enabled lost") }
}

func TestHandleCommandSkipsWhenNoCommand(t *testing.T) {
    d := NewDispatcher(Config{})
    // No registration; HandleCommand should return without panic
    d.HandleCommand(nil, nil)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd processor && go test ./internal/discordbot/slash/ -v`
Expected: FAIL — package doesn't exist.

- [ ] **Step 3: Create skeleton**

```go
// processor/internal/discordbot/slash/dispatcher.go
package slash

import (
    "github.com/bwmarrin/discordgo"

    "github.com/pokemon/poracleng/processor/internal/bot"
    "github.com/pokemon/poracleng/processor/internal/config"
    "github.com/pokemon/poracleng/processor/internal/i18n"
)

type Config struct {
    Enabled bool
    Global  bool
    Guilds  []string
    // Enable lists short command names this installation registers (e.g. "track").
    // Empty = nothing registered. Maps 1:1 from config's [discord.slash_commands] enable.
    Enable  []string
    // Optional override paths for testing
    CachePath string
    ForceSync bool
}

type Dispatcher struct {
    cfg      Config
    session  *discordgo.Session       // set by Attach()
    appID    string                   // set after session.Open()
    deps     *bot.BotDeps
    registry *bot.Registry
    bundle   *i18n.Bundle
    cfgRoot  *config.Config
}

func NewDispatcher(cfg Config) *Dispatcher {
    return &Dispatcher{cfg: cfg}
}

func (d *Dispatcher) Attach(s *discordgo.Session, deps *bot.BotDeps, registry *bot.Registry, bundle *i18n.Bundle, cfg *config.Config) {
    d.session = s
    d.deps = deps
    d.registry = registry
    d.bundle = bundle
    d.cfgRoot = cfg
}

// HandleCommand routes an ApplicationCommand interaction. No-op skeleton
// for Phase 0; subsequent tasks fill the body.
func (d *Dispatcher) HandleCommand(s *discordgo.Session, ic *discordgo.InteractionCreate) {
    if d == nil || ic == nil {
        return
    }
    // TODO: Task 11 — implement dispatch routing
}

// HandleAutocomplete routes an ApplicationCommandAutocomplete interaction.
// No-op skeleton for Phase 0.
func (d *Dispatcher) HandleAutocomplete(s *discordgo.Session, ic *discordgo.InteractionCreate) {
    if d == nil || ic == nil {
        return
    }
    // TODO: Task 28 — implement autocomplete routing
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd processor && go test ./internal/discordbot/slash/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/discordbot/slash/
git commit -m "discordbot/slash: dispatcher skeleton"
```

---

### Task 5: Wire dispatcher into Discord bot startup

**Files:**
- Modify: `processor/internal/discordbot/bot.go`
- Modify: `processor/internal/discordbot/interaction.go`
- Modify: `processor/cmd/processor/main.go`

- [ ] **Step 1: Write the failing test**

```go
// processor/internal/discordbot/slash/dispatcher_test.go — add
func TestAttachStoresSessionAndDeps(t *testing.T) {
    d := NewDispatcher(Config{Enabled: true})
    s := &discordgo.Session{}
    deps := &bot.BotDeps{}
    reg := &bot.Registry{}
    bundle := &i18n.Bundle{}
    cfg := &config.Config{}

    d.Attach(s, deps, reg, bundle, cfg)

    if d.session != s { t.Error("session not stored") }
    if d.deps != deps { t.Error("deps not stored") }
}
```

- [ ] **Step 2: Run test to verify it fails**

Already passes from Task 4. This step exists to confirm the existing `Attach` is correct.

Run: `cd processor && go test ./internal/discordbot/slash/ -v`
Expected: PASS.

- [ ] **Step 3: Wire into bot.go and main.go**

```go
// processor/internal/discordbot/bot.go — add field
type Bot struct {
    // ... existing fields ...
    slash *slash.Dispatcher  // nil when [discord.slash_commands].enabled is false
}

// In NewBot or wherever Bot is constructed:
if cfg.Discord.SlashCommands.Enabled {
    b.slash = slash.NewDispatcher(slash.Config{
        Enabled: true,
        Global:  cfg.Discord.SlashCommands.RegisterGlobally,
        Guilds:  cfg.Discord.SlashCommands.Guilds,
        Enable:  cfg.Discord.SlashCommands.Enable,
    })
}

// After session creation, before session.Open():
if b.slash != nil {
    b.slash.Attach(b.session, &b.deps, b.deps.Registry, b.deps.Translations, b.cfg)
}
```

```go
// processor/internal/discordbot/interaction.go — extend onInteractionCreate
func (b *Bot) onInteractionCreate(s *discordgo.Session, ic *discordgo.InteractionCreate) {
    switch ic.Type {
    case discordgo.InteractionApplicationCommand:
        if b.slash != nil { b.slash.HandleCommand(s, ic) }
    case discordgo.InteractionApplicationCommandAutocomplete:
        if b.slash != nil { b.slash.HandleAutocomplete(s, ic) }
    case discordgo.InteractionMessageComponent:
        b.handleComponent(s, ic)  // existing handler — name may vary
    }
}
```

Verify the existing button-handler function name in `interaction.go` and adjust.

- [ ] **Step 4: Verify the build**

Run: `cd processor && go build ./...`
Expected: clean build.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/discordbot/
git commit -m "discordbot: wire slash dispatcher behind config flag"
```

---

## Phase 1 — /version smoke test

End-to-end pipeline validation. After this phase, `/version` works against a real Discord guild.

### Task 6: Implement `reply.Send` for plain text

**Files:**
- Create: `processor/internal/discordbot/slash/reply.go`
- Create: `processor/internal/discordbot/slash/reply_test.go`

- [ ] **Step 1: Write the failing test**

```go
// processor/internal/discordbot/slash/reply_test.go
package slash

import (
    "strings"
    "testing"

    "github.com/bwmarrin/discordgo"
    "github.com/pokemon/poracleng/processor/internal/bot"
)

func TestRenderInitialIsAlwaysEphemeral(t *testing.T) {
    // All slash replies are ephemeral. Reply.IsDM=true triggers DM, not non-ephemeral.
    payload := renderInitial(bot.Reply{Text: "hello"})
    if payload.Content != "hello" { t.Errorf("content=%q", payload.Content) }
    if payload.Flags != discordgo.MessageFlagsEphemeral { t.Error("expected ephemeral") }
}

func TestRenderRepliesLongTextSplits(t *testing.T) {
    long := strings.Repeat("x", 2500)
    chunks := splitReplyText(long)
    if len(chunks) < 2 { t.Errorf("expected >1 chunk, got %d", len(chunks)) }
    for _, c := range chunks {
        if len(c) > 2000 { t.Errorf("chunk too long: %d", len(c)) }
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd processor && go test ./internal/discordbot/slash/ -run Render -v`
Expected: FAIL — `renderInitial` undefined.

- [ ] **Step 3: Implement reply.go**

Add a Reply.IsDM-handling test FIRST:

```go
// reply_test.go — add
func TestSendDMReplyOpensDMChannel(t *testing.T) {
    s := newFakeSession(withDMChannelID("dm-99"))
    ic := buildInteraction("user-99", "")
    err := Send(s.Session(), ic, []bot.Reply{{Text: "secret", IsDM: true}}, false)
    if err != nil { t.Fatal(err) }
    // Two API calls: DM send + ephemeral confirmation in channel
    if !s.didSendToChannel("dm-99") { t.Error("DM not sent") }
    if !s.didEditInteractionWithEphemeral() { t.Error("missing ephemeral confirmation") }
}
```

```go
// processor/internal/discordbot/slash/reply.go
package slash

import (
    "fmt"

    "github.com/bwmarrin/discordgo"

    "github.com/pokemon/poracleng/processor/internal/bot"
)

// Send dispatches a Reply slice to Discord. Pre: interaction has been deferred.
// All slash responses are ephemeral. Reply.IsDM=true triggers a real DM send
// (persistent in user's DM history) plus an ephemeral confirmation in-channel.
// First non-DM reply → InteractionResponseEdit. Subsequent → FollowupCreate.
func Send(s *discordgo.Session, ic *discordgo.InteractionCreate, replies []bot.Reply) error {
    firstInteractionUsed := false
    for _, r := range replies {
        if r.IsDM {
            if err := sendToDM(s, ic, r); err != nil { return err }
            if !firstInteractionUsed {
                confirm := "✅ Sent to DM"
                if _, err := s.InteractionResponseEdit(ic.Interaction, &discordgo.WebhookEdit{
                    Content: &confirm,
                }); err != nil { return err }
                firstInteractionUsed = true
            }
            continue
        }
        chunks := splitReplyText(r.Text)
        for _, chunk := range chunks {
            data := renderInitial(bot.Reply{Text: chunk, Embed: r.Embed, ImageURL: r.ImageURL})
            if !firstInteractionUsed {
                if _, err := s.InteractionResponseEdit(ic.Interaction, &discordgo.WebhookEdit{
                    Content: &data.Content,
                }); err != nil {
                    return err
                }
                firstInteractionUsed = true
            } else {
                if _, err := s.FollowupMessageCreate(ic.Interaction, true, &discordgo.WebhookParams{
                    Content: data.Content,
                    Flags:   discordgo.MessageFlagsEphemeral,
                }); err != nil {
                    return err
                }
            }
        }
    }
    return nil
}

// renderInitial builds the content payload for one Reply.
// Flags is always ephemeral — there are no public slash responses.
func renderInitial(r bot.Reply) *discordgo.InteractionResponseData {
    return &discordgo.InteractionResponseData{
        Content: r.Text,
        Flags:   discordgo.MessageFlagsEphemeral,
    }
}

// sendToDM opens (or reuses) the user's DM channel and sends the reply.
func sendToDM(s *discordgo.Session, ic *discordgo.InteractionCreate, r bot.Reply) error {
    userID := rawUserID(ic)
    if userID == "" { return fmt.Errorf("cannot resolve user for DM") }
    ch, err := s.UserChannelCreate(userID)
    if err != nil { return fmt.Errorf("open DM channel: %w", err) }
    _, err = s.ChannelMessageSendComplex(ch.ID, &discordgo.MessageSend{
        Content: r.Text,
    })
    return err
}

// rawUserID returns the bare Discord user ID (no "discord:user:" prefix).
func rawUserID(ic *discordgo.InteractionCreate) string {
    if ic.Member != nil && ic.Member.User != nil { return ic.Member.User.ID }
    if ic.User != nil { return ic.User.ID }
    return ""
}

func splitReplyText(text string) []string { return bot.SplitMessage(text, 2000) }

// (renderInitial is defined above in the main Send implementation — always ephemeral.)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd processor && go test ./internal/discordbot/slash/ -run Render -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/discordbot/slash/reply.go processor/internal/discordbot/slash/reply_test.go
git commit -m "discordbot/slash: render plain-text replies"
```

---

### Task 7: Build `/version` slash command definition

**Files:**
- Create: `processor/internal/discordbot/slash/definitions.go`
- Create: `processor/internal/discordbot/slash/definitions_test.go`

- [ ] **Step 1: Write the failing test**

```go
// processor/internal/discordbot/slash/definitions_test.go
package slash

import (
    "encoding/json"
    "os"
    "testing"
)

func TestVersionDefinition(t *testing.T) {
    bundle := testBundle(t)  // bundle with slash.cmd.version="version", slash.desc.version="Show Poracle version"
    def := buildDefinition(bundle, "cmd.version", "version", nil)
    if def.Name != "version" { t.Errorf("name=%q", def.Name) }
    if len(def.Options) != 0 { t.Errorf("expected 0 options, got %d", len(def.Options)) }
}

func TestVersionDefinitionRenamedByI18n(t *testing.T) {
    // Operator override (config/custom.en.json) renames /version → /poracle-version
    bundle := testBundle(t, withOverride("en", "slash.cmd.version", "poracle-version"))
    def := buildDefinition(bundle, "cmd.version", "version", nil)
    if def.Name != "poracle-version" { t.Errorf("expected renamed, got %q", def.Name) }
}

func TestSnapshotVersion(t *testing.T) {
    bundle := testBundle(t)
    def := buildDefinition(bundle, "cmd.version", "version", nil)
    got, _ := json.MarshalIndent(def, "", "  ")
    want, _ := os.ReadFile("testdata/version.json")
    if string(got) != string(want) {
        t.Errorf("snapshot drift:\nwant:\n%s\ngot:\n%s", want, got)
    }
}
```

Create `processor/internal/discordbot/slash/testdata/version.json`:

```json
{
  "name": "version",
  "description": "Show Poracle version",
  "options": null
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd processor && go test ./internal/discordbot/slash/ -run Version -v`
Expected: FAIL — `buildDefinition` undefined.

- [ ] **Step 3: Implement definitions.go**

```go
// processor/internal/discordbot/slash/definitions.go
package slash

import (
    "github.com/bwmarrin/discordgo"
)

// buildDefinition is the shared constructor for ApplicationCommand defs.
// Name + Description + their localizations are all sourced from the i18n
// bundle. canonShortName is the canonical English short name used for
// programmatic lookup (enable list, command routing) — does NOT change with
// localization.
func buildDefinition(
    bundle *i18n.Bundle,
    key string,                 // e.g. "cmd.track"
    canonShortName string,      // e.g. "track" — used for routing/enable
    options []*discordgo.ApplicationCommandOption,
) *discordgo.ApplicationCommand {
    return &discordgo.ApplicationCommand{
        Name:                     resolveSlashName(bundle, key, canonShortName),
        NameLocalizations:        slashNameLocalizations(bundle, key),
        Description:              slashDescription(bundle, key),
        DescriptionLocalizations: slashDescriptionLocalizations(bundle, key),
        Options:                  options,
    }
}

// resolveSlashName returns the English (primary) slash name from the i18n
// bundle's "slash.cmd.<short>" key, falling back to the canonical short name.
// Warning logged if the English key is missing.
func resolveSlashName(bundle *i18n.Bundle, key, canonShortName string) string {
    slashKey := "slash." + key  // "cmd.track" → "slash.cmd.track"
    en := bundle.For("en")
    if en == nil {
        log.Warnf("slash: English bundle missing; using canonical %q for %s", canonShortName, slashKey)
        return canonShortName
    }
    val := en.T(slashKey)
    if val == "" || val == slashKey {
        log.Warnf("slash: missing English %s; falling back to canonical %q", slashKey, canonShortName)
        return canonShortName
    }
    if !validSlashName(val) {
        log.Warnf("slash: English %s = %q fails Discord name regex; using canonical %q", slashKey, val, canonShortName)
        return canonShortName
    }
    return val
}

// slashDescription returns the English description from "slash.desc.<short>".
func slashDescription(bundle *i18n.Bundle, key string) string {
    short := strings.TrimPrefix(key, "cmd.")
    descKey := "slash.desc." + short
    en := bundle.For("en")
    if en == nil { return "" }
    val := en.T(descKey)
    if val == "" || val == descKey {
        log.Warnf("slash: missing English description %s", descKey)
        return ""
    }
    return val
}

// slashNameLocalizations / slashDescriptionLocalizations: walk loaded
// languages (not all Discord locales), look up "slash.cmd.<short>" /
// "slash.desc.<short>" per language, return only entries that exist
// and pass validSlashName for the name path.
//
// Implementation reuses localizationsForKey (Task 43) but with the
// slash.* key namespace.

// AllDefinitions returns the slash command set this build supports, filtered
// by the operator's [discord.slash_commands] enable subset. Empty enable
// means "all commands this build supports". Exported for use by the
// coverage meta-test (Task 48).
//
// The `enable` list always uses canonical English short names ("track",
// "raid", ...) regardless of i18n renaming — so an operator's enable
// config stays valid across language changes.
func AllDefinitions(bundle *i18n.Bundle, enable []string) []*discordgo.ApplicationCommand {
    allEnabled := len(enable) == 0
    enableSet := map[string]bool{}
    for _, n := range enable { enableSet[n] = true }

    defs := make([]*discordgo.ApplicationCommand, 0, len(allCommandKeys()))
    for _, key := range allCommandKeys() {
        canon := canonShortName(key)
        if !allEnabled && !enableSet[canon] { continue }
        def := buildCommandDef(bundle, key, canon)
        if def == nil { continue }
        defs = append(defs, def)
    }
    return defs
}

func buildCommandDef(bundle *i18n.Bundle, key, canon string) *discordgo.ApplicationCommand {
    switch key {
    case "cmd.version":
        return buildDefinition(bundle, key, canon, nil)
    // ... other commands added in later tasks
    }
    return nil
}

// allCommandKeys lists every slash-command key this build supports.
// Used by AllDefinitions to walk and build the registered set, filtered by config.Enable.
func allCommandKeys() []string {
    return []string{
        "cmd.version",  // Phase 1
        // Phase 2:
        "cmd.tracked", "cmd.help", "cmd.info", "cmd.language",
        // Phase 4:
        "cmd.track", "cmd.raid", "cmd.egg", "cmd.quest", "cmd.invasion",
        "cmd.lure", "cmd.nest", "cmd.maxbattle", "cmd.gym", "cmd.fort",
        "cmd.untrack",
        // Phase 5:
        "cmd.area", "cmd.profile", "cmd.location",
    }
}

// canonShortName returns the canonical English short name for a command key.
// Always the canonical name — never the i18n-localized variant.
// Used for the enable allow-list and for slash dispatch routing.
func canonShortName(key string) string {
    return strings.TrimPrefix(key, "cmd.")
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd processor && go test ./internal/discordbot/slash/ -run Version -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add processor/internal/discordbot/slash/definitions.go processor/internal/discordbot/slash/definitions_test.go processor/internal/discordbot/slash/testdata/version.json
git commit -m "discordbot/slash: /version definition + snapshot test"
```

---

### Task 8: Build `/version` mapper

**Files:**
- Create: `processor/internal/discordbot/slash/mappers/version.go`
- Create: `processor/internal/discordbot/slash/mappers/version_test.go`

- [ ] **Step 1: Write the failing test**

```go
// processor/internal/discordbot/slash/mappers/version_test.go
package mappers

import "testing"

func TestVersionMapper(t *testing.T) {
    tokens, err := Version(nil)
    if err != nil { t.Fatal(err) }
    if len(tokens) != 0 { t.Errorf("expected empty, got %v", tokens) }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd processor && go test ./internal/discordbot/slash/mappers/ -v`
Expected: FAIL — `Version` undefined.

- [ ] **Step 3: Implement**

```go
// processor/internal/discordbot/slash/mappers/version.go
package mappers

import "github.com/bwmarrin/discordgo"

// Version maps /version options to text tokens.
// /version has no options; always returns an empty token slice.
func Version(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
    return nil, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd processor && go test ./internal/discordbot/slash/mappers/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/discordbot/slash/mappers/
git commit -m "discordbot/slash/mappers: /version mapper"
```

---

### Task 9: Mapper lookup registry

**Files:**
- Modify: `processor/internal/discordbot/slash/mappers/common.go` (create)

- [ ] **Step 1: Write the failing test**

```go
// processor/internal/discordbot/slash/mappers/registry_test.go
package mappers

import "testing"

func TestLookupVersion(t *testing.T) {
    fn := Lookup("version")
    if fn == nil { t.Fatal("nil mapper for /version") }
    tokens, _ := fn(nil)
    if len(tokens) != 0 { t.Error("version should return empty") }
}

func TestLookupUnknownReturnsNil(t *testing.T) {
    if Lookup("does-not-exist") != nil { t.Error("expected nil for unknown") }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd processor && go test ./internal/discordbot/slash/mappers/ -run Lookup -v`
Expected: FAIL — `Lookup` undefined.

- [ ] **Step 3: Implement**

```go
// processor/internal/discordbot/slash/mappers/registry.go
package mappers

import "github.com/bwmarrin/discordgo"

// Mapper transforms slash command options into text-command tokens.
// Returns ([]string, nil) on success or (nil, *MapperError) on validation failure.
type Mapper func(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error)

// MapperError is a typed mapper error carrying an i18n key + format args.
// The dispatcher translates this to the user's language before sending.
type MapperError struct {
    Key  string
    Args []any
}

func (e *MapperError) Error() string { return e.Key }

// Lookup returns the mapper registered for a given slash command name.
// Returns nil for unknown commands.
func Lookup(commandName string) Mapper {
    return registry[commandName]
}

// registry maps slash command name → mapper. Populated in init() per file.
var registry = map[string]Mapper{
    "version": Version,
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd processor && go test ./internal/discordbot/slash/mappers/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/discordbot/slash/mappers/registry.go processor/internal/discordbot/slash/mappers/registry_test.go
git commit -m "discordbot/slash/mappers: lookup registry"
```

---

### Task 10: Implement Sync (no fingerprint yet)

**Files:**
- Create: `processor/internal/discordbot/slash/registration.go`
- Create: `processor/internal/discordbot/slash/registration_test.go`

- [ ] **Step 1: Write the failing test**

```go
// processor/internal/discordbot/slash/registration_test.go
package slash

import (
    "context"
    "testing"

    "github.com/bwmarrin/discordgo"
)

type fakeSession struct{ called []string }

func (f *fakeSession) ApplicationCommandBulkOverwrite(appID, guildID string, cmds []*discordgo.ApplicationCommand, opts ...discordgo.RequestOption) ([]*discordgo.ApplicationCommand, error) {
    f.called = append(f.called, guildID)
    return cmds, nil
}

func TestSyncGlobal(t *testing.T) {
    d := NewDispatcher(Config{Enabled: true, Global: true})
    d.appID = "app123"
    fs := &fakeSession{}
    d.commandsAPI = fs

    err := d.SyncCommands(context.Background())
    if err != nil { t.Fatal(err) }
    if len(fs.called) != 1 || fs.called[0] != "" {
        t.Errorf("expected global call, got %v", fs.called)
    }
}

func TestSyncGuilds(t *testing.T) {
    d := NewDispatcher(Config{Enabled: true, Global: false, Guilds: []string{"g1", "g2"}})
    d.appID = "app123"
    fs := &fakeSession{}
    d.commandsAPI = fs

    err := d.SyncCommands(context.Background())
    if err != nil { t.Fatal(err) }
    if len(fs.called) != 2 { t.Errorf("expected 2 guild calls, got %d", len(fs.called)) }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd processor && go test ./internal/discordbot/slash/ -run Sync -v`
Expected: FAIL — `SyncCommands` and `commandsAPI` undefined.

- [ ] **Step 3: Implement**

```go
// processor/internal/discordbot/slash/registration.go
package slash

import (
    "context"
    "fmt"

    "github.com/bwmarrin/discordgo"
)

// commandsAPI is the subset of *discordgo.Session SyncCommands uses,
// extracted into an interface for testability.
type commandsAPI interface {
    ApplicationCommandBulkOverwrite(appID, guildID string, cmds []*discordgo.ApplicationCommand, opts ...discordgo.RequestOption) ([]*discordgo.ApplicationCommand, error)
}

// SyncCommands pushes the current command set to Discord.
// Phase 1 omits the fingerprint cache; Task 12 adds it.
func (d *Dispatcher) SyncCommands(ctx context.Context) error {
    intent := AllDefinitions(d.bundle, d.cfg.Enable)
    api := d.commandsAPI
    if api == nil { api = d.session }

    if d.cfg.Global {
        if _, err := api.ApplicationCommandBulkOverwrite(d.appID, "", intent); err != nil {
            return fmt.Errorf("global slash sync: %w", err)
        }
        return nil
    }

    var lastErr error
    for _, gid := range d.cfg.Guilds {
        if _, err := api.ApplicationCommandBulkOverwrite(d.appID, gid, intent); err != nil {
            lastErr = fmt.Errorf("guild %s slash sync: %w", gid, err)
            continue
        }
    }
    return lastErr
}
```

Update `Dispatcher` struct in `dispatcher.go`:

```go
type Dispatcher struct {
    // ... existing fields ...
    commandsAPI commandsAPI // test seam; nil = use session
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd processor && go test ./internal/discordbot/slash/ -run Sync -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Commit**

```bash
git add processor/internal/discordbot/slash/registration.go processor/internal/discordbot/slash/registration_test.go processor/internal/discordbot/slash/dispatcher.go
git commit -m "discordbot/slash: SyncCommands (no fingerprint yet)"
```

---

### Task 11: Implement HandleCommand for /version

**Files:**
- Modify: `processor/internal/discordbot/slash/dispatcher.go`
- Modify: `processor/internal/discordbot/slash/dispatcher_test.go`
- Create: `processor/internal/discordbot/slash/context.go`

- [ ] **Step 1: Write the failing test**

```go
// processor/internal/discordbot/slash/dispatcher_test.go — add
func TestHandleCommandRoutesVersion(t *testing.T) {
    // Build deps with a fake registry exposing cmd.version
    reg := bot.NewRegistry()
    reg.Register(&fakeVersion{})

    d := NewDispatcher(Config{Enabled: true})
    fs := &fakeSession{}
    d.session = (*discordgo.Session)(nil) // not used after Attach
    d.responder = &fakeResponder{}
    d.registry = reg

    ic := buildVersionInteraction("user-1")
    d.HandleCommand(d.session, ic)

    if !fs.responded { t.Error("expected interaction response") }
}

// fakeVersion is a minimal Command stub.
type fakeVersion struct{}
func (f *fakeVersion) Name() string                                   { return "cmd.version" }
func (f *fakeVersion) Aliases() []string                              { return nil }
func (f *fakeVersion) Run(ctx *bot.CommandContext, args []string) []bot.Reply {
    return []bot.Reply{{Text: "PoracleNG dev"}}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd processor && go test ./internal/discordbot/slash/ -run HandleCommandRoutesVersion -v`
Expected: FAIL — body is still TODO.

- [ ] **Step 3: Implement dispatch**

```go
// processor/internal/discordbot/slash/dispatcher.go — replace TODO body
import (
    // ... existing imports ...
    "github.com/pokemon/poracleng/processor/internal/discordbot/slash/mappers"
)

// commandsSkippingRegistration matches the text bot's special-case logic
// for !version (and historically !poracle, which isn't surfaced in slash).
var commandsSkippingRegistration = map[string]bool{
    "cmd.version": true,
}

func (d *Dispatcher) HandleCommand(s *discordgo.Session, ic *discordgo.InteractionCreate) {
    if d == nil || ic == nil { return }

    // 1. Defer ephemerally (within 3-second deadline). All slash responses are
    //    ephemeral; only Reply.IsDM=true triggers an actual DM in addition.
    if err := s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
        Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
        Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
    }); err != nil {
        // Defer failed — most likely 3s deadline already passed. Log and abort.
        return
    }

    // 2. Resolve command key. The invoked name may be the operator-renamed
    //    or i18n-localized form (e.g. "verfolge") — look it up via the
    //    name-to-key map built at startup from the slash.cmd.* i18n keys.
    invoked := ic.ApplicationCommandData().Name
    cmdKey := d.resolveCommandKey(invoked)
    if cmdKey == "" || d.registry.Lookup(cmdKey) == nil {
        d.respondError(s, ic, "🛑 Unknown command.")
        return
    }
    canon := canonShortName(cmdKey)  // for mapper lookup, which uses canonical names

    // 3. Disabled-command check (shared text+slash mechanism). A command in
    //    [general] disabled_commands is rejected here even if /slash is enabled.
    if bot.IsCommandDisabled(d.cfgRoot.General.DisabledCommands, cmdKey) {
        d.respondError(s, ic, "🛑 This command is disabled by the operator.")
        return
    }

    // 4. Build context
    ctx, err := d.buildContext(ic, cmdKey)
    if err != nil {
        d.respondError(s, ic, fmt.Sprintf("🛑 %s", err.Error()))
        return
    }

    // 5. Registration check (skipped for /version)
    if !commandsSkippingRegistration[cmdKey] && ctx.Humans_Record == nil {
        d.respondError(s, ic, registrationErrorText(d.cfgRoot, d.bundle, ctx.Language, ic.GuildID))
        return
    }

    // 6. command_security check (text+slash shared config)
    if !d.commandAllowed(ic, cmdKey, ctx.IsAdmin) {
        d.respondError(s, ic, fmt.Sprintf("🛑 You don't have permission to run /%s.", invoked))
        return
    }

    // 7. Mapper — looked up by canonical short name (mapper registry is
    //    independent of i18n renaming)
    mapperFn := mappers.Lookup(canon)
    if mapperFn == nil {
        d.respondError(s, ic, "🛑 Command not implemented.")
        return
    }
    tokens, err := mapperFn(ic.ApplicationCommandData().Options)
    if err != nil {
        d.respondError(s, ic, formatMapperError(err, ctx.Language, d.bundle))
        return
    }

    // 8. Dispatch
    cmd := d.registry.Lookup(cmdKey)
    replies := cmd.Run(ctx, tokens)

    // 9. Send (all responses ephemeral)
    if err := Send(s, ic, replies); err != nil {
        // Log only; can't tell the user
    }
}

// formatMapperError translates a MapperError to the user's language with a 🛑 prefix.
// Non-MapperError errors fall back to "🛑 <err.Error()>".
func formatMapperError(err error, lang string, bundle *i18n.Bundle) string {
    if me, ok := err.(*mappers.MapperError); ok {
        tr := bundle.For(lang)
        if tr == nil { tr = bundle.For("en") }
        return "🛑 " + tr.Tf(me.Key, me.Args...)
    }
    return "🛑 " + err.Error()
}

// resolveCommandKey converts an invoked slash name → "cmd.<short>".
// The invoked name may be the localized form ("verfolge") or an operator-
// renamed form ("alerts"). We walk the registered set, resolve each command
// key's English slash name from the bundle, and match. Cached at startup
// to avoid bundle lookups per interaction.
func (d *Dispatcher) resolveCommandKey(invokedName string) string {
    if key, ok := d.nameToKey[invokedName]; ok { return key }
    return ""
}

// buildNameMap is called once after sync. Maps the resolved English slash
// name → canonical command key. Run-time invocations come back as that
// English name (Discord sends the primary Name, not localizations).
func (d *Dispatcher) buildNameMap() {
    d.nameToKey = map[string]string{}
    for _, key := range allCommandKeys() {
        canon := canonShortName(key)
        name := resolveSlashName(d.bundle, key, canon)
        d.nameToKey[name] = key
    }
}

// registrationErrorText composes the registration-prompt error including
// a registration-channel hint when configured for the user's guild.
func registrationErrorText(cfg *config.Config, guildID string) string {
    // Phase 1: hardcoded; Phase 6 i18n
    return "🛑 You need to register first. DM me with !poracle, or run !poracle in a registration channel on this server."
}

// respondError edits the deferred reply with an ephemeral error message.
func (d *Dispatcher) respondError(s *discordgo.Session, ic *discordgo.InteractionCreate, msg string) {
    _, _ = s.InteractionResponseEdit(ic.Interaction, &discordgo.WebhookEdit{
        Content: &msg,
    })
}
```

Create context.go (minimal Phase 1 version):

```go
// processor/internal/discordbot/slash/context.go
package slash

import (
    "github.com/bwmarrin/discordgo"

    "github.com/pokemon/poracleng/processor/internal/bot"
)

// buildContext builds a CommandContext from an interaction.
// Phase 1: minimum viable — userID, language, target=sender.
// Phase 2 (Task 14) expands with human lookup, permissions, language resolution.
func (d *Dispatcher) buildContext(ic *discordgo.InteractionCreate, cmdKey string) (*bot.CommandContext, error) {
    userID := interactionUserID(ic)
    userName := interactionUserName(ic)
    return &bot.CommandContext{
        UserID:        userID,
        UserName:      userName,
        Platform:      "discord",
        ChannelID:     ic.ChannelID,
        GuildID:       ic.GuildID,
        IsDM:          ic.GuildID == "",
        TargetID:      userID,
        TargetName:    userName,
        TargetType:    bot.TypeDiscordUser,
        Language:      d.cfgRoot.General.Locale,
        Config:        d.cfgRoot,
        Translations:  d.bundle,
        // Phase 2 adds Humans, Tracking, etc. for now leave zero — /version doesn't need them
    }, nil
}

func interactionUserID(ic *discordgo.InteractionCreate) string {
    if ic.Member != nil && ic.Member.User != nil {
        return "discord:user:" + ic.Member.User.ID
    }
    if ic.User != nil {
        return "discord:user:" + ic.User.ID
    }
    return ""
}

func interactionUserName(ic *discordgo.InteractionCreate) string {
    if ic.Member != nil && ic.Member.User != nil { return ic.Member.User.Username }
    if ic.User != nil { return ic.User.Username }
    return ""
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd processor && go test ./internal/discordbot/slash/ -run HandleCommandRoutesVersion -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/discordbot/slash/dispatcher.go processor/internal/discordbot/slash/context.go processor/internal/discordbot/slash/dispatcher_test.go
git commit -m "discordbot/slash: dispatch /version end-to-end"
```

---

### Task 12: Fingerprint cache for idempotent sync

**Files:**
- Create: `processor/internal/discordbot/slash/fingerprint.go`
- Create: `processor/internal/discordbot/slash/fingerprint_test.go`
- Modify: `processor/internal/discordbot/slash/registration.go`

- [ ] **Step 1: Write the failing test**

```go
// processor/internal/discordbot/slash/fingerprint_test.go
package slash

import (
    "testing"

    "github.com/bwmarrin/discordgo"
)

func TestFingerprintStable(t *testing.T) {
    cmds := []*discordgo.ApplicationCommand{
        {Name: "version", Description: "Show version"},
    }
    fp1 := Fingerprint(cmds)
    fp2 := Fingerprint(cmds)
    if fp1 != fp2 { t.Errorf("unstable: %s != %s", fp1, fp2) }
    if len(fp1) != 16 { t.Errorf("len(fp)=%d, want 16", len(fp1)) }
}

func TestFingerprintChangeDetected(t *testing.T) {
    a := []*discordgo.ApplicationCommand{{Name: "version", Description: "Show version"}}
    b := []*discordgo.ApplicationCommand{{Name: "version", Description: "Show V2"}}
    if Fingerprint(a) == Fingerprint(b) { t.Error("change not detected") }
}

func TestFingerprintIgnoresOrder(t *testing.T) {
    a := []*discordgo.ApplicationCommand{
        {Name: "a", Description: "A"},
        {Name: "b", Description: "B"},
    }
    b := []*discordgo.ApplicationCommand{
        {Name: "b", Description: "B"},
        {Name: "a", Description: "A"},
    }
    if Fingerprint(a) != Fingerprint(b) { t.Error("should ignore order") }
}

func TestFingerprintCacheRoundtrip(t *testing.T) {
    dir := t.TempDir()
    c := &Cache{Path: filepath.Join(dir, "fp.json")}

    c.Global = CacheEntry{Fingerprint: "abc", SyncedAt: time.Now()}
    if err := c.Save(); err != nil { t.Fatal(err) }

    var loaded Cache
    loaded.Path = c.Path
    if err := loaded.Load(); err != nil { t.Fatal(err) }
    if loaded.Global.Fingerprint != "abc" { t.Errorf("got %q", loaded.Global.Fingerprint) }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd processor && go test ./internal/discordbot/slash/ -run Fingerprint -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

```go
// processor/internal/discordbot/slash/fingerprint.go
package slash

import (
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "os"
    "sort"
    "time"

    "github.com/bwmarrin/discordgo"
)

// Fingerprint returns a 16-char hex hash of the command set. Order-insensitive
// (commands are sorted by name before hashing). Includes localizations.
func Fingerprint(cmds []*discordgo.ApplicationCommand) string {
    sorted := make([]*discordgo.ApplicationCommand, len(cmds))
    copy(sorted, cmds)
    sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })

    h := sha256.New()
    enc := json.NewEncoder(h)
    enc.SetEscapeHTML(false)
    for _, c := range sorted {
        _ = enc.Encode(c)
    }
    return hex.EncodeToString(h.Sum(nil))[:16]
}

type Cache struct {
    Path   string                 `json:"-"`
    Global CacheEntry             `json:"global"`
    Guilds map[string]CacheEntry  `json:"guilds"`
}

type CacheEntry struct {
    Fingerprint string    `json:"fingerprint"`
    SyncedAt    time.Time `json:"synced_at"`
}

func (c *Cache) Load() error {
    if c.Guilds == nil { c.Guilds = map[string]CacheEntry{} }
    data, err := os.ReadFile(c.Path)
    if os.IsNotExist(err) { return nil }
    if err != nil { return err }
    return json.Unmarshal(data, c)
}

func (c *Cache) Save() error {
    data, err := json.MarshalIndent(c, "", "  ")
    if err != nil { return err }
    return os.WriteFile(c.Path, data, 0o644)
}
```

Update SyncCommands to use the cache:

```go
// processor/internal/discordbot/slash/registration.go — replace SyncCommands
func (d *Dispatcher) SyncCommands(ctx context.Context) error {
    intent := AllDefinitions(d.bundle, d.cfg.Enable)
    want := Fingerprint(intent)
    api := d.commandsAPI
    if api == nil { api = d.session }

    cache := &Cache{Path: d.cfg.CachePath}
    _ = cache.Load()

    if d.cfg.Global {
        if cache.Global.Fingerprint == want && !d.cfg.ForceSync {
            return nil
        }
        if _, err := api.ApplicationCommandBulkOverwrite(d.appID, "", intent); err != nil {
            return fmt.Errorf("global slash sync: %w", err)
        }
        cache.Global = CacheEntry{Fingerprint: want, SyncedAt: time.Now()}
        return cache.Save()
    }

    for _, gid := range d.cfg.Guilds {
        if cache.Guilds[gid].Fingerprint == want && !d.cfg.ForceSync {
            continue
        }
        if _, err := api.ApplicationCommandBulkOverwrite(d.appID, gid, intent); err != nil {
            // Log and continue with other guilds
            continue
        }
        cache.Guilds[gid] = CacheEntry{Fingerprint: want, SyncedAt: time.Now()}
    }
    return cache.Save()
}
```

Add `CachePath` and `ForceSync` to `Config` struct.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd processor && go test ./internal/discordbot/slash/ -v`
Expected: PASS (all tests).

- [ ] **Step 5: Commit**

```bash
git add processor/internal/discordbot/slash/fingerprint.go processor/internal/discordbot/slash/fingerprint_test.go processor/internal/discordbot/slash/registration.go processor/internal/discordbot/slash/dispatcher.go
git commit -m "discordbot/slash: fingerprint-cached idempotent sync"
```

---

### Task 13: Manual test — deploy and invoke /version

**Files:** none

- [ ] **Step 1: Build and run against a test Discord guild**

```bash
cd processor && go build -o poracle-processor ./cmd/processor && ./poracle-processor -basedir ..
```

In `config/config.toml`:

```toml
[discord.slash_commands]
enabled = true
register_globally = false
guilds = ["<your test guild ID>"]
sync_on_startup = true
```

Restart the processor.

- [ ] **Step 2: Verify slash command appears in test guild**

In Discord, type `/` in the test guild. Confirm `/version` appears.

- [ ] **Step 3: Invoke `/version`**

Submit `/version`. Confirm:
- Bot responds within 3 seconds.
- Response is ephemeral.
- Body matches the `!version` text command output ("PoracleNG dev (commit, date)").

- [ ] **Step 4: Restart and verify cache works**

Restart the processor. Check log: should NOT see "POSTing slash commands" — fingerprint matches, sync skipped.

```bash
cat config/.cache/slash-fingerprint.json
```

Expect non-empty file with `global.fingerprint` (or `guilds.<gid>.fingerprint`).

- [ ] **Step 5: Commit nothing**

Manual verification; no code change. Mark Phase 1 complete.

---

## Phase 2 — Read-only commands

`/tracked`, `/help`, `/info`, `/language` (show-only). Builds out CommandContext fully, permission checks, and the read-only command path.

### Task 14: Expand `buildContext` with full DB/permission lookup

**Files:**
- Modify: `processor/internal/discordbot/slash/context.go`
- Modify: `processor/internal/discordbot/slash/context_test.go` (create)

- [ ] **Step 1: Write the failing test**

```go
func TestBuildContextLanguageFromHuman(t *testing.T) {
    d := dispatcherWithFakeHumans(t, map[string]*store.HumanLite{
        "discord:user:42": {Language: "de"},
    })
    ic := buildInteraction("42", "")
    ctx, err := d.buildContext(ic, "cmd.tracked")
    if err != nil { t.Fatal(err) }
    if ctx.Language != "de" { t.Errorf("language=%q", ctx.Language) }
}

func TestBuildContextLanguageFallbackToLocale(t *testing.T) {
    d := dispatcherWithFakeHumans(t, nil)
    d.cfgRoot.General.Locale = "fr"
    ic := buildInteraction("99", "")
    ctx, _ := d.buildContext(ic, "cmd.tracked")
    if ctx.Language != "fr" { t.Errorf("language=%q", ctx.Language) }
}

func TestBuildContextLanguageFromDiscordLocale(t *testing.T) {
    d := dispatcherWithFakeHumans(t, nil)
    d.cfgRoot.General.Locale = "en"
    ic := buildInteraction("99", "")
    ic.Locale = discordgo.German
    ctx, _ := d.buildContext(ic, "cmd.tracked")
    if ctx.Language != "de" { t.Errorf("language=%q", ctx.Language) }
}

func TestBuildContextTargetAlwaysSender(t *testing.T) {
    d := dispatcherWithFakeHumans(t, map[string]*store.HumanLite{
        "discord:user:42": {ID: "discord:user:42", ProfileNo: 1},
    })
    ic := buildInteraction("42", "")
    ctx, _ := d.buildContext(ic, "cmd.tracked")
    if ctx.TargetID != "discord:user:42" { t.Errorf("target=%q", ctx.TargetID) }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd processor && go test ./internal/discordbot/slash/ -run BuildContext -v`
Expected: FAIL — current `buildContext` is minimal.

- [ ] **Step 3: Implement full buildContext**

```go
// processor/internal/discordbot/slash/context.go — replace buildContext
func (d *Dispatcher) buildContext(ic *discordgo.InteractionCreate, cmdKey string) (*bot.CommandContext, error) {
    userID := interactionUserID(ic)
    userName := interactionUserName(ic)

    // 1. Load human (may be nil for unregistered)
    human, _ := d.deps.Humans.GetLite(context.Background(), userID)

    // 2. Resolve language
    lang := d.resolveLanguage(ic, human)

    // 3. Resolve permissions
    isAdmin := slices.Contains(d.cfgRoot.Discord.Admins, ic.Member.User.ID)

    // 4. Build context
    ctx := &bot.CommandContext{
        UserID:       userID,
        UserName:     userName,
        Platform:     "discord",
        ChannelID:    ic.ChannelID,
        GuildID:      ic.GuildID,
        IsDM:         ic.GuildID == "",
        IsAdmin:      isAdmin,
        Language:     lang,
        Config:       d.cfgRoot,
        Translations: d.bundle,
        DB:           d.deps.DB,
        Humans:       d.deps.Humans,
        Tracking:     d.deps.Tracking,
        StateMgr:     d.deps.StateMgr,
        GameData:     d.deps.GameData,
        Geocoder:     d.deps.Geocoder,
        StaticMap:    d.deps.StaticMap,
        Weather:      d.deps.Weather,
        Stats:        d.deps.Stats,
        DTS:          d.deps.DTS,
        Emoji:        d.deps.Emoji,
        ArgMatcher:   d.deps.ArgMatcher,
        Resolver:     d.deps.Resolver,
        RowText:      d.deps.RowText,
        Registry:     d.registry,
        ReloadFunc:   d.deps.ReloadFunc,

        TargetID:   userID,
        TargetName: userName,
        TargetType: bot.TypeDiscordUser,

        Humans_Record: human,  // attached as Human field below
    }
    if human != nil {
        ctx.ProfileNo = human.ProfileNo
        ctx.HasLocation = human.Latitude != 0 || human.Longitude != 0
        ctx.HasArea = len(human.Area) > 0
    }
    return ctx, nil
}

// resolveLanguage applies the chain: human.Language → Discord locale → cfg.General.Locale.
func (d *Dispatcher) resolveLanguage(ic *discordgo.InteractionCreate, human *store.HumanLite) string {
    if human != nil && human.Language != "" { return human.Language }
    if pCode, ok := discordLocaleToPoracle[ic.Locale]; ok { return pCode }
    return d.cfgRoot.General.Locale
}

// discordLocaleToPoracle — minimal mapping for Phase 2; Task 53 expands.
var discordLocaleToPoracle = map[discordgo.Locale]string{
    discordgo.German:    "de",
    discordgo.French:    "fr",
    discordgo.SpanishES: "es",
    discordgo.Italian:   "it",
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd processor && go test ./internal/discordbot/slash/ -run BuildContext -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add processor/internal/discordbot/slash/context.go processor/internal/discordbot/slash/context_test.go
git commit -m "discordbot/slash: full CommandContext build with language resolution"
```

---

### Task 15: Add command_security check to dispatch

**Files:**
- Modify: `processor/internal/discordbot/slash/dispatcher.go`
- Add tests for command_security gate

- [ ] **Step 1: Write the failing test**

```go
func TestDispatchBlockedByCommandSecurity(t *testing.T) {
    d := dispatcherWithSecurity(t, map[string][]string{
        "role-tracker": {"cmd.tracked"},
    })
    // User has NO role-tracker role
    ic := buildInteraction("42", "")
    d.HandleCommand(d.session, ic)

    if !contains(d.responder.lastMessage(), "permission") {
        t.Errorf("expected permission error, got %q", d.responder.lastMessage())
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd processor && go test ./internal/discordbot/slash/ -run BlockedByCommandSecurity -v`
Expected: FAIL — no permission check yet.

- [ ] **Step 3: Implement permission check**

```go
// In HandleCommand, after buildContext, before mapper lookup:
if !d.commandAllowed(ic, cmdKey, ctx.IsAdmin) {
    d.respondError(s, ic, fmt.Sprintf("🛑 You don't have permission to run /%s.", invoked))
    return
}

// New helper using existing bot.CommandAllowed function (reused from text bot):
func (d *Dispatcher) commandAllowed(ic *discordgo.InteractionCreate, cmdKey string, isAdmin bool) bool {
    if isAdmin { return true }
    roles := userRoles(ic)
    return bot.CommandAllowed(d.cfgRoot, cmdKey, roles)
}

func userRoles(ic *discordgo.InteractionCreate) []string {
    if ic.Member == nil { return nil }
    return ic.Member.Roles
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd processor && go test ./internal/discordbot/slash/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/discordbot/slash/dispatcher.go processor/internal/discordbot/slash/dispatcher_test.go
git commit -m "discordbot/slash: command_security gate"
```

---

### Task 16: `/tracked` slash command

**Files:**
- Modify: `processor/internal/discordbot/slash/definitions.go` (add `/tracked` registration)
- Create: `processor/internal/discordbot/slash/mappers/tracked.go`
- Create: `processor/internal/discordbot/slash/mappers/tracked_test.go`
- Create: `processor/internal/discordbot/slash/testdata/tracked.json`

- [ ] **Step 1: Write the failing test**

```go
// processor/internal/discordbot/slash/mappers/tracked_test.go
package mappers

import "testing"

func TestTrackedMapper(t *testing.T) {
    tokens, err := Tracked(nil)
    if err != nil { t.Fatal(err) }
    if len(tokens) != 0 { t.Errorf("expected empty, got %v", tokens) }
}
```

```go
// definitions_test.go — add
func TestSnapshotTracked(t *testing.T) {
    assertSnapshot(t, "tracked", buildCommandDef("cmd.tracked", "tracked"))
}
```

Create `processor/internal/discordbot/slash/testdata/tracked.json`:

```json
{
  "name": "tracked",
  "description": "List your tracking rules",
  "options": null
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd processor && go test ./internal/discordbot/slash/ -run Tracked -v && go test ./internal/discordbot/slash/mappers/ -run Tracked -v`
Expected: FAIL — `Tracked` undefined, `cmd.tracked` switch case missing in `buildCommandDef`.

- [ ] **Step 3: Implement**

```go
// processor/internal/discordbot/slash/mappers/tracked.go
package mappers

import "github.com/bwmarrin/discordgo"

func Tracked(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
    return nil, nil
}

func init() { registry["tracked"] = Tracked }
```

```go
// definitions.go — extend buildCommandDef switch
func buildCommandDef(key, name string) *discordgo.ApplicationCommand {
    switch key {
    case "cmd.version", "cmd.tracked":
        return buildDefinition(key, name, nil, nil, nil)
    }
    return nil
}
```

`cmd.tracked` is already in `allCommandKeys()`. Adding it to the `buildCommandDef` switch makes it actually registerable when the operator includes `"tracked"` in `[discord.slash_commands] enable`.

- [ ] **Step 4: Run test to verify it passes**

Run: `cd processor && go test ./internal/discordbot/slash/... -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/discordbot/slash/mappers/tracked.go processor/internal/discordbot/slash/mappers/tracked_test.go processor/internal/discordbot/slash/definitions.go processor/internal/discordbot/slash/definitions_test.go processor/internal/discordbot/slash/testdata/tracked.json
git commit -m "discordbot/slash: /tracked command"
```

---

### Task 17-19: `/help`, `/info`, `/language` (show only)

Follow the same pattern as Task 16. Each is:
1. Mapper file with `Mapper` returning empty tokens (these commands take no options for show variants).
2. `init()` registration into `registry`.
3. Extension of `buildCommandDef` switch (the key is already in `allCommandKeys()`).
4. Snapshot golden file.
5. Test for the snapshot and mapper.

**/help** — one option `topic` (autocomplete, optional). Mapper passes through:

```go
func Help(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
    o := flattenOptions(opts)
    if v, ok := o["topic"]; ok && v.StringValue() != "" {
        return []string{strings.ToLower(v.StringValue())}, nil
    }
    return nil, nil
}
```

**/info** — no options. Mapper returns nil.

**/language** — one option `code` (Choice list of installed locales). For Phase 2 implement show-only by passing no tokens (the underlying `cmd.language` shows current when no args). Set command:

```go
func Language(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
    o := flattenOptions(opts)
    if v, ok := o["code"]; ok && v.StringValue() != "" {
        return []string{strings.ToLower(v.StringValue())}, nil
    }
    return nil, nil
}
```

Build the `code` choices from `d.bundle.LoadedLanguages()` at registration time:

```go
// Build choice list dynamically at registration time
func languageChoices(bundle *i18n.Bundle) []*discordgo.ApplicationCommandOptionChoice {
    langs := bundle.LoadedLanguages()
    sort.Strings(langs)
    out := make([]*discordgo.ApplicationCommandOptionChoice, 0, len(langs))
    for _, l := range langs {
        out = append(out, &discordgo.ApplicationCommandOptionChoice{Name: l, Value: l})
    }
    return out
}
```

Each command gets its own task per the same 5-step pattern. Combined commit: `discordbot/slash: /help /info /language commands`.

---

## Phase 3 — Autocomplete primitives + RecentActivity-driven suggestions

### Task 20: UserStateLister interface and registry

**Files:**
- Create: `processor/internal/discordbot/slash/autocomplete/userstate.go`
- Create: `processor/internal/discordbot/slash/autocomplete/userstate_test.go`

- [ ] **Step 1: Write the failing test**

```go
// processor/internal/discordbot/slash/autocomplete/userstate_test.go
package autocomplete

import (
    "context"
    "testing"
)

func TestRegisterAndLookup(t *testing.T) {
    r := NewRegistry()
    r.Register("tracking", func(ctx context.Context, deps *bot.BotDeps, userID string, hint UserStateHint) ([]Choice, error) {
        return []Choice{{Label: "test", Value: "1"}}, nil
    })
    fn := r.Lookup("tracking")
    if fn == nil { t.Fatal("nil lister") }
    out, _ := fn(context.Background(), nil, "u", UserStateHint{})
    if len(out) != 1 { t.Errorf("got %d choices", len(out)) }
}

func TestFilterAndCap(t *testing.T) {
    choices := []Choice{
        {Label: "Pikachu [id:1]", Value: "1"},
        {Label: "Bulbasaur [id:2]", Value: "2"},
    }
    got := FilterAndCap(choices, "pika")
    if len(got) != 1 { t.Errorf("expected 1 match, got %d", len(got)) }
}

func TestFilterAndCapTruncates(t *testing.T) {
    longLabel := strings.Repeat("x", 200) + " [id:99]"
    choices := []Choice{{Label: longLabel, Value: "99"}}
    got := FilterAndCap(choices, "")
    if len(got[0].Name) > 100 { t.Errorf("not truncated: %d", len(got[0].Name)) }
    if !strings.HasSuffix(got[0].Name, "[id:99]") { t.Errorf("suffix lost: %q", got[0].Name) }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd processor && go test ./internal/discordbot/slash/autocomplete/ -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

```go
// processor/internal/discordbot/slash/autocomplete/userstate.go
package autocomplete

import (
    "context"
    "strings"

    "github.com/bwmarrin/discordgo"

    "github.com/pokemon/poracleng/processor/internal/bot"
)

type Choice struct {
    Label string
    Value string
}

type UserStateHint struct {
    Subtype string
    Focused string
}

type UserStateLister func(ctx context.Context, deps *bot.BotDeps, userID string, hint UserStateHint) ([]Choice, error)

type Registry struct {
    listers map[string]UserStateLister
}

func NewRegistry() *Registry { return &Registry{listers: map[string]UserStateLister{}} }
func (r *Registry) Register(name string, lister UserStateLister) { r.listers[name] = lister }
func (r *Registry) Lookup(name string) UserStateLister           { return r.listers[name] }

// FilterAndCap substring-filters by focused, caps at 25, truncates labels
// at 100 chars (preserving the suffix-anchored [id:N] / [#N] selectors).
func FilterAndCap(choices []Choice, focused string) []*discordgo.ApplicationCommandOptionChoice {
    focused = strings.ToLower(strings.TrimSpace(focused))
    out := make([]*discordgo.ApplicationCommandOptionChoice, 0, 25)
    for _, c := range choices {
        if focused != "" && !strings.Contains(strings.ToLower(c.Label), focused) {
            continue
        }
        label := c.Label
        if len(label) > 100 {
            label = "…" + label[len(label)-99:]
        }
        out = append(out, &discordgo.ApplicationCommandOptionChoice{Name: label, Value: c.Value})
        if len(out) == 25 { break }
    }
    return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd processor && go test ./internal/discordbot/slash/autocomplete/ -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add processor/internal/discordbot/slash/autocomplete/
git commit -m "discordbot/slash/autocomplete: UserStateLister + Registry primitive"
```

---

### Task 21: `listTracking` lister

**Files:**
- Create: `processor/internal/discordbot/slash/autocomplete/listers/tracking.go`
- Create: `processor/internal/discordbot/slash/autocomplete/listers/tracking_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestListTracking(t *testing.T) {
    deps := buildTestDeps(t, withTracking("raid", []TrackingRow{
        {UID: 12, PokemonID: 150 /* Mewtwo */, Team: 0},
    }))
    out, err := ListTracking(context.Background(), deps, "discord:user:42", UserStateHint{Subtype: "raid"})
    if err != nil { t.Fatal(err) }
    if len(out) != 1 { t.Fatalf("got %d", len(out)) }
    if !strings.Contains(out[0].Label, "Mewtwo") || !strings.HasSuffix(out[0].Label, "[id:12]") {
        t.Errorf("label=%q", out[0].Label)
    }
    if out[0].Value != "12" { t.Errorf("value=%q", out[0].Value) }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd processor && go test ./internal/discordbot/slash/autocomplete/listers/ -run ListTracking -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

```go
// processor/internal/discordbot/slash/autocomplete/listers/tracking.go
package listers

import (
    "context"
    "fmt"
    "strconv"

    "github.com/pokemon/poracleng/processor/internal/bot"
    "github.com/pokemon/poracleng/processor/internal/discordbot/slash/autocomplete"
)

// ListTracking enumerates the user's tracking rules of one subtype.
// Each result label is the rowtext description plus "[id:N]" suffix.
// Value is the UID as decimal string.
func ListTracking(ctx context.Context, deps *bot.BotDeps, userID string, hint autocomplete.UserStateHint) ([]autocomplete.Choice, error) {
    rules, err := deps.Tracking.ListForUser(ctx, hint.Subtype, userID)
    if err != nil { return nil, err }

    out := make([]autocomplete.Choice, 0, len(rules))
    for _, r := range rules {
        desc := deps.RowText.Describe(hint.Subtype, r)
        out = append(out, autocomplete.Choice{
            Label: fmt.Sprintf("%s [id:%d]", desc, r.UID),
            Value: strconv.FormatInt(r.UID, 10),
        })
    }
    return out, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd processor && go test ./internal/discordbot/slash/autocomplete/listers/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/discordbot/slash/autocomplete/listers/
git commit -m "slash/autocomplete/listers: ListTracking"
```

---

### Task 22-23: `listAreas` and `listProfiles`

Same pattern as Task 21. Each lister:
1. Pure function `func(ctx, deps, userID, hint) ([]Choice, error)`.
2. Queries the relevant store: `deps.Humans.GetLite(userID)` for areas, `deps.Humans.GetProfiles(userID)` for profiles.
3. Maps results to `Choice{Label, Value}`.

**ListAreas:**
```go
func ListAreas(ctx context.Context, deps *bot.BotDeps, userID string, hint autocomplete.UserStateHint) ([]autocomplete.Choice, error) {
    human, err := deps.Humans.GetLite(ctx, userID)
    if err != nil { return nil, err }
    if human == nil { return nil, nil }
    out := make([]autocomplete.Choice, 0, len(human.Area))
    for _, area := range human.Area {
        out = append(out, autocomplete.Choice{Label: area, Value: area})
    }
    return out, nil
}
```

**ListProfiles:**
```go
func ListProfiles(ctx context.Context, deps *bot.BotDeps, userID string, hint autocomplete.UserStateHint) ([]autocomplete.Choice, error) {
    profiles, err := deps.Humans.GetProfiles(ctx, userID)
    if err != nil { return nil, err }
    out := make([]autocomplete.Choice, 0, len(profiles))
    for _, p := range profiles {
        out = append(out, autocomplete.Choice{
            Label: fmt.Sprintf("%s [#%d]", p.Name, p.ProfileNo),
            Value: strconv.Itoa(p.ProfileNo),
        })
    }
    return out, nil
}
```

Both follow the 5-step TDD pattern. Commit: `slash/autocomplete/listers: ListAreas + ListProfiles`.

---

### Task 24: Pokemon name autocomplete

**Files:**
- Create: `processor/internal/discordbot/slash/autocomplete/pokemon.go`
- Create: `processor/internal/discordbot/slash/autocomplete/pokemon_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestPokemonAutocompleteExactMatch(t *testing.T) {
    deps := buildTestDeps(t)
    out := Pokemon(context.Background(), deps, "pikachu", "en")
    if len(out) == 0 { t.Fatal("expected matches") }
    if out[0].Value != "pikachu" { t.Errorf("expected pikachu, got %q", out[0].Value) }
}

func TestPokemonAutocompletePrefix(t *testing.T) {
    deps := buildTestDeps(t)
    out := Pokemon(context.Background(), deps, "char", "en")
    foundCharizard := false
    for _, c := range out {
        if c.Value == "charizard" { foundCharizard = true }
    }
    if !foundCharizard { t.Error("expected charizard in results") }
}

func TestPokemonAutocompleteCanonicalValue(t *testing.T) {
    deps := buildTestDeps(t)
    out := Pokemon(context.Background(), deps, "pi", "de")
    // Even with German labels, value is canonical English
    for _, c := range out {
        if strings.Contains(c.Value, "ä") || strings.Contains(c.Value, "ü") {
            t.Errorf("value should be canonical English, got %q", c.Value)
        }
    }
}

func TestPokemonAutocompleteEmptyFocused(t *testing.T) {
    deps := buildTestDeps(t)
    out := Pokemon(context.Background(), deps, "", "en")
    // Empty input — return nothing (RecentActivity-boosted lists handle this)
    if len(out) != 0 { t.Errorf("expected 0 for empty, got %d", len(out)) }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd processor && go test ./internal/discordbot/slash/autocomplete/ -run Pokemon -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

```go
// processor/internal/discordbot/slash/autocomplete/pokemon.go
package autocomplete

import (
    "context"
    "fmt"
    "sort"
    "strconv"
    "strings"

    "github.com/bwmarrin/discordgo"

    "github.com/pokemon/poracleng/processor/internal/bot"
)

// Pokemon returns autocomplete choices for a pokemon-name option.
// Empty focused returns nothing (caller handles RecentActivity boosting).
// Value is the canonical English lowercase name; label is the user-locale name.
func Pokemon(ctx context.Context, deps *bot.BotDeps, focused, userLang string) []*discordgo.ApplicationCommandOptionChoice {
    focused = strings.ToLower(strings.TrimSpace(focused))
    if focused == "" { return nil }

    type scored struct {
        canonical string
        label     string
        score     int
    }
    var results []scored

    tr := deps.Translations.For(userLang)
    for _, p := range deps.GameData.Pokemon {
        canonical := strings.ToLower(p.NameEnglish)
        label := canonical
        if tr != nil {
            if local := tr.T(fmt.Sprintf("poke_%d", p.ID)); local != "" {
                label = local
            }
        }
        score := scorePokemon(focused, canonical, strings.ToLower(label), p.ID)
        if score == 0 { continue }
        results = append(results, scored{canonical: canonical, label: label, score: score})
    }
    sort.Slice(results, func(i, j int) bool { return results[i].score > results[j].score })
    if len(results) > 25 { results = results[:25] }

    out := make([]*discordgo.ApplicationCommandOptionChoice, len(results))
    for i, r := range results {
        out[i] = &discordgo.ApplicationCommandOptionChoice{Name: r.label, Value: r.canonical}
    }
    return out
}

func scorePokemon(needle, eng, local string, id int) int {
    if needle == eng || needle == local { return 100 }
    if strings.HasPrefix(eng, needle) { return 50 }
    if strings.HasPrefix(local, needle) { return 40 }
    if strings.Contains(eng, needle) || strings.Contains(local, needle) { return 10 }
    if n, err := strconv.Atoi(needle); err == nil && n == id { return 5 }
    return 0
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd processor && go test ./internal/discordbot/slash/autocomplete/ -run Pokemon -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add processor/internal/discordbot/slash/autocomplete/pokemon.go processor/internal/discordbot/slash/autocomplete/pokemon_test.go
git commit -m "slash/autocomplete: pokemon name lookup"
```

---

### Task 25: Range-suggestion autocomplete (IV)

**Files:**
- Create: `processor/internal/discordbot/slash/autocomplete/range.go`
- Create: `processor/internal/discordbot/slash/autocomplete/range_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestRangeSuggestionsEmpty(t *testing.T) {
    out := IVRange("")
    expected := []string{"100", "95", "0-0"}
    if len(out) != len(expected) {
        t.Fatalf("got %d, want %d", len(out), len(expected))
    }
}

func TestRangeSuggestionsEchoesInput(t *testing.T) {
    out := IVRange("87")
    if out[0].Value != "87" { t.Errorf("expected echo first, got %q", out[0].Value) }
}

func TestRangeSuggestionsPrefixMatch(t *testing.T) {
    out := IVRange("9")
    foundNinetyFive := false
    for _, c := range out {
        if c.Value == "95" { foundNinetyFive = true }
    }
    if !foundNinetyFive { t.Error("expected 95 in prefix-9 results") }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd processor && go test ./internal/discordbot/slash/autocomplete/ -run Range -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

```go
// processor/internal/discordbot/slash/autocomplete/range.go
package autocomplete

import (
    "strings"

    "github.com/bwmarrin/discordgo"
)

var ivSuggestions = []string{"100", "95", "0-0"}

// IVRange returns autocomplete suggestions for the IV option.
// If focused is empty: return the suggestion list.
// Otherwise: echo focused as first option + filter suggestions by prefix.
func IVRange(focused string) []*discordgo.ApplicationCommandOptionChoice {
    focused = strings.TrimSpace(focused)
    var out []*discordgo.ApplicationCommandOptionChoice
    seen := map[string]bool{}
    if focused != "" {
        out = append(out, &discordgo.ApplicationCommandOptionChoice{Name: focused, Value: focused})
        seen[focused] = true
    }
    for _, s := range ivSuggestions {
        if seen[s] { continue }
        if focused != "" && !strings.HasPrefix(s, focused) { continue }
        out = append(out, &discordgo.ApplicationCommandOptionChoice{Name: s, Value: s})
        if len(out) == 25 { break }
    }
    return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd processor && go test ./internal/discordbot/slash/autocomplete/ -run Range -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/discordbot/slash/autocomplete/range.go processor/internal/discordbot/slash/autocomplete/range_test.go
git commit -m "slash/autocomplete: IV range suggestions"
```

---

### Task 26: Raid boss autocomplete (pokemon + level keywords + RecentActivity)

**Files:**
- Create: `processor/internal/discordbot/slash/autocomplete/raid_boss.go`
- Create: `processor/internal/discordbot/slash/autocomplete/raid_boss_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestRaidBossAutocompleteEmptyUsesRecentActivity(t *testing.T) {
    deps := buildTestDeps(t)
    deps.RecentActivity.RecordRaidBoss(150)  // Mewtwo
    deps.RecentActivity.RecordRaidBoss(151)  // Mew

    out := RaidBoss(context.Background(), deps, "", "en")
    if len(out) < 2 { t.Fatalf("expected ≥2 active bosses, got %d", len(out)) }
}

func TestRaidBossAutocompleteLevelKeyword(t *testing.T) {
    deps := buildTestDeps(t)
    out := RaidBoss(context.Background(), deps, "me", "en")
    foundMega := false
    for _, c := range out {
        if c.Value == "mega" { foundMega = true }
    }
    if !foundMega { t.Error("expected mega in 'me'-prefix results") }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd processor && go test ./internal/discordbot/slash/autocomplete/ -run RaidBoss -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

```go
// processor/internal/discordbot/slash/autocomplete/raid_boss.go
package autocomplete

import (
    "context"
    "strings"

    "github.com/bwmarrin/discordgo"

    "github.com/pokemon/poracleng/processor/internal/bot"
)

var raidLevelKeywords = []string{"1", "3", "5", "6", "mega", "legendary", "shadow", "ultra beast"}

func RaidBoss(ctx context.Context, deps *bot.BotDeps, focused, userLang string) []*discordgo.ApplicationCommandOptionChoice {
    focused = strings.ToLower(strings.TrimSpace(focused))
    var out []*discordgo.ApplicationCommandOptionChoice

    if focused == "" && deps.RecentActivity != nil {
        for _, id := range deps.RecentActivity.ActiveRaidBosses() {
            name := pokemonNameFor(deps, id, userLang)
            if name == "" { continue }
            out = append(out, &discordgo.ApplicationCommandOptionChoice{Name: name, Value: strings.ToLower(name)})
            if len(out) == 10 { break }  // leave room for keyword suggestions
        }
    }

    // Add matching level keywords
    for _, kw := range raidLevelKeywords {
        if focused != "" && !strings.Contains(kw, focused) { continue }
        out = append(out, &discordgo.ApplicationCommandOptionChoice{Name: kw, Value: kw})
        if len(out) == 25 { return out }
    }

    // Fall through to general pokemon autocomplete for the rest
    if focused != "" {
        for _, p := range Pokemon(ctx, deps, focused, userLang) {
            out = append(out, p)
            if len(out) == 25 { break }
        }
    }
    return out
}

func pokemonNameFor(deps *bot.BotDeps, id int, userLang string) string {
    p := deps.GameData.PokemonByID(id)
    if p == nil { return "" }
    if tr := deps.Translations.For(userLang); tr != nil {
        if name := tr.T(fmt.Sprintf("poke_%d", id)); name != "" { return name }
    }
    return p.NameEnglish
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd processor && go test ./internal/discordbot/slash/autocomplete/ -run RaidBoss -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/discordbot/slash/autocomplete/raid_boss.go processor/internal/discordbot/slash/autocomplete/raid_boss_test.go
git commit -m "slash/autocomplete: raid boss with RecentActivity + level keywords"
```

---

### Task 27: Template autocomplete

**Files:**
- Create: `processor/internal/discordbot/slash/autocomplete/template.go`
- Create: `processor/internal/discordbot/slash/autocomplete/template_test.go`

Pattern: read template names from `deps.DTS.AvailableTemplates(commandType, platform, language)`, filter by focused, return choices. Same 5-step TDD.

```go
func Template(ctx context.Context, deps *bot.BotDeps, focused, commandType, platform, userLang string) []*discordgo.ApplicationCommandOptionChoice {
    names := deps.DTS.AvailableTemplates(commandType, platform, userLang)
    return filterStringChoices(names, focused)
}
```

Commit: `slash/autocomplete: template names`.

---

### Task 28: Autocomplete dispatcher (HandleAutocomplete)

**Files:**
- Modify: `processor/internal/discordbot/slash/dispatcher.go`

- [ ] **Step 1: Write the failing test**

```go
func TestHandleAutocompleteRoutesToHandler(t *testing.T) {
    d := dispatcherWithAutocomplete(t)
    ic := buildAutocompleteInteraction("track", "pokemon", "pi")
    d.HandleAutocomplete(d.session, ic)

    choices := d.responder.lastChoices()
    found := false
    for _, c := range choices {
        if c.Value == "pikachu" { found = true }
    }
    if !found { t.Error("expected pikachu in choices") }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd processor && go test ./internal/discordbot/slash/ -run HandleAutocomplete -v`
Expected: FAIL — body still empty.

- [ ] **Step 3: Implement**

```go
// dispatcher.go — replace HandleAutocomplete body
func (d *Dispatcher) HandleAutocomplete(s *discordgo.Session, ic *discordgo.InteractionCreate) {
    if d == nil || ic == nil { return }

    data := ic.ApplicationCommandData()
    focused := focusedOption(data.Options)
    if focused == nil { return }

    cmdName := data.Name
    optName := focused.Name
    focusedValue := focused.StringValue()

    // Resolve user language for localized labels
    userID := interactionUserID(ic)
    human, _ := d.deps.Humans.GetLite(context.Background(), userID)
    userLang := d.resolveLanguage(ic, human)

    // Resolve cmdKey from invoked name to check skip-registration list
    cmdKey := d.resolveCommandKey(cmdName)

    // Unregistered user → return empty choices (don't mislead with active suggestions
    // they can't actually submit successfully)
    if human == nil && !commandsSkippingRegistration[cmdKey] {
        _ = s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
            Type: discordgo.InteractionApplicationCommandAutocompleteResult,
            Data: &discordgo.InteractionResponseData{Choices: nil},
        })
        return
    }

    // Route by (command, option) tuple
    choices := d.routeAutocomplete(cmdName, optName, focusedValue, userLang, ic)

    _ = s.InteractionRespond(ic.Interaction, &discordgo.InteractionResponse{
        Type: discordgo.InteractionApplicationCommandAutocompleteResult,
        Data: &discordgo.InteractionResponseData{Choices: choices},
    })
}

func (d *Dispatcher) routeAutocomplete(cmd, opt, focused, userLang string, ic *discordgo.InteractionCreate) []*discordgo.ApplicationCommandOptionChoice {
    switch {
    case opt == "pokemon":
        return autocomplete.Pokemon(context.Background(), d.deps, focused, userLang)
    case opt == "iv":
        return autocomplete.IVRange(focused)
    case opt == "boss" && cmd == "raid":
        return autocomplete.RaidBoss(context.Background(), d.deps, focused, userLang)
    case opt == "template":
        // commandType is the slash command name → DTS type lookup
        return autocomplete.Template(context.Background(), d.deps, focused, dtsTypeFor(cmd), "discord", userLang)
    case opt == "tracking" && cmd == "untrack":
        return d.userstateAutocomplete(ic, "tracking", typeFromOtherOption(ic, "type"), focused)
    case opt == "area":
        return d.userstateAutocomplete(ic, "areas", "", focused)
    case opt == "name" && cmd == "profile":
        return d.userstateAutocomplete(ic, "profiles", "", focused)
    }
    return nil
}

func (d *Dispatcher) userstateAutocomplete(ic *discordgo.InteractionCreate, listerName, subtype, focused string) []*discordgo.ApplicationCommandOptionChoice {
    lister := d.autocompleteRegistry.Lookup(listerName)
    if lister == nil { return nil }
    userID := interactionUserID(ic)
    out, err := lister(context.Background(), d.deps, userID, autocomplete.UserStateHint{Subtype: subtype, Focused: focused})
    if err != nil { return nil }
    return autocomplete.FilterAndCap(out, focused)
}

func focusedOption(opts []*discordgo.ApplicationCommandInteractionDataOption) *discordgo.ApplicationCommandInteractionDataOption {
    for _, o := range opts {
        if o.Focused { return o }
        if len(o.Options) > 0 {
            if sub := focusedOption(o.Options); sub != nil { return sub }
        }
    }
    return nil
}
```

Also wire up `d.autocompleteRegistry` in `NewDispatcher`:

```go
type Dispatcher struct {
    // ... existing fields ...
    autocompleteRegistry *autocomplete.Registry
}

func NewDispatcher(cfg Config) *Dispatcher {
    d := &Dispatcher{
        cfg: cfg,
        autocompleteRegistry: autocomplete.NewRegistry(),
    }
    d.autocompleteRegistry.Register("tracking", listers.ListTracking)
    d.autocompleteRegistry.Register("areas", listers.ListAreas)
    d.autocompleteRegistry.Register("profiles", listers.ListProfiles)
    return d
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd processor && go test ./internal/discordbot/slash/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/discordbot/slash/dispatcher.go processor/internal/discordbot/slash/dispatcher_test.go
git commit -m "discordbot/slash: HandleAutocomplete routing"
```

---

## Phase 4 — Mutating commands

All 11 mutating commands follow the same pattern. Per-command tasks:

1. Add the command to `buildCommandDef` switch (key already in `allCommandKeys()`).
2. Write the mapper.
3. Snapshot golden file.
4. Mapper unit tests.
5. Parity fixture entries (Phase 7).

### Pattern (use for every mutating command in this phase)

**Files per command:**
- Modify: `processor/internal/discordbot/slash/definitions.go` (add definition switch case)
- Create: `processor/internal/discordbot/slash/mappers/{cmd}.go`
- Create: `processor/internal/discordbot/slash/mappers/{cmd}_test.go`
- Create: `processor/internal/discordbot/slash/testdata/{cmd}.json`

**5-step TDD per command:**

- [ ] **Step 1**: Write the failing snapshot test + mapper unit tests covering:
  - Each option in isolation (token output for that option only).
  - Combined options (a representative full invocation).
  - Edge cases (empty optional values produce no token; range parses correctly).
  - Validation errors (where applicable, e.g. `/quest` mutual exclusion).

- [ ] **Step 2**: Run tests; expect FAIL.

- [ ] **Step 3**: Implement the mapper (see per-command spec below) + register in `registry`. Implement the definition in `buildCommandDef`.

- [ ] **Step 4**: Run tests; expect PASS. Also run `go test ./...` for full-tree regression check.

- [ ] **Step 5**: Commit each command separately: `discordbot/slash: /<command>`.

### Per-command specs

#### Task 29: `/track`

```go
// Definition options:
{String,  "pokemon",     Required, Autocomplete}   // pokemon ID as value; "Everything" entry
{String,  "iv",          Autocomplete}             // "100", "95", "0-0"
{Int,     "distance"}
{Int,     "great_rank"}
{Int,     "ultra_rank"}
{Int,     "little_rank"}
{Bool,    "clean"}
{String,  "template",    Autocomplete}
{String,  "form",        Autocomplete}             // cascades from selected pokemon
{String,  "size",        Choices: [all/xxs/xs/m/xl/xxl]}
```

**Pokemon autocomplete** (new, cascading-aware):
- Label: `"Pikachu (#25)"` (localized name + ID).
- Value: pokemon ID as decimal string (e.g. `"25"`), or literal `"everything"` for the special selector.
- The `"Everything"` choice is included only when `cfg.Tracking.EverythingFlagPermissions != "deny"` OR the user is an admin. Implementation reads `everything_flag_permissions` and the admin list.

**Form autocomplete (cascading)** — separate autocomplete handler:
- Reads the current `pokemon` option value from the interaction's already-selected options.
- Queries `deps.GameData.FormsForPokemon(id)` (existing accessor or add one) → list of form name strings.
- Returns them as choices. Empty when no pokemon selected.

**Mapper**:
```go
func Track(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
    o := flattenOptions(opts)
    tokens := []string{}

    val := strings.ToLower(o["pokemon"].StringValue())
    if val == "" { return nil, &MapperError{Key: "error.slash.track.no_pokemon"} }
    tokens = append(tokens, val)  // "25" or "everything"

    if v, ok := o["iv"]; ok && v.StringValue() != "" {
        tokens = append(tokens, "iv"+v.StringValue())
    }
    for _, league := range []string{"great", "ultra", "little"} {
        if opt, ok := o[league+"_rank"]; ok && opt.IntValue() > 0 {
            tokens = append(tokens, fmt.Sprintf("%s%d", league, opt.IntValue()))
        }
    }
    appendDistance(&tokens, o["distance"])
    if v, ok := o["clean"]; ok && v.BoolValue() { tokens = append(tokens, "clean") }
    if v, ok := o["template"]; ok && v.StringValue() != "" {
        tokens = append(tokens, "template:"+v.StringValue())
    }
    if v, ok := o["form"]; ok && v.StringValue() != "" {
        tokens = append(tokens, "form:"+v.StringValue())
    }
    if v, ok := o["size"]; ok && v.StringValue() != "" && v.StringValue() != "all" {
        tokens = append(tokens, v.StringValue())
    }
    return tokens, nil
}
```

The `everything` keyword permission check happens server-side in the existing `cmd.track` business logic, which already consults `EverythingFlagPermissions`. Slash doesn't duplicate the check.

#### Task 30: `/raid` — boss OR level, mutual exclusion

```go
{String,  "boss",        Autocomplete}              // pokemon ID; RecentActivity-boosted
{String,  "level",       Autocomplete}              // human-labeled raid level (see helper below)
{Int,     "team",        Choices: [any/valor/mystic/instinct/harmony]}
{Int,     "distance"}
{Bool,    "clean"}
{String,  "template",    Autocomplete}
```

**Mapper validates that exactly one of `boss`/`level` is set:**

```go
func Raid(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
    o := flattenOptions(opts)
    boss := o["boss"].StringValue()
    level := o["level"].StringValue()
    if boss == "" && level == "" {
        return nil, &MapperError{Key: "error.slash.raid.need_boss_or_level"}
    }
    if boss != "" && level != "" {
        return nil, &MapperError{Key: "error.slash.raid.boss_and_level"}
    }
    tokens := []string{}
    if boss != "" { tokens = append(tokens, strings.ToLower(boss)) }      // "150" or "everything"
    if level != "" { tokens = append(tokens, strings.ToLower(level)) }    // "legendary", "shadow3", ...
    // team/distance/clean/template — same as other mutating mappers
    return tokens, nil
}
```

**Shared `raidLevelChoices()` helper** drives autocomplete for `/raid level` and `/egg level`:

```go
// processor/internal/discordbot/slash/autocomplete/raid_level.go
type raidLevelChoice struct{ Label, Value string }

var raidLevelChoices = []raidLevelChoice{
    {"Tier 1", "1"}, {"Tier 3", "3"}, {"Tier 5", "5"}, {"Tier 6", "6"},
    {"Mega", "mega"}, {"Legendary", "legendary"},
    {"Shadow Tier 1", "shadow1"}, {"Shadow Tier 3", "shadow3"}, {"Shadow Tier 5", "shadow5"},
    {"Ultra Beast", "ultra beast"},
    {"Everything", "everything"},
}

func RaidLevel(focused string) []*discordgo.ApplicationCommandOptionChoice {
    focused = strings.ToLower(strings.TrimSpace(focused))
    out := make([]*discordgo.ApplicationCommandOptionChoice, 0, 25)
    for _, c := range raidLevelChoices {
        if focused != "" && !strings.Contains(strings.ToLower(c.Label), focused) { continue }
        out = append(out, &discordgo.ApplicationCommandOptionChoice{Name: c.Label, Value: c.Value})
    }
    return out
}
```

Exact labels (`"Shadow Tier 3"` vs `"Shadow Level 3"`, exact set of tiers) come from `ParamRaidLevelName` keywords in `bot/argmatch.go` at implementation time. Test: each label/value pair maps cleanly via the text parser's existing matcher.

#### Task 31: `/egg`

```go
{String,  "level",       Required, Autocomplete}   // shared raidLevelChoices helper
{Int,     "team",        Choices: [any/valor/mystic/instinct/harmony]}
{Int,     "distance"}
{Bool,    "clean"}
{String,  "template",    Autocomplete}
```

Mapper emits the lowercased level value as the first token (numeric string, `"mega"`, `"shadow3"`, `"legendary"`, `"everything"`, etc.).

#### Task 32: `/quest`

```go
{String,  "pokemon",     Autocomplete}
{String,  "item",        Autocomplete}
{Int,     "stardust"}
{String,  "candy",       Autocomplete}
{String,  "mega_energy", Autocomplete}
{String,  "xl_candy",    Autocomplete}
{Int,     "min_amount"}
{Int,     "distance"}
{Bool,    "clean"}
{String,  "template",    Autocomplete}
```

Mapper has mutual-exclusion validation:

```go
func Quest(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
    o := flattenOptions(opts)
    rewardOpts := []string{"pokemon", "item", "stardust", "candy", "mega_energy", "xl_candy"}

    var set []string
    for _, opt := range rewardOpts {
        if hasNonZeroValue(o[opt]) { set = append(set, opt) }
    }
    if len(set) == 0 {
        return nil, &MapperError{Key: "error.slash.quest.no_reward"}
    }
    if len(set) > 1 {
        return nil, &MapperError{Key: "error.slash.quest.exactly_one_reward", Args: []any{strings.Join(set, ", ")}}
    }

    tokens := []string{}
    switch set[0] {
    case "pokemon":     tokens = append(tokens, strings.ToLower(o["pokemon"].StringValue()))
    case "item":        tokens = append(tokens, "item:"+o["item"].StringValue())
    case "stardust":    tokens = append(tokens, fmt.Sprintf("stardust:%d", o["stardust"].IntValue()))
    case "candy":       tokens = append(tokens, "candy"+o["candy"].StringValue())
    case "mega_energy": tokens = append(tokens, "energy"+o["mega_energy"].StringValue())
    case "xl_candy":    tokens = append(tokens, "xlcandy"+o["xl_candy"].StringValue())
    }
    if v, ok := o["min_amount"]; ok && v.IntValue() > 0 {
        tokens = append(tokens, fmt.Sprintf("amount:%d", v.IntValue()))
    }
    appendDistance(&tokens, o["distance"])
    if v, ok := o["clean"]; ok && v.BoolValue() { tokens = append(tokens, "clean") }
    if v, ok := o["template"]; ok && v.StringValue() != "" {
        tokens = append(tokens, "template:"+v.StringValue())
    }
    return tokens, nil
}
```

#### Task 33: `/invasion`

```go
{String, "grunt_type", Required, Autocomplete}   // regular grunts AND special incident types
{Int,    "distance"}
{Bool,   "clean"}
{String, "template",   Autocomplete}
```

**`grunt_type` autocomplete** sources from two places:
1. **Regular grunts** from `deps.GameData.Grunts` (existing `gamedata/grunts.go`) — type-translated names like "Fire Grunt", "Water Grunt", plus named leaders Giovanni/Cliff/Sierra/Arlo.
2. **Special incident types** — short hard-coded list of incident keywords the text parser already accepts (e.g. `kecleon`, `gold-stop` / "Gold Pokéstop", `showcase`, `pokestop spawn`).

Implementation:
```go
// autocomplete/grunt.go
var specialIncidents = []struct{ Label, Value string }{
    {"Kecleon", "kecleon"},
    {"Gold Pokéstop", "gold-stop"},
    {"Showcase", "showcase"},
    {"Pokéstop Spawn", "pokestop spawn"},
    // …extend from invasions.json special types as discovered…
}

func Grunt(ctx context.Context, deps *bot.BotDeps, focused, userLang string) []*discordgo.ApplicationCommandOptionChoice {
    focused = strings.ToLower(strings.TrimSpace(focused))
    var out []*discordgo.ApplicationCommandOptionChoice
    if focused == "" && deps.RecentActivity != nil {
        for _, id := range deps.RecentActivity.ActiveInvasionGrunts() {
            name := gruntNameFor(deps, id, userLang)
            out = append(out, &discordgo.ApplicationCommandOptionChoice{Name: name, Value: strings.ToLower(name)})
            if len(out) == 10 { break }
        }
    }
    for _, s := range specialIncidents {
        if focused != "" && !strings.Contains(strings.ToLower(s.Label), focused) { continue }
        out = append(out, &discordgo.ApplicationCommandOptionChoice{Name: s.Label, Value: s.Value})
    }
    // …regular grunt name search via gamedata.Grunts, filter by focused, append…
    return out
}
```

Mapper: append lowercased grunt_type value + standard distance/clean/template.

#### Task 34: `/lure`

```go
{String, "lure_type", Required, Choices: [glacial/mossy/magnetic/rainy/sparkly/normal]}
{Int,    "distance"}
{Bool,   "clean"}
{String, "template",  Autocomplete}
```

Mapper: append lure_type + standard distance/clean/template.

#### Task 35: `/nest`

```go
{String, "pokemon",       Required, Autocomplete}
{Int,    "min_spawn_avg"}
{Int,    "distance"}
{Bool,   "clean"}
{String, "template",      Autocomplete}
```

Mapper: append lowercased pokemon + `t<min_spawn_avg>` + standard fields.

#### Task 36: `/maxbattle`

```go
{String, "pokemon",  Required, Autocomplete}
{Int,    "level",    Choices: [1..6]}
{Bool,   "gmax"}
{Int,    "distance"}
{Bool,   "clean"}
{String, "template", Autocomplete}
```

Mapper: lowercased pokemon + `level<level>` + `gmax` (if true) + standard.

#### Task 37: `/gym`

```go
{Int,    "team",           Choices: [any/valor/mystic/instinct/harmony]}
{Bool,   "slot_changes"}
{Bool,   "battle_changes"}
{Int,    "distance"}
{Bool,   "clean"}
{String, "template",       Autocomplete}
```

Mapper: team name + "slot changes" / "battle changes" (multi-word — preserve spaces) + standard.

#### Task 38: `/fort`

```go
{Int,    "fort_type",     Required, Choices: [pokestop/gym]}
{Bool,   "include_empty"}
{Int,    "distance"}
{Bool,   "clean"}
{String, "template",      Autocomplete}
```

Mapper: fort_type + "empty" (if include_empty) + standard.

#### Task 39: `/untrack` — sub-commands per type

```go
// Definition: 10 sub-commands under /untrack
Options: []*discordgo.ApplicationCommandOption{
    untrackSub("pokemon"),  untrackSub("raid"),     untrackSub("egg"),
    untrackSub("quest"),    untrackSub("invasion"), untrackSub("lure"),
    untrackSub("nest"),     untrackSub("gym"),      untrackSub("fort"),
    untrackSub("maxbattle"),
}

func untrackSub(typ string) *discordgo.ApplicationCommandOption {
    return &discordgo.ApplicationCommandOption{
        Type:        discordgo.ApplicationCommandOptionSubCommand,
        Name:        typ,
        Description: "Remove a " + typ + " tracking rule",
        Options: []*discordgo.ApplicationCommandOption{
            {
                Type:         discordgo.ApplicationCommandOptionString,
                Name:         "tracking",
                Description:  "Pick from your existing " + typ + " tracking rules",
                Required:     true,
                Autocomplete: true,
            },
        },
    }
}
```

**Mapper** switches on the sub-command name, but only to assist tracing — the UID is unique across types so the emitted token is always the same shape:

```go
func Untrack(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
    if len(opts) == 0 {
        return nil, &MapperError{Key: "error.slash.untrack.no_subcommand"}
    }
    sub := opts[0]
    var uid string
    for _, o := range sub.Options {
        if o.Name == "tracking" { uid = o.StringValue() }
    }
    if uid == "" { return nil, &MapperError{Key: "error.slash.untrack.no_tracking"} }
    return []string{"id:" + uid}, nil
}
```

**Autocomplete dispatcher** routes `option == "tracking"` under `/untrack <type>` to the userstate lister with `UserStateHint{Subtype: type}`, where `type` is read from the sub-command name (the parent option). Already-built `listTracking` function handles the rest.

---

## Phase 5 — User profile commands

Pattern same as Phase 4 but with Discord sub-commands instead of top-level options.

### Task 40: `/area` with sub-commands

```go
// Definition uses ApplicationCommandOptionSubCommand
Options: []*discordgo.ApplicationCommandOption{
    {Type: SubCommand, Name: "add",    Options: []{ {String, "area", Required, Autocomplete} }},
    {Type: SubCommand, Name: "remove", Options: []{ {String, "area", Required, Autocomplete} }},
    {Type: SubCommand, Name: "show"},
}
```

Mapper switches on the sub-command name and emits the equivalent text tokens:

```go
func Area(opts []*discordgo.ApplicationCommandInteractionDataOption) ([]string, error) {
    if len(opts) == 0 {
        return nil, &MapperError{Key: "error.slash.area.no_subcommand"}
    }
    sub := opts[0]
    switch sub.Name {
    case "add":
        return []string{"add", sub.Options[0].StringValue()}, nil
    case "remove":
        return []string{"remove", sub.Options[0].StringValue()}, nil
    case "show":
        return nil, nil
    }
    return nil, &MapperError{Key: "error.slash.area.unknown_subcommand"}
}
```

### Task 41: `/profile` with sub-commands

```go
Options: []*discordgo.ApplicationCommandOption{
    {Type: SubCommand, Name: "list"},
    {Type: SubCommand, Name: "change", Options: []{ {String, "name", Required, Autocomplete} }},
    {Type: SubCommand, Name: "create", Options: []{ {String, "name", Required} }},
    {Type: SubCommand, Name: "delete", Options: []{ {String, "name", Required, Autocomplete} }},
}
```

Mapper similarly switches on sub-command, emits text-grammar tokens (`change`, `create`, `delete`, `list` aren't actually used by text — adjust to whatever the underlying `cmd.profile` Run expects).

### Task 42: `/location` — coords OR place name

```go
{String, "place", Required}  // "51.28, 1.08" OR a place name like "Canterbury, UK"
```

**Mapper accepts both forms.** Because forward-geocoding hits an external API (Nominatim or Google depending on operator config), the mapper takes the dispatcher's `*bot.BotDeps` to call the existing `Geocoder`. This is the first mapper that does I/O — note in code review.

```go
// processor/internal/discordbot/slash/mappers/location.go
var coordsRe = regexp.MustCompile(`^\s*(-?\d+(?:\.\d+)?)\s*,\s*(-?\d+(?:\.\d+)?)\s*$`)

func Location(opts []*discordgo.ApplicationCommandInteractionDataOption, deps *bot.BotDeps) ([]string, error) {
    o := flattenOptions(opts)
    raw := strings.TrimSpace(o["place"].StringValue())
    if raw == "" {
        return nil, &MapperError{Key: "error.slash.location.empty"}
    }

    if m := coordsRe.FindStringSubmatch(raw); m != nil {
        return []string{m[1] + "," + m[2]}, nil
    }

    // Forward geocode the place name
    if deps.Geocoder == nil {
        return nil, &MapperError{Key: "error.slash.location.no_geocoder"}
    }
    lat, lon, err := deps.Geocoder.Forward(context.Background(), raw)
    if err != nil {
        return nil, &MapperError{Key: "error.slash.location.geocode_failed", Args: []any{raw}}
    }
    return []string{fmt.Sprintf("%g,%g", lat, lon)}, nil
}
```

**Mapper signature change.** `Location` takes `deps` as a second arg — other mappers don't. The dispatcher and `mappers.Lookup` registry have two options:

1. **Special-case `/location`** at the dispatcher level: detect the command name and pass deps explicitly. Other mappers retain the existing 1-arg signature.
2. **Generalize the mapper signature** to `(opts, deps)` everywhere.

Option 1 is less invasive. Implement as:
```go
// dispatcher.go — replace the mapper call for /location
// (uses canonical short name; i18n-renamed "location" → still routes here)
if canon == "location" {
    tokens, err = mappers.Location(ic.ApplicationCommandData().Options, d.deps)
} else {
    mapperFn := mappers.Lookup(canon)
    tokens, err = mapperFn(ic.ApplicationCommandData().Options)
}
```

Test the geocode error path with a fake `Geocoder` that returns ErrNotFound.

---

## Phase 6 — Localization

### Task 43: Discord ↔ PoracleNG locale mapping

**Files:**
- Create: `processor/internal/discordbot/slash/localization.go`
- Create: `processor/internal/discordbot/slash/localization_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestPoracleToDiscord(t *testing.T) {
    if poracleToDiscord["de"] != discordgo.German { t.Error("de mapping") }
    if poracleToDiscord["fr"] != discordgo.French { t.Error("fr mapping") }
}

func TestLocalizationsForKeyOnlyLoadedLanguages(t *testing.T) {
    bundle := i18n.NewBundle()
    bundle.Load("en", map[string]string{"slash.track.desc": "Track a pokemon"})
    bundle.Load("de", map[string]string{"slash.track.desc": "Verfolge ein Pokémon"})

    loc := localizationsForKey(bundle, "slash.track.desc")
    if loc == nil { t.Fatal("nil localizations") }
    if (*loc)[discordgo.German] != "Verfolge ein Pokémon" { t.Errorf("de=%q", (*loc)[discordgo.German]) }
    // French is not loaded → no entry
    if _, ok := (*loc)[discordgo.French]; ok { t.Error("french should be absent") }
}

func TestValidSlashName(t *testing.T) {
    if !validSlashName("track") { t.Error("track should be valid") }
    if !validSlashName("verfolge") { t.Error("verfolge should be valid") }
    if validSlashName("very long name with spaces and special chars") { t.Error("should be invalid") }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd processor && go test ./internal/discordbot/slash/ -run Localization -v`
Expected: FAIL.

- [ ] **Step 3: Implement**

```go
// processor/internal/discordbot/slash/localization.go
package slash

import (
    "regexp"

    "github.com/bwmarrin/discordgo"

    "github.com/pokemon/poracleng/processor/internal/i18n"
)

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
        if val == "" || val == key { continue }
        if !validSlashName(val) { continue }
        out[discordCode] = val
    }
    if len(out) == 0 { return nil }
    return &out
}

var slashNameRe = regexp.MustCompile(`^[\p{L}\p{N}_-]{1,32}$`)

func validSlashName(s string) bool { return slashNameRe.MatchString(s) }
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd processor && go test ./internal/discordbot/slash/ -run Localization -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/discordbot/slash/localization.go processor/internal/discordbot/slash/localization_test.go
git commit -m "discordbot/slash: localization helpers"
```

---

### Task 44: i18n key seeds + slashNameLocalizations / slashDescriptionLocalizations

`buildDefinition` already routes through `resolveSlashName` and `slashDescription` (defined in Task 7). This task adds the actual localization map builders that read from the bundle's loaded languages.

```go
// localization.go
//
// slashNameLocalizations builds NameLocalizations for a slash command from
// the "slash.cmd.<short>" key namespace in each loaded language.
func slashNameLocalizations(bundle *i18n.Bundle, key string) *map[discordgo.Locale]string {
    return localizationsForKey(bundle, "slash."+key, true /* validateSlashName */)
}

// slashDescriptionLocalizations builds DescriptionLocalizations from the
// "slash.desc.<short>" key namespace.
func slashDescriptionLocalizations(bundle *i18n.Bundle, key string) *map[discordgo.Locale]string {
    short := strings.TrimPrefix(key, "cmd.")
    return localizationsForKey(bundle, "slash.desc."+short, false /* descriptions don't need slash-name regex */)
}

// Generic helper — iterates loaded languages, fetches the key value per
// language, filters out empty / equal-to-key (untranslated) / invalid (when
// validateName=true).
func localizationsForKey(bundle *i18n.Bundle, key string, validateName bool) *map[discordgo.Locale]string {
    out := map[discordgo.Locale]string{}
    for _, lang := range bundle.LoadedLanguages() {
        if lang == "en" { continue }
        discordCode, ok := poracleToDiscord[lang]
        if !ok { continue }
        tr := bundle.For(lang)
        if tr == nil { continue }
        val := tr.T(key)
        if val == "" || val == key { continue }
        if validateName && !validSlashName(val) { continue }
        out[discordCode] = val
    }
    if len(out) == 0 { return nil }
    return &out
}
```

Seed `processor/internal/i18n/locale/en.json`:

```json
{
  "slash.cmd.version":  "version",
  "slash.cmd.tracked":  "tracked",
  "slash.cmd.track":    "track",
  "slash.cmd.raid":     "raid",
  "slash.cmd.egg":      "egg",
  "slash.cmd.quest":    "quest",
  "slash.cmd.invasion": "invasion",
  "slash.cmd.lure":     "lure",
  "slash.cmd.nest":     "nest",
  "slash.cmd.maxbattle":"maxbattle",
  "slash.cmd.gym":      "gym",
  "slash.cmd.fort":     "fort",
  "slash.cmd.untrack":  "untrack",
  "slash.cmd.area":     "area",
  "slash.cmd.profile":  "profile",
  "slash.cmd.location": "location",
  "slash.cmd.language": "language",
  "slash.cmd.help":     "help",
  "slash.cmd.info":     "info",

  "slash.desc.version":  "Show Poracle version",
  "slash.desc.tracked":  "List your tracking rules",
  "slash.desc.track":    "Track a Pokemon",
  "slash.desc.raid":     "Track a raid",
  ...

  "slash.opt.track.pokemon":      "pokemon",
  "slash.opt.track.pokemon.desc": "Pokemon to track (or 'Everything')",
  "slash.opt.track.iv":           "iv",
  "slash.opt.track.iv.desc":      "IV range (e.g. \"100\", \"95\", \"0-0\")",
  ...
}
```

**Operator overrides** use the existing `config/custom.{lang}.json` mechanism. To rename `/track` to `/alerts` for English users:

```json
// config/custom.en.json
{
  "slash.cmd.track": "alerts"
}
```

At next startup, the slash command Name becomes `alerts`. The internal cmd key, mapper registration, and `[discord.slash_commands] enable` allow-list all stay keyed on canonical `"track"`.

`resolveSlashName` already logs a warning when the English `slash.cmd.<short>` key is missing — covered.

Convert `registrationErrorText` (defined in Task 11) to use the i18n bundle:

```go
// dispatcher.go — replace registrationErrorText
func registrationErrorText(cfg *config.Config, bundle *i18n.Bundle, lang, guildID string) string {
    tr := bundle.For(lang)
    if tr == nil { tr = bundle.For("en") }
    channel := registrationChannelHint(cfg, guildID)
    if channel != "" {
        return tr.Tf("error.slash.unregistered_with_channel", channel)
    }
    return tr.T("error.slash.unregistered_dm_only")
}

// registrationChannelHint resolves a #channel mention from
// [discord] registration_channels for the user's guild, or "" if absent.
func registrationChannelHint(cfg *config.Config, guildID string) string {
    for _, rc := range cfg.Discord.RegistrationChannels {
        if rc.GuildID == guildID { return "<#" + rc.ChannelID + ">" }
    }
    return ""
}
```

Add to `processor/internal/i18n/locale/en.json`:

```json
{
  "error.slash.unregistered_with_channel": "🛑 You need to register first. DM me with !poracle, or run !poracle in {0}.",
  "error.slash.unregistered_dm_only": "🛑 You need to register first. DM me with !poracle."
}
```

### Task 45: `-sync-slash-commands` CLI flag

**Files:**
- Modify: `processor/cmd/processor/main.go`

```go
forceSyncSlash := flag.Bool("sync-slash-commands", false, "Force slash command sync regardless of cache")
flag.Parse()

// After bot.Start():
if b.slash != nil && (cfg.Discord.SlashCommands.SyncOnStartup || *forceSyncSlash) {
    cfg := slash.Config{
        // ... existing fields ...
        ForceSync: *forceSyncSlash,
    }
    if err := b.slash.SyncCommands(ctx); err != nil {
        log.Errorf("slash sync failed: %v", err)
    }
}
```

5-step TDD: write test that invokes SyncCommands with `ForceSync=true` and confirms it pushes even when fingerprint matches. Commit: `processor: -sync-slash-commands flag`.

---

## Phase 7 — Testing infrastructure

### Task 46: Parity fixture format and loader

**Files:**
- Create: `processor/internal/bot/testdata/parity.yaml`
- Create: `processor/internal/bot/parity_loader.go`
- Create: `processor/internal/bot/parity_loader_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestLoadParityFixtures(t *testing.T) {
    fixtures, err := LoadParityFixtures("testdata/parity.yaml")
    if err != nil { t.Fatal(err) }
    if len(fixtures) == 0 { t.Fatal("no fixtures loaded") }
    for _, f := range fixtures {
        if f.Name == "" { t.Errorf("nameless fixture") }
        if f.Command == "" { t.Errorf("%s: command empty", f.Name) }
    }
}
```

Create the minimal initial YAML:

```yaml
- name: version-no-options
  command: cmd.version
  slash:
    name: version
    options: {}
  text: "!version"
  expected_tokens: []

- name: track-pikachu-iv95
  command: cmd.track
  slash:
    name: track
    options:
      pokemon: pikachu
      iv: "95"
  text: "!track pikachu iv95"
  expected_tokens: [pikachu, iv95]
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd processor && go test ./internal/bot/ -run LoadParityFixtures -v`
Expected: FAIL.

- [ ] **Step 3: Implement loader**

```go
// processor/internal/bot/parity_loader.go
package bot

import (
    "os"

    "gopkg.in/yaml.v3"
)

type ParityFixture struct {
    Name           string                 `yaml:"name"`
    Description    string                 `yaml:"description"`
    Command        string                 `yaml:"command"`
    Slash          SlashInvocation        `yaml:"slash"`
    Text           string                 `yaml:"text"`
    ExpectedTokens []string               `yaml:"expected_tokens"`
}

type SlashInvocation struct {
    Name    string                 `yaml:"name"`
    Options map[string]interface{} `yaml:"options"`
}

func LoadParityFixtures(path string) ([]ParityFixture, error) {
    data, err := os.ReadFile(path)
    if err != nil { return nil, err }
    var fixtures []ParityFixture
    if err := yaml.Unmarshal(data, &fixtures); err != nil { return nil, err }
    return fixtures, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd processor && go test ./internal/bot/ -run LoadParityFixtures -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/bot/testdata/parity.yaml processor/internal/bot/parity_loader.go processor/internal/bot/parity_loader_test.go
git commit -m "bot: parity fixture loader"
```

---

### Task 47: Parity test runner

**Files:**
- Create: `processor/internal/bot/parity_test.go`

- [ ] **Step 1: Write the failing test (which IS the runner)**

```go
// processor/internal/bot/parity_test.go
package bot

import (
    "sort"
    "testing"

    "github.com/pokemon/poracleng/processor/internal/discordbot/slash/mappers"
)

func TestSlashTextParity(t *testing.T) {
    fixtures, err := LoadParityFixtures("testdata/parity.yaml")
    if err != nil { t.Fatal(err) }

    deps := buildTestDeps(t)
    parser := NewParser("!", deps.Translations, []string{"en"}, nil)

    for _, fix := range fixtures {
        t.Run(fix.Name, func(t *testing.T) {
            // 1. Slash side
            mapper := mappers.Lookup(fix.Slash.Name)
            if mapper == nil { t.Fatalf("no mapper for %q", fix.Slash.Name) }
            slashTokens, err := mapper(optionsFromMap(fix.Slash.Options))
            if err != nil { t.Fatalf("mapper error: %v", err) }

            // 2. Text side
            parsed := parser.Parse(fix.Text)
            if parsed.CommandKey != fix.Command {
                t.Fatalf("text parser routed to %q, want %q", parsed.CommandKey, fix.Command)
            }

            // 3. Both equal expected_tokens (order-insensitive)
            assertTokensEqual(t, fix.ExpectedTokens, slashTokens, "slash mapper output")
            assertTokensEqual(t, fix.ExpectedTokens, parsed.Args, "text parser output")
        })
    }
}

func assertTokensEqual(t *testing.T, want, got []string, msg string) {
    t.Helper()
    w := append([]string{}, want...)
    g := append([]string{}, got...)
    sort.Strings(w)
    sort.Strings(g)
    if !equalSlices(w, g) {
        t.Errorf("%s\n want: %v\n  got: %v", msg, w, g)
    }
}
```

- [ ] **Step 2: Run test**

Run: `cd processor && go test ./internal/bot/ -run SlashTextParity -v`
Expected: PASS for current fixtures (version, track).

- [ ] **Step 3-5: Iterate**

Add fixtures for every option of every command per the coverage rule (Task 48). Add 5-7 fixtures per commit so reviews are manageable.

```bash
git add processor/internal/bot/parity_test.go
git commit -m "bot: slash↔text parity test"
```

---

### Task 48: Coverage meta-test

**Files:**
- Create: `processor/internal/bot/coverage_test.go`

- [ ] **Step 1: Write the test**

```go
// processor/internal/bot/coverage_test.go
package bot

import (
    "testing"

    "github.com/pokemon/poracleng/processor/internal/discordbot/slash"
)

func TestEveryCommandAndOptionHasFixture(t *testing.T) {
    fixtures, err := LoadParityFixtures("testdata/parity.yaml")
    if err != nil { t.Fatal(err) }

    covered := map[string]map[string]bool{}
    for _, fix := range fixtures {
        if _, ok := covered[fix.Slash.Name]; !ok {
            covered[fix.Slash.Name] = map[string]bool{}
        }
        for opt := range fix.Slash.Options {
            covered[fix.Slash.Name][opt] = true
        }
    }

    for _, cmd := range slash.AllDefinitions(testBundle(t), nil) {
        opts, ok := covered[cmd.Name]
        if !ok {
            t.Errorf("slash command %q has no parity fixture", cmd.Name)
            continue
        }
        for _, opt := range cmd.Options {
            if opt.Type == discordgo.ApplicationCommandOptionSubCommand {
                // Subcommand options nested — flatten test
                for _, subOpt := range opt.Options {
                    key := opt.Name + "." + subOpt.Name
                    if !opts[key] {
                        t.Errorf("slash command %q subcommand %q option %q never exercised", cmd.Name, opt.Name, subOpt.Name)
                    }
                }
                continue
            }
            if !opts[opt.Name] {
                t.Errorf("slash command %q option %q never exercised in any parity fixture", cmd.Name, opt.Name)
            }
        }
    }
}
```

Expose `slash.AllDefinitions(enable []string) []*discordgo.ApplicationCommand` as a public function.

- [ ] **Step 2: Run test**

Run: `cd processor && go test ./internal/bot/ -run EveryCommand -v`
Expected: FAIL for any command with options lacking fixtures. Fix by adding fixtures.

- [ ] **Step 3: Add fixtures iteratively**

For each failing option, add a fixture exercising that option. Commit after each command's coverage is complete.

- [ ] **Step 4: Final pass — full test suite**

Run: `cd processor && go test ./...`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/bot/coverage_test.go processor/internal/bot/testdata/parity.yaml
git commit -m "bot: coverage meta-test for parity fixtures"
```

---

## Self-Review

### Spec coverage check

Walk every settled decision from `project_slash_commands_plan.md`:

| Decision | Task(s) implementing |
|---|---|
| RecentActivity always constructed | Task 1, 2 |
| `discordbot/slash/` package layout | Tasks 4-7, File Structure |
| Sync post-`session.Open()`, non-fatal | Task 5, 13 |
| New `BotDeps.RecentActivity` | Task 2 |
| New `[discord.slash_commands]` config | Task 3 |
| `/version` reuses `cmd.version` Run as smoke test | Tasks 6-13 |
| All slash responses ephemeral (no config knob) | Task 6 (`renderInitial` hardcodes ephemeral flag) |
| Plain text + emoji prefix errors | Tasks 6, 11 |
| `Reply.IsDM` → real DM + ephemeral confirmation | Task 6 needs follow-up; **GAP** |
| Errors always ephemeral | Task 11 (respondError uses ephemeral) |
| Fingerprint cache | Task 12 |
| `-sync-slash-commands` flag | Task 45 |
| Iterate only over loaded languages | Task 43 |
| Missing English → warn; missing translation → silent | Task 44; **GAP if not explicit** |
| `cmd.*` / `slash.*` key naming | Tasks 7, 43, 44 |
| Slash = personal-DM scope, target=sender | Task 14 (TargetID=UserID) |
| No `BuildTarget` for slash | Task 14 (direct context build) |
| Shared `command_security` config | Task 15 |
| Rate-limited users still execute commands | (Inherits from existing Run flow — no slash-side check) |
| Autocomplete empty for unregistered users | **GAP — need to add to Task 28** |
| Hardcoded skip-registration list (cmd.version only) | Task 11 |
| Test approach A+C+D | Task 47 (D parity), Tasks 7+ snapshots (C), all `_test.go` files (A) |
| Coverage meta-test | Task 48 |
| Parity fixture YAML format | Task 46 |
| Coverage by enforcement | Task 48 |
| Userstate primitive (explicit registration) | Task 20, plus `Dispatcher.NewDispatcher` |
| Listers: tracking, areas, profiles | Tasks 21-23 |
| Label truncation preserving suffix | Task 20 (FilterAndCap) |
| Empty results → empty list | Task 20 (filterAndCap returns nil when no matches) |
| No autocomplete caching v1 | (Inherent — no cache layer added) |
| MapperError type + translation | Task 9, 32 |
| 18 commands per command reference | Tasks 7-8, 16-19, 29-42 |

### Gaps identified and applied inline

1. **`Reply.IsDM` → real DM + ephemeral confirmation** — Applied in Task 6 (`sendToDM` helper, updated `Send` to branch on `r.IsDM` before normal rendering).

2. **Autocomplete returns empty for unregistered users** — Applied in Task 28 (added human lookup and skip-registration check at the head of `HandleAutocomplete`).

3. **Missing English key warning at startup** — Applied in Task 44 (`descriptionFor` logs a warning when the English `slash.*.desc` key is missing).

4. **Registration error using config-driven channel hint** — Applied in Task 44 (`registrationErrorText` converted to i18n with `error.slash.unregistered_with_channel` / `error.slash.unregistered_dm_only` keys; `registrationChannelHint` looks up the channel from `[discord] registration_channels`).

5. **Sub-command coverage in meta-test** — Applied in Task 48 (coverage walk flattens sub-command options as `subcommand.option` keys and checks each).

All five gaps closed.

### Placeholder scan

Scanning the plan for "TODO", "TBD", "etc", "similar to":
- Task 4 dispatcher.go has `// TODO: Task 11 — implement dispatch routing` — intentional, marks where the next task lives.
- Task 4 dispatcher.go has `// TODO: Task 28 — implement autocomplete routing` — intentional, same.
- Task 16 references "Same pattern as Task 16" via the "Pattern (use for every mutating command…)" block — pattern is explicit, not a hidden reference.

No problematic placeholders.

### Type consistency check

- `bot.Reply` used consistently across `reply.go`, dispatcher, mapper signatures.
- `*discordgo.ApplicationCommand` used consistently.
- `Mapper` type signature uniform: `func([]*discordgo.ApplicationCommandInteractionDataOption) ([]string, error)`.
- `MapperError` and `Choice` types used consistently across autocomplete and mapper layers.
- `UserStateLister` matches autocomplete lister signature throughout.
- `Fingerprint` is `func(cmds []*discordgo.ApplicationCommand) string` everywhere.

One inconsistency: Task 11's `buildContext` returns `(*bot.CommandContext, error)` while Phase 1 description implies it's infallible. **Fix**: Task 11's error return is for future use (when DB lookups fail) — keep the signature, return nil error from Phase 1 implementation. This is fine.

### Gap-fix consolidation

Apply gap fixes by extending these tasks (inline edits, not new tasks):

- **Task 6**: add `Reply.IsDM` handling.
- **Task 28**: add unregistered-user-returns-empty guard.
- **Task 44**: log warning for missing English; convert `registrationErrorText` to i18n key.
- **Task 48**: explicit subcommand handling in coverage walk.

These are noted in the task body comments above; reviewer can apply inline.

---

## Execution Notes

- **Recommended execution mode**: subagent-driven-development (fresh agent per task with two-stage review). This plan is large; subagent isolation helps avoid context-pollution.
- **Alternative**: executing-plans inline; batch by phase (each phase becomes one execution unit with a checkpoint).
- **Each phase produces a working binary**. After Phase 1, `/version` works against a real Discord guild. After Phase 2, four read-only commands work. After Phase 4, all tracking can be managed via slash. Phase 7 adds the parity safety net.
- **Branch strategy**: develop on a feature branch `slash-commands`. Avoid pushing upstream until the user reviews this plan and signs off.
- **Local testing**: a dedicated Discord test guild with `register_globally=false` and `guilds=["<test-guild-id>"]` for fast iteration. Global rollout only after all phases pass.

---

## References

- Companion design doc: `project_slash_commands_plan.md` (decisions, rationale, debate-points)
- Existing PoracleNG:
  - `processor/internal/bot/command.go` — Command interface + BotDeps + CommandContext
  - `processor/internal/bot/parser.go` — text command parser (referenced for parity test)
  - `processor/internal/bot/registry.go` — Command registry (reused as-is)
  - `processor/internal/bot/command.go:259` — `IsCommandDisabled` (shared text+slash disable mechanism)
  - `processor/internal/bot/community_logic.go:83` — `IsRegistrationChannel` (used for unregistered-user error hint)
  - `processor/internal/config/config.go:102` — `[general] disabled_commands` (existing toggle)
  - `processor/internal/config/config.go:83` — `[tracking] everything_flag_permissions` (gating "Everything" in autocomplete)
  - `processor/cmd/processor/main.go:518-539` — existing command registration pattern (matched for slash)
