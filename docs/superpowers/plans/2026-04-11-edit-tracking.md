# Edit Tracking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Enable message editing instead of sending new messages for raid RSVP updates, with generic plumbing for future types.

**Architecture:** Reuse existing `clean` column (0-3) as a bitmask: bit 0 = clean (auto-delete), bit 1 = edit (track for edits). The edit infrastructure (EditKey on Job, edit-before-send in FairQueue, Discord/Telegram Edit methods, MessageTracker) is already built — we just need to wire it from webhook handlers through the renderer to delivery. Raids are the first consumer.

**Tech Stack:** Go, sqlx, discordgo, go-telegram-bot-api, raymond (Handlebars)

---

### Task 1: Change Clean from bool to int across matching and delivery

**Files:**
- Modify: `processor/internal/webhook/types.go:184` (MatchedUser.Clean)
- Modify: `processor/internal/webhook/types.go:421` (DeliveryJob.Clean)
- Modify: `processor/internal/delivery/delivery.go:31` (Job.Clean)
- Modify: `processor/internal/matching/generic.go:16` (trackingUserData.Clean)
- Modify: `processor/internal/matching/human.go:193` (raidUserData.Clean)
- Modify: `processor/internal/delivery/queue.go:214-232` (tracking decision)

- [ ] **Step 1: Change MatchedUser.Clean from bool to int**

In `processor/internal/webhook/types.go`, change line 184:
```go
// Before:
Clean             bool                 `json:"clean"`
// After:
Clean             int                  `json:"clean"`
```

- [ ] **Step 2: Change DeliveryJob.Clean from bool to int**

In `processor/internal/webhook/types.go`, change line 421:
```go
// Before:
Clean        bool            `json:"clean"`
// After:
Clean        int             `json:"clean"`
```

Add `EditKey` field to `DeliveryJob`:
```go
Clean        int             `json:"clean"`
EditKey      string          `json:"editKey,omitempty"`
```

- [ ] **Step 3: Change delivery.Job.Clean from bool to int**

In `processor/internal/delivery/delivery.go`, change line 31:
```go
// Before:
Clean        bool            `json:"clean"`
// After:
Clean        int             `json:"clean"`
```

- [ ] **Step 4: Change trackingUserData.Clean from bool to int**

In `processor/internal/matching/generic.go`, change line 16:
```go
// Before:
Clean             bool
// After:
Clean             int
```

- [ ] **Step 5: Change raidUserData.Clean from bool to int**

In `processor/internal/matching/human.go`, change line 193:
```go
// Before:
Clean         bool
// After:
Clean         int
```

- [ ] **Step 6: Fix all matchers that assign Clean**

Every matcher copies `Clean` from DB tracking structs (which are already `int` via `db:` tags). The DB structs in `processor/internal/db/` already use `int` for `Clean`. The matchers may have been converting to bool. Search for all `Clean:` assignments in `processor/internal/matching/` and ensure they assign the int directly. Files to check:
- `generic.go` line 88: `Clean: td.Clean`
- `human.go` lines 89, 177: `Clean: td.Clean`
- `raid.go` lines 97, 159
- `quest.go` line 52
- `gym.go` line 78
- `invasion.go` line 54
- `lure.go` line 42
- `nest.go` line 52
- `maxbattle.go` line 82

- [ ] **Step 7: Update queue.go tracking decision**

In `processor/internal/delivery/queue.go`, change the tracking condition at line 214:
```go
// Before:
if sent != nil && (job.Clean || job.EditKey != "") {
// After:
if sent != nil && (job.Clean > 0 || job.EditKey != "") {
```

And update `TrackedMessage.Clean` to use int — change `Clean bool` to `Clean int` in `tracker.go` and update the eviction callback to check `msg.Clean&1 != 0` for clean deletion:
```go
// In tracker.go eviction callback:
// Before:
if msg.Clean {
// After:
if msg.Clean&1 != 0 {
```

- [ ] **Step 8: Build and fix all compilation errors**

Run: `cd processor && go build ./...`

Fix any remaining bool→int mismatches. The compiler will find them all.

- [ ] **Step 9: Run tests and fix failures**

Run: `cd processor && go test ./...`

Fix any test assertions that check `Clean == true` / `Clean == false` to use `Clean == 1` / `Clean == 0`.

- [ ] **Step 10: Commit**

```bash
git add -u
git commit -m "Change Clean from bool to int for edit tracking bitmask

Bit 0 (1) = auto-delete on TTH expiry
Bit 1 (2) = track for message editing
Values: 0=none, 1=clean, 2=edit, 3=edit+clean"
```

---

### Task 2: Wire EditKey through RenderJob and renderer

