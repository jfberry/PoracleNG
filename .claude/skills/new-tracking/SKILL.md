---
name: new-tracking
description: Generate a task checklist for adding a new tracking type (and optionally a new webhook handler) to PoracleNG. Use when planning implementation across the database, processor, matching, enrichment, API, bot commands, DTS templates, and config editor.
user-invocable: true
argument-hint: [tracking-type-name] [optional-reference-pr-url]
allowed-tools: Read, Grep, Glob, Bash(gh *), WebFetch, TaskCreate
---

# Add New Tracking Type to PoracleNG

PoracleNG is a single Go binary. Most additions today are *tracking types* against an existing webhook handler — the new code is the user-facing tracking surface (DB table, command, API endpoints, matcher, enrichment, templates, DTS editor metadata). A genuinely new webhook handler is the rarer case and is called out separately below.

**Tracking type name:** $0 (used as the SQL table name and command verb; pick singular form, e.g. `showcase`, `fort`, `maxbattle`)
**Reference PR (optional):** $1

## Instructions

1. If a reference PR URL is provided ($1), fetch the diff and description first.
2. Decide whether $0 needs a **new webhook handler** or piggybacks on an existing one (e.g. showcases ride on `fort_update` or `invasion`).
3. Read a similar existing tracking type end-to-end first (`fort` is the most recent and most complete reference). Don't guess; mirror the actual code.
4. Substitute $0 into every checklist item, drop sections that don't apply, add steps the reference PR requires that aren't covered. Present the full list before creating tasks.

## Architecture

```
Golbat → POST / → webhook/receiver.go (route by "type" string)
   → cmd/processor/{type}.go: dedup → matching/{type}.go.Match() →
     enrichment/{type}.go (base) + {type}Translate (perLang) → enqueue RenderJob
   → render worker: build LayeredView → dts.Render → delivery.Dispatcher
   → Discord REST or Telegram Bot API

User commands (Discord gateway / Telegram polling) → bot.Parser →
  bot/commands/{type}.go.Run() → store.Tracking.{Type}.ApplyDiff →
  triggerReload() → state.Set(new snapshot)

API clients → /api/tracking/{type}/{id} → api/tracking{Type}.go →
  same store.ApplyDiff path → triggerReload()
```

There is **one** code path per type — no duplicate platform-specific files. The bot framework abstracts Discord and Telegram so a single command serves both.

## Checklist

### 1. Database migration

- [ ] Create migration pair `processor/internal/db/migrations/0000NN_add_$0.up.sql` / `.down.sql`. Mirror `000002_add_maxbattle.up.sql`: `uid` BIGINT PK auto-increment, `id` VARCHAR(255), `profile_no` INT default 1, `ping` TEXT NOT NULL, `clean` TINYINT(1) NOT NULL DEFAULT 0 (bitmask: 1=clean, 2=edit), `distance` INT NOT NULL DEFAULT 0, `template` TEXT NOT NULL, plus type-specific columns. ENGINE=InnoDB. **No FK constraint** — orphans are filtered in-memory at load.
- [ ] Migrations are auto-applied at startup via `processor/internal/db/migrate.go` and embedded by `migrations/embed.go`. Nothing else to wire.
- [ ] Add `"$0"` (or `"$0s"` if pluralised) to the `trackingTables` slice in **both**:
  - `processor/internal/db/human_queries.go:29` (powers `DeleteHumanAndTracking`)
  - `processor/internal/store/human_sql.go:529` (powers profile-delete cascade and unregister)
- [ ] Add the same name to `processor/internal/bot/commands/backup.go:21` (powers `!backup` / `!apply`).

### 2. DB structs & loader

- [ ] Create `processor/internal/db/$0.go` (or extend `tracking_queries.go`) with two structs:
  - `$0Tracking` — full row with `db:` tags, used internally and in `state.State`.
  - `$0TrackingAPI` — API DTO; mark numeric-and-bool fields as `flexInt` / `flexBool` (defined in `internal/api/tracking.go`) so ReactMap-style clients sending `"clean": false` instead of `"clean": 0` are tolerated.
