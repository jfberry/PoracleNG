# `!info` Forms & Costumes Consistency Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `!info <pokemon>` forms/costumes consistent — add a raid-forms recency tracker + section, render costume sections copy-pasteable, truncate the long available-forms roster with `!info <pokemon> forms`/`costumes` reveal subcommands, and add a `/raid form` slash option.

**Architecture:** Mirror the existing raid-costume recency + `/raid costume` slash work; refactor the `!info` costume sections to the copy-pasteable format the form sections already use.

**Tech Stack:** Go, `tracker.RecentActivity`, `bot/commands/info.go`, discordgo autocomplete.

Design spec: `docs/superpowers/specs/2026-07-16-info-forms-costumes-consistency-design.md`.

## Global Constraints

- **Copy-pasteable format:** costume/form recency lines are `ctx.Code("<pokeName> <filter>:<name>")` where `<name>` is the translated name lowercased with spaces→underscores; unresolved names are **skipped** (mirrors `availableRecentForms`). `pokeName` = `ctx.Translations.For("en").T(gamedata.PokemonTranslationKey(pokemonID))`.
- **Recency mirrors the raid-costume trio** exactly: `RecordRaidForm`/`RecentRaidForms` skip id ≤ 0, same mutex + `active()` window, separate bucket (must not leak into spawn `RecentForms` or raid costumes).
- **Only the available-forms roster truncates** (cap **10**); recency sections show in full.
- **Section order** in `!info <pokemon>`: recently-seen forms → raid forms → costumes → raid costumes → available forms.
- **Sub-routes** are detected in `pokemonInfo` via a trailing `args` keyword; they must NOT be added to the top-level `Run` switch (that would create a global `!info forms`).
- **`/raid form` mirrors `/raid costume`** (definitions option, mapper token, dispatcher boost from the `boss` sibling).
- **Pre-commit gate (from `processor/`):** `go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...`.

---

### Task 1: RecentActivity raid-forms bucket + producer

**Files:**
- Modify: `processor/internal/tracker/recent_activity.go`
- Modify: `processor/cmd/processor/raid.go`
- Test: `processor/internal/tracker/recent_activity_raidform_test.go` (create)

**Interfaces:** Produces `RecordRaidForm(pokemonID, form int)`, `RecentRaidForms(pokemonID int) []int` — consumed by `!info` (Task 3) and `/raid form` (Task 4).

- [ ] **Step 1: Write the failing test** (mirror `recent_activity_raidcostume_test.go`)

```go
package tracker

import "testing"

func TestRecentRaidForms(t *testing.T) {
	r := NewRecentActivity()
	r.RecordRaidForm(25, 598)
	r.RecordRaidForm(25, 680)
	r.RecordRaidForm(25, 0) // any-form placeholder: ignored
	if got := r.RecentRaidForms(25); len(got) != 2 {
		t.Fatalf("RecentRaidForms(25) = %v, want two entries", got)
	}
	if len(r.RecentRaidForms(999)) != 0 {
		t.Error("unknown boss should have no recent raid forms")
	}
	// Separate from spawn forms and from raid costumes.
	if len(r.RecentForms(25)) != 0 {
		t.Error("raid forms must not leak into spawn RecentForms")
	}
}
```

- [ ] **Step 2: Run — verify it fails.** `go test ./internal/tracker/ -run TestRecentRaidForms -v` → FAIL (undefined).

- [ ] **Step 3: Implement** (mirror `raidCostumesByPokemon`/`RecordRaidCostume`/`RecentRaidCostumes`)

