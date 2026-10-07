# Pokemon edit mode, with an edit window — design

**Status:** proposed
**Date:** 2026-10-07

## Problem

The `edit` flag (clean bitmask bit 2) is accepted on pokemon tracking — by
`!track … edit`, by the v1 API, and by the v2 API, whose `edit` field is
documented as "keep the message updated in place" — but it does nothing.
Pokemon render jobs never set a base edit key, so the renderer
(`internal/dts/renderer.go:453`) never derives a per-user `EditKey`, and every
follow-up for an encounter is sent as a new message threaded as a reply.
API.md and CLAUDE.md both claim edit takes priority for pokemon; they are
wrong today.

The same is true for gym, invasion, nest, maxbattle and quest: v2 exposes
`edit` on all of them, but no handler sets an edit key. (Fort has no clean
column, so its v2 type already omits `edit`.) Only raid, egg (shared raid
key), lure and showcase incidents honour it.

Golbat now sends pokemon with IVs immediately and re-sends them quickly, so
follow-ups (weather boost, species/form/gender change, IV drift) are frequent.
Editing in place is the useful behaviour for those — but only while the
original is still visible. On a busy channel an edit to a message from
several minutes ago lands far up the scrollback where nobody sees it.

## Goals

1. Make `edit` work for pokemon tracking: follow-ups for the same encounter
   update the original message in place.
2. Stop editing once the original is older than a configurable window, and
   fall back to today's threaded reply.
3. Let the operator turn pokemon edit off entirely.
4. Stop advertising `edit` on types that cannot honour it: reject it in the
   v2 API.

## Non-goals

- Wiring edit for gym, invasion, nest, maxbattle, quest or fort.
- Any window for raid/egg/lure/showcase edits (they stay unlimited).
- Changing the frozen v1 API.
- Fixing the pre-existing raid behaviour where a failed edit's fallback send
  overwrites the original's tracker entry (noted under "Known issues").

## Configuration

New keys in `[tracking]`, mirrored in `config/config.example.toml` and the
config-editor schema (`internal/api/config_schema.go`):

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

Defaults: `pokemon_edit = true`, `pokemon_edit_window_mins = 5`. Negative
window values are treated as 0 (no limit).

## Behaviour

For a user whose pokemon rule has the edit bit, with `pokemon_edit = true`:

| Situation | Result |
|---|---|
| First alert for the encounter | Sent and tracked under edit key `pokemon:<encounterID>:<userID>` |
| Follow-up, original ≤ window old, rule still matches | Original edited in place with the `monster` template |
| Follow-up, original ≤ window old, rule no longer matches | Original edited in place with the `monsterChanged` template (`{{original.X}}` available) |
| Follow-up, original > window old | New message, threaded as a reply to the latest message in the chain (today's behaviour) |
| Original no longer tracked (TTL evicted) | New message, no reply target (today's behaviour) |

- **The window is measured from the original send**, not from the last edit.
  An edit does not move the message in the channel, so the original's age is
  what determines whether the user will see it.
- **Once a chain passes the window it stays on replies.** The tracker entry
  under the edit key still holds the original (and its old `SentAt`), so every
  later follow-up also finds it too old and replies to the latest message.
- With `pokemon_edit = false`, pokemon jobs carry no base edit key and
  behaviour is exactly today's. Rules keep their stored edit bit, so the
  operator can switch it back on without users redoing their tracking.
- Users without the edit bit are unaffected in every case.

## Design

### Processor: pokemon dispatch (`cmd/processor/pokemon.go`)

When `cfg.Tracking.PokemonEdit` is true, every pokemon `RenderJob` — both the
matched-users `monster` bucket and the prior-only `monsterChanged` bucket, and
the non-change single-job path — sets:

- `EditKey: "pokemon:" + encounterID` (the base; the renderer appends
  `":" + user.ID` only for users with the edit bit), and
- a new `EditMaxAge` field from `pokemon_edit_window_mins` (0 = no limit).

`RenderPokemon` and `RenderPokemonChanged` already accept `editKeyBase`.
Prior-only users are rebuilt by `rebuildMatchedUserForChange(targetID,
prior.Clean, prior.Template)`, so the edit bit from the original send carries
through and the `monsterChanged` render gets an edit key.

`EditMaxAge` flows `RenderJob` → `webhook.DeliveryJob` → `delivery.Job` the
same way `EditKey` does today (`cmd/processor/render.go`). Raid, lure and
showcase jobs leave it 0.

### Delivery (`internal/delivery`)

- `TrackedMessage` gains `SentAt int64` (`json:"sent_at,omitempty"`), set in
  `FairQueue.processJob` when a send is tracked, and persisted with the rest
  of the entry. Edits do not change it.
- `Job` gains `EditMaxAge time.Duration` (0 = no limit).
- In the edit path of `processJob`, after `LookupEdit` finds an entry: if
  `job.EditMaxAge > 0` and (`existing.SentAt == 0` or
  `now - existing.SentAt > job.EditMaxAge`), skip the edit (debug log
  `edit: prior for key=… is <age> old, past window <max>; replying instead`)
  and fall through to the existing reply-stamping path. `SentAt == 0` means
  an entry persisted before this change; with no age known it is treated as
  too old, which is the conservative choice.
- **Tracking the fallback send.** Today a new send is tracked under
  `job.EditKey`, which would overwrite the original's tracker entry and lose
  its clean-deletion. When the edit was skipped because of the window, the
  new message is instead tracked under its own key
  (`clean:<type>:<target>:<sentID>`, the existing non-edit form). The
  original keeps its entry and TTL (so it is still clean-deleted), and
  `Track` still points the reply index for `(replyKey, target)` at the new
  message, so the next follow-up replies to the latest in the chain.
- Jobs with `EditMaxAge == 0` (raid, egg, lure, showcase) take exactly
  today's path.

### Rejecting `edit` where it is not supported

Supported: **pokemon, raid, egg, lure, incident** (incident edit takes effect
for showcase events — showcase alerts reuse incident rules with
`grunt_type="showcase"`; other incident events go through the invasion path
without an edit key). Rejected: **gym, invasion, nest, maxbattle, quest**
(fort has no `edit` field in v2 at all).

**v2 API.** In each rejected type's `Translate`, `edit: true` returns a 422
problem response with an `errors[]` entry at `body.edit` and detail
`edit is not supported for <type> tracking`. `false` and `null` remain
accepted. Each rejected type's `ToRule` always reports `edit: null`, even
when a stored row has the bit set (left by v1 or older commands), so a GET →
PUT round-trip of an existing rule never fails. The `edit` field `doc` on
rejected types becomes "Not supported for <type> tracking — omit, or send
null/false." The pokemon field `doc` mentions the window and that the
operator can disable pokemon edit.

**Commands.** No change needed. Only `!track`, `!raid`, `!egg` and `!lure`
define the `edit` keyword in their parameter lists; the commands for the
rejected types (`!gym`, `!invasion`/`!incident`, `!nest`, `!maxbattle`,
`!quest`, `!fort`) never accepted it — `edit` there is reported as an
unrecognised argument. (`parseCommonTrackFields` would set bit 2 if the
keyword were present, but no rejected type's params produce it.)

**v1 API and stored rows.** Untouched. A stored edit bit on a rejected type
is harmless — nothing reads it.

### Docs

- API.md `monsterChanged` section: describe edit-in-window, the
  `monsterChanged` edit for no-longer-matching users, and the fallback.
- CLAUDE.md "Pokemon change events": same correction; add the two config keys.
- `config/config.example.toml` and `config_schema.go`: the two new keys.
- `docs/v2-api-design.md` common-fields table: note which types support `edit`.

## Testing

Delivery (`internal/delivery/queue_test.go`):
- Edit inside the window → `Sender.Edit` called, no new send.
- Edit past the window → new send with `ReplyToID` = original; original's
  tracker entry still present under the edit key; new message tracked under
  its own key; reply index points at the new message.
- `SentAt == 0` with `EditMaxAge > 0` → reply, not edit.
- `EditMaxAge == 0` → edits regardless of age (raid behaviour unchanged).
- `SentAt` survives tracker save/load.

Processor (`cmd/processor/pokemon_change_test.go`):
- `pokemon_edit = true`: matched user with edit bit gets `EditKey`
  `pokemon:<enc>:<user>` and `EditMaxAge` = window; user without the bit gets
  no `EditKey`.
- Prior-only user with edit bit gets a `monsterChanged` job carrying the edit
  key.
- `pokemon_edit = false`: no `EditKey` on any pokemon job.

v2 API (`internal/api/v2_*_test.go`):
- `edit: true` on gym (and one other rejected type) → 422 at `body.edit`.
- `edit: false` / omitted on gym → accepted.
- Stored gym row with the edit bit → GET returns `edit: null`.
- `edit: true` on pokemon → accepted and stored as bit 2.

Pre-commit gate: `go build ./... && go vet ./... && go test -count=1 ./... &&
golangci-lint run ./...`.

## Known issues (out of scope)

- Raid/egg/lure/showcase: when an edit attempt fails and the queue falls back
  to a new send, that send is tracked under the same edit key, overwriting the
  original's entry so the original is never clean-deleted. The pokemon
  window path avoids this by tracking under the message's own key; the same
  treatment could be applied to the failed-edit fallback later.
- Telegram alerts are sent as several messages (sticker, photo, text,
  location), and `TelegramSender.Edit` only edits the text message. A pokemon
  edit on Telegram therefore updates the text (CP, IV, name, weather) but not
  the sticker or the static map photo — e.g. a species change keeps the old
  sticker. Same limitation raids have today; not changed here.