- [ ] Add query helpers: `Select$0sByIDProfile`, `Select$0sByID`, `Insert$0`. Match the existing function-name conventions exactly so the generic store can find them.
- [ ] Add `$0s []*$0Tracking` to `AllData` in `processor/internal/db/loader.go:27`. Add a `Load$0s()` call in `LoadAll()` and assign to the struct.
- [ ] Add `$0s []*db.$0Tracking` to `state.State` in `processor/internal/state/state.go:11`. Assign in **both** `state.Load()` and `state.LoadWithGeofences()` in `processor/internal/state/loader.go` (one-time miss = silent breakage at runtime).

### 3. Store

- [ ] Add `New$0Store()` constructor in `processor/internal/store/tracking_sql.go` (mirrors `NewFortStore` at line 109): `table: "$0"`, `selectFn: db.Select$0sByIDProfile`, `insertFn: db.Insert$0`.
- [ ] Add `$0s TrackingStore[db.$0TrackingAPI]` field to `TrackingStores` struct (~line 128) and a constructor call in `NewTrackingStores` (~line 142).
- [ ] Add `$0GetUID` / `$0SetUID` accessor pair (~line 176). Generics can't reach struct fields; these are needed by `DiffAndClassify`/`ApplyDiff`.

### 4. Matching

- [ ] Create `processor/internal/matching/$0.go` with:
  - `$0Data` struct — the parsed-once webhook view, including lat/lon and any IDs/fields the matcher needs.
  - `$0Matcher` with `Match(*$0Data, *state.State) ([]webhook.MatchedUser, []webhook.MatchedArea)`.
- [ ] Inside `Match`: iterate `state.$0s`, apply type-specific filters, then call `ValidateHumansGeneric(humans, lat, lon, "$0", ...)` which handles distance / area / `blocked_alerts` / `admin_disable` / strict-area. The "$0" string is the alert-type label users put in their `blocked_alerts` array — keep it consistent across this call, the bot command, and config schema.
- [ ] If multiple rules per user can match with different metadata (cf. pokemon PVP), do **not** dedup by user ID in the matcher — preserve every match and dedup downstream. Otherwise dedup at the end.
- [ ] Write `*_test.go` covering: positive match, area miss, distance miss, blocked-alerts skip.

### 5. Webhook handler

**If reusing an existing handler** (most cases): in the existing handler, after `state.$0s` is populated, call `ps.$0Matcher.Match(...)` alongside the existing matcher and merge the results. No receiver / type changes.

**If new webhook**:
- [ ] Add `$0Webhook` struct to `processor/internal/webhook/types.go` with JSON tags matching Golbat's schema verbatim (case-sensitive).
- [ ] Add `Process$0(raw json.RawMessage) error` method to the `Processor` interface in `processor/internal/webhook/receiver.go`.
- [ ] Add `case "$0":` in `ServeHTTP` switch (and any aliased event names — fort_update / max_battle use both snake and dash forms).
- [ ] Create `processor/cmd/processor/$0.go` with `Process$0()` on `ProcessorService`: worker-pool gate, `metrics.WebhookProcessingDuration`, dedup via `tracker.DuplicateCache`, build `$0Data`, call matcher, on match call enricher + per-language loop, enqueue `RenderJob`. Mirror `cmd/processor/fort.go`.
- [ ] Add a duplicate-cache method `Check$0(...)` in `processor/internal/tracker/duplicate.go` keyed on whatever uniquely identifies the event. Read the Golbat docs — events that fire repeatedly need a stable key including any version/AR/timestamp field.
- [ ] Add the type label to the `metrics` label sets that take a string label (`MatchedEvents`, `MatchedUsers`, `DuplicatesSkipped` — Prometheus accepts new labels at first call, no init needed, but make sure your `.Inc()` calls use the exact same label everywhere).

### 6. Enrichment

- [ ] Create `processor/internal/enrichment/$0.go`:
  - `$0(...) (map[string]any, *staticmap.TilePending)` for the **base** layer: pokestop/gym identity, icon URLs (`e.ImgUicons` / `ImgUiconsAlt` / `StickerUicons`), map URLs (`e.addMapURLs`), geocoding (`e.addGeoResult`), weather (`e.WeatherProvider.GetCurrentWeatherInCell`), sun times (`addSunTimes`), static map (`e.addStaticMap`), expiration / TTH (`geo.FormatTime` + `geo.ComputeTTH`), fallback img (`e.setFallbackImg`).
  - `$0Translate(base, ..., lang) map[string]any` for the **per-language** layer: translated names, emoji keys. Use `e.Translations.For(lang)` and `tr.T(key)` / `tr.TfNamed(key, args)`. Identifier keys: `gamedata.PokemonTranslationKey`, `TypeTranslationKey`, `ItemTranslationKey`, `weather_*`, `team_*`, etc.
