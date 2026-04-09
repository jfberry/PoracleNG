# DTS Remaining Types + Alerter Cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add DTS rendering for all remaining alert types and remove the alerter controller rendering path, leaving the alerter as a pure delivery queue.

**Architecture:** The `dts.Renderer` already has `RenderPokemon()` working. Each remaining type gets a generic `RenderAlert()` method since the pattern is identical across types: resolve tile → build view → select template → render → deliver. The only variant is the template type string and whether the type has per-language enrichment. Weather is the one exception (per-user tile and active pokemon). After all types are migrated, the alerter controllers, handlebars dependency, and `/api/matched` route are removed.

**Tech Stack:** Go, existing dts/enrichment packages, alerter route cleanup.

---

## File Structure

```
processor/internal/dts/
  renderer.go          — Modified: add RenderAlert() generic method + RenderWeather()
  renderer_test.go     — Modified: add tests for new render methods

processor/cmd/processor/
  raid.go              — Modified: add DTS rendering path
  invasion.go          — Modified: add DTS rendering path
  quest.go             — Modified: add DTS rendering path
  lure.go              — Modified: add DTS rendering path
  nest.go              — Modified: add DTS rendering path
  gym.go               — Modified: add DTS rendering path
  fort.go              — Modified: add DTS rendering path
  maxbattle.go         — Modified: add DTS rendering path
  weather.go           — Modified: add DTS rendering path

alerter/src/controllers/   — Removed (after migration complete)
alerter/src/routes/
  postMatched.js           — Removed (after migration complete)
```

---

### Task 1: Generic RenderAlert Method

**Files:**
- Modify: `processor/internal/dts/renderer.go`
- Modify: `processor/internal/dts/renderer_test.go`

All remaining types (except weather) follow the exact same rendering pattern as pokemon but simpler (no per-user enrichment, no encountered variant). Rather than creating 9 separate methods, add one generic `RenderAlert()`.

- [ ] **Step 1: Add RenderAlert to renderer.go**

```go
// RenderAlert renders any alert type that follows the standard pattern:
// base enrichment + optional per-language enrichment → template → delivery jobs.
// templateType is the DTS template type string (e.g. "raid", "egg", "quest", "invasion",
// "lure", "nest", "gym", "fort-update", "maxbattle").
func (r *Renderer) RenderAlert(
    templateType string,
    enrichment map[string]any,
    perLangEnrichment map[string]map[string]any,
    matchedUsers []webhook.MatchedUser,
    matchedAreas []webhook.MatchedArea,
    logReference string,
) []DeliveryJob
```

The implementation is almost identical to `RenderPokemon` but:
- No `isEncountered` flag (always uses `templateType` directly)
- No per-user enrichment (pass nil to view builder)
- No user deduplication (only pokemon needs that for PVP consolidation)

Factor out the shared per-user rendering loop from `RenderPokemon` into a private helper so both methods share the same code path. The helper signature:

```go
func (r *Renderer) renderForUsers(
    templateType string,
    enrichment map[string]any,
    perLangEnrichment map[string]map[string]any,
    perUserEnrichment map[string]map[string]any,
    users []webhook.MatchedUser,
    areas []webhook.MatchedArea,
    logReference string,
) []DeliveryJob
```

Then `RenderPokemon` calls `renderForUsers` after deduplication and template type selection. `RenderAlert` calls `renderForUsers` directly.

- [ ] **Step 2: Add RenderWeather for weather's unique per-user pattern**

```go
// RenderWeather renders a weather change alert. Weather is unique because
// per-language enrichment can vary per user (different active pokemon lists)
// and may provide per-user static map tiles.
func (r *Renderer) RenderWeather(
    enrichment map[string]any,
    perUserLangEnrichment map[string]map[string]any, // userId → lang enrichment (unique per user)
    matchedUsers []webhook.MatchedUser,
    matchedAreas []webhook.MatchedArea,
    logReference string,
) []DeliveryJob
```

Weather renders one job per user, looking up that user's specific per-language enrichment from the map keyed by user ID.

- [ ] **Step 3: Write tests**

