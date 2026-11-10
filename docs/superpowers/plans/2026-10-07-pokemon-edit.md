# Pokemon Edit Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the `edit` flag work for pokemon tracking — follow-ups for the same encounter edit the original alert in place while it is at most `pokemon_edit_window_mins` old, then fall back to threaded replies — and reject `edit` in the v2 API on types that cannot honour it.

**Architecture:** Pokemon `RenderJob`s gain a base edit key `pokemon:<encounterID>` and an `EditMaxAge`; the existing renderer turns the key into a per-user edit key for rules with the edit bit. The delivery queue records when each tracked message was sent and skips the edit when the original is older than `EditMaxAge`, sending a reply instead and tracking it under its own key so the original keeps its clean-deletion. v2 `Translate` for gym/invasion/nest/maxbattle/quest returns 422 for `edit: true` and `ToRule` reports `edit: null`.

**Tech Stack:** Go, huma v2 (v2 API), jellydator/ttlcache v3 (message tracker), testify (command tests only).

**Spec:** `docs/superpowers/specs/2026-10-07-pokemon-edit-design.md`

## Global Constraints

- Config keys live in `[tracking]`: `pokemon_edit` (bool, default `true`) and `pokemon_edit_window_mins` (int, default `5`; `0` or negative = no limit).
- Base pokemon edit key is exactly `"pokemon:" + encounterID`; the renderer appends `":" + user.ID` (`internal/dts/renderer.go:453`).
- The window is measured from the original send (`TrackedMessage.SentAt`); edits never change `SentAt`.
- `SentAt == 0` (entry persisted before this change) with a non-zero window ⇒ treat as too old ⇒ reply.
- `EditMaxAge == 0` ⇒ today's behaviour exactly (raid, egg, lure, showcase never set it).
- With `pokemon_edit = false`, pokemon jobs carry no edit key and no `EditMaxAge`.
- v2 rejected types: gym, invasion, nest, maxbattle, quest. 422 detail text: `edit is not supported for <type> tracking`, error location `body.edit`. Their `ToRule` always returns `Edit: nil`.
- v1 API and bot commands are unchanged.
- Pre-commit gate from `processor/`: `go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...` (if the local golangci-lint is too old for the module's Go version, say so and rely on CI).
- Commit messages end with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`.

## Review Focus

1. **Restart with persisted tracker entries** (no `SentAt`) followed by a re-send of the same encounter: expect a threaded reply, not an edit of a possibly hours-old message. → Task 2, `TestEditWindow_UnknownSentAtReplies`.
2. **Window boundary**: an original exactly `EditMaxAge` old is still edited; one second older is not. → Task 2, `TestEditTooOld`.
3. **Clean-deletion of the original after a past-window reply**: the original's tracker entry must survive (not be overwritten by the reply), or it is never deleted. → Task 2, `TestEditWindow_PastWindowReplies`.
4. **Operator switches `pokemon_edit` off** while users have edit-bit rules: no job may carry an edit key. → Task 3, `TestDispatchPokemonAlert_EditDisabled_NoEditKey`.
5. **GET → PUT round-trip of a legacy gym rule** stored with the edit bit (written by v1): must not 422. → Task 4, `TestV2Gym_LegacyEditBitRoundTrips`.

---

### Task 1: Pokemon edit config

**Files:**
- Modify: `processor/internal/config/config.go` (`TrackingConfig` ~line 86-100; defaults ~line 938; imports)
- Modify: `processor/internal/api/config_schema.go:462` (tracking field list)
- Modify: `config/config.example.toml:587` (after `pokemon_change_tracking`)
- Test: `processor/internal/config/tracking_test.go` (create)

**Interfaces:**
- Produces: `config.TrackingConfig.PokemonEdit bool`, `config.TrackingConfig.PokemonEditWindowMins int`, `func (t TrackingConfig) PokemonEditMaxAge() time.Duration` (0 = no limit).

- [ ] **Step 1: Write the failing test**

Create `processor/internal/config/tracking_test.go`:

```go
package config

import (
	"testing"
	"time"
)

func TestTrackingPokemonEditDefaults(t *testing.T) {
	cfg, err := loadFromReader(t, "")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !cfg.Tracking.PokemonEdit {
		t.Error("pokemon_edit should default to true")
	}
	if cfg.Tracking.PokemonEditWindowMins != 5 {
		t.Errorf("pokemon_edit_window_mins should default to 5, got %d", cfg.Tracking.PokemonEditWindowMins)
	}
	if got := cfg.Tracking.PokemonEditMaxAge(); got != 5*time.Minute {
		t.Errorf("PokemonEditMaxAge() = %v, want 5m", got)
	}
}

func TestTrackingPokemonEditOverrides(t *testing.T) {
	cfg, err := loadFromReader(t, "[tracking]\npokemon_edit = false\npokemon_edit_window_mins = 12\n")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Tracking.PokemonEdit {
		t.Error("pokemon_edit = false not honoured")
	}
	if got := cfg.Tracking.PokemonEditMaxAge(); got != 12*time.Minute {
		t.Errorf("PokemonEditMaxAge() = %v, want 12m", got)
	}
}

func TestPokemonEditMaxAgeNoLimit(t *testing.T) {
	for _, mins := range []int{0, -3} {
		tc := TrackingConfig{PokemonEditWindowMins: mins}
		if got := tc.PokemonEditMaxAge(); got != 0 {
			t.Errorf("window %d: PokemonEditMaxAge() = %v, want 0 (no limit)", mins, got)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run (from `processor/`): `go test ./internal/config -run 'PokemonEdit' -v`
Expected: build failure — `cfg.Tracking.PokemonEdit undefined`.

- [ ] **Step 3: Implement**

In `TrackingConfig`, directly after the `PokemonChangeTracking` field:

```go
	// PokemonEdit honours the edit bit on pokemon rules: a follow-up for the
	// same encounter edits the original alert in place while it is within
	// PokemonEditWindowMins. Default true. When false, pokemon jobs carry no
	// edit key and follow-ups arrive as threaded replies.
	PokemonEdit bool `toml:"pokemon_edit"`
	// PokemonEditWindowMins is how old (minutes since first send) a pokemon
	// alert may be and still be edited; older alerts get a threaded reply so
	// the update isn't lost up a busy channel. Default 5. 0 or negative = no
	// limit.
	PokemonEditWindowMins int `toml:"pokemon_edit_window_mins"`
```

Add the method after the `TrackingConfig` type declaration (add `"time"` to the import block):

```go
// PokemonEditMaxAge returns the pokemon edit window; 0 means no limit.
func (t TrackingConfig) PokemonEditMaxAge() time.Duration {
	if t.PokemonEditWindowMins <= 0 {
		return 0
	}
	return time.Duration(t.PokemonEditWindowMins) * time.Minute
}
```

In the defaults block (`Tracking: TrackingConfig{...}` ~line 938):

```go
		Tracking: TrackingConfig{
			PokemonChangeTracking:      true,
			PokemonEdit:                true,
			PokemonEditWindowMins:      5,
			QuestSummaryEnabled:        true,
			QuestSummaryBufferTTLHours: 24,
		},
```

In `config_schema.go`, after the `pokemon_change_tracking` entry:

```go
			{Name: "pokemon_edit", Type: "bool", Default: true, Description: "Honour the edit flag on pokemon tracking: a follow-up for the same encounter (IV reveal, weather boost, species/form change) edits the original alert in place while it is within pokemon_edit_window_mins. When disabled, follow-ups arrive as threaded replies.", HotReload: true},
			{Name: "pokemon_edit_window_mins", Type: "int", Default: 5, Description: "Only edit a pokemon alert while it is at most this many minutes old; after that, follow-ups arrive as threaded replies so they aren't lost up a busy channel. 0 = no limit.", HotReload: true},
```

In `config/config.example.toml`, after `pokemon_change_tracking = true`:

```toml
# pokemon_edit - honour the `edit` flag on pokemon tracking. When true, a
#   follow-up for the same encounter (IV reveal, weather boost, species/form
#   change) edits the original alert in place while it is recent enough.
#   When false, pokemon rules with `edit` behave as if it were not set:
#   follow-ups arrive as threaded replies.
pokemon_edit = true
# pokemon_edit_window_mins - only edit while the original alert is at most
#   this many minutes old; after that, follow-ups arrive as threaded replies
#   so they aren't lost further up a busy channel. 0 = no limit.
pokemon_edit_window_mins = 5
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -count=1 ./internal/config ./internal/api`
Expected: PASS (the api package has schema tests that walk the field list).

- [ ] **Step 5: Commit**

```bash
git add processor/internal/config/config.go processor/internal/config/tracking_test.go processor/internal/api/config_schema.go config/config.example.toml
git commit -m "feat(config): pokemon_edit and pokemon_edit_window_mins tracking options"
```

---

### Task 2: Edit window in the delivery queue

**Files:**
- Modify: `processor/internal/delivery/tracker.go` (`TrackedMessage` struct ~line 18-43)
- Modify: `processor/internal/delivery/delivery.go` (`Job` struct, after `EditKey` ~line 35)
- Modify: `processor/internal/delivery/queue.go` (edit block ~line 372-414; tracking block ~line 563-597; new helper `editTooOld`)
- Test: `processor/internal/delivery/queue_test.go`, `processor/internal/delivery/tracker_test.go`

**Interfaces:**
- Produces: `delivery.TrackedMessage.SentAt int64`, `delivery.Job.EditMaxAge time.Duration`, `func editTooOld(msg *TrackedMessage, maxAge time.Duration, now time.Time) bool` (package-private).

- [ ] **Step 1: Write the failing tests**

Append to `processor/internal/delivery/queue_test.go`:

```go
func TestEditTooOld(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	at := func(ago time.Duration) *TrackedMessage {
		return &TrackedMessage{SentAt: now.Add(-ago).Unix()}
	}
	if editTooOld(at(5*time.Minute), 5*time.Minute, now) {
		t.Error("a message exactly at the window must still be editable")
	}
	if !editTooOld(at(5*time.Minute+time.Second), 5*time.Minute, now) {
		t.Error("a message one second past the window must not be edited")
	}
	if editTooOld(at(3*time.Hour), 0, now) {
		t.Error("maxAge 0 means no limit")
	}
	if !editTooOld(&TrackedMessage{SentAt: 0}, 5*time.Minute, now) {
		t.Error("unknown SentAt with a window must count as too old")
	}
	if editTooOld(&TrackedMessage{SentAt: 0}, 0, now) {
		t.Error("unknown SentAt with no window must stay editable (raid behaviour)")
	}
}

// newEditWindowQueue builds a started queue around a fresh tracker and a
// mock sender whose sends return sentID.
func newEditWindowQueue(t *testing.T, sentID string) (*FairQueue, *MessageTracker, *queueMockSender) {
	t.Helper()
	mock := &queueMockSender{platform: "discord", sentID: sentID}
	senders := map[string]Sender{"discord": mock}
	tracker := NewMessageTracker(t.TempDir(), senders)
	t.Cleanup(func() { tracker.cache.Stop() })
	fq := NewFairQueue(senders, tracker, QueueConfig{
		ConcurrentDiscord:  1,
		ConcurrentWebhook:  1,
		ConcurrentTelegram: 1,
	}, nil)
	fq.Start()
	return fq, tracker, mock
}

func pokemonEditJob(maxAge time.Duration) *Job {
	return &Job{
		Target:     "user1",
		Type:       "discord:user",
		Message:    json.RawMessage(`{"content":"updated"}`),
		EditKey:    "pokemon:enc1:user1",
		EditMaxAge: maxAge,
		ReplyKey:   "enc1",
		TTH:        TTH{Hours: 1},
	}
}

func trackOriginal(tracker *MessageTracker, sentAt int64) {
	tracker.Track("pokemon:enc1:user1", &TrackedMessage{
		SentID:   "chan1:msg-original",
		Target:   "user1",
		Type:     "discord:user",
		Clean:    3,
		ReplyKey: "enc1",
		SentAt:   sentAt,
	}, 30*time.Minute)
}

func TestEditWindow_InsideWindowEdits(t *testing.T) {
	fq, tracker, mock := newEditWindowQueue(t, "chan1:msg-new")
	trackOriginal(tracker, time.Now().Add(-1*time.Minute).Unix())

	fq.enqueue(pokemonEditJob(5*time.Minute), true)
	time.Sleep(100 * time.Millisecond)
	fq.Stop()

	if got := mock.getEditCalls(); len(got) != 1 || got[0] != "chan1:msg-original" {
		t.Fatalf("expected one edit of chan1:msg-original, got %v", got)
	}
	if got := len(mock.getSendCalls()); got != 0 {
		t.Fatalf("expected no new send inside the window, got %d", got)
	}
}

func TestEditWindow_PastWindowReplies(t *testing.T) {
	fq, tracker, mock := newEditWindowQueue(t, "chan1:msg-new")
	trackOriginal(tracker, time.Now().Add(-10*time.Minute).Unix())

	fq.enqueue(pokemonEditJob(5*time.Minute), true)
	time.Sleep(100 * time.Millisecond)
	fq.Stop()

	if got := len(mock.getEditCalls()); got != 0 {
		t.Fatalf("expected no edit past the window, got %d", got)
	}
	sends := mock.getSendCalls()
	if len(sends) != 1 {
		t.Fatalf("expected one new send past the window, got %d", len(sends))
	}
	if sends[0].ReplyToID != "chan1:msg-original" {
		t.Errorf("past-window send should reply to the original, got ReplyToID=%q", sends[0].ReplyToID)
	}

	// The original keeps its entry (and so its clean-deletion).
	orig := tracker.LookupEdit("pokemon:enc1:user1")
	if orig == nil || orig.SentID != "chan1:msg-original" {
		t.Fatalf("original entry under the edit key must survive, got %+v", orig)
	}
	// The reply is tracked under its own key, with a send time.
	reply := tracker.LookupEdit("clean:discord:user:user1:chan1:msg-new")
	if reply == nil {
		t.Fatal("past-window reply should be tracked under its own clean: key")
	}
	if reply.SentAt == 0 {
		t.Error("tracked reply should record SentAt")
	}
	// The next follow-up replies to the newest message in the chain.
	if got := tracker.LookupReply("enc1", "user1"); got != "chan1:msg-new" {
		t.Errorf("reply index should point at the newest message, got %q", got)
	}
}

func TestEditWindow_UnknownSentAtReplies(t *testing.T) {
	fq, tracker, mock := newEditWindowQueue(t, "chan1:msg-new")
	trackOriginal(tracker, 0) // persisted before SentAt existed

	fq.enqueue(pokemonEditJob(5*time.Minute), true)
	time.Sleep(100 * time.Millisecond)
	fq.Stop()

	if got := len(mock.getEditCalls()); got != 0 {
		t.Fatalf("unknown age must not be edited, got %d edits", got)
	}
	if got := len(mock.getSendCalls()); got != 1 {
		t.Fatalf("expected a reply send, got %d", got)
	}
}

func TestEditWindow_NoLimitEditsOldMessage(t *testing.T) {
	fq, tracker, mock := newEditWindowQueue(t, "chan1:msg-new")
	trackOriginal(tracker, time.Now().Add(-2*time.Hour).Unix())

	fq.enqueue(pokemonEditJob(0), true)
	time.Sleep(100 * time.Millisecond)
	fq.Stop()

	if got := len(mock.getEditCalls()); got != 1 {
		t.Fatalf("EditMaxAge 0 must always edit (raid behaviour), got %d edits", got)
	}
}
```

Append to `processor/internal/delivery/tracker_test.go`:

```go
func TestTrackerSaveLoadPreservesSentAt(t *testing.T) {
	dir := t.TempDir()
	senders := map[string]Sender{"discord": &mockSender{}}

	mt1 := NewMessageTracker(dir, senders)
	mt1.Track("pokemon:enc1:u1", &TrackedMessage{
		SentID: "s1",
		Target: "u1",
		Type:   "discord:user",
		SentAt: 1_800_000_000,
	}, 5*time.Minute)
	if err := mt1.Save(); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	mt1.cache.Stop()

	mt2 := NewMessageTracker(dir, senders)
	if err := mt2.Load(); err != nil {
		t.Fatalf("Load failed: %v", err)
	}
	defer mt2.cache.Stop()

	got := mt2.LookupEdit("pokemon:enc1:u1")
	if got == nil || got.SentAt != 1_800_000_000 {
		t.Fatalf("SentAt lost across Save/Load: %+v", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/delivery -run 'EditTooOld|EditWindow|PreservesSentAt' -v`
Expected: build failure — `unknown field SentAt`, `unknown field EditMaxAge`, `undefined: editTooOld`.

- [ ] **Step 3: Implement**

In `tracker.go`, add to `TrackedMessage` after `Template`:

```go
	// SentAt is the unix time the message was first sent. Edits leave it
	// unchanged, so it measures how far up the channel the message now sits.
	// 0 for entries persisted before this field existed.
	SentAt int64 `json:"sent_at,omitempty"`
```

In `delivery.go`, add to `Job` directly after `EditKey`:

```go
	// EditMaxAge bounds how old (since first send) a tracked message may be
	// and still be edited in place. 0 = no limit (raid/egg/lure/showcase).
	// Past it, the job is sent as a new message — threaded via ReplyKey —
	// and tracked under its own key so the original keeps its clean-deletion.
	EditMaxAge time.Duration `json:"-"`
```

In `queue.go`, add the helper (next to `processJob`):

```go
// editTooOld reports whether a tracked message is past the job's edit window.
// maxAge 0 means no window. SentAt 0 is an entry persisted before SentAt
// existed; with no known age it is treated as too old.
func editTooOld(msg *TrackedMessage, maxAge time.Duration, now time.Time) bool {
	if maxAge <= 0 {
		return false
	}
	if msg.SentAt == 0 {
		return true
	}
	return now.Sub(time.Unix(msg.SentAt, 0)) > maxAge
}
```

In `processJob`'s edit block, declare a flag before it and branch on the age. Change:

```go
	if job.EditKey != "" {
		existing := fq.tracker.LookupEdit(job.EditKey)
		if existing != nil {
			logref.Infof(job.LogReference, "edit: found tracked message for key=%s, attempting edit", job.EditKey)
```

to:

```go
	editWindowPassed := false
	if job.EditKey != "" {
		existing := fq.tracker.LookupEdit(job.EditKey)
		if existing != nil && editTooOld(existing, job.EditMaxAge, time.Now()) {
			// Too far up the channel for an edit to be seen: reply instead
			// (the reply-stamping path below) and track the reply under its
			// own key so the original keeps its clean-deletion.
			logref.Debugf(job.LogReference, "edit: prior for key=%s is past the %v edit window, replying instead", job.EditKey, job.EditMaxAge)
			editWindowPassed = true
		} else if existing != nil {
			logref.Infof(job.LogReference, "edit: found tracked message for key=%s, attempting edit", job.EditKey)
```

The rest of that block (the edit attempt and the trailing `} else { … no tracked message … }`) is unchanged.

In the tracking block, change:

```go
		key := job.EditKey
		if key == "" {
			key = fmt.Sprintf("clean:%s:%s:%s", job.Type, job.Target, sent.ID)
		}
```

to:

```go
		key := job.EditKey
		if key == "" || editWindowPassed {
			key = fmt.Sprintf("clean:%s:%s:%s", job.Type, job.Target, sent.ID)
		}
```

and add `SentAt: time.Now().Unix(),` to the `&TrackedMessage{…}` literal passed to `fq.tracker.Track`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -count=1 ./internal/delivery`
Expected: PASS, including the existing `TestFairQueueEditLookup`, `TestFairQueueEditFallback` and `TestQueueDoesNotStampWhenEditKeyMatches` (they use `EditMaxAge == 0`).

- [ ] **Step 5: Commit**

```bash
git add processor/internal/delivery
git commit -m "feat(delivery): edit window — reply instead of editing messages older than EditMaxAge"
```

---

### Task 3: Pokemon jobs carry the edit key and window

**Files:**
- Modify: `processor/cmd/processor/render.go` (`RenderJob` struct ~line 84; dispatch loop ~line 197)
- Modify: `processor/cmd/processor/pokemon.go` (RenderJob literals ~lines 255, 303, 323, 366; new helper)
- Test: `processor/cmd/processor/pokemon_change_test.go`, `processor/cmd/processor/render_test.go`

**Interfaces:**
- Consumes: `config.TrackingConfig.PokemonEdit`, `PokemonEditMaxAge()` (Task 1); `delivery.Job.EditMaxAge` (Task 2).
- Produces: `RenderJob.EditMaxAge time.Duration`; `func (ps *ProcessorService) pokemonEditFields(encounterID string) (string, time.Duration)`; `func buildDeliveryJob(rj RenderJob, dj webhook.DeliveryJob, tth delivery.TTH, tileBytes []byte, snap *snapshots.Snapshot) *delivery.Job`.

- [ ] **Step 1: Write the failing tests**

Append to `processor/cmd/processor/pokemon_change_test.go`:

```go
func TestDispatchPokemonAlert_EditEnabled_SetsEditFields(t *testing.T) {
	ps, ch, _ := minimalProcessor(t)
	ps.cfg.Tracking.PokemonEdit = true
	ps.cfg.Tracking.PokemonEditWindowMins = 5

	encounterID := "enc-edit"
	change := &tracker.EncounterChange{
		EncounterID: encounterID,
		Type:        tracker.ChangeSpecies,
		Old:         tracker.EncounterState{PokemonID: 16, CP: 500},
		New:         tracker.EncounterState{PokemonID: 132, CP: 500},
	}
	ps.dispatchPokemonAlert(pokemonDispatchInput{
		encounterID:    encounterID,
		change:         change,
		matched:        []webhook.MatchedUser{{ID: "still", Type: "discord:user", Clean: 2}},
		priorOnlyUsers: []webhook.MatchedUser{{ID: "gone", Type: "discord:user", Clean: 2}},
		isEncountered:  true,
	})

	jobs := drainRenderJobs(ch)
	if len(jobs) != 2 {
		t.Fatalf("expected monster + monsterChanged jobs, got %d", len(jobs))
	}
	for _, j := range jobs {
		if j.EditKey != "pokemon:"+encounterID {
			t.Errorf("IsChange=%v: EditKey = %q, want pokemon:%s", j.IsChange, j.EditKey, encounterID)
		}
		if j.EditMaxAge != 5*time.Minute {
			t.Errorf("IsChange=%v: EditMaxAge = %v, want 5m", j.IsChange, j.EditMaxAge)
		}
	}
}

func TestDispatchPokemonAlert_EditDisabled_NoEditKey(t *testing.T) {
	ps, ch, _ := minimalProcessor(t)
	ps.cfg.Tracking.PokemonEdit = false
	ps.cfg.Tracking.PokemonEditWindowMins = 5

	ps.dispatchPokemonAlert(pokemonDispatchInput{
		encounterID:   "enc-off",
		matched:       []webhook.MatchedUser{{ID: "u", Type: "discord:user", Clean: 2}},
		isEncountered: true,
	})

	jobs := drainRenderJobs(ch)
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if jobs[0].EditKey != "" || jobs[0].EditMaxAge != 0 {
		t.Errorf("pokemon_edit=false must not set edit fields, got key=%q maxAge=%v", jobs[0].EditKey, jobs[0].EditMaxAge)
	}
}
```

Add `"time"` to that file's imports if not already present.

Append to `processor/cmd/processor/render_test.go`:

```go
func TestBuildDeliveryJob_CarriesEditFields(t *testing.T) {
	rj := RenderJob{AlertType: "pokemon", ReplyKey: "enc1", EditMaxAge: 5 * time.Minute}
	dj := webhook.DeliveryJob{Target: "u1", Type: "discord:user", EditKey: "pokemon:enc1:u1"}

	got := buildDeliveryJob(rj, dj, delivery.TTH{Hours: 1}, nil, nil)

	if got.EditKey != "pokemon:enc1:u1" {
		t.Errorf("EditKey = %q", got.EditKey)
	}
	if got.EditMaxAge != 5*time.Minute {
		t.Errorf("EditMaxAge = %v, want 5m", got.EditMaxAge)
	}
	if got.ReplyKey != "enc1" || got.MsgType != "pokemon" {
		t.Errorf("ReplyKey/MsgType not carried: %+v", got)
	}
}
```

Add `delivery` and `webhook` imports to `render_test.go` if not present (`github.com/pokemon/poracleng/processor/internal/delivery`, `github.com/pokemon/poracleng/processor/internal/webhook`).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./cmd/processor -run 'EditEnabled|EditDisabled|BuildDeliveryJob' -v`
Expected: build failure — `j.EditMaxAge undefined`, `undefined: buildDeliveryJob`.

- [ ] **Step 3: Implement**

In `render.go`, add to `RenderJob` directly after `EditKey string`:

```go
	// EditMaxAge is copied onto every delivery.Job; see delivery.Job.EditMaxAge.
	// Pokemon only (from [tracking] pokemon_edit_window_mins); 0 elsewhere.
	EditMaxAge time.Duration
```

Extract the `delivery.Job` literal in the dispatch loop into a function and add `EditMaxAge`:

```go
// buildDeliveryJob assembles the delivery.Job for one rendered message.
func buildDeliveryJob(rj RenderJob, dj webhook.DeliveryJob, tth delivery.TTH, tileBytes []byte, snap *snapshots.Snapshot) *delivery.Job {
	return &delivery.Job{
		Target:        dj.Target,
		Type:          dj.Type,
		Message:       dj.Message,
		TTH:           tth,
		Clean:         dj.Clean,
		Name:          dj.Name,
		LogReference:  dj.LogReference,
		Lat:           parseCoordFloat(dj.Lat),
		Lon:           parseCoordFloat(dj.Lon),
		EditKey:       dj.EditKey,
		EditMaxAge:    rj.EditMaxAge,
		ReplyKey:      rj.ReplyKey,
		MsgType:       rj.AlertType,
		StaticMapData: tileBytes,
		Language:      dj.Language,
		Template:      dj.TemplateRequested,
		SnapshotData:  snap,
	}
}
```

and replace the literal in the loop with:

```go
			ps.dispatcher.Dispatch(buildDeliveryJob(job, j, tth,
				tileBytesForMessage(j.Message, job.TileImageData, tileURL),
				ps.buildSnapshot(job, j, tth)))
```

In `pokemon.go`, add the helper:

```go
// pokemonEditFields returns the base edit key and edit window for a pokemon
// RenderJob. The renderer appends ":<userID>" to the key only for rules with
// the edit bit. With [tracking] pokemon_edit off, both are zero so no job
// edits and follow-ups thread as replies.
func (ps *ProcessorService) pokemonEditFields(encounterID string) (string, time.Duration) {
	if !ps.cfg.Tracking.PokemonEdit {
		return "", 0
	}
	return "pokemon:" + encounterID, ps.cfg.Tracking.PokemonEditMaxAge()
}
```

Set both fields on every pokemon `RenderJob` literal (the `!trackingEnabled` path in `ProcessPokemon` ~line 255, the `ps.dispatcher == nil` path ~line 303, the matched-users bucket ~line 323, and the prior-only `monsterChanged` bucket ~line 366). In `dispatchPokemonAlert`, compute once at the top:

```go
	editKey, editMaxAge := ps.pokemonEditFields(in.encounterID)
```

and add to each literal in that function:

```go
				EditKey:           editKey,
				EditMaxAge:        editMaxAge,
```

In `ProcessPokemon`'s `!trackingEnabled` path, compute `editKey, editMaxAge := ps.pokemonEditFields(pokemon.EncounterID)` just before the literal and add the same two fields.

Verify with `grep -n "ReplyKey:" cmd/processor/pokemon.go` that every pokemon RenderJob literal now also has `EditKey:` and `EditMaxAge:`.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -count=1 ./cmd/processor`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add processor/cmd/processor/render.go processor/cmd/processor/render_test.go processor/cmd/processor/pokemon.go processor/cmd/processor/pokemon_change_test.go
git commit -m "feat(pokemon): honour the edit flag with pokemon:<encounter> edit keys"
```

---

### Task 4: Reject `edit` in v2 for types that cannot honour it

**Files:**
- Modify: `processor/internal/api/v2_tracking.go` (new helper `rejectEdit`)
- Modify: `processor/internal/api/v2_gym.go`, `v2_invasion.go`, `v2_nest.go`, `v2_maxbattle.go`, `v2_quest.go` (field `doc`, `Translate`, `ToRule`)
- Modify: `processor/internal/api/v2_pokemon.go:75` (field `doc`)
- Test: `processor/internal/api/v2_gym_test.go`, `processor/internal/api/v2_quest_test.go`, `processor/internal/api/v2_pokemon_test.go`

**Interfaces:**
- Produces: `func rejectEdit(typeName string, edit *bool) error` (nil when edit is nil/false).

- [ ] **Step 1: Write the failing tests**

Append to `processor/internal/api/v2_gym_test.go`:

```go
func TestV2Gym_EditTrueRejected(t *testing.T) {
	r, gs, _, restore := newV2GymTestAPI(t)
	defer restore()

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/gym", `[{"team":"mystic","edit":true}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for edit:true on gym, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "edit is not supported for gym tracking") {
		t.Errorf("422 body should explain why: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "body.edit") {
		t.Errorf("422 should point at body.edit: %s", w.Body.String())
	}
	if len(gs.AllRows()) != 0 {
		t.Error("rejected rule must not be stored")
	}
}

func TestV2Gym_EditFalseAccepted(t *testing.T) {
	r, _, _, restore := newV2GymTestAPI(t)
	defer restore()

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/gym", `[{"team":"mystic","edit":false}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("edit:false must be accepted, got %d: %s", w.Code, w.Body.String())
	}
}

// A rule written by v1 with the edit bit must read back as edit:null and
// survive a GET → PUT round-trip without a 422.
func TestV2Gym_LegacyEditBitRoundTrips(t *testing.T) {
	r, gs, _, restore := newV2GymTestAPI(t)
	defer restore()

	uid, err := gs.Insert(&db.GymTrackingAPI{ID: "u1", ProfileNo: 1, Team: 1, Clean: 3})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	w := v2DoReq(t, r, http.MethodGet, "/api/v2/humans/u1/tracking/gym", "")
	rules := v2RulesArray(t, v2DecodeBody(t, w), "rules")
	if len(rules) != 1 {
		t.Fatalf("expected 1 rule, got %d", len(rules))
	}
	if v, present := rules[0]["edit"]; !present || v != nil {
		t.Fatalf("edit should read back as present-but-null, got present=%v value=%v", present, v)
	}
	if rules[0]["clean"] != true {
		t.Errorf("clean bit should still read back, got %v", rules[0]["clean"])
	}

	put := `{"team":"mystic","clean":true,"edit":null}`
	w = v2DoReq(t, r, http.MethodPut, "/api/v2/humans/u1/tracking/gym/"+itoa(uid), put)
	if w.Code != http.StatusOK {
		t.Fatalf("round-trip PUT must succeed, got %d: %s", w.Code, w.Body.String())
	}
}
```

Add `"strings"` to `v2_gym_test.go` imports (`itoa` is an existing package test helper).

Append to `processor/internal/api/v2_quest_test.go` (uses that file's existing test-API constructor; open the file and use its name — it follows the `newV2<Type>TestAPI(t)` pattern returning `(r, store, pushes, restore)`):

```go
func TestV2Quest_EditTrueRejected(t *testing.T) {
	r, _, _, restore := newV2QuestTestAPI(t)
	defer restore()

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/quest", `[{"reward_type":3,"edit":true}]`)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for edit:true on quest, got %d: %s", w.Code, w.Body.String())
	}
}
```

Append to `processor/internal/api/v2_pokemon_test.go` (same constructor pattern, `newV2PokemonTestAPI`):

```go
func TestV2Pokemon_EditTrueAccepted(t *testing.T) {
	r, ps, _, restore := newV2PokemonTestAPI(t)
	defer restore()

	w := v2DoReq(t, r, http.MethodPost, "/api/v2/humans/u1/tracking/pokemon", `[{"pokemon_id":25,"edit":true}]`)
	if w.Code != http.StatusOK {
		t.Fatalf("edit:true must be accepted on pokemon, got %d: %s", w.Code, w.Body.String())
	}
	rows := ps.AllRows()
	if len(rows) != 1 || rows[0].Clean&2 == 0 {
		t.Fatalf("edit bit (2) should be stored, got %+v", rows)
	}
}
```

If a constructor's name or return arity differs from the above, adapt the call to the file's actual helper — do not add a new constructor.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api -run 'EditTrue|EditFalse|LegacyEditBit' -v`
Expected: `TestV2Gym_EditTrueRejected` and `TestV2Quest_EditTrueRejected` FAIL (200 instead of 422); `TestV2Gym_LegacyEditBitRoundTrips` FAILS (edit reads back true); pokemon and edit:false tests PASS.

- [ ] **Step 3: Implement**

In `v2_tracking.go`, next to `humaErr`:

```go
// rejectEdit returns a 422 when a rule asks for edit mode on a tracking type
// whose alerts never carry an edit key (the flag would be silently ignored).
// nil/false are accepted so GET → PUT round-trips of legacy rows keep working.
func rejectEdit(typeName string, edit *bool) error {
	if edit == nil || !*edit {
		return nil
	}
	msg := "edit is not supported for " + typeName + " tracking"
	return huma.Error422UnprocessableEntity(msg, &huma.ErrorDetail{
		Location: "body.edit",
		Message:  msg,
		Value:    true,
	})
}
```

For each of gym, invasion, nest, maxbattle, quest:

1. At the very start of `translateV2<Type>`, add (using the matching zero value and type name):

```go
	if err := rejectEdit("gym", req.Edit); err != nil {
		return db.GymTrackingAPI{}, err
	}
```

   (`"invasion"`/`db.InvasionTrackingAPI{}` in `translateV2Invasion`; `"nest"`/`db.NestTrackingAPI{}`; `"maxbattle"`/`db.MaxbattleTrackingAPI{}`; `"quest"`/`db.QuestTrackingAPI{}`.)

2. In the `packClean(...)` call, pass `false` for edit so a stray value can never be stored:

```go
		Clean:                 packClean(valueOr(req.Clean, false), false, valueOr(req.Summary, false)),
```

3. In the row-to-rule function (`gymRowToRule`, `v2InvasionToRule`, `nestRowToRule`, `maxbattleRowToRule`, `questRowToRule`), replace the `Edit:` line with:

```go
		Edit:                  nil, // edit is not supported for this type; always null
```

4. Replace the `Edit` field's `doc` tag with (type name substituted):

```go
	Edit     *bool   `json:"edit,omitempty" nullable:"true" doc:"Not supported for gym tracking — omit, or send null/false (true is rejected with 422). Always returned as null."`
```

In `v2_pokemon.go:75`, replace the `Edit` field's `doc` with:

```go
	Edit     *bool   `json:"edit,omitempty" nullable:"true" doc:"Edit the original alert in place when the same encounter is re-sent (IV reveal, weather boost, species/form change), while the alert is within the server's pokemon_edit_window_mins (default 5); later updates arrive as threaded replies. The operator can disable pokemon edit ([tracking] pokemon_edit). Clean bitmask bit 2. Omit to disable (default false). Returned as null when false."`
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test -count=1 ./internal/api`
Expected: PASS. If an existing test asserted `edit: true` round-tripping on one of the five rejected types, update it to assert the new 422/null behaviour and say so in the commit message.

- [ ] **Step 5: Commit**

```bash
git add processor/internal/api
git commit -m "feat(api): reject edit:true in v2 for gym, invasion, nest, maxbattle and quest"
```

---

### Task 5: Docs

**Files:**
- Modify: `API.md` (`#### Pokemon change template (monsterChanged)` section, the paragraph at ~line 267)
- Modify: `CLAUDE.md` (`### Pokemon change events` section)
- Modify: `docs/v2-api-design.md` (common-fields table row for `edit`, ~line 144)

- [ ] **Step 1: Update API.md**

In the paragraph beginning "The encounter event itself (non-IV → IV)…", replace the sentence "Edit mode (clean bit 2) takes priority when set: the prior message is updated in place rather than replied to." with:

```markdown
Edit mode (clean bit 2) takes priority when set and `[tracking] pokemon_edit` is on (default): while the original alert is at most `pokemon_edit_window_mins` old (default 5, `0` = no limit, measured from the first send), the follow-up edits it in place — with the `monster` template if the rule still matches, or the `monsterChanged` template if it no longer does. Once the original is older than the window, follow-ups arrive as threaded replies instead, so they aren't lost further up a busy channel.
```

- [ ] **Step 2: Update CLAUDE.md**

In "### Pokemon change events", replace "Edit mode (clean bit 2) takes priority when set." with:

```markdown
Edit mode (clean bit 2) takes priority when set: pokemon jobs carry base edit key `pokemon:<encounterID>` (renderer appends `:<userID>`) and `EditMaxAge` from `[tracking] pokemon_edit_window_mins` (default 5, 0 = no limit). `FairQueue.processJob` skips the edit when the tracked original's `SentAt` is older than `EditMaxAge` (or unknown), replies instead, and tracks the reply under its own `clean:` key so the original keeps its clean-deletion. `[tracking] pokemon_edit = false` drops the edit key entirely. v2 rejects `edit: true` for gym/invasion/nest/maxbattle/quest (their alerts never carry an edit key).
```

- [ ] **Step 3: Update docs/v2-api-design.md**

Replace the common-fields row:

```markdown
| `edit` | bool | keep the message updated in place |
```

with:

```markdown
| `edit` | bool | keep the message updated in place — honoured for pokemon (within the server's edit window), raid, egg, lure and incident (showcase); rejected with 422 for gym, invasion, nest, maxbattle and quest |
```

- [ ] **Step 4: Run the full gate**

Run (from `processor/`): `go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...`
Expected: all pass (report if golangci-lint cannot run locally).

- [ ] **Step 5: Commit**

```bash
git add API.md CLAUDE.md docs/v2-api-design.md
git commit -m "docs: pokemon edit window and v2 edit support per type"
```