- [ ] If $0 needs a **fallback image** that's not already covered by `Fallbacks.ImgURL` / `ImgURLPokestop` / `ImgURLGym`, add a new field in `processor/internal/config/config.go` `FallbacksConfig` (~line 546) and wire it on `Enricher` in `processor/cmd/processor/main.go` (~line 924).
- [ ] Add per-user fields, if any, in `processor/internal/enrichment/peruser.go` (distance/bearing already done by the common path).

### 7. Wiring (cmd/processor/main.go)

- [ ] Add `$0Matcher *matching.$0Matcher` field to `ProcessorService` (~line 818).
- [ ] Initialise it in `NewProcessorService` (~line 1144).
- [ ] Register API routes in the tracking group (~line 369): `GET`, `POST`, `DELETE byUid`, `POST /delete`.
- [ ] Register the bot command: `cmdRegistry.Register(&commands.$0Command{})` (~line 519).

### 8. Bot command

- [ ] Create `processor/internal/bot/commands/$0.go` implementing `bot.Command`:
  - `Name() string` → `"cmd.$0"` (i18n key)
  - `Aliases() []string`
  - `Run(ctx *bot.CommandContext, args []string) []bot.Reply`
- [ ] Use `bot.BuildTarget(ctx, args)` for target resolution (handles user/channel/webhook/admin overrides). Use `ctx.ArgMatcher` for typed args (distance, template, clean, etc.). Use `helpers.go::processTrackingDiff` for the create/update/no-op classification + reply text.
- [ ] **Template validation**: if user explicitly passes `template:N`, validate against loaded DTS via `ctx.DTS`; non-admins get blocked with 🙅 + an i18n message, admins get a warning but proceed.
- [ ] Store empty string for `template` when user doesn't specify one — the renderer resolves to `[general] default_template_name` at render time.
- [ ] Mirror `commands/fort.go`. Write `*_test.go` covering: bare add, duplicate (👌), remove, template-validation rejection for non-admin, type-specific keyword permutations.
- [ ] Add the `$0` block to `commands/tracked.go` so `!tracked` lists rules of this type:
  - Load via `ctx.Tracking.$0s.SelectByIDProfile` (gated on `cfg.Disable$0`).
  - Render rows via `ctx.RowText.$0RowText` (see step 9), prefixed with `[id:UID]` and the `warnRow` distance helper.
  - Use the new `section.$0s` / `section.$0s.none` i18n keys.
- [ ] Add the type's count to the summary line in `commands/info.go` (~line 777) so `!info` reports its rule count.
- [ ] If the type has natural-language keywords users might say in plain English, add an entry to `processor/internal/nlp/intent.go:47` (intent map), `nlp/filters.go:67` (filter map), and `nlp/assemble.go:28` (assemble dispatch + `assemble$0` function).

### 9. Rowtext

- [ ] Create `processor/internal/rowtext/$0.go` with `$0RowText(tr *i18n.Translator, t *db.$0Tracking) string`. Used by `!tracked`, the `tracking{Type}` API GET/POST responses, and the bot-command confirmation. Format mirrors `rowtext/fort.go`: filter description, `tracking.distance_fmt` if non-zero, `standardText` (template + clean flags) at the end.

### 10. API tracking endpoints

- [ ] Create `processor/internal/api/tracking$0.go` with these handlers (mirror `trackingFort.go`):
  - `HandleGet$0(deps *TrackingDeps) gin.HandlerFunc` → `GET /api/tracking/$0/:id`
  - `HandleCreate$0(deps *TrackingDeps) gin.HandlerFunc` → `POST /api/tracking/$0/:id` (diff & apply via `store.ApplyDiff`)
  - `HandleDelete$0(deps *TrackingDeps) gin.HandlerFunc` → `DELETE /api/tracking/$0/:id/byUid/:uid`
  - `HandleBulkDelete$0(deps *TrackingDeps) gin.HandlerFunc` → `POST /api/tracking/$0/:id/delete`
