# Raid RSVP-update template + reply chain — Design

> **Status:** Draft for implementation. Branch: `raid-rsvp` (off `slash-commands-design`).

**Goal:** Let operators define a compact `rsvpChanges` DTS template for RSVP-update notifications so they don't have to repeat the full raid card on every RSVP change. Optionally thread the egg → raid → rsvp-change messages into one Discord reply chain so the raid lifecycle is visible as a single thread in the channel.

## Decisions locked

1. **Single shared template type `rsvpChanges`.** Used for RSVP-update notifications from both raids and eggs. The template can branch internally on raid state (`{{#if eggLevel}}…{{else}}…{{/if}}`) if the operator wants different output for unhatched vs. hatched.
2. **Fallback chain:** when this is an RSVP-change job (not first notification) AND not edit mode AND `rsvpChanges` template exists, use it; otherwise fall through to the originating type (`raid` for raids, `egg` for eggs). Today's behavior is preserved by definition when `rsvpChanges` is not defined.
3. **Edit mode always uses the full template.** Editing a compact message in-place loses the user's original full-message context. The compact template is a non-edit surface only.
4. **Reply chain always set on egg/raid jobs.** Every raid and egg render job gets `ReplyKey = "raidlife:{gymID}:{raidEnd}"`. Threading only activates when there's a prior tracked message to reply to (i.e. operator has `clean` or `edit` bit set on the prior rule). No new schema, no opt-in friction, no opt-out flag — the existing tracker semantics correctly gate behavior.
5. **TTH for `rsvpChanges` messages = latest RSVP timeslot in the rendered state.** The original raid/egg alert keeps `raid.End` TTH as today. Compact RSVP messages get cleaned shortly after the meaningful event time so the thread doesn't linger.
6. **Same enrichment surface as raid/egg.** No `OriginalView`-style delta data exposed. Operators who want "before:5 going, after:7 going" are encouraged to build it from the current state and consider rendering it client-side.
7. **No `fallbacks/dts.json` entry.** Shipping a default there would make the feature unconditionally active, defeating the opt-in. Example template(s) live in `examples/dts/rsvpChanges/` for operators to copy if they want.

## Why a shared `rsvpChanges` type (not separate raid/egg variants)

- The DTS view object for raid and egg shares the same enrichment shape — same gym, same RSVP structure, same time/location fields.
- The RSVP-update payload doesn't meaningfully change between unhatched egg and hatched raid: it's about who's going and when.
- One template, one file to maintain, one fallback decision in the handler. Two would double operator effort.
- If a future operator wants distinct egg vs. raid styling, they can branch inside the template on `eggLevel` / `pokemonId` (already in the view).

## Why "reply always" instead of "reply iff rsvp_changes != 0"

- The egg → raid hand-off naturally reads as a continuation, and Discord's reply-threading makes that visible even for operators who *don't* use RSVP updates at all.
- The `MessageTracker` already stores any message whose job carries a `ReplyKey` (`wantsTracking := IsClean || IsEdit || EditKey != "" || ReplyKey != ""` in `FairQueue.processJob`). Since every raid + egg job now sets `ReplyKey`, threading works out of the box — no `clean`/`edit` prerequisite. Reply-only entries (no clean bit) sit in the tracker until natural TTL expiry; the auto-delete path remains gated on `IsClean`, so the user's messages are never deleted unintentionally.
- Operators who don't care about threading aren't affected either way — Discord's reply UI is a small chevron indicator that doesn't disrupt the channel layout.

## ReplyKey format

```
raidlife:{gymID}:{raidEnd}
```

Both egg and raid webhooks carry `gym_id` and `raid_end` (Golbat). Two raids at the same gym later in the day get distinct `raid_end` timestamps, so the key isolates one raid lifecycle.

`MessageTracker.LookupReply(ReplyKey, Target)` already returns the most-recent tracked message for that key. The egg notification gets tracked → the raid notification finds it → replies to it. The first rsvpChanges message finds the raid → replies to it. The second rsvpChanges message finds the first rsvpChanges → replies. Standard chain.

## TTH override for `rsvpChanges`

Current raid path: enricher writes `tth = ComputeTTH(raid.End)` into the view map; render pool reads it and sets `delivery.Job.TTH`.

For `rsvpChanges` jobs the cleanup time should be the latest timeslot in the current RSVP state. Implementation: the raid handler (which has both `raid.End` and the full RSVP state from the duplicate cache) computes the per-job TTH at render-job construction time and stuffs it directly into the `RenderJob` (or a new `OverrideCleanTTH` field). The renderer respects the override; if absent, falls back to map-derived `raid.End` as today.

Edge cases:
- No RSVPs in the current state (shouldn't happen for an `rsvpChanges` job by definition, but defensive): fall back to `raid.End`.
- Latest timeslot in the past at send time: TTH is zero → the message is auto-deleted on first tracker tick. This is acceptable — it means we delivered an update for a slot that already ended. Logs note the case.

## What the operator-visible surface looks like

Operator workflow today:
1. `!raid level5 rsvp clean` → gets the full raid card on first notification AND on every RSVP change, clean-deletes at raid end.
2. `!raid level5 rsvp edit clean` → gets the full raid card on first notification, edits in-place on every RSVP change, clean-deletes at raid end.

Operator workflow after this work (assuming they copy `examples/dts/rsvpChanges/`):
1. `!raid level5 rsvp clean` → gets the full raid card on first notification; **compact `rsvpChanges` card on every RSVP change**; the compact card replies to the original raid notification; cleanup is per-message (raid card at raid end, compact cards at latest timeslot).
2. `!raid level5 rsvp edit clean` → **identical to today** (edit mode is unchanged).

Operator workflow if they ALSO track eggs with `clean`:
- Egg notification → raid notification replies to egg → rsvpChanges replies to raid. Whole lifecycle is one thread.

## Out of scope

- Per-rule opt-out of the reply chain. If chatty operators want standalone messages: don't set `clean`/`edit` on the rule, or don't track eggs alongside raids. Add an explicit `no_reply` flag only if anyone complains.
- Delta enrichment (`{{previous.going}}` etc.). If operators want this, expose later behind a config flag — most use cases just want the current state.
- Shipping a default `rsvpChanges` template in `fallbacks/dts.json`. Operator-opt-in by file presence only.
- Telegram-side reply rendering. Telegram already honors `reply_to_message_id` via `delivery.Job.ReplyToID`; the existing pokemon flow exercises this path. We get Telegram threading for free.

## Files touched (preview)

- `processor/cmd/processor/raid.go` — template type selection + ReplyKey + TTH override.
- `processor/cmd/processor/egg.go` — same shape if egg handler is separate (or the same file).
- `processor/cmd/processor/render.go` — honor the per-job TTH override.
- `processor/internal/api/dts_fields.go` — register `rsvpChanges` type for the DTS editor.
- `processor/internal/dts/` — verify nothing in the loader needs changes (lookup is type-name-based; new types Just Work once an entry exists).
- `processor/cmd/processor/raid_test.go` + `egg_test.go` — template selection + ReplyKey + TTH tests.
- `examples/dts/rsvpChanges/README.md` + a sample raid.json — operator copy-and-paste artifact.
- `CLAUDE.md` — raid section update.
