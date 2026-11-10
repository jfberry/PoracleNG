---
name: new-tracking
description: Generate a task checklist for adding a new tracking type or a new Golbat webhook type (alert type) to PoracleNG. Use when planning implementation across the database, processor, matching, enrichment, v2 API, bot and slash commands, mutes/buttons, test data, DTS templates and the config editor.
user-invocable: true
argument-hint: [tracking-type-name] [optional-reference-pr-url]
allowed-tools: Read, Grep, Glob, Bash(gh *), Bash(git *), WebFetch, TaskCreate
---

# Add New Tracking Type to PoracleNG

PoracleNG is a single Go binary. A new alert type touches far more than the matcher: the DB table, state, store, v2 API, the text command, the slash command, mutes and buttons, `!tracked`/`!script`/`!backup`, the test-alert and editor plumbing, help templates, and docs. Many of these are **hand-maintained lists of type names** spread across packages, and a missed list rarely breaks the build. It shows up later as a silent gap, like a type missing from `!untrack`, unsubscribe deleting from the wrong table, or the config editor not showing the toggle.

There are three shapes of work, so establish which one you have first:

- **New webhook and new table** (e.g. `maxbattle`): everything below.
- **New webhook, existing table** (e.g. `showcase`: a new Golbat payload that alerts against the existing `invasion` rules). Do the webhook, enrichment, DTS, test-data and config sections, and skip the table, store, API and command sections.
- **New tracking type on an existing table** (e.g. v2 `incident`: a v2 type that shares the `invasion` table through a `Filter` discriminator). Do the v2, mutes and docs sections, and skip the DB and webhook sections.

**Tracking type name:** $0
**Reference PR (optional):** $1

A type can have up to five different names. Decide all of them up front and record them in the plan. fort shows how far they can diverge:

| Name | fort | maxbattle | Used by |
|------|------|-----------|---------|
| **Type** ($0, singular, also the command verb) | `fort` | `maxbattle` | command, v2 path, `RuleTable`, `mute.ruleTables`, `TablesWithUID`, `blocked_alerts` / `command_security`, `hookTypes`, metrics |
| **Table** (`<table>`) | `forts` | `maxbattle` | SQL, the §1 table lists |
| **Wire** (Golbat `"type"`) | `fort_update` | `max_battle` | receiver case, `disable_<wire>` flag, `filterValidation`, testdata `type` |
| **DTS template type** | `fort-update` | `maxbattle` | `fallbacks/dts.json`, `fieldsByType`, `view.go` aliases, dtsmap `TemplateType`, `RenderJob.TemplateType`, slash `dtsTypeFor` |
| **Maptype** | `fort-update` | `maxbattle` | the `enrichment.go` static-map switch, `tileserver_templates/*/poracle-<maptype>.json`, the `static_map_type` keys |

For a new type, keep these names the same wherever you can.

## Instructions

1. If a reference PR URL is provided ($1), fetch the diff and description first.
2. **Get a real sample payload** before anything else: one or more raw Golbat webhooks of the new type (enable `[webhookLogging]` and copy the entries from `logs/webhooks.log`), plus a link to the Golbat code or docs that emit it. The webhook struct, the dedup key, the enrichment and `testdata.json` all depend on the exact field names and on which fields are optional, and this skill can't supply them. If the user doesn't have a sample yet, stop and ask.
3. **Run the Characteristics interview below with the user before generating tasks.** The answers decide which optional steps apply and which existing type is the best reference. Ask rather than guess.
4. Read the closest existing type end to end (see "Reference files"). Mirror the actual code and don't extrapolate. Line numbers drift, so this skill names **symbols, not line numbers**. Grep for them.
5. **Run the touchpoint sweep** (next section) against the reference type. It is the ground truth, and this checklist only explains it. Every hit that is not specific to the reference type's own files is a list the new type probably belongs in.
6. Substitute $0 into every checklist item, drop the sections that don't apply, and add any steps the reference PR requires. Present the full list before creating tasks.
7. Order the tasks so each step can be built and tested before the next: migration, DB, state and store (§1–3) → matcher (§4) → handler and enrichment (§5–6) → templates and test alerts (§12–13), at which point `!poracle-test` delivers end to end → v2 (§7) → bot command, rowtext and i18n (§8, §15, §16) → slash (§9) → buttons and mutes (§10–11) → config (§14) → docs (§17) → §18. Run the full gate (§18) at each checkpoint, not just at the end. Many of the hand-maintained lists only fail in tests.

## Touchpoint sweep (do this first)

Run this from the repo root.

```bash
# every non-test file that names the reference type: each is a candidate touchpoint
grep -rlI 'maxbattle\|max_battle\|MaxBattle\|Maxbattle' processor fallbacks config DTS.md API.md README.md TILESERVER.md docs/v2-api-design.md docs/slash-commands \
  | grep -v _test.go | sort
# hand-maintained table-name lists (tables are plural for some types: monsters, lures, nests, forts)
grep -rnI '"lures"\|"forts"\|"nests"' processor --include=*.go | grep -v _test.go
# places keyed by other types' alternate spellings (DTS name / wire name differ from type name for fort)
grep -rnI 'fort-update\|fort_update' processor fallbacks | grep -v _test.go
# TESTS that enumerate types: a type missing here is silently un-covered, or the test fails
grep -rlI 'maxbattle' processor --include=*_test.go | sort
# tile templates (rampardos ships a .testdata.json sibling)
ls tileserver_templates/*/poracle-maxbattle*
```

Compare the result against the checklist. If the sweep finds a file the checklist doesn't mention, add it to the plan and fix this skill afterwards.

## Characteristics: decide first

Ask the user each of these and record the answers in the plan, so the implementation phase doesn't have to work them out again.

