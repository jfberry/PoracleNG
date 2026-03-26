# Invasion Enrichment Migration Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move invasion grunt reward/lineup building and event invasion metadata from the JS alerter to the Go processor's per-language enrichment.

**Architecture:** Extend `InvasionTranslate()` in `processor/internal/enrichment/invasion.go` to build the full `gruntRewardsList` (encounter slots with chance percentages and translated pokemon names), `gruntLineupList`, event invasion fields, grunt gender data, and grunt type color/emoji. The alerter's `pokestop.js` will then read these from the enrichment payload instead of doing its own GameData lookups.

**Tech Stack:** Go, existing `gamedata.Grunt` struct (encounters loaded from `resources/rawdata/invasions.json`), `gamedata.UtilData` (types, genders, pokestopEvent), `i18n.Translator` for per-language names.

---

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `processor/internal/enrichment/invasion.go` | Modify | Add reward/lineup/event/gender enrichment to base + translate |
| `processor/internal/enrichment/invasion_test.go` | Create | Test reward building, event invasions, gender, lineup |
| `processor/cmd/processor/invasion.go` | Modify | Pass `perLanguageEnrichment` to sender (currently not sent) |
| `alerter/src/controllers/pokestop.js` | Modify | Remove GameData lookups, read from enrichment |

---

### Task 1: Add grunt type color and emoji key to base enrichment

Currently the processor sets `gruntType` (string like "Grass") and `gruntGender` (int) in base enrichment. The alerter needs `gruntTypeColor` (hex color) and `gruntTypeEmojiKey` for per-platform emoji resolution. Event invasions need `pokestopEvent` data.

**Files:**
- Modify: `processor/internal/enrichment/invasion.go:67-84`

- [ ] **Step 1: Add type color, emoji key, and event invasion fields to base enrichment**

In `Invasion()`, after the existing grunt data block (line 67-84), add:

```go
// Type color and emoji key for template/embed use
if grunt != nil {
    typeName := grunt.Type
    if typeName == "Metal" {
        typeName = "Steel" // JS compatibility
    }
    if typeInfo, ok := gd.Util.Types[typeName]; ok {
        m["gruntTypeColor"] = typeInfo.Color
        m["gruntTypeEmojiKey"] = typeInfo.Emoji
    }
}

// Event invasions (displayType >= 7 with no grunt type)
if gruntTypeID == 0 && displayType >= 7 {
    if evtInfo, ok := gd.Util.PokestopEvent[displayType]; ok {
        m["gruntType"] = evtInfo.Name
        m["gruntTypeColor"] = evtInfo.Color
        m["gruntTypeEmojiKey"] = evtInfo.Emoji
    }
}
```

- [ ] **Step 2: Build and run tests**

Run: `cd processor && go build ./... && go test ./internal/enrichment/... -v -run Invasion`

- [ ] **Step 3: Commit**

```
git add processor/internal/enrichment/invasion.go
git commit -m "Add grunt type color, emoji key, and event invasion fields to base enrichment"
```

---

### Task 2: Add gender enrichment to InvasionTranslate

The alerter looks up `genderDataEng` from `GameData.utilData.genders[gender]`. The processor should provide `genderName` and `genderEmojiKey` in per-language enrichment.

**Files:**
- Modify: `processor/internal/enrichment/invasion.go` (InvasionTranslate function)

- [ ] **Step 1: Add gender fields to InvasionTranslate**

After the grunt name translation (line ~114), add:

```go
// Gender enrichment
gruntGender := toInt(base["gruntGender"])
if genderInfo, ok := gd.Util.Genders[gruntGender]; ok {
    m["genderName"] = tr.T(genderInfo.Name)
    m["genderEmojiKey"] = genderInfo.Emoji
}
```

- [ ] **Step 2: Build and run tests**

Run: `cd processor && go build ./...`

- [ ] **Step 3: Commit**

```
git commit -am "Add gender enrichment to InvasionTranslate"
```

---

### Task 3: Build gruntRewardsList in InvasionTranslate

This is the main enrichment work. The JS builds a `gruntRewardsList` with first/second/third encounter slots, each containing a chance percentage and array of monsters with translated names.

**Logic (from JS `pokestop.js` lines 166-234):**
- If grunt has `secondReward` and second encounters exist: first=85%, second=15%
- Else if grunt has `thirdReward`: use third slot at 100%
- Else: use first slot at 100%
- Each monster entry: `{id, formId, name, formName, fullName}` (translated per language)

Also build `gruntRewards` as a flat text string (e.g. "85%: Swablu, Cacnea\n15%: Lileep, Anorith").

**Files:**
- Modify: `processor/internal/enrichment/invasion.go` (InvasionTranslate function)

