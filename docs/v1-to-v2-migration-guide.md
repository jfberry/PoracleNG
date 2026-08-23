# v1 → v2 API Migration Guide

Audience: third-party client authors (PoracleWeb, ReactMap, custom integrations) moving off the
frozen v1 tracking/humans/profiles endpoints onto `/api/v2`.

**v1 status:** frozen and deprecated-but-supported — it keeps working exactly as today, with no
sunset date yet. New tracking types (starting with `incident`) and new capabilities (saved-location
update) ship **only** on v2.

**The live contract:** the OpenAPI 3.1 spec at `GET /openapi.json` and the interactive docs at
`GET /docs` (both public, no secret) describe every v2 endpoint and field, including the
omit-to-wildcard semantics summarised below. This guide is the map from old to new; the spec is the
source of truth. Design rationale: [`v2-api-design.md`](v2-api-design.md).

**Auth is unchanged:** send `X-Poracle-Secret` exactly as for v1.

## The big behavioural differences

1. **Strict, not lenient.** v2 rejects unknown body fields and unknown query parameters with `422`.
   No `"1"`-for-`1` or `true`-for-`1` coercion — send the documented type.
2. **Errors are RFC 9457 `application/problem+json`** (`{title, status, detail, errors[]}`), not
   v1's ad-hoc `{status:"error", message}` shapes.
3. **Omit means "any".** Never send magic sentinel numbers (`9000`, `-1`, `4096`, …) to mean
   "no constraint" — omit the field instead. Symmetrically, reads return `null` for any filter at
   its wildcard/default. GET → PUT round-trips unchanged.
4. **Create bodies are always an array** of rule objects (a single rule is a one-element array).
   v1's single-object-or-array flexibility is gone.
5. **`?silent=true`** on mutations replaces v1's `silent` + `suppressMessage` pair.
   **`?include_descriptions=true`** works uniformly on every tracking read *and* mutation.

## Endpoint mapping — tracking

`{type}` ∈ `pokemon, raid, egg, quest, invasion, incident (NEW), lure, nest, gym, fort, maxbattle`.

| v1 | v2 | Notes |
|---|---|---|
| `GET /api/tracking/{type}/{id}` | `GET /api/v2/humans/{id}/tracking/{type}` | `?profile=` defaults to the active profile. Returns `{rules:[…]}` |
| `POST /api/tracking/{type}/{id}` | `POST /api/v2/humans/{id}/tracking/{type}` | Body is an **array** of rules. Returns `{created, updated, unchanged}` |
| — | `GET /api/v2/humans/{id}/tracking/{type}/{uid}` | NEW — fetch one rule |
| — | `PUT /api/v2/humans/{id}/tracking/{type}/{uid}` | NEW — full replace (omitted fields reset to defaults; no PATCH) |
| `DELETE /api/tracking/{type}/{id}/byUid/{uid}` | `DELETE /api/v2/humans/{id}/tracking/{type}/{uid}` | Returns `{deleted:[…]}` |
| `POST /api/tracking/{type}/{id}/delete` (body `[uids]`) | `DELETE /api/v2/humans/{id}/tracking/{type}?uid=1,2,3` | Bulk delete via query |
| `GET /api/tracking/all/{id}` | `GET /api/v2/humans/{id}/tracking` | Full snapshot: `{human, tracking:{<type>:[…]}, profiles, locations, summaries}` |
| `GET /api/tracking/allProfiles/{id}` | `GET /api/v2/humans/{id}/tracking?all_profiles=true` | |
| `GET /api/tracking/pokemon/refresh` | `GET`/`POST /api/reload` | Reload alias; the documented reload endpoint |

Item operations are scoped by `(human, uid)` exactly like v1's `byUid` — you cannot touch a uid
that doesn't belong to the addressed human.

## Endpoint mapping — humans

