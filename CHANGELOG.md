# Changelog

All notable changes to PoracleNG are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Fixed

- **`distance`, `bearing`, `bearingEmoji` and `userTrackDistance` now work in
  non-pokemon templates** (raid, egg, quest, invasion, incident, lure, nest,
  gym, fort-update, maxbattle). They used to render as 0 or empty, because
  those alerts render once and share the result between users. A template
  that uses any of these fields, directly or in a partial, is now rendered once
  per user. Templates that don't use them are still shared.
- **A broken template no longer reports "Template not found".** When your
  template is matched but has a Handlebars syntax error (for example an
  unclosed `{{#if}}`), the alert now says "Template compile error". When its
  `templateFile` can't be read, it says "Template could not be loaded". The
  processor log names the exact problem. `!poracle-test` reports the same.

## [5.4.0] - 2026-11-10

Covers everything merged to `develop` since 5.2.1 (develop builds briefly
reported 5.3.0; there was no 5.3.0 release).

### Upgrade notes

Read these before upgrading. Each is described in full below.

- **Migration 000009 runs on first start and cannot be reversed.** It moves
  humans off the accidental profile 0 onto profile 1. Back up the database
  first.
- **Behind a reverse proxy with `ip_whitelist` / `ip_blacklist`?** Add the
  proxy to the new `[processor] trusted_proxies`, or every request will be
  seen as coming from the proxy. An invalid entry in any of the three lists
  now stops startup.
- **`[telegram] enabled = false` now really stops Telegram alerts.** The
  example config ships with `false`. If you added a Telegram token but kept
  that line, you were only getting alerts because of a bug, and they will stop.
  Set `enabled = true`.
- **`/debug/pprof` is off by default.** Set `[processor] enable_pprof = true`
  and send `X-Poracle-Secret` to use it.
- **A bare `!quest remove`, `!gym remove`, `!fort remove` or `!invasion remove`
  no longer deletes everything.** Users must type `… remove everything`.
  Removes also refuse arguments they don't use, rather than ignoring them.
- **`!weather` is removed** (it only ever replied "unavailable").
  `!info weather` is unaffected.
- **v2 API clients:** this release has breaking changes to v2 response shapes,
  coordinates (`latitude`/`longitude`), mutes and PUT. See "Changed" below and
  the "Changes" section of `docs/v2-api-design.md`.

### Added

#### Alerts and delivery

