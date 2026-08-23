# Mute API (v2) — Design

**Date:** 2026-06-11
**Branch:** `mute-api`
**Status:** Approved (brainstorm 2026-06-11)

## Goal

Expose the existing in-memory mute store (`internal/mute`, GitHub #109) over HTTP so API
clients (PoracleWeb and custom integrations) can list, create, and remove a user's mutes.
Today mutes are reachable only via bot commands (`!mute`/`!unmute`), alert buttons, and —
indirectly — `POST /api/command`.

## Decisions (locked)

1. **In-memory semantics kept.** Mutes remain volatile and are lost on restart, exactly as
   the package documents. The API documents this on the wire; it does not add persistence.
2. **Per-user v2 surface only.** Endpoints live under `/api/v2/humans/{id}/mutes` following
   the v2 conventions (strict bodies, problem+json, `X-Poracle-Secret`). No global
   all-users admin listing.
3. **Snapshot inclusion.** `GET /api/v2/humans/{id}/tracking` gains a `mutes` array so one
   call shows everything affecting a user's alerts.

## Approach

REST resource with **composite-key addressing**. A mute has no uid — its identity is
`(scope, value)` per human (the store replaces on same-key Add). Item-level DELETE
addresses entries by `?scope=&value=` query params rather than path segments, because
values include area names with spaces and opaque fort ids; precedent is v2 tracking's
`DELETE …/{type}?uid=` bulk form.

Rejected alternatives: synthetic item paths `/mutes/{scope}/{value}` (path-encoding
hazards for zero gain); action-style `POST /mutes/delete` (breaks v2 resource
conventions).

## Endpoints

All on the shared huma instance; errors RFC 9457 `problem+json`; unknown body and query
params rejected (422); human must exist (404 otherwise).

| Method | Path | Behaviour |
|---|---|---|
| `GET` | `/api/v2/humans/{id}/mutes` | `{mutes:[<item>…]}` — **active** entries only (expired-but-unswept are filtered) |
| `POST` | `/api/v2/humans/{id}/mutes` | Body `{scope, value?, duration_min?}` → `{mute:<item>, replaced:bool}`. Re-muting the same `(scope,value)` replaces the entry (extends expiry) and sets `replaced:true`, mirroring `Store.Add` |
| `DELETE` | `/api/v2/humans/{id}/mutes?scope=&value=` | Remove one entry → `{deleted:[<item>]}`; 404 when no matching mute exists |
| `DELETE` | `/api/v2/humans/{id}/mutes` | No params: remove **all** the user's mutes → `{deleted:[<item>…]}` (empty array when none) |

`DELETE` with `scope` but a missing-yet-required `value` (any scope except `everything`)
is a 422, as is `value` without `scope`. `scope=everything` takes no value in DELETE just
as in POST.

Response-shape conformance: DELETE returns the removed entries (`{deleted:[…]}`),
mirroring v2 tracking's delete shape. POST returns `{mute:<item>, replaced:bool}` — a
deliberate typed struct rather than the humans-action `{status:ok}`, permitted by the
design doc's "status plus extra fields keep their own typed structs" rule; it returns the
canonical entry (computed `expires_at`) so clients don't need a follow-up GET, and
`replaced` mirrors the bot's muted/re-muted distinction. Unknown human → 404 via the same
`GetLite` lookup as v2 tracking's `resolveHuman`.

## Item schema

```json
{ "scope": "gym", "value": "fae12cd34…", "expires_at": 1781190000, "remaining_secs": 3540 }
```

- `scope` — string enum: `gym | pokemon | area | pokestop | station | tracking | everything`.
  These are the existing `mute.Scope*` constants, already user-visible in command syntax.
- `value` — string; `null` for `everything`. Pokemon dex ids and tracking-rule uids are
  numeric **strings** on the wire (the store compares strings; one honest representation).
- `expires_at` — unix seconds.
- `remaining_secs` — derived convenience for UIs (`Entry.RemainingAt`), never negative.

Field descriptions state the volatility: in-memory, cleared by processor restart.

## Create validation (mirrors the bot parser)

- `scope` required, in the enum.
- `value` required for every scope except `everything`, where it must be **absent**.
- `pokemon` / `tracking` values must parse as positive integers.
- `area` values validated case-insensitively against loaded geofence names (as
  `!mute area` does); stored as given.
- `duration_min` optional int, default **60**, bounds **1–10080** (one week).
- A `tracking` uid is **not** ownership-checked: a wrong uid never matches anything
  (harmless), and checking would mean scanning all ten rule-type stores per call.

## Snapshot change

`GET /api/v2/humans/{id}/tracking` response gains `"mutes": [<item>…]` — always present,
`[]` when none, same item schema and active-only filtering as the list endpoint.

## Implementation shape

- New `internal/api/v2_mutes.go` + `v2_mutes_test.go`, following the `RegisterV2*`
  pattern; registered from `main.go` alongside the other v2 humans sub-resources and in
  `registerAllHumaOpsForTest` for the golden spec.
- Deps: the existing `*mute.Store` (already on `ProcessorService`), `store.HumanStore`
  (existence check), and the state manager (area-name validation).
- Store addition: `ListActive(humanID string, now int64) []Entry` — like `List` but
  skipping expired entries, so list/snapshot don't surface entries the sweeper hasn't
  reaped yet. No other store changes.
- Snapshot: `v2_snapshot.go` adds the `Mutes` field, populated via the same helper that
  converts `mute.Entry` → wire item.
- No state reload, no dispatcher interaction, no DB.

## Testing

- httptest unit tests: create (fresh + replace), default and bounded duration, every
  validation rejection (bad scope, missing/forbidden value, non-numeric pokemon/tracking,
  unknown area, duration out of bounds), unknown human 404, list filters expired entries,
  delete one / delete all / delete miss 404, snapshot includes mutes.
- Golden OpenAPI spec regenerated; new schemas pinned.
- Docs: API surface notes in CLAUDE.md (Mute Infrastructure section) + CHANGELOG entry.