**Files:**
- Modify: `processor/cmd/processor/render.go:18-30` (RenderJob struct)
- Modify: `processor/cmd/processor/render.go:106-118` (delivery.Job construction)
- Modify: `processor/internal/dts/renderer.go:289-302` (DeliveryJob construction, non-grouped)
- Modify: `processor/internal/dts/renderer.go:405-417` (DeliveryJob construction, grouped)

- [ ] **Step 1: Add EditKey to RenderJob**

In `processor/cmd/processor/render.go`, add `EditKey` to the `RenderJob` struct:
```go
type RenderJob struct {
    TemplateType      string
    Enrichment        map[string]any
    PerLangEnrichment map[string]map[string]any
    PerUserEnrichment map[string]map[string]any
    WebhookFields     map[string]any
    MatchedUsers      []webhook.MatchedUser
    MatchedAreas      []webhook.MatchedArea
    TilePending       *staticmap.TilePending
    IsEncountered     bool
    IsPokemon         bool
    LogReference      string
    EditKey           string // base key (e.g. "raid:gymID:end"), targetID appended per user
}
```

- [ ] **Step 2: Pass EditKey from DeliveryJob to delivery.Job**

In `processor/cmd/processor/render.go`, update the dispatch loop (around line 106):
```go
for _, j := range jobs {
    ps.dispatcher.Dispatch(&delivery.Job{
        Target:       j.Target,
        Type:         j.Type,
        Message:      j.Message,
        TTH:          tthFromMap(j.TTH),
        Clean:        j.Clean,
        EditKey:      j.EditKey,
        Name:         j.Name,
        LogReference: j.LogReference,
        Lat:          parseCoordFloat(j.Lat),
        Lon:          parseCoordFloat(j.Lon),
    })
}
```

- [ ] **Step 3: Propagate EditKey per-user in renderer (non-grouped path)**

In `processor/internal/dts/renderer.go`, in the non-grouped per-user loop (around line 289), set `EditKey` when the user's `Clean` has the edit bit:
```go
editKey := ""
if user.Clean&2 != 0 && editKeyBase != "" {
    editKey = editKeyBase + ":" + user.ID
}

jobs = append(jobs, webhook.DeliveryJob{
    Lat:          lat,
    Lon:          lon,
    Message:      rawMessage,
    Target:       user.ID,
    Type:         user.Type,
    Name:         user.Name,
    TTH:          tthMap,
    Clean:        user.Clean,
    EditKey:      editKey,
    Emoji:        emojiSlice,
    LogReference: logReference,
    Language:     language,
})
```

The `editKeyBase` parameter needs to be passed into the render functions. Add it as a parameter to `RenderAlert` and `RenderPokemon` — threaded from `RenderJob.EditKey`.

- [ ] **Step 4: Propagate EditKey per-user in renderer (grouped path)**

Same pattern in the grouped rendering path (around line 405):
```go
editKey := ""
if user.Clean&2 != 0 && editKeyBase != "" {
    editKey = editKeyBase + ":" + user.ID
}

jobs = append(jobs, webhook.DeliveryJob{
    // ... existing fields ...
    Clean:   user.Clean,
    EditKey: editKey,
    // ...
})
```

- [ ] **Step 5: Thread editKeyBase through render functions**

Update function signatures:
- `RenderPokemon(...)` — add `editKeyBase string` parameter
- `RenderAlert(...)` — add `editKeyBase string` parameter
- `renderForUsers(...)` — add `editKeyBase string` parameter
- `renderGrouped(...)` — add `editKeyBase string` parameter

Update the call site in `processRenderJob` to pass `job.EditKey`.

- [ ] **Step 6: Build and test**

Run: `cd processor && go build ./... && go test ./...`

- [ ] **Step 7: Commit**

```bash
git add -u
git commit -m "Wire EditKey through RenderJob → renderer → delivery

EditKey flows from webhook handler (base key) through the renderer
(appends :targetID per user when clean&2 is set) to delivery.Job
where the existing edit-before-send infrastructure picks it up."
```

---

### Task 3: Set EditKey in raid handler

**Files:**
- Modify: `processor/cmd/processor/raid.go:172-181` (RenderJob construction)

- [ ] **Step 1: Compute and set EditKey for raids**

In `processor/cmd/processor/raid.go`, before dispatching the RenderJob (around line 172), compute the edit key:
```go
editKey := fmt.Sprintf("raid:%s:%d", raid.GymID, raid.End)

ps.renderCh <- RenderJob{
    TemplateType:      msgType,
    Enrichment:        baseEnrichment,
    PerLangEnrichment: perLang,
    WebhookFields:     webhookFields,
    MatchedUsers:      matched,
    MatchedAreas:      matchedAreas,
    TilePending:       tilePending,
    LogReference:      raid.GymID,
    EditKey:           editKey,
}
```

