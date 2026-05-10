# Pokemon Changed + Reply Threading Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fire a new `monsterChanged` DTS template when a tracked pokemon's data changes (encounter, species, form, gender), with the prior sighting accessible via `{{original.X}}`. Subsequent messages for the same encounter (per recipient) thread as Discord/Telegram replies to the prior message — implicit behaviour, not opt-in. Edit mode (existing) still takes priority when set.

**Architecture:** The encounter tracker already detects species/form changes (`tracker/encounter.go`). Extend it to surface encounter-fill (non-IV → IV) and gender changes, and propagate the prior state into the render pipeline. The renderer constructs the new template from a `monsterChanged` type with an `original` sub-view bolted onto `LayeredView`. Delivery gains a `ReplyKey` field on `Job`; `MessageTracker` keeps a dedicated O(1) reply index `map[replyKey + ":" + target] → latestSentID` so the high-volume pokemon path doesn't pay an O(N) cache walk per send. Senders inject `message_reference` (Discord) / `reply_to_message_id` (Telegram) when the lookup hits. No bitmask change — reply is implicit for pokemon updates.

**Tech Stack:** Go monorepo (`processor/`). Webhook handler in `cmd/processor/pokemon.go`, encounter tracker in `internal/tracker/`, DTS in `internal/dts/`, delivery in `internal/delivery/`, bot commands in `internal/bot/commands/`.

---

## Decisions baked in (flag if you'd change any)

1. **Template type ID** = `monsterChanged` (camelCase, matches existing `monster` / `monsterNoIv`). The user said "pokemon_changed" verbally; using `monsterChanged` for codebase consistency. The user-facing name in templates can still be "pokemon changed".
2. **No bitmask change.** Reply isn't a bit, isn't stored on the row, isn't an opt-in. Every `monsterChanged` render carries a `ReplyKey = encounterID`; the sender threads as a reply iff a prior message for `(replyKey, target)` exists. Edit mode (existing bit 2) still wins — when set, the prior message is edited in place rather than replied to.
3. **No `default_reply_pokemon` config.** Always-on for pokemon. Users who want a single message updated in place use `edit` (existing). Users who want clean-on-TTH still use `clean` (existing); clean and reply-threading combine naturally — every message tracked for TTH delete, the latest is the reply target.
4. **What counts as a change**:
   - Species change (existing behaviour)
   - Form change (existing)
   - Gender change (new)
   - Encountered transition: CP was 0 and is now > 0 (new — this is the most common change)
   - IV refinement after encounter is treated as noise (IVs don't change once encountered)
   - Weather change is silently absorbed (environment, not pokemon)
5. **`original` view scope**: a fixed subset of fields — `pokemonId`, `formId`, `name`, `formName`, `fullName`, `cp`, `iv`, `atk`, `def`, `sta`, `level`, `gender`, `genderName`, `weatherId`, `weatherName`, `encountered` (bool), `weight`, `height`. Implemented as a `map[string]any` injected as a layer in `LayeredView` under key `"original"`. Handlebars recurses into the map naturally for `{{original.X}}` access.
6. **No matched-user gate for monsterChanged.** Every user who matched the new state gets the change render; if they were also matched at the prior state, their message threads as a reply to that prior — otherwise it's a fresh message (still as `monsterChanged`, since it's still a "change" event semantically). This avoids silently dropping IV alerts for users whose rules match the encountered state but not the non-IV state.
7. **Reply scope**: pokemon-tracking only for this feature. Other types are not wired (raids on RSVP changes are a follow-up candidate). The infrastructure (ReplyKey field, LookupReply, sender injection) is generic, so wiring more types later is local.
8. **EditKey + ReplyKey for monsterChanged** = the encounter ID. The same string serves both: edit-this-encounter, reply-to-this-encounter's-latest-message.
9. **Reply lookup is O(1).** Pokemon is the highest-volume alert type. The MessageTracker keeps a dedicated `replyIndex *ttlcache.Cache[string, string]` keyed by `replyKey + "\x00" + target` with values = `SentID`. The existing edit/clean cache is unchanged. Both indexes share TTL semantics — when a reply-tracked message is evicted from the edit cache, the matching reply-index entry is evicted too via the eviction callback.

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

**Modified — render pipeline:**
- `processor/cmd/processor/render.go` — `RenderJob` gains `ReplyKey string` and `OriginalEnrichment map[string]any` (or `*tracker.EncounterState`); render dispatches to `RenderPokemonChanged` when set
- `processor/cmd/processor/pokemon.go` — `handlePokemonChange` builds and enqueues the RenderJob

