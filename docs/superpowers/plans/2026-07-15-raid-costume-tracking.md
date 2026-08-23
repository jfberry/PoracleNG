# Raid Costume Tracking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add costume as a filter dimension on raid tracking (costumed raid bosses), mirroring the pokemon costume feature: DB column, matcher filter, `!raid`/`/raid` command, rowtext, a separate raid-costume recency tracker + `!info` section, and v1/v2 raid API.

**Architecture:** The raid layers mirror pokemon 1:1. Raid webhooks already carry `costume`, and raid enrichment already *displays* it (fullName/megaName/costumeName), so this plan is the tracking/filter/recency/info/API half only.

**Tech Stack:** Go, MySQL/sqlx, huma (v2) + gin (v1), discordgo autocomplete, existing `ResolveCostume` / `PrependRecentCostumes` / costume gamedata infra.

Design spec: `docs/superpowers/specs/2026-07-15-raid-costume-tracking-design.md`.

## Global Constraints

- **Sentinel:** `raid.costume` uses **9000 = any** (default; an absent field defaults to 9000), **0 = no costume**, **N = that costume**. This is the same sentinel raid `evolution` already uses. (Raid `form` uses 0=any, but costume must use 9000=any so `0` stays meaningful.)
- **Costume is rule-identity:** `RaidTrackingAPI.Costume` carries **no `diff` tag** (like `Form`). An absent costume MUST default to 9000 at every rule-construction site (v1 build paths, `!raid` command, v2 write) — a Go-zero `0` default creates no-costume rules and duplicate rows on re-submit.
- **Costume applies to every rule a command/API call generates** (single-form path, `pokemon_form` array path, each level).
- **Reuse existing infra:** `ArgMatcher.ResolveCostume`, `autocomplete.PrependRecentCostumes`, `autocomplete.ResolvePokemonID`, `gamedata.CostumeTranslationKey`, `gamedata.GameData.Costumes`. Do not reinvent them.
- **No UPDATE change:** costume is rule-identity, so it is never UPDATE'd in place (a costume change is insert+delete). Raid has no static `UpdateRaid`; verify no raid update lists columns before assuming.
- **Pre-commit gate (from `processor/`), all four must pass:** `go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...`.

---

### Task 1: DB storage + v1 API create path (+ idempotency)

**Files:**
- Create: `processor/internal/db/migrations/000007_add_raid_costume.up.sql`, `…down.sql`
- Modify: `processor/internal/db/raids.go` (RaidTracking + LoadRaids)
- Modify: `processor/internal/db/tracking_queries.go` (RaidTrackingAPI + 2 SELECTs + InsertRaid)
- Modify: `processor/internal/api/trackingRaid.go` (raidInsertRequest + both build paths + toRaidTracking)
- Test: `processor/internal/api/tracking_test.go` (raid idempotency)

**Interfaces:**
- Produces: `db.RaidTracking.Costume int`, `db.RaidTrackingAPI.Costume int` (json `costume`, no diff tag) — consumed by matcher (Task 2), rowtext (Task 3), command (Task 4), v2 (Task 8).

- [ ] **Step 1: Write the failing idempotency test**

Append to `tracking_test.go` (mirror `TestCreateMonster_CostumeDefaultIsIdempotent`):

```go
func TestCreateRaid_CostumeDefaultIsIdempotent(t *testing.T) {
	mock := store.NewMockHumanStore()
	mock.AddHuman(&store.Human{ID: "u1", Type: "discord:user", Name: "User", Enabled: true, Language: "en", CurrentProfileNo: 1})

	mockRaids := store.NewMockTrackingStore(store.RaidGetUID, store.RaidSetUID)
	minGD := &gamedata.GameData{Monsters: map[gamedata.MonsterKey]*gamedata.Monster{}, Util: &gamedata.UtilData{}}

	deps := &TrackingDeps{
		Humans:       mock,
		Tracking:     &store.TrackingStores{Raids: mockRaids},
		Config:       &config.Config{},
		RowText:      &rowtext.Generator{DefaultTemplateName: "1", GD: minGD},
		Translations: i18n.NewBundle(),
	}

	r := gin.New()
	r.POST("/api/tracking/raid/:id", HandleCreateRaid(deps))

	body := `{"pokemon_id":25}`
	for i, want := range []int{1, 1} { // both POSTs => still 1 row
		req := httptest.NewRequest(http.MethodPost, "/api/tracking/raid/u1", strings.NewReader(body))
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("POST %d: expected 200, got %d: %s", i, w.Code, w.Body.String())
		}
		rows := mockRaids.AllRows()
		if len(rows) != want {
			t.Fatalf("after POST %d: expected %d row(s), got %d (dup?)", i, want, len(rows))
		}
		if rows[0].Costume != 9000 {
			t.Fatalf("expected Costume=9000 from v1 default, got %d", rows[0].Costume)
		}
	}
}
```