This key is stable: same gym + same raid end time = same raid. RSVP updates for the same raid produce the same key, so the FairQueue finds the tracked message and edits it.

- [ ] **Step 2: Build and test**

Run: `cd processor && go build ./... && go test ./...`

- [ ] **Step 3: Commit**

```bash
git add processor/cmd/processor/raid.go
git commit -m "Set edit key for raids: raid:{gymID}:{end}

Enables edit-before-send for RSVP updates. First notification sends
normally and is tracked; subsequent RSVP changes edit the existing
message instead of sending a new one."
```

---

### Task 4: Add edit keyword to bot commands

**Files:**
- Modify: `processor/internal/bot/argmatch.go:165-174` (knownKeywordKeys)
- Modify: `processor/internal/bot/commands/helpers.go:260-294` (commonTrackFields)
- Modify: `processor/internal/i18n/locale/en.json`
- Modify: `processor/internal/i18n/locale/de.json`
- Modify: `processor/internal/i18n/locale/es.json`
- Modify: `processor/internal/i18n/locale/fr.json`
- Modify: `processor/internal/i18n/locale/it.json`
- Modify: `processor/internal/i18n/locale/nb-no.json`
- Modify: `processor/internal/i18n/locale/pl.json`
- Modify: `processor/internal/i18n/locale/ru.json`
- Modify: `processor/internal/i18n/locale/sv.json`

- [ ] **Step 1: Add arg.edit to knownKeywordKeys**

In `processor/internal/bot/argmatch.go`, add `"arg.edit"` to the `knownKeywordKeys` slice:
```go
var knownKeywordKeys = []string{
    "arg.remove", "arg.everything", "arg.individually",
    "arg.clean", "arg.shiny", "arg.ex",
    "arg.edit",
    "arg.rsvp", "arg.no_rsvp", "arg.rsvp_only",
    "arg.gmax",
    "arg.pokestop", "arg.gym", "arg.location", "arg.new", "arg.removal", "arg.photo", "arg.include_empty",
    "arg.stardust", "arg.energy", "arg.candy",
    "arg.slot_changes", "arg.battle_changes",
}
```

- [ ] **Step 2: Update commonTrackFields to compute clean as int**

In `processor/internal/bot/commands/helpers.go`, change the `commonTrackFields` struct and `parseCommonTrackFields`:
```go
type commonTrackFields struct {
    Template     string
    Distance     int
    Clean        int
    TemplateWarn string
}

func parseCommonTrackFields(ctx *bot.CommandContext, parsed *bot.ParsedArgs, dtsType string) (*commonTrackFields, *bot.Reply) {
    cleanVal := 0
    if parsed.HasKeyword("arg.clean") {
        cleanVal |= 1
    }
    if parsed.HasKeyword("arg.edit") {
        cleanVal |= 2
    }

    f := &commonTrackFields{
        Template: ctx.DefaultTemplate(),
        Clean:    cleanVal,
    }
    // ... rest unchanged ...
}
```

- [ ] **Step 3: Fix callers of commonTrackFields.Clean**

Every command that uses `common.Clean` currently calls `db.IntBool(common.Clean)` to convert bool→int. Since `Clean` is now `int`, remove the `IntBool` wrapper. Search for `IntBool(common.Clean)` in `processor/internal/bot/commands/` and replace with `common.Clean`:

Files to update: `raid.go`, `track.go`, `egg.go`, `quest.go`, `invasion.go`, `lure.go`, `nest.go`, `gym.go`, `fort.go`, `maxbattle.go`.

- [ ] **Step 4: Add arg.edit to all locale files**

Add `"arg.edit": "edit"` to `en.json`.
Add translated versions to other locale files:
- `de.json`: `"arg.edit": "edit"`
- `es.json`: `"arg.edit": "editar"`
- `fr.json`: `"arg.edit": "edit"`
- `it.json`: `"arg.edit": "edit"`
- `nb-no.json`: `"arg.edit": "edit"`
- `pl.json`: `"arg.edit": "edit"`
- `ru.json`: `"arg.edit": "edit"`
- `sv.json`: `"arg.edit": "edit"`

