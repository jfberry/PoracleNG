# Quest Summary Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let users opt individual quest tracking rules into "summary mode". Matching quests are buffered (not delivered immediately) until the user's scheduled summary time, when Poracle groups buffered quests by reward, renders one `questSummary` template per group with the shared reward icon and a list of pokestop locations, delivers, and clears the buffer. Users can also force delivery on demand via `!summary quest`.

**Architecture:** A new per-user `summary_schedules` table stores one schedule (active_hours JSON, same shape as profile schedules) per `(user_id, alert_type)` pair. The matched-but-not-delivered quests are held in an in-memory buffer (`processor/internal/tracker/summary_buffer.go`) snapshotted to `config/.cache/summary-buffer.json` on graceful shutdown. The quest matcher checks bit 4 of the rule's existing `clean` bitmask: if set, append the matched quest to the buffer instead of producing a delivery job. A summary scheduler goroutine wakes at the same wall-clock minute marks the profile scheduler uses (`[0,10,15,20,30,40,45,50]`), and for each user whose schedule matches the current local time, it loads buffered quests, re-enriches each in the user's current language, groups by `(reward_type, reward, with_ar)`, renders the new `questSummary` DTS template per group with a multi-pin static map autopositioned over the points, dispatches, and clears the buffer. The `!summary quest` command does the same dispatch path on demand.

**Tech Stack:** Go monorepo (`processor/`). Existing pieces reused: `db/migrations/` (numbered SQL), `store/` (interface + sql impl), `state/` (in-memory snapshot), `cmd/processor/quest.go` (matcher entry), `enrichment/` (per-quest enrichment for templates), `dts/` (template selection + rendering), `internal/bot/commands/` (command handlers), `internal/api/` (REST CRUD).

---

## Decisions baked in (flag if you'd change any)

1. **Schedule format = profile `active_hours` JSON** (`[{day,hours,mins}, …]`, ISO weekday, 24h time, 10-minute trigger window). Reuses `matchesTimeWindow` from `cmd/processor/profiles.go`. Translators / web editor already understand this shape.
2. **Schedule scope** is per-user, per-alert-type. One row per `(humans.id, type='quest')` in `summary_schedules`. Future expansion to other alert types (raids, eggs) is just new rows; no schema change needed.
3. **Per-rule opt-in** = bit 4 of the existing `clean` bitmask (so `clean & 4 != 0` means "buffer this rule's matches"). Same band as clean (bit 1) and edit (bit 2) — all are delivery-side decisions made per rule. Reply (bit 4 was reserved for it but ended up implicit) freed the slot. No new column, no migration on the tracking table, no API-shape change. Helper: `db.IsSummary(clean) bool`.
4. **Buffer is in-memory** (`map[id]map[alertType][]BufferedQuest`) with a JSON snapshot to `config/.cache/summary-buffer.json` on graceful shutdown, restored on startup. Same pattern as `delivery-tracker.json` and `gym-state.json`. No per-quest DB writes — the buffer hot path is `sync.Mutex` + map append. Crash recovery loses the in-flight buffer (acceptable: a user misses one cycle). Each entry stores **raw webhook bytes** — no per-user pre-enrichment. Two users' summaries don't share a payload anyway (quests are matched per rule, fanned out per recipient), so caching enrichment buys nothing. Storing raw also frees us from "what language was this enriched in?" — re-enrichment at dispatch time uses the user's *current* language.
5. **Buffer dedup key** = `(reward_type, reward, pokestop_id, with_ar)`. The same pokestop can host one regular quest *and* one AR-required quest at the same time with different rewards; keeping `with_ar` in the dedup PK prevents one from silently overwriting the other.
6. **Grouping key** for the summary message = `(reward_type, reward)`. AR and non-AR quests that happen to share a reward (rare) collapse into one message; each per-pokestop `quests` entry carries its own `withAR` flag so templates can label individual rows. So a user with quests for stardust + spinda + charmander gets three messages at trigger time — each with the reward's icon, a multi-pin map, and an N-location list.
7. **Trigger window**: same 10-minute matching as the profile scheduler, on the same wall-clock minute marks (`[0,10,15,20,30,40,45,50]`). A schedule of `Mon 07:30` fires once at the next matching minute mark on/after 07:30 (so 07:30 itself, since :30 is in the list). Subsequent ticks within the 10-min window don't re-fire — clearing the buffer at delivery is the de-dup signal.
8. **Buffer never delivers stale quests**. At dispatch time we filter out quests whose `expires_at` has passed.
9. **Manual `!summary quest`** clears the buffer atomically — same dispatch path as the scheduled fire, just triggered by user. If buffer is empty, the bot replies "no buffered quests".
10. **Summary delivery skips edit-mode and reply-threading**. Each summary message is a fresh send. Clean (TTH-delete) still applies normally — `clean` bit on the source quest tracking rule propagates to the summary message.
11. **Distance/sorting**: pokestops within a single group are listed in the order they were buffered (FIFO). Templates can re-sort via Handlebars helpers if needed.
12. **No per-rule schedule**. The `summary` flag on the quest rule says "this quest goes into the buffer"; the user's single per-type schedule decides when buffer fires. Simpler than per-rule cron.
13. **Quest tracking rules without `summary` set continue to fire immediately** as today. The summary path is strictly opt-in.
14. **New static-map type `questSummary`** in `[geocoding.static_map_type]`. The view builder runs `staticmap.Autoposition` over the buffered points to compute centre + zoom, then hands the tileserver `points: [{latitude, longitude, name}, ...] + zoom + latitude + longitude` so the rendered tile fits all pins. Tileserver-side template is admin's responsibility — typically a multi-pin map. Default config sets `questSummary = "multiStaticMap"` so out-of-the-box it falls through to the existing pokemon-nearby-stops template.

