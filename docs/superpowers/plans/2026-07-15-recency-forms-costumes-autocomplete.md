# Recency-Aware Form & Costume Surfaces — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Surface recently-spawned costumes and forms at the top of the `/track` pickers, and add a "recently seen forms" section to `!info`, reusing the existing `RecentActivity` autocomplete-boost mechanism.

**Architecture:** Add a per-species recent-forms bucket to `tracker.RecentActivity` (mirror of the existing recent-costumes bucket), feed it from the pokemon webhook handler, add two `Prepend*` boost helpers, wire both into the dispatcher's `/track costume` and `/track form` autocomplete cases, and add a recent-forms section to `!info`.

**Tech Stack:** Go, discordgo autocomplete, existing `tracker.RecentActivity` + `internal/discordbot/slash/autocomplete` boost pattern.

Design spec: `docs/superpowers/specs/2026-07-15-recency-forms-costumes-autocomplete-design.md`.

## Global Constraints

- **Reuse the boost mechanics verbatim:** recent entries prepended, boost capped at **10**, total capped at **25** (Discord's hard limit), dedup by choice **Value**, and the boost only fires when the **focused text is empty**. No new config/tunables.
- **6-hour recency window** — reuse `RecentActivity.active()`; do not add a new TTL.
- **`RecordForm` and `RecordCostume` skip id ≤ 0.** Form 0 is the "any-form" placeholder (never a trackable value); costume 0 is "no costume" and already skipped.
- **Value contracts unchanged:** boosted **costume** choice `Value` = costume **id as string** (matches `autocomplete.Costume`); boosted **form** choice `Value` = **lowercased translated name** (matches `autocomplete.Form`). Labels come from `costumeLabel` / `formLabel` (user lang → English fallback).
- **Per-species recency needs a pokemon:** the boost only applies when the sibling `pokemon` option resolves to an id > 0; otherwise the base list is returned unchanged.
- **Mirror existing code exactly** — `RecordCostume`/`RecentCostumes`, `PrependActiveItems`, `availableCostumes` are the templates. Do not invent new shapes.
- **Pre-commit gate (from `processor/`), all four must pass:** `go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...`.

---

### Task 1: RecentActivity recent-forms bucket

**Files:**
- Modify: `processor/internal/tracker/recent_activity.go`
- Test: `processor/internal/tracker/recent_activity_form_test.go` (create)

**Interfaces:**
- Consumes: existing `record`/`active` helpers, `now func() time.Time`.
- Produces: `func (r *RecentActivity) RecordForm(pokemonID, form int)` and `func (r *RecentActivity) RecentForms(pokemonID int) []int` — used by the producer (Task 2) and dispatcher (Task 4).

- [ ] **Step 1: Write the failing test**

Create `processor/internal/tracker/recent_activity_form_test.go`:

```go
package tracker

import "testing"

func TestRecentForms(t *testing.T) {
	r := NewRecentActivity()
	r.RecordForm(25, 598)
	r.RecordForm(25, 680)
	r.RecordForm(25, 0) // any-form placeholder: ignored
	got := r.RecentForms(25)
	if len(got) != 2 {
		t.Fatalf("RecentForms(25) = %v, want two entries [598 680]", got)
	}
	if len(r.RecentForms(999)) != 0 {
		t.Error("unknown pokemon should have no recent forms")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/tracker/ -run TestRecentForms -v`
Expected: FAIL — `RecordForm`/`RecentForms` undefined (build failure).

- [ ] **Step 3: Write minimal implementation**

In `recent_activity.go`, add the field to the struct (next to `costumesByPokemon`):

```go
	costumesByPokemon map[int]map[int]time.Time
	formsByPokemon    map[int]map[int]time.Time
```

Initialise it in `NewRecentActivity` (next to `costumesByPokemon`):

```go
		costumesByPokemon: make(map[int]map[int]time.Time),
		formsByPokemon:    make(map[int]map[int]time.Time),
```

Add the two methods after `RecentCostumes` (mirroring it exactly):

```go
// RecordForm marks form as recently seen on pokemonID. Form 0 (the "any form"
// placeholder) is ignored — it is never a trackable value.
func (r *RecentActivity) RecordForm(pokemonID, form int) {
	if pokemonID <= 0 || form <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	inner := r.formsByPokemon[pokemonID]
	if inner == nil {
		inner = make(map[int]time.Time)
		r.formsByPokemon[pokemonID] = inner
	}
	inner[form] = r.now()
}

// RecentForms returns the recency-windowed list of form IDs recently seen on
// pokemonID.
func (r *RecentActivity) RecentForms(pokemonID int) []int {
	r.mu.Lock()
	inner := r.formsByPokemon[pokemonID]
	r.mu.Unlock()
	if inner == nil {
		return nil
	}
	return r.active(inner) // reuse the existing recency window logic
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/tracker/ -run TestRecentForms -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/tracker/recent_activity.go processor/internal/tracker/recent_activity_form_test.go
git commit -m "feat(tracker): per-species RecordForm/RecentForms recency bucket"
```

---

### Task 2: Producer — record spawn form

**Files:**
- Modify: `processor/cmd/processor/pokemon.go` (the `RecordCostume` call site, ~line 57)

**Interfaces:**
- Consumes: `RecentActivity.RecordForm` (Task 1), `pokemon.PokemonID`, `pokemon.Form`.
- Produces: nothing new; feeds the recency bucket at runtime.

**Note:** This is a one-line glue change in a webhook handler with no isolated unit seam (the existing `RecordCostume` producer likewise has no unit test). Verification is build + vet; the behaviour is exercised by Task 1's tested `RecentForms` and Task 4's routing tests.

- [ ] **Step 1: Add the producer call**

The current block reads:

```go
		if ps.recentActivity != nil && pokemon.Costume > 0 {
			ps.recentActivity.RecordCostume(pokemon.PokemonID, pokemon.Costume)
		}
```

Change it to also record the form (guard `Form > 0` mirrors the `Costume > 0` guard; `RecordForm` also guards internally):

```go
		if ps.recentActivity != nil {
			if pokemon.Costume > 0 {
				ps.recentActivity.RecordCostume(pokemon.PokemonID, pokemon.Costume)
			}
			if pokemon.Form > 0 {
				ps.recentActivity.RecordForm(pokemon.PokemonID, pokemon.Form)
			}
		}
```

- [ ] **Step 2: Verify build + vet**

Run: `go build ./... && go vet ./...`
Expected: no output (success).

- [ ] **Step 3: Commit**

```bash
git add processor/cmd/processor/pokemon.go
git commit -m "feat(processor): record spawn form into RecentActivity"
```

---

### Task 3: Autocomplete boost helpers + exported pokemon resolver

**Files:**
- Modify: `processor/internal/discordbot/slash/autocomplete/recent_activity_boost.go`
- Modify: `processor/internal/discordbot/slash/autocomplete/form.go` (add exported `ResolvePokemonID`)
- Test: `processor/internal/discordbot/slash/autocomplete/recent_activity_boost_test.go` (append)

**Interfaces:**
- Consumes: `costumeLabel` (costume.go), `formLabel` (form.go), `resolvePokemonID` (form.go).
- Produces:
  - `func PrependRecentCostumes(base []*discordgo.ApplicationCommandOptionChoice, deps *bot.BotDeps, costumeIDs []int, userLang string) []*discordgo.ApplicationCommandOptionChoice`
  - `func PrependRecentForms(base []*discordgo.ApplicationCommandOptionChoice, deps *bot.BotDeps, formIDs []int, userLang string) []*discordgo.ApplicationCommandOptionChoice`
  - `func ResolvePokemonID(deps *bot.BotDeps, name string) int`

- [ ] **Step 1: Write the failing tests**

Append to `recent_activity_boost_test.go`:

```go
func costumeFormBoostDeps(t *testing.T) *bot.BotDeps {
	t.Helper()
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"poke_25":     "Pikachu",
		"costume_1":   "Holiday 2016",
		"costume_8":   "Flying",
		"form_598":    "Normal",
		"form_680":    "Winter 2023",
	}))
	bundle.LinkFallbacks()
	gd := &gamedata.GameData{
		Costumes: map[int]gamedata.CostumeInfo{
			1: {ID: 1, Name: "Holiday 2016"},
			8: {ID: 8, Name: "Flying"},
		},
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 25, Form: 598}: {PokemonID: 25},
			{ID: 25, Form: 680}: {PokemonID: 25},
		},
	}
	return &bot.BotDeps{Translations: bundle, GameData: gd, Cfg: &config.Config{}}
}

func TestPrependRecentCostumes_BoostsFirstAndDedups(t *testing.T) {
	deps := costumeFormBoostDeps(t)
	base := Costume(context.Background(), deps, "", "en")
	// Use id 1 ("Holiday 2016"), which sorts AFTER "Flying" alphabetically, so
	// seeing it first proves the boost (not just alphabetical order).
	out := PrependRecentCostumes(base, deps, []int{1}, "en")
	if len(out) == 0 || out[0].Name != "Holiday 2016" {
		t.Fatalf("first = %+v, want Holiday 2016 (recent id 1 prepended)", firstName(out))
	}
	count := 0
	for _, c := range out {
		if c.Value == "1" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("costume 1 appears %d times, want 1 (dedup against base)", count)
	}
}

func TestPrependRecentCostumes_EmptyFallsThrough(t *testing.T) {
	deps := costumeFormBoostDeps(t)
	base := Costume(context.Background(), deps, "", "en")
	out := PrependRecentCostumes(base, deps, nil, "en")
	if len(out) != len(base) {
		t.Errorf("len(out)=%d, len(base)=%d — nil recency should pass through", len(out), len(base))
	}
}

func TestPrependRecentForms_BoostsFirstAndDedups(t *testing.T) {
	deps := costumeFormBoostDeps(t)
	base := Form(context.Background(), deps, "pikachu", "", "en")
	out := PrependRecentForms(base, deps, []int{680}, "en")
	if len(out) == 0 || out[0].Name != "Winter 2023" {
		t.Fatalf("first = %+v, want Winter 2023 (recent form 680 prepended)", firstName(out))
	}
	count := 0
	for _, c := range out {
		if c.Value == "winter 2023" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("form 'winter 2023' appears %d times, want 1 (dedup against base)", count)
	}
}

func TestResolvePokemonID(t *testing.T) {
	deps := costumeFormBoostDeps(t)
	if got := ResolvePokemonID(deps, "pikachu"); got != 25 {
		t.Errorf("ResolvePokemonID(pikachu) = %d, want 25", got)
	}
	if got := ResolvePokemonID(deps, "25"); got != 25 {
		t.Errorf("ResolvePokemonID(\"25\") = %d, want 25", got)
	}
	if got := ResolvePokemonID(deps, ""); got != 0 {
		t.Errorf("ResolvePokemonID(\"\") = %d, want 0", got)
	}
}
```

(`firstName` already exists in this test file; `config`, `gamedata`, `i18n`, `tracker`, `context` are already imported.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/discordbot/slash/autocomplete/ -run 'PrependRecent|ResolvePokemonID' -v`
Expected: FAIL — the three new functions are undefined.

- [ ] **Step 3: Write minimal implementation**

In `form.go`, add after `resolvePokemonID`:

```go
// ResolvePokemonID exposes resolvePokemonID to the dispatcher package so
// per-species recency boosts (RecentCostumes/RecentForms) can key on the
// sibling pokemon option. Accepts the canonical English name or a numeric id;
// returns 0 when unresolved.
func ResolvePokemonID(deps *bot.BotDeps, name string) int {
	return resolvePokemonID(deps, name)
}
```

In `recent_activity_boost.go`, add `"strconv"` to the import block, then append the two helpers:

```go
// PrependRecentCostumes prepends the costumes recently seen on the selected
// pokemon (from RecentActivity.RecentCostumes) to the flat costume choice list
// on /track costume. Label = translated costume name, Value = costume id as a
// string (matching autocomplete.Costume). Caps the boost at 10, dedups by
// Value, and stops at Discord's 25-choice limit — same contract as
// PrependActiveItems.
func PrependRecentCostumes(base []*discordgo.ApplicationCommandOptionChoice, deps *bot.BotDeps, costumeIDs []int, userLang string) []*discordgo.ApplicationCommandOptionChoice {
	if deps == nil || len(costumeIDs) == 0 || deps.Translations == nil {
		return base
	}
	out := make([]*discordgo.ApplicationCommandOptionChoice, 0, 25)
	seen := map[string]bool{}
	add := func(name, value string) bool {
		if seen[value] {
			return false
		}
		seen[value] = true
		out = append(out, &discordgo.ApplicationCommandOptionChoice{Name: name, Value: value})
		return len(out) >= 25
	}
	enTr := deps.Translations.For("en")
	userTr := deps.Translations.For(userLang)
	for i, id := range costumeIDs {
		if i >= 10 {
			break
		}
		label := costumeLabel(enTr, userTr, id)
		if label == "" {
			continue
		}
		if add(label, strconv.Itoa(id)) {
			return out
		}
	}
	for _, c := range base {
		v, _ := c.Value.(string)
		if add(c.Name, v) {
			return out
		}
	}
	return out
}

// PrependRecentForms prepends the forms recently seen on the selected pokemon
// (from RecentActivity.RecentForms) to autocomplete.Form's alphabetical list on
// /track form. Label/Value follow formLabel (translated name / lowercased
// name). Same 10/25/dedup contract as PrependRecentCostumes.
func PrependRecentForms(base []*discordgo.ApplicationCommandOptionChoice, deps *bot.BotDeps, formIDs []int, userLang string) []*discordgo.ApplicationCommandOptionChoice {
	if deps == nil || len(formIDs) == 0 || deps.Translations == nil {
		return base
	}
	out := make([]*discordgo.ApplicationCommandOptionChoice, 0, 25)
	seen := map[string]bool{}
	add := func(name, value string) bool {
		if seen[value] {
			return false
		}
		seen[value] = true
		out = append(out, &discordgo.ApplicationCommandOptionChoice{Name: name, Value: value})
		return len(out) >= 25
	}
	enTr := deps.Translations.For("en")
	userTr := deps.Translations.For(userLang)
	for i, id := range formIDs {
		if i >= 10 {
			break
		}
		label, value := formLabel(enTr, userTr, id)
		if value == "" {
			continue
		}
		if add(label, value) {
			return out
		}
	}
	for _, c := range base {
		v, _ := c.Value.(string)
		if add(c.Name, v) {
			return out
		}
	}
	return out
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/discordbot/slash/autocomplete/ -run 'PrependRecent|ResolvePokemonID' -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add processor/internal/discordbot/slash/autocomplete/recent_activity_boost.go processor/internal/discordbot/slash/autocomplete/form.go processor/internal/discordbot/slash/autocomplete/recent_activity_boost_test.go
git commit -m "feat(slash): PrependRecentCostumes/PrependRecentForms boost helpers"
```

---

### Task 4: Wire boosts into the dispatcher

**Files:**
- Modify: `processor/internal/discordbot/slash/dispatcher.go` (`routeAutocomplete`, the `form` and `costume` cases)
- Test: `processor/internal/discordbot/slash/dispatcher_test.go` (append)

**Interfaces:**
- Consumes: `autocomplete.ResolvePokemonID`, `autocomplete.PrependRecentCostumes`, `autocomplete.PrependRecentForms`, `RecentActivity.RecentCostumes`, `RecentActivity.RecentForms`, existing `siblingOptionString`.

- [ ] **Step 1: Write the failing tests**

Append to `dispatcher_test.go`. Build an autocomplete interaction with a `pokemon` sibling option (mirror the interaction construction already used in this file around the `HandleAutocomplete` tests):

```go
func costumeFormRouteDeps(t *testing.T) *bot.BotDeps {
	t.Helper()
	bundle := i18n.NewBundle()
	bundle.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"poke_25":   "Pikachu",
		"costume_1": "Holiday 2016",
		"costume_8": "Flying",
		"form_598":  "Normal",
		"form_680":  "Winter 2023",
	}))
	bundle.LinkFallbacks()
	gd := &gamedata.GameData{
		Costumes: map[int]gamedata.CostumeInfo{
			1: {ID: 1, Name: "Holiday 2016"},
			8: {ID: 8, Name: "Flying"},
		},
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 25, Form: 598}: {PokemonID: 25},
			{ID: 25, Form: 680}: {PokemonID: 25},
		},
	}
	ra := tracker.NewRecentActivity()
	ra.RecordCostume(25, 1)   // "Holiday 2016" — sorts after "Flying", proves boost
	ra.RecordForm(25, 680)    // "Winter 2023" — sorts after "Normal", proves boost
	return &bot.BotDeps{Translations: bundle, GameData: gd, Cfg: &config.Config{}, RecentActivity: ra}
}