- [ ] All four call `deps.TriggerReload()` after mutations and use `deps.RowText.$0RowText` for reply descriptions (i18n via `deps.Translations`).
- [ ] Validate enum-style fields (e.g. `validFortTypes` in `trackingFort.go:21`) up front — return 400 with the rejected value rather than silently storing junk.
- [ ] Add the `$0` branch to `processor/internal/api/trackingAll.go`:
  - `enrich$0s(deps, tr, rows)` helper that converts API rows to `*$0AllDesc` with the `Description` field set via `deps.RowText.$0RowText`.
  - Cases in **both** the per-profile loop (~line 329) and the all-profiles loop (~line 479).
- [ ] No separate route file — register the four handlers in `cmd/processor/main.go` step 7 above.

### 11. DTS templates & rendering

- [ ] Add a default template entry for `$0` to `fallbacks/dts.json` (one for each platform you support — usually `discord` and `telegram`). Each entry shape:
  ```json
  {
    "id": 1,
    "type": "$0",
    "language": "en",
    "default": true,
    "platform": "discord",
    "template": { "embed": { ... } }
  }
  ```
  Set `"default": true` on at least one entry per (type, platform) so the selection chain has a fallback. Templates can use `"templateFile": "dts/$0.txt"` instead of inline `template` if the prose is awkward to embed in JSON.
- [ ] If the template needs shared partials, add them to `fallbacks/partials.json` and reference via `{{> partialName}}`.
- [ ] If the type's templates wrap URLs with `<S< ... >S>` markers for Shlink shortening, no extra wiring — the renderer already scans for them.
- [ ] Confirm `tileMode` in `processor/cmd/processor/tilemode.go` does the right thing for $0; if no template uses `{{staticMap}}` it short-circuits to `Skip` automatically.

### 12. DTS field metadata (config editor & template editor)

- [ ] Add `$0Fields []FieldDef` to `processor/internal/api/dts_fields.go` listing every template-visible field with its `Type`, `Description`, `Category`, and `Preferred` flag (controls editor highlighting).
- [ ] If $0 has block scopes (`{{#each ...}}` regions), define `$0BlockScopes` mapping the block name to the field set inside it. See `monsterBlockScopes` in the same file as a model.
- [ ] If $0 has handy boilerplate snippets, add `$0Snippets []Snippet`.
- [ ] Add a row to the `fieldsByType` map (~line 652):
  ```go
  "$0": {Fields: append(commonFields, $0Fields...), BlockScopes: $0BlockScopes, Snippets: append(commonSnippets, $0Snippets...)},
  ```
  These power `GET /api/dts/fields/$0`, the template-editor field picker in PoracleWeb, and the `/api/dts/enrich` dry-run endpoint.

### 13. Test data (poracle-test)

- [ ] Add at least one entry to `fallbacks/testdata.json` keyed by `{"type": "$0", "test": "default", "location": "...", "webhook": {...}}`. Add multiple entries for distinct scenarios (cf. `fort_update` has `edit`, `editdesc`, `editloc`, `editdescimg`, `editall`, `new`, `remove`).
- [ ] Add `case "$0":` in **both** `processor/cmd/processor/test.go:64` (process route) and `:443` (mapping function), plus `processor/cmd/processor/enrich.go:50` if the type has a dedicated enrichment dry-run path.
- [ ] Add `"$0"` to `validHooks` in `processor/internal/bot/commands/poracletest.go:89` and the alias mapping at `:303` if the type's webhook name differs from its tracking name (e.g. `fort_update` → `fort-update`).
- [ ] Confirm `!poracle-test $0,default` in DM produces a delivered message. Custom variants are added to `config/testdata.json` (override-by-key merge — config wins on collision).

### 14. Config

- [ ] Add `Disable$0 bool \`toml:"disable_$0"\`` to the `[general]` section of `processor/internal/config/config.go` (alongside `DisableQuest`, `DisableFortUpdate`).
- [ ] Add `disable_$0 = false` to the `[general]` section of `config/config.example.toml` (~line 207, in the `disable_*` block).
- [ ] Add a matching `Field` entry under the `general` section in `processor/internal/api/config_schema.go` (~line 106). This makes the toggle visible in the PoracleWeb config editor:
  ```go
  {Name: "disable_$0", Type: "bool", Default: false, Description: "Disable $0 webhook processing", HotReload: false},
  ```