---

## File Structure

**New files:**
- `processor/internal/db/migrations/NNN_summary_schedules.up.sql` and `.down.sql`
- `processor/internal/store/summary.go` — `SummaryScheduleStore` interface (DB-backed)
- `processor/internal/store/summary_sql.go` — sqlx implementation of the schedule store
- `processor/internal/store/mock_summary.go` — test double for the schedule store
- `processor/internal/tracker/summary_buffer.go` — in-memory buffer + JSON snapshot/restore
- `processor/internal/tracker/summary_buffer_test.go`
- `processor/cmd/processor/summary_scheduler.go` — periodic goroutine + dispatch helper
- `processor/internal/bot/commands/summary.go` — `!summary` command
- `processor/internal/api/summary.go` — REST endpoints
- `processor/internal/dts/quest_summary.go` — view builder for grouped quests
- `processor/internal/dts/quest_summary_test.go`

**Modified — schema / data layer:**
- `processor/internal/db/clean.go` — add `IsSummary(clean) bool` (bit 4 helper)
- `processor/internal/state/state.go` — load summary schedules into the in-memory snapshot

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

### Task 1.1: Add `IsSummary` helper to clean bitmask

**Files:**
- Modify: `processor/internal/db/clean.go`
- Test: `processor/internal/db/clean_test.go`

```go
const cleanBitSummary = 4

// IsSummary reports whether bit 4 of clean is set, opting the rule
// into buffered/grouped delivery via the summary scheduler.
func IsSummary(clean int) bool { return clean&cleanBitSummary != 0 }
```

- [ ] **Step 1**: Test cases for each combination (0/1/2/3/4/5/6/7) of the bitmask, asserting `IsClean`/`IsEdit`/`IsSummary` return the right pair.

- [ ] **Step 2-5**: TDD.

- [ ] **Step 6**: Commit.

```
git commit -m "db: IsSummary bit (clean & 4) for buffered-delivery rules"
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

### Task 2.2: In-memory SummaryBuffer with snapshot persistence

**Files:**
- Create: `processor/internal/tracker/summary_buffer.go`
- Test: `processor/internal/tracker/summary_buffer_test.go`

Lives in `tracker/` alongside the other in-memory trackers (`encounter.go`, `weather.go`, `gym.go`).

```go
package tracker