- [ ] **Step 2: Run — verify it fails**

Run: `go test ./internal/api/ -run TestCreateRaid_CostumeDefaultIsIdempotent -v`
Expected: FAIL to compile (`RaidTrackingAPI` has no `Costume`) — this is the RED signal.

- [ ] **Step 3: Migration**

`000007_add_raid_costume.up.sql`:
```sql
ALTER TABLE `raid`
  ADD COLUMN `costume` INT NOT NULL DEFAULT 9000;
```
`000007_add_raid_costume.down.sql`:
```sql
ALTER TABLE `raid` DROP COLUMN `costume`;
```

- [ ] **Step 4: DB structs + SQL**

`db/raids.go` — add to `RaidTracking` after `Form`:
```go
	Form                  int            `db:"form"`
	Costume               int            `db:"costume"`
```
`db/raids.go` `LoadRaids` SELECT — add `costume` after `form`:
```go
		`SELECT uid, id, profile_no, pokemon_id, level, team, exclusive, form, costume, evolution,
```

`db/tracking_queries.go` `RaidTrackingAPI` — add after `Form` (NO diff tag, like Form):
```go
	Form                  int         `db:"form"                    json:"form"`
	Costume               int         `db:"costume"                 json:"costume"`
```
`SelectRaidsByIDProfile` SELECT — add `costume` after `form`:
```go
		        COALESCE(template, '') AS template, team, pokemon_id, form, costume,
```
`SelectRaidsByID` SELECT — add `costume` after `form` (same edit in that query).
`InsertRaid` — add `costume` to columns and `raid.Costume` to binds (keep counts balanced):
```go
		`INSERT INTO raid (id, profile_no, ping, clean, distance, template,
		        team, pokemon_id, form, costume, level, exclusive, move, evolution, gym_id, rsvp_changes,
		        override_location_label, override_areas)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		raid.ID, raid.ProfileNo, raid.Ping, raid.Clean, raid.Distance, raid.Template,
		raid.Team, raid.PokemonID, raid.Form, raid.Costume, raid.Level, raid.Exclusive,
		raid.Move, raid.Evolution, raid.GymID, raid.RSVPChanges,
		nullIfEmpty(raid.OverrideLocationLabel), marshalOverrideAreas(raid.OverrideAreas)`
```
(18 columns / 18 placeholders / 18 binds — recount after editing.)

- [ ] **Step 5: v1 API create path**

`api/trackingRaid.go` `raidInsertRequest` — add after `Form`:
```go
	Form                  flexInt           `json:"form"`
	Costume               flexInt           `json:"costume"`
```
In `HandleCreateRaid`, right after `tmpl, dist, team, clean, excl, move, evo, gymID, rsvp := buildRaidCommon(req)`:
```go
			// Costume defaults to 9000 (the "any costume" wildcard) when absent,
			// so v1 clients that don't send it never create no-costume rules and
			// re-submits diff cleanly (Costume has no `diff` tag).
			costume := req.Costume.intValue(9000)
