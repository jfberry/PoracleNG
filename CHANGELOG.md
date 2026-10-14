# Changelog

All notable changes to PoracleNG are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added

- **Personal shiny / background tracking** (mirrors Golbat#423). Pokemon rules
  can require that the pokemon is shiny (or has the special background) for
  one of the user's linked game accounts: `!track … personalshiny|personalbackground`,
  `/track personal_shiny|personal_background`, v1 `personal` bitmask, v2
  `personal_shiny` / `personal_background`. Needs the new `[shiny_rates]`
  feed; fails closed until it loads. Synced with Golbat#423 @ bec2c23: known
  Dittos roll at 1/64, the feed's `fallback` rate, and `[shiny_rates] max_bytes`.
- **Linked game accounts.** `!account` / `/account` (list/add/remove/rename; `!account add <name> <player-id>`) and
  `GET|POST /api/v2/humans/{id}/accounts`, `PUT|DELETE …/accounts/{player_id}`;
  the v2 snapshot carries `accounts`. Player IDs are 1-256 ASCII
  letters/digits, case-sensitive. `[player_accounts] self_service` gates
  non-admin linking. `/health` advertises `personalShiny`.
- **Feed-backed shiny info.** `shinyPossible` is variant-aware when
  `[shiny_rates]` is configured; new `shinyRate` / `shinyRateBoosted` DTS
  fields (pokemon, quest, nest); per-user `personalShiny*`,
  `personalBackground*`, `hasLinkedAccounts`. `!info <pokemon>` shows the
  expected shiny rate, `!info shiny` what is boosted now, and
  `!poracle-admin status` a "Shiny rates" section.
- Migration `000010_personal_shiny` (`player_accounts` table, `monsters.personal`).
- **v2 tracking `ping`.** Every v2 rule type accepts and returns an optional `ping`
  (mention text). Previously v2 stored `""`, which erased pings set by the bot or v1 on any PUT.
- **v2 clear location.** `DELETE /api/v2/humans/{id}/location[?profile=]`
  resets a human's (or profile's) location to unset. Unlike
  `POST …/location {lat:0,lon:0}` it is not refused for area-restricted users.
