<!-- Ready-to-post GitHub issue. Copy everything below the line. -->
<!-- STATUS (2026-06): posted as jfberry/PoracleNG#138; v2 has since been BUILT on branch
     huma-api-migration. This file is the historical RFC text. The as-built contract is
     docs/v2-api-design.md + the live spec at /openapi.json; fields marked "prospective"
     below (e.g. pvp_ranking_evolution) are now implemented. The v1->v2 client mapping is
     docs/v1-to-v2-migration-guide.md. -->
<!-- Suggested title: RFC: PoracleNG v2 API — clean, documented, OpenAPI-first (feedback wanted) -->

---

## RFC: PoracleNG v2 API

We're adding a **clean, strict, documented v2 API** (`/api/v2`) alongside the existing API. **v1 is unaffected** — it keeps working exactly as today; this is a new surface you can adopt on your own schedule. We'd love feedback from client/integration authors **before** we build it.

### Why

The current API is undocumented and, by necessity, tolerant of malformed input (it silently coerces wrong types). v2 is the opposite: an OpenAPI 3.1 contract generated from the server, strict validation with clear errors, and one honest representation per field. We'll be encouraging all v1 API users to move to v2 so they can access new tracking types; v1 stays supported (deprecation only later, with notice).

### Conventions

- **Auth:** `X-Poracle-Secret: <secret>` header (same secret as v1).
- **Errors:** RFC 9457 `application/problem+json`:
  ```json
  { "title": "Unprocessable Entity", "status": 422, "detail": "validation failed",
    "errors": [ { "message": "expected integer", "location": "body.rules[0].min_iv", "value": "ninety" } ] }
  ```
- **Success:** typed body directly — no `{ "status": "ok" }` wrapper.
- **Strict:** unknown body/query fields are rejected (`422`). No coercion — send the right types.
- **Field types:** game-master dictionary IDs (and numeric ranges) are **integers** (`pokemon_id`, `move`, `reward_type`, `lure_id`, invasion `type_id`/`grunt_id`, incident `display_type`, …); fixed categories are **string enums** (`team`, `gender`, `fort_type`, `rsvp_changes`); flags are **booleans**.
- **Docs:** OpenAPI at `/api/v2/openapi.json`, interactive docs at `/api/v2/docs` (public).

### Resource model

Tracking rules are **sub-resources of the human** (the human is the user). `uid` is unique per type; every item op is **scoped by `(human, uid)`** — you can't touch a uid that isn't the addressed human's (matches v1's ownership guard). `{type}` ∈ `pokemon, raid, egg, quest, invasion, incident, lure, nest, gym, fort, maxbattle`.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/v2/humans/{id}/tracking` | Full snapshot — human + all-type rules + profiles + locations + summaries |
| `GET` | `/api/v2/humans/{id}/tracking/{type}` | List one type |
| `POST` | `/api/v2/humans/{id}/tracking/{type}` | Create rule(s) — body is an array |
| `GET` | `/api/v2/humans/{id}/tracking/{type}/{uid}` | Fetch one rule |
| `PUT` | `/api/v2/humans/{id}/tracking/{type}/{uid}` | Full-replace one rule |
| `DELETE` | `/api/v2/humans/{id}/tracking/{type}/{uid}` | Delete one rule |
| `DELETE` | `/api/v2/humans/{id}/tracking/{type}?uid=1,2,3` | Bulk delete |

- `{id}` (the human) is always in the path; `profile` is `?profile={n}` (defaults to active).
- List → `{ "rules": [ … ] }`. **Snapshot** (`…/tracking`, no type) → `{ "human": {…}, "tracking": { "<type>": [...] }, "profiles": [...], "locations": [...], "summaries": [...] }` (`?all_profiles=true` spans all profiles; replaces v1 `all/{id}` + `allProfiles/{id}`).
- Create → `{ "created": [...], "updated": [...], "unchanged": [...] }`; Delete → `{ "deleted": [...] }` (each rule carries its `uid`; POST keeps v1's diff/upsert).
- **`?include_descriptions=true`** works on **every** tracking endpoint (reads **and** mutations): when set, each rule in the response (`rules` / `created` / `updated` / `unchanged` / `deleted`) gets a `description` (human-readable rowtext, in the human's language). Status is conveyed by which array the rule's in — no separate `message` field. (The assembled confirmation message stays the Discord/Telegram push, gated by `?silent`.)
- `PUT` is a full replace; omitted fields reset to defaults.
- Mutations accept `?silent=true` to apply without notifying the user (single param; replaces v1's `silent`+`suppressMessage`).

### Common rule fields

`distance` (int; `0` = use profile areas), `template` (string), `clean`/`edit`/`summary` (bool), `ping` (string), `override_location_label` (string), `override_areas` (string[]).

### Per-type fields (`*` = required)

- **pokemon** — `pokemon_id`* , `form`, `min_iv`/`max_iv`, `min_cp`/`max_cp`, `min_level`/`max_level`, `atk`/`def`/`sta` & `max_atk`/`max_def`/`max_sta`, `rarity`/`max_rarity`, `size`/`max_size` (all int), `gender` (enum `any|male|female|genderless`), `pvp_ranking_league` (int — CP cap `0|500|1500|2500`), `pvp_ranking_best`/`pvp_ranking_worst`/`pvp_ranking_min_cp`/`pvp_ranking_cap` (int), `pvp_ranking_evolution` (int — mega/evolution discriminator: `0`=default, `2`=Mega X, `3`=Mega Y; *prospective*).
- **raid** — `pokemon_id` (int, `0`=any), `form`, `level`, `move`, `evolution` (int), `team` (enum `harmony|mystic|valor|instinct|any`), `exclusive` (bool), `gym_id` (string), `rsvp_changes` (enum `none|rsvp|rsvp_only`).
- **egg** — `level` (int), `team` (enum), `exclusive` (bool), `gym_id` (string), `rsvp_changes` (enum).
- **quest** — `reward_type`* (int: `2`=item,`3`=stardust,`4`=candy,`7`=pokemon,`12`=mega_energy), `reward` (int), `amount` (int), `form` (int), `shiny` (bool).
- **invasion** — exactly one mode: `type_id` (int poke-type, + optional `gender` enum) | `grunt_id` (int, exact grunt — implies type+gender) | `everything` (bool) | `boss` (bool).
- **incident** — `display_type`* (int — game event id, e.g. `9` = Showcase; names documented).
- **lure** — `lure_id` (int item id: `0`=any, `501`=normal … `506`=sparkly).
- **nest** — `pokemon_id`, `form`, `min_spawn_avg` (all int).
- **gym** — `team` (enum), `slot_changes` (bool), `battle_changes` (bool), `gym_id` (string).
- **fort** — `fort_type` (enum `pokestop|gym|everything`), `include_empty` (bool, default `true`), `change_types` (string[] of `location|new|removal|image_url|name|description`).
- **maxbattle** — `pokemon_id`, `level`, `move` (int), `gmax` (bool).

### Examples

```
POST /api/v2/humans/123456/tracking/pokemon?profile=1
[ { "pokemon_id": 149, "min_iv": 95, "gender": "female", "clean": true },
  { "pokemon_id": 384, "pvp_ranking_league": 1500, "pvp_ranking_best": 1, "pvp_ranking_worst": 5, "edit": true } ]