| Question | Effect of "yes" | Pattern to copy |
|----------|-----------------|-----------------|
| **1. Webhook source.** Is this a new Golbat event type, or does it ride on an existing handler? | New: webhook struct, receiver case, handler, dedup, test plumbing. Piggyback: extend the existing handler to also run the new matcher. | New: `cmd/processor/maxbattle.go`, the `case "max_battle":` in `webhook/receiver.go`. New webhook on an existing table: `cmd/processor/showcase.go` (commit `fbeaebb0` plus its follow-ups `cf3cb353`, `5e7d106a`, `f69204de`). |
| **2. New table, or existing table?** | Existing table: no migration or store. A v2 type can share a table through `v2TrackingType.Filter`. | `api/v2_incident.go` (an incident `Filter` over the `invasion` table, with `invasion` using the inverse filter). |
| **3. v1 API endpoints?** | **Default no.** v1 (`/api/tracking/*`) is frozen and deprecated. `incident` exists only in v2. Add v1 only if a named client (e.g. PoracleWeb) can't move to v2 yet. | `api/trackingMaxbattle.go` and `api/trackingAll.go`, only if yes. |
| **4. Pokemon-by-ID filter** ("this pokemon" / "any pokemon")? | `pokemon_id` column with the `9000` "any" sentinel, pokemon-resolver wiring in the command, and a nullable `pokemon_id` in v2 using the `v2_wildcards.go` constants. | `db/maxbattle.go`, `matching/maxbattle.go`, `bot/commands/maxbattle.go`, `api/v2_maxbattle.go`. |
| **5. Form- or costume-aware?** | `form` column (0 = any). Costume adds a column, slash autocomplete and `!info` work. | Form: `maxbattle`. Costume: migrations `000006`/`000007` and the raid costume spec. |
| **6. Numeric range filters** (IV / level / CP)? | A pair of columns per range, with sentinels. v2 exposes them as nullable ints. | `db/monsters.go`, `api/v2_pokemon.go`. For a single threshold see maxbattle `level`. |
| **7. List filters** (multi-select IDs)? | A JSON array in a TEXT column. The matcher unmarshals it once at load, not per event. **Never store comma-separated values.** | `db/forts.go` `ChangeTypes` and `matching/fort.go`. |
| **8. Time-bound expiry?** | The enrichment computes `tth` / `disappearTime`, and it makes clean-deletion meaningful. | `enrichment/maxbattle.go` (`geo.ComputeTTH`, `geo.FormatTime`). |
| **9. Edit support** (the same event later updates the message)? | Clean bit 2, a stable `EditKey` per event, and `MessageTracker` lookup in `FairQueue`. The type also goes in `config.DefaultEditTypes` (`[tracking] default_edit`), must be removed from `v2EditUnsupported` (`api/v2_patch.go`), and must stop calling `rejectEdit` in its v2 `Translate`. **Discuss the EditKey shape with the user** (which fields make it "the same alert"?). | `cmd/processor/raid.go`, `db/clean.go`, `delivery/tracker.go`. For reply threading see `ReplyKey` (`raidlife:…`). |
| **10. Auto-delete on TTH?** | Clean bit 1. Requires #8. | Same as #9. |
| **11. Multi-rule per user with metadata?** | The matcher does **not** dedup by user, and per-user enrichment merges the entries. This is rare. | `matching/pokemon.go`, `enrichment/peruser.go` `consolidateUsers`. |
| **12. Bound to a fixed POI** (stop / gym / station)? | A nullable `*_id` column. Consider a `specific<poi>` `blocked_alerts` key and a mute scope (see §11). | `station_id` in maxbattle, `gym_id` in raid. The `specificstation` check is in `matching/maxbattle.go`. |
| **13. Per-type tile / fallback image?** | `tileserver_templates/<set>/poracle-$0.json` for every tile set, the `staticmap` key list in `enrichment.go`, and possibly a new `Fallbacks.ImgURL…`. | `poracle-maxbattle.json`, the `case "maxbattle":` in `enrichment/enrichment.go`. |
| **14. Repeat-firing webhook?** | The dedup key needs a version or timestamp field. | `tracker/duplicate.go` (`CheckQuest`, `CheckShowcase`). |
| **15. Translation requirements?** | A `$0Translate` per-language enrichment function. | `enrichment/maxbattle.go` `MaxbattleTranslate`. |
| **16. Command-security gated?** | A `command_security` key and a matching `blocked_alerts` value. | `bot/permissions.go` `BlockedAlerts` and `commandSecurityName`. |
| **17. Recently-seen boost for slash autocomplete?** | Record the IDs in `tracker.RecentActivity` and expose them in `/api/v2/activity`. | `RecordMaxBattleBoss` in `cmd/processor/maxbattle.go`, and `ActiveMaxBattleBosses` in `slash/dispatcher.go` and `api/v2_activity.go`. |
| **18. Natural-language keywords?** | Entries in the NLP intent and filter maps, plus an assembler. | `nlp/intent.go`, `nlp/filters.go`, `nlp/assemble.go` `assembleMaxbattle`. |

## Architecture

```
Golbat → POST / → webhook/receiver.go (route by "type")
  → cmd/processor/$0.go: disable check → worker pool → dedup → matching.$0Matcher.Match
    → filterBlocked → filterValidation → filterMuted → applyDefaultEdit (edit types only)
    → enrichment (base + per-language) → RenderJob → render pool → delivery.Dispatcher
    → FairQueue → Discord REST / Telegram Bot API → SnapshotWriter (buttons)

Text command (Discord/Telegram) → host runs BuildTarget + bot.RunLocked → commands.$0Command.Run
  → store.ApplyDiff(ctx.Tracking.$0s, …) → ctx.TriggerReload()
Slash command → slash/mappers.$0 turns options into text tokens → the same text command
v2 API → registerV2Tracking (generic CRUD/PATCH/bulk/dry-run) → store.ApplyDiff → reload
```

## Checklist

### 1. Database migration (new table only)