```
Add `Costume: costume,` to **both** `db.RaidTrackingAPI{…}` literals (the `pokemon_form` loop and the level loop).

`toRaidTracking` — add `Costume: api.Costume,` to the returned `db.RaidTracking{…}`.

- [ ] **Step 6: Run — verify GREEN**

Run: `go test ./internal/api/ -run TestCreateRaid_CostumeDefaultIsIdempotent -v`
Expected: PASS.

- [ ] **Step 7: Full gate + commit**

```bash
go build ./... && go vet ./... && go test -count=1 ./internal/api/ ./internal/db/ && golangci-lint run ./internal/api/ ./internal/db/
git add processor/internal/db processor/internal/api/trackingRaid.go processor/internal/api/tracking_test.go
git commit -m "feat(db): raid.costume column + v1 API absent->9000 default"
```

---

### Task 2: Matcher

**Files:**
- Modify: `processor/internal/matching/raid.go` (RaidData + filter)
- Modify: `processor/cmd/processor/raid.go` (populate RaidData.Costume)
- Test: `processor/internal/matching/raid_test.go`

**Interfaces:**
- Consumes: `db.RaidTracking.Costume` (Task 1). Produces: nothing new.

- [ ] **Step 1: Write the failing test**

Append to `raid_test.go` (mirror `TestRaidMatchBasic`; note existing fixtures set `Evolution: 9000`):

```go
func TestRaidMatchCostume(t *testing.T) {
	human := makeHuman("user1")
	mk := func(costume int) *db.RaidTracking {
		return &db.RaidTracking{
			ID: "user1", ProfileNo: 1, PokemonID: 25, Level: 5,
			Team: 4, Exclusive: false, Form: 0, Costume: costume, Evolution: 9000,
			Move: 9000, Distance: 0, Template: "1",
		}
	}
	data := &RaidData{
		GymID: "gym1", PokemonID: 25, Form: 0, Costume: 1, Level: 5,
		TeamID: 1, Evolution: 0, Move1: 100, Move2: 200, Latitude: 51.0, Longitude: 0.0,
	}
	matcher := &RaidMatcher{}
	check := func(costume, wantN int) {
		st := makeRaidTestState([]*db.RaidTracking{mk(costume)}, nil, map[string]*db.Human{"user1": human})
		if got, _ := matcher.MatchRaid(data, st); len(got) != wantN {
			t.Errorf("costume=%d: got %d matches, want %d", costume, len(got), wantN)
		}
	}
	check(9000, 1) // any matches the costume-1 boss
	check(1, 1)    // exact match
	check(2, 0)    // different costume: no match
	check(0, 0)    // "no costume" filter: costumed boss must not match
}
```

- [ ] **Step 2: Run — verify it fails**

Run: `go test ./internal/matching/ -run TestRaidMatchCostume -v`
Expected: FAIL to compile (`RaidData`/`RaidTracking` have no `Costume`).

- [ ] **Step 3: Implement**

`matching/raid.go` `RaidData` — add after `Form`:
```go
	Form      int
	Costume   int
```
`matching/raid.go` `MatchRaid` — after the evolution check (`if r.Evolution != 9000 && r.Evolution != raid.Evolution { continue }`):
```go
		// costume match — 9000 = any; else exact (incl. 0 = no costume)
		if r.Costume != 9000 && r.Costume != raid.Costume {
			continue
		}