(Most languages keep "edit" as-is since it's a common loanword in gaming contexts.)

- [ ] **Step 5: Build and test**

Run: `cd processor && go build ./... && go test ./...`

- [ ] **Step 6: Commit**

```bash
git add -u
git commit -m "Add edit keyword to bot commands

!raid pikachu edit → clean=2 (edit only)
!raid pikachu edit clean → clean=3 (edit + clean)
!raid pikachu clean → clean=1 (clean only, unchanged)"
```

---

### Task 5: Update rowtext to display edit flag

**Files:**
- Modify: `processor/internal/rowtext/rowtext.go:24-34` (standardText function)

- [ ] **Step 1: Write test for edit display**

In `processor/internal/rowtext/rowtext_test.go`, add a test:
```go
func TestStandardTextEditFlag(t *testing.T) {
    tr := testTranslator()
    // clean=0: no suffix
    if got := testStandardText(tr, "", "1", 0); strings.Contains(got, "clean") || strings.Contains(got, "edit") {
        t.Errorf("clean=0 should have no suffix, got %q", got)
    }
    // clean=1: "clean"
    if got := testStandardText(tr, "", "1", 1); !strings.Contains(got, "clean") {
        t.Errorf("clean=1 should contain 'clean', got %q", got)
    }
    // clean=2: "edit"
    if got := testStandardText(tr, "", "1", 2); !strings.Contains(got, "edit") || strings.Contains(got, "clean") {
        t.Errorf("clean=2 should contain 'edit' only, got %q", got)
    }
    // clean=3: "edit, clean"
    if got := testStandardText(tr, "", "1", 3); !strings.Contains(got, "edit") || !strings.Contains(got, "clean") {
        t.Errorf("clean=3 should contain both, got %q", got)
    }
}
```

(Adapt helper names to match existing test patterns in the file.)

- [ ] **Step 2: Run test to verify it fails**

Run: `cd processor && go test ./internal/rowtext/ -run TestStandardTextEditFlag -v`

- [ ] **Step 3: Update standardText to handle int clean values**

In `processor/internal/rowtext/rowtext.go`, change `standardText`:
```go
func standardText(tr *i18n.Translator, template, defaultTemplate string, clean int) string {
    var text string
    if template != "" && template != defaultTemplate {
        text += " " + tr.Tf("tracking.template_fmt", template)
    }
    if clean&2 != 0 {
        text += " " + tr.T("tracking.edit")
    }
    if clean&1 != 0 {
        text += " " + tr.T("tracking.clean")
    }
    return text
}
```

- [ ] **Step 4: Update all callers of standardText**

Change all call sites from `bool` to `int` parameter. Files: `monster.go`, `raid.go`, `quest.go`, `invasion.go`, `lure.go`, `nest.go`, `gym.go`, `maxbattle.go` in `processor/internal/rowtext/`.

- [ ] **Step 5: Add tracking.edit to locale files**

Add to `en.json`: `"tracking.edit": "edit"`
Add to other locale files with appropriate translations:
- `es.json`: `"tracking.edit": "editar"`
- `sv.json`: `"tracking.edit": "edit"`
- Others: `"tracking.edit": "edit"`

- [ ] **Step 6: Run tests**

Run: `cd processor && go test ./internal/rowtext/ -v`

- [ ] **Step 7: Commit**

```bash
git add -u
git commit -m "Display edit flag in tracking descriptions

clean=2 shows 'edit', clean=3 shows 'edit clean'"
```

---

### Task 6: Integration test — full edit flow

**Files:**
- Modify: `processor/internal/delivery/queue_test.go`

- [ ] **Step 1: Write integration test for edit-before-send with int Clean**

The existing test at `queue_test.go` lines 230-287 tests edit-before-send but uses `EditKey` directly. Add a test that verifies the clean bitmask interpretation:

```go
func TestEditCleanBitmask(t *testing.T) {
    // clean=2 (edit only): should track for edit but NOT delete on expiry
    // clean=3 (edit+clean): should track for edit AND delete on expiry
    // clean=1 (clean only): should NOT track for edit, but delete on expiry
    // clean=0: should not track at all

    // Test that job with Clean=2 and EditKey set tracks for edit
    // Test that tracked message eviction with Clean=2 does NOT call Delete
    // Test that tracked message eviction with Clean=3 DOES call Delete
}
```

- [ ] **Step 2: Run test**

Run: `cd processor && go test ./internal/delivery/ -run TestEditCleanBitmask -v`

- [ ] **Step 3: Commit**

```bash
git add processor/internal/delivery/queue_test.go
git commit -m "Add integration test for edit tracking bitmask"
```

---

### Task 7: Full build, all tests, manual verification

- [ ] **Step 1: Full build**

Run: `cd processor && go build ./...`

- [ ] **Step 2: Run all tests**

Run: `cd processor && go test ./... -count=1`

- [ ] **Step 3: Commit any remaining fixes**

```bash
git add -u
git commit -m "Fix remaining edit tracking issues from full test run"
```