Test cases:
- `RenderAlert("raid", ...)` with simple enrichment → produces delivery job with correct template type
- `RenderAlert("fort-update", ...)` with no per-language enrichment → still works
- `RenderAlert` with multiple users → one job per user
- `RenderWeather` with per-user enrichment → correct per-user data in each job
- Refactored `RenderPokemon` still passes all existing tests

- [ ] **Step 4: Run tests**

```bash
cd processor && go test -v ./internal/dts/ -run TestRender
```

- [ ] **Step 5: Commit**

```bash
git add processor/internal/dts/renderer.go processor/internal/dts/renderer_test.go
git commit -m "feat(dts): add generic RenderAlert and RenderWeather methods"
```

---

### Task 2: Wire Remaining Handlers — Raid, Invasion, Quest, Lure

**Files:**
- Modify: `processor/cmd/processor/raid.go`
- Modify: `processor/cmd/processor/invasion.go`
- Modify: `processor/cmd/processor/quest.go`
- Modify: `processor/cmd/processor/lure.go`

Each handler gets the same pattern inserted before the existing `ps.sender.Send()` call:

```go
if ps.dtsRenderer != nil {
    // Resolve pending tile
    if tilePending != nil {
        wait := time.Until(tilePending.Deadline)
        if wait <= 0 {
            wait = time.Millisecond
        }
        select {
        case url := <-tilePending.Result:
            tilePending.Apply(url)
        case <-time.After(wait):
            tilePending.Apply(tilePending.Fallback)
        }
    }
    jobs := ps.dtsRenderer.RenderAlert(
        msgType,           // "raid", "egg", "invasion", "quest", or "lure"
        baseEnrichment,
        perLang,
        matched,
        matchedAreas,
        logReference,
    )
    if len(jobs) > 0 {
        if err := ps.sender.DeliverMessages(jobs); err != nil {
            l.Errorf("Failed to deliver rendered messages: %s", err)
        }
    }
    return nil // skip old path
}
```

For each handler, identify:
- **raid.go**: `msgType` is either `"raid"` or `"egg"` (already computed). Variables: `baseEnrichment`, `perLang`, `matched`, `matchedAreas`, `tilePending`. Log ref: `raid.GymID`.
- **invasion.go**: Template type `"invasion"`. Variables: `baseEnrichment`, `perLang`, `matched`, `matchedAreas`, `tilePending`. Log ref: `inv.PokestopID`.
- **quest.go**: Template type `"quest"`. Variables: `enrichment` (not `baseEnrichment`), `perLang`, `matched`, `matchedAreas`, `tilePending`. Log ref: `quest.PokestopID`.
- **lure.go**: Template type `"lure"`. Variables: `enrichment`, `perLang`, `matched`, `matchedAreas`, `tilePending`. Log ref: `lure.PokestopID`.

- [ ] **Step 1: Add DTS rendering to raid.go**
- [ ] **Step 2: Add DTS rendering to invasion.go**
- [ ] **Step 3: Add DTS rendering to quest.go**
- [ ] **Step 4: Add DTS rendering to lure.go**
- [ ] **Step 5: Verify build**

```bash
cd processor && go build ./cmd/processor
```

- [ ] **Step 6: Commit**

```bash
git add processor/cmd/processor/raid.go processor/cmd/processor/invasion.go \
       processor/cmd/processor/quest.go processor/cmd/processor/lure.go
git commit -m "feat: add DTS rendering to raid, invasion, quest, lure handlers"
```

---

### Task 3: Wire Remaining Handlers — Nest, Gym, Fort, Maxbattle

**Files:**
- Modify: `processor/cmd/processor/nest.go`
- Modify: `processor/cmd/processor/gym.go`
- Modify: `processor/cmd/processor/fort.go`
- Modify: `processor/cmd/processor/maxbattle.go`

Same pattern as Task 2. Type-specific notes:

- **nest.go**: Template type `"nest"`. Variables: `enrichment`, `perLang`, `matched`, `matchedAreas`, `tilePending`. Log ref: `nest.NestID`.
- **gym.go**: Template type `"gym"`. Variables: `enrichment`, `perLang`, `matched`, `matchedAreas`, `tilePending`. Log ref: `gymID`.
- **fort.go**: Template type `"fort-update"`. **No per-language enrichment** — pass `nil` for perLang. Variables: `enrichment`, `matched`, `matchedAreas`, `tilePending`. Log ref: `fortID`.
- **maxbattle.go**: Template type `"maxbattle"`. Variables: `enrichment`, `perLang`, `matched`, `matchedAreas`, `tilePending`. Log ref: `mb.ID`.

- [ ] **Step 1: Add DTS rendering to nest.go**
- [ ] **Step 2: Add DTS rendering to gym.go**
- [ ] **Step 3: Add DTS rendering to fort.go** (note: no perLang)
- [ ] **Step 4: Add DTS rendering to maxbattle.go**
- [ ] **Step 5: Verify build**

```bash
cd processor && go build ./cmd/processor
```

- [ ] **Step 6: Commit**

```bash
git add processor/cmd/processor/nest.go processor/cmd/processor/gym.go \
       processor/cmd/processor/fort.go processor/cmd/processor/maxbattle.go
git commit -m "feat: add DTS rendering to nest, gym, fort, maxbattle handlers"
```

---

### Task 4: Wire Weather Handler

**Files:**
- Modify: `processor/cmd/processor/weather.go`

Weather is different: it already sends one payload per user (lines 115-149 in weather.go). The current code computes per-language enrichment per user (because each user may have different active pokemon affecting the tile).