```
`cmd/processor/raid.go` — in the `matching.RaidData{…}` literal, add `Costume: raid.Costume,` (next to `Form: raid.Form`).

- [ ] **Step 4: Run — verify GREEN + gate + commit**

```bash
go test ./internal/matching/ -run TestRaidMatchCostume -v
go build ./... && go vet ./... && go test -count=1 ./internal/matching/ ./cmd/... && golangci-lint run ./internal/matching/ ./cmd/...
git add processor/internal/matching/raid.go processor/internal/matching/raid_test.go processor/cmd/processor/raid.go
git commit -m "feat(matching): filter raid rules by costume (9000=any)"
```

---

### Task 3: rowtext (`!tracked` line)

**Files:**
- Modify: `processor/internal/rowtext/raid.go`
- Test: `processor/internal/rowtext/raid_test.go` (create if absent, else append)

**Interfaces:** Consumes `db.RaidTracking.Costume` (Task 1), `g.GD.Costumes`.

- [ ] **Step 1: Write the failing test**

Append/create a rowtext test (mirror the monster rowtext costume test; use a `Generator` with `GD.Costumes`):

```go
func TestRaidRowText_Costume(t *testing.T) {
	tr := i18n.NewTranslator("en", map[string]string{
		"poke_25":   "Pikachu",
		"costume_1": "Holiday 2016",
		"msg.no_costume": "no costume",
	})
	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{{ID: 25, Form: 0}: {PokemonID: 25}},
		Costumes: map[int]gamedata.CostumeInfo{1: {ID: 1, Name: "Holiday 2016"}},
	}
	g := &Generator{GD: gd, DefaultTemplateName: "1"}

	// costume N -> shows the name
	if got := g.RaidRowText(tr, &db.RaidTracking{PokemonID: 25, Level: 5, Costume: 1, Move: 9000, Evolution: 9000, Template: "1"}); !strings.Contains(got, "Holiday 2016") {
		t.Errorf("costume 1 row should contain the costume name, got: %q", got)
	}
	// costume 0 -> "no costume"
	if got := g.RaidRowText(tr, &db.RaidTracking{PokemonID: 25, Level: 5, Costume: 0, Move: 9000, Evolution: 9000, Template: "1"}); !strings.Contains(got, "no costume") {
		t.Errorf("costume 0 row should contain 'no costume', got: %q", got)
	}
	// costume 9000 (any) -> nothing costume-related
	if got := g.RaidRowText(tr, &db.RaidTracking{PokemonID: 25, Level: 5, Costume: 9000, Move: 9000, Evolution: 9000, Template: "1"}); strings.Contains(got, "Holiday 2016") || strings.Contains(got, "no costume") {
		t.Errorf("costume 9000 row should not mention costume, got: %q", got)
	}
}
```

- [ ] **Step 2: Run — verify it fails** (compile error: no `Costume` field until Task 1 merged — this task builds on Task 1). Run `go test ./internal/rowtext/ -run TestRaidRowText_Costume -v`; expect assertion failure (no costume text emitted).

- [ ] **Step 3: Implement**

In `rowtext/raid.go` `RaidRowText`, after the `formName` clause (mirror `rowtext/monster.go`'s costume block), add:
```go
	// Costume: 9000 (wildcard) omitted; 0 = "no costume"; N>0 = translated name
	// (masterfile-name fallback when the gamelocale key is missing).
	if raid.Costume != 9000 {
		costumeName := tr.T("msg.no_costume")
		if raid.Costume != 0 {
			key := gamedata.CostumeTranslationKey(raid.Costume)
			costumeName = tr.T(key)
			if costumeName == key && g.GD != nil {
				if info, ok := g.GD.Costumes[raid.Costume]; ok && info.Name != "" {
					costumeName = info.Name
				}
			}
		}
		s += " | " + costumeName
	}
```
Place it consistently with where monster rowtext puts it (after the name/form, before distance/template). Ensure `gamedata` is imported (it is — `translateMonsterName`).

- [ ] **Step 4: GREEN + gate + commit**

```bash
go test ./internal/rowtext/ -run TestRaidRowText_Costume -v
go build ./... && go vet ./... && go test -count=1 ./internal/rowtext/ && golangci-lint run ./internal/rowtext/
git add processor/internal/rowtext/raid.go processor/internal/rowtext/raid_test.go
git commit -m "feat(rowtext): show costume in raid !tracked line"
```

---

### Task 4: `!raid` command (add + remove by costume)

**Files:**
- Modify: `processor/internal/bot/commands/raid.go`
- Test: `processor/internal/bot/commands/raid_test.go`

**Interfaces:** Consumes `ArgMatcher.ResolveCostume`, `db.RaidTrackingAPI.Costume`.

- [ ] **Step 1: Write the failing test**

Append to `raid_test.go`, reusing the existing `raidCtx(t)` helper (it builds a `CommandContext` with a `store.NewMockTrackingStore[db.RaidTrackingAPI]`, an `ArgMatcher`, and a pokemon resolver; assert stored rows via `ctx.Tracking.Raids.SelectByIDProfile("user1", 1)`). `raidCtx`'s `ArgMatcher` seeds the costume vocabulary from `GameData.Costumes`, so its `GameData` must contain `Costumes: {1: {ID:1, Name:"Holiday 2016"}}` and its "en" translator `costume_1: "Holiday 2016"` for `costume:holiday_2016` to resolve — **add these to `raidCtx` if absent** (additive; existing raid tests are unaffected).

```go
func TestRaid_Costume(t *testing.T) {
	ctx := raidCtx(t)
	(&RaidCommand{}).Run(ctx, []string{"pikachu", "costume:holiday_2016"})
	rows, _ := ctx.Tracking.Raids.SelectByIDProfile("user1", 1)
	if len(rows) != 1 || rows[0].Costume != 1 {
		t.Fatalf("expected 1 raid rule with Costume=1, got %+v", rows)
	}

	// Bare add defaults to 9000 (any).
	ctx2 := raidCtx(t)
	(&RaidCommand{}).Run(ctx2, []string{"pikachu"})
	rows2, _ := ctx2.Tracking.Raids.SelectByIDProfile("user1", 1)
	if len(rows2) != 1 || rows2[0].Costume != 9000 {
		t.Fatalf("bare !raid pikachu should store Costume=9000, got %+v", rows2)
	}
}
```
> The `!raid pikachu` add path resolves the pokemon and builds one rule; confirm `raidCtx`'s resolver knows pikachu (id 25). If `raidCtx` uses a different species, use that species' name + a `costume_N`/`Costumes[N]` pair instead.

- [ ] **Step 2: Run — verify it fails.** `go test ./internal/bot/commands/ -run TestRaid_Costume -v` → FAIL (costume not parsed/stored).

- [ ] **Step 3: Implement**

In `bot/commands/raid.go` `Run`, before the rule-building blocks, resolve the costume (mirror `track.go:73-88`):
```go
	// Costume filter — 9000 = any (default), 0 = no costume, N = specific.
	costume := 9000
	if costumeArg, ok := parsed.Strings["costume"]; ok {
		id, resolved := ctx.ArgMatcher.ResolveCostume(costumeArg, ctx.Language)
		if !resolved {
			return []bot.Reply{{React: "🙅", Text: tr.Tf("msg.costume_not_found", ctx.EscapeForCode(costumeArg), bot.CommandPrefix(ctx))}}
		}
		costume = id
	}