| v1 | v2 | Notes |
|---|---|---|
| `POST /api/humans` | `POST /api/v2/humans` | Typed body |
| `GET /api/humans/one/{id}` | `GET /api/v2/humans/{id}` | |
| `GET /api/humans/{id}` (available areas) | `GET /api/v2/humans/{id}/areas` | |
| `POST /api/humans/{id}/start` | `POST /api/v2/humans/{id}/enable` | No body |
| `POST /api/humans/{id}/stop` | `POST /api/v2/humans/{id}/disable` | No body |
| `POST /api/humans/{id}/adminDisabled` | `POST /api/v2/humans/{id}/admin-disable` | Body `{disabled: bool}` |
| `POST /api/humans/{id}/language` | `POST /api/v2/humans/{id}/language` | Body `{language: string}`, validated against available locales |
| `POST /api/humans/{id}/setLocation/{lat}/{lon}` | `POST /api/v2/humans/{id}/location` | Body `{lat, lon}` floats |
| `GET /api/humans/{id}/checkLocation/{lat}/{lon}` | `GET /api/v2/humans/{id}/check-location?lat=&lon=` | |
| `POST /api/humans/{id}/setAreas` | `POST /api/v2/humans/{id}/areas` | Body `{areas: []string}` |
| `POST /api/humans/{id}/switchProfile/{n}` | `POST /api/v2/humans/{id}/profile` | Body `{profile_no: int}` |
| `GET /api/humans/{id}/locations` | `GET /api/v2/humans/{id}/locations` | |
| `GET /api/humans/{id}/locations/{label}` | `GET /api/v2/humans/{id}/locations/{label}` | |
| `POST /api/humans/{id}/locations/add` | `POST /api/v2/humans/{id}/locations` | Body `{label, lat, lon}` |
| — | `PUT /api/v2/humans/{id}/locations/{label}` | **NEW** — update coords (v1 forced delete + re-add). Body `{lat, lon}` |
| `POST /api/humans/{id}/locations/{label}/delete` | `DELETE /api/v2/humans/{id}/locations/{label}` | `409` if referenced by a rule's `override_location_label` |
| `GET /api/humans/{id}/roles` | `GET /api/v2/humans/{id}/roles` | |
| `POST /api/humans/{id}/roles/add/{roleId}` | `POST /api/v2/humans/{id}/roles/{roleId}` | |
| `POST /api/humans/{id}/roles/remove/{roleId}` | `DELETE /api/v2/humans/{id}/roles/{roleId}` | |
| `GET /api/humans/{id}/getAdministrationRoles` | `GET /api/v2/humans/{id}/admin-roles` | |

## Endpoint mapping — profiles

Profiles are a sub-resource of the human in v2.

| v1 | v2 | Notes |
|---|---|---|
| `GET /api/profiles/{id}` | `GET /api/v2/humans/{id}/profiles` | v2 returns `active_hours` as a **typed array**, not a JSON string |
| `POST /api/profiles/{id}/add` | `POST /api/v2/humans/{id}/profiles` | |
| `POST /api/profiles/{id}/update` | `PATCH /api/v2/humans/{id}/profiles/{profile_no}` | Updates `active_hours` (typed, validated) |
| `DELETE /api/profiles/{id}/byProfileNo/{n}` | `DELETE /api/v2/humans/{id}/profiles/{n}` | |
| `POST /api/profiles/{id}/copy/{from}/{to}` | `POST /api/v2/humans/{id}/profiles/{to}/copy` | Source in body: `{from_profile: int}`; target is the path `{profile_no}` |

## Field-semantics changes

### `clean` bitmask → three booleans

v1 stores message-lifecycle flags packed in one int (`clean`): bit 1 = clean-delete, bit 2 = edit,
bit 4 = summary. v2 unpacks them:

| v1 `clean` value | v2 equivalent |
|---|---|
| `0` | omit all three |
| `1` | `"clean": true` |
| `2` | `"edit": true` |
| `3` | `"clean": true, "edit": true` |
| `4` (quest) | `"summary": true` |

### Enums: magic int → string