func trackPokemonSiblingIC(pokemon string) *discordgo.InteractionCreate {
	return &discordgo.InteractionCreate{Interaction: &discordgo.Interaction{
		Type: discordgo.InteractionApplicationCommandAutocomplete,
		Data: discordgo.ApplicationCommandInteractionData{
			Name: "track",
			Options: []*discordgo.ApplicationCommandInteractionDataOption{
				{Name: "pokemon", Type: discordgo.ApplicationCommandOptionString, Value: pokemon},
			},
		},
	}}
}

func TestRouteAutocomplete_TrackCostume_BoostsRecentForPokemon(t *testing.T) {
	d := NewDispatcher(Config{})
	d.bundle = testBundle(t)
	d.cfgRoot = &config.Config{}
	d.deps = costumeFormRouteDeps(t)
	ic := trackPokemonSiblingIC("pikachu")
	out := d.routeAutocomplete("track", "costume", "", "en", ic)
	if len(out) == 0 || out[0].Name != "Holiday 2016" {
		t.Errorf("/track costume empty focused: first=%+v, want Holiday 2016 (recent costume 1 for pikachu)", firstName(out))
	}
}

func TestRouteAutocomplete_TrackForm_BoostsRecentForPokemon(t *testing.T) {
	d := NewDispatcher(Config{})
	d.bundle = testBundle(t)
	d.cfgRoot = &config.Config{}
	d.deps = costumeFormRouteDeps(t)
	ic := trackPokemonSiblingIC("pikachu")
	out := d.routeAutocomplete("track", "form", "", "en", ic)
	if len(out) == 0 || out[0].Name != "Winter 2023" {
		t.Errorf("/track form empty focused: first=%+v, want Winter 2023 (recent form 680 for pikachu)", firstName(out))
	}
}

