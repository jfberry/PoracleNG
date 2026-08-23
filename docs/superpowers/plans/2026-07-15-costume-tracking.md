# Pokémon Costume Tracking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let operators track Pokémon by costume, display costumes in alerts/`!info`, and expose costume on both API versions.

**Architecture:** Mirror the existing `form` filter — a new `monsters.costume` column filtered in the matcher — but with a `9000`-wildcard so `0` can mean "no costume". Costume names come from the existing `costume_{id}` translations and `costumes.json`; the costume is woven into `fullName` and surfaced in `!info` via the shared `RecentActivity`.

**Tech Stack:** Go, MySQL (sqlx), huma (v2 API), gin (v1 API), raymond/DTS.

## Global Constraints

- **Wildcard sentinel:** `bot.WildcardID = 9000`. Costume: **9000 = any**, **0 = no costume**, **N = that costume**. (Unlike `form`, where 0 = any.)
- **v1 compatibility:** an absent `costume` in a v1 payload MUST default to **9000**, never `0`.
- Pre-commit gate (run from `processor/`): `go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...`. Every commit must pass it.
- Costume is present pre-encounter (`seen_type:wild`), so costume filtering is not gated by the encounter-only-stat-skip rule.
- Costume applies to the **spawn's** name only, never PVP/evolution ranking entries.

---

## File structure

- `internal/gamedata/costumes.go` (new) — `CostumeInfo`, loader, `CostumeTranslationKey`.
- `internal/gamedata/gamedata.go` / loader — hold `Costumes map[int]CostumeInfo`.
- `internal/db/migrations/00XX_add_monster_costume.{up,down}.sql` (new).
- `internal/db/monsters.go` — `MonsterTracking.Costume`, SELECT column.
- `internal/db/tracking_queries.go` — `MonsterTrackingAPI.Costume` + absent→9000 `UnmarshalJSON`.
- `internal/matching/pokemon.go` — `ProcessedPokemon.Costume`, costume filter.
- `internal/enrichment/translate.go` — weave costume into `buildFullName`.
- `internal/enrichment/pokemon.go` — pass costume through; `costumeName`.
- `internal/tracker/recent_activity.go` — `RecordCostume` / `RecentCostumes`.
- `cmd/processor/pokemon.go` — call `RecordCostume`.
- `internal/bot/argmatch.go` — `arg.prefix.costume` + costume name vocabulary.
- `internal/bot/commands/{track,untrack,info}.go` + `internal/rowtext/monster.go`.
- `internal/discordbot/slash/{definitions.go,mappers/track.go,autocomplete/*}`.
- `internal/api/v2_pokemon.go` — `Costume *int`.
- `internal/api/dts_fields.go` + `DTS.md`.
- `internal/i18n/locale/en.json` — new keys.

---

### Task 1: Costume game data

**Files:**
- Create: `internal/gamedata/costumes.go`
- Modify: `internal/gamedata/utildata.go` (or the game-data holder that already loads `pokemon.json`/`forms.json`) to add `Costumes map[int]CostumeInfo` and load `resources/rawdata/costumes.json`
- Test: `internal/gamedata/costumes_test.go`

**Interfaces:**
- Produces: `type CostumeInfo struct { ID int; Name, Proto string; NoEvolve bool }`; `func CostumeTranslationKey(id int) string` → `"costume_{id}"`; `GameData.Costumes map[int]CostumeInfo`.

- [ ] **Step 1: Write the failing test**

```go
// internal/gamedata/costumes_test.go
package gamedata

import "testing"

func TestCostumeTranslationKey(t *testing.T) {
	if got := CostumeTranslationKey(1); got != "costume_1" {
		t.Errorf("CostumeTranslationKey(1) = %q, want costume_1", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/gamedata/ -run TestCostumeTranslationKey`
Expected: FAIL — `undefined: CostumeTranslationKey`.

- [ ] **Step 3: Write minimal implementation**

