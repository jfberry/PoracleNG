# API Delivery Destination — Design & Third-Party Specification

**Date:** 2026-07-19
**Status:** Design approved, not yet implemented
**Audience:** Part 1 is the contract for the third party implementing the receiver. Part 2 is the PoracleNG implementation plan.

---

## Summary

PoracleNG gains a third delivery platform alongside Discord and Telegram: `api`. An `api` destination is an HTTP endpoint operated by a third party. Poracle renders alerts through a DTS template set dedicated to the `api` platform and POSTs each rendered alert, wrapped in a stable envelope, to that endpoint.

Two destination types exist: `api:user` and `api:channel`. They behave exactly like their Discord and Telegram counterparts for matching, filtering, profiles, areas, mutes, rate limiting and clean/edit lifecycle. The only differences are the transport and the fact that there is no command-line surface — `api` destinations are created and configured entirely through the v2 REST API.

---

# Part 1 — Receiver Specification

> **Canonical hand-off document:** the self-contained receiver contract is [docs/api-delivery-receiver-spec.md](../../api-delivery-receiver-spec.md). Part 1 below is retained as design context; the standalone doc is what a partner implements against. Keep the two in sync — the envelope and response tables must match.

This part is the contract. A receiver that implements it correctly will work with PoracleNG without further coordination.

## 1.1 Endpoint

The operator configures a single absolute HTTPS URL. Poracle sends every operation — send, edit, delete — to that one URL as a `POST`. The operation is discriminated by the `op` field in the body and mirrored in the `X-Poracle-Op` header.

One HTTP request carries exactly one message. Poracle does not batch. The receiver may batch internally.

## 1.2 Request headers

| Header | Value |
|---|---|
| `Content-Type` | `application/json; charset=utf-8` |
| `X-Poracle-Secret` | The shared secret, verbatim. Header name and an optional value prefix are operator-configurable, so a deployment may instead send `Authorization: Bearer <secret>`. |
| `X-Poracle-Op` | `send`, `edit`, or `delete` — mirrors the body field. |
| `X-Poracle-Message-Id` | Mirrors `message_id` in the body, so a receiver can dedupe or log without parsing the body. |
| `User-Agent` | `PoracleNG/<version>` |

`X-Poracle-Signature` and `X-Poracle-Timestamp` are **reserved**. They are not sent in v1. A future version may add an opt-in HMAC-SHA256 body signature; receivers must ignore unrecognised `X-Poracle-*` headers rather than reject them.

The receiver MUST reject any request whose secret does not match, with `401` or `403`.

## 1.3 Envelope

All three operations share one envelope shape. Fields marked *omitted when empty* are absent from the JSON entirely rather than sent as `null` or `""`.

### `op: "send"`

```json
{
  "version": 1,
  "op": "send",
  "message_id": "7c9e6a1f-4b2d-4c8a-9f3e-1a2b3c4d5e6f",
  "revision": 0,
  "sent_at": 1770000000,
  "alert_type": "pokemon",
  "template_id": "default",
  "destination": {
    "id": "u-42",
    "type": "api:user",
    "name": "James",
    "language": "en"
  },
  "location": { "lat": 51.5074, "lon": -0.1278 },
  "expires_at": 1770001800,
  "lifecycle": { "clean": true, "editable": false },
  "in_reply_to": "a1b2c3d4-5e6f-4a7b-8c9d-0e1f2a3b4c5d",
  "tracking_uids": [45, 46],
  "areas": ["london", "city"],
  "payload": {
    "title": "Pikachu 100%",
    "pokemon_id": 25,
    "iv": 100,
    "cp": 842,
    "level": 25,
    "despawn": "18:30:00",
    "address": "10 Downing Street, London"
  }
}
```

| Field | Type | Notes |
|---|---|---|
| `version` | int | Envelope version. `1` for this specification. Incremented only for breaking changes; additive fields do not bump it. Receivers MUST reject unknown major versions rather than guess. |
| `op` | string | `send` \| `edit` \| `delete`. |
| `message_id` | string | Poracle-minted UUIDv4 identifying the **logical message**, not the request. Constant across the original send, every subsequent edit, and the eventual delete. Matches `^[0-9a-f-]{36}$` (lowercase hex + hyphens) and is therefore always colon-free. |
| `revision` | int | **Reserved — always `0` in the current implementation.** The intended future contract is monotonic (`0` send, `1`,`2`,… edits, delete carrying the last revision), forming a richer idempotency key with `message_id`. Until monotonic revisions ship, receivers must not key on it — see §1.5. |
| `sent_at` | int | Unix seconds at which Poracle issued the request. |
| `alert_type` | string | One of the DTS alert types — see §1.7. |
| `template_id` | string | The DTS template identifier that produced `payload`. Lets the receiver switch on operator-defined template variants. |
| `destination.id` | string | The Poracle human ID. Chosen by the third party at creation time. Constrained to `^[A-Za-z0-9._~-]{1,128}$`. |
| `destination.type` | string | `api:user` or `api:channel`. |
| `destination.name` | string | Human-readable label. Display only. |
| `destination.language` | string | The locale used to render `payload`. |
| `location` | object | Alert coordinates. Omitted when the alert has no location. |
| `expires_at` | int | Unix seconds at which the alert stops being relevant (despawn, raid end, quest expiry). Omitted when the alert has no natural expiry. |
| `lifecycle.clean` | bool | `true` means Poracle intends to send an `op: "delete"` for this message near `expires_at`. See §1.6. |
| `lifecycle.editable` | bool | `true` means Poracle may send one or more `op: "edit"` for this message. |
| `in_reply_to` | string | *Omitted when empty.* Identifier of a previously delivered message to the same destination that this one continues — e.g. an egg alert followed by the raid alert for the same gym. Its value is the `id` the receiver returned for that earlier message, or that message's `message_id` if the receiver returned none. |
| `tracking_uids` | int[] | *Omitted when empty.* Database UIDs of the tracking rules that matched. Stable identifiers the receiver can use to offer "stop tracking this" against the v2 tracking API. |
| `areas` | string[] | *Omitted when empty.* Geofence area names containing the alert. |
| `payload` | object | The rendered alert content, in the canonical schema of §1.7. Always a JSON object, never a string or array. |