func TestRouteAutocomplete_TrackCostume_NoPokemonNoBoost(t *testing.T) {
	d := NewDispatcher(Config{})
	d.bundle = testBundle(t)
	d.cfgRoot = &config.Config{}
	d.deps = costumeFormRouteDeps(t)
	// No sibling pokemon → flat alphabetical list ("Flying" first), recency not
	// applied. The recent costume ("Holiday 2016", id 1) must NOT be boosted to
	// the top.
	out := d.routeAutocomplete("track", "costume", "", "en", trackPokemonSiblingIC(""))
	if len(out) == 0 || out[0].Name != "Flying" {
		t.Errorf("/track costume with no pokemon should be flat/alphabetical (Flying first), got first=%+v", firstName(out))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/discordbot/slash/ -run 'TrackCostume|TrackForm' -v`
Expected: FAIL — the costume/form cases don't boost yet (first entry is alphabetical, not the recent one).

- [ ] **Step 3: Update the dispatcher cases**

Replace the existing `form` case:

```go
	case opt == "form" && cmd == "track":
		pokemonValue := siblingOptionString(ic, "pokemon")
		base := autocomplete.Form(context.Background(), d.deps, pokemonValue, focused, userLang)
		if focused == "" && d.deps != nil && d.deps.RecentActivity != nil {
			if pid := autocomplete.ResolvePokemonID(d.deps, pokemonValue); pid > 0 {
				base = autocomplete.PrependRecentForms(base, d.deps, d.deps.RecentActivity.RecentForms(pid), userLang)
			}
		}
		return base
```

Replace the existing `costume` case:

```go
	case opt == "costume" && cmd == "track":
		base := autocomplete.Costume(context.Background(), d.deps, focused, userLang)
		if focused == "" && d.deps != nil && d.deps.RecentActivity != nil {
			if pid := autocomplete.ResolvePokemonID(d.deps, siblingOptionString(ic, "pokemon")); pid > 0 {
				base = autocomplete.PrependRecentCostumes(base, d.deps, d.deps.RecentActivity.RecentCostumes(pid), userLang)
			}
		}
		return base
```

Update the two doc comments above these cases to note the recency boost (the old comment says costume "doesn't cascade from the selected pokemon option" — it now optionally does, for recency only).

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/discordbot/slash/ -run 'TrackCostume|TrackForm' -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
git add processor/internal/discordbot/slash/dispatcher.go processor/internal/discordbot/slash/dispatcher_test.go
git commit -m "feat(slash): boost recent costumes/forms to top of /track pickers"
```

---

### Task 5: `!info` recently-seen forms section

**Files:**
- Modify: `processor/internal/bot/commands/info.go` (add `availableRecentForms` helper + render it)
- Modify: `processor/internal/i18n/locale/en.json` (add `msg.info.recent_forms`)
- Test: `processor/internal/bot/commands/info_recent_forms_test.go` (create)

**Interfaces:**
- Consumes: `ctx.RecentActivity.RecentForms`, `ctx.GameData`, `gamedata.FormTranslationKey`.
- Produces: `func (c *InfoCommand) availableRecentForms(ctx *bot.CommandContext, pokemonID int) []string`.

- [ ] **Step 1: Write the failing test**

Create `processor/internal/bot/commands/info_recent_forms_test.go`, mirroring `info_costume_test.go` exactly (integration-style via `cmd.Run`, using `testCtx`):

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

// infoFormCtx mirrors infoCostumeCtx (info_costume_test.go) but wires a named
// form (680 → "Winter 2023") so !info pikachu can exercise the recent-forms
// section.
func infoFormCtx(t *testing.T) *bot.CommandContext {
	t.Helper()
	ctx, _ := testCtx(t)

	gd := &gamedata.GameData{
		Monsters: map[gamedata.MonsterKey]*gamedata.Monster{
			{ID: 25, Form: 0}:   {PokemonID: 25, FormID: 0},
			{ID: 25, Form: 680}: {PokemonID: 25, FormID: 680},
		},
		Moves: map[int]*gamedata.Move{},
		Types: map[int]*gamedata.TypeInfo{},
		Util:  &gamedata.UtilData{},
	}

	ctx.Translations.AddTranslator(i18n.NewTranslator("en", map[string]string{
		"poke_25":  "Pikachu",
		"form_680": "Winter 2023",
	}))

	ctx.Resolver = bot.NewPokemonResolver(gd, ctx.Translations, []string{"en"}, nil)
	ctx.GameData = gd
	ctx.RecentActivity = tracker.NewRecentActivity()

	return ctx
}

func TestInfo_Pokemon_RecentlySeenForms(t *testing.T) {
	ctx := infoFormCtx(t)
	ctx.RecentActivity.RecordForm(25, 680)

	cmd := &InfoCommand{}
	replies := cmd.Run(ctx, []string{"pikachu"})
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	text := replies[0].Text
	if !strings.Contains(text, "680 — Winter 2023") {
		t.Errorf("expected 'id — name' recent form line, got: %q", text)
	}
	if !strings.Contains(text, "Recently-seen forms") {
		t.Errorf("expected a recently-seen forms header, got: %q", text)
	}
}

func TestInfo_Pokemon_NoRecentForms_SectionOmitted(t *testing.T) {
	ctx := infoFormCtx(t)

	cmd := &InfoCommand{}
	replies := cmd.Run(ctx, []string{"pikachu"})
	if len(replies) == 0 {
		t.Fatal("expected at least one reply, got none")
	}
	if strings.Contains(replies[0].Text, "Recently-seen forms") {
		t.Errorf("expected no recent-forms section when none recorded, got: %q", replies[0].Text)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/bot/commands/ -run 'TestInfo_Pokemon_RecentlySeenForms|TestInfo_Pokemon_NoRecentForms' -v`
Expected: FAIL — `availableRecentForms` undefined / no recent-forms section rendered.

- [ ] **Step 3: Implement the helper and render it**

Add the helper after `availableCostumes` in `info.go`:

```go
// availableRecentForms returns "id — name" display strings for forms recently
// seen on pokemonID (via RecentActivity), sorted by id. Mirrors
// availableCostumes; returns nil when RecentActivity isn't wired or nothing
// has been seen recently.
func (c *InfoCommand) availableRecentForms(ctx *bot.CommandContext, pokemonID int) []string {
	if ctx.RecentActivity == nil {
		return nil
	}
	ids := ctx.RecentActivity.RecentForms(pokemonID)
	if len(ids) == 0 {
		return nil
	}
	sort.Ints(ids)

	tr := ctx.Tr()
	enTr := ctx.Translations.For("en")
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		key := gamedata.FormTranslationKey(id)
		name := tr.T(key)
		if name == key {
			name = enTr.T(key)
		}
		if name == key {
			continue // unresolved form name — skip rather than show "form_N"
		}
		result = append(result, fmt.Sprintf("%d — %s", id, name))
	}
	return result
}
```

Render it in the `!info <pokemon>` body **immediately before** the recently-seen-costumes block (approved order: recent forms → recent costumes → available forms). Insert directly above the existing `costumes := c.availableCostumes(...)` block:

```go
	// Recently-seen forms for tracking (form:<name>)
	recentForms := c.availableRecentForms(ctx, pokemonID)
	if len(recentForms) > 0 {
		sb.WriteByte('\n')
		sb.WriteString(tr.T("msg.info.recent_forms") + "\n")
		for _, f := range recentForms {
			sb.WriteString("  " + f + "\n")
		}
	}
```

- [ ] **Step 4: Add the i18n key**

In `processor/internal/i18n/locale/en.json`, add next to `msg.info.available_costumes` (wording parallels the costume header "Recently-seen costumes:"):

```json
	"msg.info.recent_forms": "Recently-seen forms:",
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/bot/commands/ -run 'TestInfo_Pokemon_RecentlySeenForms|TestInfo_Pokemon_NoRecentForms' -v`
Expected: PASS (2 tests).

- [ ] **Step 6: Commit**

```bash
git add processor/internal/bot/commands/info.go processor/internal/i18n/locale/en.json processor/internal/bot/commands/info_test.go
git commit -m "feat(info): recently-seen forms section in !info"
```

---

### Final: full gate

- [ ] Run the complete pre-commit gate from `processor/`:

```bash
go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...
```

Expected: build/vet clean, all tests pass, `0 issues.` from the linter.
