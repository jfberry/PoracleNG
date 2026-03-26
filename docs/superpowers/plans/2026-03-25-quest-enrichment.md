# Quest Enrichment Migration Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move quest title translation and reward name translation from the JS alerter to the Go processor's per-language enrichment, using identifier keys from `resources/gamelocale/`.

**Architecture:** Add `QuestTranslate()` to `processor/internal/enrichment/quest.go`. Parse quest webhook fields (`title`, `target`, `type`, `conditions`). Translate quest title via `quest_title_{title}` key with `%{amount_0}` placeholder substitution. Build structured reward data (`rewardData`) with translated pokemon, item, and energy/candy names. Build summary strings (`rewardString`, `monsterNames`, `itemNames`, etc.). The alerter reads from enrichment and only does per-platform emoji resolution.

**Tech Stack:** Go, `resources/gamelocale/` identifier keys (`quest_title_*`, `quest_reward_*`, `item_*`, `poke_*`), named placeholder substitution (`%{amount_0}` → target value).

---

## Data Sources

### Quest webhook fields (from Golbat)
```json
{
  "title": "quest_catch_pokemon_plural",     // quest title key suffix
  "type": 4,                                 // quest type ID
  "target": 10,                              // quest target count
  "template": "challenge_catch_easy",        // quest template string
  "conditions": [{"type": 1, "info": {"pokemon_type_ids": [11]}}],
  "rewards": [{"type": 7, "info": {"pokemon_id": 608, "form_id": 2216}}]
}
```

### Translation keys (resources/gamelocale/)
| Key | Example (EN) | Example (DE) |
|-----|-------------|-------------|
| `quest_title_{title}` | "Catch %{amount_0} Pokémon" | "Fange %{amount_0} Pokémon." |
| `item_{id}` | "Razz Berry" | "Himmihbeere" |
| `poke_{id}` | "Lampent" | "Laternecto" |
| `quest_reward_3` | "Stardust" | "Sternenstaub" |
| `quest_reward_4` | "Candy" | "Bonbon" |
| `quest_reward_12` | "Mega Energy" | "Mega Energie" |

### Placeholder format
gamelocale uses `%{name}` named placeholders (NOT `{0}` positional):
- `%{amount_0}` → quest target
- `%{amount}` → reward amount
- `%{pokemon}` → pokemon name

## File Structure

| File | Action | Responsibility |
|------|--------|----------------|
| `processor/internal/webhook/types.go` | Modify | Add `Title`, `Target`, `Type`, `Conditions` to QuestWebhook |
| `processor/internal/i18n/i18n.go` | Modify | Add `FormatNamed()` for `%{name}` placeholder substitution |
| `processor/internal/enrichment/quest.go` | Modify | Add `QuestTranslate()`, base reward structuring |
| `processor/internal/enrichment/quest_enrichment_test.go` | Create | Test quest title, rewards, translations |
| `processor/cmd/processor/quest.go` | Modify | Wire `perLanguageEnrichment` for quests |
| `alerter/src/controllers/quest.js` | Modify | Read from enrichment, remove GameData lookups |

---

### Task 1: Add named placeholder substitution to i18n

The gamelocale translation strings use `%{name}` placeholders (e.g. `%{amount_0}`, `%{pokemon}`). Our i18n package only supports positional `{0}` placeholders. Add a `FormatNamed()` function.

**Files:**
- Modify: `processor/internal/i18n/i18n.go`

- [ ] **Step 1: Add FormatNamed function**

```go
// FormatNamed replaces %{name} placeholders in a string with values from a map.
// Used for gamelocale translations that use named placeholders like %{amount_0}, %{pokemon}.
func FormatNamed(s string, values map[string]string) string {
    for k, v := range values {
        s = strings.ReplaceAll(s, "%{"+k+"}", v)
    }
    return s
}
```

Add a convenience method on Translator:

```go
// TfNamed translates a key and substitutes %{name} placeholders from the map.
func (t *Translator) TfNamed(key string, values map[string]string) string {
    return FormatNamed(t.T(key), values)
}
```

- [ ] **Step 2: Build and test**
- [ ] **Step 3: Commit**

---

### Task 2: Add quest fields to QuestWebhook struct

The webhook struct is missing `title`, `target`, `type`, and `conditions` which are needed for quest title translation.