```
Add `Costume: costume,` to **both** `db.RaidTrackingAPI{…}` insert literals (the pokemon path ~line 131 and the level/everything path ~line 182).

**Remove by costume:** in the removal branch, when `parsed.Strings["costume"]` is set, filter the existing rules to those whose `Costume` matches the resolved id before removing (mirror how `!untrack costume:N` narrows monster removal in `untrack.go`). Resolve the same way; skip rules whose `Costume != resolvedCostume`.

- [ ] **Step 4: GREEN + gate + commit**

```bash
go test ./internal/bot/commands/ -run 'TestRaid' -v
go build ./... && go vet ./... && go test -count=1 ./internal/bot/... && golangci-lint run ./internal/bot/...
git add processor/internal/bot/commands/raid.go processor/internal/bot/commands/raid_test.go
git commit -m "feat(bot): !raid costume filter (add + remove)"
```

---

### Task 5: RecentActivity — raid costume bucket + producer

**Files:**
- Modify: `processor/internal/tracker/recent_activity.go`
- Modify: `processor/cmd/processor/raid.go` (producer)
- Test: `processor/internal/tracker/recent_activity_raidcostume_test.go` (create)

**Interfaces:** Produces `RecordRaidCostume(pokemonID, costume int)`, `RecentRaidCostumes(pokemonID int) []int` — consumed by `!info` (Task 6) and slash (Task 7).

- [ ] **Step 1: Write the failing test**

```go
package tracker

import "testing"

func TestRecentRaidCostumes(t *testing.T) {
	r := NewRecentActivity()
	r.RecordRaidCostume(25, 1)
	r.RecordRaidCostume(25, 12)
	r.RecordRaidCostume(25, 0) // no-costume: ignored
	if got := r.RecentRaidCostumes(25); len(got) != 2 {
		t.Fatalf("RecentRaidCostumes(25) = %v, want two entries", got)
	}
	if len(r.RecentRaidCostumes(999)) != 0 {
		t.Error("unknown boss should have no recent raid costumes")
	}
	// Separate from spawn costumes.
	if len(r.RecentCostumes(25)) != 0 {
		t.Error("raid costumes must not leak into the spawn RecentCostumes bucket")
	}
}
```

- [ ] **Step 2: Run — verify it fails.** `go test ./internal/tracker/ -run TestRecentRaidCostumes -v` → FAIL (undefined).

- [ ] **Step 3: Implement** (mirror `costumesByPokemon`/`RecordCostume`/`RecentCostumes` exactly)

`recent_activity.go` — add field + init:
```go
	costumesByPokemon     map[int]map[int]time.Time
	raidCostumesByPokemon map[int]map[int]time.Time
```
```go
		costumesByPokemon:     make(map[int]map[int]time.Time),
		raidCostumesByPokemon: make(map[int]map[int]time.Time),