type BufferedQuest struct {
	RewardType int
	Reward     int
	PokestopID string
	WithAR     bool   // primary (false) vs AR-required (true) quest
	Payload    []byte // raw webhook bytes
	ExpiresAt  int64
	CreatedAt  int64
}

type bufferKey struct {
	RewardType, Reward int
	PokestopID         string
	WithAR             bool
}

// SummaryBuffer holds matched-but-not-delivered quests in memory,
// keyed by (humanID, alertType). Dedup PK = (rewardType, reward,
// pokestopID) so quest rotation produces new entries rather than
// overwriting.
type SummaryBuffer struct {
	mu   sync.Mutex
	data map[string]map[string]map[bufferKey]BufferedQuest
	path string // snapshot path; "" disables persistence
}

func NewSummaryBuffer(snapshotPath string) *SummaryBuffer {
	return &SummaryBuffer{
		data: make(map[string]map[string]map[bufferKey]BufferedQuest),
		path: snapshotPath,
	}
}

func (sb *SummaryBuffer) Append(humanID, alertType string, q BufferedQuest) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	byType := sb.data[humanID]
	if byType == nil {
		byType = make(map[string]map[bufferKey]BufferedQuest)
		sb.data[humanID] = byType
	}
	bucket := byType[alertType]
	if bucket == nil {
		bucket = make(map[bufferKey]BufferedQuest)
		byType[alertType] = bucket
	}
	bucket[bufferKey{q.RewardType, q.Reward, q.PokestopID, q.WithAR}] = q
}

func (sb *SummaryBuffer) List(humanID, alertType string) []BufferedQuest {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	bucket := sb.data[humanID][alertType]
	out := make([]BufferedQuest, 0, len(bucket))
	for _, q := range bucket {
		out = append(out, q)
	}
	return out
}

func (sb *SummaryBuffer) Clear(humanID, alertType string) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	if byType := sb.data[humanID]; byType != nil {
		delete(byType, alertType)
		if len(byType) == 0 {
			delete(sb.data, humanID)
		}
	}
}

// SweepExpired drops entries whose ExpiresAt is past asOf. Returns
// the count removed.
func (sb *SummaryBuffer) SweepExpired(asOf int64) int { ... }

// Save writes the current buffer to the snapshot path as JSON.
// Called on graceful shutdown.
func (sb *SummaryBuffer) Save() error { ... }

// Load restores the buffer from the snapshot path. Missing file is
// not an error. Called at startup.
func (sb *SummaryBuffer) Load() error { ... }
```

Tests cover: append+list, dedup (re-append updates the entry), Clear scope, SweepExpired, Save → Load round-trip, missing-file Load is silent, malformed-file Load is silent (logged but doesn't fail startup).

- [ ] **Steps 1-6**: TDD.

- [ ] **Commit**:

```
git commit -m "tracker: in-memory SummaryBuffer + JSON snapshot persistence"
```

### Task 2.3: Mock SummaryScheduleStore

**Files:**
- Create: `processor/internal/store/mock_summary.go`

In-memory map-backed implementation of `SummaryScheduleStore`. Used by command tests + scheduler tests so they don't need a database. (`SummaryBuffer` is already in-memory; no separate mock needed.)

- [ ] Commit:

```
git commit -m "store: in-memory mock SummaryScheduleStore"
```

---

## PR 3 — State load for summary schedules

**Goal:** State snapshot includes summary schedules so matchers / scheduler can read them without a DB hit. Quest-tracking carries summary as bit 4 of `clean`, which already flows end-to-end via the existing column — no per-rule plumbing needed.

### Task 3.1: state.State carries summary schedules

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

- [ ] **Step 1**: Failing test — a single quest webhook against two rules (one with `clean=0`, one with `clean=4`) returns one MatchedUser in `Immediate` and one in `Buffered`. The split is decided by `db.IsSummary(rule.Clean)`.

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
    ps.summaryBuffer.Append(m.ID, "quest", tracker.BufferedQuest{
        RewardType: processed.RewardType,
        Reward:     processed.Reward,
        PokestopID: processed.PokestopID,
        WithAR:     processed.WithAR,
        Payload:    raw, // the original webhook bytes
        ExpiresAt:  processed.QuestExpire,
        CreatedAt:  time.Now().Unix(),
    })
}
```

