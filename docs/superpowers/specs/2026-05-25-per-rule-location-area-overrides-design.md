# Per-rule location & area overrides

**Date:** 2026-05-25
**Status:** Design approved, awaiting implementation plan

## Problem

Today every tracking rule for a user uses that user's *single* location and *single* area set for distance/area matching. Users who care about more than one geography (work, home, somewhere they visit weekly) have to switch profiles or maintain duplicate accounts. This is heavy ceremony for what should be a per-rule annotation: "alert me about iv100 within 500m of Home, and about raids in the Ashford geofence regardless of where I am."

## Goals

- Let users save **named locations** (e.g. `Home`, `Work`) once and reference them on tracking rules
- Let users specify **per-rule area overrides** that replace (not merge with) their human-level areas for that rule only
- Default behaviour (no override) is unchanged — existing rules continue to use the human's location + areas
- The matcher-side change is a single site; all 10 tracking types pick it up uniformly

## Non-goals

- Renaming a saved location (delete + add for v1)
- A `force` flag on `!location remove` (refuse-when-referenced is enough)
- Per-profile named-locations index (profiles still hold their single override location, unchanged)
- Editing the override on an existing rule without re-issuing the full tracking command (consistent with the existing UX where rules are recreated, not edited)

## Override chain

At match time, both **areas** and the **location/distance anchor** resolve through a three-layer chain, highest wins, NULL = fall through:

```
tracking-rule override   →   profile (existing)   →   human (existing)
```