- **v2 tracking enforces `[tracking] max_distance`.** Every v2 tracking type
  now clamps `distance` to the configured maximum, as the bot commands do
  (previously v2 only capped at the earth's circumference). Over-limit values
  are clamped, not rejected, so GET → POST/PUT never fails on them; a stored
  rule above a since-lowered limit comes back from that round trip as
  `updated` (rewritten at the limit), not `unchanged`.
- **v2 mutes API.** `GET/POST /api/v2/humans/{id}/mutes` and
  `DELETE /api/v2/humans/{id}/mutes[?scope=&value=]` expose the in-memory alert
  mutes (the `!mute` / alert-button feature) over HTTP, and the v2 full
  snapshot now carries a `mutes` array. Mutes remain volatile — they are
  cleared by a processor restart.
- **OpenAPI 3.1 spec + interactive docs.** The processor now serves a single
  OpenAPI 3.1 document at `GET /openapi.json` with interactive documentation at
  `GET /docs` (both public, no `X-Poracle-Secret` required). The spec covers the
  entire `/api/*` and `/api/v2/*` surface, generated from the code via
  [huma](https://github.com/danielgtaylor/huma) mounted on gin.
- **New strict `/api/v2` API** for tracking and humans/profiles — typed bodies,
  `additionalProperties: false`, enforced required fields, and no lenient type
  coercion. A malformed request gets a clear `422` instead of a silent guess.
  - **Tracking** is human-scoped: `GET|POST /api/v2/humans/{id}/tracking/{type}`,
    `GET|PUT|DELETE /api/v2/humans/{id}/tracking/{type}/{uid}`, bulk
    `DELETE …/{type}?uid=1,2,3`, and a full snapshot at
    `GET /api/v2/humans/{id}/tracking` (human + all-type rules + profiles +
    locations + summaries). Supports `?profile=`, `?include_descriptions=`,
    `?silent=`, and `?all_profiles=`.
  - **humans/profiles** are exposed as discrete, typed action endpoints under
    `/api/v2/humans/{id}/…` (enable/disable, admin-disable, language, location,
    areas, check-location, locations CRUD, roles, profiles CRUD with a strict
    typed `active_hours` schema, and profile switch).
- **New `incident` tracking type** (game `PokestopEvent`, e.g. Showcase) —
  available only via the `/api/v2` tracking surface, bringing the v2 tracking
  type count to 11 (pokemon, raid, egg, quest, invasion, incident, lure, nest,
  gym, fort, maxbattle).
- **New `PUT /api/v2/humans/{id}/locations/{label}`** — update a saved
  location's coordinates in place, completing saved-location CRUD (v1 required
  delete + re-add to move a location).

### Changed

- **Error bodies on the huma `/api` surface are now RFC 9457
  `application/problem+json`** (`status`, `title`, `detail`, `errors[]`),
  replacing the old ad-hoc `{ "status": "error", "message": … }` /
  `{ "error": … }` bodies. **This is a behavior change for clients that parse
  error response bodies.** HTTP status codes are unchanged, though a few
  input-validation failures that were manual `400`s now surface as huma
  validation `422`s.
- **`include_empty` on fort tracking now defaults to `true`** when omitted,
  honoring the `forts` DB column default. The previous gin handler defaulted it
  to `false`; API clients that omit `include_empty` now get `true`.
- **New humans start on profile 1, and migration 000009 moves existing
  ones off profile 0.** `!poracle`, Discord role reconciliation, `!channel add`,
  `!webhook add` and Telegram channel registration created humans on a profile
  0 that has no `profiles` row, while their default profile is 1. Bot commands
  worked, but rules a client saved to profile 1 never matched, and a user who
  added and switched to profile 2 could never get back to their profile-0
  rules. The migration copies the live area/location onto profile 1 for humans
  currently on 0 and moves them to profile 1, merging their profile-0 rules in.
  For a human that had already switched away, profile-0 rules move to profile
  1 if it is empty, and are deleted as abandoned if the user had since rebuilt
  profile 1. Humans with a genuine `profiles` row for 0 are left alone. Not
  reversible.
- **v2 pokemon: a GET → POST of a rule stored with a legacy wildcard alias is
  `unchanged`, not a duplicate.** Rows holding `0` for `pvp_ranking_best`,
  `rarity`, `size` or `pvp_ranking_worst` read back as `null`, which the write
  stores as `1`/`-1`/`4096`; the create/PUT diff now treats the two as the
  same rule. Stored rows are not rewritten.
- **PVP rules stored with `pvp_ranking_worst = 0` now match.** `0` is not a
  rank; the bot already reads it as "no upper limit" on input and v2 reports it
  as `null`, but the matcher dropped every rank above 0, so such a rule never
  fired. Rules now load with `0` treated as `4096` (no limit).
- **A ping-only change updates the rule instead of adding a copy.** `ping`
  is now an updatable diff field, like `distance` and `template`, for the bot
  commands, v1 and v2 alike. Re-running a tracking command (or re-posting a
  rule) with a different mention — or with none — changes that rule's ping in
  place; previously it created a second rule. As with the other updatable
  fields, changing the ping together with another field still adds a new rule.
- **v2 `?profile=` on location and areas no longer touches the active
  profile.** Setting or clearing the location, or setting areas, for an
  inactive profile also overwrote the human's live location/areas — the copy
  the matcher reads — so e.g. `DELETE …/location?profile=2` while profile 1 was
  active stopped every distance rule matching. The store now writes the human
  row only when the named profile is the current one.

### Deprecated

- **v1 tracking/humans/profiles endpoints are frozen and
  deprecated-but-supported.** `/api/tracking/*`, `/api/humans/*`,
  `/api/profiles/*` (and `/api/tracking/pokemon/refresh`) remain on gin,
  unchanged and fully functional — clients migrate to `/api/v2` on their own
  schedule. Migration is encouraged to access new tracking types (e.g.
  `incident`) and the cleaner strict contract. No sunset date is set yet.
