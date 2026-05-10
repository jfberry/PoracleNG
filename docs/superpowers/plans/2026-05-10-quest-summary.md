# Quest Summary Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let users opt individual quest tracking rules into "summary mode". Matching quests are buffered (not delivered immediately) until the user's scheduled summary time, when Poracle groups buffered quests by reward, renders one `questSummary` template per group with the shared reward icon and a list of pokestop locations, delivers, and clears the buffer. Users can also force delivery on demand via `!summary quest`.

**Architecture:** A new per-user `summary_schedules` table stores one schedule (active_hours JSON, same shape as profile schedules) per `(user_id, alert_type)` pair. A new `summary_buffer` table holds matched-but-not-delivered quests. The quest matcher checks the rule's new `summary` column: if set, append the matched quest to the buffer instead of producing a delivery job. A summary scheduler goroutine ticks every minute (mirroring the existing profile scheduler), and for each user whose schedule matches the current local time, it loads buffered quests, groups by `(reward_type, reward)`, renders the new `questSummary` DTS template per group, dispatches, and clears the buffer. The `!summary quest` command does the same dispatch path on demand.

**Tech Stack:** Go monorepo (`processor/`). Existing pieces reused: `db/migrations/` (numbered SQL), `store/` (interface + sql impl), `state/` (in-memory snapshot), `cmd/processor/quest.go` (matcher entry), `enrichment/` (per-quest enrichment for templates), `dts/` (template selection + rendering), `internal/bot/commands/` (command handlers), `internal/api/` (REST CRUD).

---

## Decisions baked in (flag if you'd change any)

1. **Schedule format = profile `active_hours` JSON** (`[{day,hours,mins}, …]`, ISO weekday, 24h time, 10-minute trigger window). Reuses `matchesTimeWindow` from `cmd/processor/profiles.go`. Translators / web editor already understand this shape.
2. **Schedule scope** is per-user, per-alert-type. One row per `(humans.id, type='quest')` in `summary_schedules`. Future expansion to other alert types (raids, eggs) is just new rows; no schema change needed.
3. **Per-rule opt-in** is a new `summary tinyint default 0` column on the `quest` tracking table. Distinct from the `clean` bitmask — `summary` changes the *delivery path* (buffer vs immediate), not the *message lifecycle*. Keeping it separate avoids muddling the bitmask further.
4. **Buffer is persisted** in a `summary_buffer` table. Quests have TTH up to 12+ hours; processor restarts shouldn't lose buffered quests. Each row stores **raw webhook bytes** — no per-user pre-enrichment. Two users with the same buffered quest don't share a payload anyway (quests are matched per rule, fanned out per recipient), so caching pre-enriched JSON wins nothing. Storing raw also frees us from "what language was this enriched in?" — re-enrichment at dispatch time uses the user's *current* language.
5. **Grouping key** = `(reward_type, reward)`. Two quests both rewarding "100 stardust" group together. Two quests rewarding different pokemon (Spinda vs Charmander) are separate groups → separate messages.
6. **One message per reward group**. So a user with quests for stardust + spinda + charmander gets three messages at trigger time — each with the reward's icon and an N-location list.
7. **Trigger window**: same 10-minute matching as the profile scheduler. A schedule of `Mon 07:30` fires once between 07:30 and 07:40 each Monday. Subsequent ticks within the window don't re-fire (we mark as delivered when buffer is cleared).
8. **Buffer never delivers stale quests**. At dispatch time we filter out quests whose `expires_at` has passed.
9. **Manual `!summary quest`** clears the buffer atomically — same dispatch path as the scheduled fire, just triggered by user. If buffer is empty, the bot replies "no buffered quests".
10. **Summary delivery skips edit-mode and reply-threading**. Each summary message is a fresh send. Clean (TTH-delete) still applies normally — `clean` bit on the source quest tracking rule propagates to the summary message.
11. **Distance/sorting**: pokestops within a single group are listed in the order they were buffered (FIFO). Templates can re-sort via Handlebars helpers if needed.
12. **No per-rule schedule**. The `summary` flag on the quest rule says "this quest goes into the buffer"; the user's single per-type schedule decides when buffer fires. Simpler than per-rule cron.
13. **Quest tracking rules without `summary` set continue to fire immediately** as today. The summary path is strictly opt-in.
14. **New static-map type `questSummary`** in `[geocoding.static_map_type]`. The view builder hands the tileserver a `points: [{latitude, longitude, name}, ...]` list. Tileserver-side template is admin's responsibility — typically a multi-pin map covering the bounding box of the points. Default config sets `questSummary = "multiStaticMap"` so out-of-the-box it falls through to the existing pokemon-nearby-stops template.

---

## File Structure