**Tests:**
- `processor/internal/tracker/encounter_test.go` — extend with new change types
- `processor/internal/dts/original_view_test.go` — view shape
- `processor/internal/delivery/tracker_test.go` — LookupReply behaviour, eviction propagation
- `processor/internal/delivery/queue_test.go` — reply-key flow, edit takes priority over reply

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

## PR 3 — ReplyKey field, dedicated O(1) reply index

**Goal:** Delivery infrastructure can carry a reply key and look up the latest sent message for that (key, target) in O(1). No senders use it yet.

### Task 3.1: Add ReplyKey to Job and TrackedMessage

**Files:**
- Modify: `processor/internal/delivery/delivery.go`
- Modify: `processor/internal/delivery/tracker.go`
- Test: `processor/internal/delivery/tracker_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestLookupReplyReturnsLatest(t *testing.T) {
	tr := newTrackerForTest(t)
	tr.Track("edit-1", &TrackedMessage{
		SentID: "msg-1", Target: "targetA", Type: "discord:user",
		ReplyKey: "rk1",
	}, time.Hour)
	tr.Track("edit-2", &TrackedMessage{
		SentID: "msg-2", Target: "targetA", Type: "discord:user",
		ReplyKey: "rk1",
	}, time.Hour)
	if got := tr.LookupReply("rk1", "targetA"); got != "msg-2" {
		t.Fatalf("LookupReply = %q, want msg-2", got)
	}
	// Different target should not match.
	if got := tr.LookupReply("rk1", "targetB"); got != "" {
		t.Errorf("LookupReply cross-target = %q, want empty", got)
	}
	// Different key should not match.
	if got := tr.LookupReply("rk-other", "targetA"); got != "" {
		t.Errorf("LookupReply wrong key = %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run, expect undefined ReplyKey / LookupReply**

- [ ] **Step 3: Add ReplyKey to Job and TrackedMessage**

```go
// delivery.go
type Job struct {
	// ... existing ...
	ReplyKey string // when non-empty, the (ReplyKey, Target) pair indexes the
	                // latest sent message in MessageTracker for reply chaining
}

// tracker.go
type TrackedMessage struct {
	// ... existing ...
	ReplyKey string
}
```

- [ ] **Step 4: Add the dedicated reply index to MessageTracker**

Pokemon is the highest-volume alert type. A per-send O(N) walk would be a real cost. Use a separate ttlcache keyed by `replyKey + "\x00" + target`, value = `SentID`.

```go
// tracker.go
type MessageTracker struct {
	// ... existing fields ...
	cache       *ttlcache.Cache[string, *TrackedMessage] // existing — keyed by edit key
	replyIndex  *ttlcache.Cache[string, string]          // new — keyed by replyKey \x00 target
}

func replyIndexKey(replyKey, target string) string {
	return replyKey + "\x00" + target
}