`recent_activity.go` — field + init:
```go
	raidCostumesByPokemon map[int]map[int]time.Time
	raidFormsByPokemon    map[int]map[int]time.Time
```
```go
		raidCostumesByPokemon: make(map[int]map[int]time.Time),
		raidFormsByPokemon:    make(map[int]map[int]time.Time),
```
Methods (after `RecentRaidCostumes`):
```go
// RecordRaidForm marks form as recently seen on a raid boss pokemonID.
func (r *RecentActivity) RecordRaidForm(pokemonID, form int) {
	if pokemonID <= 0 || form <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	inner := r.raidFormsByPokemon[pokemonID]
	if inner == nil {
		inner = make(map[int]time.Time)
		r.raidFormsByPokemon[pokemonID] = inner
	}
	inner[form] = r.now()
}

// RecentRaidForms returns the recency-windowed form IDs recently seen on raid
// boss pokemonID.
func (r *RecentActivity) RecentRaidForms(pokemonID int) []int {
	r.mu.Lock()
	inner := r.raidFormsByPokemon[pokemonID]
	r.mu.Unlock()
	if inner == nil {
		return nil
	}
	return r.active(inner)
}
```
`cmd/processor/raid.go` — add beside the existing raid-costume producer (inside the same `if ps.recentActivity != nil {` block):
```go
			if raid.Form > 0 {
				ps.recentActivity.RecordRaidForm(raid.PokemonID, raid.Form)
			}
```

- [ ] **Step 4: GREEN + gate + commit**

```bash
go test ./internal/tracker/ -run TestRecentRaidForms -v
go build ./... && go vet ./... && go test -count=1 ./internal/tracker/ ./cmd/... && golangci-lint run ./internal/tracker/ ./cmd/...
git add processor/internal/tracker/recent_activity.go processor/internal/tracker/recent_activity_raidform_test.go processor/cmd/processor/raid.go
git commit -m "feat(tracker): RecordRaidForm/RecentRaidForms bucket + producer"
```

---

### Task 2: Copy-pasteable costume sections + raid-forms section + reorder

**Files:**
- Modify: `processor/internal/bot/commands/info.go`
- Modify: `processor/internal/i18n/locale/en.json`
- Test: `processor/internal/bot/commands/info_raid_form_test.go` (create) + extend costume tests

**Interfaces:** Consumes `RecentRaidForms` (Task 1). Produces `availableRecentRaidForms`, `costumeTrackLines`.

- [ ] **Step 1: Write the failing tests**

Add to a new `info_raid_form_test.go` (reuse an `!info` test ctx helper that primes GameData + translations + RecentActivity — mirror `infoRaidCostumeCtx`/`infoFormCtx`; include `poke_25`, `form_680`="Winter 2023", `costume_1`="Holiday 2016", `GameData.Costumes{1}`, and Monsters `{25,0}`/`{25,680}`):

```go
func TestInfo_Pokemon_RecentRaidForms(t *testing.T) {
	ctx := infoFormCostumeCtx(t) // helper priming forms + costumes + RecentActivity
	ctx.RecentActivity.RecordRaidForm(25, 680)
	text := (&InfoCommand{}).Run(ctx, []string{"pikachu"})[0].Text
	if !strings.Contains(text, "Recently-seen raid forms") || !strings.Contains(text, "form:winter_2023") {
		t.Errorf("expected copy-pasteable recent raid forms section, got: %q", text)
	}
}

func TestInfo_Pokemon_CostumesCopyPasteable(t *testing.T) {
	ctx := infoFormCostumeCtx(t)
	ctx.RecentActivity.RecordCostume(25, 1)      // spawn costume
	ctx.RecentActivity.RecordRaidCostume(25, 1)  // raid costume
	text := (&InfoCommand{}).Run(ctx, []string{"pikachu"})[0].Text
	if !strings.Contains(text, "costume:holiday_2016") {
		t.Errorf("costume sections must be copy-pasteable 'costume:<name>', got: %q", text)
	}
	if strings.Contains(text, "1 — Holiday 2016") {
		t.Errorf("costume sections must NOT use the old 'id — name' format, got: %q", text)
	}
}
```

- [ ] **Step 2: Run — verify they fail.**

- [ ] **Step 3: Implement**