```
Add the methods after `RecentCostumes`:
```go
// RecordRaidCostume marks costume as recently seen on a raid boss pokemonID.
func (r *RecentActivity) RecordRaidCostume(pokemonID, costume int) {
	if pokemonID <= 0 || costume <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	inner := r.raidCostumesByPokemon[pokemonID]
	if inner == nil {
		inner = make(map[int]time.Time)
		r.raidCostumesByPokemon[pokemonID] = inner
	}
	inner[costume] = r.now()
}

// RecentRaidCostumes returns the recency-windowed costume IDs recently seen on
// raid boss pokemonID.
func (r *RecentActivity) RecentRaidCostumes(pokemonID int) []int {
	r.mu.Lock()
	inner := r.raidCostumesByPokemon[pokemonID]
	r.mu.Unlock()
	if inner == nil {
		return nil
	}
	return r.active(inner)
}
```
`cmd/processor/raid.go` — beside the existing `RecordRaidBoss` call:
```go
		if ps.recentActivity != nil {
			ps.recentActivity.RecordRaidBoss(raid.PokemonID)
			if raid.Costume > 0 {
				ps.recentActivity.RecordRaidCostume(raid.PokemonID, raid.Costume)
			}
		}
```
(If the existing `RecordRaidBoss` call already sits under an `if ps.recentActivity != nil` guard, just add the costume line inside it.)

- [ ] **Step 4: GREEN + gate + commit**

```bash
go test ./internal/tracker/ -run TestRecentRaidCostumes -v
go build ./... && go vet ./... && go test -count=1 ./internal/tracker/ ./cmd/... && golangci-lint run ./internal/tracker/ ./cmd/...
git add processor/internal/tracker/recent_activity.go processor/internal/tracker/recent_activity_raidcostume_test.go processor/cmd/processor/raid.go
git commit -m "feat(tracker): RecordRaidCostume/RecentRaidCostumes bucket + producer"
```

---

### Task 6: `!info` recently-seen raid costumes section

**Files:**
- Modify: `processor/internal/bot/commands/info.go`
- Modify: `processor/internal/i18n/locale/en.json`
- Test: `processor/internal/bot/commands/info_raid_costume_test.go` (create)

**Interfaces:** Consumes `RecentRaidCostumes` (Task 5).

- [ ] **Step 1: Write the failing test** (mirror `info_costume_test.go`)

```go
package commands

import (
	"strings"
	"testing"

	"github.com/pokemon/poracleng/processor/internal/bot"
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

func infoRaidCostumeCtx(t *testing.T) *bot.CommandContext {
	t.Helper()
	ctx, _ := testCtx(t)
	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{{ID: 25, Form: 0}: {PokemonID: 25, FormID: 0}},
		Moves:    map[int]*gamedata.Move{}, Types: map[int]*gamedata.TypeInfo{}, Util: &gamedata.UtilData{},
		Costumes: map[int]gamedata.CostumeInfo{12: {ID: 12, Name: "Party Hat"}},
	}
	ctx.Translations.AddTranslator(i18n.NewTranslator("en", map[string]string{"poke_25": "Pikachu", "costume_12": "Party Hat"}))
	ctx.Resolver = bot.NewPokemonResolver(gd, ctx.Translations, []string{"en"}, nil)
	ctx.GameData = gd
	ctx.RecentActivity = tracker.NewRecentActivity()
	return ctx
}

func TestInfo_Pokemon_RecentRaidCostumes(t *testing.T) {
	ctx := infoRaidCostumeCtx(t)
	ctx.RecentActivity.RecordRaidCostume(25, 12)
	replies := (&InfoCommand{}).Run(ctx, []string{"pikachu"})
	if len(replies) == 0 {
		t.Fatal("expected a reply")
	}
	text := replies[0].Text
	if !strings.Contains(text, "12 — Party Hat") || !strings.Contains(text, "Recently-seen raid costumes") {
		t.Errorf("expected recent raid costume section, got: %q", text)
	}
}