// LookupReply returns the SentID of the latest message for this reply
// key + target, or "" if none. O(1).
func (mt *MessageTracker) LookupReply(replyKey, target string) string {
	if replyKey == "" {
		return ""
	}
	item := mt.replyIndex.Get(replyIndexKey(replyKey, target))
	if item == nil {
		return ""
	}
	return item.Value()
}
```

In the constructor, instantiate `replyIndex` with the same TTL behaviour as `cache`.

- [ ] **Step 5: Track() also writes the reply index**

When `Track(editKey, msg, ttl)` is called and `msg.ReplyKey != ""`, insert into the reply index too with the same TTL:

```go
func (mt *MessageTracker) Track(editKey string, msg *TrackedMessage, ttl time.Duration) {
	mt.cache.Set(editKey, msg, ttl)
	if msg.ReplyKey != "" {
		mt.replyIndex.Set(replyIndexKey(msg.ReplyKey, msg.Target), msg.SentID, ttl)
	}
}
```

- [ ] **Step 6: Run, expect pass**

```
go test -count=1 -run TestLookupReply ./internal/delivery/
```

- [ ] **Step 7: Commit**

```
git add processor/internal/delivery/delivery.go processor/internal/delivery/tracker.go processor/internal/delivery/tracker_test.go
git commit -m "delivery: ReplyKey field + O(1) reply index in MessageTracker"
```

### Task 3.2: Eviction of the edit-cache entry also evicts the reply index

**Files:**
- Modify: `processor/internal/delivery/tracker.go`
- Test: `processor/internal/delivery/tracker_test.go`

- [ ] **Step 1: Failing test**

```go
func TestEvictionPropagatesToReplyIndex(t *testing.T) {
	tr := newTrackerForTest(t)
	tr.Track("edit-1", &TrackedMessage{
		SentID: "msg-1", Target: "u1", ReplyKey: "rk1",
	}, 50*time.Millisecond)

	if got := tr.LookupReply("rk1", "u1"); got != "msg-1" {
		t.Fatalf("pre-eviction LookupReply = %q", got)
	}
	time.Sleep(150 * time.Millisecond) // > TTL
	if got := tr.LookupReply("rk1", "u1"); got != "" {
		t.Errorf("post-eviction LookupReply = %q, want empty", got)
	}
}
```

- [ ] **Step 2: Run, expect pass already (both caches share TTL)**

If the test passes immediately, document the implicit guarantee in `tracker.go` and skip step 3.

- [ ] **Step 3: If it fails, hook the eviction callback**

ttlcache supports `OnEviction`. On eviction of an entry from `cache` whose `ReplyKey != ""`, also delete the corresponding `replyIndex` entry. (If both caches are running their own TTL clocks, alignment may drift; explicit propagation is more reliable than relying on parallel timing.)

- [ ] **Step 4: Run, expect pass**

- [ ] **Step 5: Commit**

```
git commit -am "delivery: align reply-index eviction with edit-cache eviction"
```

### Task 3.3: Save/Load preserves the reply index

**Files:**
- Modify: `processor/internal/delivery/tracker.go` (Save/Load JSON serialization)
- Test: `processor/internal/delivery/tracker_test.go`

- [ ] **Step 1: Round-trip test**

```go
func TestTrackerSaveLoadPreservesReplyIndex(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tracker.json")
	tr1 := newTrackerForTest(t)
	tr1.persistPath = path
	tr1.Track("edit-1", &TrackedMessage{SentID: "m1", Target: "u1", ReplyKey: "rk1"}, time.Hour)
	if err := tr1.Save(); err != nil {
		t.Fatal(err)
	}
	tr2 := newTrackerForTest(t)
	tr2.persistPath = path
	if err := tr2.Load(); err != nil {
		t.Fatal(err)
	}
	if got := tr2.LookupReply("rk1", "u1"); got != "m1" {
		t.Fatalf("LookupReply after load = %q, want m1", got)
	}
}
```

- [ ] **Step 2: Run, expect failure**

- [ ] **Step 3: Either persist the reply index directly, or rebuild from `cache` on Load**

Cheapest: walk the loaded `cache` after Load and re-insert into `replyIndex` for entries with `ReplyKey != ""`. No new persisted format required.

- [ ] **Step 4: Run, expect pass**

- [ ] **Step 5: Commit**

```
git commit -am "delivery: rebuild reply index from edit cache on tracker load"
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

## PR 5 — Wire the change handler to fire monsterChanged

**Goal:** `handlePokemonChange` actually enqueues a render job. The matched users are the same set that originally tracked the encounter. Each user receives a `monsterChanged` render with `original.X` populated.

### Task 5.1: RenderJob carries OriginalView and ChangeType

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

### Task 5.2: handlePokemonChange enqueues the job

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

### Task 5.3: Edit takes priority over reply

**Files:**
- Modify: `processor/internal/delivery/queue.go`
- Test: `processor/internal/delivery/queue_test.go`

- [ ] **Step 1: Failing test**

A queue-level test that schedules a Job with both `EditKey` and `ReplyKey` set, where the tracker has a prior message under the EditKey. Assert the sender's `Edit()` is called (not `Send()`), and `ReplyToID` is *not* stamped on the job.

- [ ] **Step 2: Verify ordering in processJob**

```go
if job.EditKey != "" {
	if prior := fq.tracker.LookupEdit(job.EditKey); prior != nil {
		// edit path — short-circuit before any reply-resolution
		return fq.sender.Edit(prior, job)
	}
}
if job.ReplyKey != "" && job.ReplyToID == "" {
	if msgID := fq.tracker.LookupReply(job.ReplyKey, job.Target); msgID != "" {
		job.ReplyToID = msgID
	}
}
return fq.sender.Send(job)
```

The point of this task is to make explicit (and test) that an existing edit-tracked message wins. Reply only kicks in when there's no prior edit-tracked message for the same key.

- [ ] **Step 3: Run, expect pass**

- [ ] **Step 4: Commit**

```
git commit -am "delivery/queue: edit takes priority over reply when both keys are set"
```

---

## PR 6 — Docs + integration smoke + manual checklist

**Goal:** Admin-facing documentation, an end-to-end integration test, and a checklist for verifying in a live Discord/Telegram channel.