POST /api/v2/humans/123456/tracking/invasion?profile=1
[ { "grunt_id": 41 }, { "type_id": 12, "gender": "female" } ]

POST /api/v2/humans/123456/tracking/incident?profile=1
[ { "display_type": 9 } ]

GET    /api/v2/humans/123456/tracking?include_descriptions=true   # full snapshot
DELETE /api/v2/humans/123456/tracking/raid/80921
```

### Humans, profiles & schedules (v2)

Discrete, typed endpoints under `/api/v2` (problem+json, strict):

**Humans:** `POST /api/v2/humans` (create) · `GET …/humans/{id}` (resource; includes read-only `blocked_alerts`) · `GET …/{id}/areas` · `POST …/{id}/{enable|disable|admin-disable|language|location|areas|profile}` · `GET …/{id}/check-location?lat=&lon=` · **saved locations** `GET` (list), `GET/{label}`, `POST {label,lat,lon}`, **`PUT/{label} {lat,lon}` (NEW — edit a saved location)**, `DELETE/{label}` · **roles** `GET`, `POST|DELETE …/{roleId}`, `GET …/{id}/admin-roles`.

**Profiles:** `GET /api/v2/humans/{id}/profiles` · `POST` (add) · `PATCH …/{profile_no}` (active_hours) · `DELETE …/{profile_no}` · `POST …/{profile_no}/copy`.

**`active_hours` — now a real typed schema** (shared by profile schedules and `POST /summaries/{id}/{alertType}`; replaces the old freeform-JSON passthrough). An array of entries:

| field | type | required | bounds |
|---|---|---|---|
| `day` | int | yes | 0–6 (0 = Sunday) |
| `hours` | int | yes | 0–23 |
| `mins` | int | yes | 0–59 |
| `step` | int | no | ≥0 hours; `>0` ⇒ range entry |
| `end_hours` / `end_mins` | int | iff `step>0` | 0–23 / 0–59 |

Single-fire `{day,hours,mins}`, or range (adds `step`/`end_*`, fires every `step` hours to `end`, no cross-midnight). Strict ints (drops v1's `"00"` string coercion).

**`blocked_alerts`** is read-only on the human resource (derived from Discord roles / `command_security`, not API-settable): `monster`(=pokemon)`|pvp|raid|egg|quest|invasion|lure|nest|gym|fort|maxbattle|specificgym|specificstation`.

### Questions we'd love your input on

1. **Collection scoping** — is `?user=&profile=` on the collection comfortable, or would you prefer `/api/v2/users/{id}/tracking/{type}`?
2. **Create response** — is `{created, updated, unchanged}` useful, or do you just want the resulting rules?
3. **int vs string-enum split** — does the game-master-id-as-int / fixed-category-as-string-enum split match how you think about these fields? Any field you'd flip?
4. **invasion two-axis** (`type_id` vs `grunt_id`) and the **separate `incident` type** — does this fit your use cases?
5. **humans/profiles shape** — we kept **discrete action endpoints** (enable/disable/language/location/areas/profile) rather than a consolidated `PATCH`. Does that suit your client, and is the typed `active_hours` schema right?
6. **Anything in v1 you depend on** that isn't represented here?

Thanks! Comments here or on the linked design doc.