- **Pokemon edit mode.** The `edit` flag on pokemon tracking now works (it was
  accepted but ignored). A follow-up for the same encounter edits the original
  alert, using `monster` while the rule still matches and `monsterChanged`
  once it doesn't. Edits happen only within `[tracking]
  pokemon_edit_window_mins` (default `5`, `0` = no limit). After that, a
  threaded reply is sent instead, so updates aren't lost further up a busy
  channel. `[tracking] pokemon_edit = false` turns pokemon edits off. `!track`
  accepts `edit`. `/track`, `/raid`, `/egg` and `/lure` gain an `edit` option.
  A successful edit refreshes the clean-deletion timer, and Telegram's
  "message is not modified" response counts as a success.
- **`[tracking] default_edit`** (list of `pokemon`, `raid`, `egg`, `lure`,
  `incident`). Rules of a listed type edit the original alert in place even
  when the rule doesn't set `edit`. It is applied at match time and nothing is
  stored, so existing rules pick it up immediately and removing a type reverts
  them. `!tracked` labels these rules "edit (default)". Exposed as
  `defaultEdit` in `GET /api/config/poracleWeb`.
- **Telegram photo captions.** `photo` accepts an object,
  `{"url": "…", "caption": "…"}`, alongside the existing URL string. The
  caption is sanitised and truncated to Telegram's 1024-character limit. A
  captioned photo notifies unless a separate text message is also sent, so
  each alert notifies once. Edits update the caption. See "Telegram Message
  Fields" in `DTS.md` (#334, #340).
- **Raid weather-change forecast.** Hatched raids now render `weatherChange`,
  `weatherChangeTime`, `weatherCurrentName/Emoji` and `weatherNextName/Emoji`
  when the forecast could change the boss's boost. Pokemon gain
  `boostingWeathersEmoji`.
- **Template helpers ported from PoracleJS:** `capitalizeAll`, `ellipsis`,
  `startsWith` and `remove`. Previously these rendered as nothing, so
  `{{{capitalizeAll gymName}}}` dropped the gym name.
- **Custom maps behave like PoracleJS.** Map values may be objects or numbers,
  `{{#map …}}…{{else}}…{{/map}}` block form works, maps are keyed by their
  `name` field with `language` honoured, and a file may hold several maps.
  `{{formId}}` is available on monster, raid and nest. Evolution entries gain
  `nameEng` / `fullNameEng` / `formNameEng` / `typeEmoji`, and
  `hasMegaEvolutions` covers the whole evolution chain.
  `GET /api/dts/custommaps` and a `customMaps` capability on `/health` let
  the DTS editor preview `{{map}}`.
- **`showcaseFocusEmoji`** is populated, with themable `showcase-focus-<type>`
  emoji keys (default 🏆).
- **Bundled nest test data** for `!poracle-test nest riverside`. With
  `location: "current"` the nest and its outline move to the tester.

#### Commands and administration

- **`[tracking] max_areas`** (default `0` = no limit). Caps how many areas a
  user may hold per profile (`!area add`, v1 `setAreas`, v2 set-areas, and the
  initial `area` on v1/v2 human create) and per rule (`area:` on tracking
  commands, `override_areas` on v1 and v2 tracking writes). Over-limit writes
  are refused (bot reply, v1 400, v2 422), not truncated. A rule or profile
  already over the limit — because the limit was lowered — may keep or shrink
  its own list, so GET → PUT/PATCH round trips keep working; its list cannot
  be copied into a new rule. Exposed as `maxAreas` in
  `GET /api/config/poracleWeb`.
- **`[tracking] max_distance` and `max_areas` apply to users only.** Admins,
  and channels, webhooks, groups and threads (which only admins and delegated
  admins configure), are exempt on every surface: bot commands (text and
  slash) and v1/v2 writes. v2 `trusted` requests are exempt too. So a radius
  an admin gives a channel is no longer clamped by a later API round trip.
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
- **`@everyone` works as a Discord role again.** Discord never lists
  `@everyone` in a member's roles; its ID is the guild ID. PoracleJS treated
  the guild ID as a role every member holds, so `user_role = ["<guild id>"]`
  meant "any member of this server". NG did not, and with
  `remove_invalid_users = true` it admin-disabled every member of such a
  community. The guild ID now counts as a role for reconciliation,
  `command_security` and delegated admin (#261).
- **Delegated admin restored to PoracleJS behaviour.** Discord entries keyed
  by a category ID now grant access to the channels and threads in it.
  Telegram `[[telegram.delegated_admins]]` users can manage tracking in their
  chat (previously the setting was only logged), area-security community admins
  can manage their communities' groups, and community admins can run
  `/userlist` again, limited to their communities.
- **Admins bypass `command_security` on text commands and `/api/command`**, as
  they already did for slash commands.
- **`!poracle-admin slash list`** shows the registered slash commands as
  clickable Discord `</name:id>` mentions, with the syntax in a code block for
  reuse in announcements or templates.
- **Slash autocomplete:** `/help` topics (previously no suggestions at all),
  and the `settime` time syntax on `/profile` and `/summary` with glossed
  suggestions such as "Mon–Fri, hourly 07:00 → 18:00".
- **`!help` rewritten.** Every topic was checked against the current commands
  and broken examples were fixed. Topic overviews are shown again (they were
  discarded), and new `ask` and `role` topics appear only when those features
  are enabled.
- **`!quest` with several rewards** creates a rule for each reward named, as
  PoracleJS did. Previously only the first reward kind was kept.
  `!quest remove` removes each one in the same way.
- **`!nest everything gen1` / `everything fire`** applies the filter across
  every species, as `!track` does.
- **`!info <pokemon> form:`** accepts the translated prefix (e.g. French
  `forme:`).
- **`POST /api/command`** accepts commands with or without the prefix.
- **PVP `level_caps` mismatch warning.** When Golbat sends ranks for a level cap
  not listed in `[pvp] level_caps`, a warning is logged once per cap and shown
  in `!poracle-admin warnings`. Unlisted caps are dropped, which can make PVP
  rules silently miss.
- **`[geocoding] local_language`.** Lookups in this language are sent without a
  language parameter, so the provider returns its native place names. Set it
  with Photon prebuilt indexes, which only accept default/en/de/fr and return
  400 (`{{addr}}` = Unknown) for any other `[general] locale`.
- **Discord startup retries.** A temporary DNS or network failure at startup
  is retried every 60 seconds instead of leaving Discord commands unavailable
  until a restart. The HTTP server, webhooks and Telegram start in the
  meantime. Invalid tokens and disallowed intents still fail immediately.
- **Delivery shedding metrics.** `poracle_delivery_lane_shed_total` and
  `delivery_total{result="shed_lane_full"}` count alerts dropped from a full
  destination queue (see "Changed").

#### API

- **v2 humans list and delete.** `GET /api/v2/humans` (`?type=`, `?id=a,b,c`)
  returns `last_checked`, `disabled_date` and `notes` with each human.
  `DELETE /api/v2/humans/{id}` deletes a human.
- **`GET /api/v2/activity`** lists everything seen in the last 6 hours: raid
  and max battle bosses, quest reward ids, invasion grunts, and costumes and
  forms on wild pokemon and raid bosses. Clients can use it to fill pickers
  from what is live.
- **`GET /api/masterdata/costumes?locale=`** serves translated costume names,
  and `/health` gains a `costume` capability.
- **`GET /api/geocode/reverse?lat=&lon=&language=`** returns the full parsed
  address (404 when there is none). Forward geocoding results now include
  `displayName`, `name`, `streetNumber`, `streetName`, `state` and `zipcode`.
  `GET /api/config/poracleWeb` includes the geocoding `provider`.
- **v2 profiles:** create returns the created profile; `PATCH` is a real
  partial update and can rename.
- **v2 set-areas** reports which areas it stored and which it rejected, and
  accepts `?profile=` and `?all_profiles=`. An opt-in `trusted` flag (body
  for set-areas, `?trusted=` on tracking writes) lifts the `userSelectable`
  filter only: unknown fence names are still rejected and the community
  restriction still applies.
- **v2 tracking `ping`.** Every v2 rule type accepts and returns an optional `ping`
  (mention text). Previously v2 stored `""`, which erased pings set by the bot or v1 on any PUT.
- **v2 clear location.** `DELETE /api/v2/humans/{id}/location[?profile=]`
  resets a human's (or profile's) location to unset. Unlike
  `POST …/location {latitude:0,longitude:0}` it is not refused for area-restricted users.
- **v2 tracking enforces `[tracking] max_distance`.** Every v2 tracking type
  now clamps `distance` to the configured maximum, as the bot commands do
  (previously v2 only capped at the earth's circumference). Over-limit values
  are clamped, not rejected, so GET → POST/PUT never fails on them; a stored
  rule above a since-lowered limit comes back from that round trip as
  `updated` (rewritten at the limit), not `unchanged`.
- **v1 tracking enforces `[tracking] max_distance`.** v1 tracking POSTs now
  clamp `distance` like the bot and v2 do; previously v1 stored any radius
  (pokemon capped only at the earth's circumference).
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

#### Behaviour

- **Edited tracking rules keep their uid everywhere.** Changing a rule (e.g.
  `!track pikachu iv90` then `!track pikachu iv90 d500`, a v1/v2 POST, or a v2
  PUT) used to give it a new id. Mutes on it stopped applying, mute and
  unsubscribe buttons on earlier alerts stopped working, and a remembered
  `id:N` no longer matched. The rule now keeps its id. New rules still get
  fresh ids.
- **A ping-only change updates the rule instead of adding a copy.** `ping`
  is now an updatable diff field, like `distance` and `template`, for the bot
  commands, v1 and v2 alike. Re-running a tracking command (or re-posting a
  rule) with a different mention — or with none — changes that rule's ping in
  place; previously it created a second rule. As with the other updatable
  fields, changing the ping together with another field still adds a new rule.
- **BREAKING: one set of remove rules for every tracking command.** Raid, egg,
  quest, invasion/incident, lure, nest, gym, fort, maxbattle and `!untrack`
  now share these rules:
  - Unrecognised words are refused before anything is removed. Previously
    `!invasion remove <typo>` deleted every invasion rule.
  - `id:N` stands alone; any other argument beside it is refused.
  - An argument the remove doesn't use is refused ("Not used when removing:
    mystic") instead of being ignored.
  - A bare `remove` is refused. Use `… remove everything`, which filters can
    narrow (`!invasion remove everything female`, `!nest remove everything
    gen1`).

  Previously a bare `!quest`/`!gym`/`!fort`/`!invasion remove` removed
  everything, and a bare `!raid`/`!maxbattle`/`!nest remove` replied 👌 and
  removed nothing. `/untrack` always sends `id:N` and is unaffected (#347).
- **Unused tracking arguments are reported, not ignored.** `!track … shiny`,
  `gen`/type on `!raid`, and an unknown `move:` on `!raid`/`!maxbattle`
  (which used to mean "any move") are now refused. An unknown pokemon in
  `!quest energy:<x>` / `candy:<x>` is refused instead of widening to any
  pokemon. `!maxbattle` with a gen or type and no named pokemon is refused.
- **`[discord]` / `[telegram] enabled = false` stops that platform's alerts**,
  including quest summaries. Previously it only stopped the bot's commands. See
  the upgrade note about the example config's `[telegram] enabled = false`.
- **A full delivery queue drops its oldest alert instead of stalling every
  destination.** One destination taking repeated rate limits (e.g. a Telegram
  group getting 429s) used to fill its queue and block all render workers, so
  unrelated destinations stopped getting alerts. Now the oldest queued alert
  for that destination is dropped and counted. Rate-limit notices and ban
  farewells are never dropped.
- **Rarity tier 6 (Unseen).** Once enough data has been gathered, species with
  no sightings in the window are tier 6, so `rarity:6` matches. Rules with a
  minimum rarity (e.g. `rarity:rare`) now also fire for unseen species.
  `rarity:very-rare` / `ultra-rare` and `very_rare` now parse.
- **`!restore` replaces instead of merging.** In one transaction it clears every
  tracking table for the target's current profile and refills it from the
  file. Table and column names in the file are validated first; an unknown name
  rejects the whole restore before anything changes.
- **`!script` reproduces every rule setting.** Per-rule `area:` / `location:`
  overrides, pings, quest `summary`, `template:` and `clean` on every type,
  and quest `shiny`/`form:`/`amount:` were dropped. Quest wildcards are written
  as `all_pokemon` / `all_items`, items by name, `!maxbattle` and `!fort` lines
  are emitted, and multi-word values are quoted (#329).
- **`!start` and `!enable` clear the user's rate-limit violation history**, so a
  re-enabled user can be banned again if they flood. `!enable`, `!disable` and
  `!unregister` report unknown IDs as "Not registered" instead of claiming
  success.
- **`user`/`name` target overrides are stricter.** Only `user:<numeric id>`
  overrides the target, so words like `Userton` are no longer read as one.
  Bare `name<X>` overrides only when `X` is an existing named target, so words
  like `namco` stay in the arguments. `name:X` is unchanged.
- **`{{map 'x' key}}` with an unknown key renders nothing** instead of echoing
  the key, matching PoracleJS.
- **`previousControlTeamEmoji` shows the previous controller's team**, as its
  name says. It showed the old team.
- **Template fallback messages say why**: "Template not found", "Template
  render error" or "Template produced invalid JSON".
- **Maintenance pause messages say alerts are dropped** while paused, which is
  what happens (they said "queued").
- **`/fort` drops the `clean` option**, which `!fort` never accepted, and gains
  the Station and Everything fort types. Slash commands re-register on the
  next start.
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
- **Startup refuses to skip a dirty migration.** It retries a dirty 000009 and
  stops with an error on any other dirty version, instead of silently marking
  it applied.
- **Area spelling.** Stored area names are canonical fence names (lowercase);
  spaces versus underscores is only a comparison key. State loading normalises
  `override_areas`, so overrides stored with underscores now match; set-areas,
  community `allowed_areas`, and `!area add/remove` accept either spelling.
  v1 still writes and returns spaces.
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
- **Reconcilers prune before saving a community-membership change.** If the
  prune fails, `community_membership` / `area_restriction` are not saved and
  the change is retried at the next reconcile (member event or
  `check_role_interval` sweep).
- **Go 1.27** is required to build from source. Dependencies were refreshed.

#### API

- **BREAKING: v2 has its own human and profile shapes** (it was returning the
  v1 wire format). On the human (`GET /v2/humans/{id}`, `POST /v2/humans`,
  the snapshot's `human`): `enabled` and `admin_disable` are booleans;
  `area`, `community_membership` and `blocked_alerts` are arrays (`[]` when
  empty); `area_restriction` is an array or `null` (`null` = unrestricted,
  `[]` = no areas allowed); an unset `language` is `""`. Profile `area` is an
  array, snapshot `profiles[].active_hours` is the typed array, and snapshot
  `summaries[]` is `{alert_type, active_hours}` with ISO weekdays.
- **BREAKING: v2 coordinates are `latitude` / `longitude`.** This applies to
  the bodies of `POST …/location`, `POST …/locations` and
  `PUT …/locations/{label}`, and to the `check-location` query
  (`?latitude=&longitude=`). `POST /v2/humans` `area` is an array.
- **BREAKING: v2 `PUT …/tracking/{type}/{uid}` keeps the rule's `uid`.** It
  used to return a new one.
- **BREAKING: v2 `active_hours.day` is ISO 1–7** (1 = Monday, 7 = Sunday).
  `day: 0` is rejected, where it used to store a schedule that never fired.
- **BREAKING: v2 invasion rules are read as `grunt_type` only.** `type_id`
  and `everything`/`boss` are no longer returned, so every stored grunt type,
  including leaders and `npc_*`, has a read form. Catch-all rules return their
  gender, and `gender` is accepted with every mode except `grunt_id` (#330).
- **BREAKING: v2 numeric tracking fields are bounded** (55 fields across all 11
  types). An out-of-range value is a 422 naming the field. A rule stored out of
  range can no longer be PUT back unchanged.
- **BREAKING: `override_areas` validation now runs on v1 and v2 tracking
  writes.** It had never run. A v1 client sending an area the user may not
  select now gets 400 instead of 200 and a rule that never matched.
- **v2 pokemon `pokemon_id` is optional.** Omitted or `null` is the catch-all
  (stored as 0, read back as `null`), so a catch-all rule round-trips again.
- **v2 language** is validated against the loaded locales.
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
- **BREAKING: maxbattle edits now squash.** `clean`, `distance` and `template` are
  updatable fields for maxbattle, as for raid: re-posting a rule with one of
  them changed updates the rule in place instead of adding a copy. Within one
  batch an existing row is an update target for at most one candidate.
- **v2 `edit: true` is refused (422) for gym, invasion, nest, maxbattle and
  quest**, whose alerts never edit. Those types read back `edit: null`, so
  legacy rules still round-trip.
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
- **v2 `…/areas` set (`POST`) with `?all_profiles`** is documented as a
  whole-list replace.
- **Wildcard contract.** `null` on a v2 rule means unconstrained; clients never
  need the stored sentinel, which may change without notice. Enforced by a test
  that every nullable field on all 11 types reads back `null` and round-trips.
- **`showcase` is listed in `disabledHooks`.**

### Fixed

#### Alerts

- **Weather-change alerts were dropped** when Golbat re-sent pokemon under the
  new weather before the change was processed: nothing appeared to flip boost,
  so every user's alert was skipped.
- **Weather-change alerts showed no affected pokemon** unless
  `show_altered_pokemon_static_map` was on. `activePokemons` is now set whenever
  `show_altered_pokemon` is on; the static-map flag only controls the map.
- **AccuWeather forecasts no longer cause false weather-change alerts.** With
  `[weather] enable_forecast = true`, forecast hours were stored alongside the
  weather the game reported, so a forecast that disagreed with the game became
  the baseline for change detection: the next weather webhook, and then
  boosted-pokemon inference, each alerted a change that never happened. A
  forecast fetch could also overwrite the current hour, making `gameWeatherId`
  show the forecast. Forecasts are now kept separately and only fill hours the
  game hasn't reported, for the next-hour prediction and `GET /api/weather`
  (#356).
- **Same-CP species changes** (e.g. a Ditto reveal) were dropped as duplicates
  and never reached `monsterChanged`.
- **PVP rules stored with `pvp_ranking_worst = 0` now match.** `0` is not a
  rank; the bot already reads it as "no upper limit" on input and v2 reports it
  as `null`, but the matcher dropped every rank above 0, so such a rule never
  fired. Rules now load with `0` treated as `4096` (no limit).
- **Rate-limit bans fired over and over.** Once a user crossed
  `max_limits_before_stop`, every later breach re-ran the ban, filling the
  shame and admin channels with identical messages. The ban now fires once per
  violation window.
- **Telegram: a failed sticker or photo dropped the whole alert.** It is now
  skipped with a warning and the rest of the alert is sent; a captioned photo
  that fails falls back to sending its caption as text. Non-429 4xx errors are
  no longer retried. Edits keep link previews, so a map shown as a hidden link
  survives an edit.
- **Nest static maps** always fell back to the default map: the tileserver
  template was never sent the map centre, zoom or outline.
- **Quest summaries failed to render** when a pokestop name contained a double
  quote. Stop names in summaries and max battle station names are now escaped.
- **Quest summaries ignored mutes, validation and platform `enabled`.** Mutes
  now apply when the summary is sent, validation when the quest is buffered,
  and a disabled platform is checked at both points.
- **The `unsubscribe` button failed on pokemon and fort-update alerts**
  ("unknown alert type").
- **Template selection ignored a default in the user's language** when a
  language-less default also existed.
- **Template helpers inside blocks.** Inline helpers inside `{{#if}}`,
  `{{#each}}`, `{{else}}` or comparison blocks could render empty (e.g. `{{map
  …}}` inside `{{#isnt …}}`). A helper used as a block's argument
  (`{{#eq (pokemon id 0) 'Pikachu'}}`) always took the else branch. Scalar
  `{{#each}}` items reached helpers wrapped, so `{{gt this 1}}` was always
  false. Large numbers used as map keys no longer turn into scientific notation.
- **A missing helper argument rendered as `[]`**; it now renders empty.
- **Fields that rendered empty:** `{{latitude}}`/`{{longitude}}` on fort-update
  alerts, raid `{{ex}}`, `oldTeamEmoji`. Maxbattle `{{id}}` is the boss
  pokemon id, not the station id.
- **Crashes:** `{{#pokemon id}}` without a form, and `trace` logging, panicked.
  Help, the `!poracle` greeting and reconciliation greetings now render with
  the language frame and panic recovery, and reconciliation greetings use
  `[general] locale`.

#### Commands

- **`!lure remove id:N` never worked**, so removing lure rules from the `/untrack`
  picker failed. `!maxbattle remove level:N` replied 👌 and removed nothing.
- **Slash commands that failed outright:** `/track size:` (any size other than
  "All"), a typed `form:`/`costume:` with capitals, and an `iv:` value such as
  `100%` aborted the command. `/profile copyto` failed on every autocomplete
  pick. The `settime` autocomplete was unreachable.
- **Slash area pickers:** `/area add` offered the areas you already had, and
  neither picker could build a multi-area list. `/area add`/`remove` accept
  comma-separated lists, and duplicate `area:` entries are removed.
- **The `everything` guard** let `everything size:m` and
  `everything rarity:common` through, which subscribe to nearly every spawn.
- **Mute aliases and area unmute.** `!raid mute gym X` and other per-type
  aliases only understood `id:N`; `!track mute id:N` failed; `!unmute area`
  was case- and underscore-sensitive.
- **Other remove and argument bugs** (#347): a typo in `!quest remove`,
  gym/egg `remove everything`, `!track remove id:N`, extra quest items and
  fort types being dropped, and `!info form:`.
- **`!pa config`** section lookup and search ignore case and treat `_` and
  spaces alike, and its output is in code blocks so Telegram no longer turns
  `snake_case` into italics.
- **`!channel add`** keeps underscores in `area:` and `name:` values.
- **`/profile copyto` autocomplete** no longer offers the current profile.

#### Database and API

- **Migration 000009 no longer fails on legacy mixed-collation databases.**
  On databases adopted from Knex whose tables use different collations, it
  failed with "Illegal mix of collations". It now normalises IDs before
  comparing them and commits its data changes in one transaction. A database
  where an older build skipped a failed 000009 needs a manual reset to
  version 8; see `docs/migration-collations.md`.
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
- **`!profile add` reported the wrong profile number** when an older profile
  had the same name.
- **Config editor defaults** were wrong for several keys, so saving e.g.
  Telegram `enabled = false` in the editor deleted the line and the code
  default came back. `config.example.toml` no longer contains keys the code
  ignores.

### Removed

- **`!weather`**, which always replied "unavailable". `!info weather` and
  `/info weather` are unaffected. `weather` is no longer in backups, and
  `!restore` ignores it in older backup files.

### Security

- **IP whitelist/blacklist could be bypassed** by sending `X-Forwarded-For`.
  New `[processor] trusted_proxies` (IPs or CIDRs; empty by default, which
  trusts no proxy). `ip_whitelist` / `ip_blacklist` accept CIDR ranges and
  IPv6, and an invalid entry in any of the three lists stops startup (#264).
- **`/debug/pprof` was served without authentication.** It is now mounted only
  with `[processor] enable_pprof = true`, behind `api_secret` (#265).
- **Startup warns when the API is open** (blank `api_secret`) on a
  non-loopback address, including Docker's `0.0.0.0` (#266).
- **`!restore` put table and column names from the backup file straight into
  SQL.** Names are now checked against an allow-list and the table's real
  columns (#267).

## [5.2.1] - 2026-08-23

### Added

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

### Deprecated

- **v1 tracking/humans/profiles endpoints are frozen and
  deprecated-but-supported.** `/api/tracking/*`, `/api/humans/*`,
  `/api/profiles/*` (and `/api/tracking/pokemon/refresh`) remain on gin,
  unchanged and fully functional — clients migrate to `/api/v2` on their own
  schedule. Migration is encouraged to access new tracking types (e.g.
  `incident`) and the cleaner strict contract. No sunset date is set yet.