For DTS rendering, weather needs to:
1. Resolve the base tile pending
2. For each user: compute per-language enrichment (with user's active pokemon), resolve any per-user tile, then render
3. Deliver all jobs

Since weather already loops per user, the DTS rendering integrates into that loop:

```go
if ps.dtsRenderer != nil {
    // Resolve base tile
    if baseTilePending != nil {
        wait := time.Until(baseTilePending.Deadline)
        if wait <= 0 { wait = time.Millisecond }
        select {
        case url := <-baseTilePending.Result:
            baseTilePending.Apply(url)
        case <-time.After(wait):
            baseTilePending.Apply(baseTilePending.Fallback)
        }
    }

    var allJobs []dts.DeliveryJob
    for _, user := range matched {
        // Compute per-language enrichment for this user
        lang := user.Language
        if lang == "" { lang = ps.cfg.General.Locale }
        if lang == "" { lang = "en" }

        var perLang map[string]map[string]any
        if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
            langEnrichment, userTilePending := ps.enricher.WeatherTranslate(...)
            if userTilePending != nil {
                // Resolve per-user tile
                wait := time.Until(userTilePending.Deadline)
                if wait <= 0 { wait = time.Millisecond }
                select {
                case url := <-userTilePending.Result:
                    userTilePending.Apply(url)
                case <-time.After(wait):
                    userTilePending.Apply(userTilePending.Fallback)
                }
            }
            perLang = map[string]map[string]any{lang: langEnrichment}
        }

        jobs := ps.dtsRenderer.RenderAlert(
            "weatherchange",
            baseEnrichment,
            perLang,
            []webhook.MatchedUser{user},
            matchedAreas,
            logReference,
        )
        allJobs = append(allJobs, jobs...)
    }

    if len(allJobs) > 0 {
        if err := ps.sender.DeliverMessages(allJobs); err != nil {
            l.Errorf("Failed to deliver weather messages: %s", err)
        }
    }
    return
}
```

- [ ] **Step 1: Add DTS rendering to weather.go**
- [ ] **Step 2: Verify build**

```bash
cd processor && go build ./cmd/processor
```

- [ ] **Step 3: Commit**

```bash
git add processor/cmd/processor/weather.go
git commit -m "feat: add DTS rendering to weather handler (per-user tile support)"
```

---

### Task 5: Integration Testing

- [ ] **Step 1: Build processor**

```bash
cd processor && go build ./cmd/processor
```

- [ ] **Step 2: Run all dts tests**

```bash
cd processor && go test -v ./internal/dts/
```

- [ ] **Step 3: Manual testing**

Set `render_dts = true` in `config/config.toml`. Start processor + alerter. Verify each alert type renders correctly:

- Pokemon: already working
- Raid: trigger via poracle-test (`!poracle-test raid,1`)
- Egg: trigger via poracle-test (`!poracle-test raid,1` with egg template)
- Quest: trigger via poracle-test (`!poracle-test quest,1`)
- Invasion: trigger via webhook
- Lure: trigger via webhook
- Nest: trigger via poracle-test (`!poracle-test nest,1`)
- Gym: trigger via webhook
- Fort: trigger via webhook
- Maxbattle: trigger via poracle-test (`!poracle-test max-battle,1`)
- Weather: trigger via weather change in area

- [ ] **Step 4: Commit any fixes**

---

### Task 6: Remove Alerter Controller Path

**Files:**
- Remove: `alerter/src/controllers/monster.js`
- Remove: `alerter/src/controllers/raid.js`
- Remove: `alerter/src/controllers/quest.js`
- Remove: `alerter/src/controllers/pokestop.js` (invasion)
- Remove: `alerter/src/controllers/pokestop_lure.js` (lure)
- Remove: `alerter/src/controllers/gym.js`
- Remove: `alerter/src/controllers/nest.js`
- Remove: `alerter/src/controllers/fortupdate.js`
- Remove: `alerter/src/controllers/maxbattle.js`
- Remove: `alerter/src/controllers/weather.js`
- Remove: `alerter/src/controllers/controller.js` (base class)
- Remove: `alerter/src/controllers/common/` (if exists)
- Remove: `alerter/src/routes/postMatched.js`
- Remove: `alerter/src/lib/handlebars.js`
- Remove: `alerter/src/lib/more-handlebars.js`
- Remove: `alerter/src/lib/partials.js`
- Remove: `alerter/src/lib/emojiLookup.js`
- Modify: `alerter/src/app.js` — remove controller instantiation, `processOne()`, `handleMatchedAlarms()`, matchedQueue, hookQueue, and all controller imports
- Modify: `alerter/package.json` — remove `handlebars` and `@budibase/handlebars-helpers` dependencies

**Important**: This task should only be done after ALL types are confirmed working via the processor DTS rendering path. The `render_dts` config flag should be made default `true` or removed (always on).

- [ ] **Step 1: Remove controllers directory**

Delete all files in `alerter/src/controllers/`.

- [ ] **Step 2: Remove postMatched.js route**

Delete `alerter/src/routes/postMatched.js`.

- [ ] **Step 3: Remove handlebars setup files**

Delete `alerter/src/lib/handlebars.js`, `alerter/src/lib/more-handlebars.js`, `alerter/src/lib/partials.js`, `alerter/src/lib/emojiLookup.js`.

- [ ] **Step 4: Clean up app.js**

Remove from `alerter/src/app.js`:
- Controller imports and instantiation (MonsterController, RaidController, etc.)
- `processOne()` function
- `handleMatchedAlarms()` function and its interval
- `matchedQueue`, `hookQueue`, `alarmProcessor` declarations
- The PromiseQueue import and usage
- `fastify.matchedQueue` decoration

Keep: `deliverMessages.js` route (auto-loaded), Discord/Telegram worker setup, message delivery queues.

- [ ] **Step 5: Remove handlebars npm dependencies**

```bash
cd alerter && npm uninstall handlebars @budibase/handlebars-helpers
```

- [ ] **Step 6: Make render_dts always on**

In `processor/cmd/processor/main.go`, remove the `cfg.Processor.RenderDTS` check — always initialize the DTS renderer. Remove the `RenderDTS` config field (or keep it but default to `true`).

- [ ] **Step 7: Verify alerter starts**

```bash
cd alerter && node src/app.js
```

- [ ] **Step 8: Verify full pipeline**

Start processor + alerter. Trigger alerts. Verify delivery works end-to-end.

- [ ] **Step 9: Commit**

```bash
git add -A
git commit -m "feat: remove alerter controllers, handlebars, and /api/matched route

The processor now renders all DTS templates directly. The alerter
is a pure delivery queue receiving pre-rendered messages via
/api/deliverMessages."
```