- [ ] `processor/internal/db/migrations/0000NN_add_$0.{up,down}.sql`, numbered after the highest existing one. Mirror `000002_add_maxbattle.up.sql` **plus** the columns added since then, all in the CREATE: `uid int(10) unsigned AUTO_INCREMENT PK`, `id varchar(255)`, `profile_no int NOT NULL DEFAULT 1` (profile 0 no longer exists, see `000009`), `ping text NOT NULL`, `clean tinyint(1) NOT NULL DEFAULT 0` (bitmask 1=clean, 2=edit, 4=summary), `distance int NOT NULL DEFAULT 0`, `template text NOT NULL`, `override_location_label VARCHAR(64) NULL`, `override_areas TEXT NULL`, then the type-specific columns. `utf8mb4_unicode_ci`. **No foreign key and no unique key** (`000008` dropped them, because duplicates are handled by `ApplyDiff`).
- [ ] Migrations are embedded and run at startup, so nothing else needs wiring.
- [ ] Add the table name to every hand-maintained table list:
  - `db/human_queries.go` `humanOwnedTables` (human delete cascade)
  - `store/human_sql.go` `trackingTables` (profile delete/copy) **and** `humanOwnedTables`
  - `store/user_locations_sql.go` `trackingTypePathMap` (table → API type name, used for saved-location references and the v2 area-op `broadened` keys) **and** `trackingTablesWithOverrideAreas` (`StripOverrideAreas`, v2 area DELETE/PATCH)
  - `bot/commands/backup.go` `backupTables` (`!backup` / `!restore`)
  - Only if the table could carry a legacy FK: `db/migrate.go` `DropForeignKeys`
  - Copy each list by editing it in place, not by pasting from another file. Some of them deliberately include `weather` and `user_locations` and others don't.
- [ ] Do **not** add the table to `000009_profile_zero_to_one`, which is historical. Declare `COLLATE utf8mb4_unicode_ci` explicitly as `000002` does. Any future cross-table SQL needs `CONVERT(id USING utf8mb4)`, as `000009` shows.
- [ ] Tests with their own table lists: `db/migrate_integration_test.go` `expectedTables`, `bot/commands/restore_core_test.go` (the backup table list), `store/user_locations_sql_test.go`. Do **not** add the table to `db/migrate_collation_test.go` `profileRepairTables`. That test runs only the historical `000009` migration, which can't repair a table created after it.

### 2. DB structs, loader, state

- [ ] `db/$0.go`: `$0Tracking` (the full row, loaded into state, with `OverrideAreas` parsed by `loadOverrideKeys`) and `Load$0s`.
- [ ] `db/tracking_queries.go`: `$0TrackingAPI`, with `db`/`json` tags and **`diff` tags**. `diff:"-"` for uid/id/profile_no/raw columns, `diff:"update"` for the fields a one-field change should squash into an existing rule (usually ping, clean, distance, template), and `areakey:"true"` on `OverrideAreas`. These tags drive `store.ApplyDiff` / `SquashPolicy` for the bot, v1 and v2 alike. The only test that exercises them is a per-type scenario in `store/squash_scenarios_test.go` (see `maxbattleScenario`), so add one. Then `Select$0sByIDProfile`, `Select$0sByID` (only if v1 or all-profiles needs it) and `Insert$0` (including the override columns, via `nullIfEmpty` / `marshalOverrideAreas`).
- [ ] `db/orphans.go`: `func (t *$0Tracking) GetID() string`.
- [ ] `db/loader.go`: a field in `AllData`, the `Load$0s` call in `LoadAll`, and `filterSlice` against humans.
- [ ] `state/state.go`: a `$0s` field. `state/loader.go`: assign it in **both** `Load()` and `LoadWithGeofences()`, add it to both "State loaded" log lines, and set `metrics.StateTrackingRules` in the metrics block, which is a third place. Use **one** new `metrics.Type$0` constant (`metrics/metrics.go`) for the state gauge, the matcher and the handler. fort is a counter-example: it uses the literal `"fort"` in `state/loader.go` but `TypeFortUpdate` (`"fort_update"`) in the matcher, so don't copy it.
- [ ] `bot/commands/poracle_admin_util.go` (rule total) and `poracle_admin_status.go` (per-type count row, plus the i18n key `cmd.poracle_admin.status.tracking_type.$0`).

### 3. Store

- [ ] `store/tracking_sql.go`: `New$0Store`, a `$0s` field in `TrackingStores`, the constructor call in `NewTrackingStores`, the `$0GetUID`/`$0SetUID` accessors, and a `check("$0", …)` in `TablesWithUID` (which powers bare `!mute id:N` resolution). `ReassignUID` (stable uids across edits) is generic. `TablesWithUID` nil-guards each store, so if the field is missing everything still compiles, but bare `!mute id:N` silently can't find the type.
- [ ] Test helpers that build `TrackingStores` field by field with `store.NewMockTrackingStore[...]` need the new field wherever the test covers every type: `bot/commands/tracked_test.go`, `discordbot/slash/autocomplete/listers/listers_test.go`, and `buttonactions/buttonactions_test.go` `recordingTrackingStores`.

### 4. Matching

- [ ] `matching/$0.go`: the `$0Data` and `$0Matcher` types (`StrictLocations`, `AreaSecurityEnabled`), and `Match(data, st) ([]webhook.MatchedUser, []webhook.MatchedArea)` built on `st.Geofence.PointAreasAndNames`.
- [ ] Every `trackingUserData` must set **`RuleTable: "$0"`** (the type name, not the plural table). Tracking mutes compare the table as well as the uid, and the value must equal the `mute.ruleTables` key and the `TablesWithUID` `check("$0")` key. Also set the override fields and `IsSpecificMatch` for POI rules.
- [ ] Call `ValidateHumansGeneric` with `"$0"` as the `blockedAlertType` argument. It is matched against `humans.blocked_alerts`, so it must equal the `command_security` name in `bot/permissions.go` (`BlockedAlerts`, `commandSecurityName`); otherwise role-based blocking silently never fires. Record the `metrics.MatchingDuration` / `MatchingCandidates` metrics with `metrics.Type$0`.
- [ ] Tests: a positive match, sentinel handling, area miss, distance miss, `blocked_alerts`, and the override location/areas.

