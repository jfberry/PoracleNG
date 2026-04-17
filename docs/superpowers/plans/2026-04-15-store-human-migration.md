# Migrate humans-table consumers to HumanStore Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Retire `db.HumanFull` / `db.SelectOneHumanFull` / `db.CreateHuman` / `db.CreateDefaultProfile` / `db.HumanFullColumns` by routing every consumer through `store.HumanStore`. No more raw `*sqlx.DB` access against the `humans` table outside the store package.

**Architecture:** The `store.HumanStore` interface in `processor/internal/store/human.go` is complete (Get, Create, ListByType{,Enabled}, Update, all the SetX field updaters, all profile operations). `*store.Human` is the typed end-state — `bool` for flags instead of `int`, `[]string` for JSON-array columns instead of raw JSON strings, `string` for Language instead of `null.String`. The migration is mechanical: per file, swap call sites and adapt field accesses. The `db` package retires its `HumanFull` shape once the last consumer is gone.

**Tech Stack:** Go 1.22+, sqlx, go-sql-driver/mysql. Existing test framework: `go test ./...` from `processor/`. No new dependencies.

## File Structure

Files modified by this plan (no new files):

- `processor/internal/discordbot/bot.go` — pass humanStore into NewReconciliation
- `processor/internal/discordbot/reconciliation.go` — store field, ListByType, Get, Create, full ReconcileUser type migration
- `processor/internal/discordbot/channel.go` — Get, Create, CreateDefaultProfile
- `processor/internal/discordbot/webhook.go` — Get, Create, CreateDefaultProfile
- `processor/internal/discordbot/autocreate.go` — Create, CreateDefaultProfile
- `processor/internal/telegrambot/bot.go` — pass humanStore into NewTelegramReconciliation
- `processor/internal/telegrambot/reconciliation.go` — store field, ListByType, Get, Create, full type migration
- `processor/internal/telegrambot/channel.go` — Get, Create, CreateDefaultProfile
- `processor/internal/api/humans.go` — 6 Get sites, Create, CreateDefaultProfile
- `processor/internal/api/trackingAll.go` — 2 Get sites
- `processor/internal/db/human_queries.go` — DELETE HumanFull, SelectOneHumanFull, CreateHuman, CreateDefaultProfile, HumanFullColumns

Field-shape mapping reference (used throughout):

| `db.HumanFull` | `store.Human` | Migration pattern |
|----------------|---------------|-------------------|
| `Enabled int` (0/1) | `Enabled bool` | `h.Enabled == 0` → `!h.Enabled`; `h.Enabled = 1` → `h.Enabled = true` |
| `AdminDisable int` (0/1) | `AdminDisable bool` | `h.AdminDisable == 0` → `!h.AdminDisable`; `h.AdminDisable == 1` → `h.AdminDisable` |
| `Area string` (raw JSON) | `Area []string` | `Area: "[]"` → `Area: nil` (or omit field); JSON parsing replaced with direct slice use |
| `CommunityMembership string` (raw JSON) | `CommunityMembership []string` | same — direct slice |
| `AreaRestriction null.String` | `AreaRestriction []string` (nil = unset) | `h.AreaRestriction.Valid` → `h.AreaRestriction != nil`; `h.AreaRestriction.SetValid(jsonStr)` → `h.AreaRestriction = parsedSlice` |
| `BlockedAlerts null.String` | `BlockedAlerts []string` (nil = unset) | `h.BlockedAlerts.ValueOrZero()` → marshal `h.BlockedAlerts` to compare; `SetValid` → `= []string{...}` |
| `Language null.String` | `Language string` (empty = unset) | `h.Language.Valid && h.Language.String != ""` → `h.Language != ""` |
| `DisabledDate null.Time` | `DisabledDate null.Time` | unchanged |
| `LastChecked null.Time` | `LastChecked null.Time` | unchanged |

`bot.UpdateHuman(db, id, fields)` becomes `humanStore.Update(id, fields)` — same `map[string]any` shape, just routed through the store.

`db.CreateDefaultProfile(db, id, name, "[]", 0, 0)` → `humanStore.CreateDefaultProfile(id, name, nil, 0, 0)` (signature: `CreateDefaultProfile(id, name string, areas []string, lat, lon float64)`).

---

## Task 1: Wire `humanStore` into discord and telegram bot constructors (no behavior change)

**Files:**
- Modify: `processor/internal/discordbot/bot.go`
- Modify: `processor/internal/discordbot/reconciliation.go`
- Modify: `processor/internal/telegrambot/bot.go`
- Modify: `processor/internal/telegrambot/reconciliation.go`
- Modify: `processor/cmd/processor/main.go` (call sites that build the bot configs)