`in_reply_to`, `tracking_uids`, and `areas` are populated on every send where they have a value — there is no staged rollout for these three. The static map URL is **not** an envelope field: it is the payload field `static_map` (§1.7.2), present only when the template that produced `payload` references `{{staticMap}}`. `revision` (above) is the only envelope field that remains reserved/unimplemented in this version — see §1.5.

### `op: "edit"`

Identical to `send`, plus:

| Field | Type | Notes |
|---|---|---|
| `provider_message_id` | string | *Omitted when the receiver returned no `id` for the original send.* The receiver's own identifier for the message being edited. |

`payload` carries the full new content — edits are replacements, not patches. `message_id` is the same value as the original send, which is how a receiver that ignored `provider_message_id` can still locate the message. `revision` stays `0` in the current implementation (monotonic revisions are a future addition).

### `op: "delete"`

```json
{
  "version": 1,
  "op": "delete",
  "message_id": "7c9e6a1f-4b2d-4c8a-9f3e-1a2b3c4d5e6f",
  "revision": 0,
  "provider_message_id": "abc123",
  "sent_at": 1770001800,
  "destination": { "id": "u-42", "type": "api:user", "name": "James", "language": "en" }
}
```

No `payload`, no `alert_type`, no `location`.

## 1.4 Response contract

The receiver SHOULD respond within 10 seconds. Poracle's per-request timeout is operator-configurable and defaults to 10s.

### Success

Any of `200`, `201`, `202`, `204`. A `202` is the natural answer for a receiver that queues internally.

The body is optional. If the receiver wants Poracle to address later edits and deletes by the receiver's own identifier, it returns:

```json
{ "id": "abc123" }
```

`id` MUST match `^[A-Za-z0-9._~-]{1,128}$` — in particular it must not contain a colon, because Poracle composes its internal tracking key as `<destinationID>:<messageID>:<providerID>`. An `id` that fails validation is logged and ignored; Poracle then addresses the message by `message_id` alone. Any other keys in the response body are ignored.

A receiver that has no meaningful identifier of its own should return an empty body or `{}`. Poracle will then use `message_id` for edits and deletes, which is why `message_id` is guaranteed unique and stable.

### Failure

| Status | Meaning | Poracle behaviour |
|---|---|---|
| `429` | Rate limited | Honours `Retry-After` (seconds, or HTTP-date), then retries. Counts against the retry budget. |
| `404`, `410` | This destination no longer exists | Treated as permanent. Increments the destination's consecutive-failure counter; once the operator-configured threshold is reached (default 10) the human is disabled, exactly as when a Discord user blocks the bot. On `op: "delete"` only, `404` is treated as **success** — the message is already gone. |
| `401`, `403` | Auth rejected | Logged at ERROR with a distinct message naming the misconfigured secret, then dropped. Not retried and not failure-counted, because a wrong secret is an operator problem, not a destination problem — silently disabling every destination would be the wrong cure. |
| Other `4xx` | Bad request | Logged with the response body and dropped. No retry, no failure counting. This is the correct response for a payload the receiver cannot understand. |
| `5xx`, timeout, connection error | Transient | Retried up to `max_retries` times (default 3) with exponential backoff and jitter, then dropped. |

Poracle never blocks the alert pipeline on a slow receiver; a destination that is failing gets its messages dropped rather than backing up the queue.

## 1.5 Idempotency and ordering

- Poracle holds a per-destination mutex, so **at most one request is in flight per `destination.id` at a time**, requests for a single destination arrive in order, and a failed request's retries complete before the next operation to that destination. There is no ordering guarantee across different destinations. These guarantees make the rules below safe.
- **Network retries carry an identical body** (same `op`, same payload); `5xx`/`429`/connection errors are retried this way. Processing an identical retry twice must be harmless; deduplicating them is optional.
- **`op: "send"`** uses a fresh `message_id` each time, so a receiver may dedupe sends by `message_id`.
- **`op: "edit"`** replaces the message content in full; **apply every edit** (replacement is idempotent and edits arrive in order, so last-write-wins is correct). Do not key on `revision` — it is always `0` in the current implementation. When monotonic revisions ship, `(message_id, revision)` becomes the precise idempotency key.
- A `delete` may legitimately arrive for a `message_id` the receiver never saw (e.g. the original send failed after Poracle recorded it). Respond `404` or `200`; both are safe.

## 1.6 Message lifecycle and expiry

`expires_at` is **advisory and authoritative for the receiver's own housekeeping**. It is always present when the alert has a natural expiry, regardless of `lifecycle.clean`.

- `lifecycle.clean = false` — Poracle will never send a `delete`. The receiver may expire the message on `expires_at` if it wants to.
- `lifecycle.clean = true` — Poracle intends to send `op: "delete"` at `expires_at`. Poracle's tracker persists to disk across restarts, so this is reliable in normal operation, but **the receiver should still treat `expires_at` as the backstop**: a lost tracker file or a long outage means the delete never comes.

In short: act on `delete` when it arrives; fall back to `expires_at` when it doesn't.

## 1.7 Canonical payload schema

`payload` is produced by a Handlebars template. **Poracle proposes the canonical schema below and the receiver agrees to it**; that agreed schema is then shipped as a self-contained partner template pack (§1.7.5) which operators install as a single file. An operator may add extra keys, so the receiver MUST ignore keys it does not recognise. Removing or renaming a canonical key is a breaking change requiring re-agreement.

The schema is derived from Poracle's curated template-field registry, which is served live at `GET /api/dts/fields/{type}` and is the authoritative list of what is available. Every key below maps to exactly one registry field, so the mapping is auditable against a running server.

### 1.7.1 Sanitisation rules

The registry is filtered by these rules before becoming payload keys:

| Rule | Rationale |
|---|---|
| Exclude fields flagged `deprecated` | They are aliases kept for legacy Discord templates (`staticmap`, `mapurl`, `applemap`, `distime`). |
| Exclude fields flagged `rawWebhook` | Raw scanner field names (`individual_attack`, `move_1`, `gym_name`) duplicate a preferred field and follow the scanner's naming, not ours. |
| Exclude every emoji field | `*Emoji`, `flag`, `bearingEmoji`, `emoji` are per-platform *presentation* resolved from `emoji.json`. A JSON consumer wants the underlying value. |
| Exclude operator-instance map links | `rdmUrl`, `reactMapUrl`, `rocketMadUrl`, `diademUrl`, `wazeMapUrl`, `campfireUrl` point at whatever the operator happens to run. The receiver has the coordinates and can build its own links. |
| Prefer machine values over formatted strings | Where the registry offers both a unix timestamp and a locale-formatted string, the timestamp is the primary key (`despawn_at`) and the formatted string is kept alongside with a `_display` suffix. |
| Prefer English arrays for enumerable values | `types` uses the English `typeNameEng` array so the receiver can switch on it; the localised joined string is available as `types_display`. |
| Keys are `snake_case` | Matches the envelope. The template performs the rename, so `payload.pokemon_id` comes from `{{pokemonId}}`. |

### 1.7.2 Common block — present in every payload

Repeated inline in every entry — see §1.7.5 for why there is no partial. Note that latitude, longitude, `areas` and `expires_at` are **not** repeated here — they are envelope fields (§1.3). The address lives in the payload rather than the envelope because it is geocoded per language, and the payload is the language-dependent half.

| Payload key | Type | Registry field |
|---|---|---|
| `address.formatted` | string | `addr` |
| `address.street_number` | string | `streetNumber` |
| `address.street_name` | string | `streetName` |
| `address.neighbourhood` | string | `neighbourhood` |
| `address.suburb` | string | `suburb` |
| `address.city` | string | `city` |
| `address.state` | string | `state` |
| `address.postcode` | string | `zipcode` |
| `address.country` | string | `country` |
| `address.country_code` | string | `countryCode` |
| `address.intersection` | string | `intersection` |
| `icon_url` | string | `imgUrl` |
| `map_urls.google` | string | `googleMapUrl` |
| `map_urls.apple` | string | `appleMapUrl` |
| `static_map` | string | `staticMap` |
| `time_remaining.days` | int | `tthd` |
| `time_remaining.hours` | int | `tthh` |
| `time_remaining.minutes` | int | `tthm` |
| `time_remaining.seconds` | int | `tths` |
| `sun.sunrise_display` | string | `sunriseTime` |
| `sun.sunset_display` | string | `sunsetTime` |
| `sun.is_night` | bool | `isNight` |
| `sun.is_dawn` | bool | `isDawn` |
| `sun.is_dusk` | bool | `isDusk` |

| `distance_m` | int | `distance` |
| `bearing_deg` | int | `bearing` |

`distance_m` is the metres between the alert and the user's effective location anchor (per-rule override → profile location → account default); `bearing_deg` is the compass bearing from that anchor, `0` = north. Both are `0` for destinations tracking by area rather than distance.

`static_map` is the public URL of a pre-generated static map tile. It is a **payload** field, not an envelope field. Poracle generates a tile only if some template in the render batch references `{{staticMap}}`, so a partner pack that never wants tiles simply omits it from its templates and the key never appears in that payload.

> **Depends on a prerequisite fix.** Today the renderer only surfaces these two fields for pokemon alerts; every other type is group-rendered and the per-user values, though computed by the matcher, never reach the template. That is a pre-existing defect affecting Discord and Telegram equally, and is fixed separately before this work — see `2026-07-19-per-user-distance-bearing-design.md`. This schema assumes the fix has landed.

### 1.7.3 `pokemon` (and `monsterChanged`)

```json
{
  "pokemon_id": 25,
  "form_id": 0,
  "name": "Pikachu",
  "full_name": "Pikachu",
  "name_en": "Pikachu",
  "form_name": "",
  "costume_name": "",
  "gender": "Male",
  "generation": 1,
  "rarity": "Common",
  "size": "Normal",
  "shiny_possible": true,
  "encountered": true,
  "iv": 100, "atk": 15, "def": 15, "sta": 15,
  "cp": 842, "level": 25,
  "weight": "6.42", "height": "0.42",
  "base_stats": { "baseAttack": 112, "baseDefense": 96, "baseStamina": 111 },
  "types": ["Electric"],
  "types_display": "Electric",
  "color": "#F2D94E",
  "quick_move": "Thunder Shock", "quick_move_en": "Thunder Shock", "quick_move_id": 214,
  "charge_move": "Wild Charge",  "charge_move_en": "Wild Charge",  "charge_move_id": 102,
  "despawn_at": 1770001800,
  "despawn_display": "18:30:00",
  "despawn_verified": true,
  "weather": { "boosted": false, "boost_name": "Rain", "current": "Clear" },
  "pvp": { "great": [], "ultra": [], "little": [] },
  "weaknesses": [ { "multiplier": 1.6, "types": ["Ground"] } ],
  "pokestop_name": "Nearby Stop",
  "distance_m": 1240,
  "bearing_deg": 315,
  "address": { }, "icon_url": "", "map_urls": { }, "time_remaining": { }, "sun": { }
}
```

Registry sources: `pokemonId`, `formId`, `name`, `fullName`, `nameEng`, `formName`, `costumeName`, `genderName`, `generation`, `rarityName`, `sizeName`, `shinyPossible`, `encountered`, `iv`, `atk`, `def`, `sta`, `cp`, `level`, `weight`, `height`, `baseStats`, `typeNameEng`, `typeName`, `color`, `quickMoveName`, `quickMoveNameEng`, `quickMoveId`, `chargeMoveName`, `chargeMoveNameEng`, `chargeMoveId`, `despawnTimestamp`, `time`, `confirmedTime`, `boosted`, `boostWeatherName`, `gameWeatherName`, `pvpGreat`, `pvpUltra`, `pvpLittle`, `weaknessList`, `pokestopName`, `distance`, `bearing`.

PVP entries are sanitised to: `rank`, `cp`, `level`, `cap`, `capped`, `percentage`, `pokemon_id` (`pokemon`), `form_id` (`form`), `evolution`, `name`, `full_name`, `name_en`.

`monsterChanged` uses this same schema plus a `previous` object holding the pre-change values of `pokemon_id`, `form_id`, `full_name`, `gender`, `iv`, `cp`, `level` and `weather.boosted`, sourced from the renderer's `{{original.*}}` scope.