- [ ] If $0 has its own subsection (rare — most types fit inside `[general]` and `[fallbacks]`), define a config struct and matching `Section` entry in `config_schema.go`. `config_resolve.go`, `config_migrate.go`, `config_values.go`, and `config_validate.go` are all schema-driven and need no per-type code.
- [ ] If $0 needs a per-type tile style, document the maptype name in the `[geocoding] static_map_type` description (~line 345 of `config_schema.go`) — but the wiring (a `map[string]string`) accepts any key with no struct change.

### 15. i18n

- [ ] Add bot-facing strings to `processor/internal/i18n/locale/en.json`:
  - `cmd.$0` — the verb (e.g. `"$0"`)
  - `section.$0s` — `!tracked` heading (e.g. `"**$0s:**"`)
  - `section.$0s.none` — empty-state line
  - `tracking.$0_fmt` and any rule-formatting keys your `$0RowText` uses
- [ ] Translations for other shipped locales: any keys missing from a locale fall back to English per-key (see `Bundle.LinkFallbacks`), so partial coverage is fine — the Crowdin export will pick the new keys up.

### 16. Documentation

- [ ] Add a `## $0 (\`$0\`)` section to `DTS.md` listing template fields with their type and description. Mirror the Quest / Invasion / Fort sections (table layout, then any sub-sections for nested arrays / objects).
- [ ] Add `$0` to the contents listing at the top of `DTS.md` if there is one.
- [ ] Update `CLAUDE.md` only if the new type changes high-level data flow, matching semantics, or a cross-cutting invariant. Per-type details belong in DTS.md / API.md, not CLAUDE.md.
- [ ] Add `$0` to API.md's "Tracking types" line (the per-type CRUD endpoints share a uniform pattern; usually no other API doc change needed).

### 17. Sanity checks before calling it done

- [ ] `go test ./...` — full suite green; new tests cover the matcher, the rowtext, the bot command, and the API CRUD path.
- [ ] `!poracle-test $0,default` end-to-end produces a delivered alert in DM (try both Discord and Telegram if both are configured).
- [ ] `!tracked` lists the new rule under the new section header with the right description; `!$0 remove ...` removes it; duplicate add returns 👌; `!untrack id:N` works (delegates to the same store).
- [ ] `GET /api/tracking/$0/{id}` returns the rule with `description` populated; `POST` diff path correctly creates / updates / no-ops; `DELETE byUid/{uid}` works.
- [ ] `GET /api/dts/fields/$0` returns the fields you listed; `GET /api/dts/testdata?type=$0` returns the new test scenarios; `POST /api/dts/render` with a sample template renders.
- [ ] `[general] disable_$0 = true` silently drops the type end-to-end (handler returns early, command still parses but produces no DB writes, `!tracked` skips the section).
- [ ] `!backup` includes $0 rows; `!apply <name>` restores them; `!unregister` cascades cleanly; profile delete cascades cleanly.
- [ ] Reload semantics: a `POST /api/reload` after creating a rule via API picks it up without restart; matched events fire on the next webhook of that type.

## Reference files (read these first)

The most recent and most complete walk-through of all 17 sections is the `fort` type. Read these in order:

```
processor/internal/db/migrations/000002_add_maxbattle.{up,down}.sql   # migration template
processor/internal/db/forts.go                                         # {Type}Tracking, {Type}TrackingAPI, query helpers
processor/internal/state/state.go + loader.go                          # state wiring
processor/internal/store/tracking_sql.go                               # store generic + UID accessors
processor/internal/matching/fort.go + fort_test.go                     # matcher
processor/internal/enrichment/fort.go                                  # base + Translate
processor/cmd/processor/fort.go                                        # webhook handler
processor/cmd/processor/main.go                                        # routes + command registration + matcher init
processor/internal/api/trackingFort.go                                 # CRUD handlers
processor/internal/api/trackingAll.go                                  # enrichForts + branch in both loops
processor/internal/bot/commands/fort.go + fort_test.go                 # bot command
processor/internal/bot/commands/tracked.go                             # !tracked block
processor/internal/rowtext/fort.go                                     # rowtext
processor/internal/api/dts_fields.go                                   # fortUpdateFields + fieldsByType row
processor/internal/api/config_schema.go                                # disable_fort_update
fallbacks/dts.json                                                     # default template entry
fallbacks/testdata.json                                                # test scenarios
config/config.example.toml                                             # disable_fort_update line
processor/internal/i18n/locale/en.json                                 # cmd.fort, section.forts, tracking.* keys
DTS.md                                                                 # type field documentation
```