```go
// internal/gamedata/costumes.go
package gamedata

import "fmt"

// CostumeInfo is one entry from resources/rawdata/costumes.json.
type CostumeInfo struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Proto    string `json:"proto"`
	NoEvolve bool   `json:"noEvolve"`
}

// CostumeTranslationKey returns "costume_{id}" for a costume ID.
func CostumeTranslationKey(id int) string {
	return fmt.Sprintf("costume_%d", id)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/gamedata/ -run TestCostumeTranslationKey`
Expected: PASS.

- [ ] **Step 5: Add the loader**

In the game-data loader that already reads `resources/rawdata/*.json` (grep `pokemon.json` under `internal/gamedata` / `internal/resources` for the read site), add:

```go
// alongside the pokemon/forms load:
costumeBytes, err := os.ReadFile(filepath.Join(rawDir, "costumes.json"))
if err == nil {
	var raw map[string]CostumeInfo
	if json.Unmarshal(costumeBytes, &raw) == nil {
		gd.Costumes = make(map[int]CostumeInfo, len(raw))
		for _, c := range raw {
			gd.Costumes[c.ID] = c
		}
	}
}
```

Add `Costumes map[int]CostumeInfo` to the `GameData` struct.

- [ ] **Step 6: Add a load test**

```go
// internal/gamedata/costumes_test.go — append
func TestCostumesLoaded(t *testing.T) {
	gd := loadTestGameData(t) // reuse the existing gamedata test loader
	if gd.Costumes == nil || gd.Costumes[1].Name == "" {
		t.Fatalf("costume 1 not loaded; got %+v", gd.Costumes[1])
	}
}
```
(If no `loadTestGameData` helper exists, mirror the loader used by `gamedata_test.go`.)

- [ ] **Step 7: Run tests + gate + commit**

Run: `go test ./internal/gamedata/ && go build ./...`
```bash
git add internal/gamedata/costumes.go internal/gamedata/costumes_test.go internal/gamedata/*.go
git commit -m "feat(gamedata): load costumes.json + CostumeTranslationKey"
```

---

### Task 2: DB schema + tracking structs + v1 absent→9000 default

**Files:**
- Create: `internal/db/migrations/00XX_add_monster_costume.up.sql` / `.down.sql` (pick the next number after the highest existing migration)
- Modify: `internal/db/monsters.go` (`MonsterTracking` struct + SELECT), `internal/db/tracking_queries.go` (`MonsterTrackingAPI` + `UnmarshalJSON`)
- Test: `internal/api/costume_unmarshal_test.go` (new)

**Interfaces:**
- Produces: `MonsterTracking.Costume int` (`db:"costume"`), `MonsterTrackingAPI.Costume int` (`db:"costume" json:"costume"`), and `MonsterTrackingAPI.UnmarshalJSON` defaulting absent costume to 9000.

- [ ] **Step 1: Migration**

```sql
-- 00XX_add_monster_costume.up.sql
ALTER TABLE monsters ADD COLUMN costume INT NOT NULL DEFAULT 9000;
```
```sql
-- 00XX_add_monster_costume.down.sql
ALTER TABLE monsters DROP COLUMN costume;
```

- [ ] **Step 2: Struct fields**

`internal/db/monsters.go` — add to `MonsterTracking` after `Form`:
```go
	Costume int `db:"costume"`
```
`internal/db/tracking_queries.go` — add to `MonsterTrackingAPI` after `Form`:
```go
	Costume int `db:"costume" json:"costume"`
```

- [ ] **Step 3: SELECT column**

`internal/db/monsters.go:92` — add `costume,` to the SELECT column list (after `form,`):
```go
	`SELECT uid, id, profile_no, pokemon_id, form, costume, distance,
```
(The generic store INSERT builds from `db` tags, so no INSERT edit is needed — verify by grepping the store; if there's an explicit monster insert column list, add `costume` there too.)

- [ ] **Step 4: Write the failing test for the absent→9000 default**

```go
// internal/api/costume_unmarshal_test.go
package api

