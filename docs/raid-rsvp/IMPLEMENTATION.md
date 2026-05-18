# Raid RSVP-update template + reply chain — Implementation

> **For agentic workers:** Use `superpowers:subagent-driven-development`. Fresh subagent per task, two-stage review per task, continuous execution.

**Companion document:** `DESIGN.md` (rationale and decisions). Read first.

**Branch:** `raid-rsvp` (off `slash-commands-design`).

**Scope:** Optional `rsvpChanges` DTS template type, implicit egg→raid→rsvpChanges reply chain, per-job TTH override so compact RSVP messages clean at the latest timeslot.

---

## File structure

### Modified

- `processor/cmd/processor/raid.go` — template type selection, ReplyKey, TTH override per matched user.
- `processor/cmd/processor/egg.go` — same shape if eggs are handled in a separate file (confirm during Task 0).
- `processor/cmd/processor/render.go` — wire per-job TTH override into `delivery.Job.TTH`.
- `processor/internal/dts/` — verify nothing structural is needed; add `rsvpChanges` to any type-list constant if one exists.
- `processor/internal/api/dts_fields.go` (or equivalent) — register `rsvpChanges` so the DTS editor surfaces it.
- `CLAUDE.md` — raid section: document the template, the reply chain, the per-message TTH.

### New

- `examples/dts/rsvpChanges/README.md` — operator-facing note + the user's example screenshot reference.
- `examples/dts/rsvpChanges/rsvp-update.json` — sample template (compact card, location links, current RSVP counts per timeslot).

### Tests

- `processor/cmd/processor/raid_test.go` — additions for template selection and ReplyKey.
- `processor/cmd/processor/egg_test.go` — same.
- A targeted TTH test for the per-job override path.

---

## Phase 0: Familiarisation

### Task 0.1: Confirm egg handler location and shape

- [ ] Read `processor/cmd/processor/raid.go` to see the current render-job construction flow.
- [ ] Find where eggs are handled — likely `processor/cmd/processor/egg.go` if separate, or a branch within `raid.go`.
- [ ] Note the exact line where the RenderJob is constructed for each matched user.
- [ ] Verify that `RenderJob` has the fields needed: `TemplateType`, `EditKey`, `ReplyKey`, plus a path to override the cleanup TTH.

Output: brief notes on where each modification lands. No code yet.

---

## Phase 1: Template selection + ReplyKey + TTH

### Task 1.1: Raid handler template selection

- [ ] In `processor/cmd/processor/raid.go`'s render-job construction loop, decide template type per matched user:
  - If `isFirstNotification == true` → `templateType = "raid"` (current behavior).
  - Else if `db.IsEdit(user.Clean)` → `templateType = "raid"`.
  - Else → check `ts.Exists("rsvpChanges", platform, user.Template, language)`; if true → `templateType = "rsvpChanges"`; else → `templateType = "raid"`.
- [ ] Look at any existing template-selection helper (the slash dispatcher uses similar shape) to see if there's a reusable function.
- [ ] Keep the change minimal — one branch in the per-user loop. Don't refactor surrounding code.

### Task 1.2: Egg handler template selection

- [ ] Same logic in the egg handler. `rsvpChanges` falls back to `egg` (not `raid`).
- [ ] If eggs are processed in the same file as raids (a branch on raid state), it's one function with the fallback type derived from the originating raid state.

### Task 1.3: ReplyKey on raid + egg jobs

- [ ] In both handlers, ALWAYS set `RenderJob.ReplyKey = fmt.Sprintf("raidlife:%s:%d", raid.GymID, raid.End)` on every job — first notification AND RSVP changes.
- [ ] No condition on `rsvp_changes` or `clean` — threading is gated downstream by whether prior messages are tracked.
- [ ] Use `raid.End` (Unix seconds) — both egg and raid webhooks carry the same value, so eggs and the subsequent raid land on the same key.

### Task 1.4: Per-job TTH override for `rsvpChanges`

- [ ] When the chosen template type is `rsvpChanges`, compute the cleanup TTH from the latest RSVP timeslot in the current rendered state (the RSVP state already lives in the duplicate cache; pull it out at handler-build time).
- [ ] Set the per-job TTH override. Choose one of:
  - Add `OverrideCleanTTH int64` (Unix seconds) to `RenderJob` and have the render pool honor it; OR
  - Compute the full `delivery.TTH{Days,Hours,Minutes,Seconds}` in the handler and set it on the `RenderJob` directly, bypassing the enrichment-map path for `rsvpChanges` jobs.