| field | v1 | v2 |
|---|---|---|
| `team` | `0–4` | `"harmony" \| "mystic" \| "valor" \| "instinct" \| "any"` |
| `gender` | `0–3` | `"any" \| "male" \| "female" \| "genderless"` |
| `fort_type` | string | `"pokestop" \| "gym" \| "everything"` (unchanged values, now enforced) |
| `rsvp_changes` | `0–2` | `"none" \| "rsvp" \| "rsvp_only"` |

Game-master dictionary IDs stay **integers** (`pokemon_id`, `form`, `move`, `reward_type`,
`lure_id`, invasion `type_id`/`grunt_id`, incident `display_type`, `pvp_ranking_league`,
`pvp_ranking_evolution`).

### Sentinels → omit / null

Stop sending these v1 magic values; omit the field instead (and expect `null` on read):

| v1 sentinel | meant | v2 request |
|---|---|---|
| `min_iv: -1`, `rarity: -1`, `size: -1` | no floor | omit |
| `max_iv: 100`, `max_level: 55`, `max_atk/def/sta: 15`, `max_rarity: 6`, `max_size: 5` | no ceiling | omit |
| `max_cp: 9000` | no CP cap | omit |
| `pvp_ranking_worst: 4096` | no rank limit | omit |
| `max_weight: 9000000` | no weight cap | omit |
| raid/maxbattle `pokemon_id: 9000` | any boss (track by level) | omit `pokemon_id` |
| raid/maxbattle `level: 90` | all tiers | omit `level` |
| `form: 0`, `pvp_ranking_league: 0`, … | any / off | omit |

`distance: 0` keeps its v1 meaning — "use the profile's geofence areas instead of a radius" — and
is **not** a sentinel to avoid.

### Type-specific changes

- **invasion** — v2 requires **exactly one** targeting mode per rule: `type_id` (int poke-type,
  optional `gender`) | `grunt_id` (int exact character) | `everything: true` | `boss: true`. The
  facade translates down to the same stored grunt-type names v1 wrote.
- **incident (NEW)** — pokestop events (e.g. Showcases) split out of invasion into their own type,
  keyed by the game's `display_type` int. Not available on v1.
- **fort** — `include_empty` now defaults to **`true`** when omitted (v1 defaulted false).
- **quest** — `reward_type` (proto int: `2`=item, `3`=stardust, `4`=candy, `7`=pokemon,
  `12`=mega_energy) is required; `reward`, `amount`, `form`, `shiny` optional.
- **pokemon** — `pvp_ranking_evolution` (int mega/temp-evolution discriminator: `0` base, `1` Mega,
  `2` Mega X, `3` Mega Y) is new on v2.
- **`active_hours`** (profile PATCH and `POST /api/summaries/{id}/{alertType}`) — now a validated
  typed array of `{day 0–6, hours 0–23, mins 0–59, optional step/end_hours/end_mins}` entries,
  strict ints, no cross-midnight ranges. v1's freeform JSON passthrough is gone.

### Response shapes

- Reads return the resource directly (`{rules:[…]}`, `{human:…}`, the snapshot) — no
  `{status:"ok"}` envelope.
- Tracking mutations return the diff: `{created, updated, unchanged}` (POST/PUT) or `{deleted}`
  (DELETE), each entry carrying its `uid`; with `?include_descriptions=true` each entry also
  carries its human-readable `description`.
- Pure actions (enable/disable/language/areas/locations/roles/profile mutations) return the shared
  `{status:"ok"}`.

## Suggested migration order

1. Point reads at the snapshot (`GET /api/v2/humans/{id}/tracking`) — it replaces `all/{id}` and
   `allProfiles/{id}` in one call and returns profiles/locations/summaries you previously fetched
   separately.
2. Move tracking mutations type by type, dropping your sentinel-value handling as you go.
3. Move humans/profiles/locations actions last — they're mechanical renames per the tables above.
4. Adopt `problem+json` error handling once, for both v2 and the huma-migrated `/api/*` endpoints.