**Files:**
- Modify: `processor/internal/webhook/types.go`

- [ ] **Step 1: Add fields to QuestWebhook**

```go
type QuestWebhook struct {
    PokestopID string        `json:"pokestop_id"`
    Name       string        `json:"pokestop_name"`
    Latitude   float64       `json:"latitude"`
    Longitude  float64       `json:"longitude"`
    Title      string        `json:"title"`      // quest title key suffix
    Target     int           `json:"target"`     // quest target count
    QuestType  int           `json:"type"`       // quest type ID
    Template   string        `json:"template"`   // quest template string
    Rewards    []QuestReward `json:"rewards"`
}
```

- [ ] **Step 2: Build and test**
- [ ] **Step 3: Commit**

---

### Task 3: Add base reward structuring to Quest enrichment

Structure the raw `rewards` array into `rewardData` with typed fields: `monsters`, `items`, `dustAmount`, `energyMonsters`, `candy`. Also add `shinyPossible` for pokemon rewards.

**Files:**
- Modify: `processor/internal/enrichment/quest.go`

- [ ] **Step 1: Add reward structuring to Quest() base enrichment**

After the existing enrichment, add:

```go
// Structure reward data
rewardData := buildRewardData(rewards)
m["rewardData"] = rewardData
m["dustAmount"] = rewardData.DustAmount
m["itemAmount"] = rewardData.ItemAmount

// Shiny possible for pokemon rewards
if len(rewardData.Monsters) > 0 && e.ShinyProvider != nil {
    m["shinyPossible"] = e.ShinyProvider.IsShinyPossible(
        rewardData.Monsters[0].PokemonID, rewardData.Monsters[0].FormID)
    m["isShiny"] = rewardData.Monsters[0].Shiny
}
```

Define the `RewardData` struct and `buildRewardData` function:

```go
type RewardData struct {
    Monsters       []MonsterReward
    Items          []ItemReward
    DustAmount     int
    ItemAmount     int
    EnergyMonsters []EnergyReward
    Candy          []CandyReward
}

type MonsterReward struct {
    PokemonID int
    FormID    int
    Shiny     bool
}

type ItemReward struct {
    ID     int
    Amount int
}

type EnergyReward struct {
    PokemonID int
    Amount    int
}

type CandyReward struct {
    PokemonID int
    Amount    int
}
```

Build from `matching.QuestRewardData` (reward types: 2=item, 3=stardust, 4=candy, 7=pokemon, 12=mega energy).

- [ ] **Step 2: Build and test**
- [ ] **Step 3: Commit**

---

### Task 4: Add QuestTranslate per-language enrichment

This is the main translation work. Translate quest title, pokemon names, item names, energy names, candy names, and build summary strings.

**Files:**
- Modify: `processor/internal/enrichment/quest.go`

- [ ] **Step 1: Add QuestTranslate function**

The function receives the base enrichment map, the quest webhook, and a language code. It adds:

1. **Quest title**: `questString` — look up `quest_title_{title}` with `%{amount_0}` → target
2. **Monster reward names**: For each monster in `rewardData.Monsters`, translate via `TranslateMonsterNames` → set `name`, `formName`, `fullName` on each. Build `monsterNames` (comma-joined).
3. **Item names**: For each item, translate via `item_{id}` key → set `name`. Build `itemNames` ("6 Razz Berry").
4. **Stardust**: If `dustAmount > 0`, use `quest_reward_3` key for name.
5. **Mega energy names**: For each energy monster, translate pokemon name via `poke_{id}`, use `quest_reward_12` for "Mega Energy". Build `energyMonstersNames` ("10 Charizard Mega Energy").
6. **Candy names**: For each candy monster, translate pokemon name via `poke_{id}`, use `quest_reward_4` for "Candy". Build `candyMonstersNames` ("3 Pikachu Candy").
7. **Reward string**: Join all non-empty parts: monsterNames, stardust, itemNames, energyMonstersNames, candyMonstersNames.
8. **Shiny emoji key**: If `shinyPossible`, set `shinyPossibleEmojiKey` = "shiny".

- [ ] **Step 2: Build and test**
- [ ] **Step 3: Commit**

---

### Task 5: Wire perLanguageEnrichment in quest handler

**Files:**
- Modify: `processor/cmd/processor/quest.go`