Add a shared costume line helper + a raid-forms helper in `info.go`:
```go
// costumeTrackLines builds copy-pasteable "<pokemon> costume:<name>" strings for
// the given (sorted) costume ids — name lowercased with spaces→underscores,
// mirroring availableRecentForms's form format. Unresolved names are skipped.
func (c *InfoCommand) costumeTrackLines(ctx *bot.CommandContext, pokemonID int, ids []int) []string {
	tr := ctx.Tr()
	enTr := ctx.Translations.For("en")
	pokeName := enTr.T(gamedata.PokemonTranslationKey(pokemonID))
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		name := costumeName(ctx, tr, id)
		if name == "" || name == gamedata.CostumeTranslationKey(id) {
			continue
		}
		trackingName := strings.ReplaceAll(strings.ToLower(name), " ", "_")
		result = append(result, ctx.Code(fmt.Sprintf("%s costume:%s", pokeName, trackingName)))
	}
	return result
}

// availableRecentRaidForms mirrors availableRecentForms but sources RecentRaidForms.
func (c *InfoCommand) availableRecentRaidForms(ctx *bot.CommandContext, pokemonID int) []string {
	if ctx.RecentActivity == nil {
		return nil
	}
	ids := ctx.RecentActivity.RecentRaidForms(pokemonID)
	if len(ids) == 0 {
		return nil
	}
	sort.Ints(ids)
	tr := ctx.Tr()
	enTr := ctx.Translations.For("en")
	pokeName := enTr.T(gamedata.PokemonTranslationKey(pokemonID))
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		key := gamedata.FormTranslationKey(id)
		name := tr.T(key)
		if name == key {
			name = enTr.T(key)
		}
		if name == key {
			continue
		}
		trackingName := strings.ReplaceAll(strings.ToLower(name), " ", "_")
		result = append(result, ctx.Code(fmt.Sprintf("%s form:%s", pokeName, trackingName)))
	}
	return result
}
```
Rewrite `availableCostumes` and `availableRaidCostumes` to use the shared helper:
```go
func (c *InfoCommand) availableCostumes(ctx *bot.CommandContext, pokemonID int) []string {
	if ctx.RecentActivity == nil {
		return nil
	}
	ids := ctx.RecentActivity.RecentCostumes(pokemonID)
	if len(ids) == 0 {
		return nil
	}
	sort.Ints(ids)
	return c.costumeTrackLines(ctx, pokemonID, ids)
}
func (c *InfoCommand) availableRaidCostumes(ctx *bot.CommandContext, pokemonID int) []string {
	if ctx.RecentActivity == nil {
		return nil
	}
	ids := ctx.RecentActivity.RecentRaidCostumes(pokemonID)
	if len(ids) == 0 {
		return nil
	}
	sort.Ints(ids)
	return c.costumeTrackLines(ctx, pokemonID, ids)
}
```
In `pokemonInfo`, insert the raid-forms section between the spawn-forms and spawn-costumes blocks (final order: forms → raid forms → costumes → raid costumes → available forms):
```go
	// Recently-seen raid forms
	recentRaidForms := c.availableRecentRaidForms(ctx, pokemonID)
	if len(recentRaidForms) > 0 {
		sb.WriteByte('\n')
		sb.WriteString(tr.T("msg.info.recent_raid_forms") + "\n")
		for _, f := range recentRaidForms {
			sb.WriteString("  " + f + "\n")
		}
	}
```
`en.json` — add:
```json
	"msg.info.recent_raid_forms": "**Recently-seen raid forms:**",
```

- [ ] **Step 4: GREEN + gate + commit**

```bash
go test ./internal/bot/commands/ -run 'TestInfo_Pokemon_(RecentRaidForms|CostumesCopyPasteable)' -v
go build ./... && go vet ./... && go test -count=1 ./internal/bot/... ./internal/i18n/... && golangci-lint run ./internal/bot/...
git add processor/internal/bot/commands/info.go processor/internal/bot/commands/info_raid_form_test.go processor/internal/i18n/locale/en.json
git commit -m "feat(info): copy-pasteable costume sections + recently-seen raid forms"
```

---

### Task 3: Roster truncation + `!info <pokemon> forms`/`costumes` sub-routes

**Files:**
- Modify: `processor/internal/bot/commands/info.go`
- Modify: `processor/internal/i18n/locale/en.json`
- Test: `processor/internal/bot/commands/info_subroute_test.go` (create)

**Interfaces:** Consumes the Task 2 helpers + `availableForms`.

- [ ] **Step 1: Write the failing tests**

