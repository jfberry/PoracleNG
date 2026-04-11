# Edit Tracking Design

## Goal

Enable message editing instead of sending new messages for webhook updates (e.g., RSVP changes on raids). The first consumer is raids/eggs with RSVP updates; the plumbing is generic so other types (pokemon encounter changes, etc.) can opt in later.

## Clean Column Encoding

Reuse the existing `clean` tinyint column across all tracking tables (monsters, raid, egg, quest, invasion, lures, nests, gym, forts, maxbattle). No schema migration needed.

| Value | Meaning |
|-------|---------|
| 0 | No tracking, no cleanup |
| 1 | Clean only (delete message on TTH expiry) |
| 2 | Edit only (update existing message, keep after expiry) |
| 3 | Edit + clean (update existing message, delete on expiry) |

## Bot Command Changes

### Edit keyword

Add `arg.edit` keyword to all tracking commands. When `edit` is specified:
- `edit` alone → `clean=2`
- `edit clean` together → `clean=3`
- `clean` alone → `clean=1` (unchanged)

Examples:
```
!raid pikachu           → clean=0
!raid pikachu clean     → clean=1
!raid pikachu edit      → clean=2
!raid pikachu edit clean → clean=3
!track bulbasaur edit   → clean=2
!egg level:5 edit clean → clean=3
```

### Locale changes

- Add `arg.edit` key to all locale files (en.json, de.json, es.json, etc.)
- Add `arg.edit` to `knownKeywordKeys` in `argmatch.go`

### Row text

`rowtext` package displays:
- clean=1: "clean"
- clean=2: "edit"
- clean=3: "edit, clean"

## Edit Key Format

A stable string that uniquely identifies a message across updates for the same destination. Format: `{type}:{identityFields}:{targetID}`

### Per-type keys

| Type | Edit key | Identity |
|------|----------|----------|
| Raid/egg | `raid:{gymID}:{end}:{targetID}` | Same gym + same raid end time = same raid |
| Pokemon (future) | `pokemon:{encounterID}:{targetID}` | Same encounter = same pokemon |

The `{targetID}` suffix is appended per-user by the renderer since one webhook event fans out to multiple destinations.

## Generic Plumbing Changes

### Structs

1. **`webhook.DeliveryJob`** — add `EditKey string` field
2. **`RenderJob`** — add `EditKey string` field so webhook handlers can set the base key (without targetID suffix)

### Flow: webhook handler → renderer → delivery

1. Webhook handler computes base edit key (e.g., `raid:{gymID}:{end}`) and sets it on `RenderJob`
2. DTS renderer propagates `RenderJob.EditKey` into each per-user `DeliveryJob`, appending `:{targetID}` to make the key per-destination
3. `render.go` maps `DeliveryJob.EditKey` → `delivery.Job.EditKey`

### Clean flag interpretation

Anywhere that currently checks `Clean bool`:
- `clean & 1 != 0` (bit 0) → track for TTH deletion
- `clean & 2 != 0` (bit 1) → set EditKey for message editing

In the matching layer, `Clean` is currently `bool`. Change to `int` and propagate the raw value through `MatchedUser` → `DeliveryJob` → `delivery.Job`.

## Raid Handler Changes

In `raid.go`, when building the `RenderJob`:
- Compute `editKey = fmt.Sprintf("raid:%s:%d", gymID, raid.End)`
- Set `RenderJob.EditKey = editKey`
- The renderer checks each matched user's `clean` value: if `clean >= 2`, append `:{targetID}` and set on the `DeliveryJob`; otherwise leave `EditKey` empty (no edit tracking for this user)

### RSVP update flow

1. First raid notification: `EditKey` set → message sent normally → tracked in MessageTracker under edit key with TTH
2. RSVP update arrives for same gym/raid: duplicate checker returns `(false, false)` (changed RSVPs) → same `EditKey` computed → FairQueue looks up tracked message → calls `sender.Edit()` → message updated in place
3. If edit fails (message deleted by user, channel gone, etc.): falls through to send-new, re-tracked under same key
4. On TTH expiry: if clean bit set (value 1 or 3), message deleted; if not (value 2), message stays

## Existing Infrastructure (already built, unused)

These components are fully implemented and tested but have no callers:

- `delivery.Job.EditKey` field
- `FairQueue.processJob()` edit-before-send logic
- `DiscordSender.Edit()` — PATCH /channels/{id}/messages/{id}
- `TelegramSender.Edit()` — editMessageText API
- `MessageTracker.LookupEdit()` / `UpdateEdit()`
- Message tracker persistence across restarts

## Telegram Limitation

Telegram's edit API only updates the text component. Sticker, photo, and location messages sent before the text are not re-sent on edit. Acceptable for the initial implementation.

## What Stays the Same

- DB schema — no migration needed (clean column already accepts integers)
- MessageTracker implementation — already supports edit keys
- FairQueue — already has edit-before-send
- Discord/Telegram senders — Edit() methods already exist
- API tracking CRUD — clean already accepts integer values via flexInt

## Files to Modify

- `processor/internal/bot/argmatch.go` — add `arg.edit` to knownKeywordKeys
- `processor/internal/bot/commands/raid.go` (or equivalent) — handle edit keyword, set clean=2/3
- `processor/internal/bot/commands/track.go` — handle edit keyword
- `processor/internal/bot/commands/egg.go` — handle edit keyword
- `processor/internal/i18n/locale/*.json` — add `arg.edit` translation key
- `processor/internal/webhook/types.go` — add EditKey to DeliveryJob
- `processor/cmd/processor/render.go` — map EditKey from DeliveryJob to delivery.Job
- `processor/cmd/processor/raid.go` — compute edit key for raids
- `processor/internal/dts/renderer.go` — add EditKey to RenderJob, propagate per-user
- `processor/internal/matching/human.go` — change Clean from bool to int in MatchedUser
- `processor/internal/matching/*.go` — propagate int clean through all matchers
- `processor/internal/rowtext/*.go` — display edit/edit+clean descriptions
- `processor/internal/delivery/queue.go` — interpret clean int for tracking decisions