### 1.7.4 Remaining types

| Type | Payload keys → registry fields |
|---|---|
| `raid` | `level`←`level`, `level_name`←`levelName`, `boss.pokemon_id`←`pokemonId`, `boss.name`←`name`, `boss.full_name`←`fullName`, `boss.name_en`←`nameEng`, `boss.form_name`←`formName`, `boss.costume_name`←`costumeName`, `boss.types`←`typeNameEng`, `boss.color`←`color`, `boss.cp20`←`cp20`, `boss.cp25`←`cp25`, `boss.quick_move`←`quickMoveName`, `boss.charge_move`←`chargeMoveName`, `boss.shiny_possible`←`shinyPossible`, `boss.weaknesses`←`weaknessList`, `gym.name`←`gymName`, `gym.image_url`←`gymUrl`, `gym.team`←`teamName`, `gym.ex`←`ex`, `hatch_at`←`hatchTimestamp`, `end_at`←`endTimestamp`, `hatch_display`←`hatchTime`, `end_display`←`time`, `rsvps[]`←`rsvps` |
| `egg` | `level`, `level_name`, `gym.*` (as raid), `hatch_at`←`hatchTimestamp`, `end_at`←`endTimestamp`, `hatch_display`←`time`, `rsvps[]` |
| `rsvpChanges` | Identical to `raid`, with updated `rsvps[]`. Sent only when an `rsvpChanges` template exists; otherwise the full `raid` payload is sent. |
| `quest` | `pokestop_name`, `quest`←`questString`, `reward`←`rewardString`, `conditions`←`conditionString`, `conditions_en`←`conditionStringEng`, `condition_list[]`←`conditionList`, `reward_detail.{dust_amount, item_name, item_amount, monster_name, monster_full_name, energy_amount, energy_monster_name, candy_amount, candy_monster_name, xl_candy_amount, xl_candy_monster_name, shiny}` |
| `invasion` | `pokestop_name`, `grunt_name`←`gruntName`, `grunt_type`←`gruntType`, `grunt_type_name`←`gruntTypeName`, `grunt_type_id`←`gruntTypeId`, `display_type_id`←`displayTypeId`, `gender`←`genderName`, `color`←`gruntTypeColor`, `expires_display`←`time`, `rewards`←`gruntRewardsList`, `lineup[]`←`gruntLineupList` |
| `incident` | `pokestop_name`, `pokestop_id`←`pokestopId`, `pokestop_image_url`←`pokestopUrl`, `display_type`←`displayType`, `incident_type_name`←`incidentTypeName`, `color`, `expires_display`←`disappearTime`. For Showcase incidents, additionally `showcase.{present, total_entries, last_update, focus_type, focus_category, focus_name, entries[]}` ← `showcasePresent`, `showcaseTotalEntries`, `showcaseLastUpdate`, `showcaseFocusType`, `showcaseFocusCategory`, `showcaseFocusName`, `showcase` |
| `lure` | `pokestop_name`, `lure_type_id`←`lureTypeId`, `lure_type_name`←`lureTypeName`, `color`←`lureTypeColor`, `expires_display`←`time` |
| `nest` | `nest_name`←`nest_name`, `pokemon_id`←`pokemonId`, `name`, `full_name`←`fullName`, `name_en`←`nameEng`, `form_name`←`formName`, `types`←`typeNameEng`, `color`, `spawn_avg`←`pokemonSpawnAvg`, `pokemon_count`←`pokemonCount`, `shiny_possible`←`shinyPossible` |
| `gym` | `gym_name`←`gymName`, `image_url`←`gymUrl`, `team`←`teamName`, `old_team`←`oldTeamName`, `slots_available`←`slotsAvailable`, `old_slots_available`←`oldSlotsAvailable`, `in_battle`←`inBattle`, `ex`, `color`←`gymColor`, `previous_control_name`←`previousControlName` |
| `fort` | `fort_id`←`id`, `fort_type`←`fortType`, `name`, `change_type`←`changeType`, `change_type_text`←`changeTypeText`, `edit_types[]`←`editTypesList`, `is_edit_name`, `is_edit_location`, `is_edit_image`, `is_empty`←`isEmpty`, `new_name`, `old_name`, `new_description`, `old_description`, `new_image_url`, `old_image_url` |
| `maxbattle` | `pokemon_id`←`pokemonId`, `name`, `full_name`←`fullName`, `costume_name`←`costumeName`, `level`, `gmax`, `pokestop_name`, `types`←`typeNameEng`, `color`, `quick_move`←`quickMoveName`, `charge_move`←`chargeMoveName`, `end_at`←`endTimestamp`, `end_display`←`time`, `total_stationed`←`totalStationedPokemon`, `total_stationed_gmax`←`totalStationedGmax` |
| `weatherchange` | `weather`←`weatherName`, `old_weather`←`oldWeatherName`, `change_in.{hours, minutes, seconds}`←`weatherTthh`/`weatherTthm`/`weatherTths`, `affected_pokemon[]`←`enrichedActivePokemons` |
| `questSummary` | A digest, not a single alert. Top-level from `BuildQuestSummaryView` (`dts/quest_summary.go:106`): `reward_type`←`rewardType`, `reward_id`←`reward`, `reward_form`←`rewardForm`, `reward_name`←`rewardName`, `count`←`count` (total stops across the whole reward group, not just this chunk), `chunk`←`chunk`, `chunks`←`chunks` (1-indexed pagination for oversized groups; both `1` when unsplit). `entries[]`←`quests`, each entry a per-pokestop quest view carrying `pokestop_name`, `latitude`, `longitude` plus the same reward/quest keys as the `quest` type above. The envelope's `location` and the payload's `static_map` refer to the multi-pin overview map for this chunk, whose pins are exactly `entries[]`. No per-user `distance_m`/`bearing_deg` — a digest spans many locations. |

### 1.7.5 The partner template pack

Each partner integration is distributed as **one self-contained file**. The first is Diadem, shipping sixteen `[[entry]]` blocks — one per `alert_type` in §1.7, plus a dedicated `showcase` entry that renders Showcase-flavoured `incident` alerts (see §1.7.4):

```
fallbacks/dts/diadem.toml     # shipped with Poracle, readonly
config/dts/diadem.toml        # operator's copy, overrides the shipped one
```

