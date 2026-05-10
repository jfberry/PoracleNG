# Pokemon Changed + Reply Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fire a new `monsterChanged` DTS template when a tracked pokemon's data changes (encounter, species, form, gender), with the prior sighting accessible via `{{original.X}}`. Add a `reply` bit to the clean/edit bitmask so subsequent updates send as Discord/Telegram replies to the previous message. Optional config to make `reply` the default for new pokemon tracking rules.

**Architecture:** The encounter tracker already detects species/form changes (`tracker/encounter.go`). Extend it to surface encounter-fill (non-IV → IV) and gender changes, and propagate the prior state into the render pipeline. The renderer constructs the new template from a `monsterChanged` type with an `original` sub-view bolted onto `LayeredView`. Delivery gains a `ReplyKey` field on `Job` parallel to `EditKey`; `MessageTracker` exposes `LookupReply(key, target)` returning the latest message ID for that key, which the senders inject as `message_reference` (Discord) or `reply_to_message_id` (Telegram). Reply bit = 4 in the existing clean/edit bitmask. Reply and edit are mutually exclusive — validation rejects the combo. Reply + clean works (every send tracked for TTH delete; latest wins for reply target).

**Tech Stack:** Go monorepo (`processor/`). Webhook handler in `cmd/processor/pokemon.go`, encounter tracker in `internal/tracker/`, DTS in `internal/dts/`, delivery in `internal/delivery/`, bot commands in `internal/bot/commands/`, store + migrations in `internal/db/` and `internal/store/`.

---

## Decisions baked in (flag if you'd change any)