func TestInfo_Pokemon_NoRaidCostumes_SectionOmitted(t *testing.T) {
	ctx := infoRaidCostumeCtx(t)
	replies := (&InfoCommand{}).Run(ctx, []string{"pikachu"})
	if len(replies) == 0 {
		t.Fatal("expected a reply")
	}
	if strings.Contains(replies[0].Text, "Recently-seen raid costumes") {
		t.Errorf("no raid-costume section when none recorded, got: %q", replies[0].Text)
	}
}
```

- [ ] **Step 2: Run — verify it fails.** `go test ./internal/bot/commands/ -run TestInfo_Pokemon_RecentRaidCostumes -v` → FAIL (`availableRaidCostumes` undefined / section absent).

- [ ] **Step 3: Implement**

`info.go` — add a helper mirroring `availableCostumes`:
```go
// availableRaidCostumes returns "id — name" display strings for costumes
// recently seen on raid boss pokemonID (via RecentActivity), sorted by id.
func (c *InfoCommand) availableRaidCostumes(ctx *bot.CommandContext, pokemonID int) []string {
	if ctx.RecentActivity == nil {
		return nil
	}
	ids := ctx.RecentActivity.RecentRaidCostumes(pokemonID)
	if len(ids) == 0 {
		return nil
	}
	sort.Ints(ids)
	tr := ctx.Tr()
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		result = append(result, fmt.Sprintf("%d — %s", id, costumeName(ctx, tr, id)))
	}
	return result
}
```
Render it directly after the spawn "Recently-seen costumes" block:
```go
	raidCostumes := c.availableRaidCostumes(ctx, pokemonID)
	if len(raidCostumes) > 0 {
		sb.WriteByte('\n')
		sb.WriteString(tr.T("msg.info.recent_raid_costumes") + "\n")
		for _, rc := range raidCostumes {
			sb.WriteString("  " + rc + "\n")
		}
	}
```
`en.json` — add next to `msg.info.available_costumes`:
```json
	"msg.info.recent_raid_costumes": "**Recently-seen raid costumes:**",
```

- [ ] **Step 4: GREEN + gate + commit**

```bash
go test ./internal/bot/commands/ -run 'TestInfo_Pokemon_.*RaidCostume' -v
go build ./... && go vet ./... && go test -count=1 ./internal/bot/... ./internal/i18n/... && golangci-lint run ./internal/bot/...
git add processor/internal/bot/commands/info.go processor/internal/bot/commands/info_raid_costume_test.go processor/internal/i18n/locale/en.json
git commit -m "feat(info): recently-seen raid costumes section"
```

---

### Task 7: Slash `/raid costume` option + autocomplete boost

**Files:**
- Modify: `processor/internal/discordbot/slash/definitions.go` (raid option)
- Modify: `processor/internal/discordbot/slash/mappers/raid.go` (emit `costume:`)
- Modify: `processor/internal/discordbot/slash/dispatcher.go` (route + boost)
- Test: `processor/internal/discordbot/slash/mappers/raid_test.go` + `dispatcher_test.go`

**Interfaces:** Consumes `autocomplete.Costume`, `autocomplete.PrependRecentCostumes`, `autocomplete.ResolvePokemonID`, `RecentRaidCostumes`.

- [ ] **Step 1: Write the failing tests**

Mapper test (append to `mappers/raid_test.go`; the file asserts exact token slices via `reflect.DeepEqual` — no `contains` helper). `/raid` with `boss=pikachu` + `costume=1` maps to `["pikachu", "costume:1"]` (boss emits the pokemon name as a bare token; costume appends `costume:1`):
```go
func TestRaidMapper_Costume(t *testing.T) {
	tokens, err := Raid([]*discordgo.ApplicationCommandInteractionDataOption{
		{Name: "boss", Type: discordgo.ApplicationCommandOptionString, Value: "pikachu"},
		{Name: "costume", Type: discordgo.ApplicationCommandOptionString, Value: "1"},
	})
	if err != nil {
		t.Fatalf("Raid mapper error: %v", err)
	}
	if !reflect.DeepEqual(tokens, []string{"pikachu", "costume:1"}) {
		t.Errorf("tokens=%v, want [pikachu costume:1]", tokens)
	}
}
```
> Confirm the token order matches the mapper's append order (boss token before the costume token). If the mapper emits boss differently, match its actual output.
Dispatcher test (append to `dispatcher_test.go`, mirror `TestRouteAutocomplete_TrackCostume_BoostsRecentForPokemon`): a `/raid costume` autocomplete with sibling `boss=pikachu` and a recorded raid costume boosts it first. Build the interaction with a `boss` (not `pokemon`) option; prime `RecordRaidCostume(25, 1)` on the deps' RecentActivity; use a costume that sorts AFTER the alphabetical-first base entry (id 1 "Holiday 2016" vs base "Flying").

- [ ] **Step 2: Run — verify they fail.**

- [ ] **Step 3: Implement**

`definitions.go` `raidOptions` — add to the `opts` slice (mirror the `boss` stringOpt with autocomplete=true):
```go
		stringOpt(bundle, "raid.costume", "costume", "Raid boss costume", false, true),