`raw` is the original `json.RawMessage` for this webhook. No per-user enrichment is performed at buffer time — the scheduler re-enriches at dispatch time using the user's *current* language, so language changes between buffer and dispatch transparently work.

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

// loop wakes at the same wall-clock minute marks the profile
// scheduler uses (0/10/15/20/30/40/45/50). The 10-minute trigger
// window in matchesTimeWindow lines up with these so a schedule of
// `Mon 07:30` fires at the 07:30 wakeup.
func (s *SummaryScheduler) loop() {
    defer close(s.done)
    for {
        now := time.Now()
        next := nextScheduleTime(now, profileScheduleMinutes) // existing helper
        select {
        case <-s.stop:
            return
        case <-time.After(next.Sub(now)):
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
    rows := s.buffer.List(humanID, alertType)
    if len(rows) == 0 {
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
        view               map[string]any // per-pokestop view fields, withAR injected
    }
    enriched := make([]enrichedEntry, 0, len(fresh))
    for _, r := range fresh {
        // questEnrichOne reproduces the same per-quest enrichment
        // ProcessQuest does for an immediate alert, scoped to one
        // language. Implementation: parse r.Payload → run base + per-lang
        // → return merged map for the per-pokestop entry. Inject
        // withAR onto the view so templates can label this row.
        pokestopView := s.questEnrichOne(r.Payload, lang)
        if pokestopView == nil {
            continue
        }
        pokestopView["withAR"] = r.WithAR
        enriched = append(enriched, enrichedEntry{r.RewardType, r.Reward, pokestopView})
    }
    if len(enriched) == 0 {
        s.buffer.Clear(humanID, alertType)
        return
    }
    // Group by (reward_type, reward). AR-quests sharing a reward with
    // a regular quest collapse into one message; the per-row withAR
    // flag distinguishes individual entries.
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

### Task 5.4: Wire scheduler + buffer in main.go

**Files:**
- Modify: `processor/cmd/processor/main.go`

Construct `tracker.NewSummaryBuffer(filepath.Join(getConfigDir(), ".cache", "summary-buffer.json"))`. Call `Load()` at startup (right after the message tracker loads), pass to the matcher / scheduler. Wire `Save()` into the shutdown sequence between dispatcher.Stop and tracker save (same band as gym-state save). Construct the SummaryScheduler with the buffer, the schedule store, and the dispatch callback.

- [ ] **Commit**:

```
git commit -m "main: wire SummaryBuffer (Load/Save) + SummaryScheduler"
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
// reward-group's questSummary message. Reward fields are shared
// across the group; per-pokestop fields (including each entry's own
// withAR flag) live in `quests`.
func BuildQuestSummaryView(
    rewardType, rewardID int,
    perPokestopViews []map[string]any,
    sm *staticmap.Resolver,
    gd *gamedata.GameData,
    tr *i18n.Translator,
) map[string]any {
    // Shared reward icon — per-pokestop view already has imgUrl resolved.
    var sharedImg string
    if len(perPokestopViews) > 0 {
        if v, ok := perPokestopViews[0]["imgUrl"].(string); ok {
            sharedImg = v
        }
    }

    // Multi-pin static map URL. Autoposition over the points to compute
    // centre + zoom so the tile fits all pins — same helper area maps use.
    var staticMap string
    if sm != nil {
        markers := make([]staticmap.LatLon, 0, len(perPokestopViews))
        points := make([]map[string]any, 0, len(perPokestopViews))
        for _, q := range perPokestopViews {
            lat, _ := q["latitude"].(float64)
            lon, _ := q["longitude"].(float64)
            markers = append(markers, staticmap.LatLon{Latitude: lat, Longitude: lon})
            points = append(points, map[string]any{
                "latitude":  lat,
                "longitude": lon,
                "name":      q["name"],
            })
        }
        pos := staticmap.Autoposition(staticmap.AutopositionShape{
            Markers: markers,
        }, 500, 250, 1.25, 17.5) // mirror !area's autoposition args
        if pos != nil {
            staticMap = sm.GetPregeneratedTileURL("questSummary", map[string]any{
                "points":    points,
                "zoom":      pos.Zoom,
                "latitude":  pos.Latitude,
                "longitude": pos.Longitude,
            }, "staticMap")
        }
    }

    return map[string]any{
        "rewardType": rewardType,
        "reward":     rewardID,
        "rewardName": rewardName(rewardType, rewardID, gd, tr),
        "imgUrl":     sharedImg,
        "staticMap":  staticMap,
        "count":      len(perPokestopViews),
        "quests":     perPokestopViews, // each entry exposes its own withAR
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

Mirror the existing keyword pattern (`clean`, `edit`). When `arg.summary` is parsed, OR bit 4 into `f.clean` (`f.clean |= 4`).

Validation: summary + edit conflict (edit means "update one in-place message"; summary means "buffer and group across many quests"). Reject the combination with a clear message, mirroring the existing combination-validation pattern.

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
- ✅ Schedule per user, per alert type with active_hours JSON (PR 1.2, 2.1) — DB-backed
- ✅ `summary` opt-in on quest tracking rules — bit 4 of `clean` bitmask (PR 1.1)
- ✅ Buffer is in-memory with shutdown-snapshot (PR 2.2) — survives graceful restart, loses on crash (acceptable)
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

- [ ] `!quest spinda summary` adds a tracking row with `clean & 4 != 0` (verify in DB)
- [ ] No `!summary` schedule set: matching spinda quests append to `summary_buffer` with no immediate alert
- [ ] `!summary quest settime weekdays 07:30` writes to `summary_schedules`
- [ ] `!summary quest now` dispatches grouped messages and clears the buffer
- [ ] Scheduler tick at the configured time fires identical dispatch
- [ ] Mixed rules (one `summary` + one regular) on the same user: regular fires immediately, summary one buffers
- [ ] Two pokestops with the same Spinda quest: one questSummary message with `count: 2` and a 2-element `quests` array
- [ ] An AR-required Spinda + a regular Spinda at the same pokestop both buffer (don't overwrite each other) and render in one questSummary message with two `quests` entries — one with `withAR=true`, one with `withAR=false`
- [ ] AR + non-AR quests with *different* rewards at one pokestop render as two separate messages (different reward groups)
- [ ] The summary message renders a multi-pin static-map autopositioned to fit the points (zoom/centre look right, pins visible)
- [ ] Quest expires before dispatch: filtered out at delivery time, never sent
- [ ] Graceful restart with non-empty buffer + valid schedule: snapshot at `config/.cache/summary-buffer.json` exists; on restart it's reloaded and the next scheduled fire delivers it
- [ ] Hard kill (SIGKILL) with non-empty buffer: snapshot wasn't written; buffer is empty after restart (acceptable, documented behaviour)
- [ ] `!summary quest cleartime` removes the schedule (subsequent ticks are no-ops)
- [ ] Buffer sweep removes long-stale rows past `quest_summary_buffer_ttl_hours`
- [ ] API endpoints CRUD round-trip via curl
- [ ] Web editor can edit `questSummary` template (via dts_fields.go registration)

## Execution Handoff

Plan saved. Two execution paths:

1. **Subagent-Driven Development** (recommended) — fresh subagent per task, two-stage review per task, fast iteration.
2. **Inline Execution** — execute in this session via superpowers:executing-plans, batch checkpoints.

Pick one when you're ready to start.