1. **Template type ID** = `monsterChanged` (camelCase, matches existing `monster` / `monsterNoIv`). The user said "pokemon_changed" verbally; using `monsterChanged` for codebase consistency. The user-facing name in templates can still be "pokemon changed".
2. **Bitmask layout**: bit 0 = clean, bit 1 = edit, bit 2 = reply. So 4 = reply only, 5 = clean+reply, 6 = edit+reply (REJECTED), 7 = REJECTED.
3. **Edit + reply mutually exclusive**: validation in `track.go` and the API handler rejects the combo with a clear error. Rationale: edit means "update the existing message in place"; reply means "send a new message threaded under the previous". They can't both be the wire effect.
4. **What counts as a change**:
   - Species change (existing behaviour)
   - Form change (existing)
   - Gender change (new)
   - Encountered transition: CP was 0 and is now > 0 (new — this is the most common change)
   - IV refinement after encounter is treated as noise (IVs don't change once encountered)
   - Weather change is silently absorbed (environment, not pokemon)
5. **`original` view scope**: a fixed subset of fields — `pokemonId`, `formId`, `name`, `formName`, `fullName`, `cp`, `iv`, `atk`, `def`, `sta`, `level`, `gender`, `genderName`, `weatherId`, `weatherName`, `encountered` (bool), `weight`, `height`. Implemented as a `map[string]any` injected as a layer in `LayeredView` under key `"original"`. Handlebars recurses into the map naturally for `{{original.X}}` access.
6. **Default reply via config**: `[tracking] default_reply_pokemon = false` (default). When `true`, `!track` calls without explicit `clean/edit/reply` keywords default to `reply` (bitmask = 4). API mutations are unaffected — this only changes what bot commands write.
7. **Reply scope**: pokemon-tracking only for this feature. The bit could apply to any type but we only wire it for pokemon (and the change-fired `monsterChanged` rendering). Other types ignore the bit.
8. **EditKey + ReplyKey for monsterChanged** = the encounter ID. One key value, two semantic uses (edit-this-encounter, reply-to-this-encounter's-latest-message).

---

## File Structure

**New files:**
- `processor/internal/tracker/encounter_change.go` — change-type enum + extended detection (split from encounter.go for readability)
- `processor/internal/dts/original_view.go` — `BuildOriginalView(prior, gameData, translator) map[string]any`

**Modified — encounter / change detection:**
- `processor/internal/tracker/encounter.go` — add Gender to `EncounterState`, extend `Track()` to detect encounter and gender changes, return `EncounterChange.Type`
- `processor/cmd/processor/pokemon.go` — replace the TODO at line 223 with a real handler that builds a `RenderJob` for `monsterChanged`

**Modified — DTS:**
- `processor/internal/dts/layered_view.go` — accept an `original` map and resolve `original` field
- `processor/internal/dts/renderer.go` — `RenderPokemonChanged()` (or a flag on `RenderPokemon`) that injects `original`
- `processor/internal/dts/templates.go` — no change (types are open strings); `monsterChanged` registers organically when a template ships

**Modified — delivery:**
- `processor/internal/delivery/delivery.go` — add `ReplyKey string` field to `Job`
- `processor/internal/delivery/tracker.go` — add `LookupReply(key, target)` returning the latest sent message; on every successful send with a `ReplyKey`, update the latest pointer
- `processor/internal/delivery/queue.go` — before send, if `ReplyKey != ""`, look up latest and stash the message ID on the job for the sender
- `processor/internal/delivery/discord.go` — when reply target is set, inject `"message_reference": {"message_id": "..."}`
- `processor/internal/delivery/telegram.go` — when reply target is set, inject `"reply_to_message_id": <id>`

**Modified — bitmask + commands:**
- `processor/internal/db/clean.go` — add `IsReply(clean int) bool`, bit 2
- `processor/internal/bot/commands/track.go` — accept `reply` keyword, OR into bitmask, validate vs edit
- `processor/internal/bot/commands/raid.go`, `egg.go`, `gym.go`, etc. — same keyword (only pokemon wires reply, but consistent parsing)
- `processor/internal/i18n/locale/en.json` — `arg.reply` = `"reply"`, error message keys
- `processor/internal/api/tracking.go` — accept `reply: true` shorthand if useful (or rely on `clean: 4`)

**Modified — config:**
- `processor/internal/config/config.go` — `Tracking.DefaultReplyPokemon bool`
- `processor/internal/api/config_schema.go` — schema entry for the editor
- `config/config.example.toml` — documented option

**Modified — render pipeline:**
- `processor/cmd/processor/render.go` — `RenderJob` gains `ReplyKey string` and `OriginalEnrichment map[string]any` (or `*tracker.EncounterState`); render dispatches to `RenderPokemonChanged` when set
- `processor/cmd/processor/pokemon.go` — `handlePokemonChange` builds and enqueues the RenderJob

**Tests:**
- `processor/internal/tracker/encounter_test.go` — extend with new change types
- `processor/internal/dts/original_view_test.go` — view shape
- `processor/internal/delivery/tracker_test.go` — LookupReply behaviour
- `processor/internal/delivery/queue_test.go` — reply-key flow, edit-vs-reply mutual exclusion
- `processor/internal/db/clean_test.go` — `IsReply` semantics
- `processor/internal/bot/commands/track_test.go` — `reply` keyword parsing + edit+reply rejection

---

## PR 1 — Extend EncounterState and change detection

**Goal:** EncounterState records gender; `Track()` detects encounter, species, form, and gender changes and tags the change with a typed reason.

### Task 1.1: Add Gender to EncounterState and ChangeType enum

**Files:**
- Modify: `processor/internal/tracker/encounter.go`
- Create: `processor/internal/tracker/encounter_change.go`
- Test: `processor/internal/tracker/encounter_test.go`

- [ ] **Step 1: Write the failing test**

```go
// In encounter_test.go — append after existing tests.
func TestTrackDetectsEncountered(t *testing.T) {
	et := NewEncounterTracker()
	t.Cleanup(func() { /* eviction goroutine — leak is OK for the test process */ })

	first := EncounterState{PokemonID: 1, Form: 0, CP: 0}
	if isNew, change := et.Track("enc-1", first); !isNew || change != nil {
		t.Fatalf("first sighting: isNew=%v change=%v", isNew, change)
	}

	encountered := EncounterState{PokemonID: 1, Form: 0, CP: 1500, ATK: 15, DEF: 14, STA: 13}
	isNew, change := et.Track("enc-1", encountered)
	if isNew {
		t.Fatal("expected isNew=false on update")
	}
	if change == nil {
		t.Fatal("expected change for non-IV → IV transition")
	}
	if change.Type != ChangeEncountered {
		t.Errorf("change.Type = %v, want ChangeEncountered", change.Type)
	}
	if change.Old.CP != 0 || change.New.CP != 1500 {
		t.Errorf("CP not propagated: old=%d new=%d", change.Old.CP, change.New.CP)
	}
}

func TestTrackDetectsGenderChange(t *testing.T) {
	et := NewEncounterTracker()
	et.Track("enc-2", EncounterState{PokemonID: 25, Gender: 1})
	_, change := et.Track("enc-2", EncounterState{PokemonID: 25, Gender: 2})
	if change == nil || change.Type != ChangeGender {
		t.Fatalf("expected ChangeGender, got %v", change)
	}
}

func TestTrackIVNoiseIgnored(t *testing.T) {
	et := NewEncounterTracker()
	et.Track("enc-3", EncounterState{PokemonID: 25, CP: 1000, ATK: 10})
	_, change := et.Track("enc-3", EncounterState{PokemonID: 25, CP: 1000, ATK: 11})
	if change != nil {
		t.Fatalf("post-encounter IV change should not fire, got %+v", change)
	}
}
```

- [ ] **Step 2: Run tests, expect failure**

```
go test -count=1 -run "TestTrackDetects|TestTrackIVNoise" ./internal/tracker/
```

Expected: undefined `Gender`, `ChangeType`, `ChangeEncountered`, `ChangeGender`.

- [ ] **Step 3: Add Gender to EncounterState**

In `encounter.go`, extend struct:

```go
type EncounterState struct {
	PokemonID     int
	Form          int
	Gender        int
	Weather       int
	CP            int
	ATK           int
	DEF           int
	STA           int
	DisappearTime int64
	InsertedAt    int64
}
```

- [ ] **Step 4: Create encounter_change.go**

```go
package tracker

// ChangeType describes which dimension of an encounter changed.
type ChangeType int

const (
	ChangeNone ChangeType = iota
	ChangeSpecies
	ChangeForm
	ChangeGender
	ChangeEncountered // non-IV sighting filled in with CP/IVs
)

func (c ChangeType) String() string {
	switch c {
	case ChangeSpecies:
		return "species"
	case ChangeForm:
		return "form"
	case ChangeGender:
		return "gender"
	case ChangeEncountered:
		return "encountered"
	}
	return "none"
}
```

- [ ] **Step 5: Add Type field to EncounterChange**

In `encounter.go`:

```go
type EncounterChange struct {
	EncounterID string
	Type        ChangeType
	Old         EncounterState
	New         EncounterState
}
```

- [ ] **Step 6: Extend Track() to set Type**

Replace the existing `changed := ...` block:

```go
var changeType ChangeType
switch {
case old.PokemonID != newState.PokemonID:
	changeType = ChangeSpecies
case old.Form != newState.Form:
	changeType = ChangeForm
case old.Gender != newState.Gender && old.Gender != 0 && newState.Gender != 0:
	changeType = ChangeGender
case old.CP == 0 && newState.CP > 0:
	changeType = ChangeEncountered
}

if changeType != ChangeNone {
	change := &EncounterChange{
		EncounterID: encounterID,
		Type:        changeType,
		Old:         *old,
		New:         newState,
	}
	cp := newState
	cp.InsertedAt = old.InsertedAt
	et.entries[encounterID] = &cp
	return false, change
}

*old = newState
return false, nil
```

Note: gender change only fires when both old and new are non-zero — initial gender resolution doesn't count as a change.

- [ ] **Step 7: Run tests, expect pass**

```
go test -count=1 -run "TestTrack" ./internal/tracker/
```

- [ ] **Step 8: Commit**

```
git add processor/internal/tracker/
git commit -m "tracker: detect encountered + gender changes, tag change type"
```

### Task 1.2: Wire Gender into pokemon.go's encounter state build

**Files:**
- Modify: `processor/cmd/processor/pokemon.go` (around line 79 where `encounterState` is built)

- [ ] **Step 1: Read pokemon.go around the EncounterState construction**

```
grep -n "EncounterState\|encounterState" processor/cmd/processor/pokemon.go
```

- [ ] **Step 2: Add Gender field**

Locate the `encounterState := tracker.EncounterState{...}` literal and add `Gender: pokemon.Gender,` (the field name on `webhook.Pokemon` may be `Gender int`; verify in `processor/internal/webhook/types.go`).

- [ ] **Step 3: Build, vet, run pokemon tests**

```
go build ./... && go test -count=1 ./cmd/processor/... ./internal/tracker/...
```

- [ ] **Step 4: Commit**

```
git add processor/cmd/processor/pokemon.go
git commit -m "processor: pass gender into encounter tracker"
```

---

## PR 2 — `original` view in DTS + monsterChanged template path

**Goal:** Renderer can produce a `monsterChanged` template render with `{{original.X}}` accessible. Original-view fields are computed from the prior `EncounterState`.

### Task 2.1: Build the original-view map

**Files:**
- Create: `processor/internal/dts/original_view.go`
- Test: `processor/internal/dts/original_view_test.go`

- [ ] **Step 1: Write failing tests**

```go
package dts

import (
	"testing"

	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

func TestBuildOriginalView_NotEncountered(t *testing.T) {
	gd := loadTestGameData(t) // existing helper if available; else inline a stub
	prior := tracker.EncounterState{PokemonID: 25, Form: 0, CP: 0, Gender: 1}
	v := BuildOriginalView(prior, gd, nil) // nil translator → English fallback
	if v["pokemonId"] != 25 {
		t.Errorf("pokemonId = %v, want 25", v["pokemonId"])
	}
	if v["encountered"] != false {
		t.Errorf("encountered = %v, want false", v["encountered"])
	}
	if v["cp"] != 0 {
		t.Errorf("cp = %v, want 0", v["cp"])
	}
}

func TestBuildOriginalView_Encountered(t *testing.T) {
	gd := loadTestGameData(t)
	prior := tracker.EncounterState{
		PokemonID: 25, Form: 0, CP: 1500,
		ATK: 15, DEF: 14, STA: 13, Gender: 1,
	}
	v := BuildOriginalView(prior, gd, nil)
	if v["encountered"] != true {
		t.Error("encountered should be true once CP > 0")
	}
	wantIV := (15 + 14 + 13) * 100.0 / 45.0
	if v["iv"].(float64) != wantIV {
		t.Errorf("iv = %v, want %v", v["iv"], wantIV)
	}
}
```

If `loadTestGameData` doesn't exist, add a tiny helper inline that returns a `*gamedata.GameData` with at least monster ID 25 mapped to a name.

- [ ] **Step 2: Run tests, expect undefined**

```
go test -count=1 -run TestBuildOriginalView ./internal/dts/
```

- [ ] **Step 3: Implement BuildOriginalView**

```go
package dts

import (
	"github.com/pokemon/poracleng/processor/internal/gamedata"
	"github.com/pokemon/poracleng/processor/internal/i18n"
	"github.com/pokemon/poracleng/processor/internal/tracker"
)

// BuildOriginalView returns the field map exposed under {{original.X}}
// in monsterChanged templates. Mirrors the pokemon view but only carries
// fields a "what changed" template typically needs.
func BuildOriginalView(prior tracker.EncounterState, gd *gamedata.GameData, tr *i18n.Translator) map[string]any {
	encountered := prior.CP > 0
	out := map[string]any{
		"pokemonId":   prior.PokemonID,
		"formId":      prior.Form,
		"gender":      prior.Gender,
		"weatherId":   prior.Weather,
		"cp":          prior.CP,
		"atk":         prior.ATK,
		"def":         prior.DEF,
		"sta":         prior.STA,
		"encountered": encountered,
	}
	if encountered {
		out["iv"] = float64(prior.ATK+prior.DEF+prior.STA) * 100.0 / 45.0
	} else {
		out["iv"] = 0.0
	}

	if gd != nil {
		mon := gd.GetMonster(prior.PokemonID, prior.Form)
		out["name"] = monsterDisplayName(mon, tr)
		out["formName"] = formDisplayName(prior.Form, tr)
		out["fullName"] = monsterFullName(mon, prior.Form, tr)
	}
	return out
}
```

`monsterDisplayName`, `formDisplayName`, `monsterFullName` may already exist as helpers — if so, reuse; otherwise add tiny wrappers next to `BuildOriginalView`.

- [ ] **Step 4: Run tests, expect pass**

```
go test -count=1 -run TestBuildOriginalView ./internal/dts/
```

- [ ] **Step 5: Commit**

```
git add processor/internal/dts/original_view.go processor/internal/dts/original_view_test.go
git commit -m "dts: original view builder for monsterChanged templates"
```

### Task 2.2: LayeredView accepts and resolves the `original` map

**Files:**
- Modify: `processor/internal/dts/layered_view.go`
- Test: `processor/internal/dts/layered_view_test.go` (existing or new)

- [ ] **Step 1: Write failing test**

```go
func TestLayeredViewOriginal(t *testing.T) {
	v := LayeredView{
		base:     map[string]any{"name": "Sinistea"},
		original: map[string]any{"name": "Polteageist", "cp": 1500},
	}
	if v.GetField("name") != "Sinistea" {
		t.Error("base name should win")
	}
	orig := v.GetField("original")
	m, ok := orig.(map[string]any)
	if !ok {
		t.Fatalf("original = %T, want map", orig)
	}
	if m["name"] != "Polteageist" {
		t.Errorf("original.name = %v, want Polteageist", m["name"])
	}
	if m["cp"] != 1500 {
		t.Errorf("original.cp = %v, want 1500", m["cp"])
	}
}
```

- [ ] **Step 2: Run, expect undefined `original` field**

```
go test -count=1 -run TestLayeredViewOriginal ./internal/dts/
```

- [ ] **Step 3: Add `original` map to LayeredView**

In `layered_view.go`:

```go
type LayeredView struct {
	base, perLang, perUser, emoji, computed, webhook, dtsDict map[string]any
	original                                                  map[string]any // nil for non-change renders
}
```

In `GetField`, before the existing layer cascade:

```go
if name == "original" && v.original != nil {
	return v.original
}
```

- [ ] **Step 4: Run test, expect pass**

```
go test -count=1 -run TestLayeredViewOriginal ./internal/dts/
```

- [ ] **Step 5: Commit**

```
git add processor/internal/dts/layered_view.go processor/internal/dts/layered_view_test.go
git commit -m "dts: layered view exposes original.* sub-resolver"
```

### Task 2.3: RenderPokemonChanged plumbs original through the renderer

**Files:**
- Modify: `processor/internal/dts/renderer.go`

- [ ] **Step 1: Inspect existing RenderPokemon signature**

```
grep -n "func.*RenderPokemon\b" processor/internal/dts/renderer.go
```

Note the parameters; confirm where `LayeredView` is constructed (`renderForUsers` per the survey).

- [ ] **Step 2: Add RenderPokemonChanged**

A thin wrapper that takes the same args as `RenderPokemon` plus a `original map[string]any`, threads `original` through to `renderForUsers`, and selects template type `monsterChanged` instead of `monster`/`monsterNoIv`.

```go
func (r *Renderer) RenderPokemonChanged(
	enrichment, perLang, perUser, webhookFields map[string]any,
	original map[string]any,
	matched []webhook.MatchedUser,
	tile *staticmap.TilePending,
	editKeyBase string,
) []DeliveryJob {
	return r.renderForUsers(
		"monsterChanged", // template type
		enrichment, perLang, perUser, webhookFields,
		original, // new param
		matched, tile, editKeyBase,
	)
}
```

Update `renderForUsers` to accept and pass `original` through to `LayeredView{ original: original, ... }`.

- [ ] **Step 3: Add a smoke test that renders a monsterChanged template**

If a test fixture exists for `RenderPokemon`, fork it with a monsterChanged template that references `{{original.cp}}`. Otherwise put a TODO note in the PR description and skip — the integration coverage in PR 6 will exercise this path.

- [ ] **Step 4: Run dts tests**

```
go test -count=1 ./internal/dts/...
```

- [ ] **Step 5: Commit**

```
git add processor/internal/dts/renderer.go
git commit -m "dts: RenderPokemonChanged threads original into LayeredView"
```

---

## PR 3 — ReplyKey field and MessageTracker.LookupReply

**Goal:** Delivery infrastructure can carry a reply key and look up the latest sent message for that key. No senders use it yet.

### Task 3.1: Add ReplyKey to Job and TrackedMessage

**Files:**
- Modify: `processor/internal/delivery/delivery.go`
- Modify: `processor/internal/delivery/tracker.go`
- Test: `processor/internal/delivery/tracker_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestLookupReplyReturnsLatest(t *testing.T) {
	tr := newTrackerForTest(t)
	tr.Track("rk1:targetA", &TrackedMessage{
		SentID: "msg-1", Target: "targetA", Type: "discord:user",
		ReplyKey: "rk1",
	}, time.Hour)
	tr.Track("rk1:targetA", &TrackedMessage{
		SentID: "msg-2", Target: "targetA", Type: "discord:user",
		ReplyKey: "rk1",
	}, time.Hour)
	got := tr.LookupReply("rk1", "targetA")
	if got == nil || got.SentID != "msg-2" {
		t.Fatalf("LookupReply = %+v, want SentID=msg-2", got)
	}
	// Different target should not match.
	if tr.LookupReply("rk1", "targetB") != nil {
		t.Error("LookupReply matched wrong target")
	}
}
```

`newTrackerForTest` likely exists; if not, instantiate `MessageTracker` with no senders.

- [ ] **Step 2: Run, expect undefined ReplyKey / LookupReply**

- [ ] **Step 3: Add ReplyKey to Job and TrackedMessage**

```go
// delivery.go
type Job struct {
	// ... existing ...
	ReplyKey string // when non-empty, latest message with this key+target is the reply target
}

// tracker.go
type TrackedMessage struct {
	// ... existing ...
	ReplyKey string
}
```

- [ ] **Step 4: Implement LookupReply**

The simplest correct implementation walks the cache. Performance is OK because the tracker is typically O(few thousand) entries.

```go
// LookupReply returns the most recently tracked message with the given
// ReplyKey targeted at target, or nil if none. Walks the cache; per-key
// indexing can be added later if profiling shows it matters.
func (mt *MessageTracker) LookupReply(replyKey, target string) *TrackedMessage {
	if replyKey == "" {
		return nil
	}
	var latest *TrackedMessage
	var latestAt time.Time
	mt.cache.Range(func(_ string, item *ttlcache.Item[*TrackedMessage]) bool {
		v := item.Value()
		if v == nil || v.ReplyKey != replyKey || v.Target != target {
			return true
		}
		// item.ExpiresAt() − TTL gives roughly when it was inserted.
		// Track InsertedAt explicitly on TrackedMessage instead.
		if latest == nil || v.InsertedAt.After(latestAt) {
			latest = v
			latestAt = v.InsertedAt
		}
		return true
	})
	return latest
}
```

Add `InsertedAt time.Time` to `TrackedMessage` and set it in `Track()`. (If the existing struct already has a timestamp, reuse it.)

- [ ] **Step 5: Run test, expect pass**

```
go test -count=1 -run TestLookupReply ./internal/delivery/
```

- [ ] **Step 6: Commit**

```
git add processor/internal/delivery/delivery.go processor/internal/delivery/tracker.go processor/internal/delivery/tracker_test.go
git commit -m "delivery: ReplyKey field + MessageTracker.LookupReply"
```

### Task 3.2: Tracker save/load preserves ReplyKey + InsertedAt

**Files:**
- Modify: `processor/internal/delivery/tracker.go` (Save/Load JSON serialization)
- Test: `processor/internal/delivery/tracker_test.go`

- [ ] **Step 1: Add a round-trip test**

```go
func TestTrackerSaveLoadPreservesReplyKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tracker.json")
	tr1 := newTrackerForTest(t)
	tr1.persistPath = path
	tr1.Track("k", &TrackedMessage{SentID: "m1", Target: "u1", ReplyKey: "rk1"}, time.Hour)
	if err := tr1.Save(); err != nil {
		t.Fatal(err)
	}
	tr2 := newTrackerForTest(t)
	tr2.persistPath = path
	if err := tr2.Load(); err != nil {
		t.Fatal(err)
	}
	got := tr2.LookupReply("rk1", "u1")
	if got == nil || got.SentID != "m1" {
		t.Fatalf("LookupReply after load = %+v", got)
	}
}
```

- [ ] **Step 2: Run, expect failure (field missing from serialized form)**

- [ ] **Step 3: Add ReplyKey + InsertedAt to the persisted struct**

If serialization uses an explicit DTO, add the fields there. Otherwise the json tags on `TrackedMessage` already cover it.

- [ ] **Step 4: Run, expect pass**

- [ ] **Step 5: Commit**

```
git commit -am "delivery: persist ReplyKey + InsertedAt across restarts"
```

---

## PR 4 — Senders inject reply target

**Goal:** When a Job carries a `ReplyKey` and the tracker has a matching prior message, the Discord/Telegram payload references it (`message_reference` / `reply_to_message_id`). The FairQueue does the lookup and stamps the message ID onto the job before send.

### Task 4.1: FairQueue resolves ReplyKey to a concrete message ID

**Files:**
- Modify: `processor/internal/delivery/queue.go`
- Modify: `processor/internal/delivery/delivery.go` (add `ReplyToID string` ephemeral field on Job, set by queue, read by senders)
- Test: `processor/internal/delivery/queue_test.go`

- [ ] **Step 1: Write failing test**

A queue test that schedules a Job with `ReplyKey = "k1"`, pre-populates the tracker with a message under that key, and asserts the sender receives a Job with `ReplyToID` set.

- [ ] **Step 2: Run, expect failure**

- [ ] **Step 3: Implement the lookup**

In `FairQueue.processJob` (or wherever a job is handed to the sender):

```go
if job.ReplyKey != "" && job.ReplyToID == "" {
	if prior := fq.tracker.LookupReply(job.ReplyKey, job.Target); prior != nil {
		job.ReplyToID = prior.SentID
	}
}
```

(`SentID` for Discord is `channelID:messageID`; for Telegram it's `chatID:messageID`. Senders will split as needed.)

- [ ] **Step 4: Run, expect pass**

- [ ] **Step 5: Commit**

```
git commit -am "delivery: queue stamps ReplyToID on jobs with a known prior"
```

### Task 4.2: Discord sender injects `message_reference`

**Files:**
- Modify: `processor/internal/delivery/discord.go`

- [ ] **Step 1: Add a unit test that verifies the JSON body includes `message_reference` when ReplyToID is set**

Mock the HTTP transport (`httptest.Server`) and assert the captured body's JSON contains `"message_reference": {"message_id": "..."}`.

- [ ] **Step 2: Run, expect failure**

- [ ] **Step 3: Implement injection**

Where the JSON message body is built (around `postMessage`), if `job.ReplyToID != ""`:

```go
// Split "channelID:messageID" → use the message ID half.
msgID := splitSentID(job.ReplyToID)
if msgID != "" {
	body["message_reference"] = map[string]any{
		"message_id":         msgID,
		"fail_if_not_exists": false, // gracefully degrade if the prior was deleted
	}
}
```

`fail_if_not_exists: false` means Discord sends the message as a regular post if the parent has been deleted, rather than 400'ing.

- [ ] **Step 4: Run, expect pass**

- [ ] **Step 5: Commit**

```
git commit -am "delivery/discord: send as reply when prior message known"
```

### Task 4.3: Telegram sender injects `reply_to_message_id`

**Files:**
- Modify: `processor/internal/delivery/telegram.go`

- [ ] **Step 1: Test the same way as Discord**

- [ ] **Step 2: Run, expect failure**

- [ ] **Step 3: Implement injection**

In `sendMessage` and any other body-building helper that should support reply (text, photo, sticker — venue/location replies are unusual; skip those):

```go
if msgID := splitSentID(job.ReplyToID); msgID != "" {
	body["reply_to_message_id"] = msgID
	body["allow_sending_without_reply"] = true // graceful degrade
}
```

- [ ] **Step 4: Run, expect pass**

- [ ] **Step 5: Commit**

```
git commit -am "delivery/telegram: send as reply when prior message known"
```

### Task 4.4: Track the sent message under both EditKey and ReplyKey

**Files:**
- Modify: `processor/internal/delivery/queue.go` (post-send tracking)

- [ ] **Step 1: Verify what's currently stored on success**

The existing flow stores the sent message under `EditKey` if set. For `monsterChanged` sends we want the message stored under `ReplyKey` too so the next change can find it.

- [ ] **Step 2: Add ReplyKey storage**

After a successful send, if `job.ReplyKey != ""`:

```go
fq.tracker.Track(replyTrackerKey(job.ReplyKey, job.Target), &TrackedMessage{
	SentID:     sentID,
	Target:     job.Target,
	Type:       job.Type,
	Clean:      job.Clean,
	ReplyKey:   job.ReplyKey,
	InsertedAt: time.Now(),
}, ttl)
```

The map key (`replyTrackerKey`) needs to be unique per (key, target) — e.g. `"reply:" + replyKey + ":" + target`. Use the same TTL the EditKey path uses.

- [ ] **Step 3: Test**

Add an integration-style test: schedule a job with `ReplyKey`, then schedule another job with the same `ReplyKey`, and verify the second job gets the first's message ID stamped on `ReplyToID`.

- [ ] **Step 4: Commit**

```
git commit -am "delivery: track sent messages under ReplyKey for chain replies"
```

---

## PR 5 — Reply bit on the bitmask, bot keyword, validation

**Goal:** `clean = 4` is "reply mode". Bot accepts `reply` keyword. Edit + reply combo is rejected at the command and API layers.

### Task 5.1: clean.go gains IsReply

**Files:**
- Modify: `processor/internal/db/clean.go`
- Test: `processor/internal/db/clean_test.go`

- [ ] **Step 1: Test**

```go
func TestIsReply(t *testing.T) {
	cases := []struct {
		clean        int
		clean_, edit bool
		reply        bool
	}{
		{0, false, false, false},
		{1, true, false, false},
		{2, false, true, false},
		{3, true, true, false},
		{4, false, false, true},
		{5, true, false, true},
	}
	for _, c := range cases {
		if IsClean(c.clean) != c.clean_ || IsEdit(c.clean) != c.edit || IsReply(c.clean) != c.reply {
			t.Errorf("clean=%d: %v %v %v want %v %v %v",
				c.clean, IsClean(c.clean), IsEdit(c.clean), IsReply(c.clean),
				c.clean_, c.edit, c.reply)
		}
	}
}
```

- [ ] **Step 2: Run, expect undefined**

- [ ] **Step 3: Add IsReply**

```go
const (
	cleanBitClean = 1
	cleanBitEdit  = 2
	cleanBitReply = 4
)

func IsReply(clean int) bool { return clean&cleanBitReply != 0 }
```

(Refactor the existing IsClean/IsEdit to use the constants; pure cleanup.)

- [ ] **Step 4: Test passes**

- [ ] **Step 5: Commit**

```
git commit -am "db: IsReply bit in clean bitmask"
```

### Task 5.2: track.go accepts `reply` keyword and validates

**Files:**
- Modify: `processor/internal/bot/commands/track.go`
- Modify: `processor/internal/i18n/locale/en.json` (add `arg.reply`, `msg.track.edit_reply_conflict`)
- Test: `processor/internal/bot/commands/track_test.go` (existing or new)

- [ ] **Step 1: Failing test**

Test parses `!track pikachu reply` and expects `clean & 4 != 0`. Test parses `!track pikachu edit reply` and expects an error reply.

- [ ] **Step 2: Run, expect parse failure / no reply support**

- [ ] **Step 3: Add the keyword and validation**

In `trackParams`:

```go
{Type: bot.ParamKeyword, Key: "arg.reply"},
```

In the filter-parse block where `arg.clean` / `arg.edit` are handled:

```go
if parsed.HasKeyword("arg.reply") {
	f.clean |= 4
}
if db.IsEdit(f.clean) && db.IsReply(f.clean) {
	return []bot.Reply{{React: "🙅", Text: tr.T("msg.track.edit_reply_conflict")}}
}
```

en.json:

```json
"arg.reply": "reply",
"msg.track.edit_reply_conflict": "`edit` and `reply` are mutually exclusive — pick one.",
```

(Add the `arg.reply` key to every locale file with a value of `"reply"` — translators can localise later. The `msg.track.edit_reply_conflict` string only needs en; other locales fall back.)

- [ ] **Step 4: Run, expect pass**

- [ ] **Step 5: Commit**

```
git add processor/internal/bot/commands/track.go processor/internal/i18n/locale/
git commit -m "bot: !track accepts reply keyword; reject edit+reply combo"
```

### Task 5.3: Add `reply` to other tracking commands' allowed keywords

**Files:**
- Modify: `processor/internal/bot/commands/raid.go`, `egg.go`, `gym.go`, `quest.go`, `nest.go`, `lure.go`, `fort.go`, `invasion.go`, `maxbattle.go`

- [ ] **Step 1: Decide scope**

Reply is only wired for pokemon (`monsterChanged`). Other types accept the keyword but it's effectively a no-op (no change events fire `monsterChanged`-style for them). Keep parsing consistent so `!raid level:5 reply` doesn't fail on unrecognised arg.

- [ ] **Step 2: Add the keyword param to each command's `paramsX()` helper**

```go
{Type: bot.ParamKeyword, Key: "arg.reply"},
```

And the OR-into-bitmask block. No edit+reply validation here unless explicitly desired (skip for now — the validation only matters where reply is meaningful).

- [ ] **Step 3: Test parses don't choke**

```
go test -count=1 ./internal/bot/commands/...
```

- [ ] **Step 4: Commit**

```
git commit -am "bot: accept reply keyword on all tracking commands (no-op outside pokemon)"
```

### Task 5.4: API tracking handler accepts `clean = 4..7` and validates

**Files:**
- Modify: `processor/internal/api/tracking.go` (or wherever monster tracking POST is handled)

- [ ] **Step 1: Test**

POST a tracking row with `clean: 6` (edit+reply), expect 400. POST `clean: 5` (clean+reply), expect 200.

- [ ] **Step 2: Implement validation**

In the handler that accepts monster tracking creates/updates:

```go
if db.IsEdit(row.Clean) && db.IsReply(row.Clean) {
	c.AbortWithStatusJSON(400, gin.H{"error": "clean: edit (2) and reply (4) cannot be combined"})
	return
}
```

- [ ] **Step 3: Run, expect pass**

- [ ] **Step 4: Commit**

```
git commit -am "api/tracking: reject edit+reply combo"
```

---

## PR 6 — Wire the change handler to fire monsterChanged

**Goal:** `handlePokemonChange` actually enqueues a render job. The matched users are the same set that originally tracked the encounter. Each user receives a `monsterChanged` render with `original.X` populated.

### Task 6.1: RenderJob carries OriginalView and ChangeType

**Files:**
- Modify: `processor/cmd/processor/render.go`
- Modify: `processor/internal/dts/renderer.go` (if `RenderPokemonChanged` was already added in PR 2 this might be a no-op)

- [ ] **Step 1: Add fields to RenderJob**

```go
type RenderJob struct {
	// ... existing ...
	IsChange       bool                  // dispatch to RenderPokemonChanged
	OriginalView   map[string]any        // the {{original.X}} bag
	ChangeType     string                // for analytics / template branching
	ReplyKey       string
}
```

- [ ] **Step 2: Render dispatch**

In the render worker (`processRenderJob`), when `j.IsChange` is true, call `RenderPokemonChanged` instead of `RenderPokemon`. Threading `j.OriginalView` and `j.ReplyKey` into the call site.

- [ ] **Step 3: Build, run existing tests**

```
go build ./... && go test -count=1 ./cmd/processor/... ./internal/dts/...
```

- [ ] **Step 4: Commit**

```
git commit -am "render: RenderJob carries OriginalView + ReplyKey + IsChange"
```

### Task 6.2: handlePokemonChange enqueues the job

**Files:**
- Modify: `processor/cmd/processor/pokemon.go` (replace TODO at line 223)

- [ ] **Step 1: Reuse the matching pipeline for the changed pokemon**

The new state's matched-users list is what we need. Re-run the same matcher path the initial sighting takes:

```go
func (ps *ProcessorService) handlePokemonChange(l *log.Entry, raw json.RawMessage, change *tracker.EncounterChange, st *state.State) {
	// Build a fresh MatchData for the new state. Re-use the same code
	// path as the normal pokemon handler from line 100 onward.
	matched := matching.MatchPokemon(... newState ..., st)
	matched = ps.filterBlocked(matched)
	if len(matched) == 0 {
		return
	}

	enr := ps.enrichPokemon(... newState ...)
	original := dts.BuildOriginalView(change.Old, ps.gameData, ps.translator(""))

	// Compute reply/edit key — encounter ID is stable across the chain.
	editKey := pokemon.EncounterID
	replyKey := pokemon.EncounterID

	job := &RenderJob{
		TemplateType:   "monsterChanged",
		IsChange:       true,
		ChangeType:     change.Type.String(),
		EditKey:        editKey,
		ReplyKey:       replyKey,
		OriginalView:   original,
		Enrichment:     enr.Base,
		PerLangEnrichment: enr.PerLang,
		PerUserEnrichment: enr.PerUser,
		WebhookFields:  webhookFields,
		MatchedUsers:   matched,
		IsEncountered:  newState.CP > 0,
		IsPokemon:      true,
	}
	ps.enqueueRender(job)
}
```

The exact field names depend on the existing handler — match them. Refactor the original handler to share the build-job helper if duplication grows.

- [ ] **Step 2: Per-user reply-key targeting**

The render worker constructs one DeliveryJob per user. Each Job gets `ReplyKey = "<encounterID>"`. The MessageTracker `LookupReply(replyKey, target)` already partitions by target, so users don't cross-thread.

- [ ] **Step 3: Smoke test**

Add `processor/cmd/processor/pokemon_change_test.go` — exercise: matcher finds 1 user, `Track()` returns ChangeEncountered, `handlePokemonChange` enqueues 1 RenderJob with IsChange=true and ReplyKey set. Stub the render queue with a channel of size 1.

- [ ] **Step 4: Commit**

```
git commit -am "processor: handlePokemonChange enqueues monsterChanged render job"
```

### Task 6.3: Filter monsterChanged matches by `reply` bit when no template exists

**Files:**
- Modify: `processor/cmd/processor/pokemon.go` (handlePokemonChange) or `processor/internal/dts/renderer.go`

- [ ] **Step 1: Decide policy**

For users whose tracking rule has `clean & 4 == 0` (no reply bit), should we still send the change as a normal alert? This would be very noisy — every encounter fires twice (once at non-IV, once at IV). Better: only fire `monsterChanged` for users who opted into reply (or for whom an admin has set `default_reply_pokemon`).

- [ ] **Step 2: Filter in handlePokemonChange**

Before enqueuing, drop matched users where `MatchedUser.Clean & 4 == 0`:

```go
filtered := make([]webhook.MatchedUser, 0, len(matched))
for _, m := range matched {
	if db.IsReply(m.Clean) {
		filtered = append(filtered, m)
	}
}
if len(filtered) == 0 {
	return
}
matched = filtered
```

This gates: only users who asked for replies get the second-stage message.

- [ ] **Step 3: Test the gate**

- [ ] **Step 4: Commit**

```
git commit -am "processor: only fire monsterChanged for users with reply bit set"
```

---

## PR 7 — Default reply config

**Goal:** Admin can flip a switch so new pokemon tracking rules created via bot commands default to `reply` mode.

### Task 7.1: Config field

**Files:**
- Modify: `processor/internal/config/config.go`
- Modify: `processor/internal/api/config_schema.go`
- Modify: `config/config.example.toml`

- [ ] **Step 1: Add field to Tracking struct**

```go
type TrackingConfig struct {
	// ... existing ...
	DefaultReplyPokemon bool `toml:"default_reply_pokemon"`
}
```

- [ ] **Step 2: Schema entry**

In `config_schema.go`, in the tracking section, add:

```go
{Path: "tracking.default_reply_pokemon", Type: "bool", Default: false,
	Description: "Default new !track rules to reply-mode (clean bit 4) so updates thread under the original alert"},
```

- [ ] **Step 3: Document in example.toml**

```toml
[tracking]
# default_reply_pokemon = false
# When true, !track creates new rules with clean=4 (reply) by default.
# Users who want clean instead can explicitly type !track pikachu clean.
```

- [ ] **Step 4: Commit**

```
git commit -am "config: tracking.default_reply_pokemon"
```

### Task 7.2: track.go applies the default

**Files:**
- Modify: `processor/internal/bot/commands/track.go`

- [ ] **Step 1: Apply default after parsing keywords**

In the bitmask-build block:

```go
if !parsed.HasKeyword("arg.clean") && !parsed.HasKeyword("arg.edit") && !parsed.HasKeyword("arg.reply") {
	if ctx.Config.Tracking.DefaultReplyPokemon {
		f.clean |= 4
	}
}
```

This only kicks in when the user gave no explicit keyword — explicit `clean` or `edit` overrides the default.

- [ ] **Step 2: Test default-on and default-off paths**

- [ ] **Step 3: Commit**

```
git commit -am "bot: !track applies default_reply_pokemon when no keyword set"
```

---

## PR 8 — Docs + integration smoke + manual checklist

**Goal:** Admin-facing documentation, an end-to-end integration test, and a checklist for verifying in a live Discord/Telegram channel.

### Task 8.1: API.md / template docs

**Files:**
- Modify: `API.md` (clean bitmask doc)
- Modify: project README or a dedicated docs page if one exists for templates

- [ ] **Step 1: Document the bitmask change**

Update the clean field reference: 1 = clean, 2 = edit, 4 = reply, with a note that 6/7 are invalid. Mention `monsterChanged` template type and `{{original.X}}` access.

- [ ] **Step 2: Commit**

```
git commit -am "docs: clean=4 (reply), monsterChanged template type, original.* fields"
```

### Task 8.2: Integration smoke test

**Files:**
- Test: `processor/cmd/processor/integration_pokemon_change_test.go` (new)

- [ ] **Step 1: Write the test**

Stand up a fake `Sender` that records jobs. Drive a non-IV pokemon webhook → assert one job sent with template `monster`/`monsterNoIv`. Drive the same encounter with IVs filled → assert a second job sent with template `monsterChanged`, `ReplyKey == encounterID`, and the second job's body contains a reference to the first sent message ID.

- [ ] **Step 2: Run, expect pass after PR 6 lands**

- [ ] **Step 3: Commit**

```
git commit -am "test: end-to-end pokemon-change reply smoke"
```

### Task 8.3: Manual verification checklist

**Files:**
- Modify: this plan file (append a checklist)

- [ ] **Step 1: Append**

```markdown
## Manual verification (before merging)

- [ ] Telegram: !track pikachu reply → see two messages in DM, second is a reply to first
- [ ] Discord: same as above; `message_reference` arrow visible
- [ ] Telegram: !track pikachu reply clean → both messages delete on TTH
- [ ] Discord: !track pikachu edit reply → bot rejects with "mutually exclusive"
- [ ] !info form list, !area, !profile, !poracle-clean still work (no regression from helper change in PR 2/3 wiring)
- [ ] default_reply_pokemon = true: !track pikachu (no keyword) creates row with clean=4
- [ ] default_reply_pokemon = true: !track pikachu clean (explicit) creates row with clean=1 (default suppressed)
- [ ] monsterChanged template can render `{{original.cp}}`, `{{original.fullName}}`, `{{original.encountered}}` with prior values
- [ ] Restart processor mid-encounter: ReplyKey lookup still works after Tracker.Load (PR 3.2)
```

- [ ] **Step 2: Commit**

```
git commit -am "plan: manual verification checklist"
```

---

## Self-Review Notes

Coverage check against the spec:
- ✅ `monsterChanged` DTS template type (PR 2)
- ✅ `{{original.X}}` access (PR 2.1, 2.2)
- ✅ Reply bit in bitmask (PR 5.1)
- ✅ Reply via Discord + Telegram (PR 4)
- ✅ Reply works through non-IV → IV → form/gender chain (PR 6 — change types covered in PR 1)
- ✅ Reply combines with clean (PR 4.4 — every send tracked under reply key with TTL; clean uses its own per-message TTL)
- ✅ Reply default config (PR 7)
- ✅ All in a branch (worktree at `../PoracleNG-pokemon-changed`, branch `pokemon-changed-reply`)

Risks / open questions:
- LookupReply walks the full cache. If cache grows beyond ~10k entries this becomes a per-send hot-path cost. Mitigation: add a secondary index `map[replyKey]map[target]*TrackedMessage` if profiling shows it. Not in scope for v1.
- Edit+reply rejection at API layer covers monster tracking; if the schema migration ever loads pre-existing rows with clean=6 from an older client, they pass through undetected. PR 5.4 should also add a one-shot scan at startup that logs (not deletes) any such rows.
- `BuildOriginalView` constructs the original view from `EncounterState` only — it doesn't include geocoding/maps for the prior position (the position doesn't change between sightings of the same encounter). If a future change tracks position drift, the view builder needs extending.
- The `arg.reply` keyword is added to non-pokemon commands as a parse-only no-op so users don't get "unrecognized arg" errors. If a tracking type later wants reply semantics (raids on RSVP changes are an obvious candidate), the wire-up is one more pair of code paths.

## Execution Handoff

Plan saved. Two execution paths:

1. **Subagent-Driven Development** (recommended) — fresh subagent per task, two-stage review per task, fast iteration.
2. **Inline Execution** — execute in this session via superpowers:executing-plans, batch checkpoints.

Pick one when you're ready to start.