**Self-contained means no partials.** DTS partials are loaded from a single global `config/partials.json`, so a partner pack could not ship its own without merging into a file the operator also owns. The common block of §1.7.2 is therefore repeated verbatim in every entry. That is the deliberate cost of a single drop-in artefact: once the schema is agreed, the file can be handed to any operator, or shipped in `fallbacks/`, with no other installation step and no dependency on anything else in their config.

**Partner identity is the template ID.** Entries use `platform = "api"` with `id = "diadem"`. The operator points api destinations at the pack with `[api_delivery] template = "diadem"`; a second partner ships `id = "partnerB"` and coexists in the same install without collision. Because DTS selection already keys on `(type, platform, id, language)`, this needs no new mechanism — and a per-rule `template:diadem` still works for anyone who wants it per tracking rule.

**Overriding.** Shipped packs are readonly. Template selection runs user entries first and falls back to readonly entries only if nothing matched, so an operator who needs an extra field copies `fallbacks/dts/diadem.toml` to `config/dts/` and edits it there — the same pattern the bundled help and info templates already use.

**Validated by an automated conformance test.** `processor/cmd/processor/api_pack_conformance_test.go` (§2.8) renders every entry in the shipped Diadem pack through real enrichment — not stubs — against the bundled `fallbacks/testdata.json` fixtures, and asserts each rendered payload is valid JSON with a curated set of required keys per type present and **non-empty**. The required-key set deliberately targets the fields most prone to silent breakage (identity, location, moves, rsvps, boss/gym names) — the non-empty assertion is what catches a template field name that no longer matches enrichment output. (`nest` has no bundled fixture and is not covered by this test; the Showcase editor-preview path has a separate known gap — real Showcase delivery is unaffected.) Because the common block of §1.7.2 is duplicated across all sixteen entries with no partial, this test is also what catches drift between those copies as the schema or enrichment evolves — it is not optional.

**Authoring rule — guard every numeric interpolation.** A field with no value renders as an empty string, so a bare `"cp": {{cp}}` produces `"cp": ` and invalid JSON. The renderer detects this (`json.Valid`) and substitutes a fallback message, meaning the receiver silently gets the wrong body. Numeric fields must be written `"cp": {{#if cp}}{{cp}}{{else}}0{{/if}}` or equivalent. This matters most for `iv`/`cp`/`level` on unencountered pokemon and for `distance_m`/`bearing_deg`.

A complete entry, showing the inline common block:

```toml
# fallbacks/dts/diadem.toml

[[entry]]
type = "pokemon"
platform = "api"
id = "diadem"
template = """
{
  "pokemon_id": {{pokemonId}},
  "form_id": {{#if formId}}{{formId}}{{else}}0{{/if}},
  "name": "{{name}}",
  "full_name": "{{fullName}}",
  "name_en": "{{nameEng}}",
  "form_name": "{{formName}}",
  "costume_name": "{{costumeName}}",
  "gender": "{{genderName}}",
  "shiny_possible": {{#if shinyPossible}}true{{else}}false{{/if}},
  "encountered": {{#if encountered}}true{{else}}false{{/if}},
  "iv": {{#if iv}}{{iv}}{{else}}null{{/if}},
  "cp": {{#if cp}}{{cp}}{{else}}null{{/if}},
  "level": {{#if level}}{{level}}{{else}}null{{/if}},
  "types": [{{#each typeNameEng}}"{{this}}"{{#unless @last}},{{/unless}}{{/each}}],
  "color": "{{color}}",
  "despawn_at": {{despawnTimestamp}},
  "despawn_display": "{{time}}",
  "despawn_verified": {{#if confirmedTime}}true{{else}}false{{/if}},
  "distance_m": {{#if distance}}{{distance}}{{else}}0{{/if}},
  "bearing_deg": {{#if bearing}}{{bearing}}{{else}}0{{/if}},
  "address": {
    "formatted": "{{addr}}",
    "street_number": "{{streetNumber}}",
    "street_name": "{{streetName}}",
    "neighbourhood": "{{neighbourhood}}",
    "suburb": "{{suburb}}",
    "city": "{{city}}",
    "state": "{{state}}",
    "postcode": "{{zipcode}}",
    "country": "{{country}}",
    "country_code": "{{countryCode}}",
    "intersection": "{{intersection}}"
  },
  "icon_url": "{{imgUrl}}",
  "map_urls": { "google": "{{googleMapUrl}}", "apple": "{{appleMapUrl}}" },
  "time_remaining": {
    "days": {{#if tthd}}{{tthd}}{{else}}0{{/if}},
    "hours": {{#if tthh}}{{tthh}}{{else}}0{{/if}},
    "minutes": {{#if tthm}}{{tthm}}{{else}}0{{/if}},
    "seconds": {{#if tths}}{{tths}}{{else}}0{{/if}}
  },
  "sun": {
    "sunrise_display": "{{sunriseTime}}",
    "sunset_display": "{{sunsetTime}}",
    "is_night": {{#if isNight}}true{{else}}false{{/if}},
    "is_dawn": {{#if isDawn}}true{{else}}false{{/if}},
    "is_dusk": {{#if isDusk}}true{{else}}false{{/if}}
  }
}
"""
```

Unencountered pokemon emit `null` for `iv`/`cp`/`level` rather than `0`, so the receiver can distinguish "not scanned" from "genuinely zero".

### 1.7.6 Template selection

A destination only receives types it has tracking rules for.

If no `api` entry exists for a type, template selection falls back only to entries with `platform = ""` (the platform wildcard). **An `api` destination will never be sent a Discord- or Telegram-specific template**, so it can never receive Discord embed markup or Telegram MarkdownV2. If neither an `api` nor a wildcard entry exists for the type, no message is sent.

## 1.8 Managing destinations

There is no chat-command surface for `api` destinations. The third party creates and configures them over the existing v2 REST API using the same `X-Poracle-Secret` header.