### 5. Webhook handler (new webhook only)

- [ ] `webhook/types.go`: a `$0Webhook` with json tags exactly matching Golbat.
- [ ] `webhook/receiver.go`: add `Process$0` to the `Processor` interface and a `case` in `(*Handler).handle` (with any wire aliases). Every implementer of the interface must gain the method, or the build breaks: `ProcessorService` and the `recordingProcessor` stub in `receiver_test.go` (check with `grep -rn 'ProcessMaxbattle(' processor`). Add receiver tests.
- [ ] Dedup: `(dc *DuplicateCache) Check$0` in `tracker/duplicate.go`, plus `duplicate_test.go`.
- [ ] `cmd/processor/$0.go`: mirror `maxbattle.go` for the `Disable$0` early return, worker pool, `metrics.WebhookProcessingDuration`, dedup (`tracker/duplicate.go` `Check$0`) and `metrics.DuplicatesSkipped`. Then call the matcher followed by **`filterBlocked` → `filterValidation("<wire type>", …)` → `filterMuted(…, mute.Event{<scope ids>})`**, and `applyDefaultEdit` if the type supports edit. After that: `MatchedEvents`/`MatchedUsers` metrics, `tileMode`, base enrichment, the per-language loop over `distinctLanguages`, `parseWebhookFields(raw)`, and the `RenderJob{AlertType, TemplateType, …, LogReference}` (plus `EditKey`/`ReplyKey` if #9 applies).
- [ ] Wire the matcher into `ProcessorService` (`cmd/processor/main.go`: the field and its initialisation next to `maxbattleMatcher`).

### 6. Enrichment and DTS view

- [ ] `enrichment/$0.go`: the base layer (`$0(...) (map[string]any, *staticmap.TilePending)`) with map URLs, geocoding, weather, sun times, static map, TTH and fallback image, plus `$0Translate` for the per-language layer.
- [ ] `enrichment/enrichment.go`: a `case "$0":` in the static-map key list (`keys` / `pregenKeys`). This is **required** if the type has a tile. The `default` branch sends the tileserver only latitude/longitude, so a `poracle-$0.json` that reads any other field renders blank with no error. The maptype string passed here is also the `[geocoding] static_map_type` / `tileserver_settings` key.
- [ ] `dts/view.go`: a per-type alias table entry (`"$0": {…}`) and any new emoji-key fields in `singleEmojiKeys` / `arrayEmojiKeys`. New emoji names also need defaults in `gamedata/util.json` (see `f69204de`). If `{{id}}` or `{{time}}` need special meanings, see `buildComputedFields` in `dts/layered_view.go`.
- [ ] `cmd/processor/enrich.go`: a `case` in `(ps *ProcessorService) enrichForType` and a method `(ps *ProcessorService) enrich$0(raw, language) (*enrichResult, error)`. This is the shared path for `/api/dts/enrich`, the editor and `poracle-test`, and is where showcase was initially missed (`cf3cb353`). Tests: a case in `cmd/processor/enrich_test.go`, and the DTS-name list in `cmd/processor/dts_alias_test.go`.
- [ ] Tests: field-wiring tests in the style of `enrichment/fieldwiring_test.go` for anything derived.

### 7. v2 API (always, for a user-trackable type)

- [ ] `api/v2_$0.go`: a strict `v2$0Rule`. Every field is optional with `nullable:"true"` and a `doc`: scalars are pointers, and `override_areas` is a plain `[]string`. Include only the delivery fields the type supports: a type with no expiry follows `v2_fort.go`, which has no clean/edit/summary. A `summary` field alone doesn't give you digest delivery. That needs dispatch plumbing and an entry in `v2SummaryTypes` (`v2_patch.go`), so omit it unless the type has a digest. The fields are distance, template, ping, clean/edit/summary as applicable, override_location_label and override_areas. `translateV2$0` (defaults via `valueOr` and the `wc*` constants in `v2_wildcards.go`, `clampDistance(deps, oc.limitExempt, …)`, `validateOverrideFields`, `canonicalOverrideAreas`, `packClean`, and `rejectEdit` when edit is unsupported), `$0RowToRule` (with `ptrUnless` for the null-means-unconstrained projection), and `RegisterV2Tracking$0` via `registerV2Tracking(v2TrackingType{Name, Store, Translate, ToRule, GetUID, SetUID, RowText[, Filter][, Canonicalize]})`. Check whether `Canonicalize` is needed, which is the case when stored rows can hold alias wildcard values. That one call provides GET/POST/PUT/DELETE, bulk DELETE, PATCH, `dry_run`, `previous`, and the snapshot provider.
- [ ] `cmd/processor/main.go`: call `api.RegisterV2Tracking$0` **before** `RegisterV2TrackingSnapshot`, which collects the snapshot providers when it is called. The same ordering applies in `registerAllHumaOpsForTest` (`api/huma_openapi_golden_test.go`).
- [ ] `api/v2_patch.go`: add the type to `v2EditUnsupported` if edit is unsupported (`TestV2Patch_EditUnsupportedMatchesTranslate` enforces this).
- [ ] Type-name enums and docs: the `tracking_type` `enum:"…"` tags (three of them) in `api/v2_mutes.go`, and the `broadened` doc string in `api/v2_humans.go`.
- [ ] Tests: `api/v2_$0_test.go` (mirror `v2_maxbattle_test.go`: create, sentinels, unknown field/query rejection, round trip), plus a row in `api/v2_roundtrip_all_test.go` (every nullable field round-trips `null`), the `v2_patch_test.go` edit table, and `registerAllHumaOpsForTest` in `huma_openapi_golden_test.go` (kept in sync with main.go by hand). Regenerate the golden with `UPDATE_GOLDEN=1 go test ./internal/api -run Golden`.
- [ ] Only if #3 is yes: v1 `api/tracking$0.go` (mirror `trackingMaxbattle.go`, with `flexInt`/`flexBool` request fields), the gin routes in main.go, and the `enrich$0s` branch in both loops of `api/trackingAll.go`.

### 8. Bot text command

- [ ] `bot/commands/$0.go`: `Name()` returns `"cmd.$0"`. In `Run`, call `RouteToMuteFromType(ctx, "$0", args)` first, then `extractPings`, `usageReply` / `helpArgReply` (`msg.$0.usage`), the `Disable$0` check, `ctx.ArgMatcher.Match`, `bot.ReportUnrecognized`, `parseCommonTrackFields` (template validation and distance), and `parseOverride` (location/area overrides). Build the rows using `ctx.TargetID` / `ctx.ProfileNo` (the host has already run `BuildTarget` and takes the per-human lock through `bot.RunLocked`, so don't lock inside the command). Then `overrideLimitReply(ctx, tracked, insert, store.$0GetUID, store.$0SetUID)` ([tracking] max_areas on `area:`, judged on the diff), `store.ApplyDiff(ctx.Tracking.$0s, …)`, `buildTrackingMessage`, and `ctx.TriggerReload()`. The v1 handler, if any, calls `overrideLimitMsg` before its `DiffAndClassify`; v2 gets it from `registerV2Tracking`. Remove goes through `removeByUIDs` for `id:N`.
- [ ] Register it in `cmd/processor/main.go` next to `commands.MaxbattleCommand`.
- [ ] `bot/commands/conversions.go`: `$0APIToTracking`, plus the override-preservation cases in `conversions_test.go`.
- [ ] `bot/commands/tracked.go`: load gated on `Disable$0`, add the section block (`section.$0s` / `.none`, `fmtID`, `warnRow`), and add it to the total count.
- [ ] `bot/commands/script.go`: a `$0ToScript` that emits **every setting the command accepts** (`664687c2`), **and** the call in `ScriptCommand.Run`, which selects each store's rows explicitly. Without that call the serializer compiles but is never used. Add a round-trip test.
- [ ] `bot/commands/untrack.go` `validUntrackTypes` (kept in step with the slash `/untrack` subs), and `bot/commands/mute.go` `isTrackingTypeToken`.
- [ ] `bot/commands/info.go`: the DTS-type display-name map. `bot/permissions.go`: `commandSecurityName` and `BlockedAlerts` if #16 applies.
- [ ] NLP (#18): `nlp/intent.go`, `nlp/filters.go`, `nlp/assemble.go`, and `nlp/match.go` if moves are relevant.
- [ ] Tests: bare add, duplicate (👌), update squash, remove, `id:N` remove, template rejection for non-admins, keyword permutations, and the mute alias.

### 9. Slash command (Discord)

- [ ] `discordbot/slash/definitions.go`: a `case "cmd.$0":` in the definition switch, a `$0Options(bundle)` builder (`stringOpt`/`boolOpt`/`distanceOpt`/`cleanOpt`/`templateOpt`, plus choices), `"cmd.$0"` in `allCommandKeys`, and `"$0"` in the `untrackOptions` subs.
- [ ] `discordbot/slash/mappers/$0.go`: a mapper from options to text tokens, registered via `func init() { registry["$0"] = $0 }`, plus `mappers/$0_test.go`.
- [ ] `discordbot/slash/dispatcher.go`: `isTrackerCommand` (**required**; it gates `location`/`areas` autocomplete), `dtsTypeFor` (only if the DTS type differs from the command name), and any per-option autocomplete such as a recent-activity boost.
- [ ] `internal/bot/testdata/parity.yaml`: one fixture per option of `/$0`, plus fixtures for the new `/untrack $0` subcommand. `bot/coverage_test.go` `TestEveryCommandAndOptionHasFixture` **fails** for any uncovered slash command or option (copy the `# /maxbattle` block).
- [ ] The tracker-command lists in `discordbot/slash/definitions_test.go`, and the `dtsTypeFor` table in `discordbot/slash/dispatcher_test.go`.
- [ ] `discordbot/slash/autocomplete/listers/tracking.go`: a `case "$0":` (for `/untrack $0` rule picking) and a `to$0Tracking` converter.
- [ ] i18n: `slash.cmd.$0`, `slash.desc.$0`, the `slash.opt.$0.*` and `slash.choice.$0.*` keys, the `slash.opt.untrack.$0*` keys, and `error.slash.$0.*`.
- [ ] `discordbot/slash/snapshot_dump_test.go` `TestDumpSnapshots` has its own hand-written list: add the `{"cmd.$0", "$0"}` pair and the `withOverride` en strings. Add `TestSnapshot$0` in `definitions_test.go` (mirror `TestSnapshotMaxbattle`), and extend both subtype loops in `mappers/untrack_test.go`.
- [ ] Then regenerate the definition snapshots with `POROCLE_DUMP_SNAPSHOTS=1 go test ./internal/discordbot/slash -run TestDumpSnapshots`. The env var really is spelled "POROCLE" in the code, so don't correct it. This writes `testdata/$0.json` and updates `untrack.json`. Re-registration with Discord is driven by the fingerprint, so nothing needs to be triggered by hand.

### 10. Delivery-side features: snapshots and buttons

- [ ] Snapshots (`buildSnapshot`) are generic and need no per-type code. Check that `AlertType` and the rule UIDs flow through the `RenderJob`.
- [ ] `buttonactions/unsubscribe.go`: add the alert type (and its wire aliases) to `alertTypeTable` and a `case` in `deleteTrackingByAlertType`. **The switch's `default` branch deletes maxbattle rows**, so a missing case would delete from the wrong table. The test that would catch this is maintained by hand. Add the type to `recordingTrackingStores`, the alert-type map in `TestUnsubscribeRoutesEveryRenderedAlertType`, and the alias list in `TestAlertTypeTable` (`buttonactions/buttonactions_test.go`).
- [ ] Button mute with `scope=tracking` uses the same `alertTypeTable`. With other scopes, check that `muteScopeValue` (`buttonactions/mute.go`) can find the type's POI or pokemon field in the snapshot.

### 11. Mutes

- [ ] `mute/mute.go` `ruleTables`: `"$0": "$0"` (this also makes it valid for the v2 `tracking_type`), plus the list in `mute/mute_test.go`.
- [ ] The handler's `mute.Event` carries the identifiers the type has (`PokemonID`, `GymID`, `PokestopID`, `StationID`).
- [ ] **New POI scope only** (e.g. a new kind of fort): a `mute.Scope…` constant and its match logic, `buttons.Scope…` and `muteScopeValue`, the `!mute` scope noun parsing, the v2 mutes `scope` enum and validation, and the i18n key `msg.mute.scope_<scope>`.

### 12. DTS templates, help, editor metadata

- [ ] `fallbacks/dts.json`: a default entry per platform (`"default": true`, discord and telegram). Use `templateFile` for prose that is awkward in JSON. `UsesTile` / `tileMode` handle `{{staticMap}}` automatically.
- [ ] `fallbacks/dts/help/$0.json` (the `!help $0` topic, resolved from the usage key by `topicFromUsageKey`), plus a line in `fallbacks/dts/help/index.json` and the type in `script.json`'s list. Add `"$0"` to `helpIDs` in `dts/help_shipped_test.go`. Without it the new help file is never load-tested or checked against Discord field limits. The file name must equal the middle segment of `msg.$0.usage`, because `topicFromUsageKey` derives the topic from it. Also update the plain-text `msg.help.text` in en.json.
- [ ] `api/dts_fields.go`: `$0Fields` (with `Type`, `Description`, `Category` and `Preferred`, and `RawWebhook` / `PreferredAlternative` for raw aliases), `$0Snippets`, and a `fieldsByType` row via `alertFields(mapLinks, tth, sun, $0Fields)`. Add the type to `typeEnrichEntries` in `dts_fields_pertype_test.go`. `dts_fields_produced_test.go` fails on any listed field that nothing produces. Its `TestFieldCatalogue_PerTypeGroups` has an explicit table of which common-field groups each type gets, so add a row.

### 13. Test alerts and the editor (`!poracle-test`, `/api/test`, `/api/dts/*`)

- [ ] `fallbacks/testdata.json`: scenarios keyed `{"type": "<wire type>", "test": "…", "location": "…", "webhook": {…}}`.
- [ ] `dtsmap/dtsmap.go` `types`: the DTS name entry, plus identity entries for every wire spelling (underscore, hyphen). This single table drives `/api/dts/testdata`, `/api/test` and `!poracle-test` resolution.
- [ ] `cmd/processor/test.go`: a `case` in the process switch, the method `(ps *ProcessorService) processTest$0(raw, target webhook.MatchedUser)` (built on `enrich$0` and `renderJobFromEnrich`), and the wire → DTS mapping in `resolveDTSTypeFromRaw`.
- [ ] The `type` `doc:` strings in `api/huma_actions.go` and `api/huma_dts_writes.go` (`enrichBodyShape`), which list the types by hand. Then regenerate the OpenAPI golden.
- [ ] Tests: the alias and case-folding cases in `dtsmap/dtsmap_test.go`.
- [ ] `bot/commands/poracletest.go`: `validHooks` (the CLI spelling), the timestamp-freshening `case` (so the preview isn't already expired), and the wire → DTS mapping.
- [ ] `api/test_type.go` / `test_type_test.go`, `api/huma_actions_test.go` and `bot/commands/poracletest_test.go`: the wire-type resolution rows. Optionally add an enrichment equivalence case in `enrichment/enrichment_equivalence_test.go` (it runs against the testdata entries).
- [ ] Derived types (synthesised from processor state rather than a webhook, like `monsterChanged`) are `Derived: true` in dtsmap. Follow `docs/superpowers/specs/2026-07-18-derived-dts-test-data-design.md`.

### 14. Config

Settings keyed by type are spread across several files: one struct field, the example TOML, the editor schema and the `/api/config/poracleWeb` view. Some of them also have type lists in description strings. Two tests keep the files consistent: `config/example_test.go` `TestExampleConfigHasNoUnknownKeys` (every example key must exist in the struct) and `api/config_schema_defaults_test.go` `TestConfigSchemaDefaultsMatchCode` (every schema field must exist in the struct, with a default equal to the code default). For plain scalar and list fields, `config_resolve.go`, `config_values.go`, the config writer, `/api/config/values`, `!poracle-admin config` and `overrides.json` migration (`overrides.go` / `migrate_overrides.go`, which match on TOML tags) are all generic, so they need no per-type code. The exceptions are map-backed or nested-row tables, which need explicit extract/save conversions in `config_values.go` (`ExtractValues`, `nestTableUpdates`; see the communities and tileserver_settings handling), and semantic validation, which goes in `config_validate.go` `validateField`. Both need round-trip tests.

**Always needed:**

- [ ] `config/config.go` `GeneralConfig`: `Disable$0 bool`. Existing flags are named after the **wire type** (`disable_max_battle`, `disable_fort_update`), so use `toml:"disable_<wire_type>"`. The default is `false`, so nothing goes in the `Load` defaults.
- [ ] `config/config.example.toml`: the line `disable_<wire_type> = false` in the `disable_*` block of `[general]`.
- [ ] `api/config_schema.go`: a `general` `ConfigFieldDef{Name: "disable_<wire_type>", Type: "bool", Default: false, …}` (the config editor toggle).
- [ ] `api/huma_config.go` `hookTypes`: `{"$0", cfg.General.Disable$0}`, which builds `disabledHooks` in `/api/config/poracleWeb`. It uses the **type name**, not the wire name. Add a test in `huma_config_test.go`. This was missed for showcase (`5e7d106a`).
- [ ] Wire the flag in four places: the handler's early return, the command's 🙅 reply, and **both** gates in `tracked.go` (the load and the section).

**Needed when the matching Characteristics answer is "yes":**

- [ ] **Command-security gated** (#16): the `command_security` map needs no code change, but the code that reads it lists types. Update `bot/permissions.go` `BlockedAlerts` and `commandSecurityName`, and the gateable-keys comment above `[discord.command_security]` in `config.example.toml`.
- [ ] **Edit** (#9): `config.DefaultEditTypes`, the `default_edit` description in `config_schema.go` (which lists the supported types), and the `default_edit` comment in `config.example.toml`.
- [ ] **Tile** (#13): `[geocoding] static_map_type` and `tileserver_settings` are maps keyed by maptype, so there is no struct change. However, the `static_map_type` / `tileserver_settings` descriptions in `config_schema.go` and the comments in `config.example.toml` list the known maptypes by hand.
- [ ] **New fallback image**: a `FallbacksConfig.ImgURL…` field (`toml:"img_url_…"`), its default in the `Load` defaults literal, the `fallbacks` schema `ConfigFieldDef` (with the same default), and the `[fallbacks]` example line. The value must also reach the enricher: add an `Enricher.FallbackImg…` field and copy it from `cfg.Fallbacks` in `NewProcessorService` (`cmd/processor/main.go`). Otherwise the configured value is silently ignored.
- [ ] `[discord.slash_commands] enable` is an allow-list of free-form names, and `[general] disabled_commands` takes free-form command names. Neither needs code, but mention the new name in the example comment.

**If the type needs its own `[$0]` section** (several settings that aren't simple per-type toggles):

- [ ] `config/config.go`: `type $0Config struct{…}` with `toml` tags, a `Config` field `toml:"$0"`, defaults in the `cfg := &Config{…}` literal in `Load`, and any validation or startup warnings in `Load` / `main.go` (follow `UnsupportedDefaultEdit`).
- [ ] `config/config.example.toml`: a `[$0]` block where each key has a comment. Every key must exist in the struct.
- [ ] `api/config_schema.go`: a `ConfigSection{Name: "$0", Title, Fields: []ConfigFieldDef{…}}`. `Name` is the TOML path (nested sections use dots, e.g. `discord.slash_commands`). Each `Field.Default` must equal the code default; a default applied where the value is used goes in `appliedAtUse` in the defaults test instead. Set `HotReload` only if the consumer reads the config at use time rather than caching it at startup.
- [ ] `config/toml_encode.go` `SectionOrder`: optional, but without it the section is written alphabetically after the ordered ones. This affects the file rewritten by `POST /api/config/values` and the order shown by `!pa config`.
- [ ] `bot/commands/poracle_admin_config.go` `redactedFields`: only for secrets such as tokens and passwords.
- [ ] Map-valued settings, or rows the editor should show as a table: add the conversions in `config_values.go` and the validation in `config_validate.go`, as described above.
- [ ] Docs: the "Key sections" line in CLAUDE.md, README.md and CHANGELOG.md, plus `docs/CONFIG-EDITOR-INSTRUCTIONS.md` if the editor needs special UI.

### 15. i18n (`processor/internal/i18n/locale/en.json`)

- [ ] `cmd.$0`, `section.$0s`, `section.$0s.none`, `msg.$0.usage` (plus any `msg.$0.*` validation errors), `msg.no_permission.$0` (if gated), `cmd.poracle_admin.status.tracking_type.$0`, the `tracking.*` rowtext keys, the slash keys from §9 (`slash.cmd/desc/opt/choice.$0*`, `slash.opt.untrack.$0*`, `error.slash.$0.*`), and the plain-text `msg.help.text` line. Other locales fall back to English key by key, and Crowdin picks up the new keys.

### 16. Rowtext

- [ ] `rowtext/$0.go`: `(g *Generator) $0RowText(tr, *db.$0Tracking) string`, used by `!tracked`, command confirmations, slash autocomplete, unsubscribe and v2 `include_descriptions`. If the type supports edit, honour `Generator.DefaultEdit`.

### 17. Documentation

- [ ] `DTS.md`: a `## … (\`$0\`)` field section, and the type in the weather/boost field lists if applicable.
- [ ] `docs/v2-api-design.md`: the per-type field section and wildcard table rows. Mention the new type in `CHANGELOG.md`.
- [ ] `API.md`: the type lists (tracking types, `disabledHooks`, the test/webhook type tables), plus a v1 section only if #3 is yes.
- [ ] Operator note (README and CHANGELOG): Golbat must be configured to send the new webhook type to Poracle, and the tileserver needs the new `poracle-<maptype>` template.
- [ ] `TILESERVER.md` (if #13 applies), `README.md` (type counts and lists), and `docs/slash-commands/README.md` (command count and list).
- [ ] `CLAUDE.md`: update the cross-cutting statements that count or enumerate types ("10 tracking types", "11th tracking type", the Database table list, the `!untrack <type>` list). Keep per-type detail in DTS.md / API.md.

### 18. Done means

- [ ] The pre-commit gate passes from `processor/`: `go build ./... && go vet ./... && go test -count=1 ./... && golangci-lint run ./...`. Without a local Go toolchain, run it in the golang Docker image.
- [ ] Type-enumerating tests: re-run `grep -rlIi 'maxbattle\|max_battle\|fort-update\|fort_update\|"forts"\|"nests"\|"lures"' processor --include=*_test.go` and check each one also covers `$0`. Search every spelling: some tables, such as the field-group table, list fort and nest but not maxbattle. The tests named in earlier sections are the known ones: §1 tables, §2 squash, §3 mock stores, §6 enrich and dts_alias, §9 parity and definitions, §10 buttonactions, §11 mute, §12 help and dts_fields.
- [ ] Re-run the touchpoint sweep with `$0` alongside the reference type and diff the two file lists. Any file that names the reference but not `$0` needs a deliberate justification.
- [ ] Manual checks: `!poracle-test <cli-type>,default` delivers on Discord and Telegram. `!$0 …` adds a rule, a duplicate gets 👌, a one-field change updates in place with the same `id:N`. `!tracked`, `!script` (and pasting its output back), `!untrack $0 id:N` / `!untrack $0 …` (bare `!untrack id:N` only removes pokemon rules), `/$0` and `/untrack $0`, and `!$0 mute id:N` all work. The unsubscribe and mute buttons work on a delivered alert. v2 GET/POST/PUT/PATCH/DELETE, the `dry_run` variants, and the full snapshot `GET /api/v2/humans/{id}/tracking` include the type. `GET /api/dts/fields/$0`, `/api/dts/testdata`, `/api/dts/enrich` and `/api/config/poracleWeb` `disabledHooks` all respond correctly. `disable_<wire_type> = true` drops the type end to end (handler, command and both `!tracked` gates). `!backup`/`!restore`, profile copy and delete, and human delete cover the new table.

## Cross-cutting verification

The checklist above makes sure the type is *wired in*. These checks make sure it *behaves correctly* everywhere it shows up. Add them to the plan as explicit tasks. Most of them are tests and not only manual checks.

- [ ] **Every way of creating a rule gives the same row.** A rule can come from `!$0 …`, `/$0`, v2 `POST`, `!script` replay or `!restore`. Each path must store an identical row (same sentinels, same `clean` bits, same override spelling), and a one-field change must squash the same way through every path. Write at least one test that creates the same rule through the bot command and through v2 and compares the stored rows. Disagreements between paths have caused real bugs before (`e15ab0c3` for the slash form, the raid/maxbattle level sentinels).
- [ ] **Permissions.** For the new command, check:
  - a non-admin in a registered channel is blocked;
  - `user:` / `name:` overrides are admin or webhook-admin only;
  - delegated channel admins work;
  - `command_security` gating (#16) applies to the text command, `/$0` and `/api/command` alike;
  - with `area_security` on, `override_areas` is limited to `PermittedAreas`, and `strict_locations` still applies at match time.

  These are mostly generic, but a mistake specific to one type is a security bug, so test them anyway.
- [ ] **Clients and feature detection.** Decide whether clients (PoracleWeb, ReactMap, the DTS editor) need to detect the new type. If they do, add a flag to `api.Capabilities` (`api/api.go`, built in the function that returns `Capabilities{…}`), mirroring `Costume`, and add it to the `/health` capabilities list in CLAUDE.md and API.md. List the client changes needed (tracking UI, editor field list, any new masterdata endpoint for ID lists) and raise them with the client maintainers.
- [ ] **Upgrades and data lifecycle.** Check:
  - the migration on an existing install, on both MySQL and MariaDB, and that the down migration runs;
  - profile copy carries the new rules across;
  - `disable_<wire_type> = true` with existing rules leaves them stored but inert, and `!tracked` hides them;
  - **`!restore` of a backup taken before the type existed clears the new type's rules.** Restore replaces every table in `backupTables`, and a table missing from the file ends up empty. Decide whether that's acceptable, and either document it in the release notes or skip absent tables (`legacyBackupTables` in `backup.go` handles the reverse case, a table that has been removed).
- [ ] **Hot-path cost.** The matcher scans `st.$0s` linearly on every webhook. Check:
  - the geofence lookup happens once per event;
  - there are no per-rule allocations;
  - any JSON-array columns are parsed at state load, not per event;
  - the dedup key and the `DuplicateCache` TTL suit how often the event type fires;
  - metrics labels never contain IDs.

  For a high-volume type, benchmark the matcher (`go test -bench`) with a realistic rule count.
- [ ] **Bad and unusual payloads.** Test the handler and enrichment with: optional fields missing, zero or absent coordinates, an expiry already in the past (drop the event), an unknown enum value, and a duplicate that arrives after a state reload. Golbat payloads change between versions, so prefer tolerant decoding: unknown fields are ignored, and a missing field means "unknown", never a zero that matches a rule.
- [ ] **Localisation.** Slash command and option names translated into other locales must satisfy Discord's naming rules (lowercase, at most 32 characters). Invalid ones are silently dropped back to English by `slash/localization.go` (see `TestLocalizationsForKeyValidateNameDropsInvalid`), so check the registered result rather than assuming. The translated text-command alias (`cmd.$0` in each locale) must not collide with an existing command in that language.
- [ ] **Telegram.** The Telegram fallback template must escape correctly for its parse mode (MarkdownV2 is strict), and the send order (sticker, photo, text, location, venue) must produce a sensible message. Test-deliver to Telegram, not only Discord.

## Reference files

| Reference | Use when… |
|-----------|-----------|
| **`maxbattle`** (default) | New webhook with a new table, pokemon by ID with the `9000` sentinel, a POI id, level/form/move filters, no edit. Every touchpoint above has a maxbattle instance, so use it for the sweep. |
| **`raid`** / **`egg`** | Edit/RSVP (`EditKey`, `ReplyKey`, `rsvpChanges` derived template, `OverrideCleanTTH`), default_edit, costume. |
| **`showcase`** | New webhook payload alerting against an **existing** table (commits `fbeaebb0`, `8829cb82`, `1d5d3b88`, `cf3cb353`, `5e7d106a`, `f69204de`). |
| **`incident`** | A v2 type sharing a table with another type via `Filter` (`api/v2_incident.go`), its own text and slash command, and mute table mapping to `invasion`. |
| **`fort`** | Enum-only filters with JSON-array columns and no expiry. |