```go
func TestInfo_Pokemon_FormsTruncated(t *testing.T) {
	ctx := manyFormsCtx(t) // ctx whose GameData.Monsters gives species 25 >10 named forms
	text := (&InfoCommand{}).Run(ctx, []string{"pikachu"})[0].Text
	if !strings.Contains(text, "More than 10 forms") {
		t.Errorf("expected roster truncation hint, got: %q", text)
	}
}

func TestInfo_Pokemon_FormsSubroute(t *testing.T) {
	ctx := manyFormsCtx(t)
	ctx.RecentActivity.RecordForm(25, 680)
	text := (&InfoCommand{}).Run(ctx, []string{"pikachu", "forms"})[0].Text
	// Full roster (no truncation hint) AND recent forms.
	if strings.Contains(text, "More than 10 forms") {
		t.Errorf("!info pikachu forms must show the full roster untruncated, got: %q", text)
	}
	if !strings.Contains(text, "form:winter_2023") {
		t.Errorf("!info pikachu forms should include recent forms, got: %q", text)
	}
}

func TestInfo_Pokemon_CostumesSubroute(t *testing.T) {
	ctx := infoFormCostumeCtx(t)
	ctx.RecentActivity.RecordCostume(25, 1)
	ctx.RecentActivity.RecordRaidCostume(25, 8) // a second, raid-only costume
	text := (&InfoCommand{}).Run(ctx, []string{"pikachu", "costumes"})[0].Text
	if !strings.Contains(text, "costume:holiday_2016") {
		t.Errorf("!info pikachu costumes should show combined recent costumes, got: %q", text)
	}
}
```
> `manyFormsCtx` primes `GameData.Monsters` with >10 `{25, N}` entries and matching `form_N` translations. Reuse/extend the Task 2 ctx helper.

- [ ] **Step 2: Run — verify they fail.**

- [ ] **Step 3: Implement**

**Roster truncation** — in `pokemonInfo`'s available-forms block:
```go
	forms := c.availableForms(ctx, pokemonID)
	if len(forms) > 0 {
		sb.WriteByte('\n')
		sb.WriteString(tr.T("msg.info.available_forms") + "\n")
		const formCap = 10
		shown := forms
		if len(shown) > formCap {
			shown = shown[:formCap]
		}
		for _, f := range shown {
			sb.WriteString("  " + f + "\n")
		}
		if len(forms) > formCap {
			enTr := ctx.Translations.For("en")
			pokeName := enTr.T(gamedata.PokemonTranslationKey(pokemonID))
			hintCmd := ctx.Code(bot.CommandPrefix(ctx) + tr.T("cmd.info") + " " + pokeName + " " + tr.T("msg.info.sub.forms"))
			sb.WriteString("  " + tr.Tf("msg.info.more_forms", formCap, hintCmd) + "\n")
		}
	}
```

**Sub-route detection** — in `pokemonInfo`, after the `form:` extraction loop and before `name := strings.Join(nameArgs, " ")`, peel off a trailing forms/costumes keyword:
```go
	tr := ctx.Tr()
	enTr := ctx.Translations.For("en")
	subMatch := func(key, tok string) bool {
		return tok == strings.ToLower(tr.T(key)) || tok == strings.ToLower(enTr.T(key))
	}
	var subMode string
	if len(nameArgs) > 1 {
		last := strings.ToLower(nameArgs[len(nameArgs)-1])
		switch {
		case subMatch("msg.info.sub.forms", last):
			subMode = "forms"
			nameArgs = nameArgs[:len(nameArgs)-1]
		case subMatch("msg.info.sub.costumes", last):
			subMode = "costumes"
			nameArgs = nameArgs[:len(nameArgs)-1]
		}
	}
```
(If `tr`/`enTr` are already declared later in `pokemonInfo`, hoist them here and remove the duplicate declarations.)

After `pokemonID` is resolved, branch to the sub-views:
```go
	switch subMode {
	case "forms":
		return c.pokemonFormsFull(ctx, pokemonID)
	case "costumes":
		return c.pokemonCostumesFull(ctx, pokemonID)
	}
```