```
POST /api/v2/humans
{ "id": "u-42", "name": "James", "type": "api:user", "language": "en" }

POST /api/v2/humans/u-42/setLocation/51.5074/-0.1278
POST /api/v2/humans/u-42/areas         { "areas": ["london"] }
POST /api/v2/humans/u-42/tracking/pokemon
      [ { "pokemon_id": 25, "min_iv": 90, "distance": 5000, "clean": 1 } ]

GET  /api/v2/humans/u-42/tracking      # full snapshot: human, rules, profiles, locations, mutes
```

`id` is chosen by the third party and must match `^[A-Za-z0-9._~-]{1,128}$`.

Discord admins see `api` destinations in `!userlist api` and can admin-disable them, but cannot run tracking commands against them.

## 1.9 Worked example

Alert fires for a 100 IV Pikachu matching a rule with the clean bit set.

**1 — send**

```http
POST /poracle HTTP/1.1
Host: third.party
X-Poracle-Secret: s3cr3t
X-Poracle-Op: send
X-Poracle-Message-Id: 7c9e6a1f-4b2d-4c8a-9f3e-1a2b3c4d5e6f
Content-Type: application/json

{"version":1,"op":"send","message_id":"7c9e6a1f-4b2d-4c8a-9f3e-1a2b3c4d5e6f","revision":0,
 "sent_at":1770000000,"alert_type":"pokemon","template_id":"default",
 "destination":{"id":"u-42","type":"api:user","name":"James","language":"en"},
 "location":{"lat":51.5074,"lon":-0.1278},
 "expires_at":1770001800,
 "lifecycle":{"clean":true,"editable":false},
 "tracking_uids":[45],
 "areas":["london","city"],
 "payload":{"title":"Pikachu 100%","iv":100,"cp":842,"level":25}}
```

```http
HTTP/1.1 202 Accepted
Content-Type: application/json

{"id":"abc123"}
```

**2 — delete, 30 minutes later at despawn**

```http
POST /poracle HTTP/1.1
X-Poracle-Op: delete
X-Poracle-Message-Id: 7c9e6a1f-4b2d-4c8a-9f3e-1a2b3c4d5e6f

{"version":1,"op":"delete","message_id":"7c9e6a1f-4b2d-4c8a-9f3e-1a2b3c4d5e6f","revision":0,
 "provider_message_id":"abc123","sent_at":1770001800,
 "destination":{"id":"u-42","type":"api:user","name":"James","language":"en"}}
```

```http
HTTP/1.1 204 No Content
```

## 1.10 Receiver implementation checklist

- [ ] One `POST` route, dispatching on `op`.
- [ ] Constant-time comparison of the secret; reject with `401`.
- [ ] Reject `version` values you do not understand.
- [ ] Apply every `edit` (full replacement); dedupe only identical network retries. Do not key on `revision` (always `0` today).
- [ ] Store the mapping `message_id → your record` so `edit` and `delete` resolve even when you return no `id`.
- [ ] Return `{"id": "..."}` only if the value is colon-free and matches `^[A-Za-z0-9._~-]{1,128}$`.
- [ ] Return `202` fast; do rendering and fan-out asynchronously.
- [ ] Return `404`/`410` when a destination is genuinely gone, so Poracle stops sending. Do **not** use `404` for transient backend failures — that will disable the destination.
- [ ] Expire messages on `expires_at` as a backstop, even when `lifecycle.clean` is `true`.
- [ ] Treat `delete` for an unknown `message_id` as success.
- [ ] Ignore unrecognised envelope keys and `X-Poracle-*` headers.

---

# Part 2 — PoracleNG Implementation

## 2.1 Configuration

```toml
[api_delivery]
enabled       = true
endpoint      = "https://third.party/poracle"
secret        = "s3cr3t"
secret_header = "X-Poracle-Secret"   # e.g. "Authorization"
secret_prefix = ""                    # e.g. "Bearer "
template      = "diadem"              # DTS template id for the partner pack
timeout_ms    = 10000
max_retries   = 3
concurrency   = 4
log_only      = false                 # dry-run: log the envelope instead of POSTing

# Reserved. Named endpoints are NOT implemented in v1, but the type syntax
# `api:<name>:user` and this config shape are held open so a second receiver
# can be added without a wire-format or schema change.
# [api_delivery.endpoints.partnerB]
# endpoint = "..."
# secret   = "..."
```

`enabled = true` with an empty `endpoint` is a startup validation error. When `enabled = false`, no `api` sender is registered and any `api:*` job is dropped by the queue's existing "no sender for platform" path.

## 2.2 New code

**`internal/delivery/api.go` — `APISender`** implementing `delivery.Sender`:

| Method | Behaviour |
|---|---|
| `Send` | Builds the `op:"send"` envelope, POSTs, returns `SentMessage{ID: "<destID>:<messageID>[:<providerID>]"}`. |
| `Edit` | Parses the SentID, POSTs `op:"edit"` with the new `job.Message` as `payload`. |
| `Delete` | Parses the SentID, POSTs `op:"delete"`. `404` is success. |
| `Platform` | `"api"`. |
| `WaitForRateLimit` | Blocks until any deadline learned from the last `429` has passed. Called before the semaphore is acquired, per the interface contract. |

Envelope construction reads from the existing `delivery.Job`: `Target`, `Type`, `Name`, `Language`, `Lat`, `Lon`, `TTH` (→ `expires_at`), `Clean` (→ `lifecycle` via `db.IsClean`/`db.IsEdit`), `ReplyToID` (→ `in_reply_to`), `MsgType` (→ `alert_type`), `Message` (→ `payload`), plus `TemplateID` (→ `template_id`), `TrackingUIDs` (→ `tracking_uids`), and `Areas` (→ `areas`).

`revision` is hardcoded to `0` on every `send`, `edit`, and `delete` — the reserved-field decision (§1.3) has no code path that increments it.

`TemplateID`, `TrackingUIDs`, and `Areas` are populated in `cmd/processor/render.go` from the same values `buildSnapshot` already assembles on `RenderJob`. The static map URL needs no equivalent `Job` field: it flows through the render pipeline as an ordinary template variable (`{{staticMap}}`) into `payload.static_map` (§1.7.2), the same as `icon_url` or any other rendered field.

Registered in `NewDispatcher` when `cfg.APIEndpoint != ""`, alongside the existing Discord and Telegram registrations.

## 2.3 Existing enumeration points to extend

Derived from a full audit of platform/type branching.