```
`mappers/raid.go` — after the boss/level/team tokens, add:
```go
	if costume := getString(o["costume"]); costume != "" {
		tokens = append(tokens, "costume:"+costume)
	}
```
`dispatcher.go` `routeAutocomplete` — add a case for `(cmd="raid", opt="costume")`:
```go
	case opt == "costume" && cmd == "raid":
		base := autocomplete.Costume(context.Background(), d.deps, focused, userLang)
		if focused == "" && d.deps != nil && d.deps.RecentActivity != nil {
			if pid := autocomplete.ResolvePokemonID(d.deps, siblingOptionString(ic, "boss")); pid > 0 {
				base = autocomplete.PrependRecentCostumes(base, d.deps, d.deps.RecentActivity.RecentRaidCostumes(pid), userLang)
			}
		}
		return base
```

- [ ] **Step 4: GREEN + gate + commit**

```bash
go test ./internal/discordbot/slash/... -run 'Raid.*Costume|Costume.*Raid' -v
go build ./... && go vet ./... && go test -count=1 ./internal/discordbot/... && golangci-lint run ./internal/discordbot/...
git add processor/internal/discordbot/slash/definitions.go processor/internal/discordbot/slash/mappers/raid.go processor/internal/discordbot/slash/mappers/raid_test.go processor/internal/discordbot/slash/dispatcher.go processor/internal/discordbot/slash/dispatcher_test.go
git commit -m "feat(slash): /raid costume option + recent-raid-costume autocomplete boost"
```

---

### Task 8: v2 raid API

**Files:**
- Modify: `processor/internal/api/v2_raid.go`
- Regenerate: the OpenAPI golden (whatever the repo's golden-update command is; mirror how the v2 pokemon costume change regenerated it)
- Test: `processor/internal/api/v2_raid_test.go` (append round-trip)

**Interfaces:** Consumes `db.RaidTrackingAPI.Costume` (Task 1).

- [ ] **Step 1: Write the failing test**

Append a round-trip test (mirror the v2 pokemon costume test): create a v2 raid rule with `costume` set → stored `Costume` matches; create with `costume` null/omitted → stored 9000; read back → `ptrUnless(9000)` returns null at wildcard, the value otherwise. Follow the existing `v2_raid_test.go` request/response idiom.

- [ ] **Step 2: Run — verify it fails** (compile: `v2RaidRule` has no `Costume`).

- [ ] **Step 3: Implement**

`v2_raid.go` `v2RaidRule` — add after `Form`:
```go
	Form      *int    `json:"form,omitempty" nullable:"true" doc:"..."`
	Costume   *int    `json:"costume,omitempty" nullable:"true" doc:"Costume id. Omit/null = any (stored 9000). 0 = no costume. N = that costume."`
```
`translateV2Raid` — add after `Form: valueOr(req.Form, 0),`:
```go
		Costume:               valueOr(req.Costume, 9000),
```
`raidRowToRule` — add after `Form: ptrUnless(row.Form, 0),`:
```go
		Costume:   ptrUnless(row.Costume, 9000),
```

- [ ] **Step 4: Regenerate OpenAPI golden**

Regenerate `internal/api/testdata/openapi.golden.json`:
```bash
UPDATE_GOLDEN=1 go test ./internal/api/ -run TestOpenAPIGolden
```
Then run it without the env var to confirm it passes: `go test ./internal/api/ -run TestOpenAPIGolden`. Inspect the golden diff — it must be **additive** (a `costume` property on the v2 raid rule request/response schemas only). Do NOT hand-edit the golden.

- [ ] **Step 5: GREEN + full gate + commit**

```bash
go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...
git add processor/internal/api/v2_raid.go processor/internal/api/v2_raid_test.go <openapi-golden-path>
git commit -m "feat(api): v2 raid Costume field (9000/0/null semantics)"
```

---

### Final: full gate

- [ ] From `processor/`: `go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...` — all green, `0 issues.`