Today: profile overrides human via mutation at profile-switch time (the profile's lat/lon/area are copied onto the human row in `SetActiveProfile`). This design adds: tracking-rule override read at match time from two new nullable columns; no mutation, no copy.

## Command surface

### `!location` (existing behaviour preserved, new subcommands added)

| Command | Effect |
|---|---|
| `!location` | Show default location + usage *(existing)* |
| `!location <coords-or-place>` | Set default location *(existing)* |
| `!location add <name> <coords-or-place>` | Save named location. Name is the first positional arg, location string is everything after. Mirrors `!area add <name>` style. Quoted names supported: `!location add "Holiday Home" santa monica` |
| `!location list` | List default + all named locations |
| `!location show Home` | Show one named location's coords, address, map link |
| `!location remove Home` | Remove named location by positional arg. Refuses if any tracking rule references it; reply lists which rules + their types |
| `!location remove default` | Clear the default location *(replaces the old bare `!location remove`)* |
| `!location remove` *(bare)* | Error, show usage |

### Tracking-rule params (all 10 trackers: pokemon, raid, egg, quest, invasion, lure, nest, gym, fort, maxbattle)

| Combination | Behaviour |
|---|---|
| `d:500` alone *(existing)* | Distance from human.location |
| `location:Home d:500` | Distance from Home's coords |
| `location:Home` (no `d:`) | **Reject** — "`location:` requires `d:`" |
| `area:Berlin` (single) | Area-mode override; replaces user's areas for this rule |
| `area:Berlin area:Munich` | Area-mode, repeated tokens accumulate |
| `area:Berlin,Munich` | Area-mode, comma-separated tokens accumulate (both forms work, mix freely) |
| `area:Berlin d:500` | **Reject** — "`area:` and `d:` are mutually exclusive (area-mode vs distance-mode)" |
| `area:Berlin location:Home` | **Reject** — "pick one of `area:` or `location:`" |

Areas in `area:` are validated against the user's currently-permitted areas (community membership in area-security mode, else all loaded geofences); reject the **whole command** with a clear error if any are not permitted. Labels in `location:` are validated against the user's saved-locations index; reject with "no saved location named `X`. Add it with `!location add ... label:X`."

## Schema

### `user_locations` (new table)

Column naming follows project convention (`uid` = PK, `id` = human cross-ref). No `FOREIGN KEY` clause — referential integrity is enforced manually via the existing delete-cascade routine.

```sql
CREATE TABLE user_locations (
  uid        INT PRIMARY KEY AUTO_INCREMENT,
  id         VARCHAR(50) NOT NULL,            -- human id (cross-ref, no FK)
  label      VARCHAR(64) NOT NULL,
  latitude   DOUBLE NOT NULL,
  longitude  DOUBLE NOT NULL,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  UNIQUE KEY uniq_id_label (id, label),
  INDEX idx_id (id)
);
```

- Labels stored case-preserved for display; uniqueness check + lookup are case-insensitive (matches the existing `!area add` lowercase-for-matching convention)
- Cleanup on human delete: add `"user_locations"` to the existing manual-cascade slice in `processor/internal/db/human_queries.go` AND `processor/internal/store/human_sql.go`. The slice is misnamed for this purpose now ("trackingTables" implies tracking-rule tables) — rename to `humanOwnedTables` while we're in there

### Tracking-table columns (added to all 10 tracking tables)

```sql
ALTER TABLE <each_of: monsters, raid, egg, quest, invasion, lures, nests, gym, forts, maxbattle>
  ADD COLUMN override_location_label VARCHAR(64) NULL,
  ADD COLUMN override_areas          TEXT NULL;  -- JSON array, same shape as humans.area
```

NULL on both = "fall through to profile, then human".

### Migration

Single migration file `00000X_per_rule_overrides.up.sql` / `.down.sql`. One file because the columns are conceptually one feature. MySQL implicitly commits each DDL statement, so partial-failure on the 7th of 10 ALTERs would leave the schema half-applied. Practical risk is low (all 10 ALTERs are the same shape — they pass or fail uniformly), but operators should monitor the migration log on first run. The DSN's `multiStatements=true` handles execution on fresh installs.

## Matcher integration

Single change site in `processor/internal/matching/generic.go` (`ValidateHumansGeneric`):

```go
// Determine anchor location for distance check
anchorLat, anchorLon := human.Latitude, human.Longitude
if rule.OverrideLocationLabel != "" {
    if loc, ok := human.Locations[strings.ToLower(rule.OverrideLocationLabel)]; ok {
        anchorLat, anchorLon = loc.Latitude, loc.Longitude
    }
    // else: orphaned ref — silently fall through to human. The delete-refuse
    // guard means this only happens if the DB was edited externally.
}

// Determine effective area set for area check
effectiveAreas := human.Area
if len(rule.OverrideAreas) > 0 {
    effectiveAreas = rule.OverrideAreas
}

// Existing distance-vs-area branch, but using anchorLat/Lon and effectiveAreas
if rule.Distance > 0 {
    dist := HaversineDistance(anchorLat, anchorLon, ev.Lat, ev.Lon)
    if dist > rule.Distance { continue }
} else {
    if !areaOverlap(effectiveAreas, matchedAreaNames) { continue }
}
```

All 10 tracking types pick this up uniformly since they all flow through `ValidateHumansGeneric`.

## State loading

- `db.LoadAll()` gains a `LoadUserLocations()` query returning `map[humanID]map[lowerLabel]*UserLocation`
- Each loaded human gets a `human.Locations` field populated at state-build time
- Atomic swap with the rest of state — no separate refresh path
- `!location add` / `!location remove` call the existing debounced `triggerReload()` so override resolution sees new locations within 500ms

## Per-tracking-rule structs + API DTOs

Every `*Tracking` struct and `*TrackingAPI` struct gains:

```go
OverrideLocationLabel string   `db:"override_location_label" json:"override_location_label"`
OverrideAreas         []string `db:"-"                       json:"override_areas"`  // marshalled separately, JSON column
```

`flexBool` / `flexInt` JSON type coercion in `api/tracking.go` continues unchanged. `tracking.ApplyDiff` picks up the new fields via its `diff:` struct-tag walker — no per-type diff logic changes needed.

## Reconciliation (area-security mode)

When a user loses access to an area (community-role change, manual `!area remove`), the existing reconciler walks `human.Area`. Extension: also walk every tracking row's `override_areas` for that user; drop any area now disallowed; if the override list becomes empty, NULL the column (rule falls back to human areas).

- Adds one query per affected user during reconciliation
- Piggybacks on the existing `AreaLogic.ValidateAndPrune` path
- No effect when area-security mode is off

## REST API

New endpoints nested under the existing `/api/humans/{id}` resource. Follows project verb-in-path style (matches `/api/profiles/{id}/add`, `.../update`, `.../delete`, `DELETE .../byUid/{uid}`):

| Method | Path | Purpose |
|---|---|---|
| `GET`  | `/api/humans/{id}/locations`                  | List all named locations + the default. Response envelope: `{locations: {default: {latitude, longitude}, named: [{label, latitude, longitude, created_at}, ...]}}` |
| `POST` | `/api/humans/{id}/locations/add`              | Create one or more named locations. Body: single `{label, latitude, longitude}` OR `{label, place}` (server geocodes), or an array. 409 (in the per-row response) if a label already exists for this user. Mirrors the create-or-batch-create shape used by `POST /api/tracking/{type}/{id}` |
| `GET`  | `/api/humans/{id}/locations/{label}`          | Show one named location. Label lookup is case-insensitive. 404 if missing |
| `POST` | `/api/humans/{id}/locations/{label}/delete`   | Remove named location. 409 with `{referencing_rules: [{type, uid, ...}]}` body if any tracking rule references it; otherwise standard success envelope |

The existing `POST /api/humans/{id}/setLocation/{lat}/{lon}` continues to set the **default** location, unchanged.

**Conventions matched from the rest of the API surface:**
- Verb-in-path (`/add`, `/delete`) rather than pure REST DELETE on the resource — matches profiles/tracking
- Response envelope by resource name (`{locations: ...}`) — matches `{pokemon: [...]}`, `{raid: [...]}` from tracking GETs
- Success / error responses go through the same `trackingJSONOK` / `trackingJSONError` helpers used elsewhere
- Lookup-by-natural-key directly in the path (`{label}`) rather than a `byLabel/{label}` segment — there's no ambiguity to resolve (unlike `byUid/{uid}` which disambiguates UID from content-field POSTs)

**Tracking-rule CRUD endpoints** (`POST /api/tracking/{type}/{id}` for all 10 types) accept the two new fields on each tracking-rule object in the request body:

```json
{
  "uid": 123,
  "pokemon_id": 25,
  "distance": 500,
  "override_location_label": "Home",   // string or null
  "override_areas": ["berlin", "munich"], // array of lowercased area names, or null
  ...
}
```

Server-side validation mirrors the bot-command checks:
- `override_location_label` set + `distance == 0` → 400 "location requires distance"
- `override_areas` non-empty + `distance > 0` → 400 "area and distance are mutually exclusive"
- `override_location_label` set + `override_areas` non-empty → 400 "pick one of location or area"
- `override_location_label` references a label not in the user's saved locations → 400 "unknown location label"
- Any `override_areas` entry not in the user's currently-permitted areas → 400 "area not permitted"

`flexBool` / `flexInt` JSON coercion in `api/tracking.go` continues to apply to existing fields; the two new fields are plain string / string-array and don't need coercion.

## Slash commands

- New `/location` with subcommands `add`, `list`, `show`, `remove`, `set-default`, `remove-default`
  - `add` takes `place` + `label` options
  - `show` / `remove` take a `label` option with autocomplete from the user's saved locations
- Existing `/track`, `/raid`, `/egg`, `/quest`, `/invasion`, `/lure`, `/nest`, `/gym`, `/fort`, `/maxbattle` gain two new options:
  - `location` — autocomplete from the user's labels
  - `areas` — autocomplete from user's allowed areas; multi-select via comma-separated text (Discord doesn't have native multi-select on string options)
  - Same mutually-exclusive validation as text commands, enforced server-side
