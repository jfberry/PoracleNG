# Store Abstractions & Business Logic Extraction

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Abstract database access behind interfaces (HumanStore, TrackingStore) and extract business logic (AreaLogic) so that commands are testable, logic is reusable for APIs, and the diff/apply pattern is generalized.

**Architecture:** Stores provide typed CRUD over the DB. Business logic modules (AreaLogic, CommunityLogic) operate on store data without direct SQL. Commands compose stores + logic. Tests inject mock stores.

**Tech Stack:** Go generics (1.21+), existing sqlx, existing db package types

---

## Current Problems

1. **30+ command files contain raw SQL** — `ctx.DB.Exec("UPDATE humans SET area = ?...")`, `ctx.DB.Get(&h, "SELECT...")` scattered everywhere
2. **JSON marshal/unmarshal of area/community** repeated in 6+ places — `json.Unmarshal([]byte(areaJSON), &areas)`, `json.Marshal(areas)`
3. **Area logic embedded in area command** — available areas filtering, duplicate removal, security checking, display name resolution all live in `commands/area.go` but are needed by API endpoints and other commands
4. **Diff+apply pattern duplicated 10 times** — identical 30-line block in every tracking command
5. **No way to test commands without a real database**

## Design

### HumanStore Interface

```go
// processor/internal/store/human.go
package store

type Human struct {
    ID                  string
    Type                string
    Name                string
    Enabled             bool
    AdminDisable        bool
    DisabledDate        *time.Time
    Area                []string   // decoded from JSON
    Latitude            float64
    Longitude           float64
    Language            string
    ProfileNo           int
    CommunityMembership []string   // decoded from JSON
    AreaRestriction     []string   // decoded from JSON
    BlockedAlerts       []string   // decoded from JSON
    Fails               int
    Notes               string
}

type HumanStore interface {
    // Core CRUD
    Get(id string) (*Human, error)                    // returns nil if not found
    GetByType(id, typ string) (*Human, error)         // lookup by id + type
    Create(h *Human) error
    Delete(id string) error

    // Field updates (each triggers no reload — caller decides)
    SetEnabled(id string, enabled bool) error
    SetAdminDisable(id string, disable bool, setDate bool) error
    SetLocation(id string, lat, lon float64) error
    SetLocationForProfile(id string, profileNo int, lat, lon float64) error
    SetArea(id string, areas []string) error           // marshals to JSON
    SetAreaForProfile(id string, profileNo int, areas []string) error
    SetLanguage(id string, lang string) error
    SetProfileNo(id string, profileNo int) error
    SetCommunity(id string, communities []string, restrictions []string) error
    SetBlockedAlerts(id string, blocked *string) error
    SetName(id string, name string) error

    // Queries
    IsRegistered(id string) (bool, error)
    ListByType(typ string) ([]*Human, error)
    ListAll() ([]*Human, error)
    ListByTypeExcluding(typ string, excludeIDs []string) ([]*Human, error)
}
```

The real implementation wraps `*sqlx.DB` and handles JSON marshaling internally. Commands never see raw JSON strings for area/community/blocked_alerts.

### TrackingStore Interface

```go
// processor/internal/store/tracking.go
package store

type TrackingStore[T any] interface {
    SelectByIDProfile(id string, profileNo int) ([]T, error)
    Insert(row *T) (int64, error)
    DeleteByUIDs(id string, uids []int64) error
    DeleteAllForProfile(id string, profileNo int) error
}

// ApplyDiff runs the standard diff+insert+update pattern.
// Returns counts of unchanged, updated, and inserted rows.
func ApplyDiff[T any](
    s TrackingStore[T],
    id string,
    insert []T,
    tracked []T,
) (unchanged, updated, newInsert []T) {
    // Uses api.DiffTracking for comparison
    // Returns categorized results — caller decides what to do with them
}

// ApplyAndPersist is the full diff+delete+insert+reload cycle.
func ApplyAndPersist[T any](
    s TrackingStore[T],
    id string,
    profileNo int,
    insert []T,
    getUID func(*T) int64,
    setUID func(*T, int64),
    reloadFunc func(),
) (unchanged, updated, inserted []T, err error) {
    tracked, err := s.SelectByIDProfile(id, profileNo)
    // ... diff ...
    // ... delete updated UIDs ...
    // ... insert new + updated ...
    reloadFunc()
    return
}

// TrackingStores holds all 10 tracking store instances.
type TrackingStores struct {
    Monsters   TrackingStore[db.MonsterTrackingAPI]
    Raids      TrackingStore[db.RaidTrackingAPI]
    Eggs       TrackingStore[db.EggTrackingAPI]
    Quests     TrackingStore[db.QuestTrackingAPI]
    Invasions  TrackingStore[db.InvasionTrackingAPI]
    Lures      TrackingStore[db.LureTrackingAPI]
    Gyms       TrackingStore[db.GymTrackingAPI]
    Nests      TrackingStore[db.NestTrackingAPI]
    Forts      TrackingStore[db.FortTrackingAPI]
    Maxbattles TrackingStore[db.MaxbattleTrackingAPI]
}
```