import (
	"encoding/json"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/db"
)

func TestMonsterTrackingAPI_CostumeDefaults(t *testing.T) {
	cases := []struct {
		name string
		body string
		want int
	}{
		{"absent → 9000", `{"pokemon_id":25}`, 9000},
		{"explicit 0 → 0", `{"pokemon_id":25,"costume":0}`, 0},
		{"explicit 5 → 5", `{"pokemon_id":25,"costume":5}`, 5},
	}
	for _, c := range cases {
		var m db.MonsterTrackingAPI
		if err := json.Unmarshal([]byte(c.body), &m); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if m.Costume != c.want {
			t.Errorf("%s: Costume = %d, want %d", c.name, m.Costume, c.want)
		}
	}
}
```

- [ ] **Step 5: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestMonsterTrackingAPI_CostumeDefaults`
Expected: FAIL — absent case gives 0, want 9000.

- [ ] **Step 6: Add the defaulting UnmarshalJSON**

`internal/db/tracking_queries.go` — add:
```go
// UnmarshalJSON defaults an absent costume to the 9000 wildcard ("any") rather
// than the Go zero-value 0 ("no costume"), so v1 clients (ReactMap/PoracleWeb)
// that don't send the field never create no-costume rules. Present values pass
// through verbatim.
func (m *MonsterTrackingAPI) UnmarshalJSON(data []byte) error {
	type alias MonsterTrackingAPI
	tmp := alias{Costume: 9000}
	if err := json.Unmarshal(data, &tmp); err != nil {
		return err
	}
	*m = MonsterTrackingAPI(tmp)
	return nil
}
```
(`encoding/json` is already imported in `monsters.go`; ensure it's imported in `tracking_queries.go`.)

- [ ] **Step 7: Run test + gate + commit**

Run: `go test ./internal/api/ -run TestMonsterTrackingAPI_CostumeDefaults && go build ./...`
```bash
git add internal/db/migrations/00XX_add_monster_costume.*.sql internal/db/monsters.go internal/db/tracking_queries.go internal/api/costume_unmarshal_test.go
git commit -m "feat(db): monsters.costume column + API absent→9000 default"
```

---

### Task 3: Matcher costume filter

**Files:**
- Modify: `internal/matching/pokemon.go` (`ProcessedPokemon.Costume`, populate it, filter in `matchMonsters`)
- Test: `internal/matching/pokemon_costume_test.go` (new)

**Interfaces:**
- Consumes: `MonsterTracking.Costume` (Task 2).
- Produces: `ProcessedPokemon.Costume int`; costume filter in `matchMonsters`.

- [ ] **Step 1: Write the failing test**

```go
// internal/matching/pokemon_costume_test.go
package matching

import (
	"testing"
	"github.com/pokemon/poracleng/processor/internal/db"
)

func TestMatchMonsters_Costume(t *testing.T) {
	m := &PokemonMatcher{}
	data := &ProcessedPokemon{PokemonID: 25, Form: 598, Costume: 1}
	mk := func(costume int) []*db.MonsterTracking {
		return []*db.MonsterTracking{{ID: "u1", PokemonID: 25, Form: 0, Costume: costume}}
	}
	if got := m.matchMonsters(data, mk(9000), 25, 0, false, 0, pvpZero()); len(got) != 1 {
		t.Error("costume 9000 (any) should match")
	}
	if got := m.matchMonsters(data, mk(1), 25, 0, false, 0, pvpZero()); len(got) != 1 {
		t.Error("costume 1 should match costume-1 spawn")
	}
	if got := m.matchMonsters(data, mk(2), 25, 0, false, 0, pvpZero()); len(got) != 0 {
		t.Error("costume 2 should NOT match costume-1 spawn")
	}
	if got := m.matchMonsters(data, mk(0), 25, 0, false, 0, pvpZero()); len(got) != 0 {
		t.Error("costume 0 (no costume) should NOT match costumed spawn")
	}
}
```
(Add a `pvpZero()` helper returning a zero `pvp.LeagueRank` if the test package doesn't already have one; import `pvp` as needed.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/matching/ -run TestMatchMonsters_Costume`
Expected: FAIL — `ProcessedPokemon` has no `Costume`, and no filter yet.

- [ ] **Step 3: Add `Costume` to `ProcessedPokemon` and populate it**

`internal/matching/pokemon.go` — add `Costume int` to `ProcessedPokemon` (after `Form`), and in `ProcessPokemonWebhook` set `processed.Costume = pokemon.Costume`.

- [ ] **Step 4: Add the filter in `matchMonsters`**

Immediately after the existing form check (`if monster.Form != 0 && monster.Form != formToCheck { continue }`), add:
```go
		// Costume check: 9000 = any; any other value (incl. 0 = no costume) is exact.
		if monster.Costume != 9000 && monster.Costume != data.Costume {
			continue
		}
```

- [ ] **Step 5: Run test + gate + commit**

Run: `go test ./internal/matching/ -run TestMatchMonsters_Costume`
Expected: PASS.
```bash
git add internal/matching/pokemon.go internal/matching/pokemon_costume_test.go
git commit -m "feat(matching): filter pokemon rules by costume (9000=any)"
```

---

### Task 4: Enrichment — costume in `fullName` + `costumeName`

**Files:**
- Modify: `internal/enrichment/translate.go` (`buildFullName` + `translateNames` signature), `internal/enrichment/pokemon.go` (pass costume, set `costumeName`)
- Test: `internal/enrichment/translate_costume_test.go` (new)

**Interfaces:**
- Consumes: `CostumeTranslationKey` (Task 1).
- Produces: `fullName` = `"<name> (<costumeName>)"` when `costume > 0`; `costumeName` field.

- [ ] **Step 1: Write the failing test**

```go
// internal/enrichment/translate_costume_test.go
package enrichment
// mirror the harness in translate_test.go / invasion_test.go (newInvasionBundle).
// Assert: buildFullName-with-costume for Pikachu costume 1 → "Pikachu (Holiday 2016)",
// and costume 0 → "Pikachu" unchanged. Use costume_1 = "Holiday 2016" in the bundle.
```
Write a concrete test using the existing bundle helper: build a translator with `poke_25`="Pikachu", `form_598`="Normal", `costume_1`="Holiday 2016"; call the costume-aware `buildFullName` with costume 1 and assert `"Pikachu (Holiday 2016)"`, with costume 0 assert `"Pikachu"`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/enrichment/ -run Costume`
Expected: FAIL (signature/behaviour missing).

- [ ] **Step 3: Thread costume through**

`internal/enrichment/translate.go` — add a `costume int` param to `buildFullName` and, after the mega composition, before returning:
```go
	if costume > 0 && tr != nil {
		if cn := tr.T(CostumeTranslationKey(costume)); cn != "" && cn != CostumeTranslationKey(costume) {
			fullName = fullName + " (" + cn + ")"
		}
	}
```
Thread `costume` from `translateNames` (add a `costume int` arg) into both the main `fullName` and `fullNameEng` calls. `BuildFullNameWithAlignment` passes `costume` through to `buildFullName`. **Do not** add costume to the PVP/evolution entry calls in `pokemon.go` (they call `translateNames`/`buildFullName` for candidate ranks — pass `0`).

- [ ] **Step 4: Set `costumeName` + wire the spawn call**

`internal/enrichment/pokemon.go` — where `translateNames` is called for the spawn, pass `pokemon.Costume`; and add:
```go
	m["costumeName"] = tr.T(gamedata.CostumeTranslationKey(pokemon.Costume)) // "" when 0/unset resolves to key
```
Guard so costume 0 yields empty `costumeName` (only set when `pokemon.Costume > 0`).

- [ ] **Step 5: Run test + gate + commit**

Run: `go test ./internal/enrichment/`
```bash
git add internal/enrichment/translate.go internal/enrichment/pokemon.go internal/enrichment/translate_costume_test.go
git commit -m "feat(enrichment): weave costume into fullName + costumeName"
```

---

### Task 5: RecentActivity — RecordCostume / RecentCostumes

**Files:**
- Modify: `internal/tracker/recent_activity.go`, `cmd/processor/pokemon.go`
- Test: `internal/tracker/recent_activity_costume_test.go` (new)

**Interfaces:**
- Produces: `func (r *RecentActivity) RecordCostume(pokemonID, costume int)`; `func (r *RecentActivity) RecentCostumes(pokemonID int) []int` (sorted, recency-windowed).

- [ ] **Step 1: Write the failing test**

```go
// internal/tracker/recent_activity_costume_test.go
package tracker

import "testing"

func TestRecentCostumes(t *testing.T) {
	r := NewRecentActivity()
	r.RecordCostume(25, 1)
	r.RecordCostume(25, 8)
	r.RecordCostume(25, 0) // no-costume: ignored
	got := r.RecentCostumes(25)
	if len(got) != 2 {
		t.Fatalf("RecentCostumes(25) = %v, want [1 8]", got)
	}
	if r.RecentCostumes(999) != nil && len(r.RecentCostumes(999)) != 0 {
		t.Error("unknown pokemon should have no recent costumes")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tracker/ -run TestRecentCostumes`
Expected: FAIL — methods undefined.

- [ ] **Step 3: Implement (two-level map)**

`recent_activity.go` — add field `costumesByPokemon map[int]map[int]time.Time` (init in `NewRecentActivity`), then:
```go
func (r *RecentActivity) RecordCostume(pokemonID, costume int) {
	if pokemonID <= 0 || costume <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	inner := r.costumesByPokemon[pokemonID]
	if inner == nil {
		inner = make(map[int]time.Time)
		r.costumesByPokemon[pokemonID] = inner
	}
	inner[costume] = r.now()
}

func (r *RecentActivity) RecentCostumes(pokemonID int) []int {
	r.mu.Lock()
	inner := r.costumesByPokemon[pokemonID]
	r.mu.Unlock()
	if inner == nil {
		return nil
	}
	return r.active(inner) // reuse the existing recency window + sort
}
```
(If `active` sorts/filters by the same window used elsewhere, reuse it; otherwise mirror its window constant.)

- [ ] **Step 4: Wire the producer**

`cmd/processor/pokemon.go` — in `ProcessPokemon`, near the existing `ps.stats.RecordSighting(...)`:
```go
	if ps.recentActivity != nil {
		ps.recentActivity.RecordCostume(pokemon.PokemonID, pokemon.Costume)
	}
```

- [ ] **Step 5: Run test + gate + commit**

Run: `go test ./internal/tracker/ -run TestRecentCostumes`
```bash
git add internal/tracker/recent_activity.go internal/tracker/recent_activity_costume_test.go cmd/processor/pokemon.go
git commit -m "feat(tracker): RecordCostume/RecentCostumes on shared RecentActivity"
```

---

### Task 6: Argmatcher — `costume:` name resolution

**Files:**
- Modify: `internal/bot/argmatch.go` (costume prefix param + multi-word vocabulary + resolver)
- Test: `internal/bot/argmatch_costume_test.go` (new)

**Interfaces:**
- Produces: parsed `parsed.Strings["costume"]` from `costume:<name-or-id>`; a `ResolveCostume(name, lang) (id int, ok bool)` that maps a costume name (user lang + English) or numeric string → id.

- [ ] **Step 1: Write the failing test**

Mirror `argmatch_test.go`'s multi-word matcher harness (`newMultiWordTestMatcher`), seeding `costume_1`="Holiday 2016". Assert: `costume:holiday_2016` and eager-joined `costume:holiday 2016` resolve to id 1; `costume:0` → 0; `costume:5` → 5.

- [ ] **Step 2: Run to verify it fails.** Run: `go test ./internal/bot/ -run Costume` → FAIL.

- [ ] **Step 3: Implement**

- Add `arg.prefix.costume` handling (a `ParamPrefixString` entry, like `arg.prefix.form`), so `parsed.Strings["costume"]` is captured.
- Seed the costume names into the multi-word vocabulary (same place items/moves/forms are seeded from translations) so `costume:holiday 2016` eager-joins.
- Add `ResolveCostume`: if the value parses as an int, return it; else lowercase-match against `costume_{id}` in user lang and English (mirror `filterByForm`'s translation lookup), returning the id.

- [ ] **Step 4: Run + commit.**
```bash
git add internal/bot/argmatch.go internal/bot/argmatch_costume_test.go
git commit -m "feat(bot): costume: arg name resolution"
```

---

### Task 7: `!track` / `!untrack` costume + `!tracked` rowtext

**Files:**
- Modify: `internal/bot/commands/track.go`, `internal/bot/commands/untrack.go`, `internal/rowtext/monster.go` (the monster rowtext file), `internal/i18n/locale/en.json`
- Test: `internal/bot/commands/track_costume_test.go` (new)

**Interfaces:**
- Consumes: `ResolveCostume` (Task 6), `MonsterTrackingAPI.Costume` (Task 2), `RowText` monster formatting.

- [ ] **Step 1: Write the failing test**

Mirror `track_test.go`: run `!track pikachu costume:holiday_2016` and assert the stored `MonsterTrackingAPI.Costume == 1`; `!track pikachu costume:0` → 0; bare `!track pikachu` → 9000. Assert `!untrack pikachu costume:1` removes only the costume-1 rule. Assert the rowtext for a costume-1 rule contains "Holiday 2016".

- [ ] **Step 2: Run to verify it fails.** Run: `go test ./internal/bot/commands/ -run Costume` → FAIL.

- [ ] **Step 3: Implement**

- `track.go`: after resolving the mon list, read `parsed.Strings["costume"]`, resolve via `ResolveCostume` (default 9000 when absent), set `insert[i].Costume`. On unresolved name, return a 🙅 reply (mirror `applyFormFilter`'s not-found path) — add i18n `msg.costume_not_found`.
- `untrack.go`: include costume in the remove match so `!untrack pikachu costume:1` targets that rule.
- `rowtext/monster.go`: when `Costume != 9000`, append the costume — `tr.T(costume_{id})` for N>0, `msg.no_costume` for 0.
- `en.json`: add `arg.prefix.costume` ("costume"), `msg.costume_not_found`, `msg.no_costume` ("no costume").

- [ ] **Step 4: Run + gate + commit.**
```bash
git add internal/bot/commands/track.go internal/bot/commands/untrack.go internal/rowtext/monster.go internal/i18n/locale/en.json internal/bot/commands/track_costume_test.go
git commit -m "feat(bot): !track/!untrack costume + costume in !tracked"
```

---

### Task 8: `!info costumes` + per-species recently-seen

**Files:**
- Modify: `internal/bot/commands/info.go`, `internal/i18n/locale/en.json`
- Test: `internal/bot/commands/info_costume_test.go` (new)

**Interfaces:**
- Consumes: `GameData.Costumes` (Task 1), `RecentActivity.RecentCostumes` (Task 5).

- [ ] **Step 1: Write the failing test**

Mirror `info_test.go`. (a) `!info costumes` returns a reply listing costume names (assert it contains "Holiday 2016"). (b) After `ctx`'s RecentActivity has `RecordCostume(25,1)`, `!info pikachu` reply contains a recently-seen costume section with "Holiday 2016".

- [ ] **Step 2: Run to verify it fails.** → FAIL.

- [ ] **Step 3: Implement**

- Add a `case matchSub("msg.info.sub.costumes"):` in the subcommand dispatch → a `showCostumes(ctx)` that lists `GameData.Costumes` sorted by id as `id — <costume_{id} name>`.
- In the per-pokemon path (near `availableForms`), add `availableCostumes(ctx, pokemonID)` reading `ctx.RecentActivity.RecentCostumes(pokemonID)`, formatting `id — name`, under a `msg.info.available_costumes` header. Skip the section when empty.
- `en.json`: `msg.info.sub.costumes` ("costumes"), `msg.info.costumes.header`, `msg.info.available_costumes`.
- Confirm `ctx.RecentActivity` is available to bot commands (it's already passed to `bot/command.go`); if not exposed on `CommandContext`, add it.

- [ ] **Step 4: Run + commit.**
```bash
git add internal/bot/commands/info.go internal/i18n/locale/en.json internal/bot/commands/info_costume_test.go
git commit -m "feat(bot): !info costumes + per-species recently-seen costumes"
```

---

### Task 9: Slash `/track` costume option + autocomplete

**Files:**
- Modify: `internal/discordbot/slash/definitions.go`, `internal/discordbot/slash/mappers/track.go`, `internal/discordbot/slash/autocomplete/` (+ dispatcher wiring)
- Test: `internal/discordbot/slash/mappers/track_test.go` (extend)

**Interfaces:**
- Consumes: the `/track` mapper → `costume:<id>` token consumed by Task 7's parser.

- [ ] **Step 1: Write the failing test**

Extend `mappers/track_test.go`: a `/track` option set with `costume` present emits a `costume:<id>` token (mirror the existing `form` option test at `mappers/track.go:49`).

- [ ] **Step 2: Run to verify it fails.** → FAIL.

- [ ] **Step 3: Implement**

- `definitions.go`: add `stringOpt(bundle, "track.costume", "costume", "Pokemon costume", false, true)` (autocomplete=true), mirroring `track.form` at `definitions.go:500`.
- `mappers/track.go`: after the form block (`:49`), add:
```go
	if v, ok := o["costume"]; ok && v.StringValue() != "" {
		tokens = append(tokens, "costume:"+v.StringValue())
	}
```
- Autocomplete: add a `costume` autocomplete provider listing `GameData.Costumes` (label = name, value = id); wire it in the dispatcher's autocomplete switch (mirror the `form` autocomplete).

- [ ] **Step 4: Run + commit.**
```bash
git add internal/discordbot/slash/definitions.go internal/discordbot/slash/mappers/track.go internal/discordbot/slash/autocomplete/ internal/discordbot/slash/dispatcher.go internal/discordbot/slash/mappers/track_test.go
git commit -m "feat(slash): /track costume option with name autocomplete"
```

---

### Task 10: v2 Pokémon API `Costume`

**Files:**
- Modify: `internal/api/v2_pokemon.go`
- Test: `internal/api/v2_pokemon_costume_test.go` (new)

**Interfaces:**
- Consumes: `valueOr` / `ptrUnless` (existing in `v2_pokemon.go`), `MonsterTrackingAPI.Costume`.

- [ ] **Step 1: Write the failing test**

Assert `translateV2Pokemon` (the write mapper) maps: `Costume=nil` → 9000, `Costume=ptr(0)` → 0, `Costume=ptr(5)` → 5; and the read mapper returns `nil` when the stored costume is 9000, `ptr(0)` when 0, `ptr(5)` when 5. (Mirror the existing Form round-trip test if present.)

- [ ] **Step 2: Run to verify it fails.** → FAIL.

- [ ] **Step 3: Implement**

- Add to the v2 rule struct (near `Form` at `:24`):
```go
	Costume *int `json:"costume,omitempty" nullable:"true" doc:"Costume id. Omit/null = any (stored 9000). 0 = no costume. N = that costume."`
```
- Write mapper (near `Form: valueOr(req.Form, 0)` at `:123`):
```go
		Costume: valueOr(req.Costume, 9000),
```
- Read mapper (near `Form: ptrUnless(row.Form, 0)` at `:167`):
```go
		Costume: ptrUnless(row.Costume, 9000),
```

- [ ] **Step 4: Run + gate + commit.**
```bash
git add internal/api/v2_pokemon.go internal/api/v2_pokemon_costume_test.go
git commit -m "feat(api): v2 pokemon Costume field (9000/0/null semantics)"
```

---

### Task 11: DTS field metadata + docs

**Files:**
- Modify: `internal/api/dts_fields.go` (`monsterFields`), `DTS.md`
- Test: `internal/api/dts_fields_costume_test.go` (new)

**Interfaces:**
- Consumes: `fieldsByType["monster"]`.

- [ ] **Step 1: Write the failing test**

```go
// internal/api/dts_fields_costume_test.go
package api

import "testing"

func TestMonsterFields_Costume(t *testing.T) {
	m := fieldsByType["monster"]
	if !hasFieldDef(m.Fields, "costumeName") {
		t.Error("monster type should list costumeName")
	}
}
```
(`hasFieldDef` exists from the showcase field test.)

- [ ] **Step 2: Run to verify it fails.** → FAIL.

- [ ] **Step 3: Implement**

- `dts_fields.go` — add to `monsterFields` (near the existing `costume` field at `:155`):
```go
	{Name: "costumeName", Type: "string", Description: "Translated costume name (empty when no costume). Note: fullName already includes it parenthesised.", Category: "other"},
```
- `DTS.md` — document `costumeName` in the monster field table, and note that `fullName` includes the costume as `(Costume Name)`.

- [ ] **Step 4: Run + gate + commit.**
```bash
git add internal/api/dts_fields.go DTS.md internal/api/dts_fields_costume_test.go
git commit -m "docs(dts): costumeName field + fullName-includes-costume note"
```

---

### Task 12: Test data + end-to-end verification

**Files:**
- Modify: `fallbacks/testdata.json` (a costumed pokemon test entry)
- Manual/gate verification

- [ ] **Step 1:** Add a `pokemon` testdata entry with `costume: 1` (a costumed Pikachu) so `!poracle-test pokemon,<id>` renders a costumed name.

- [ ] **Step 2:** Run the full gate:
```
go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...
```
Expected: all pass, 0 lint issues.

- [ ] **Step 3:** Sanity-check end to end (if an env is available): `!track pikachu costume:holiday_2016` → ✅; `!tracked` shows the costume; `!info costumes` lists all; `!poracle-test pokemon,<costumed-id>` renders `Pikachu (Holiday 2016)`.

- [ ] **Step 4: Commit.**
```bash
git add fallbacks/testdata.json
git commit -m "test: costumed-pokemon testdata + costume tracking e2e verified"
```

---

## Self-review notes

- **Spec coverage:** gamedata (T1), DB+v1-default (T2), matcher (T3), fullName+costumeName (T4), RecentActivity (T5), argmatcher (T6), commands+rowtext/!tracked (T7), !info global+recent (T8), slash (T9), v2 API (T10), DTS fields/docs (T11), testdata/e2e (T12). All spec sections mapped.
- **v1 compatibility** is covered by T2's `UnmarshalJSON` default (the shared `MonsterTrackingAPI` is the v1 parse target) + T2's SELECT/insert column wiring.
- **Wildcard consistency:** 9000 = any, 0 = no costume used identically in T2 (default), T3 (matcher), T7 (command default), T10 (v2 valueOr/ptrUnless).
- **Open items for the implementer to confirm against live code:** exact game-data load site (T1 Step 5), whether the monster store INSERT uses an explicit column list (T2 Step 3), `ctx.RecentActivity` exposure on `CommandContext` (T8), and the `active()` recency-window reuse (T5).