**Sub-view renderers**:
```go
// pokemonFormsFull renders !info <pokemon> forms: recent forms (spawn + raid)
// plus the full available-forms roster (untruncated).
func (c *InfoCommand) pokemonFormsFull(ctx *bot.CommandContext, pokemonID int) []bot.Reply {
	tr := ctx.Tr()
	var sb strings.Builder
	writeSection := func(header string, lines []string) {
		if len(lines) == 0 {
			return
		}
		if sb.Len() > 0 {
			sb.WriteByte('\n')
		}
		sb.WriteString(tr.T(header) + "\n")
		for _, l := range lines {
			sb.WriteString("  " + l + "\n")
		}
	}
	writeSection("msg.info.recent_forms", c.availableRecentForms(ctx, pokemonID))
	writeSection("msg.info.recent_raid_forms", c.availableRecentRaidForms(ctx, pokemonID))
	writeSection("msg.info.available_forms", c.availableForms(ctx, pokemonID))
	if sb.Len() == 0 {
		return []bot.Reply{{Text: tr.T("msg.info.no_form_data")}}
	}
	return []bot.Reply{{Text: sb.String()}}
}

// pokemonCostumesFull renders !info <pokemon> costumes: the combined recently-seen
// costumes (spawn + raid), deduped, copy-pasteable.
func (c *InfoCommand) pokemonCostumesFull(ctx *bot.CommandContext, pokemonID int) []bot.Reply {
	tr := ctx.Tr()
	seen := map[int]bool{}
	var ids []int
	if ctx.RecentActivity != nil {
		for _, id := range append(ctx.RecentActivity.RecentCostumes(pokemonID), ctx.RecentActivity.RecentRaidCostumes(pokemonID)...) {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	sort.Ints(ids)
	lines := c.costumeTrackLines(ctx, pokemonID, ids)
	if len(lines) == 0 {
		return []bot.Reply{{Text: tr.T("msg.info.no_costume_data")}}
	}
	var sb strings.Builder
	sb.WriteString(tr.T("msg.info.available_costumes") + "\n")
	for _, l := range lines {
		sb.WriteString("  " + l + "\n")
	}
	return []bot.Reply{{Text: sb.String()}}
}
```

`en.json` — add:
```json
	"msg.info.sub.forms": "forms",
	"msg.info.more_forms": "More than {0} forms — do {1} for the full list",
	"msg.info.no_form_data": "No form data for that pokemon.",
```
(`msg.info.no_costume_data` already exists.)

- [ ] **Step 4: GREEN + gate + commit**

```bash
go test ./internal/bot/commands/ -run 'TestInfo_Pokemon_(FormsTruncated|FormsSubroute|CostumesSubroute)' -v
go build ./... && go vet ./... && go test -count=1 ./internal/bot/... ./internal/i18n/... && golangci-lint run ./internal/bot/...
git add processor/internal/bot/commands/info.go processor/internal/bot/commands/info_subroute_test.go processor/internal/i18n/locale/en.json
git commit -m "feat(info): truncate forms roster + !info <pokemon> forms/costumes subroutes"
```

---

### Task 4: `/raid form` slash option + autocomplete boost

**Files:**
- Modify: `processor/internal/discordbot/slash/definitions.go`
- Modify: `processor/internal/discordbot/slash/mappers/raid.go`
- Modify: `processor/internal/discordbot/slash/dispatcher.go`
- Regenerate: slash fixtures (`testdata/raid.json`, `internal/bot/testdata/parity.yaml`) — a new slash option changes the definition snapshot + parity coverage. Update them the way Task 7 of the raid-costume plan did (run the snapshot/parity tests, apply their regenerated output; do NOT hand-craft).
- Test: `mappers/raid_test.go` + `dispatcher_test.go`

**Interfaces:** Consumes `autocomplete.Form`, `autocomplete.PrependRecentForms`, `autocomplete.ResolvePokemonID`, `RecentRaidForms` (Task 1).

- [ ] **Step 1: Write the failing tests**