### AreaLogic

```go
// processor/internal/bot/area_logic.go (or internal/area/)
package bot

type AreaInfo struct {
    Name           string
    Group          string
    UserSelectable bool
    IsActive       bool   // currently in user's area list
}

type AreaLogic struct {
    fences []geofence.Fence
    cfg    *config.Config
}

func NewAreaLogic(fences []geofence.Fence, cfg *config.Config) *AreaLogic

// GetAvailableAreas returns areas the user can select, filtered by
// area security/community membership if enabled.
func (a *AreaLogic) GetAvailableAreas(communities []string) []AreaInfo

// GetAvailableAreasMarked returns available areas with IsActive set
// based on the user's current area list.
func (a *AreaLogic) GetAvailableAreasMarked(communities []string, currentAreas []string) []AreaInfo

// AddAreas adds areas to user's list, deduplicating and validating
// against available areas. Returns (added, notFound, newList).
func (a *AreaLogic) AddAreas(currentAreas []string, communities []string, toAdd []string) (added []string, notFound []string, newList []string)

// RemoveAreas removes areas from user's list.
// Returns (removed, newList).
func (a *AreaLogic) RemoveAreas(currentAreas []string, toRemove []string) (removed []string, newList []string)

// ResolveDisplayNames maps lowercase area names to their display names
// from the geofence data.
func (a *AreaLogic) ResolveDisplayNames(areas []string) []string

// FilterByRestriction filters an area list against location restrictions.
func (a *AreaLogic) FilterByRestriction(areas []string, restrictions []string) []string

// ValidateAreaForUser checks if an area change is permitted given
// the user's community restrictions.
func (a *AreaLogic) ValidateAreaForUser(areas []string, communities []string) []string
```

This extracts all area logic from `commands/area.go` into a testable, reusable module. The area command becomes thin: parse args → call AreaLogic → format reply.

### Updated CommandContext

```go
type CommandContext struct {
    // Identity (unchanged)
    UserID, UserName, Platform, ChannelID, GuildID string
    IsDM, IsAdmin, IsCommunityAdmin bool
    Language string
    ProfileNo int
    HasLocation, HasArea bool
    TargetID, TargetName, TargetType string
    Permissions Permissions

    // Stores (replace *sqlx.DB)
    Humans   HumanStore
    Tracking *TrackingStores
    DB       *sqlx.DB          // DEPRECATED — kept for migration, remove when all commands converted

    // Logic modules
    AreaLogic      *AreaLogic
    CommunityLogic *CommunityLogic  // already exists, formalize

    // Other deps (unchanged)
    Config, StateMgr, GameData, Translations, Geofence, Fences,
    Dispatcher, RowText, Resolver, ArgMatcher, Geocoder, StaticMap,
    Weather, Stats, DTS, Emoji, NLP, Registry, ReloadFunc
}
```

---

## Tasks

### Task 1: HumanStore Interface + Real Implementation

**Files:**
- Create: `processor/internal/store/human.go` — interface + Human struct
- Create: `processor/internal/store/human_sql.go` — sqlx implementation
- Create: `processor/internal/store/human_sql_test.go` — tests with real DB (or SQLite)

The real implementation wraps existing `db.HumanFull` and the scattered SQL queries into typed methods. JSON fields (area, community_membership, area_restriction, blocked_alerts) are decoded/encoded internally.

### Task 2: TrackingStore Interface + Generic ApplyDiff

**Files:**
- Create: `processor/internal/store/tracking.go` — generic interface + ApplyDiff
- Create: `processor/internal/store/tracking_sql.go` — implementations wrapping existing db.Select*/Insert* functions
- Create: `processor/internal/store/tracking_test.go`

Each of the 10 tracking types gets a `NewMonsterStore(db)`, `NewEggStore(db)`, etc. that returns a `TrackingStore[db.MonsterTrackingAPI]`. The `ApplyAndPersist` function replaces the duplicated 30-line block in all commands.

### Task 3: AreaLogic Extraction

**Files:**
- Create: `processor/internal/bot/area_logic.go` — AreaLogic struct + methods
- Create: `processor/internal/bot/area_logic_test.go` — unit tests (no DB needed)
- Modify: `processor/internal/bot/commands/area.go` — thin wrapper calling AreaLogic

Extract from `commands/area.go`:
- `getAvailableAreas` → `AreaLogic.GetAvailableAreas`
- `getUserAreas` + `setUserAreas` → use HumanStore.GetArea/SetArea
- `addAreas` logic → `AreaLogic.AddAreas` (deduplicate, validate, merge)
- `removeAreas` logic → `AreaLogic.RemoveAreas`
- `resolveAreaDisplayNames` → `AreaLogic.ResolveDisplayNames`
- Area security filtering → `AreaLogic.GetAvailableAreas(communities)`