- [ ] **Step 1: Add per-language enrichment loop**

After `enrichment, tilePending := ps.enricher.Quest(...)`, add the same pattern as raid/invasion:

```go
var perLang map[string]map[string]any
if ps.enricher.GameData != nil && ps.enricher.Translations != nil {
    perLang = make(map[string]map[string]any)
    for _, lang := range distinctLanguages(matched, ps.cfg.General.Locale) {
        perLang[lang] = ps.enricher.QuestTranslate(enrichment, &quest, lang)
    }
}
```

Pass `PerLanguageEnrichment: perLang` in the `OutboundPayload`.

Need to pass the full `quest` webhook to `QuestTranslate` since it needs `Title` and `Target` for the quest string.

- [ ] **Step 2: Build and test**
- [ ] **Step 3: Commit**

---

### Task 6: Write quest enrichment tests

**Files:**
- Create: `processor/internal/enrichment/quest_enrichment_test.go`

- [ ] **Step 1: Test quest title translation**

Create translations with `quest_title_quest_catch_pokemon_plural: "Catch %{amount_0} Pokémon"`. Call `QuestTranslate` with target=10. Verify `questString` = "Catch 10 Pokémon".

- [ ] **Step 2: Test quest title German**

Same with German: "Fange %{amount_0} Pokémon." → "Fange 10 Pokémon."

- [ ] **Step 3: Test pokemon reward translation**

Quest with pokemon reward (type 7, pokemon_id=25). Verify `monsterNames` contains translated name, `rewardData` monsters have `name`/`fullName`.

- [ ] **Step 4: Test item reward translation**

Quest with item reward (type 2, item_id=701). Verify `itemNames` = "6 Razz Berry".

- [ ] **Step 5: Test stardust reward**

Quest with stardust reward (type 3, amount=200). Verify `dustAmount` = 200, `rewardString` contains stardust text.

- [ ] **Step 6: Test mega energy reward**

Quest with mega energy reward (type 12, pokemon_id=9). Verify `energyMonstersNames` contains pokemon name + "Mega Energy".

- [ ] **Step 7: Test reward string combines all parts**

Quest with multiple reward types. Verify `rewardString` joins them correctly.

- [ ] **Step 8: Commit**

---

### Task 7: Update alerter quest.js to read from enrichment

**Files:**
- Modify: `alerter/src/controllers/quest.js`

- [ ] **Step 1: Replace quest title lookup with enrichment read**

Replace `getQuest()` calls with reads from `langEnrichment.questString`.

- [ ] **Step 2: Replace reward name lookups with enrichment reads**

Replace GameData.monsters/items lookups with reads from `langEnrichment.monsterNames`, `langEnrichment.itemNames`, etc.

- [ ] **Step 3: Remove getQuest() and getReward() methods**

These are no longer needed — processor handles all of this.

- [ ] **Step 4: Commit**

---

### Task 8: Copy quest webhook samples to testdata

Copy representative quest webhooks from `~/Downloads/questhook.txt` to `config/testdata.json` or `fallbacks/testdata.json` so `!poracle-test quest` works.

**Files:**
- Modify: `fallbacks/testdata.json`

- [ ] **Step 1: Add quest test webhooks**

Add 4 quest samples: pokemon reward, item reward, stardust reward, mega energy reward.

- [ ] **Step 2: Commit**

---

## Key Design Decisions

1. **Named placeholders**: gamelocale uses `%{name}` not `{0}`. A `FormatNamed()` helper replaces these from a string→string map. Simple and sufficient.

2. **Identifier keys only**: `quest_title_{title}`, `item_{id}`, `poke_{id}`, `quest_reward_{type}` — no English-as-key dependency. The `resources/locale/` enRefMerged files (`"Stardust"` etc.) are NOT used.

3. **Quest title from webhook `title` field**: Golbat sends `"quest_catch_pokemon_plural"` → full key is `"quest_title_quest_catch_pokemon_plural"`. The `quest_task` MAD field is ignored (MAD obsolete).

4. **Reward data structured in base enrichment**: `buildRewardData()` runs once. Per-language enrichment adds translated names on top. This avoids re-parsing rewards per language.

5. **The alerter's `getReward()` and `getQuest()` methods are removed entirely** — processor does all the work.