This task only adds a field and wires it through constructors. No call site uses it yet. Establishes the plumbing so subsequent tasks can call `r.humanStore.X(...)`.

- [ ] **Step 1: Read the discord bot wiring**

Read `processor/internal/discordbot/bot.go` around the `NewReconciliation(...)` call. Read `processor/internal/discordbot/reconciliation.go` around the `Reconciliation` struct and `NewReconciliation` constructor. Identify the `BotDeps` (or equivalent) struct that holds `DB`, then add a `Humans store.HumanStore` field to that struct. Confirm what the existing dependency injection looks like.

- [ ] **Step 2: Add `humanStore store.HumanStore` field to `Reconciliation`**

In `processor/internal/discordbot/reconciliation.go`, add the field to the struct. Add the corresponding parameter to `NewReconciliation`. Add the import for `store` if not already present (the package path is `github.com/pokemon/poracleng/processor/internal/store`).

```go
type Reconciliation struct {
    session      *discordgo.Session
    db           *sqlx.DB
    humanStore   store.HumanStore
    cfg          *config.Config
    translations *i18n.Bundle
    dtsStore     *dts.TemplateStore
    log          *log.Entry
}

func NewReconciliation(
    session *discordgo.Session,
    dbx *sqlx.DB,
    humanStore store.HumanStore,
    cfg *config.Config,
    translations *i18n.Bundle,
    dtsStore *dts.TemplateStore,
) *Reconciliation {
    return &Reconciliation{
        session:      session,
        db:           dbx,
        humanStore:   humanStore,
        cfg:          cfg,
        translations: translations,
        dtsStore:     dtsStore,
        log:          log.WithField("subsystem", "discord-reconciliation"),
    }
}
```

- [ ] **Step 3: Update `discordbot/bot.go` caller**

Find `NewReconciliation(session, cfg.DB, cfg.Cfg, ...)` (around line 84 of `discordbot/bot.go`). Add the humanStore arg from the BotDeps struct. If BotDeps doesn't have a Humans field yet, add it.

```go
b.reconciliation = NewReconciliation(session, cfg.DB, cfg.Humans, cfg.Cfg, cfg.Translations, cfg.DTS)
```

- [ ] **Step 4: Repeat for telegrambot**

Same surgery in `processor/internal/telegrambot/reconciliation.go` (struct `TelegramReconciliation`, constructor `NewTelegramReconciliation`) and `processor/internal/telegrambot/bot.go`.

- [ ] **Step 5: Update `cmd/processor/main.go` to pass the store**