| # | Location | Change |
|---|---|---|
| 1 | `delivery/queue.go` — `semaphoreFor`, `counterFor` | Both currently `default:` to the **Discord** semaphore. Add an `apiSem` + `apiInFlight` + `QueueConfig.ConcurrentAPI` + `Dispatcher.APIDepth()`, and an explicit `case "api"`. Without this, api traffic silently throttles behind Discord. |
| 2 | `ratelimit/ratelimit.go` — `isUserType` | Add `api:user` so it draws `dm_limit`, not `channel_limit`. |
| 3 | `cmd/processor/render.go:431` `snapshotTargetType` **and** `dts/renderer.go:516` `deliveryTargetType` | Byte-identical duplicate switches. Extract one `delivery.TargetClass(type)` used by both, add `api:user`→`dm` / `api:channel`→`channel`. This also closes the pre-existing `telegram:topic` gap, where both switches return `""`. |
| 4 | `dts/templates.go:792` metadata pre-seed, `:938` load-log switch | Add `"api"` so the config editor lists the platform and startup logs count its templates. |
| 5 | `bot/commands/userlist.go:39` | Add `case "api"` to the platform filter. |
| 6 | `bot/commands/broadcast.go:110` | Target expansion is `t.Platform == "discord" \|\| ""`, which sweeps `api:user` into the Discord bucket. Make the platform match explicit and exclude or handle `api`. |
| 7 | `store/human.go` (new) | Add `ValidHumanTypes` allow-list and enforce it in v1 `POST /api/humans` and v2 `POST /api/v2/humans`, returning `422`. See §2.5. |
| 8 | `config/config.go`, `config/toml_encode.go:25`, `api/config_values.go:274` | New `[api_delivery]` section, section ordering, and the `api_delivery_` prefix nesting for the config editor. |
| 9 | `api/testdata/openapi.golden.json` | Regenerate; the human `type` doc strings enumerate the known types. |
| 10 | `dts/templates.go:194` | The `fallbacks/dts/` walker matches `.json` only. Extend to `.toml` through the existing TOML loader so partner packs ship as readable TOML. |
| 11 | `dts/renderer.go` `ResolveTemplate` | Takes a platform argument; an empty rule template resolves to `[api_delivery] template` for `api` destinations instead of `[general] default_template_name`. Callers in `cmd/processor/tilemode.go:34` and the render paths update accordingly. |

## 2.4 Existing behaviour that already does the right thing

No change needed — verified against the audit:

- **`PlatformFromType`** splits generically on `:`; only `"webhook"` is special-cased. `api:user` → `"api"` falls out.
- **`canUploadInline`** returns `false` for any non-Discord platform, so api destinations land in tile mode `URL`. And because `tileMode` first tests `UsesTile(type, platform, tmplID, lang)` per destination, an api template with no `{{staticMap}}` reference contributes nothing to the decision and **no tile is generated on its account**.
- **Template selection** is already keyed `(type, platform, id, language)` with the two-pass user-then-fallback chain. `platform = "api"` works unchanged, including per-language variants and `template:X` on tracking rules.
- **Buttons** — `renderer.go:442` gates injection on `platform == "discord"`, so api destinations get none, matching the v1 decision to omit them.
- **Matching** — `internal/matching` never branches on platform. Areas, distance, profiles, `blocked_alerts`, mutes and strict area restriction all apply as-is.
- **Mutes** — `filterMuted` is platform-agnostic; the v2 mutes API works against `api` humans unchanged.
- **Failure handling and auto-disable** — `FairQueue`'s `failCounts` / `failThreshold` / `onDisabled` path is platform-agnostic and picks up `PermanentError` from any sender.
- **`recipientIsAdmin`**, `api/roles.go`, `api/v2_roles.go`, `config_resolve.go isPlatformVerifiable`, Discord and Telegram reconciliation loops — all already gate on specific Discord/Telegram types and correctly exclude `api`.
- **Emoji** — `emoji.Lookup` resolves `custom["api"][key]` then falls back to `defaults[key]`. An operator wanting emoji-free JSON adds an `"api"` section to `emoji.json` with empty strings; otherwise the unicode defaults apply.

## 2.5 Human type validation

Today `humans.type` is a free-form `varchar(255)` with no validation on either the v1 or v2 create endpoint — `internal/api/v2_humans.go:120` and `internal/api/humans.go:616` only default an empty value to `discord:user`. A typo such as `api:users` produces a human that matches normally and then has every job silently dropped at `queue.go:180` with `no sender for platform`.

Since the third party will be creating humans programmatically, this design adds:

- `store.ValidHumanTypes` — the canonical set: `discord:user`, `discord:channel`, `discord:thread`, `telegram:user`, `telegram:group`, `telegram:channel`, `telegram:topic`, `webhook`, `api:user`, `api:channel`.
- A `store.ValidateHumanType(t string) error`, called from both create endpoints → `422` on failure.
- ID charset validation `^[A-Za-z0-9._~-]{1,128}$` for `api:*` types only, because the SentID composition `<destID>:<messageID>:<providerID>` requires a colon-free destination ID. Existing platforms keep their current ID handling.

This is a targeted fix to a real gap the new destination type exposes, not general-purpose refactoring. It is additive: every currently-valid type stays valid.

## 2.6 Message identity and the tracker

`MessageTracker.SentID` for `api` is `<destinationID>:<messageID>[:<providerID>]`, deliberately mirroring Discord's `<rateLimitKey>:<messageID>`.

All three components are guaranteed colon-free — `destinationID` by §2.5 validation, `messageID` by the UUID hex-and-hyphen alphabet, `providerID` by the §1.4 response validation. This means `splitSentID` (which splits on the *last* colon) and `ExtractMessageIDForSnapshot` keep working without a platform-specific branch: they yield the provider ID when one exists and the Poracle message ID otherwise, which is exactly what an addressing key should be.

Embedding the destination ID is what lets `Sender.Delete(ctx, sentID)` — which receives no `Job` and therefore no target — reconstruct the `destination` block for the delete envelope.