**New files:**
- `processor/internal/db/migrations/NNN_summary_schedules.up.sql` and `.down.sql`
- `processor/internal/db/migrations/NNN_summary_buffer.up.sql` and `.down.sql`
- `processor/internal/db/migrations/NNN_quest_summary_column.up.sql` and `.down.sql`
- `processor/internal/store/summary.go` — `SummaryScheduleStore` + `SummaryBufferStore` interfaces
- `processor/internal/store/summary_sql.go` — sqlx implementations
- `processor/internal/store/mock_summary.go` — test double
- `processor/cmd/processor/summary_scheduler.go` — periodic goroutine + dispatch helper
- `processor/internal/bot/commands/summary.go` — `!summary` command
- `processor/internal/api/summary.go` — REST endpoints
- `processor/internal/dts/quest_summary.go` — view builder for grouped quests
- `processor/internal/dts/quest_summary_test.go`

**Modified — schema / data layer:**
- `processor/internal/db/quest.go` — add `Summary int` field to the API/DB struct
- `processor/internal/state/state.go` — load summary schedules into the in-memory snapshot
- `processor/internal/store/tracking.go` — quest-tracking diff propagates `summary`
- `processor/internal/store/tracking_sql.go` — quest CRUD reads/writes `summary`

**Modified — matching / dispatch:**
- `processor/internal/matching/quest.go` — when matching rule has `summary=1`, return separate buckets `Immediate` and `Buffered`
- `processor/cmd/processor/quest.go` — split: immediate matches go to render queue (existing path); buffered matches get inserted into `summary_buffer`

**Modified — DTS:**
- `processor/internal/dts/templates.go` — `questSummary` is a new type string (no code change required; types are open)
- `processor/internal/dts/renderer.go` — `RenderQuestSummary(reward, quests, matched)` — sibling to `RenderQuest`
- `processor/internal/api/dts_fields.go` — register `questSummary` field set
- `fallbacks/dts.json` — ship `questSummary` defaults for Discord and Telegram

**Modified — bot:**
- `processor/internal/bot/commands/quest.go` — accept `summary` keyword on quest tracking
- `processor/internal/i18n/locale/en.json` — `arg.summary`, `cmd.summary`, `msg.summary.usage`, `msg.summary.no_buffered`, `msg.summary.scheduled`, `msg.summary.delivered`

**Modified — main / wiring:**
- `processor/cmd/processor/main.go` — start the summary scheduler goroutine; close it in shutdown
- `processor/internal/config/config.go` — `[tracking] quest_summary_enabled bool` (default true) and `quest_summary_buffer_ttl_hours` (default 24, sweep abandoned buffer rows)
- `processor/internal/api/config_schema.go` — schema entries for the above
- `config/config.example.toml` — document

**Tests:**
- `processor/internal/store/summary_sql_test.go` — store CRUD
- `processor/cmd/processor/summary_scheduler_test.go` — schedule matching, dispatch, buffer clearing
- `processor/internal/matching/quest_test.go` — extend with summary-routing cases
- `processor/internal/bot/commands/summary_test.go` — command parsing
- `processor/internal/dts/quest_summary_test.go` — grouping + view shape

---

## PR 1 — Schema + migrations

**Goal:** New tables + column. No code reads them yet.

### Task 1.1: Add `summary` column to quest tracking

**Files:**
- Create: `processor/internal/db/migrations/NNN_quest_summary_column.up.sql`
- Create: `processor/internal/db/migrations/NNN_quest_summary_column.down.sql`

NNN is the next available number — list `processor/internal/db/migrations/` and pick `last+1`.

- [ ] **Step 1**: Write the up migration:

```sql
ALTER TABLE quest ADD COLUMN summary tinyint(1) NOT NULL DEFAULT 0;
```

- [ ] **Step 2**: Down migration:

```sql
ALTER TABLE quest DROP COLUMN summary;
```

- [ ] **Step 3**: Build + run, confirm migration applies cleanly on a fresh DB and on an existing one.

```
cd processor && go build ./... && go test -count=1 ./internal/db/...
```

(The migrations test should round-trip up→down→up.)

- [ ] **Step 4**: Commit.

```
git add processor/internal/db/migrations/
git commit -m "db: add summary column to quest tracking"
```

### Task 1.2: Create `summary_schedules` table

**Files:**
- Create: `processor/internal/db/migrations/NNN_summary_schedules.up.sql`
- Create: `processor/internal/db/migrations/NNN_summary_schedules.down.sql`

