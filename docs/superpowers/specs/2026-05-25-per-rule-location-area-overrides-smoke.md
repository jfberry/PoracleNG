# Per-rule location & area overrides — Smoke checklist

Run through these in a real Discord/Telegram + processor + MySQL setup
to validate the feature end-to-end before merging.

Branch: `per-rule-location-area-overrides`. Migration `000004_per_rule_overrides` adds the `user_locations` table and `override_location_label` + `override_areas` columns on all 10 tracking tables.

## !location surface

- [ ] `!location add Home canterbury` → reply confirms save with coords
- [ ] `!location add "Holiday Home" santa monica` → quoted multi-word name works
- [ ] `!location add Home 0,0` → rejected as duplicate (already exists)
- [ ] `!location list` → shows default + named, sorted by label
- [ ] `!location show home` → case-insensitive lookup; shows coords + map link
- [ ] `!location show Nope` → "No saved location" error
- [ ] `!location remove default` → clears the default location
- [ ] `!location remove` (bare) → usage error
- [ ] `!location remove Home` → succeeds when no rules reference it
- [ ] `!location` (bare, with default set) → shows current default + usage hint (unchanged from before)
- [ ] `!location 51.5,-0.1` → sets default (unchanged)

## Tracking rules with override

For each command (`!track`, `!raid`, `!egg`, `!quest`, `!invasion`, `!lure`, `!nest`, `!gym`, `!fort`, `!maxbattle`):

- [ ] `<cmd> ... d:500 location:Home` → rule stored with `override_location_label="Home"`
- [ ] `<cmd> ... location:Home` (no `d:`) → rejected ("needs a `d:`")
- [ ] `<cmd> ... d:500 area:london` → rejected (a+d mutually exclusive)
- [ ] `<cmd> ... area:london area:berlin` → rule stored with both areas in JSON
- [ ] `<cmd> ... area:london,berlin` → rule stored with both areas (comma-split)
- [ ] `<cmd> ... area:berlin location:Home` → rejected (mutually exclusive)
- [ ] `<cmd> ... area:NotPermitted` → rejected ("not in your allowed areas")
- [ ] `<cmd> ... d:500 location:Nope` → rejected ("No saved location")

After successful creates: `!tracked` shows the rule with `@ Home` or `in london, berlin` appended.

## Refuse-when-referenced

- [ ] Create an override rule referencing "Home"
- [ ] `!location remove Home` → rejected with list of referencing rule type/uid
- [ ] After removing the referencing rule, `!location remove Home` → succeeds

## Webhook matching

- [ ] Trigger a webhook within 500m of "Home" coords → alert fires (rule with `location:Home d:500`)
- [ ] Trigger a webhook outside that radius → alert does not fire even if within the human's default-location distance
- [ ] Trigger a webhook in the "berlin" geofence → rule with `area:berlin` fires even if the human's areas don't include berlin
- [ ] Rendered message shows distance from the override location (not the human default)
- [ ] Rendered message bearing reflects the override location

## REST API

- [ ] `GET /api/humans/u1/locations` → envelope `{"locations": {"default": {...}, "named": [...]}}`; default omitted when unset
- [ ] `GET /api/humans/u1/locations/home` → case-insensitive 200 with `{label, latitude, longitude}`
- [ ] `GET /api/humans/u1/locations/nope` → 404 with error body
- [ ] `POST /api/humans/u1/locations/add` with single body `{"label":"Home","latitude":51.5,"longitude":-0.1}` → 200 + persisted
- [ ] `POST /api/humans/u1/locations/add` with array body → 200 + multi-row results
- [ ] `POST /api/humans/u1/locations/add` with duplicate label → row reports `"duplicate"` in `error` field
- [ ] `POST /api/humans/u1/locations/add` with `place` field (no lat/lon) → row reports `"place geocoding not yet supported"` (v1 limitation)
- [ ] `POST /api/humans/u1/locations/Home/delete` when no rules reference it → 200
- [ ] `POST /api/humans/u1/locations/Home/delete` while referenced → 409 + `referencing_rules` body with `[{type, uid}]` array
- [ ] `POST /api/tracking/pokemon/u1` body with `override_location_label` + `distance: 0` → 400 "requires distance"
- [ ] Same body with `override_location_label` + `distance: 500` + valid label → 200 + persisted row carries override
- [ ] Same body with `override_location_label` referencing unknown label → 400 "unknown location label"
- [ ] Same body with `override_areas` + `distance > 0` → 400 "mutually exclusive"
- [ ] Same body with `override_areas` containing a not-permitted area → 400 "area not permitted"

## Slash commands

- [ ] `/location add name:Home place:51.5,-0.1` → works end-to-end (mirrors `!location add`)
- [ ] `/location list` → mirrors `!location list`
- [ ] `/location show name:<partial>` → autocomplete suggests user's saved labels
- [ ] `/location remove name:Home` → mirrors `!location remove Home` (refuses if referenced)
- [ ] `/location remove-default` → clears the default location
- [ ] `/track` with `location:Home` option + `d:500` → rule stored with override
- [ ] `/track` location-option autocomplete suggests saved labels
- [ ] `/track` with `areas: berlin,munich` → parses comma-separated correctly
- [ ] `/track` areas-option autocomplete suggests permitted area names
- [ ] Mutually-exclusive checks fire server-side on slash too (same error messages)
- [ ] All 10 tracker slash commands accept both options (`/track`, `/raid`, `/egg`, `/quest`, `/invasion` and `/incident`, `/lure`, `/nest`, `/gym`, `/fort`, `/maxbattle`)

## Reconciliation (area-security mode only)

- [ ] User loses access to area X (community-role change or manual `!area remove`)
- [ ] Reconciliation runs (manual or scheduled)
- [ ] Rules with `override_areas: ["X"]` are NULLed (override falls back to human areas)
- [ ] Rules with `override_areas: ["X", "Y"]` become `["Y"]`
- [ ] User's tracking rules continue to function with their remaining permitted areas

## Cascade on user delete

- [ ] Delete a user via the existing delete-human routine
- [ ] `user_locations` rows for that user are removed
- [ ] No orphaned `user_locations` rows remain (`SELECT * FROM user_locations WHERE id = '<deleted-id>'` returns 0 rows)

## Profile interaction

- [ ] Switching profile does NOT clear per-rule overrides (they're per-rule, not per-profile)
- [ ] Per-rule overrides apply consistently across all of the user's profiles
- [ ] Profile's own area/lat/lon override (the existing layer) still works — overrides are layered: tracking-rule > profile > human

## Edge cases

- [ ] Bare-remove on the i18n key `msg.location.default_removed` works correctly (regression check from the Task 8 bridging fix)
- [ ] Removing a saved location with the wrong case (`!location remove HOME` when saved as `Home`) still matches via case-insensitive lookup
- [ ] Inserting a tracking rule via API with `override_areas: []` (empty array) treats it as no override (NULL persisted)
- [ ] Inserting a rule with `override_areas: ["BERLIN", "munich"]` (mixed case) lowercases before validation but preserves the user's input case in storage (matches `!area add` convention)