- [ ] Defensive fallback: if no RSVPs in the state at the moment of the rsvpChanges job (shouldn't happen by definition), use `raid.End`.
- [ ] If the latest timeslot is already in the past: TTH = 0 (the tracker will evict on first tick). Log at debug; don't fail.

### Task 1.5: Render pool honors the TTH override

- [ ] In `processor/cmd/processor/render.go` (the `tthFromMap` path), prefer the override on `RenderJob` when present; fall back to map-derived TTH otherwise.
- [ ] Confirm no other fields on the view are affected — the override only changes what gets stuffed into `delivery.Job.TTH`.

---

## Phase 2: DTS surface

### Task 2.1: Register `rsvpChanges` in the DTS fields API

- [ ] Find the API handler for `/api/dts/fields/{type}` (likely `processor/internal/api/dts.go` or `dts_fields.go`).
- [ ] Add `rsvpChanges` as a known type. Same field surface as `raid` + `egg` (no extra delta fields per the design).
- [ ] If there's a type-list constant elsewhere (e.g. an `allDTSTypes` slice), add `rsvpChanges` there too.
- [ ] No DB migration needed — this is just metadata so the DTS web editor knows what fields to autocomplete.

---

## Phase 3: Examples

### Task 3.1: Sample `rsvpChanges` template

- [ ] Create `examples/dts/rsvpChanges/README.md` explaining:
  - What this template is for.
  - When it's used (RSVP-change notifications, non-edit mode).
  - The fallback behavior (raid/egg if absent).
  - How to install (copy `rsvp-update.json` into `config/dts/`).
- [ ] Create `examples/dts/rsvpChanges/rsvp-update.json` with a sample template. Reference format (from the operator's example):
  - Title row with bell emoji + "RSVP Update: <raid summary> at <gym name>".
  - Body: "Players registered:" header, then one row per timeslot showing time-of-day + going/maybe counts.
  - Location line with Google/Apple/Waze map link.
  - Same image as the corresponding raid template.
- [ ] Look at existing example templates in `examples/dts/` (e.g. `NPlumb`, `multilingual`, `Fabio`) for the JSON shape — match it.

---

## Phase 4: Tests

### Task 4.1: Template-selection unit tests

- [ ] In `processor/cmd/processor/raid_test.go`, add tests:
  - First notification → `templateType == "raid"`.
  - RSVP change without `rsvpChanges` defined → `templateType == "raid"`.
  - RSVP change with `rsvpChanges` defined + non-edit → `templateType == "rsvpChanges"`.
  - RSVP change with edit bit set → `templateType == "raid"` regardless of template existence.
- [ ] Mirror tests in `processor/cmd/processor/egg_test.go` with `egg` as the fallback.

### Task 4.2: ReplyKey unit tests

- [ ] Add tests asserting `ReplyKey` is set on every raid + egg job with the expected format (`raidlife:{gymID}:{raidEnd}`).
- [ ] Sanity: same gym at different `raidEnd` produces different ReplyKey (lifecycle isolation).

### Task 4.3: TTH override unit test

- [ ] Build a synthetic raid state with three RSVP timeslots and a chosen `rsvpChanges` template.
- [ ] Assert the job's TTH points at the latest timeslot, not `raid.End`.
- [ ] Assert that for first-notification (or edit-mode) jobs the TTH is still `raid.End`.

### Task 4.4: Integration test for the full chain

- [ ] Run a sequence: egg notification → raid notification (post-hatch simulating the same gym+end) → two rsvpChanges notifications.
- [ ] Assert: all four jobs share the same ReplyKey. (Threading itself is exercised by existing MessageTracker tests; we just need the key to be right.)
- [ ] If integration setup is heavy, skip this and rely on per-task unit tests + manual smoke.

---

## Phase 5: Docs

### Task 5.1: CLAUDE.md raid section

- [ ] In the raid section (or message-lifecycle section), document:
  - The `rsvpChanges` template type (optional, falls back to `raid`/`egg`).
  - The implicit reply chain via `ReplyKey = raidlife:{gymID}:{raidEnd}`.
  - The per-job TTH for `rsvpChanges` (latest timeslot, not raid end).
  - That edit mode always uses the full template.
- [ ] Keep it terse — one paragraph plus a bullet list.

### Task 5.2: Cross-reference from existing docs

- [ ] If `docs/slash-commands/DESIGN.md` mentions raid alerts, consider adding a one-line pointer.
- [ ] Probably nothing else needed.

---

## Phase 6: Final review

### Task 6.1: /simplify sweep

- [ ] Run the simplify skill over the branch's diff.
- [ ] Apply any meaningful findings.

### Task 6.2: Build + race tests green

- [ ] `cd processor && go build ./...`
- [ ] `cd processor && go test -race ./internal/... ./cmd/...`
- [ ] No failures.

### Task 6.3: Smoke document

- [ ] Add `docs/raid-rsvp/SMOKE.md` with a 10-item checklist an operator works through to verify the feature against a real Golbat raid stream.

---

## Cross-cutting

### Backward compatibility

The feature is opt-in by template file presence:
- If `rsvpChanges` template doesn't exist → fall through to current `raid`/`egg` behavior. No diff visible to operators who don't copy the example.
- ReplyKey is always set but only does anything when prior messages are tracked. Operators who don't use `clean`/`edit` on their rules see no change.

### Hot path cost

- One extra `ts.Exists()` lookup per matched user per RSVP-change job. Cheap (in-memory map check).
- One extra string format for `ReplyKey`. Negligible.
- TTH computation reads RSVP state from the duplicate cache (already accessed by the handler).

### Edge cases to bear in mind during implementation

- Two raids at the same gym back-to-back: different `raid.End` → different ReplyKey → no cross-contamination.
- Operator tracks raids but not eggs: raid notifications have no prior egg to reply to → standalone, then rsvpChanges replies to raid as expected.
- Operator tracks eggs but not raids: eggs are standalone, rsvpChanges don't fire (no raid tracking → no rsvp_changes column set).
- Operator tracks both with different `clean` settings: each rule independent; ReplyKey present but threading depends on whether THAT rule's message is tracked.
- `rsvp_only` mode (`rsvp_changes=2`): first message only fires when RSVPs exist. With rsvpChanges template, the first message still uses `raid`/`egg` (full card) per Task 1.1's first-notification rule.