- [ ] **Step 1: Add helper function to translate encounter entries**

```go
// translateEncounterSlot translates a list of grunt encounter entries into
// the format expected by DTS templates: [{id, formId, name, formName, fullName}, ...]
func (e *Enricher) translateEncounterSlot(entries []gamedata.GruntEncounterEntry, gd *gamedata.GameData, tr *i18n.Translator) []map[string]any {
    result := make([]map[string]any, 0, len(entries))
    for _, enc := range entries {
        nameInfo := make(map[string]any)
        TranslateMonsterNames(nameInfo, gd, tr, enc.ID, enc.FormID, 0)
        result = append(result, map[string]any{
            "id":       enc.ID,
            "formId":   enc.FormID,
            "name":     nameInfo["name"],
            "formName": nameInfo["formName"],
            "fullName": nameInfo["fullName"],
        })
    }
    return result
}
```

- [ ] **Step 2: Build gruntRewardsList in InvasionTranslate**

After gender enrichment, add the reward list building:

```go
// Grunt rewards structure with translated names
if grunt != nil {
    first := grunt.EncountersByPosition("first")
    second := grunt.EncountersByPosition("second")
    third := grunt.EncountersByPosition("third")

    rewardsList := map[string]any{}

    if grunt.SecondReward && len(second) > 0 {
        // Two reward slots: 85% first, 15% second
        rewardsList["first"] = map[string]any{
            "chance":   85,
            "monsters": e.translateEncounterSlot(first, gd, tr),
        }
        rewardsList["second"] = map[string]any{
            "chance":   15,
            "monsters": e.translateEncounterSlot(second, gd, tr),
        }
    } else if grunt.ThirdReward && len(third) > 0 {
        // Single slot from third position at 100%
        rewardsList["first"] = map[string]any{
            "chance":   100,
            "monsters": e.translateEncounterSlot(third, gd, tr),
        }
    } else if len(first) > 0 {
        // Single slot from first position at 100%
        rewardsList["first"] = map[string]any{
            "chance":   100,
            "monsters": e.translateEncounterSlot(first, gd, tr),
        }
    }
    m["gruntRewardsList"] = rewardsList

    // Also build flat gruntRewards text string for simple templates
    m["gruntRewards"] = buildGruntRewardsText(rewardsList)
}
```

- [ ] **Step 3: Add buildGruntRewardsText helper**

```go
// buildGruntRewardsText builds a flat text summary from gruntRewardsList.
// e.g. "85%: Swablu, Cacnea\n15%: Lileep, Anorith"
func buildGruntRewardsText(rewardsList map[string]any) string {
    var parts []string
    for _, slot := range []string{"first", "second", "third"} {
        slotData, ok := rewardsList[slot].(map[string]any)
        if !ok {
            continue
        }
        monsters, _ := slotData["monsters"].([]map[string]any)
        if len(monsters) == 0 {
            continue
        }
        chance := toInt(slotData["chance"])
        names := make([]string, len(monsters))
        for i, mon := range monsters {
            names[i], _ = mon["fullName"].(string)
        }
        if chance < 100 {
            parts = append(parts, fmt.Sprintf("%d%%: %s", chance, strings.Join(names, ", ")))
        } else {
            parts = append(parts, strings.Join(names, ", "))
        }
    }
    return strings.Join(parts, "\n")
}
```

- [ ] **Step 4: Build and run tests**

Run: `cd processor && go build ./... && go test ./internal/enrichment/...`

- [ ] **Step 5: Commit**

```
git commit -am "Build gruntRewardsList with translated names in InvasionTranslate"
```

---

### Task 4: Write invasion enrichment tests

**Files:**
- Create: `processor/internal/enrichment/invasion_test.go`

- [ ] **Step 1: Write test for grunt rewards list with two slots**

Test grunt type 23 (Grass grunt with secondReward=true): should produce first (85%) and second (15%) slots with translated pokemon names.

- [ ] **Step 2: Write test for grunt rewards list with single slot**

Test a grunt with secondReward=false: should produce single first slot at 100%.

- [ ] **Step 3: Write test for event invasion enrichment**

Test displayType=7 with gruntTypeID=0: should set gruntType, gruntTypeColor, gruntTypeEmojiKey from PokestopEvent data.

- [ ] **Step 4: Write test for gender enrichment**

Test that gruntGender is translated to genderName + genderEmojiKey.

- [ ] **Step 5: Write test for grunt type color and emoji**

Test that gruntTypeColor and gruntTypeEmojiKey are set from type data. Include "Metal" → "Steel" mapping.

- [ ] **Step 6: Run all tests**

