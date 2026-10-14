# Changelog

All notable changes to PoracleNG are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added

- **`[tracking] max_areas`** (default `0` = no limit). Caps how many areas a
  user may hold per profile (`!area add`, v1 `setAreas`, v2 set-areas, and the
  initial `area` on v1/v2 human create) and per rule (`area:` on tracking
  commands, `override_areas` on v1 and v2 tracking writes). Over-limit writes
  are refused (bot reply, v1 400, v2 422), not truncated. A rule or profile
  already over the limit — because the limit was lowered — may keep or shrink
  its own list, so GET → PUT/PATCH round trips keep working; its list cannot
  be copied into a new rule. Exposed as `maxAreas` in
  `GET /api/config/poracleWeb`.
- **`exemptFromAreaSecurity` geofence property.** A fence that declares it
  (native or GeoJSON) is outside the community system: area_security never
  filters it out of a user's permitted areas, and reconciliation never prunes
  it. It does not imply `userSelectable` (a non-selectable exempt fence still
  needs `trusted: true` via the API) and does not affect `strict_locations`.
  With `area_security` on and `strict_locations` off (the default), selection
  is the only gate, so any user can select an exempt fence and receive its
  alerts. `GET /api/v2/humans/{id}/areas` reports the flag per fence (#243).
- **`[reconciliation.discord|telegram] prune_user_areas`** (default `false`).
  On a community-membership change, removes areas the user may no longer hold
  from `humans.area` and every profile. Irreversible: an incomplete
  `allowed_areas` empties that community's members. Only currently loaded
  fences are ever removed, so a partial geofence load can't strip selections (#243).
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
- **v1 tracking enforces `[tracking] max_distance`.** v1 tracking POSTs now
  clamp `distance` like the bot and v2 do; previously v1 stored any radius
  (pokemon capped only at the earth's circumference).
- **`[tracking] max_distance` and `max_areas` apply to users only.** Admins,
  and channels, webhooks, groups and threads (which only admins and delegated
  admins configure), are exempt on every surface: bot commands (text and
  slash) and v1/v2 writes. v2 `trusted` requests are exempt too. So a radius
  an admin gives a channel is no longer clamped by a later API round trip.
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
- **v2 `?dry_run=true`** on tracking POST, PUT, single and bulk DELETE and
  PATCH, and on area DELETE/PATCH. Runs the same validation and diff but
  writes nothing; created rules have `uid` 0 and the response carries
  `"dry_run": true`.
- **v2 `previous` on updated rules.** A rule in `updated` (POST, PUT, PATCH)
  carries `previous`, the rule as it was stored before the edit, so a client
  can show what changed. A real commit reports what it actually replaced.
- **Bulk PATCH of delivery settings.** `PATCH /api/v2/humans/{id}/tracking/{type}`
  with `{"uids":[…],"set":{distance,clean,edit,summary,template,ping}}` changes
  those settings on the listed rules and keeps each rule's `uid`. One outcome
  per rule (`updated` / `unchanged` / `refused` with reason `invalid`,
  `conflict` or `not_found`); never all-or-nothing. `summary` is quest only;
  `edit` is refused for gym, invasion, maxbattle, nest and quest.
- **Bulk DELETE accepts a body.** `DELETE …/tracking/{type}` takes either
  `?uid=1,2,3` or `{"uids":[…]}` (not both, not neither: `422`), for lists too
  long for a URL.
- **v2 area operations.** `DELETE /api/v2/humans/{id}/areas?name=` removes one
  area name from the human, the profile(s) and every rule's `override_areas`,
  reporting `removed_from` and `broadened` (rules whose override list became
  empty and now use the profile's areas). `PATCH …/areas?from=&to=` renames one
  name everywhere (`to` must be permitted: `422` otherwise, nothing changes).
  Both take `?profile=`, `?all_profiles=` and `?dry_run=` (#252, #253, #259).
- **v2 mutes: typed `tracking_type`.** A `tracking` mute names the table of
  the rule it silences (any of the 11 type names; `incident` is stored as
  `invasion`). `POST` checks that the uid is the human's rule in that table;
  `DELETE ?scope=tracking&tracking_type=&value=`. The bot's `!<type> mute id:N`
  keeps the type, and a bare `!mute id:N` / `!unmute id:N` resolves it or is
  refused when ambiguous.
- **`/health` and `servers` in the OpenAPI spec.** `GET /health` is documented
  (public); the spec has `servers: [{url: "/api"}]` so paths carry their prefix.
- **Per-human write lock.** All writes to one human (API, bot commands,
  reconcilers, apply, rate-limit disable, buttons, profile scheduler) are now
  serialised, so concurrent edits no longer interleave.

### Changed

- **BREAKING: v2 mutes with `scope=tracking` require `tracking_type`**
  (`422` without it). Mute uids are per-table, so an untyped tracking mute
  could silence an unrelated rule in another table; an untyped entry now
  matches nothing.
- **BREAKING: the 401 from the `X-Poracle-Secret` check is `application/problem+json`
  on every huma route** (all of `/api/v2` and the unversioned `/api` routes).
  The frozen v1 routes (`/api/tracking`, `/api/humans`, `/api/profiles`) keep
  `{"status":"authError",…}`.
- **BREAKING: v2 `PUT …/tracking/{type}/{uid}` returns `409` ("would duplicate
  rule N") when the edit would make the rule a duplicate or near-duplicate (one
  updatable field apart) of another rule.** Previously only an exact duplicate
  was refused.
- **BREAKING: v2 returns `override_areas` as canonical fence names**
  (lowercase, as stored); they were previously returned space-normalised.
- **v2 POST reports `unchanged` rules with their real uid.** A submitted rule that
  exactly matches a stored one comes back under `unchanged` carrying the stored
  rule's `uid` (previously `0`), and it is matched against *every* stored rule
  before any near-duplicate update is considered, so it can no longer overwrite a
  different rule.
- **v2 PUT of an unchanged rule reports it as `unchanged`.** A `PUT` whose body
  matches the stored rule (e.g. GET → PUT, including legacy alias values and
  spaced area spellings) returns the rule under `unchanged` with its uid and
  `updated: []`; nothing is written, reloaded or pushed (also under `?dry_run`),
  as for POST and PATCH. Previously it rewrote the row and pushed a "changed"
  confirmation. PUT responses now always carry `created`, `updated` and
  `unchanged` as arrays (`created` was `null`).
- **v2 area DELETE / PATCH report the current profile when only `humans.area`
  changed.** For a human whose current profile has no `profiles` row,
  `removed_from` / `renamed_in` now list the current profile number instead of
  `[]` when the call wrote `humans.area` (also under `?dry_run`).
- **BREAKING: maxbattle edits now squash.** `clean`, `distance` and `template` are
  updatable fields for maxbattle, as for raid: re-posting a rule with one of
  them changed updates the rule in place instead of adding a copy. Within one
  batch an existing row is an update target for at most one candidate.
- **Area spelling.** Stored area names are canonical fence names (lowercase);
  spaces versus underscores is only a comparison key. State loading normalises
  `override_areas`, so overrides stored with underscores now match; set-areas,
  community `allowed_areas`, and `!area add/remove` accept either spelling.
  v1 still writes and returns spaces.
- **Reconcilers prune before saving a community-membership change.** If the
  prune fails, `community_membership` / `area_restriction` are not saved and
  the change is retried at the next reconcile (member event or
  `check_role_interval` sweep).
- **v2 `…/areas` set (`POST`) with `?all_profiles`** is documented as a
  whole-list replace.
- **Wildcard contract.** `null` on a v2 rule means unconstrained; clients never
  need the stored sentinel, which may change without notice. Enforced by a test
  that every nullable field on all 11 types reads back `null` and round-trips.

- **One definition of the areas a human may hold** (#243). v2 set-areas,
  `override_areas` validation (v1 and v2 tracking writes), v2 GET areas and
  reconciliation all use `bot.PermittedAreas`; `!area` shares its community
  rules. Under `area_security`, a community member is now community-filtered
  even when their `area_restriction` is empty (communities with no
  `location_fence`) — previously v2 set-areas let such members select any
  user-selectable fence. v1 `POST /api/humans/{id}/setAreas` is frozen and
  unchanged. Discord reconciliation's `override_areas` prune now keeps
  exempt fences, fences granted via `trusted`, and fences whose names
  contain underscores.
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
- **Migration 000009 no longer fails on legacy mixed-collation databases.**
  On databases adopted from Knex whose tables use different collations, it
  failed with "Illegal mix of collations". It now normalises IDs before
  comparing them and commits its data changes in one transaction. Startup
  retries a dirty 000009 and refuses to skip any other dirty migration,
  instead of silently marking it applied. A database where an older build
  skipped a failed 000009 needs a manual reset to version 8; see
  `docs/migration-collations.md`.
- **Migration 000009 runs on MariaDB 10.5 and 10.6.** The collation fix above
  failed there, on fresh installs and upgrades alike, with "BLOB/TEXT column
  'id' used in key specification without a key length": those versions type
  the normalised ID as TEXT. Its temporary tables now declare the ID column.
  A database left dirty at 000009 by that error is retried on the next start.
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
- **v2 snapshot `?all_profiles=true` returns every rule the human has.** It
  read only profile numbers with a `profiles` row, so a human with no row for
  their active profile (common after a PoracleJS migration) got no rules at
  all, and rules left by a deleted profile were missing. It now reads every
  profile number a rule sits on, plus the active profile, as v1
  `allProfiles/{id}` did. Such rules carry a `profile_no` that `profiles` does
  not list (#330).
- **v2 area `?all_profiles=true` covers the same profiles as the snapshot.**
  `DELETE`/`PATCH /api/v2/humans/{id}/areas` and `POST …/areas` with
  `all_profiles` built their profile list from `profiles` rows only, so a
  human with a row for another profile but none for the active one kept the
  removed or renamed name in `humans.area` and in that profile's
  `override_areas`, while the snapshot still showed it. They now use the
  snapshot's set: profiles rows, the active profile, and every profile a rule
  sits on.
- **v2 invasion catch-alls keep their gender.** `everything` and `boss` rules
  read back with `gender: null` even when stored as male or female, although
  the matcher applies it, so a read-modify-write widened the rule to both
  genders. Reads now return the gender, and `gender` is accepted with every
  mode except `grunt_id` (#330).

### Deprecated

- **v1 tracking/humans/profiles endpoints are frozen and
  deprecated-but-supported.** `/api/tracking/*`, `/api/humans/*`,
  `/api/profiles/*` (and `/api/tracking/pokemon/refresh`) remain on gin,
  unchanged and fully functional — clients migrate to `/api/v2` on their own
  schedule. Migration is encouraged to access new tracking types (e.g.
  `incident`) and the cleaner strict contract. No sunset date is set yet.