- Slash command mappers (`processor/internal/discordbot/slash/mappers/*.go`) gain wiring for the two new fields

## Rowtext display

`!tracked` output gains override indicators on each affected row. New i18n keys: `tracking.override_location_fmt` ("@ {0}"), `tracking.override_areas_fmt` ("in {0}"). Existing `RowText.*RowText` helpers gain the format calls; no per-type duplication.

## i18n surface

New keys for `!location add/list/show/remove` replies, the rejection messages, and the rowtext additions:

**`!location` command replies** (under `msg.location.*`):
- `msg.location.added`
- `msg.location.duplicate`
- `msg.location.list_header` / `msg.location.list_row` / `msg.location.list_default` / `msg.location.list_empty`
- `msg.location.show` / `msg.location.show_not_found`
- `msg.location.removed` / `msg.location.default_removed` / `msg.location.remove_referenced`
- `msg.location.add_usage` / `msg.location.show_usage` / `msg.location.remove_usage`
- `msg.location.geocode_failed`

**Override validation rejection messages** (under `msg.override.*` — not `msg.location.*`):
- `msg.override.requires_distance` — `location:` used without `d:`
- `msg.override.area_and_distance` — `area:` + `d:` are mutually exclusive
- `msg.override.area_and_location` — `area:` + `location:` are mutually exclusive
- `msg.override.unknown_location` — label not in user's saved locations
- `msg.override.area_not_permitted` — area not in community's allowed areas

**Rowtext display** (under `tracking.*`):
- `tracking.override_location_fmt`
- `tracking.override_areas_fmt`

English source only as part of this PR; Crowdin picks up the rest.

## Testing surface

### Unit tests
- `matching/generic_test.go`:
  - Override-location anchors distance check
  - Override-areas replaces (not merges with) human areas
  - Orphaned label (label not in human.Locations) falls through to human
  - Rule with only override-areas fires correctly when human has no areas of its own
- `bot/commands/location_test.go`:
  - `add`/`list`/`show`/`remove` happy paths
  - Refuse-delete-when-referenced (with rule-count and types in error message)
  - Case-insensitive label match on `show` / `remove`
- `bot/commands/track_test.go` plus the 9 other tracker commands' test files:
  - `area:` + `d:` rejected
  - `location:` without `d:` rejected
  - `area:` + `location:` rejected
  - Valid combos parse and populate the right fields on the tracking row
- `store/tracking_sql_test.go`: round-trip the new columns including NULL handling

### API tests
- `api/locations_test.go`: list/create/show/delete happy paths; 409 on duplicate label; 409 on delete-while-referenced (with referencing-rules payload); 404 on missing label; case-insensitive lookup
- `api/tracking_test.go`: the 5 server-side override validation rules return 400 with the expected error key; valid override populates the row

### Integration
- End-to-end webhook flow through processor with an override-set rule that should fire and one that should not

### Smoke checklist
Document at `docs/superpowers/specs/2026-05-25-per-rule-location-area-overrides-smoke.md` covering: create location, list, show, reference on rule, refuse-delete, set up area override, exercise area-security pruning, slash-command parity.

## Out of scope (explicit deferrals)

- Location rename (delete + add for now)
- `force` flag on `!location remove` (refuse-with-list is sufficient for v1)
- Per-profile named-locations index (profiles still hold their single override location, unchanged)
- Reverse-geocode display on saved locations beyond what `!location show` does once
- Editing an override on an existing rule without re-issuing the full tracking command (consistent with existing UX — rules are immutable; users create new with same key + new override)