Run: `cd processor && go test ./internal/enrichment/... -v -run Invasion`

- [ ] **Step 7: Commit**

```
git add processor/internal/enrichment/invasion_test.go
git commit -m "Add invasion enrichment tests for rewards, events, gender, type color"
```

---

### Task 5: Wire perLanguageEnrichment in invasion handler

Currently `processor/cmd/processor/invasion.go` sends enrichment but no perLanguageEnrichment for invasions (unlike pokemon/raid which do).

**Files:**
- Modify: `processor/cmd/processor/invasion.go`

- [ ] **Step 1: Add per-language enrichment to invasion handler**

After `baseEnrichment := ps.enricher.Invasion(...)`, add the per-language enrichment loop (same pattern as raid.go):

```go
// Compute per-language translated enrichment
var perLang map[string]map[string]any
if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
    perLang = make(map[string]map[string]any)
    for _, lang := range distinctLanguages(matched, ps.cfg.General.Locale) {
        perLang[lang] = ps.enricher.InvasionTranslate(baseEnrichment, gruntTypeID, lang)
    }
}
```

Update the `ps.sender.Send` call to include `PerLanguageEnrichment: perLang`.

**Wait** — check if this is already done. The handler already calls `InvasionTranslate` and sends `PerLanguageEnrichment`. This step may already be complete from the initial enrichment migration. Verify by reading the file first.

- [ ] **Step 2: Build and test**

Run: `cd processor && go build ./... && go test ./...`

- [ ] **Step 3: Commit if changed**

---

### Task 6: Update alerter to read enrichment instead of GameData

**Files:**
- Modify: `alerter/src/controllers/pokestop.js`

- [ ] **Step 1: Replace GameData grunt lookups with enrichment reads**

In the per-user loop (lines 138-254), replace the GameData lookups with reads from `langEnrichment`:
- `data.gruntRewardsList` → read from `langEnrichment.gruntRewardsList`
- `data.gruntRewards` → read from `langEnrichment.gruntRewards`
- `data.gruntTypeColor` → read from `data.gruntTypeColor` (already in base enrichment)
- `data.gruntTypeEmoji` → resolve from `langEnrichment.gruntTypeEmojiKey` or `data.gruntTypeEmojiKey`
- `data.genderDataEng` → read `genderName`/`genderEmojiKey` from langEnrichment

- [ ] **Step 2: Remove the TODO comment at line 162**

- [ ] **Step 3: Test manually with a running instance**

Trigger an invasion alert and verify the Discord/Telegram message shows correct:
- Grunt rewards with translated pokemon names
- Correct chance percentages (85%/15%)
- Grunt type emoji and color
- Gender emoji

- [ ] **Step 4: Commit**

```
git add alerter/src/controllers/pokestop.js
git commit -m "Read invasion rewards from processor enrichment, remove GameData lookups"
```

---

### Task 7: Small gaps — maxbattle weather forecast + gym previous team name

Quick fixes identified in the migration gap analysis.

**Files:**
- Modify: `processor/internal/enrichment/maxbattle.go`
- Modify: `processor/internal/enrichment/gym.go`

- [ ] **Step 1: Add weather forecast to maxbattle base enrichment**

Same pattern as raid.go — add `weatherForecastCurrent`, `weatherForecastNext`, `nextHourTimestamp`, `weatherChangeTime` fields using the AccuWeather forecast provider.

- [ ] **Step 2: Add previous team name to GymTranslate**

In `GymTranslate`, if `oldTeamID >= 0`, add `previousControlName` translated team name.

- [ ] **Step 3: Build and test**

Run: `cd processor && go build ./... && go test ./...`

- [ ] **Step 4: Commit**

```
git commit -am "Add weather forecast to maxbattle, previous team name to gym translate"
```

---

## Notes for Implementation

1. **Translation key format**: Pokemon names use `poke_{id}`, forms use `form_{formId}`. The `TranslateMonsterNames()` helper handles this — use it for encounter entries.

2. **"Metal" → "Steel" mapping**: The JS code maps grunt type "Metal" to "Steel" for type lookups. The Go code needs the same mapping.

3. **JS `encounters.first/second/third`**: The JS accesses encounters as `gruntType.encounters.first` (an object with position as key). The Go `Grunt` struct stores encounters flat with a `Position` field — use `EncountersByPosition("first")` to get the equivalent.

4. **Lineup data**: The webhook `lineup` field is currently `null` in our samples. The lineup building code should handle this gracefully (skip if nil/empty). Golbat may populate this in the future.

5. **The alerter still owns**: emoji resolution (key → platform-specific emoji), `intersection` geocoding, template rendering, field aliases.