### Task 6.1: API.md / template docs

**Files:**
- Modify: `API.md` (clean bitmask doc)
- Modify: project README or a dedicated docs page if one exists for templates

- [ ] **Step 1: Document the new behaviour**

Document the `monsterChanged` template type, the `{{original.X}}` field set, and that pokemon updates thread as replies on Discord/Telegram automatically when a prior message exists for the same encounter and recipient. Note that edit mode (existing) takes priority — when set, the prior message is updated in place rather than replied to.

- [ ] **Step 2: Commit**

```
git commit -am "docs: monsterChanged template, original.* fields, implicit reply threading"
```

### Task 6.2: Integration smoke test

**Files:**
- Test: `processor/cmd/processor/integration_pokemon_change_test.go` (new)

- [ ] **Step 1: Write the test**

Stand up a fake `Sender` that records jobs. Drive a non-IV pokemon webhook → assert one job sent with template `monster`/`monsterNoIv`. Drive the same encounter with IVs filled → assert a second job sent with template `monsterChanged`, `ReplyKey == encounterID`, and the second job's body contains a reference to the first sent message ID.

- [ ] **Step 2: Run, expect pass after PR 5 lands**

- [ ] **Step 3: Commit**

```
git commit -am "test: end-to-end pokemon-change reply smoke"
```

### Task 6.3: Manual verification checklist

**Files:**
- Modify: this plan file (append a checklist)

- [ ] **Step 1: Append**

```markdown
## Manual verification (before merging)

- [ ] Telegram: !track pikachu → non-IV sighting then encountered IV → see two messages in DM, second is a reply to first
- [ ] Discord: same as above; `message_reference` arrow visible
- [ ] Telegram: !track pikachu clean → both messages delete on TTH; reply chain still threaded
- [ ] !track pikachu edit (existing edit mode) → second sighting *edits* the first message, no reply
- [ ] User adds tracking *between* sighting 1 and sighting 2 → sees only sighting 2 as a fresh message (no prior to reply to)
- [ ] User has two rules where rule A matches at non-IV and rule B matches at IV (different filters) → second message replies to first (same encounter, same target)
- [ ] monsterChanged template can render `{{original.cp}}`, `{{original.fullName}}`, `{{original.encountered}}` with prior values
- [ ] Restart processor mid-encounter: ReplyKey lookup still works after Tracker.Load (PR 3.3)
- [ ] !info form list, !area, !profile, !poracle-clean still work (no regression from helper changes earlier on main)
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
- ✅ Reply via Discord + Telegram (PR 4)
- ✅ Reply works through non-IV → IV → form/gender chain (PR 5 — change types covered in PR 1)
- ✅ Reply combines with clean (PR 4 — both indexes share TTL semantics; the edit cache continues to fire clean-on-TTH evictions independently)
- ✅ Edit takes priority over reply when both keys are present (PR 5.3)
- ✅ Linkage even when rules match the encounter "anyway" (PR 5.2 — the gate-by-bit is removed; everyone matched at the new state gets the change render, replied if a prior message exists)
- ✅ Reply is implicit, not opt-in — no DB column change, no config knob
- ✅ O(1) lookup via dedicated `replyIndex` ttlcache (PR 3.1)
- ✅ All in a branch (worktree at `../PoracleNG-pokemon-changed`, branch `pokemon-changed-reply`)

Risks / open questions:
- `BuildOriginalView` builds from `EncounterState` only — no geocoding/maps for the prior position (the position doesn't change between sightings of the same encounter). If a future change tracks position drift, the view builder needs extending.
- Reply infrastructure is generic; only pokemon wires it. Raids on RSVP changes are an obvious next consumer (replace edit with reply for richer chains). One follow-up PR.
- `replyIndex` is a second ttlcache instance — eviction is wired by the OnEviction callback (Task 3.2). Make sure the `cache` and `replyIndex` use the same TTL settings to avoid divergence; if the existing tracker uses per-entry TTL, the reply index must too.
- The encounter tracker's existing in-memory state (no persistence) means a processor restart loses the "old" state. After restart, the next encounter sighting is treated as a fresh non-IV → no `monsterChanged` fires, and the IV sighting (if it comes after restart) fires as a fresh `monster` render. This is acceptable — change tracking is a best-effort enrichment, not a guarantee.

## Execution Handoff

Plan saved. Two execution paths:

1. **Subagent-Driven Development** (recommended) — fresh subagent per task, two-stage review per task, fast iteration.
2. **Inline Execution** — execute in this session via superpowers:executing-plans, batch checkpoints.

Pick one when you're ready to start.