### Task 4: CommunityLogic Formalization

**Files:**
- Modify: `processor/internal/bot/community_logic.go` — ensure all functions use HumanStore
- Create: `processor/internal/bot/community_logic_test.go` — unit tests

The existing CommunityLogic functions (AddCommunity, RemoveCommunity, CalculateLocationRestrictions, FindCommunityForChannel, IsRegistrationChannel) already exist. Formalize them to use HumanStore for any DB access and add tests.

### Task 5: Mock Stores

**Files:**
- Create: `processor/internal/store/mock_human.go` — MockHumanStore for testing
- Create: `processor/internal/store/mock_tracking.go` — MockTrackingStore for testing

Simple in-memory implementations that record calls and return configured responses.

### Task 6: Wire Stores into CommandContext

**Files:**
- Modify: `processor/internal/bot/command.go` — add Humans, Tracking, AreaLogic fields
- Modify: `processor/cmd/processor/main.go` — create stores, pass to BotDeps
- Modify: `processor/internal/discordbot/bot.go` — pass stores to CommandContext
- Modify: `processor/internal/telegrambot/bot.go` — same
- Modify: `processor/internal/api/command.go` — same

Keep `DB *sqlx.DB` on CommandContext during migration. Commands can be converted incrementally.

### Task 7: Convert Simple Commands to Stores

Convert the simplest commands first to validate the pattern:
- `start.go` — `ctx.Humans.SetEnabled(ctx.TargetID, true)`
- `stop.go` — `ctx.Humans.SetEnabled(ctx.TargetID, false)`
- `language.go` — `ctx.Humans.SetLanguage(ctx.TargetID, matched)`
- `location.go` — `ctx.Humans.SetLocation(ctx.TargetID, lat, lon)`
- `enable.go` / `disable.go` — `ctx.Humans.SetAdminDisable(id, ...)`

### Task 8: Convert Area Command to AreaLogic + HumanStore

The area command becomes thin:
```go
func (c *AreaCommand) addAreas(ctx, args) []Reply {
    toAdd := args
    communities, _ := ctx.Humans.GetCommunity(ctx.TargetID)
    added, notFound, newList := ctx.AreaLogic.AddAreas(currentAreas, communities, toAdd)
    ctx.Humans.SetArea(ctx.TargetID, newList)
    // format reply
}
```

### Task 9: Convert Tracking Commands to TrackingStore + ApplyDiff

Convert one tracking command (egg) as the template, then the rest follow:
```go
func (c *EggCommand) Run(ctx, args) []Reply {
    // ... parse args, build insert structs ...
    unchanged, updated, inserted, err := store.ApplyAndPersist(
        ctx.Tracking.Eggs, ctx.TargetID, ctx.ProfileNo, insert, ...)
    message := buildTrackingMessage(tr, ctx, len(unchanged), len(updated), len(inserted), ...)
    return []Reply{{React: react, Text: message}}
}
```

### Task 10: Write Command Tests

With mock stores available, write tests for:
- `start`/`stop` — simplest, validate store calls
- `egg` — medium, validate arg parsing + diff behavior
- `track` — complex, validate PVP, everything flag, form/type/gen filtering
- `area` — validate AreaLogic integration
- `poracle` — validate registration flow

Each test creates a mock store, sets up expected data, calls `cmd.Run()`, and asserts:
1. The correct store methods were called with correct args
2. The reply text/react is correct
3. ReloadFunc was called (or not) appropriately

---

## Implementation Order

1. **Task 3: AreaLogic** — standalone, no interface changes needed, immediately testable
2. **Task 1: HumanStore** — interface + real impl
3. **Task 2: TrackingStore** — generic interface + ApplyDiff
4. **Task 5: Mock stores** — enable testing
5. **Task 6: Wire into CommandContext** — add new fields alongside DB
6. **Task 7: Convert simple commands** — validate pattern
7. **Task 8: Convert area command** — uses AreaLogic
8. **Task 9: Convert tracking commands** — uses ApplyDiff
9. **Task 4: CommunityLogic tests** — formalize existing code
10. **Task 10: Command tests** — the payoff

Tasks 1-5 are foundation. Tasks 6-9 are incremental migration (can be done command-by-command). Task 10 is the goal.

---

## Migration Strategy

Commands are converted incrementally. During migration, both `ctx.DB` and `ctx.Humans`/`ctx.Tracking` are available. A command can use the new store for some operations and the old DB for others. Once all commands are converted, `ctx.DB` is removed.

This means no big-bang refactor — each command can be converted and tested independently.

## Testing Philosophy

- **AreaLogic tests**: Pure logic tests, no mocks needed (just geofence data + config)
- **HumanStore tests**: Integration tests with real DB (SQLite or MySQL)
- **Command tests with mocks**: Fast unit tests that verify command logic
- **Integration tests**: Full pipeline through `/api/command` endpoint with real DB