Reply chaining reuses the existing machinery unchanged: `Job.ReplyKey` is already set per alert type (`encounterID` for pokemon, `raidlife:{gymID}:{raidEnd}` for raid/egg), `FairQueue` stamps `Job.ReplyToID` from `MessageTracker.LookupReply(replyKey, target)`, and `APISender` parses that SentID and emits its addressing half as `in_reply_to`.

## 2.7 Rendering

`fallbacks/dts/diadem.toml` ships one `[[entry]]` per alert type with `platform = "api"` and `id = "diadem"`. This satisfies "one file, consistent body" while keeping the standard selection chain intact — per-language variants and `template:X` on a tracking rule continue to work.

These templates are **the canonical payload schema of §1.7 in executable form** — authored by us and agreed with the receiver, not left to the operator.

**Shipped as a readonly fallback, not a config file.** `LoadTemplates` already walks `fallbacks/dts/` and marks everything readonly (`templates.go:192`), and the two-pass selection prefers user entries, so an operator who needs an extra field copies the file into `config/dts/` and edits it there. This is the same distribution model the bundled help and info templates already use, and it means a Poracle upgrade ships schema updates automatically to operators who haven't customised.

**One loader change needed:** the `fallbacks/dts/` walker accepts `.json` only (`templates.go:194`), while `config/dts/` takes both `.json` and `.toml`. Extend the fallback walker to `.toml` via the existing TOML loader, so the partner pack can be authored with readable `"""` multi-line bodies and so the copy-to-`config/dts/` path is format-symmetric.

**No partials.** DTS partials load from a single global `config/partials.json`, so a partner pack cannot ship one without merging into a file the operator also owns. The common block of §1.7.2 is repeated verbatim in all sixteen entries. The schema conformance test in §2.8 is what catches drift between the copies — that is the mitigation for the duplication, and it is why the test is not optional.

**Partner identity is the template ID.** `id = "diadem"`, selected via `[api_delivery] template = "diadem"`. A second partner ships `id = "partnerB"` and coexists. This reuses the existing `(type, platform, id, language)` selection key rather than inventing a mechanism, and composes with the reserved named-endpoint syntax of §2.1.

**Template resolution needs a per-platform default.** Tracking rules store an empty template and the renderer resolves it to `[general] default_template_name`. That default names a Discord template. `ResolveTemplate` gains a platform argument so `api` destinations resolve an empty rule template to `[api_delivery] template` instead.

**Field selection is derived from `internal/api/dts_fields.go`**, the curated registry served at `GET /api/dts/fields/{type}`, filtered by the §1.7.1 rules. Building the templates by hand from that registry — rather than emitting it programmatically — keeps the wire schema stable when the registry gains fields.

**Numeric guards are mandatory.** An absent field renders as an empty string, so a bare `"cp": {{cp}}` yields invalid JSON; the renderer's `json.Valid` check then substitutes the fallback message and the receiver silently gets the wrong body. Every numeric interpolation in the pack is guarded (§1.7.5).

One behavioural exception: **`ping` is not appended for platform `api`.** A Discord mention string is meaningless in a JSON payload and would corrupt it. The renderer's ping-append step is skipped when the resolved platform is `api`.

Render output that is not valid JSON is dropped with a warning, matching current Discord behaviour.

## 2.8 Testing

- Unit tests for `APISender`: envelope construction per op, `revision` sequencing across send → edit → edit → delete, SentID composition and parsing, provider-ID validation and rejection, status-code classification (2xx/429/401/403/404/410/other 4xx/5xx), `Retry-After` parsing, retry-and-backoff, `404`-on-delete-is-success, `log_only` mode.
- Queue tests: `api` gets its own semaphore and does not consume Discord's; `PermanentError` from `APISender` drives the existing auto-disable path.
- Rate-limit test: `api:user` draws `dm_limit`, `api:channel` draws `channel_limit`.
- Tile-mode test: an api-only batch whose template omits `{{staticMap}}` yields `TileModeSkip`; a mixed batch with a Discord template that uses it yields a URL and the api payload carries `static_map`.
- Type-validation tests on both create endpoints.
- **Payload schema conformance**: a golden test that renders every entry in the shipped partner pack against the existing `testdata.json` fixtures and asserts the output validates against a JSON Schema generated from §1.7. This does three jobs: it stops a future enrichment change from silently breaking the receiver's contract, it fails loudly if a canonical key is renamed or dropped, and — because the common block is duplicated across sixteen entries with no partial — it is the only thing that catches drift between those copies. Implemented as `TestAPIPackConformance` in `processor/cmd/processor/api_pack_conformance_test.go`.
- A rendering test per entry asserting valid JSON when every optional field is absent (unencountered pokemon, no geocoding, no PVP data), pinning the numeric-guard requirement of §1.7.5. An unguarded interpolation is invisible until a sparse webhook arrives in production.
- A test that `[api_delivery] template` is what an empty rule template resolves to for `api` destinations, and that `[general] default_template_name` still wins for Discord and Telegram.
- Fallback loader test: a `.toml` file in `fallbacks/dts/` loads, is marked readonly, and is overridden by a same-`(type, platform, id, language)` entry in `config/dts/`.
- Regenerate `api/testdata/openapi.golden.json`.
- End-to-end: `!poracle-test pokemon,<id>` against an `api:user` destination, with `log_only = true`, produces a well-formed envelope in the log.

## 2.9 Explicitly out of scope for v1

| Item | Reason |
|---|---|
| Buttons and any click-back protocol | Requires an inbound endpoint and a session model. The receiver can build equivalent UI and act through the existing v2 API (mutes, tracking deletion) using `tracking_uids` and `destination.id`. `buttons` is reserved in the envelope schema. |
| HMAC body signature | The shared secret over HTTPS is sufficient for v1. `X-Poracle-Signature` / `X-Poracle-Timestamp` are reserved header names so this is additive later. |
| Multiple named receivers | One endpoint now. The `api:<name>:user` type syntax and `[api_delivery.endpoints.<name>]` config shape are reserved so a second receiver needs no wire-format or schema change. |
| Batched delivery | The `FairQueue` per-destination mutex is what guarantees ordering; batching would break it. Receivers batch internally. |
| Chat commands for `api` destinations | Managed entirely through the v2 API by design. |
| Inline image bytes | URL only (`payload.static_map`). Adding a sibling `payload.static_map_data` with raw bytes later is additive. |