Find where the discord and telegram bot configs are built and ensure `Humans: humanStore` is included. Search for the BotDeps construction; the `humanStore` variable is already in scope (created earlier in `main.go` and assigned to `proc.humans` and the API's `TrackingDeps`).

- [ ] **Step 6: Verify build**

Run: `cd processor && go build ./...`
Expected: clean build, no errors.

- [ ] **Step 7: Run tests**

Run: `cd processor && go test ./...`
Expected: all packages pass.

- [ ] **Step 8: Commit**

```bash
git add processor/internal/discordbot/bot.go \
        processor/internal/discordbot/reconciliation.go \
        processor/internal/telegrambot/bot.go \
        processor/internal/telegrambot/reconciliation.go \
        processor/cmd/processor/main.go
git commit -m "Inject HumanStore into reconciliation constructors

Plumbing only — no call site uses humanStore yet. Subsequent commits
migrate the consumers off db.HumanFull and onto store.Human."
```

---

## Task 2: Migrate `api/trackingAll.go` to `humanStore.Get`

**Files:**
- Modify: `processor/internal/api/trackingAll.go` (lines 231 and 360)

Smallest migration first. Only Language is read from the result.

- [ ] **Step 1: Read the file context**

Read `processor/internal/api/trackingAll.go` around lines 225-245 and 355-385. Identify how `humanFull.Language.String` and `humanFull.Language.Valid` are used.

- [ ] **Step 2: Replace SelectOneHumanFull at line 231**

Change:
```go
humanFull, err := db.SelectOneHumanFull(deps.DB, human.ID)
```
to:
```go
humanFull, err := deps.Humans.Get(human.ID)
```

`humanFull` is now `*store.Human` (nil if not found).

- [ ] **Step 3: Adapt Language field access**

Replace any reading of `humanFull.Language.String` and `humanFull.Language.Valid` with direct `humanFull.Language` (string; empty == unset). Pattern:

```go
// before
if humanFull.Language.Valid && humanFull.Language.String != "" {
    lang = humanFull.Language.String
}
// after
if humanFull.Language != "" {
    lang = humanFull.Language
}
```

- [ ] **Step 4: Repeat for line 360**

Same surgery: swap the call site and the field access.

- [ ] **Step 5: Verify build**

Run: `cd processor && go build ./...`
Expected: clean.

- [ ] **Step 6: Run tests**

Run: `cd processor && go test ./internal/api/...`
Expected: pass.

- [ ] **Step 7: Commit**

```bash
git add processor/internal/api/trackingAll.go
git commit -m "Route api/trackingAll.go through humanStore.Get"
```

---

## Task 3: Migrate `api/humans.go` to `humanStore`

**Files:**
- Modify: `processor/internal/api/humans.go` (6 SelectOneHumanFull sites at lines 27, 239, 300, 357, 421, 527; 1 db.HumanFull literal at lines 539-548; CreateHuman at line 609; CreateDefaultProfile at line 620)

Larger migration, all in one file. Each handler is independent.

- [ ] **Step 1: Read the create-human handler (lines 520-625)**

Read `HandleCreateHuman` end-to-end. Note where `db.HumanFull` is constructed, fields set, then passed to `db.CreateHuman`. Note the AreaRestriction `null.String` and Language `null.String` set patterns.

- [ ] **Step 2: Replace each SelectOneHumanFull call**

For each of the 6 sites, change `db.SelectOneHumanFull(deps.DB, id)` to `deps.Humans.Get(id)`. The variable shape changes from `*db.HumanFull` to `*store.Human`.

For each site, walk forward in the handler and adapt every field access using the mapping table at the top of this plan. Common patterns in this file:
- `parseMembership(human.CommunityMembership)` — `CommunityMembership` is now `[]string` directly. Delete the parse call; use the slice.
- `human.AreaRestriction.Valid` and `human.AreaRestriction.String` — replace with `human.AreaRestriction != nil` and `human.AreaRestriction` ([]string).
- `humanFull.Language.Valid` / `humanFull.Language.String` — replace as in Task 2.

For any field assignments (e.g. `human.Enabled = 0`), convert ints to bools.

- [ ] **Step 3: Rewrite the literal construction (lines 539-548)**

Replace:
```go
human := &db.HumanFull{
    ID:                  body.ID,
    Type:                body.Type,
    Name:                body.Name,
    Enabled:             1,
    Area:                "[]",
    Latitude:            body.Latitude,
    Longitude:           body.Longitude,
    AdminDisable:        0,
    CurrentProfileNo:    1,
    CommunityMembership: "[]",
}
// then human.Language.SetValid(body.Language) etc.
```
with:
```go
human := &store.Human{
    ID:               body.ID,
    Type:             body.Type,
    Name:             body.Name,
    Enabled:          true,
    Latitude:         body.Latitude,
    Longitude:        body.Longitude,
    CurrentProfileNo: 1,
    Language:         body.Language, // "" if unset
    Notes:            body.Notes,
}
if body.AreaRestriction != "" {
    var ar []string
    if err := json.Unmarshal([]byte(body.AreaRestriction), &ar); err == nil {
        human.AreaRestriction = ar
    }
}
```

(Adapt to whatever shape the request body uses — read the surrounding code to confirm field names.)

- [ ] **Step 4: Replace `db.CreateHuman` and `db.CreateDefaultProfile`**

```go
// before
if err := db.CreateHuman(deps.DB, human); err != nil { ... }
if err := db.CreateDefaultProfile(deps.DB, body.ID, profileName, human.Area, human.Latitude, human.Longitude); err != nil { ... }
// after
if err := deps.Humans.Create(human); err != nil { ... }
if err := deps.Humans.CreateDefaultProfile(body.ID, profileName, human.Area, human.Latitude, human.Longitude); err != nil { ... }
```

`human.Area` is now `[]string` — matches the store signature. If the create handler builds a profile area separately, pass it as `[]string`.

- [ ] **Step 5: Verify build**

Run: `cd processor && go build ./...`
Expected: clean. If not, fix the per-site field-access adaptations one by one.

- [ ] **Step 6: Run tests**

Run: `cd processor && go test ./internal/api/...`
Expected: pass.

- [ ] **Step 7: Commit**

```bash
git add processor/internal/api/humans.go
git commit -m "Route api/humans.go through humanStore (Get, Create, CreateDefaultProfile)"
```

---

## Task 4: Migrate `discordbot/channel.go` to `humanStore`

**Files:**
- Modify: `processor/internal/discordbot/channel.go` (lines 115, 193 SelectOneHumanFull; lines 128-137 literal; line 139 CreateHuman; line 146 CreateDefaultProfile)

Channel commands in `discordbot/` use `b.DB *sqlx.DB` directly. Add `b.HumanStore store.HumanStore` to the bot struct (or BotDeps).

- [ ] **Step 1: Audit the discordbot.Bot struct for HumanStore access**

Read `processor/internal/discordbot/bot.go` to confirm the Bot struct has the HumanStore field already (added in Task 1) or the BotDeps does. If only the Reconciliation has it, also add it to the Bot struct so per-command files like `channel.go` can reach it.

```go
type Bot struct {
    session     *discordgo.Session
    DB          *sqlx.DB
    Humans      store.HumanStore
    // ...
}
```

Wire it in `NewBot` and at the call site in `cmd/processor/main.go`.

- [ ] **Step 2: Replace existence-check SELECTs (lines 115, 193)**

```go
// before
existing, _ := db.SelectOneHumanFull(b.DB, targetID)
// after
existing, _ := b.Humans.Get(targetID)
```

`existing` is now `*store.Human`. If subsequent code only checks `existing != nil`, no further change needed.

- [ ] **Step 3: Rewrite the literal (lines 128-137)**

```go
// before
h := &db.HumanFull{
    ID:                  targetID,
    Type:                bot.TypeDiscordChannel,
    Name:                targetName,
    Enabled:             1,
    Area:                "[]",
    Latitude:            0,
    Longitude:           0,
    AdminDisable:        0,
    CurrentProfileNo:    1,
    CommunityMembership: "[]",
}
if language != "" {
    h.Language.SetValid(language)
}
// after
h := &store.Human{
    ID:               targetID,
    Type:             bot.TypeDiscordChannel,
    Name:             targetName,
    Enabled:          true,
    CurrentProfileNo: 1,
    Language:         language, // "" if unset
}
```

- [ ] **Step 4: Replace CreateHuman and CreateDefaultProfile**

```go
// before
if err := db.CreateHuman(b.DB, h); err != nil { ... }
if err := db.CreateDefaultProfile(b.DB, targetID, targetName, "[]", 0, 0); err != nil { ... }
// after
if err := b.Humans.Create(h); err != nil { ... }
if err := b.Humans.CreateDefaultProfile(targetID, targetName, nil, 0, 0); err != nil { ... }
```

- [ ] **Step 5: Verify build**

Run: `cd processor && go build ./...`
Expected: clean.

- [ ] **Step 6: Run tests**

Run: `cd processor && go test ./internal/discordbot/... ./internal/bot/...`
Expected: pass.

- [ ] **Step 7: Commit**

```bash
git add processor/internal/discordbot/channel.go processor/internal/discordbot/bot.go processor/cmd/processor/main.go
git commit -m "Route discordbot/channel.go through humanStore"
```

---

## Task 5: Migrate `discordbot/webhook.go` to `humanStore`

**Files:**
- Modify: `processor/internal/discordbot/webhook.go` (lines 149, 255 SelectOneHumanFull; lines 227-233 literal; line 235 CreateHuman; line 242 CreateDefaultProfile)

Same pattern as Task 4. The Bot struct already has the Humans field after Task 4.

- [ ] **Step 1: Replace the two SelectOneHumanFull calls**

Use the same pattern as Task 4 step 2. `b.HumanStore.Get(m.ChannelID)`.

- [ ] **Step 2: Rewrite the literal at lines 227-233**

Same shape transformation as Task 4 step 3, adapting for the webhook-specific field set.

- [ ] **Step 3: Replace CreateHuman and CreateDefaultProfile**

Same as Task 4 step 4.

- [ ] **Step 4: Verify build + tests**

```bash
cd processor && go build ./... && go test ./internal/discordbot/...
```
Expected: clean + pass.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/discordbot/webhook.go
git commit -m "Route discordbot/webhook.go through humanStore"
```

---

## Task 6: Migrate `discordbot/autocreate.go` to `humanStore`

**Files:**
- Modify: `processor/internal/discordbot/autocreate.go` (lines 270-276 literal; line 278 CreateHuman; line 285 CreateDefaultProfile)

No SelectOneHumanFull here, only construction + Create.

- [ ] **Step 1: Read the autocreate flow**

Read `processor/internal/discordbot/autocreate.go` around lines 260-290. Note where `targetID`, `targetName`, and any community membership data come from.

- [ ] **Step 2: Rewrite the literal + Create + CreateDefaultProfile**

Same pattern as Task 4 steps 3-4.

- [ ] **Step 3: Verify build + tests**

```bash
cd processor && go build ./... && go test ./internal/discordbot/...
```
Expected: clean + pass.

- [ ] **Step 4: Commit**

```bash
git add processor/internal/discordbot/autocreate.go
git commit -m "Route discordbot/autocreate.go through humanStore"
```

---

## Task 7: Migrate `telegrambot/channel.go` to `humanStore`

**Files:**
- Modify: `processor/internal/telegrambot/channel.go` (lines 121, 164 SelectOneHumanFull; lines 130-141 literal; line 143 CreateHuman; line 151 CreateDefaultProfile)
- Possibly modify: `processor/internal/telegrambot/bot.go` (add Humans to Bot struct if not already)

Mirror of Task 4. Add `Humans store.HumanStore` to the telegram Bot struct if not present (it already exists on `TelegramReconciliation` from Task 1; the per-command files need it too).

- [ ] **Step 1: Audit telegrambot.Bot struct**

Read `processor/internal/telegrambot/bot.go` and confirm `Humans` field on the Bot struct. Add it if missing, wire from main.go.

- [ ] **Step 2: Replace the two SelectOneHumanFull calls (lines 121, 164)**

Same as Task 4 step 2. `b.Humans.Get(targetID)`.

- [ ] **Step 3: Rewrite the literal at lines 130-141**

Same shape transformation as Task 4 step 3. The telegram channel literal also uses `h.AreaRestriction.SetValid(string(areaRestrictionJSON))` — replace with `h.AreaRestriction = areaRestrictionSlice` ([]string).

- [ ] **Step 4: Replace CreateHuman + CreateDefaultProfile**

Same as Task 4 step 4.

- [ ] **Step 5: Verify build + tests**

```bash
cd processor && go build ./... && go test ./internal/telegrambot/...
```
Expected: clean + pass.

- [ ] **Step 6: Commit**

```bash
git add processor/internal/telegrambot/channel.go processor/internal/telegrambot/bot.go processor/cmd/processor/main.go
git commit -m "Route telegrambot/channel.go through humanStore"
```

---

## Task 8: Migrate `discordbot/reconciliation.go` to `humanStore` (the big one)

**Files:**
- Modify: `processor/internal/discordbot/reconciliation.go` (line 138 SELECT batch load, line 223 SelectOneHumanFull, line 644 SELECT batch load, lines 279 + 374 literals, lines 291 + 387 CreateHuman, lines 295 + 391 CreateDefaultProfile, plus per-call-site field migrations through ReconcileUser/reconcileNonAreaSecurity/reconcileAreaSecurity/DisableUser)

Largest single-file migration. The function `ReconcileUser(id string, user *db.HumanFull, ...)` and its helpers `reconcileNonAreaSecurity`, `reconcileAreaSecurity`, `DisableUser` all change parameter types to `*store.Human`. Field accesses use the mapping table.

- [ ] **Step 1: Read the full reconciliation file**

Read `processor/internal/discordbot/reconciliation.go` end-to-end. Note every place `db.HumanFull` appears (parameter type, field access, struct literal). Note the `bot.UpdateHuman(r.db, id, updates)` call at line 345 — this becomes `r.humanStore.Update(id, updates)`. Note the raw `r.db.Exec("UPDATE humans SET admin_disable = ?, disabled_date = ?, blocked_alerts = ? WHERE id = ?", ...)` at line 314 — this becomes a `humanStore.Update(id, map[string]any{"admin_disable": 0, "disabled_date": nil, "blocked_alerts": ...})` call.

- [ ] **Step 2: Replace the SyncDiscordRole batch SELECT (line 138)**

```go
// before
var usersToCheck []db.HumanFull
err := r.db.Select(&usersToCheck, `SELECT `+db.HumanFullColumns+` FROM humans WHERE type = 'discord:user'`)
// after
usersToCheck, err := r.humanStore.ListByType("discord:user")
```

`usersToCheck` is now `[]*store.Human` (note pointer). The downstream loop `for i := range usersToCheck { user := &usersToCheck[i]; ... }` becomes `for _, user := range usersToCheck { ... }` — slice elements are already pointers.

- [ ] **Step 3: Update the admin filter loop**

```go
// before
var filtered []db.HumanFull
for _, u := range usersToCheck {
    if !isAdminID(r.cfg, u.ID) {
        filtered = append(filtered, u)
    }
}
// after
var filtered []*store.Human
for _, u := range usersToCheck {
    if !isAdminID(r.cfg, u.ID) {
        filtered = append(filtered, u)
    }
}
```

- [ ] **Step 4: Change `ReconcileUser` signature and body**

```go
// before
func (r *Reconciliation) ReconcileUser(id string, user *db.HumanFull, discordUser *DiscordUserInfo, syncNames, removeInvalidUsers bool) {
// after
func (r *Reconciliation) ReconcileUser(id string, user *store.Human, discordUser *DiscordUserInfo, syncNames, removeInvalidUsers bool) {
```

Update the call sites in `SyncDiscordRole` (line 164) and `ReconcileSingleUser` (line 230) — both now pass `*store.Human`.

- [ ] **Step 5: Change `reconcileNonAreaSecurity` signature and body**

```go
func (r *Reconciliation) reconcileNonAreaSecurity(id string, user *store.Human, name string, roleList []string, blocked *string, syncNames, removeInvalidUsers bool) {
```

Adapt every field access using the mapping table:
- `user != nil && user.AdminDisable == 0` → `user != nil && !user.AdminDisable`
- `user.AdminDisable == 1 && user.DisabledDate.Valid` → `user.AdminDisable && user.DisabledDate.Valid`
- `user.Name != name` — unchanged (Name is string in both)
- `user.BlockedAlerts.ValueOrZero()` → marshal `user.BlockedAlerts` to compare against `*blocked`. Cleaner: compare slices directly.

```go
// before
blockedStr := nullableStr(blocked)
userBlockedStr := user.BlockedAlerts.ValueOrZero()
if blockedStr != userBlockedStr {
    if blocked != nil {
        updates["blocked_alerts"] = *blocked
    } else {
        updates["blocked_alerts"] = nil
    }
}
// after
// blocked is *string holding JSON; compare to marshaled user.BlockedAlerts
var userBlockedJSON string
if user.BlockedAlerts != nil {
    b, _ := json.Marshal(user.BlockedAlerts)
    userBlockedJSON = string(b)
}
desiredJSON := nullableStr(blocked)
if desiredJSON != userBlockedJSON {
    if blocked != nil {
        updates["blocked_alerts"] = *blocked
    } else {
        updates["blocked_alerts"] = nil
    }
}
```

The `bot.UpdateHuman(r.db, id, updates)` at line 345 stays as-is for now if it accepts a map and uses the `Update` SQL builder; if it specifically wants the `*sqlx.DB`, change to `r.humanStore.Update(id, updates)` instead.

- [ ] **Step 6: Rewrite the create-user literal at lines 279-289**

```go
// before
h := &db.HumanFull{
    ID:                  id,
    Type:                bot.TypeDiscordUser,
    Name:                name,
    Enabled:             1,
    Area:                "[]",
    CommunityMembership: "[]",
}
if blocked != nil {
    h.BlockedAlerts.SetValid(*blocked)
}

if err := db.CreateHuman(r.db, h); err != nil { ... }
if err := db.CreateDefaultProfile(r.db, id, name, "[]", 0, 0); err != nil { ... }
// after
h := &store.Human{
    ID:      id,
    Type:    bot.TypeDiscordUser,
    Name:    name,
    Enabled: true,
}
if blocked != nil {
    var b []string
    if err := json.Unmarshal([]byte(*blocked), &b); err == nil {
        h.BlockedAlerts = b
    }
}

if err := r.humanStore.Create(h); err != nil { ... }
if err := r.humanStore.CreateDefaultProfile(id, name, nil, 0, 0); err != nil { ... }
```

- [ ] **Step 7: Rewrite the reactivate UPDATE (lines 304-318)**

```go
// before
args := []any{0, nil}
setClauses := "admin_disable = ?, disabled_date = ?"
if blocked != nil {
    setClauses += ", blocked_alerts = ?"
    args = append(args, *blocked)
} else {
    setClauses += ", blocked_alerts = ?"
    args = append(args, nil)
}
args = append(args, id)
if _, err := r.db.Exec("UPDATE humans SET "+setClauses+" WHERE id = ?", args...); err != nil { ... }
// after
updates := map[string]any{
    "admin_disable": 0,
    "disabled_date": nil,
}
if blocked != nil {
    updates["blocked_alerts"] = *blocked
} else {
    updates["blocked_alerts"] = nil
}
if err := r.humanStore.Update(id, updates); err != nil { ... }
```

- [ ] **Step 8: Apply the same surgery to `reconcileAreaSecurity`**

Lines 353-440 mirror reconcileNonAreaSecurity but with community/areaRestriction handling. Use the mapping table for `user.AdminDisable`, `user.DisabledDate.Valid`, the literal at lines 374-385, and the CreateHuman/CreateDefaultProfile calls.

- [ ] **Step 9: Apply to `DisableUser`**

Read the function definition (search for `func (r *Reconciliation) DisableUser(`). Change parameter from `*db.HumanFull` to `*store.Human`. Adapt `user.AdminDisable == 0` etc. as above.

- [ ] **Step 10: Replace the SyncDiscordChannels SELECT (line 644)**

```go
// before
var channels []db.HumanFull
err := r.db.Select(&channels, `SELECT `+db.HumanFullColumns+` FROM humans WHERE type = 'discord:channel' AND admin_disable = 0`)
// after
channels, err := r.humanStore.ListByTypeEnabled("discord:channel")
```

Channels is now `[]*store.Human`. Adapt downstream field reads.

- [ ] **Step 11: Replace the lone SelectOneHumanFull at line 223**

```go
// before
user, err := db.SelectOneHumanFull(r.db, id)
// after
user, err := r.humanStore.Get(id)
```

- [ ] **Step 12: Verify build**

Run: `cd processor && go build ./...`
Expected: clean. Compile errors here are likely missed field-access conversions — fix per the mapping table and rebuild.

- [ ] **Step 13: Run tests**

Run: `cd processor && go test ./internal/discordbot/... ./internal/bot/...`
Expected: pass.

- [ ] **Step 14: Commit**

```bash
git add processor/internal/discordbot/reconciliation.go
git commit -m "Route discordbot/reconciliation.go through humanStore

Migrates ReconcileUser, reconcileNonAreaSecurity, reconcileAreaSecurity,
DisableUser, and the two batch SELECTs onto store.Human. Removes raw
db.HumanFull / SELECT * usage from this file."
```

---

## Task 9: Migrate `telegrambot/reconciliation.go` to `humanStore`

**Files:**
- Modify: `processor/internal/telegrambot/reconciliation.go` (line 129 SELECT, line 168 SelectOneHumanFull, lines 205 + 269 literals, lines 223 + 319 CreateHuman, lines 227 + 323 CreateDefaultProfile, line 335 SELECT, plus all field migrations through `SyncTelegramUsers`/`reconcileNonAreaSecurity`/`reconcileAreaSecurity`/`DisableUser`)

Mirror of Task 8.

- [ ] **Step 1: Apply the same migrations as Task 8**

Walk through each step from Task 8 with the telegram equivalents:
- Line 129 SELECT → `r.humanStore.ListByType("telegram:user")`
- Line 335 SELECT → for the OR-pattern (`type = 'telegram:channel' OR type = 'telegram:group'`), use `r.humanStore.ListByTypes([]string{"telegram:channel", "telegram:group"})`. Verify the existing method handles `admin_disable = 0` filtering — `ListByTypes` already filters disabled per the store interface contract.
- Line 168 SelectOneHumanFull → `r.humanStore.Get(id)`
- Each literal → `*store.Human` shape
- Each CreateHuman/CreateDefaultProfile → store equivalents
- Each `*db.HumanFull` parameter → `*store.Human`
- Each field access → mapping table

- [ ] **Step 2: Verify build**

Run: `cd processor && go build ./...`
Expected: clean.

- [ ] **Step 3: Run tests**

Run: `cd processor && go test ./internal/telegrambot/... ./internal/bot/...`
Expected: pass.

- [ ] **Step 4: Commit**

```bash
git add processor/internal/telegrambot/reconciliation.go
git commit -m "Route telegrambot/reconciliation.go through humanStore"
```

---

## Task 10: Delete dead code in `db/human_queries.go`

**Files:**
- Modify: `processor/internal/db/human_queries.go` (delete `HumanFull`, `SelectOneHumanFull`, `CreateHuman`, `CreateDefaultProfile`, `HumanFullColumns`)
- Modify: `processor/internal/store/human_sql.go` (remove the `db.HumanFullColumns` references, replace with a local-package constant if still needed for explicit column lists)

Cleanup pass. After Tasks 1-9, no consumer should reference the deleted symbols.

- [ ] **Step 1: Verify zero references to the symbols slated for deletion**

Run each of these greps. Each MUST return nothing (excluding the file we're about to edit and the test files). If any return matches, fix that consumer first.

```bash
cd processor
grep -rn 'db\.HumanFull\b' --include='*.go' .
grep -rn 'db\.SelectOneHumanFull\b' --include='*.go' .
grep -rn 'db\.CreateHuman\b' --include='*.go' .
grep -rn 'db\.CreateDefaultProfile\b' --include='*.go' .
grep -rn 'db\.HumanFullColumns\b' --include='*.go' .
```

Expected: each command returns no output (or only matches inside `internal/db/` files we control).

- [ ] **Step 2: Move `HumanFullColumns` into `store/human_sql.go` (if still needed there)**

The `store.SQLHumanStore` queries currently reference `db.HumanFullColumns`. Since the store is the only remaining caller, copy the constant into `store/human_sql.go` (renamed to `humanRowColumns`, package-private), and delete the `db.HumanFullColumns` export.

```go
// in store/human_sql.go, near the humanRow struct
const humanRowColumns = `id, type, name, enabled, area, latitude, longitude, fails, ` +
    `last_checked, language, admin_disable, disabled_date, current_profile_no, ` +
    `community_membership, area_restriction, notes, blocked_alerts`
```

Replace every `db.HumanFullColumns` reference in `store/human_sql.go` with `humanRowColumns`.

- [ ] **Step 3: Delete `HumanFull` struct, `HumanFullColumns`, `SelectOneHumanFull`, `CreateHuman`, `CreateDefaultProfile` from `db/human_queries.go`**

Read the current file. Delete:
- `type HumanFull struct { ... }` (~17 lines)
- `const HumanFullColumns = ...` (~3 lines)
- `func SelectOneHumanFull(...)` (~12 lines)
- `func CreateHuman(...)` (~12 lines)
- `func CreateDefaultProfile(...)` (~10 lines)

Keep `ProfileRow`, `UpdateProfileHours`, `CopyProfile`, `trackingTables`, and `SelectOneHuman` (different from SelectOneHumanFull — uses HumanAPI shape; verify it stays in use).

- [ ] **Step 4: Drop the `db` import from `discordbot/reconciliation.go` and `telegrambot/reconciliation.go` if no longer needed**

After Task 8/9, those files may no longer reference the `db` package. Run:

```bash
go build ./... 2>&1
```

If it complains about unused imports, remove them.

- [ ] **Step 5: Verify build**

Run: `cd processor && go build ./...`
Expected: clean.

- [ ] **Step 6: Run full test suite**

Run: `cd processor && go test ./...`
Expected: every package passes.

- [ ] **Step 7: Commit**

```bash
git add processor/internal/db/human_queries.go \
        processor/internal/store/human_sql.go \
        processor/internal/discordbot/reconciliation.go \
        processor/internal/telegrambot/reconciliation.go
git commit -m "Delete db.HumanFull and friends now that HumanStore owns humans

After migrating every consumer to store.HumanStore, the legacy
db.HumanFull / db.SelectOneHumanFull / db.CreateHuman /
db.CreateDefaultProfile / db.HumanFullColumns symbols have no callers.
The remaining humans-table column constant lives in store_sql.go
where the only SQL still touching humans is implemented."
```

---

## Task 11: Update CLAUDE.md to reflect the new structure

**Files:**
- Modify: `CLAUDE.md`

- [ ] **Step 1: Find any reference to `HumanFull` or `db.SelectOneHumanFull` in CLAUDE.md**

Run: `grep -n 'HumanFull\|SelectOneHumanFull' CLAUDE.md`
Expected: matches in the directory-structure or database section.

- [ ] **Step 2: Update those references**

Replace mentions of `db.HumanFull` consumers with the corresponding `store.HumanStore` description. Add a one-paragraph note in the "Database" or "Store" section: "All humans-table reads and writes outside the store package go through `store.HumanStore`. Direct `*sqlx.DB` use against `humans` is reserved for the store implementation in `processor/internal/store/human_sql.go`."

- [ ] **Step 3: Commit**

```bash
git add CLAUDE.md
git commit -m "CLAUDE.md: document HumanStore as the sole humans-table boundary"
```

---

## Self-Review

This plan covers:
- Constructor wiring for both bots (Task 1)
- Six SelectOneHumanFull sites in api/ (Tasks 2-3)
- Three discordbot per-command files (Tasks 4-6)
- One telegrambot per-command file (Task 7)
- Both reconciliation files with full type migration (Tasks 8-9)
- Cleanup of legacy symbols (Task 10)
- Documentation update (Task 11)

Total: 14 SelectOneHumanFull call sites migrated + 7 literal constructions rewritten + 4 batch SELECTs replaced + 5 legacy `db` symbols deleted + ~15 file modifications across 11 commits.

Each task ends with a green test suite and a self-contained commit. If a task fails midway, the previous commit is a clean rollback point. The bigger refactors (Tasks 8 and 9) are the riskiest and are isolated so they can be reviewed independently.

If a follow-up clean-up is needed (e.g. `bot.UpdateHuman` or other `*sqlx.DB` usages in the bot packages), file as a separate plan.