Mapper test (exact `reflect.DeepEqual`, mirror `TestRaidMapper_Costume`):
```go
func TestRaidMapper_Form(t *testing.T) {
	tokens, err := Raid([]*discordgo.ApplicationCommandInteractionDataOption{
		{Name: "boss", Type: discordgo.ApplicationCommandOptionString, Value: "pikachu"},
		{Name: "form", Type: discordgo.ApplicationCommandOptionString, Value: "alolan"},
	})
	if err != nil {
		t.Fatalf("Raid mapper error: %v", err)
	}
	if !reflect.DeepEqual(tokens, []string{"pikachu", "form:alolan"}) {
		t.Errorf("tokens=%v, want [pikachu form:alolan]", tokens)
	}
}
```
Dispatcher routing test — mirror `TestRouteAutocomplete_RaidCostume_BoostsRecentForBoss` **exactly** (same deps builder, same `boss`-option interaction builder it uses), swapping costume→form: install a FRESH `RecentActivity` that primes ONLY the raid-forms bucket (so it discriminates `RecentRaidForms` from spawn `RecentForms`), record form **680** "Winter 2023" (sorts after the base-alphabetical first form → first-position proves boosting), route `("raid", "form", "", "en", <boss=pikachu IC>)`, and assert `out[0].Name == "Winter 2023"`.

```go
func TestRouteAutocomplete_RaidForm_BoostsRecentForBoss(t *testing.T) {
	d := NewDispatcher(Config{})
	d.bundle = testBundle(t)
	d.cfgRoot = &config.Config{}
	d.deps = costumeFormRouteDeps(t) // GameData.Monsters {25,680} + form_680="Winter 2023" (add if absent, additive)
	d.deps.RecentActivity = tracker.NewRecentActivity()
	d.deps.RecentActivity.RecordRaidForm(25, 680)
	ic := <build an autocomplete InteractionCreate with a "boss" String option = "pikachu",
	       copying the exact helper used by TestRouteAutocomplete_RaidCostume_BoostsRecentForBoss>
	out := d.routeAutocomplete("raid", "form", "", "en", ic)
	if len(out) == 0 || out[0].Name != "Winter 2023" {
		t.Errorf("/raid form empty focused: first=%+v, want Winter 2023 (recent raid form)", firstName(out))
	}
}
```
> Read `TestRouteAutocomplete_RaidCostume_BoostsRecentForBoss` in `dispatcher_test.go` and reuse its `boss`-option interaction builder verbatim. The FRESH-RecentActivity + raid-only recording is what makes the test discriminate the raid bucket (a regression to spawn `RecentForms` would leave the boost empty → base-alphabetical first → FAIL).

- [ ] **Step 2: Run — verify they fail.**

- [ ] **Step 3: Implement**

`definitions.go` `raidOptions` — add before the costume option (or adjacent):
```go
		stringOpt(bundle, "raid.form", "form", "Raid boss form", false, true),
```
`mappers/raid.go` — after the costume emit:
```go
	if form := getString(o["form"]); form != "" {
		tokens = append(tokens, "form:"+form)
	}
```
`dispatcher.go` `routeAutocomplete` — add a case (mirror the `(cmd="raid", opt="costume")` case):
```go
	case opt == "form" && cmd == "raid":
		base := autocomplete.Form(context.Background(), d.deps, siblingOptionString(ic, "boss"), focused, userLang)
		if focused == "" && d.deps != nil && d.deps.RecentActivity != nil {
			if pid := autocomplete.ResolvePokemonID(d.deps, siblingOptionString(ic, "boss")); pid > 0 {
				base = autocomplete.PrependRecentForms(base, d.deps, d.deps.RecentActivity.RecentRaidForms(pid), userLang)
			}
		}
		return base
```

- [ ] **Step 4: Regenerate fixtures + GREEN + full gate + commit**

```bash
# regenerate slash definition snapshot + parity fixture per their test's update mechanism
go test ./internal/discordbot/slash/... -run 'Raid.*Form|Form.*Raid' -v
go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...
git add processor/internal/discordbot/slash/definitions.go processor/internal/discordbot/slash/mappers/raid.go processor/internal/discordbot/slash/mappers/raid_test.go processor/internal/discordbot/slash/dispatcher.go processor/internal/discordbot/slash/dispatcher_test.go processor/internal/discordbot/slash/testdata/raid.json processor/internal/bot/testdata/parity.yaml
git commit -m "feat(slash): /raid form option + recent-raid-form autocomplete boost"
```

---

### Final: full gate

- [ ] From `processor/`: `go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...` — all green, `0 issues.`