```sql
CREATE TABLE summary_schedules (
  id varchar(64) NOT NULL,
  alert_type varchar(32) NOT NULL,
  active_hours varchar(4096) NOT NULL DEFAULT '[]',
  PRIMARY KEY (id, alert_type),
  CONSTRAINT fk_summary_schedules_human FOREIGN KEY (id) REFERENCES humans(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

- [ ] **Step 1-3**: Same TDD pattern as 1.1.

- [ ] **Step 4**: Commit.

```
git commit -m "db: summary_schedules table"
```

### Task 1.3: Create `summary_buffer` table

**Files:**
- Create: `processor/internal/db/migrations/NNN_summary_buffer.up.sql`
- Create: `processor/internal/db/migrations/NNN_summary_buffer.down.sql`

```sql
CREATE TABLE summary_buffer (
  id varchar(64) NOT NULL,
  alert_type varchar(32) NOT NULL,
  reward_type int NOT NULL,
  reward int NOT NULL,
  pokestop_id varchar(64) NOT NULL,
  payload mediumblob NOT NULL,            -- raw quest webhook bytes; re-enriched at dispatch
  expires_at int unsigned NOT NULL,        -- unix; sweep past this
  created_at int unsigned NOT NULL,
  PRIMARY KEY (id, alert_type, reward_type, reward, pokestop_id),
  KEY idx_summary_buffer_expires (expires_at),
  CONSTRAINT fk_summary_buffer_human FOREIGN KEY (id) REFERENCES humans(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

The composite primary key dedupes: a user can't accumulate the same pokestop's quest twice for the same reward. Quest rotation (midnight) produces a new row with a different `reward_type/reward` instead of overwriting.

- [ ] **Step 1-3**: TDD.

- [ ] **Step 4**: Commit.

```
git commit -m "db: summary_buffer table"
```

---

## PR 2 — Store layer

**Goal:** Typed CRUD for the new tables, isolated behind the `store` interfaces.

### Task 2.1: SummaryScheduleStore interface + sql impl

**Files:**
- Create: `processor/internal/store/summary.go`
- Create: `processor/internal/store/summary_sql.go`
- Test: `processor/internal/store/summary_sql_test.go`

- [ ] **Step 1**: Write failing tests:

```go
// summary_sql_test.go
func TestSummaryScheduleStore_Roundtrip(t *testing.T) {
	s := newTestSummaryScheduleStore(t)
	hours := `[{"day":1,"hours":7,"mins":30}]`
	if err := s.Set("user-1", "quest", hours); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("user-1", "quest")
	if err != nil {
		t.Fatal(err)
	}
	if got.ActiveHours != hours {
		t.Errorf("ActiveHours = %q, want %q", got.ActiveHours, hours)
	}
}

func TestSummaryScheduleStore_Delete(t *testing.T) { ... }
func TestSummaryScheduleStore_ListByType(t *testing.T) { ... }
```

- [ ] **Step 2**: Run, expect undefined.

- [ ] **Step 3**: Define types:

```go
// summary.go
type SummarySchedule struct {
	ID                string
	AlertType         string
	ActiveHours       string
	ParsedActiveHours []db.ActiveHourEntry
}

type SummaryScheduleStore interface {
	Get(id, alertType string) (*SummarySchedule, error)
	Set(id, alertType, activeHoursJSON string) error
	Delete(id, alertType string) error
	ListByType(alertType string) ([]SummarySchedule, error)
}
```

- [ ] **Step 4**: Implement sql impl in `summary_sql.go`. Pattern matches `tracking_sql.go`. Reuse `db.ActiveHourEntry` parsing helper.

- [ ] **Step 5**: Run tests, expect pass.

- [ ] **Step 6**: Commit.

```
git commit -m "store: SummaryScheduleStore (typed CRUD)"
```

### Task 2.2: SummaryBufferStore

**Files:**
- Append to: `processor/internal/store/summary.go`
- Append to: `processor/internal/store/summary_sql.go`
- Append to: `processor/internal/store/summary_sql_test.go`

```go
type BufferedQuest struct {
	ID         string
	AlertType  string
	RewardType int
	Reward     int
	PokestopID string
	Payload    []byte
	ExpiresAt  int64
	CreatedAt  int64
}

type SummaryBufferStore interface {
	Append(q BufferedQuest) error
	List(id, alertType string) ([]BufferedQuest, error)  // all non-expired
	Clear(id, alertType string) error
	SweepExpired(asOf int64) (int, error)                // for periodic cleanup
}
```

Tests cover: append+list, dedup-on-pokestop (re-append updates payload + expires_at), expired filter on List, sweep returns count and removes.

- [ ] **Steps 1-6** as above.

- [ ] **Commit**:

```
git commit -m "store: SummaryBufferStore with dedup-on-pokestop"
```

### Task 2.3: Mock store

**Files:**
- Create: `processor/internal/store/mock_summary.go`

In-memory map-backed implementations of both interfaces. Used by command tests + scheduler tests so they don't need a database.

- [ ] Commit:

```
git commit -m "store: in-memory mock SummaryScheduleStore + SummaryBufferStore"
```

---

## PR 3 — Wire `summary` into quest tracking + state load

**Goal:** Quest tracking rules carry the `summary` flag end-to-end. State snapshot includes summary schedules so matchers can read them without a DB hit.

### Task 3.1: QuestTrackingAPI carries `summary`

**Files:**
- Modify: `processor/internal/db/quest.go` — add `Summary int` field with json + db tags
- Modify: `processor/internal/store/tracking.go`, `tracking_sql.go` — Quest store reads/writes the column
- Modify: `processor/internal/api/trackingQuest.go` (or wherever quest CRUD lives) — accept `summary` in request bodies, propagate to the store

- [ ] **Step 1**: Failing test — POST a quest tracking row with `"summary": true`, GET it back, assert `Summary == 1`.

- [ ] **Step 2-5**: TDD.

- [ ] **Step 6**: Commit.

```
git commit -m "tracking/quest: summary column in API + store"
```

### Task 3.2: state.State carries summary schedules

**Files:**
- Modify: `processor/internal/state/state.go`

Add to `state.State`:

```go
SummarySchedules map[string]map[string]db.ActiveHourEntry // id -> alert_type -> entries
```

Loader reads via `SummaryScheduleStore.ListByType("quest")` (later: any types we want). Atomic swap as today.

- [ ] **Steps 1-5**: TDD.

- [ ] **Step 6**: Commit.

```
git commit -m "state: load summary schedules into the in-memory snapshot"
```

---

## PR 4 — Matcher routing

**Goal:** Quest matching produces two buckets — immediate and buffered. The handler dispatches each appropriately.

### Task 4.1: Quest matcher returns split result

**Files:**
- Modify: `processor/internal/matching/quest.go`
- Test: `processor/internal/matching/quest_test.go`

- [ ] **Step 1**: Failing test — a single quest webhook against two rules (one with summary=0, one with summary=1) returns one MatchedUser in `Immediate` and one in `Buffered`.

- [ ] **Step 2-5**: Refactor `Match` to return `(immediate []MatchedUser, buffered []MatchedUser, areas []MatchedArea)`. Internal matching logic unchanged; only the per-rule split changes.

- [ ] **Step 6**: Update the single caller (`cmd/processor/quest.go`) to unpack three return values.

- [ ] **Step 7**: Commit.

```
git commit -m "matching/quest: split matched users into immediate vs buffered"
```

### Task 4.2: Quest handler buffers buffered matches

**Files:**
- Modify: `processor/cmd/processor/quest.go`

Existing behaviour: matched users → enrich → render → dispatch. New behaviour:

```go
immediate, buffered, areas := ps.questMatcher.Match(processed, st)
// existing path for immediate
// new path for buffered — store raw webhook for later re-enrichment
for _, m := range buffered {
    ps.summaryBuffer.Append(store.BufferedQuest{
        ID:         m.ID,
        AlertType:  "quest",
        RewardType: processed.RewardType,
        Reward:     processed.Reward,
        PokestopID: processed.PokestopID,
        Payload:    raw, // the original webhook bytes
        ExpiresAt:  processed.QuestExpire,
        CreatedAt:  time.Now().Unix(),
    })
}
```

`raw` is the original `json.RawMessage` for this webhook. No per-user enrichment is performed at buffer time — the scheduler does that at dispatch time using the user's *current* language, so language changes between buffer and dispatch transparently work.

- [ ] **Step 1**: Test — a buffered match results in one row in `summary_buffer`, no render-queue dispatch.

- [ ] **Step 2-5**: Implement.

- [ ] **Step 6**: Commit.

```
git commit -m "processor/quest: append buffered matches to summary_buffer"
```

---

## PR 5 — Summary scheduler

**Goal:** A goroutine that ticks every minute, finds users whose schedule matches now, builds + dispatches their summary, and clears the buffer.

### Task 5.1: Scheduler goroutine + tick logic

**Files:**
- Create: `processor/cmd/processor/summary_scheduler.go`
- Test: `processor/cmd/processor/summary_scheduler_test.go`

```go
type SummaryScheduler struct {
    cfg          *config.Config
    state        *state.Manager
    humans       store.HumanStore
    schedules    store.SummaryScheduleStore
    buffer       store.SummaryBufferStore
    dispatch     func(humanID, alertType string)  // callback; the work itself goes here
    stop         chan struct{}
    done         chan struct{}
}

func (s *SummaryScheduler) Start() { go s.loop() }

func (s *SummaryScheduler) Close() { close(s.stop); <-s.done }

func (s *SummaryScheduler) loop() {
    ticker := time.NewTicker(time.Minute)
    defer ticker.Stop()
    defer close(s.done)
    for {
        select {
        case <-s.stop:
            return
        case <-ticker.C:
            s.tick()
        }
    }
}

func (s *SummaryScheduler) tick() {
    snap := s.state.Get()
    for humanID, byType := range snap.SummarySchedules {
        for alertType, entries := range byType {
            human, err := s.humans.Get(humanID)
            if err != nil || human == nil {
                continue
            }
            if !matchesTimeWindow(... entries, human.Latitude, human.Longitude ...) {
                continue
            }
            s.dispatch(humanID, alertType)
        }
    }
}
```

`matchesTimeWindow` is the exact logic from `cmd/processor/profiles.go::isProfileActive`. Extract to a shared helper (`cmd/processor/active_hours.go`) and call from both.

- [ ] **Step 1**: Test the dispatch logic with stubs — pass a fake schedule, fake human, assert dispatch called when `now` is in window.

- [ ] **Step 2-5**: Implement.

- [ ] **Step 6**: Commit.

```
git commit -m "processor: SummaryScheduler — periodic tick over per-user schedules"
```

### Task 5.2: Dispatch implementation

**Files:**
- Append to: `processor/cmd/processor/summary_scheduler.go`

The `dispatch` callback's body:

```go
func (s *SummaryScheduler) deliver(humanID, alertType string) {
    rows, err := s.buffer.List(humanID, alertType)
    if err != nil || len(rows) == 0 {
        return
    }
    // Filter expired
    now := time.Now().Unix()
    fresh := rows[:0]
    for _, r := range rows {
        if r.ExpiresAt > now {
            fresh = append(fresh, r)
        }
    }
    if len(fresh) == 0 {
        s.buffer.Clear(humanID, alertType)
        return
    }
    // Look up the human once for language + location.
    human, _ := s.humans.Get(humanID)
    if human == nil {
        s.buffer.Clear(humanID, alertType)
        return
    }
    lang := human.Language
    if lang == "" {
        lang = s.cfg.General.Locale
    }
    // Re-enrich each buffered raw webhook for this user's current
    // language. Dispatch is rare so the cost is bounded.
    type enrichedEntry struct {
        rewardType, reward int
        view               map[string]any  // per-pokestop view fields
    }
    enriched := make([]enrichedEntry, 0, len(fresh))
    for _, r := range fresh {
        var pokestopView map[string]any
        // s.questEnrichOne reproduces the same per-quest enrichment
        // ProcessQuest does for an immediate alert, scoped to one
        // language. Implementation: parse r.Payload → run base + per-lang
        // → return merged map for the per-pokestop entry.
        pokestopView = s.questEnrichOne(r.Payload, lang)
        if pokestopView == nil {
            continue // malformed payload — skip silently
        }
        enriched = append(enriched, enrichedEntry{r.RewardType, r.Reward, pokestopView})
    }
    if len(enriched) == 0 {
        s.buffer.Clear(humanID, alertType)
        return
    }
    // Group by (reward_type, reward).
    type key struct{ Type, Reward int }
    groups := map[key][]map[string]any{}
    rewards := []key{} // preserve first-seen order across groups
    for _, e := range enriched {
        k := key{e.rewardType, e.reward}
        if _, exists := groups[k]; !exists {
            rewards = append(rewards, k)
        }
        groups[k] = append(groups[k], e.view)
    }
    // For each group, render + dispatch one questSummary message.
    for _, k := range rewards {
        s.renderAndDispatchGroup(human, k.Type, k.Reward, groups[k])
    }
    // Clear after successful dispatch.
    s.buffer.Clear(humanID, alertType)
}
```

`renderAndDispatchGroup` calls `dts.RenderQuestSummary` (added in PR 6) and then enqueues delivery.Jobs. Dispatch is synchronous within the tick; tick rate is once per minute so total throughput is bounded.

- [ ] **Step 1-6**: As above.

- [ ] **Commit**:

```
git commit -m "processor: SummaryScheduler.deliver — group + render + dispatch + clear"
```

### Task 5.3: Buffer expiry sweep

**Files:**
- Append to: `processor/cmd/processor/summary_scheduler.go`

Every N minutes (`config.Tracking.QuestSummaryBufferTTLHours`), call `buffer.SweepExpired(now)`. Keeps the table clean for users whose schedule never fires (e.g. they removed the schedule but quests are still buffered).

A cheap addition — runs on the same goroutine, every 10th tick or so.

- [ ] **Commit**:

```
git commit -m "processor: SummaryScheduler periodic SweepExpired"
```

### Task 5.4: Wire scheduler in main.go

**Files:**
- Modify: `processor/cmd/processor/main.go`

Construct the scheduler, start it after dispatcher start, close it as part of shutdown sequence (between dispatcher.Stop and tracker.Save — same lifetime as profile scheduler).

- [ ] **Commit**:

```
git commit -m "main: start + shutdown SummaryScheduler"
```

---

## PR 6 — `questSummary` template type + renderer

**Goal:** A new DTS type that takes a reward and a list of pokestop entries and renders one message.

### Task 6.1: View builder for grouped quests

**Files:**
- Create: `processor/internal/dts/quest_summary.go`
- Test: `processor/internal/dts/quest_summary_test.go`

```go
// BuildQuestSummaryView returns the template context for one
// reward-group's questSummary message. The reward fields are shared
// across the group; per-pokestop fields live in `quests`. The scheduler
// has already re-enriched each per-pokestop view (one map per quest,
// language-correct).
func BuildQuestSummaryView(
    rewardType, rewardID int,
    perPokestopViews []map[string]any,
    sm *staticmap.Resolver,
    gd *gamedata.GameData,
    tr *i18n.Translator,
) map[string]any {
    // Pull the shared reward icon. Each per-pokestop view already
    // resolved imgUrl (same for every quest in this group), so we copy
    // it from the first entry rather than re-deriving.
    var sharedImg string
    if len(perPokestopViews) > 0 {
        if v, ok := perPokestopViews[0]["imgUrl"].(string); ok {
            sharedImg = v
        }
    }

    // Generate the multi-pin static map URL via the configured tile type.
    var staticMap string
    if sm != nil {
        points := make([]map[string]any, 0, len(perPokestopViews))
        for _, q := range perPokestopViews {
            points = append(points, map[string]any{
                "latitude":  q["latitude"],
                "longitude": q["longitude"],
                "name":      q["name"],
            })
        }
        staticMap = sm.GetPregeneratedTileURL("questSummary", map[string]any{
            "points": points,
        }, "staticMap")
    }

    return map[string]any{
        "rewardType":  rewardType,
        "reward":      rewardID,
        "rewardName":  rewardName(rewardType, rewardID, gd, tr),
        "imgUrl":      sharedImg,
        "staticMap":   staticMap,
        "count":       len(perPokestopViews),
        "quests":      perPokestopViews, // {{#each quests}} ... {{/each}}
    }
}
```

`rewardName` is a small helper that resolves a `(rewardType, reward)` pair into a translated name (stardust amount, item name, pokemon name, etc.) — likely already exists in `enrichment/quest.go`; reuse.

Tests assert: shared fields populated; `quests` slice carries per-pokestop fields; multiple pokestops same reward → one view with N entries; `staticMap` URL contains a points list when the resolver is configured.

- [ ] **Steps 1-6**: TDD.

- [ ] **Commit**:

```
git commit -m "dts: BuildQuestSummaryView — grouped quest template context"
```

### Task 6.2: RenderQuestSummary entry point

**Files:**
- Modify: `processor/internal/dts/renderer.go`

Sibling of `RenderQuest`. Takes the matched user(s) for the destination, the view from BuildQuestSummaryView, the template type `"questSummary"`, builds delivery jobs.

The destination set for a summary is one user (the owner of the schedule). So the matched-users slice is `[]webhook.MatchedUser{ownerEntry}` — synthesize it from the human's row.

- [ ] **Commit**:

```
git commit -m "dts: RenderQuestSummary"
```

### Task 6.3: questSummary tile mode + config wiring

**Files:**
- Modify: `processor/cmd/processor/tilemode.go` — recognise `"questSummary"` template type so tile-mode dispatch handles it (URL / Inline / URLWithBytes work the same as for other types).
- Modify: `processor/internal/config/config.go` — `Geocoding.StaticMapType.QuestSummary string` (or matching field-name pattern with the existing entries).
- Modify: `processor/internal/api/config_schema.go` — schema entry.
- Modify: `config/config.example.toml` — add `questSummary = "multiStaticMap"` under `[geocoding.static_map_type]` with a doc comment that admins can swap to a dedicated tileserver template if they want.

The view builder (Task 6.1) already passes `points: [...]` to the resolver; this task makes sure the named tile type resolves to a real config entry and the tile-mode logic doesn't trip on the new type.

- [ ] **Step 1**: Test — render a questSummary view with the resolver mocked, assert the URL the resolver was asked to build was for tile type `"questSummary"` with a points list.

- [ ] **Step 2-5**: Implement.

- [ ] **Commit**:

```
git commit -m "tile/config: questSummary static-map type"
```

### Task 6.4: Register questSummary in the API field registry

**Files:**
- Modify: `processor/internal/api/dts_fields.go`

Add `"questSummary"` to the type→fields map. Field set: shared reward fields (`rewardName`, `imgUrl`, `count`) + an `each`-able `quests` array whose item shape mirrors the per-pokestop quest fields. Document the nested namespace.

- [ ] **Commit**:

```
git commit -m "api/dts_fields: register questSummary"
```

---

## PR 7 — Bot commands

**Goal:** `!quest <pokemon> summary` opts a quest tracking rule into summary mode. `!summary <type>` (with subcommands `settime`, `cleartime`, `now`, `status`) manages the schedule and forces dispatch.

### Task 7.1: `summary` keyword on `!quest`

**Files:**
- Modify: `processor/internal/bot/commands/quest.go`
- Modify: `processor/internal/i18n/locale/en.json` — `arg.summary`

Mirror the existing keyword pattern (`reply`, `clean`, `edit`). When `arg.summary` is parsed, set `f.summary = 1` on the tracking row.

Validation: summary + edit may conflict (edit means "update in place"; summary means "buffer and group"). Reject the combination with a clear message, mirroring the edit+reply rejection.

- [ ] **Commit**:

```
git commit -m "bot/quest: accept summary keyword"
```

### Task 7.2: New `!summary` command

**Files:**
- Create: `processor/internal/bot/commands/summary.go`

Subcommand router similar to `!profile`:

- `!summary quest` — show current schedule + buffer count
- `!summary quest settime <timestring>` — set schedule (parse same syntax as `!profile settime`)
- `!summary quest cleartime` — remove schedule
- `!summary quest now` — force-dispatch the buffer right now (calls scheduler's deliver)
- `!summary quest status` — alias for the no-arg form

Reuse the time-parser from `!profile settime` — it already accepts e.g. `weekdays 07:30`. Extract it to `bot/commands/active_hours.go` if it's not already shared.

i18n keys:

```json
"cmd.summary": "summary",
"msg.summary.usage": "Usage: `{0}summary quest [settime <times>|cleartime|now|status]`",
"msg.summary.no_buffered": "No buffered quests yet — they'll appear after your next match.",
"msg.summary.scheduled": "Schedule for {0} summaries: {1}",
"msg.summary.unscheduled": "No schedule set for {0} summaries.",
"msg.summary.cleared": "Cleared schedule for {0} summaries.",
"msg.summary.delivered": "Delivered {0} summary message(s) covering {1} reward group(s).",
"msg.summary.no_schedule_set": "Set a schedule first with `{0}summary {1} settime <times>`."
```

- [ ] **Step 1**: Tests for each subcommand — set/clear/show/now.

- [ ] **Step 2-5**: Implement.

- [ ] **Step 6**: Commit.

```
git commit -m "bot: !summary command with set/clear/show/now subcommands"
```

### Task 7.3: Register the command + permissions

**Files:**
- Modify: wherever the command registry is built (likely `bot.go` or `Registry` setup in `cmd/processor/main.go`)

Add `&commands.SummaryCommand{}` to the registry. No special permission — any registered user can manage their own schedule.

- [ ] **Commit**:

```
git commit -m "bot: register !summary in the command registry"
```

---

## PR 8 — REST API

**Goal:** Web editor / external clients can manage schedules and trigger summaries via API.

### Task 8.1: API endpoints

**Files:**
- Create: `processor/internal/api/summary.go`
- Modify: `processor/cmd/processor/main.go` — register routes

```
GET    /api/summaries/{id}                     — list all schedules for user
GET    /api/summaries/{id}/{type}              — one schedule
POST   /api/summaries/{id}/{type}              — set schedule (body: { active_hours })
DELETE /api/summaries/{id}/{type}              — clear schedule
POST   /api/summaries/{id}/{type}/trigger      — force dispatch
GET    /api/summaries/{id}/{type}/buffer       — list buffered items (debug aid)
```

Permission: same as other tracking endpoints — `x-poracle-secret` header check via `RequireSecretGin`.

- [ ] **Steps**: TDD per endpoint. Mock store + scheduler for unit tests.

- [ ] **Commit** (one per logical group):

```
git commit -m "api/summary: schedule CRUD + trigger endpoint"
```

---

## PR 9 — Fallback DTS templates

**Goal:** Ship `questSummary` defaults so admins get a working template out of the box.

### Task 9.1: Discord + Telegram fallback entries

**Files:**
- Modify: `fallbacks/dts.json`

Two new entries (`platform: "discord"`, `platform: "telegram"`), `type: "questSummary"`, `id: 1`, `default: true`. Bodies showcase the shared reward + a `{{#each quests}}` listing pokestop names with a Maps link.

Discord example body:

```handlebars
**{{rewardName}} — {{count}} active**

{{#each quests}}
• [{{name}}]({{googleMapUrl}})
{{/each}}
```

Telegram analog renders the same Markdown — the polling-bot's Markdown→HTML converter handles the conversion.

- [ ] **Commit**:

```
git commit -m "fallbacks: ship questSummary templates (discord + telegram)"
```

---

## PR 10 — Docs

### Task 10.1: User-facing docs

**Files:**
- Modify: `DTS.md` — new section "Quest Summary (`questSummary`)" with the field table for the shared reward fields and the `quests` item shape.
- Modify: `API.md` — document the four new endpoints.
- Modify: `CLAUDE.md` — short paragraph describing the buffer table, scheduler, and `!summary` command. Place it near the existing "Profile scheduler" mention if any.

- [ ] **Commit**:

```
git commit -m "docs: questSummary template + summary scheduling commands"
```

---

## Self-Review Notes

Coverage check against the spec:
- ✅ Schedule per user, per alert type with active_hours JSON (PR 1.2, 2.1)
- ✅ `summary` opt-in on quest tracking rules (PR 1.1, 3.1)
- ✅ Buffer persistence (PR 1.3, 2.2) — survives restart
- ✅ Scheduler ticks and dispatches at scheduled time (PR 5)
- ✅ Group by reward, one message per group (PR 5.2 + PR 6.1)
- ✅ `!quest <pokemon> summary` adds the flag (PR 7.1)
- ✅ `!summary quest now` forces immediate dispatch (PR 7.2)
- ✅ `questSummary` DTS type with `imgUrl` + `staticMap` + `quests` `{{#each}}` (PR 6, PR 9)
- ✅ New `questSummary` static-map tile type (PR 6.3)
- ✅ Fallback templates ship (PR 9)

Open questions / risks:
- **Reward-icon resolution**: `questSummary.imgUrl` is the shared reward icon — the regular per-quest enrichment already populates `imgUrl` for the regular `quest` template, so the view builder copies it from the first per-pokestop view rather than re-deriving.
- **Buffer dedup semantics**: composite PK `(id, alert_type, reward_type, reward, pokestop_id)` covers quest rotation: a pokestop changing reward across midnight produces a new row instead of silently overwriting.
- **Multi-language**: not a concern — payload is raw webhook bytes, scheduler re-enriches at dispatch time using the user's *current* language. Free language-change support.
- **Scheduler tick race**: if the user adds tracking, matches a quest, and the scheduler tick fires within the same minute, the buffer might be cleared empty before the match arrives. The 10-minute trigger window mitigates this; for first-set schedules we could fire at the next window rather than the current one. Not a blocker for v1.
- **Manual `now` while a scheduled fire is mid-flight**: rare. The buffer is cleared atomically; a second trigger sees an empty buffer and replies "no buffered quests". Acceptable.
- **Migration ordering**: PR 1's three migrations must use unique sequential numbers with no gap. Confirm by `ls processor/internal/db/migrations/` before each.

## Manual verification (before merging)

- [ ] `!quest spinda summary` adds a tracking row with `summary=1` (verify in DB)
- [ ] No `!summary` schedule set: matching spinda quests append to `summary_buffer` with no immediate alert
- [ ] `!summary quest settime weekdays 07:30` writes to `summary_schedules`
- [ ] `!summary quest now` dispatches grouped messages and clears the buffer
- [ ] Scheduler tick at the configured time fires identical dispatch
- [ ] Mixed rules (one `summary` + one regular) on the same user: regular fires immediately, summary one buffers
- [ ] Two pokestops with the same Spinda quest: one questSummary message with `count: 2` and a 2-element `quests` array
- [ ] The summary message renders a multi-pin static-map covering both pokestops (verify the URL params or the embedded image in Discord)
- [ ] Quest expires before dispatch: filtered out at delivery time, never sent
- [ ] Restart processor with non-empty buffer + valid schedule: next scheduled fire delivers the buffer
- [ ] `!summary quest cleartime` removes the schedule (subsequent ticks are no-ops)
- [ ] Buffer sweep removes long-stale rows past `quest_summary_buffer_ttl_hours`
- [ ] API endpoints CRUD round-trip via curl
- [ ] Web editor can edit `questSummary` template (via dts_fields.go registration)

## Execution Handoff

Plan saved. Two execution paths:

1. **Subagent-Driven Development** (recommended) — fresh subagent per task, two-stage review per task, fast iteration.
2. **Inline Execution** — execute in this session via superpowers:executing-plans, batch checkpoints.

Pick one when you're ready to start.
